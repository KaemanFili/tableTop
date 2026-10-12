package handlers

import (
	"bytes"
	"crypto/rand"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"os"
	"strconv"
)

func (h *EndpointHandler) mapPicker(w http.ResponseWriter, r *http.Request) {
	if !h.requireMapGM(w, r) {
		return
	}
	var rendered bytes.Buffer
	if err := h.indexTemplate.ExecuteTemplate(&rendered, "map-picker", h.mapLibrary.Maps()); err != nil {
		log.Println("Render map picker:", err)
		http.Error(w, "Could not open saved maps.", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write(rendered.Bytes())
}

func (h *EndpointHandler) loadMap(w http.ResponseWriter, r *http.Request) {
	if !h.requireMapGM(w, r) {
		return
	}
	ws := h.webSocketHandler
	ws.boardMutex.Lock()
	defer ws.boardMutex.Unlock()
	definition, err := h.mapLibrary.LoadMap(r.PathValue("id"))
	if errors.Is(err, os.ErrNotExist) {
		http.Error(w, "Saved map not found.", http.StatusNotFound)
		return
	}
	if err != nil {
		log.Println("Load saved map:", err)
		http.Error(w, "Could not read this saved map.", http.StatusInternalServerError)
		return
	}
	if err := definition.Validate(); err != nil {
		http.Error(w, "Invalid saved map.", http.StatusConflict)
		return
	}
	path, _ := h.mapLibrary.ImagePath(definition.ID)
	if _, err := os.Stat(path); err != nil {
		http.Error(w, "The saved map image is unavailable.", http.StatusInternalServerError)
		return
	}
	board := definition.Board
	if err := board.Validate(); err != nil {
		http.Error(w, err.Error(), http.StatusConflict)
		return
	}
	// Send only the image and fit in boardMapChanged; utilities have their own messages.
	definition.Board = nil
	ws.clearBoardLocked()
	ws.currentMap = &definition
	ws.broadcastValueLocked(map[string]any{"type": "boardMapChanged", "map": definition})
	if board != nil {
		h.state.RestoreUnits(board.Units)
		// Fresh utility IDs keep delayed erase/drawing messages from targeting loaded content.
		owner := "loaded/" + rand.Text()
		for index, stroke := range board.Strokes {
			ws.drawingPoints += len(stroke.Points)
			stroke.ID, stroke.Owner = owner+"/"+strconv.Itoa(index+1), owner
			stroke.Offset, stroke.Finished, stroke.Generation = 0, true, ws.boardGeneration
			ws.strokes[stroke.ID] = stroke
			ws.broadcastValueLocked(stroke)
		}
		for index, mark := range board.Fog {
			ws.drawingPoints += len(mark.Points)
			mark.ID, mark.Owner, mark.Generation = owner+"/"+strconv.Itoa(index+1), owner, ws.boardGeneration
			ws.fogMarks = append(ws.fogMarks, mark)
			ws.broadcastValueLocked(mark)
		}
		for index, ruler := range board.Rulers {
			ruler.ID, ruler.Generation = owner+"/"+strconv.Itoa(index+1), ws.boardGeneration
			ws.rulers[ruler.ID] = ruler
			ws.broadcastValueLocked(ruler)
		}
	}
	ws.broadcastValueLocked("unit created")
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write([]byte(`<p role="status" data-map-saved>Map loaded.</p>`))
}

func (ws *WebSocketHandler) broadcastValueLocked(value any) {
	message, _ := json.Marshal(value)
	ws.broadcastUnitsLocked(message)
}

func (h *EndpointHandler) deleteMap(w http.ResponseWriter, r *http.Request) {
	if !h.requireMapGM(w, r) {
		return
	}
	ws := h.webSocketHandler
	ws.boardMutex.Lock()
	id := r.PathValue("id")
	if err := h.mapLibrary.Delete(id); err != nil {
		ws.boardMutex.Unlock()
		log.Println("Delete map:", err)
		http.Error(w, "The map could not be deleted. Please try again.", http.StatusInternalServerError)
		return
	}
	if ws.currentMap != nil && ws.currentMap.ID == id {
		ws.currentMap = nil
		ws.broadcastValueLocked(map[string]any{"type": "boardMapChanged", "map": nil})
	}
	ws.boardMutex.Unlock()
	h.mapPicker(w, r)
}
