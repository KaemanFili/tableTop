package persistence

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"tableTop/main/internal/app/model"
)

type MapLibrary struct {
	mutex          sync.RWMutex
	dir            string
	maps           map[string]model.MapDefinition
	limits         *StorageLimits
	store          DocumentStore[model.MapDefinition]
	pendingDeletes map[string]bool
	defaults       PluginDefaults
}

func NewMapLibrary(dir string, limits ...*StorageLimits) (*MapLibrary, error) {
	return NewMapLibraryWithStore(dir, nil, limits...)
}

func NewMapLibraryWithStore(dir string, store DocumentStore[model.MapDefinition], limits ...*StorageLimits) (*MapLibrary, error) {
	library := &MapLibrary{dir: dir, maps: make(map[string]model.MapDefinition), limits: firstStorageLimit(limits), store: store}
	if err := os.MkdirAll(filepath.Join(dir, "maps"), 0755); err != nil {
		return nil, err
	}
	var maps []model.MapDefinition
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
		err = scanMapMetadata(filepath.Join(dir, "maps.json"), func(m model.MapDefinition) error {
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
			visit := func(yield func(model.MapDefinition) error) error {
				return scanMapFile(filepath.Join(dir, "maps.json"), yield)
			}
			if lazy, ok := store.(MapDocumentStore); ok {
				err = lazy.InitializeEach(visit)
				if err == nil {
					maps, _, err = lazy.List()
				}
			} else {
				err = visit(func(m model.MapDefinition) error { maps = append(maps, m); return nil })
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

func mapMetadata(m model.MapDefinition) model.MapDefinition {
	m.HasSavedBoard = m.HasSavedBoard || m.Board != nil
	m.Board = nil
	return m
}

func (l *MapLibrary) MetadataByID(id string) (model.MapDefinition, bool) {
	l.mutex.RLock()
	defer l.mutex.RUnlock()
	if l.pendingDeletes[id] {
		return model.MapDefinition{}, false
	}
	if definition, owned := l.maps[id]; owned {
		return definition, true
	}
	return l.defaultMapByID(id)
}

// MapByID is retained for callers that need a full saved document.
func (l *MapLibrary) MapByID(id string) (model.MapDefinition, bool) {
	m, err := l.LoadMap(id)
	if err == nil {
		return m, true
	}
	// Legacy callers can still inspect an entry when its backing file is unreadable.
	return l.MetadataByID(id)
}

func (l *MapLibrary) LoadMap(id string) (model.MapDefinition, error) {
	l.mutex.RLock()
	defer l.mutex.RUnlock()
	if l.pendingDeletes[id] {
		return model.MapDefinition{}, os.ErrNotExist
	}
	if _, ok := l.maps[id]; !ok && l.defaults != nil {
		return l.defaults.LoadMap(id)
	}
	return l.readMap(id)
}

func (l *MapLibrary) readMap(id string) (model.MapDefinition, error) {
	if lazy, ok := l.store.(MapDocumentStore); ok {
		return lazy.Get(id)
	}
	if l.store != nil {
		entries, _, err := l.store.Load()
		if err != nil {
			return model.MapDefinition{}, err
		}
		for _, m := range entries {
			if m.ID == id {
				return m, nil
			}
		}
	} else {
		var found *model.MapDefinition
		err := scanMapRaw(filepath.Join(l.dir, "maps.json"), func(raw json.RawMessage) error {
			var header struct {
				ID string `json:"id"`
			}
			if err := json.Unmarshal(raw, &header); err != nil {
				return err
			}
			if header.ID == id {
				var m model.MapDefinition
				if err := json.Unmarshal(raw, &m); err != nil {
					return err
				}
				found = &m
			}
			return nil
		})
		if err != nil {
			return model.MapDefinition{}, err
		}
		if found != nil {
			return *found, nil
		}
	}
	return model.MapDefinition{}, os.ErrNotExist
}

func (l *MapLibrary) ImagePath(id string) (string, bool) {
	if _, exists := l.MetadataByID(id); !exists {
		return "", false
	}
	return filepath.Join(l.dir, "maps", id+".png"), true
}

func (l *MapLibrary) Maps() []model.MapDefinition {
	l.mutex.RLock()
	defer l.mutex.RUnlock()
	maps := make([]model.MapDefinition, 0, len(l.maps))
	if l.defaults != nil {
		maps = append(maps, l.defaults.Maps()...)
	}
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
func (l *MapLibrary) SaveBoard(definition model.MapDefinition, board model.SavedBoard) error {
	l.mutex.Lock()
	defer l.mutex.Unlock()
	l.limits.lock()
	defer l.limits.unlock()
	if l.pendingDeletes[definition.ID] {
		return errors.New("map deletion is not confirmed; retry deleting the map before saving")
	}
	if _, exists := l.maps[definition.ID]; !exists {
		return ErrReadOnly
	}
	if err := definition.Validate(); err != nil {
		return err
	}
	if !board.WithinLimits() {
		return model.ErrCapacity
	}
	definition.Artwork = l.maps[definition.ID].Artwork
	if _, _, err := l.mapArtwork(l.maps[definition.ID]); err != nil {
		return err
	}
	// JSON copying separates saved data from mutable live drawing slices.
	data, err := json.Marshal(board)
	if err != nil {
		return err
	}
	var saved model.SavedBoard
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
func (l *MapLibrary) Create(definition model.MapDefinition, pngImage []byte) (model.MapDefinition, error) {
	definition.Name = strings.TrimSpace(definition.Name)
	if err := definition.Validate(); err != nil {
		return model.MapDefinition{}, err
	}
	if len(pngImage) == 0 && definition.Artwork == nil {
		return model.MapDefinition{}, errors.New("Choose a map image.")
	}
	if len(pngImage) == 0 {
		if _, err := pluginImage(l.defaults, definition.Artwork); err != nil {
			return model.MapDefinition{}, err
		}
	} else {
		definition.Artwork = nil
	}
	definition.ReadOnly = false
	l.mutex.Lock()
	defer l.mutex.Unlock()
	l.limits.lock()
	defer l.limits.unlock()
	var id [16]byte
	if _, err := rand.Read(id[:]); err != nil {
		return model.MapDefinition{}, err
	}
	definition.ID = hex.EncodeToString(id[:])
	imagePath := filepath.Join(l.dir, "maps", definition.ID+".png")
	if len(pngImage) > 0 {
		if err := writeStorageFile(imagePath, pngImage, 0644, l.limits); err != nil {
			return model.MapDefinition{}, err
		}
	}
	if err := l.save(definition); err != nil {
		// A MongoDB timeout may follow a committed write; retain its artwork.
		if l.store == nil {
			os.Remove(imagePath)
		}
		return model.MapDefinition{}, err
	}
	l.maps[definition.ID] = mapMetadata(definition)
	return definition, nil
}

func (l *MapLibrary) save(definition model.MapDefinition) error {
	if l.store != nil {
		return l.store.Put(definition)
	}
	return rewriteMapFile(filepath.Join(l.dir, "maps.json"), definition.ID, &definition, l.limits)
}

// Delete removes an owned map and artwork, restoring the full document on failure.
func (l *MapLibrary) Delete(id string) error {
	l.mutex.Lock()
	defer l.mutex.Unlock()
	if _, owned := l.maps[id]; !owned {
		return l.defaultDeletionError(id)
	}
	l.limits.lock()
	defer l.limits.unlock()
	original, readErr := l.readMap(id)
	if readErr != nil && !errors.Is(readErr, os.ErrNotExist) {
		return readErr
	}
	if err := l.persistMapDeletion(id); err != nil {
		l.markUnconfirmedDelete(id)
		return err
	}
	if err := l.removeMapArtwork(id); err != nil {
		return l.rollbackMapDeletion(id, original, readErr, err)
	}
	delete(l.maps, id)
	delete(l.pendingDeletes, id)
	return nil
}
func (l *MapLibrary) defaultMapByID(id string) (model.MapDefinition, bool) {
	if l.defaults == nil {
		return model.MapDefinition{}, false
	}
	return l.defaults.MetadataByID(id)
}
func (l *MapLibrary) defaultDeletionError(id string) error {
	if _, exists := l.defaultMapByID(id); exists {
		return ErrReadOnly
	}
	return nil
}
func (l *MapLibrary) persistMapDeletion(id string) error {
	if l.store != nil {
		return deleteDocument(l.store, id)
	}
	// Shrinking the library must remain possible when the quota is full.
	return rewriteMapFile(filepath.Join(l.dir, "maps.json"), id, nil, nil)
}
func (l *MapLibrary) removeMapArtwork(id string) error {
	err := os.Remove(filepath.Join(l.dir, "maps", id+".png"))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}
func (l *MapLibrary) rollbackMapDeletion(id string, original model.MapDefinition, readErr, removeErr error) error {
	if readErr != nil {
		l.markDeletePending(id)
		return removeErr
	}
	return errors.Join(removeErr, l.restoreMap(original))
}
func (l *MapLibrary) restoreMap(original model.MapDefinition) error {
	// Rollback restores existing data even after the host lowers storage quotas.
	if l.store == nil {
		return rewriteMapFile(filepath.Join(l.dir, "maps.json"), original.ID, &original, nil)
	}
	if err := l.store.Put(original); err != nil {
		l.markDeletePending(original.ID)
		return err
	}
	delete(l.pendingDeletes, original.ID)
	return nil
}
func (l *MapLibrary) markUnconfirmedDelete(id string) {
	if l.store == nil {
		return
	}
	l.markDeletePending(id)
}
func (l *MapLibrary) markDeletePending(id string) {
	if l.pendingDeletes == nil {
		l.pendingDeletes = make(map[string]bool)
	}
	l.pendingDeletes[id] = true
}
