package handlers

import (
	"encoding/json"
	"io"
	"net/http"
)

type boardPing struct {
	X          *int   `json:"x"`
	Y          *int   `json:"y"`
	Generation uint64 `json:"generation"`
}

func (h *EndpointHandler) pingBoard(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	r.Body = http.MaxBytesReader(w, r.Body, 1024)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	var ping boardPing
	if err := decoder.Decode(&ping); err != nil || ping.X == nil || ping.Y == nil {
		http.Error(w, "Choose a point on the board.", http.StatusBadRequest)
		return
	}
	if decoder.Decode(new(any)) != io.EOF || *ping.X < 0 || *ping.Y < 0 || *ping.X > 1000000 || *ping.Y > 1000000 {
		http.Error(w, "Choose a point on the board.", http.StatusBadRequest)
		return
	}
	data, _ := json.Marshal(struct {
		Type string `json:"type"`
		boardPing
	}{"boardPing", ping})
	ws := h.webSocketHandler
	ws.boardMutex.Lock()
	defer ws.boardMutex.Unlock()
	if ping.Generation != ws.boardGeneration {
		w.WriteHeader(http.StatusConflict)
		return
	}
	ws.broadcastUnitsLocked(data)
	w.WriteHeader(http.StatusNoContent)
}
