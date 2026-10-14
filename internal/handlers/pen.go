package handlers

import (
	"encoding/json"
	"math"
	"regexp"
	"strconv"

	"tableTop/main/internal/app/model"
)

const maxStrokePoints = 8192

var penColorPattern = regexp.MustCompile(`^#[0-9a-fA-F]{6}$`)

type penStroke = model.PenStroke

func (h *WebSocketHandler) updatePen(owner string, data []byte) error {
	var update penStroke
	if json.Unmarshal(data, &update) != nil || update.Type != "penStroke" {
		return nil
	}
	if number, err := strconv.ParseUint(update.ID, 10, 64); err != nil || number == 0 {
		return nil
	}
	if update.Offset < 0 || len(update.Points) > 16 || (len(update.Points) == 0 && !update.Finished) {
		return nil
	}
	for _, point := range update.Points {
		if !validRulerPoint(&point) {
			return nil
		}
	}
	update.ID = owner + "/" + update.ID
	update.Owner = owner
	h.boardMutex.Lock()
	defer h.boardMutex.Unlock()
	if update.Generation != h.boardGeneration {
		return nil
	}
	if h.erasedStrokes[update.ID] {
		return nil
	}
	stroke, exists := h.strokes[update.ID]
	if (!exists && len(h.strokes) >= model.MaxBoardStrokes) || len(update.Points) > model.MaxBoardPoints-h.drawingPoints {
		return model.ErrCapacity
	}
	if !exists {
		if update.Offset != 0 || len(update.Points) == 0 {
			return nil
		}
		if update.Color == "" {
			update.Color = "#1d4ed8"
		}
		if update.Width == 0 {
			update.Width = 3
		}
		if !penColorPattern.MatchString(update.Color) || math.IsNaN(update.Width) || math.IsInf(update.Width, 0) || update.Width < 1 || update.Width > 240 {
			return nil
		}
		stroke = penStroke{Type: "penStroke", ID: update.ID, Owner: owner, Color: update.Color, Width: update.Width, Generation: update.Generation}
	}
	if stroke.Finished || update.Offset != len(stroke.Points) || len(stroke.Points)+len(update.Points) > maxStrokePoints {
		return nil
	}
	h.drawingPoints += len(update.Points)
	stroke.Points = append(stroke.Points, update.Points...)
	stroke.Finished = update.Finished
	h.strokes[stroke.ID] = stroke
	// Each stroke keeps the style chosen when it started, including later batches.
	update.Color, update.Width = stroke.Color, stroke.Width
	message, _ := json.Marshal(update)
	h.broadcastUnitsLocked(message)
	return nil
}

func (h *WebSocketHandler) erasePen(data []byte) error {
	var request struct {
		Type string   `json:"type"`
		IDs  []string `json:"ids"`
	}
	if json.Unmarshal(data, &request) != nil || request.Type != "penErase" || len(request.IDs) == 0 || len(request.IDs) > 16 {
		return nil
	}
	h.boardMutex.Lock()
	defer h.boardMutex.Unlock()
	if len(request.IDs) > model.MaxErasedStrokes-len(h.erasedStrokes) {
		return model.ErrCapacity
	}
	ids := make([]string, 0, len(request.IDs))
	seen := make(map[string]bool)
	for _, id := range request.IDs {
		if seen[id] {
			continue
		}
		seen[id] = true
		_, exists := h.strokes[id]
		if !exists && !h.erasedStrokes[id] {
			continue
		}
		h.drawingPoints -= len(h.strokes[id].Points)
		delete(h.strokes, id)
		// Remember removed IDs so a late drawing batch cannot recreate an erased path.
		h.erasedStrokes[id] = true
		ids = append(ids, id)
	}
	if len(ids) == 0 {
		return nil
	}
	message, _ := json.Marshal(struct {
		Type string   `json:"type"`
		IDs  []string `json:"ids"`
	}{"penErased", ids})
	h.broadcastUnitsLocked(message)
	return nil
}
