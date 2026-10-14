package model

import (
	"encoding/json"
	"errors"
	"regexp"
	"slices"
	"strings"
	"unicode/utf8"

	bundled "tableTop/main/plugins"
)

type UnitStatus string

type UnitStatusOption struct {
	Value UnitStatus `json:"value" bson:"value"`
	Color string     `json:"color" bson:"color"`
}

// UnitStatuses returns the bundled defaults; live states own their session definitions.
func UnitStatuses() []UnitStatusOption { return slices.Clone(defaultUnitStatuses()) }

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

func defaultUnitStatuses() []UnitStatusOption {
	data, err := bundled.Builtin.ReadFile("builtin/1/statuses.json")
	if err != nil {
		panic(err)
	}
	var options []UnitStatusOption
	if err := json.Unmarshal(data, &options); err != nil {
		panic(err)
	}
	return options
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
	if status != "" && s.StatusColor(status) == "transparent" {
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
