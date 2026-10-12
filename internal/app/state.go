package app

import (
	"strconv"
	"sync"
)

type UnitData struct {
	Left     int        `bson:"left"`
	Top      int        `bson:"top"`
	ID       string     `bson:"id"`
	ImageID  string     `bson:"imageID"`
	Name     string     `bson:"name"`
	UnitType string     `bson:"unitType"`
	Status   UnitStatus `bson:"status"`
	Size     UnitSize   `bson:"size"`
	ImageFit ImageFit   `bson:"imageFit"`
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
	statuses   []UnitStatusOption
}

func NewState() *AppState {
	return &AppState{
		units:    make(map[string]UnitData),
		statuses: UnitStatuses(),
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

func (s *AppState) Units() []UnitData {
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

func (s *AppState) RemoveUnit(id string) bool {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	if _, exists := s.units[id]; !exists {
		return false
	}
	delete(s.units, id)
	return true
}

// ClearUnits keeps the ID counter so delayed client updates cannot target new tokens.
func (s *AppState) ClearUnits() []string {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	ids := make([]string, 0, len(s.units))
	for id := range s.units {
		ids = append(ids, id)
	}
	clear(s.units)
	return ids
}

func (s *AppState) UnitByID(id string) (UnitData, bool) {
	s.mutex.RLock()
	defer s.mutex.RUnlock()

	unit, exists := s.units[id]
	return unit, exists
}

// RestoreUnits assigns fresh IDs so delayed updates cannot change restored tokens.
func (s *AppState) RestoreUnits(units []UnitData) {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	clear(s.units)
	for _, unit := range units {
		s.nextUnitID++
		unit.ID = "U" + strconv.FormatUint(s.nextUnitID, 10)
		s.units[unit.ID] = unit
	}
}
