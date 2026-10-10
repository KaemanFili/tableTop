package handlers

import (
	"encoding/json"
	"net/http"
)

func (h *EndpointHandler) clearBoard(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if r.URL.Query().Get("gm") != "true" {
		http.Error(w, "Only a GM can clear the map.", http.StatusForbidden)
		return
	}
	ws := h.webSocketHandler
	ws.unitCreatedMutex.Lock()
	defer ws.unitCreatedMutex.Unlock()
	ws.clearBoardLocked()
	w.WriteHeader(http.StatusNoContent)
}

// clearBoardLocked requires unitCreatedMutex, including when loading another map.
func (ws *WebSocketHandler) clearBoardLocked() {
	ids := ws.state.ClearUnits()
	clear(ws.rulers)
	clear(ws.strokes)
	clear(ws.erasedStrokes)
	ws.fogMarks = nil
	ws.currentMap = nil
	ws.boardGeneration++
	message, _ := json.Marshal(struct {
		Type       string   `json:"type"`
		IDs        []string `json:"ids"`
		Generation uint64   `json:"generation"`
	}{"boardCleared", ids, ws.boardGeneration})
	ws.broadcastUnitsLocked(message)
}
