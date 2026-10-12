package handlers

import (
	"crypto/rand"
	"encoding/json"
	"io"
	"math/big"
	"net/http"
	"tableTop/main/internal/auth"
)

type diceGroup struct {
	Qty      int   `json:"qty"`
	Sides    int   `json:"sides"`
	Modifier int64 `json:"modifier"`
	Values   []int `json:"values,omitempty"`
	Subtotal int64 `json:"subtotal"`
}

type diceRoll struct {
	Type      string      `json:"type"`
	ID        string      `json:"id"`
	RequestID string      `json:"requestId"`
	Groups    []diceGroup `json:"groups"`
	Total     int64       `json:"total"`
	Private   bool        `json:"private,omitempty"`
}

func (h *EndpointHandler) rollDice(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	r.Body = http.MaxBytesReader(w, r.Body, 16384)
	var request struct {
		RequestID string `json:"requestId"`
		Private   bool   `json:"private"`
		Groups    []struct {
			Qty      int   `json:"qty"`
			Sides    int   `json:"sides"`
			Modifier int64 `json:"modifier"`
		} `json:"groups"`
	}
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if decoder.Decode(&request) != nil || decoder.Decode(new(any)) != io.EOF || len(request.RequestID) == 0 || len(request.RequestID) > 64 || len(request.Groups) == 0 || len(request.Groups) > 100 {
		http.Error(w, "Choose dice groups to roll.", http.StatusBadRequest)
		return
	}
	if request.Private && !auth.IsGM(r) {
		http.Error(w, "Only a GM can roll privately.", http.StatusForbidden)
		return
	}
	count := 0
	for _, group := range request.Groups {
		switch group.Sides {
		case 4, 6, 8, 10, 12, 20, 100:
		default:
			http.Error(w, "Choose a supported die type.", http.StatusBadRequest)
			return
		}
		count += group.Qty
		if group.Qty < 1 || group.Qty > 100 || count > 100 || group.Modifier < -1000000 || group.Modifier > 1000000 {
			http.Error(w, "Roll up to 100 dice with modifiers between -1000000 and 1000000.", http.StatusBadRequest)
			return
		}
	}
	result := diceRoll{Type: "diceRolled", ID: rand.Text(), RequestID: request.RequestID, Private: request.Private}
	for _, group := range request.Groups {
		rolled := diceGroup{Qty: group.Qty, Sides: group.Sides, Modifier: group.Modifier, Subtotal: group.Modifier}
		for range group.Qty {
			value, err := rand.Int(rand.Reader, big.NewInt(int64(group.Sides)))
			if err != nil {
				http.Error(w, "Could not roll dice. Try again.", http.StatusInternalServerError)
				return
			}
			die := int(value.Int64()) + 1
			rolled.Values = append(rolled.Values, die)
			rolled.Subtotal += int64(die)
		}
		result.Groups = append(result.Groups, rolled)
		result.Total += rolled.Subtotal
	}
	data, _ := json.Marshal(result)
	if !result.Private {
		h.webSocketHandler.broadcastUnits(data)
	}
	w.Header().Set("Content-Type", "application/json")
	w.Write(data)
}
