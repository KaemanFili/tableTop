package handlers

import (
	"encoding/json"
	"math"
	"strconv"
	"tableTop/main/internal/app"
)

// Paint and erasure are ordered brush marks; replaying them restores partial holes.
type fogMark = app.FogMark

func (h *WebSocketHandler) updateFog(owner string, data []byte) error {
	var mark fogMark
	if json.Unmarshal(data, &mark) != nil || mark.Type != "fogMark" || (mark.Mode != "paint" && mark.Mode != "erase") {
		return nil
	}
	if number, err := strconv.ParseUint(mark.ID, 10, 64); err != nil || number == 0 {
		return nil
	}
	if len(mark.Points) == 0 || len(mark.Points) > 16 || math.IsNaN(mark.Width) || math.IsInf(mark.Width, 0) || mark.Width < 0.25 || mark.Width > 6 {
		return nil
	}
	for _, point := range mark.Points {
		if !validRulerPoint(&point) {
			return nil
		}
	}
	mark.ID, mark.Owner = owner+"/"+mark.ID, owner
	h.boardMutex.Lock()
	defer h.boardMutex.Unlock()
	if mark.Generation != h.boardGeneration {
		return nil
	}
	if len(h.fogMarks) >= app.MaxFogMarks || len(mark.Points) > app.MaxBoardPoints-h.drawingPoints {
		return app.ErrCapacity
	}
	h.drawingPoints += len(mark.Points)
	h.fogMarks = append(h.fogMarks, mark)
	message, _ := json.Marshal(mark)
	h.broadcastUnitsLocked(message)
	return nil
}

// Replace fog history so whole-board actions also release old drawing capacity.
func (h *WebSocketHandler) setFog(owner string, data []byte) {
	var request struct {
		Covered    *bool  `json:"covered"`
		Generation uint64 `json:"generation"`
	}
	if json.Unmarshal(data, &request) != nil || request.Covered == nil {
		return
	}
	h.boardMutex.Lock()
	defer h.boardMutex.Unlock()
	if request.Generation != h.boardGeneration {
		return
	}
	for _, mark := range h.fogMarks {
		h.drawingPoints -= len(mark.Points)
	}
	h.fogMarks = nil
	h.broadcastValueLocked(map[string]string{"type": "fogReset"})
	if *request.Covered {
		mark := fogMark{Type: "fogMark", ID: owner + "/fill", Owner: owner, Mode: "fill", Generation: h.boardGeneration}
		h.fogMarks = append(h.fogMarks, mark)
		h.broadcastValueLocked(mark)
	}
}
