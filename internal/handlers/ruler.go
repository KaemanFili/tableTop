package handlers

import (
	"encoding/json"
	"math"
)

// Coordinates are grid squares so every board uses the same distance and location.
type rulerPoint struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
}

type boardRuler struct {
	Type  string      `json:"type"`
	ID    string      `json:"id"`
	Start *rulerPoint `json:"start,omitempty"`
	End   *rulerPoint `json:"end,omitempty"`
	Clear bool        `json:"clear,omitempty"`
}

func validRulerPoint(point *rulerPoint) bool {
	return point != nil && !math.IsNaN(point.X) && !math.IsNaN(point.Y) &&
		!math.IsInf(point.X, 0) && !math.IsInf(point.Y, 0) &&
		point.X >= 0 && point.Y >= 0 && point.X <= 64 && point.Y <= 64
}

func (h *WebSocketHandler) updateRuler(id string, data []byte) {
	var ruler boardRuler
	if json.Unmarshal(data, &ruler) != nil || ruler.Type != "rulerChanged" {
		return
	}
	if !ruler.Clear && (!validRulerPoint(ruler.Start) || !validRulerPoint(ruler.End)) {
		return
	}
	ruler.ID = id
	if ruler.Clear {
		ruler.Start, ruler.End = nil, nil
	}
	h.unitCreatedMutex.Lock()
	defer h.unitCreatedMutex.Unlock()
	if ruler.Clear {
		delete(h.rulers, id)
	} else {
		h.rulers[id] = ruler
	}
	message, _ := json.Marshal(ruler)
	h.broadcastUnitsLocked(message)
}
