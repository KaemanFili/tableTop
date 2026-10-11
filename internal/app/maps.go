package app

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"unicode/utf8"
)

type MapDefinition struct {
	ID      string      `json:"id"`
	Name    string      `json:"name"`
	Width   float64     `json:"width"`
	Height  float64     `json:"height"`
	OffsetX float64     `json:"offsetX"`
	OffsetY float64     `json:"offsetY"`
	Board   *SavedBoard `json:"board,omitempty"`
}

func (m MapDefinition) Validate() error {
	if strings.TrimSpace(m.Name) == "" || !utf8.ValidString(m.Name) || utf8.RuneCountInString(m.Name) > 80 {
		return errors.New("Enter a map name between 1 and 80 characters.")
	}
	for _, value := range []float64{m.Width, m.Height} {
		if math.IsNaN(value) || math.IsInf(value, 0) || value < 0.1 || value > 64 {
			return errors.New("Map width and height must be between 0.1 and 64 grid squares.")
		}
	}
	for _, value := range []float64{m.OffsetX, m.OffsetY} {
		if math.IsNaN(value) || math.IsInf(value, 0) || value < -64 || value > 64 {
			return errors.New("Map position must be between -64 and 64 grid squares.")
		}
	}
	return nil
}

type MapLibrary struct {
	mutex sync.RWMutex
	dir   string
	maps  map[string]MapDefinition
}

func NewMapLibrary(dir string) (*MapLibrary, error) {
	library := &MapLibrary{dir: dir, maps: make(map[string]MapDefinition)}
	if err := os.MkdirAll(filepath.Join(dir, "maps"), 0755); err != nil {
		return nil, err
	}
	data, err := os.ReadFile(filepath.Join(dir, "maps.json"))
	if errors.Is(err, os.ErrNotExist) {
		return library, nil
	}
	if err != nil {
		return nil, err
	}
	var maps []MapDefinition
	if err := json.Unmarshal(data, &maps); err != nil {
		return nil, err
	}
	for _, definition := range maps {
		id, err := hex.DecodeString(definition.ID)
		if err != nil || len(id) != 16 || definition.Validate() != nil {
			return nil, errors.New("invalid saved map")
		}
		if _, exists := library.maps[definition.ID]; exists {
			return nil, errors.New("duplicate saved map")
		}
		library.maps[definition.ID] = definition
	}
	return library, nil
}

func (l *MapLibrary) MapByID(id string) (MapDefinition, bool) {
	l.mutex.RLock()
	defer l.mutex.RUnlock()
	definition, exists := l.maps[id]
	return definition, exists
}

func (l *MapLibrary) ImagePath(id string) (string, bool) {
	if _, exists := l.MapByID(id); !exists {
		return "", false
	}
	return filepath.Join(l.dir, "maps", id+".png"), true
}

func (l *MapLibrary) Maps() []MapDefinition {
	l.mutex.RLock()
	defer l.mutex.RUnlock()
	maps := make([]MapDefinition, 0, len(l.maps))
	for _, definition := range l.maps {
		maps = append(maps, definition)
	}
	sort.Slice(maps, func(i, j int) bool {
		if maps[i].Name == maps[j].Name {
			return maps[i].ID < maps[j].ID
		}
		return maps[i].Name < maps[j].Name
	})
	return maps
}

// SaveBoard replaces the current map's saved content while retaining its artwork.
func (l *MapLibrary) SaveBoard(definition MapDefinition, board SavedBoard) error {
	l.mutex.Lock()
	defer l.mutex.Unlock()
	if _, exists := l.maps[definition.ID]; !exists {
		return errors.New("map is not in the library")
	}
	if err := definition.Validate(); err != nil {
		return err
	}
	if _, err := os.Stat(filepath.Join(l.dir, "maps", definition.ID+".png")); err != nil {
		return err
	}
	// JSON copying separates saved data from mutable live drawing slices.
	data, err := json.Marshal(board)
	if err != nil {
		return err
	}
	var saved SavedBoard
	if err := json.Unmarshal(data, &saved); err != nil {
		return err
	}
	definition.Board = &saved
	if err := l.save(definition); err != nil {
		return err
	}
	l.maps[definition.ID] = definition
	return nil
}

// Create stores normalized PNG artwork and an atomic catalog update before exposing the map.
func (l *MapLibrary) Create(definition MapDefinition, pngImage []byte) (MapDefinition, error) {
	definition.Name = strings.TrimSpace(definition.Name)
	if err := definition.Validate(); err != nil {
		return MapDefinition{}, err
	}
	if len(pngImage) == 0 {
		return MapDefinition{}, errors.New("Choose a map image.")
	}
	l.mutex.Lock()
	defer l.mutex.Unlock()
	var id [16]byte
	if _, err := rand.Read(id[:]); err != nil {
		return MapDefinition{}, err
	}
	definition.ID = hex.EncodeToString(id[:])
	imagePath := filepath.Join(l.dir, "maps", definition.ID+".png")
	if err := os.WriteFile(imagePath, pngImage, 0644); err != nil {
		return MapDefinition{}, err
	}
	if err := l.save(definition); err != nil {
		os.Remove(imagePath)
		return MapDefinition{}, err
	}
	l.maps[definition.ID] = definition
	return definition, nil
}

func (l *MapLibrary) save(definition MapDefinition) error {
	maps := []MapDefinition{definition}
	for _, existing := range l.maps {
		if existing.ID != definition.ID {
			maps = append(maps, existing)
		}
	}
	return l.saveMaps(maps)
}

func (l *MapLibrary) saveMaps(maps []MapDefinition) error {
	return writeJSONFile(filepath.Join(l.dir, "maps.json"), maps)
}

// Delete removes a saved map and its artwork, preserving the entry on failure.
func (l *MapLibrary) Delete(id string) error {
	l.mutex.Lock()
	defer l.mutex.Unlock()
	if _, exists := l.maps[id]; !exists {
		return nil
	}
	remaining := make([]MapDefinition, 0, len(l.maps)-1)
	original := make([]MapDefinition, 0, len(l.maps))
	for _, definition := range l.maps {
		original = append(original, definition)
		if definition.ID != id {
			remaining = append(remaining, definition)
		}
	}
	if err := l.saveMaps(remaining); err != nil {
		return err
	}
	if err := os.Remove(filepath.Join(l.dir, "maps", id+".png")); err != nil && !errors.Is(err, os.ErrNotExist) {
		return errors.Join(err, l.saveMaps(original))
	}
	delete(l.maps, id)
	return nil
}
