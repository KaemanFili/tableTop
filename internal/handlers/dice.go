package handlers

import (
	"crypto/rand"
	"encoding/json"
	"errors"
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
	Type       string      `json:"type"`
	ID         string      `json:"id"`
	RequestID  string      `json:"requestId"`
	Groups     []diceGroup `json:"groups"`
	Total      int64       `json:"total"`
	Private    bool        `json:"private,omitempty"`
	RollerName string      `json:"rollerName"`
}

type diceGroupRequest struct {
	Qty      int   `json:"qty"`
	Sides    int   `json:"sides"`
	Modifier int64 `json:"modifier"`
}

type diceRollRequest struct {
	RequestID string             `json:"requestId"`
	Private   bool               `json:"private"`
	Groups    []diceGroupRequest `json:"groups"`
}

func (h *EndpointHandler) rollDice(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	r.Body = http.MaxBytesReader(w, r.Body, 16384)
	request, err := decodeDiceRollRequest(r.Body)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if request.Private && !auth.IsGM(r) {
		http.Error(w, "Only a GM can roll privately.", http.StatusForbidden)
		return
	}
	if err := validateDiceGroups(request.Groups); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	result, err := generateDiceRoll(request, diceRollerName(r))
	if err != nil {
		http.Error(w, "Could not roll dice. Try again.", http.StatusInternalServerError)
		return
	}
	data, _ := json.Marshal(result)
	if !result.Private {
		h.webSocketHandler.broadcastUnits(data)
	}
	w.Header().Set("Content-Type", "application/json")
	w.Write(data)
}

func decodeDiceRollRequest(body io.Reader) (diceRollRequest, error) {
	var request diceRollRequest
	decoder := json.NewDecoder(body)
	decoder.DisallowUnknownFields()
	if decoder.Decode(&request) != nil || decoder.Decode(new(any)) != io.EOF || len(request.RequestID) == 0 || len(request.RequestID) > 64 || len(request.Groups) == 0 || len(request.Groups) > 100 {
		return diceRollRequest{}, errors.New("Choose dice groups to roll.")
	}
	return request, nil
}

func validateDiceGroups(groups []diceGroupRequest) error {
	count := 0
	for _, group := range groups {
		switch group.Sides {
		case 4, 6, 8, 10, 12, 20, 100:
		default:
			return errors.New("Choose a supported die type.")
		}
		count += group.Qty
		if group.Qty < 1 || group.Qty > 100 || count > 100 || group.Modifier < -1000000 || group.Modifier > 1000000 {
			return errors.New("Roll up to 100 dice with modifiers between -1000000 and 1000000.")
		}
	}
	return nil
}

func diceRollerName(r *http.Request) string {
	if user := auth.CurrentUser(r); user != nil {
		return user.Name
	}
	if auth.IsGM(r) {
		return "GM"
	}
	return "Player"
}

// Groups have already passed validation before random values are generated.
func generateDiceRoll(request diceRollRequest, rollerName string) (diceRoll, error) {
	result := diceRoll{
		Type: "diceRolled", ID: rand.Text(), RequestID: request.RequestID,
		Private: request.Private, RollerName: rollerName,
	}
	for _, group := range request.Groups {
		rolled, err := rollDiceGroup(group)
		if err != nil {
			return diceRoll{}, err
		}
		result.Groups = append(result.Groups, rolled)
		result.Total += rolled.Subtotal
	}
	return result, nil
}

func rollDiceGroup(group diceGroupRequest) (diceGroup, error) {
	rolled := diceGroup{Qty: group.Qty, Sides: group.Sides, Modifier: group.Modifier, Subtotal: group.Modifier}
	for range group.Qty {
		value, err := rand.Int(rand.Reader, big.NewInt(int64(group.Sides)))
		if err != nil {
			return diceGroup{}, err
		}
		die := int(value.Int64()) + 1
		rolled.Values = append(rolled.Values, die)
		rolled.Subtotal += int64(die)
	}
	return rolled, nil
}
