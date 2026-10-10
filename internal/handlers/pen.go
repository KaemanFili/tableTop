package handlers

import (
	"encoding/json"
	"math"
	"regexp"
	"strconv"
)

const maxStrokePoints = 8192

var penColorPattern = regexp.MustCompile(`^#[0-9a-fA-F]{6}$`)

type penStroke struct {
	Type     string       `json:"type"`
	ID       string       `json:"id"`
	Owner    string       `json:"owner"`
	Offset   int          `json:"offset"`
	Points   []rulerPoint `json:"points"`
	Finished bool         `json:"finished"`
	Color    string       `json:"color"`
	Width    float64      `json:"width"`
}

func (h *WebSocketHandler) updatePen(owner string, data []byte) {
	var update penStroke
	if json.Unmarshal(data, &update) != nil || update.Type != "penStroke" {
		return
	}
	if number, err := strconv.ParseUint(update.ID, 10, 64); err != nil || number == 0 {
		return
	}
	if update.Offset < 0 || len(update.Points) > 16 || (len(update.Points) == 0 && !update.Finished) {
		return
	}
	for _, point := range update.Points {
		if !validRulerPoint(&point) {
			return
		}
	}
	update.ID = owner + "/" + update.ID
	update.Owner = owner
	h.unitCreatedMutex.Lock()
	defer h.unitCreatedMutex.Unlock()
	if h.erasedStrokes[update.ID] {
		return
	}
	stroke, exists := h.strokes[update.ID]
	if !exists {
		if update.Offset != 0 || len(update.Points) == 0 {
			return
		}
		if update.Color == "" {
			update.Color = "#1d4ed8"
		}
		if update.Width == 0 {
			update.Width = 3
		}
		if !penColorPattern.MatchString(update.Color) || math.IsNaN(update.Width) || math.IsInf(update.Width, 0) || update.Width < 1 || update.Width > 12 {
			return
		}
		stroke = penStroke{Type: "penStroke", ID: update.ID, Owner: owner, Color: update.Color, Width: update.Width}
	}
	if stroke.Finished || update.Offset != len(stroke.Points) || len(stroke.Points)+len(update.Points) > maxStrokePoints {
		return
	}
	stroke.Points = append(stroke.Points, update.Points...)
	stroke.Finished = update.Finished
	h.strokes[stroke.ID] = stroke
	// Each stroke keeps the style chosen when it started, including later batches.
	update.Color, update.Width = stroke.Color, stroke.Width
	message, _ := json.Marshal(update)
	h.broadcastUnitsLocked(message)
}

func (h *WebSocketHandler) erasePen(data []byte) {
	var request struct {
		Type string   `json:"type"`
		IDs  []string `json:"ids"`
	}
	if json.Unmarshal(data, &request) != nil || request.Type != "penErase" || len(request.IDs) == 0 || len(request.IDs) > 16 {
		return
	}
	h.unitCreatedMutex.Lock()
	defer h.unitCreatedMutex.Unlock()
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
		delete(h.strokes, id)
		// Remember removed IDs so a late drawing batch cannot recreate an erased path.
		h.erasedStrokes[id] = true
		ids = append(ids, id)
	}
	if len(ids) == 0 {
		return
	}
	message, _ := json.Marshal(struct {
		Type string   `json:"type"`
		IDs  []string `json:"ids"`
	}{"penErased", ids})
	h.broadcastUnitsLocked(message)
}
