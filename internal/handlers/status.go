package handlers

import (
	"encoding/json"
	"errors"
	"net/http"

	"tableTop/main/internal/app"
)

type unitStatusChange struct {
	ID     string         `json:"id"`
	Status app.UnitStatus `json:"status"`
	Color  string         `json:"color"`
}

func (h *EndpointHandler) setUnitStatus(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	r.Body = http.MaxBytesReader(w, r.Body, 4096)
	if err := r.ParseForm(); err != nil || !r.PostForm.Has("status") {
		http.Error(w, "Choose a status.", http.StatusBadRequest)
		return
	}
	unit, err := h.state.SetUnitStatus(r.PathValue("id"), app.UnitStatus(r.PostFormValue("status")))
	if err != nil {
		code := http.StatusBadRequest
		if errors.Is(err, app.ErrUnitNotFound) {
			code = http.StatusNotFound
		}
		http.Error(w, err.Error(), code)
		return
	}
	change := unitStatusChange{unit.ID, unit.Status, h.state.StatusColor(unit.Status)}
	trigger, _ := json.Marshal(map[string]any{"unitStatusChanged": change})
	w.Header().Set("HX-Trigger", string(trigger))
	data, _ := json.Marshal(struct {
		Type string `json:"type"`
		unitStatusChange
	}{"unitStatusChanged", change})
	h.webSocketHandler.broadcastUnits(data)
	w.WriteHeader(http.StatusNoContent)
}
