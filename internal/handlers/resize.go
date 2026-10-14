package handlers

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"

	"tableTop/main/internal/app/model"
)

type unitSizeChange struct {
	ID      string         `json:"id"`
	Size    model.UnitSize `json:"size"`
	Squares float64        `json:"squares"`
	Label   string         `json:"label"`
}

func sizeChange(unit model.UnitData) unitSizeChange {
	return unitSizeChange{ID: unit.ID, Size: unit.Size, Squares: unit.Size.Squares(), Label: unit.Size.Label()}
}

func (h *EndpointHandler) resizeUnit(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	r.Body = http.MaxBytesReader(w, r.Body, 4096)
	if err := r.ParseForm(); err != nil {
		http.Error(w, "Choose up or down.", http.StatusBadRequest)
		return
	}
	unit, changed, err := h.state.ResizeUnit(r.PathValue("id"), r.PostFormValue("direction"))
	if err != nil {
		switch {
		case errors.Is(err, model.ErrInvalidSizeDirection):
			http.Error(w, "Choose up or down.", http.StatusBadRequest)
		case errors.Is(err, model.ErrUnitNotFound):
			http.Error(w, "This unit is no longer on the board.", http.StatusNotFound)
		default:
			log.Println("Resize unit:", err)
			http.Error(w, "Could not resize this unit.", http.StatusInternalServerError)
		}
		return
	}
	if changed {
		h.webSocketHandler.BroadcastUnitSizeChanged(unit)
	}
	trigger, _ := json.Marshal(map[string]any{"unitSizeChanged": sizeChange(unit)})
	w.Header().Set("HX-Trigger", string(trigger))
	w.WriteHeader(http.StatusNoContent)
}

func (h *WebSocketHandler) BroadcastUnitSizeChanged(unit model.UnitData) {
	data, _ := json.Marshal(struct {
		Type string `json:"type"`
		unitSizeChange
	}{"unitSizeChanged", sizeChange(unit)})
	h.broadcastUnits(data)
}
