package app

import (
	"errors"
	"regexp"
	"slices"
	"strings"
	"sync"
	"unicode/utf8"
)

type UnitStatus string

type UnitStatusOption struct {
	Value UnitStatus `bson:"value"`
	Color string     `bson:"color"`
}

// Definitions are loaded once at startup; readers receive their own slice.
var statusDefinitions = struct {
	sync.RWMutex
	options []UnitStatusOption
}{options: defaultUnitStatuses()}

func UnitStatuses() []UnitStatusOption {
	statusDefinitions.RLock()
	defer statusDefinitions.RUnlock()
	return slices.Clone(statusDefinitions.options)
}

var statusColorPattern = regexp.MustCompile(`^#[0-9a-fA-F]{6}$`)

func validateUnitStatuses(options []UnitStatusOption) error {
	if len(options) == 0 {
		return errors.New("at least one status definition is required")
	}
	if len(options) > 256 {
		return errors.New("too many status definitions")
	}
	seen := make(map[UnitStatus]bool)
	for _, option := range options {
		name := string(option.Value)
		if name == "" || strings.TrimSpace(name) != name || !utf8.ValidString(name) || utf8.RuneCountInString(name) > 80 || seen[option.Value] || !statusColorPattern.MatchString(option.Color) {
			return errors.New("invalid status definition: names must be unique and colors must use #RRGGBB")
		}
		seen[option.Value] = true
	}
	return nil
}

func setUnitStatuses(options []UnitStatusOption) error {
	if err := validateUnitStatuses(options); err != nil {
		return err
	}
	statusDefinitions.Lock()
	defer statusDefinitions.Unlock()
	statusDefinitions.options = slices.Clone(options)
	return nil
}

func defaultUnitStatuses() []UnitStatusOption {
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
