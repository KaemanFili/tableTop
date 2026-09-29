package app

import (
	"strconv"
	"sync"
)

type UnitData struct {
	Left     int
	Top      int
	ID       string
	ImageID  string
	Name     string
	UnitType string
	Size     UnitSize
	ImageFit ImageFit
}

const (
	DefaultUnitImageID = "default-unit"
	NPCUnitImageID     = "goblin"
	PCUnitImageID      = "knight"
)

type AppState struct {
	mutex      sync.RWMutex
	units      map[string]UnitData
	nextUnitID uint64
}

func NewState() *AppState {
	return &AppState{
		units: make(map[string]UnitData),
	}
}

func (s *AppState) UpdateUnitPosition(id string, left, top int) bool {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	unit, exists := s.units[id]
	if !exists {
		return false
	}
	unit.Left = left
	unit.Top = top
	s.units[id] = unit
	return true
}

func (s *AppState) UnitsSnapshot() []UnitData {
	s.mutex.RLock()
	defer s.mutex.RUnlock()

	units := make([]UnitData, 0, len(s.units))
	for _, unit := range s.units {
		units = append(units, unit)
	}

	return units
}

func (s *AppState) AddRandomUnit(imageID string) UnitData {
	return s.AddUnit(UnitDefinition{ImageID: imageID})
}

func (s *AppState) AddUnit(definition UnitDefinition) UnitData {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	if definition.Size == "" {
		definition.Size = SizeMedium
	}
	if definition.ImageFit == (ImageFit{}) {
		definition.ImageFit = DefaultImageFit()
	}
	s.nextUnitID++
	id := "U" + strconv.FormatUint(s.nextUnitID, 10)
	unit := UnitData{
		Left:     100,
		Top:      150,
		ID:       id,
		ImageID:  definition.ImageID,
		Name:     definition.Name,
		UnitType: definition.UnitType,
		Size:     definition.Size,
		ImageFit: definition.ImageFit,
	}
	s.units[id] = unit

	return unit
}

func (s *AppState) UnitCount() int {
	s.mutex.RLock()
	defer s.mutex.RUnlock()

	return len(s.units)
}

func (s *AppState) UnitByID(id string) (UnitData, bool) {
	s.mutex.RLock()
	defer s.mutex.RUnlock()

	unit, exists := s.units[id]
	return unit, exists
}
