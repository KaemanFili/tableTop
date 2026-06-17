package app

import (
	"math/rand"
	"strconv"
	"sync"
)

type UnitData struct {
	Left int
	Top  int
	ID   string
}

type AppState struct {
	mutex sync.RWMutex
	units map[string]UnitData
}

func NewState() *AppState {
	return &AppState{
		units: make(map[string]UnitData),
	}
}

func (s *AppState) SetUnit(unit UnitData) {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	s.units[unit.ID] = unit
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

func (s *AppState) AddRandomUnit() UnitData {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	for {
		id := "U" + strconv.Itoa(rand.Intn(100))
		if _, exists := s.units[id]; exists {
			continue
		}

		unit := UnitData{
			Left: 100,
			Top:  150,
			ID:   id,
		}
		s.units[id] = unit

		return unit
	}
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
