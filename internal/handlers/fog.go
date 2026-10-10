package handlers

import (
	"encoding/json"
	"math"
	"strconv"
	"tableTop/main/internal/app"
)

// Paint and erasure are ordered brush marks; replaying them restores partial holes.
type fogMark = app.FogMark

func (h *WebSocketHandler) updateFog(owner string, data []byte) {
	var mark fogMark
	if json.Unmarshal(data, &mark) != nil || mark.Type != "fogMark" || (mark.Mode != "paint" && mark.Mode != "erase") {
		return
	}
	if number, err := strconv.ParseUint(mark.ID, 10, 64); err != nil || number == 0 {
		return
	}
	if len(mark.Points) == 0 || len(mark.Points) > 16 || math.IsNaN(mark.Width) || math.IsInf(mark.Width, 0) || mark.Width < 0.25 || mark.Width > 6 {
		return
	}
	for _, point := range mark.Points {
		if !validRulerPoint(&point) {
			return
		}
	}
	mark.ID, mark.Owner = owner+"/"+mark.ID, owner
	h.unitCreatedMutex.Lock()
	defer h.unitCreatedMutex.Unlock()
	if mark.Generation != h.boardGeneration {
		return
	}
	h.fogMarks = append(h.fogMarks, mark)
	message, _ := json.Marshal(mark)
	h.broadcastUnitsLocked(message)
}
