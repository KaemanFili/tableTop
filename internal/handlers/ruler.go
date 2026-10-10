package handlers

import (
	"encoding/json"
	"math"
	"tableTop/main/internal/app"
)

// Coordinates are grid squares so every board uses the same distance and location.
type rulerPoint = app.BoardPoint

type boardRuler = app.BoardRuler

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
	if ruler.Generation != h.boardGeneration {
		return
	}
	if ruler.Clear {
		delete(h.rulers, id)
	} else {
		h.rulers[id] = ruler
	}
	message, _ := json.Marshal(ruler)
	h.broadcastUnitsLocked(message)
}
