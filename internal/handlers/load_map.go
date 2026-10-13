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
	"tableTop/main/internal/app"
)

func (h *EndpointHandler) mapPicker(w http.ResponseWriter, r *http.Request) {
	if !h.requireMapGM(w, r) {
		return
	}
	if err := h.mapLibrary.ValidateDefaults(); err != nil {
		http.Error(w, err.Error(), http.StatusServiceUnavailable)
		return
	}
	var rendered bytes.Buffer
	data := libraryGroup[app.MapDefinition]{}
	for _, m := range h.mapLibrary.Maps() {
		if m.ReadOnly {
			data.Defaults = append(data.Defaults, m)
		} else {
			data.Owned = append(data.Owned, m)
		}
	}
	if err := h.indexTemplate.ExecuteTemplate(&rendered, "map-picker", data); err != nil {
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
	if !h.validateMapForLoad(w, definition) {
		return
	}
	h.restoreMapLocked(definition)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write([]byte(`<p role="status" data-map-saved>Map loaded.</p>`))
}

func (h *EndpointHandler) validateMapForLoad(w http.ResponseWriter, definition app.MapDefinition) bool {
	if err := definition.Validate(); err != nil {
		http.Error(w, "Invalid saved map.", http.StatusConflict)
		return false
	}
	if _, _, err := h.mapLibrary.Artwork(definition.ID); err != nil {
		http.Error(w, "The saved map image is unavailable.", http.StatusInternalServerError)
		return false
	}
	board := definition.Board
	if err := board.Validate(); err != nil {
		http.Error(w, err.Error(), http.StatusConflict)
		return false
	}
	return true
}

// The caller holds boardMutex; validation must succeed before replacing live state.
func (h *EndpointHandler) restoreMapLocked(definition app.MapDefinition) {
	ws := h.webSocketHandler
	board := definition.Board
	// Send only the image and fit in boardMapChanged; utilities have their own messages.
	definition.Board = nil
	ws.clearBoardLocked()
	ws.currentMap = &definition
	ws.broadcastValueLocked(map[string]any{"type": "boardMapChanged", "map": definition})
	if board != nil {
		h.state.RestoreUnits(board.Units)
		// Fresh utility IDs keep delayed erase/drawing messages from targeting loaded content.
		owner := "loaded/" + rand.Text()
		ws.restoreStrokesLocked(board.Strokes, owner)
		ws.restoreFogLocked(board.Fog, owner)
		ws.restoreRulersLocked(board.Rulers, owner)
	}
	ws.broadcastValueLocked("unit created")
}

func (ws *WebSocketHandler) restoreStrokesLocked(values []app.PenStroke, owner string) {
	for index, stroke := range values {
		ws.drawingPoints += len(stroke.Points)
		stroke.ID, stroke.Owner = owner+"/"+strconv.Itoa(index+1), owner
		stroke.Offset, stroke.Finished, stroke.Generation = 0, true, ws.boardGeneration
		ws.strokes[stroke.ID] = stroke
		ws.broadcastValueLocked(stroke)
	}
}

func (ws *WebSocketHandler) restoreFogLocked(values []app.FogMark, owner string) {
	for index, mark := range values {
		ws.drawingPoints += len(mark.Points)
		mark.ID, mark.Owner, mark.Generation = owner+"/"+strconv.Itoa(index+1), owner, ws.boardGeneration
		ws.fogMarks = append(ws.fogMarks, mark)
		ws.broadcastValueLocked(mark)
	}
}

func (ws *WebSocketHandler) restoreRulersLocked(values []app.BoardRuler, owner string) {
	for index, ruler := range values {
		ruler.ID, ruler.Generation = owner+"/"+strconv.Itoa(index+1), ws.boardGeneration
		ws.rulers[ruler.ID] = ruler
		ws.broadcastValueLocked(ruler)
	}
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
		if errors.Is(err, app.ErrReadOnly) {
			http.Error(w, err.Error(), http.StatusConflict)
			return
		}
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
