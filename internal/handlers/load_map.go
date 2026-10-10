package handlers

import (
	"bytes"
	"crypto/rand"
	"encoding/json"
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
	definition, exists := h.mapLibrary.MapByID(r.PathValue("id"))
	if !exists {
		http.Error(w, "Saved map not found.", http.StatusNotFound)
		return
	}
	path, _ := h.mapLibrary.ImagePath(definition.ID)
	if _, err := os.Stat(path); err != nil {
		http.Error(w, "The saved map image is unavailable.", http.StatusInternalServerError)
		return
	}
	board := definition.Board
	// Send only the image and fit in boardMapChanged; utilities have their own messages.
	definition.Board = nil
	ws := h.webSocketHandler
	ws.unitCreatedMutex.Lock()
	defer ws.unitCreatedMutex.Unlock()
	ws.clearBoardLocked()
	ws.currentMap = &definition
	ws.broadcastValueLocked(map[string]any{"type": "boardMapChanged", "map": definition})
	if board != nil {
		h.state.RestoreUnits(board.Units)
		// Fresh utility IDs keep delayed erase/drawing messages from targeting loaded content.
		owner := "loaded/" + rand.Text()
		for index, stroke := range board.Strokes {
			stroke.ID, stroke.Owner = owner+"/"+strconv.Itoa(index+1), owner
			stroke.Offset, stroke.Finished, stroke.Generation = 0, true, ws.boardGeneration
			ws.strokes[stroke.ID] = stroke
			ws.broadcastValueLocked(stroke)
		}
		for index, mark := range board.Fog {
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
