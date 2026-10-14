package model

import "slices"

func ValidateUnitStatuses(options []UnitStatusOption) error { return validateUnitStatuses(options) }
func DefaultUnitStatuses() []UnitStatusOption               { return defaultUnitStatuses() }

func (s *AppState) UnitStatuses() []UnitStatusOption { return slices.Clone(s.statuses) }
func (s *AppState) StatusColor(status UnitStatus) string {
	for _, option := range s.statuses {
		if option.Value == status {
			return option.Color
		}
	}
	return "transparent"
}
func NewStateWithStatuses(values []UnitStatusOption) (*AppState, error) {
	if err := validateUnitStatuses(values); err != nil {
		return nil, err
	}
	s := NewState()
	s.statuses = slices.Clone(values)
	return s, nil
}
