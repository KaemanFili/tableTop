package app

import (
	"errors"
	"slices"
)

var (
	ErrInvalidSizeDirection = errors.New("choose up or down")
	ErrUnitNotFound         = errors.New("unit not found")
)

// ResizeUnit changes one placed token by one category, without changing its library entry.
func (s *AppState) ResizeUnit(id, direction string) (UnitData, bool, error) {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	var step int
	switch direction {
	case "up":
		step = 1
	case "down":
		step = -1
	default:
		return UnitData{}, false, ErrInvalidSizeDirection
	}
	unit, exists := s.units[id]
	if !exists {
		return UnitData{}, false, ErrUnitNotFound
	}
	return s.getChangedSize(unit, step)
}

// getChangedSize requires the caller to hold s.mutex.
func (s *AppState) getChangedSize(unit UnitData, step int) (UnitData, bool, error) {
	sizes := UnitSizes()
	index, err := unit.Size.getIndex(sizes)
	if err != nil {
		return UnitData{}, false, err
	}
	next := index + step
	if next < 0 || next >= len(sizes) {
		return unit, false, nil
	}
	unit.Size = sizes[next].Value
	s.units[unit.ID] = unit
	return unit, true, nil
}

type UnitSize string

func (s UnitSize) getIndex(sizes []UnitSizeOption) (int, error) {
	index := slices.IndexFunc(sizes, func(option UnitSizeOption) bool {
		return option.Value == s
	})
	if index == -1 {
		return -1, errors.New("unit has an invalid size")
	}
	return index, nil
}

const (
	SizeTiny       UnitSize = "tiny"
	SizeSmall      UnitSize = "small"
	SizeMedium     UnitSize = "medium"
	SizeLarge      UnitSize = "large"
	SizeHuge       UnitSize = "huge"
	SizeGargantuan UnitSize = "gargantuan"
)

type UnitSizeOption struct {
	Value     UnitSize
	Label     string
	Footprint string
	Squares   float64
}

// UnitSizes lists the D&D footprints; Squares is the width and height in 5-foot cells.
func UnitSizes() []UnitSizeOption {
	return []UnitSizeOption{
		{SizeTiny, "Tiny", "2½ × 2½ ft · ½ × ½ square", 0.5},
		{SizeSmall, "Small", "5 × 5 ft · 1 × 1 square", 1},
		{SizeMedium, "Medium", "5 × 5 ft · 1 × 1 square", 1},
		{SizeLarge, "Large", "10 × 10 ft · 2 × 2 squares", 2},
		{SizeHuge, "Huge", "15 × 15 ft · 3 × 3 squares", 3},
		{SizeGargantuan, "Gargantuan", "20 × 20 ft · 4 × 4 squares", 4},
	}
}

func (s UnitSize) Valid() bool {
	for _, option := range UnitSizes() {
		if option.Value == s {
			return true
		}
	}
	return false
}

func (s UnitSize) Label() string {
	for _, option := range UnitSizes() {
		if option.Value == s {
			return option.Label
		}
	}
	return "Medium"
}

func (s UnitSize) Squares() float64 {
	for _, option := range UnitSizes() {
		if option.Value == s {
			return option.Squares
		}
	}
	return 1
}
