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
	ID            string         `json:"id" bson:"id"`
	Name          string         `json:"name" bson:"name"`
	Width         float64        `json:"width" bson:"width"`
	Height        float64        `json:"height" bson:"height"`
	OffsetX       float64        `json:"offsetX" bson:"offsetX"`
	OffsetY       float64        `json:"offsetY" bson:"offsetY"`
	HasSavedBoard bool           `json:"-" bson:"-"`
	Source        *ContentSource `json:"source,omitempty" bson:"source,omitempty"`
	Board         *SavedBoard    `json:"board,omitempty" bson:"board,omitempty"`
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
	mutex          sync.RWMutex
	dir            string
	maps           map[string]MapDefinition
	limits         *StorageLimits
	store          DocumentStore[MapDefinition]
	pendingDeletes map[string]bool
}

func NewMapLibrary(dir string, limits ...*StorageLimits) (*MapLibrary, error) {
	return NewMapLibraryWithStore(dir, nil, limits...)
}

func NewMapLibraryWithStore(dir string, store DocumentStore[MapDefinition], limits ...*StorageLimits) (*MapLibrary, error) {
	library := &MapLibrary{dir: dir, maps: make(map[string]MapDefinition), limits: firstStorageLimit(limits), store: store}
	if err := os.MkdirAll(filepath.Join(dir, "maps"), 0755); err != nil {
		return nil, err
	}
	var maps []MapDefinition
	initialized := false
	var err error
	if lazy, ok := store.(MapDocumentStore); ok {
		maps, initialized, err = lazy.List()
	} else if store != nil {
		maps, initialized, err = store.Load()
	}
	if err != nil {
		return nil, err
	}
	if !initialized {
		err = scanMapMetadata(filepath.Join(dir, "maps.json"), func(m MapDefinition) error {
			if store != nil {
				return nil
			}
			maps = append(maps, mapMetadata(m))
			return nil
		})
		if err != nil {
			return nil, err
		}
		if store != nil {
			visit := func(yield func(MapDefinition) error) error {
				return scanMapFile(filepath.Join(dir, "maps.json"), yield)
			}
			if lazy, ok := store.(MapDocumentStore); ok {
				err = lazy.InitializeEach(visit)
				if err == nil {
					maps, _, err = lazy.List()
				}
			} else {
				err = visit(func(m MapDefinition) error { maps = append(maps, m); return nil })
				if err == nil {
					err = store.Initialize(maps)
				}
			}
			if err != nil {
				return nil, err
			}
		}
	}
	for _, definition := range maps {
		id, err := hex.DecodeString(definition.ID)
		if err != nil || len(id) != 16 || definition.Validate() != nil {
			return nil, errors.New("invalid saved map")
		}
		if _, exists := library.maps[definition.ID]; exists {
			return nil, errors.New("duplicate saved map")
		}
		library.maps[definition.ID] = mapMetadata(definition)
	}
	return library, nil
}

func mapMetadata(m MapDefinition) MapDefinition {
	m.HasSavedBoard = m.HasSavedBoard || m.Board != nil
	m.Board = nil
	return m
}

func (l *MapLibrary) MetadataByID(id string) (MapDefinition, bool) {
	l.mutex.RLock()
	defer l.mutex.RUnlock()
	if l.pendingDeletes[id] {
		return MapDefinition{}, false
	}
	m, ok := l.maps[id]
	return m, ok
}

// MapByID is retained for callers that need a full saved document.
func (l *MapLibrary) MapByID(id string) (MapDefinition, bool) {
	m, err := l.LoadMap(id)
	if err == nil {
		return m, true
	}
	// Legacy callers can still inspect an entry when its backing file is unreadable.
	return l.MetadataByID(id)
}

func (l *MapLibrary) LoadMap(id string) (MapDefinition, error) {
	l.mutex.RLock()
	defer l.mutex.RUnlock()
	if _, ok := l.maps[id]; !ok || l.pendingDeletes[id] {
		return MapDefinition{}, os.ErrNotExist
	}
	return l.readMap(id)
}

func (l *MapLibrary) readMap(id string) (MapDefinition, error) {
	if lazy, ok := l.store.(MapDocumentStore); ok {
		return lazy.Get(id)
	}
	if l.store != nil {
		entries, _, err := l.store.Load()
		if err != nil {
			return MapDefinition{}, err
		}
		for _, m := range entries {
			if m.ID == id {
				return m, nil
			}
		}
	} else {
		var found *MapDefinition
		err := scanMapRaw(filepath.Join(l.dir, "maps.json"), func(raw json.RawMessage) error {
			var header struct {
				ID string `json:"id"`
			}
			if err := json.Unmarshal(raw, &header); err != nil {
				return err
			}
			if header.ID == id {
				var m MapDefinition
				if err := json.Unmarshal(raw, &m); err != nil {
					return err
				}
				found = &m
			}
			return nil
		})
		if err != nil {
			return MapDefinition{}, err
		}
		if found != nil {
			return *found, nil
		}
	}
	return MapDefinition{}, os.ErrNotExist
}

func (l *MapLibrary) ImagePath(id string) (string, bool) {
	if _, exists := l.MetadataByID(id); !exists {
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
	l.limits.lock()
	defer l.limits.unlock()
	if l.pendingDeletes[definition.ID] {
		return errors.New("map deletion is not confirmed; retry deleting the map before saving")
	}
	if _, exists := l.maps[definition.ID]; !exists {
		return errors.New("map is not in the library")
	}
	if err := definition.Validate(); err != nil {
		return err
	}
	if !board.WithinLimits() {
		return ErrCapacity
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
	definition.Source = l.maps[definition.ID].Source
	definition.Board = &saved
	if err := l.save(definition); err != nil {
		return err
	}
	l.maps[definition.ID] = mapMetadata(definition)
	return nil
}

// Create stores normalized PNG artwork and an atomic library update before exposing the map.
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
	l.limits.lock()
	defer l.limits.unlock()
	var id [16]byte
	if _, err := rand.Read(id[:]); err != nil {
		return MapDefinition{}, err
	}
	definition.ID = hex.EncodeToString(id[:])
	imagePath := filepath.Join(l.dir, "maps", definition.ID+".png")
	if err := writeStorageFile(imagePath, pngImage, 0644, l.limits); err != nil {
		return MapDefinition{}, err
	}
	if err := l.save(definition); err != nil {
		// A MongoDB timeout may follow a committed write; retain its artwork.
		if l.store == nil {
			os.Remove(imagePath)
		}
		return MapDefinition{}, err
	}
	l.maps[definition.ID] = mapMetadata(definition)
	return definition, nil
}

func (l *MapLibrary) save(definition MapDefinition) error {
	if l.store != nil {
		return l.store.Put(definition)
	}
	return rewriteMapFile(filepath.Join(l.dir, "maps.json"), definition.ID, &definition, l.limits)
}

// Delete removes a saved map and its artwork, preserving the entry on failure.
func (l *MapLibrary) Delete(id string) error {
	l.mutex.Lock()
	defer l.mutex.Unlock()
	l.limits.lock()
	defer l.limits.unlock()
	if _, exists := l.maps[id]; !exists {
		return nil
	}
	original, readErr := l.readMap(id)
	if readErr != nil && !errors.Is(readErr, os.ErrNotExist) {
		return readErr
	}
	var err error
	if l.store != nil {
		err = deleteDocument(l.store, id)
	} else {
		err = rewriteMapFile(filepath.Join(l.dir, "maps.json"), id, nil, nil)
	}
	if err != nil {
		if l.store != nil {
			if l.pendingDeletes == nil {
				l.pendingDeletes = make(map[string]bool)
			}
			l.pendingDeletes[id] = true
		}
		return err
	}
	if err := os.Remove(filepath.Join(l.dir, "maps", id+".png")); err != nil && !errors.Is(err, os.ErrNotExist) {
		if readErr != nil {
			if l.pendingDeletes == nil {
				l.pendingDeletes = make(map[string]bool)
			}
			l.pendingDeletes[id] = true
			return err
		}
		// Restore the previous library even if limits were lowered below its
		// existing size. This rollback does not grow usage beyond the original.
		if l.store != nil {
			rollbackErr := l.store.Put(original)
			if rollbackErr != nil {
				if l.pendingDeletes == nil {
					l.pendingDeletes = make(map[string]bool)
				}
				l.pendingDeletes[id] = true
			} else {
				delete(l.pendingDeletes, id)
			}
			return errors.Join(err, rollbackErr)
		}
		return errors.Join(err, rewriteMapFile(filepath.Join(l.dir, "maps.json"), id, &original, nil))
	}
	delete(l.maps, id)
	delete(l.pendingDeletes, id)
	return nil
}
