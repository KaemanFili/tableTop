package app

import "errors"

type UnitStatus string

type UnitStatusOption struct {
	Value UnitStatus
	Color string
}

// eventually we should be able to add custom statuses, but for now we will just use a fixed set of statuses with colors. Good excuse for some kind of document store
// we could probably use the same document based storage for unit library
func UnitStatuses() []UnitStatusOption {
	return []UnitStatusOption{
		{"Blinded", "#64748B"},
		{"Charmed", "#EC4899"},
		{"Deafened", "#5F9EA0"},
		{"Exhaustion", "#C2410C"},
		{"Frightened", "#9333EA"},
		{"Grappled", "#F59E0B"},
		{"Incapacitated", "#DC2626"},
		{"Invisible", "#22D3EE"},
		{"Paralyzed", "#991B1B"},
		{"Petrified", "#78716C"},
		{"Poisoned", "#65A30D"},
		{"Prone", "#EAB308"},
		{"Restrained", "#D97706"},
		{"Stunned", "#D946EF"},
		{"Unconscious", "#7F1D1D"},
		{"Dead", "#450A0A"},
	}
}

func (status UnitStatus) Color() string {
	for _, option := range UnitStatuses() {
		if option.Value == status {
			return option.Color
		}
	}
	return "transparent"
}

var ErrInvalidStatus = errors.New("unknown status")

func (s *AppState) SetUnitStatus(id string, status UnitStatus) (UnitData, error) {
	if status != "" && status.Color() == "transparent" {
		return UnitData{}, ErrInvalidStatus
	}
	s.mutex.Lock()
	defer s.mutex.Unlock()
	unit, exists := s.units[id]
	if !exists {
		return UnitData{}, ErrUnitNotFound
	}
	unit.Status = status
	s.units[id] = unit
	return unit, nil
}
