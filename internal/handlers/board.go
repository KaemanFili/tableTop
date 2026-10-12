package handlers

import (
	"encoding/json"
	"net/http"
	"tableTop/main/internal/auth"
)

func (h *EndpointHandler) clearBoard(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if !auth.IsGM(r) {
		http.Error(w, "Only a GM can clear the map.", http.StatusForbidden)
		return
	}
	ws := h.webSocketHandler
	ws.boardMutex.Lock()
	defer ws.boardMutex.Unlock()
	ws.clearBoardLocked()
	w.WriteHeader(http.StatusNoContent)
}

// clearBoardLocked requires boardMutex, including when loading another map.
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
