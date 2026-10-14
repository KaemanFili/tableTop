package persistence

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"unicode/utf8"

	"tableTop/main/internal/app/model"
)

type UnitLibrary struct {
	mutex          sync.RWMutex
	dir            string
	units          map[string]model.UnitDefinition
	limits         *StorageLimits
	store          DocumentStore[model.UnitDefinition]
	pendingDeletes map[string]bool
	defaults       PluginDefaults
}

const unitDocumentVersion = 1

type unitDocument struct {
	Version int                    `json:"version" bson:"version"`
	Units   []model.UnitDefinition `json:"units" bson:"units"`
}

func NewUnitLibrary(dir string, limits ...*StorageLimits) (*UnitLibrary, error) {
	return NewUnitLibraryWithStore(dir, nil, limits...)
}

func NewUnitLibraryWithStore(dir string, store DocumentStore[model.UnitDefinition], limits ...*StorageLimits) (*UnitLibrary, error) {
	if err := os.MkdirAll(filepath.Join(dir, "images"), 0755); err != nil {
		return nil, err
	}
	var saved []model.UnitDefinition
	initialized := false
	var err error
	if store != nil {
		saved, initialized, err = store.Load()
		if err != nil {
			return nil, err
		}
	}
	if !initialized {
		data, readErr := os.ReadFile(filepath.Join(dir, "units.json"))
		switch {
		case errors.Is(readErr, os.ErrNotExist):
			saved = starterUnits()
		case readErr != nil:
			return nil, readErr
		default:
			saved, err = decodeUnitDocument(data)
			if err != nil {
				return nil, fmt.Errorf("read unit library: %w", err)
			}
		}
	}
	library := &UnitLibrary{dir: dir, units: make(map[string]model.UnitDefinition, len(saved)), limits: firstStorageLimit(limits), store: store}

	for _, entry := range saved {
		unit, err := normalizeSavedUnit(entry)
		if err != nil {
			return nil, fmt.Errorf("invalid saved unit %q: %w", entry.ID, err)
		}
		if _, exists := library.units[unit.ID]; exists {
			return nil, fmt.Errorf("duplicate saved unit %q", unit.ID)
		}
		library.units[unit.ID] = unit
	}
	if store != nil && !initialized {
		if err := store.Initialize(library.Units()); err != nil {
			return nil, err
		}
	}
	return library, nil
}

func starterUnits() []model.UnitDefinition {
	return []model.UnitDefinition{
		{ID: "knight", Name: "Knight", UnitType: "pc", ImageID: model.PCUnitImageID, Size: model.SizeMedium, ImageFit: model.DefaultImageFit()},
		{ID: "goblin", Name: "Goblin", UnitType: "npc", ImageID: model.NPCUnitImageID, Size: model.SizeSmall, ImageFit: model.DefaultImageFit()},
	}
}

func decodeUnitDocument(data []byte) ([]model.UnitDefinition, error) {
	data = bytes.TrimSpace(data)
	if len(data) == 0 {
		return nil, errors.New("empty unit library")
	}
	if data[0] == '[' {
		// Legacy libraries contain custom entries only; supply their starter units.
		var units []model.UnitDefinition
		if err := json.Unmarshal(data, &units); err != nil {
			return nil, err
		}
		return append(starterUnits(), units...), nil
	}

	var document unitDocument
	if err := json.Unmarshal(data, &document); err != nil {
		return nil, err
	}
	if document.Version != unitDocumentVersion {
		return nil, fmt.Errorf("unsupported unit library version %d", document.Version)
	}
	// Versioned libraries contain the entire library, including starter deletions.
	return document.Units, nil
}

func normalizeSavedUnit(unit model.UnitDefinition) (model.UnitDefinition, error) {
	// Entries created before sizes and image framing occupied one grid square.
	if unit.Size == "" {
		unit.Size = model.SizeMedium
	}
	if unit.ImageFit == (model.ImageFit{}) {
		unit.ImageFit = model.DefaultImageFit()
	}
	if unit.ID != "knight" && unit.ID != "goblin" {
		id, err := hex.DecodeString(unit.ID)
		if err != nil || len(id) != 16 {
			return model.UnitDefinition{}, errors.New("invalid unit ID")
		}
	}
	if err := ValidateUnitDefinition(unit.Name, unit.UnitType, unit.Size); err != nil {
		return model.UnitDefinition{}, err
	}
	if err := unit.ImageFit.Validate(); err != nil {
		return model.UnitDefinition{}, err
	}
	if unit.Artwork == nil && unit.ImageID != unit.ID && unit.ImageID != model.PCUnitImageID && unit.ImageID != model.NPCUnitImageID {
		return model.UnitDefinition{}, errors.New("invalid image ID")
	}
	return unit, nil
}

func ValidateUnitDefinition(name, unitType string, size model.UnitSize) error {
	if strings.TrimSpace(name) == "" || !utf8.ValidString(name) || utf8.RuneCountInString(name) > 80 {
		return errors.New("Enter a name between 1 and 80 characters.")
	}
	if unitType != "pc" && unitType != "npc" {
		return errors.New("Choose PC or NPC for the unit type.")
	}
	if !size.Valid() {
		return errors.New("Choose a size from Tiny to Gargantuan.")
	}
	return nil
}

func (l *UnitLibrary) Units() []model.UnitDefinition {
	l.mutex.RLock()
	defer l.mutex.RUnlock()
	units := make([]model.UnitDefinition, 0, len(l.units))
	if l.defaults != nil {
		units = append(units, l.defaults.Units()...)
	}
	for _, unit := range l.units {
		units = append(units, unit)
	}
	sort.Slice(units, func(i, j int) bool {
		a, b := strings.ToLower(units[i].Name), strings.ToLower(units[j].Name)
		if a == b {
			return units[i].ID < units[j].ID
		}
		return a < b
	})
	return units
}

func (l *UnitLibrary) UnitByID(id string) (model.UnitDefinition, bool) {
	l.mutex.RLock()
	defer l.mutex.RUnlock()
	if l.pendingDeletes[id] {
		return model.UnitDefinition{}, false
	}
	if unit, exists := l.units[id]; exists {
		return unit, true
	}
	return l.defaultUnitByID(id)
}

func (l *UnitLibrary) ImagePath(id string) (string, bool) {
	l.mutex.RLock()
	defer l.mutex.RUnlock()
	// Artwork remains available for placed tokens and saved boards after deletion.
	decoded, err := hex.DecodeString(id)
	if err != nil || len(decoded) != 16 {
		return "", false
	}
	return filepath.Join(l.dir, "images", id+".png"), true
}

// Create commits the image and library before making the new unit visible.
// pngImage, when present, must be a validated, re-encoded PNG from the handler.
func (l *UnitLibrary) Create(name, unitType string, size model.UnitSize, fit model.ImageFit, pngImage []byte) (model.UnitDefinition, error) {
	return l.create(model.UnitDefinition{Name: strings.TrimSpace(name), UnitType: unitType, Size: size, ImageFit: fit}, pngImage)
}
func (l *UnitLibrary) CreateCopy(id, name, unitType string, size model.UnitSize, fit model.ImageFit, pngImage []byte) (model.UnitDefinition, error) {
	original, ok := l.UnitByID(id)
	if !ok {
		return model.UnitDefinition{}, os.ErrNotExist
	}
	if !original.ReadOnly {
		return model.UnitDefinition{}, errors.New("choose a plugin default to copy")
	}
	original.Name, original.UnitType, original.Size, original.ImageFit = strings.TrimSpace(name), unitType, size, fit
	return l.create(original, pngImage)
}
func (l *UnitLibrary) create(unit model.UnitDefinition, pngImage []byte) (model.UnitDefinition, error) {
	if err := l.validateNewUnit(unit); err != nil {
		return model.UnitDefinition{}, err
	}
	unit.ReadOnly = false
	l.mutex.Lock()
	defer l.mutex.Unlock()
	l.limits.lock()
	defer l.limits.unlock()
	var randomID [16]byte
	if _, err := rand.Read(randomID[:]); err != nil {
		return model.UnitDefinition{}, err
	}
	unit.ID = hex.EncodeToString(randomID[:])
	imagePath, err := l.storeUnitArtwork(&unit, pngImage)
	if err != nil {
		return model.UnitDefinition{}, err
	}
	if err := l.persistUnit(unit); err != nil {
		l.discardUncommittedArtwork(imagePath)
		return model.UnitDefinition{}, err
	}
	l.units[unit.ID] = unit
	return unit, nil
}
func (l *UnitLibrary) validateNewUnit(unit model.UnitDefinition) error {
	if err := ValidateUnitDefinition(unit.Name, unit.UnitType, unit.Size); err != nil {
		return err
	}
	if err := unit.ImageFit.Validate(); err != nil {
		return err
	}
	if unit.Artwork == nil {
		return nil
	}
	_, err := pluginImage(l.defaults, unit.Artwork)
	return err
}

// The caller holds the library and storage locks while writing artwork/documents.
func (l *UnitLibrary) storeUnitArtwork(unit *model.UnitDefinition, pngImage []byte) (string, error) {
	if len(pngImage) > 0 {
		unit.ImageID, unit.Artwork = unit.ID, nil
		path := filepath.Join(l.dir, "images", unit.ID+".png")
		return path, writeStorageFile(path, pngImage, 0644, l.limits)
	}
	if unit.Artwork != nil || unit.ImageID != "" {
		return "", nil
	}
	unit.ImageID = model.NPCUnitImageID
	if unit.UnitType == "pc" {
		unit.ImageID = model.PCUnitImageID
	}
	return "", nil
}
func (l *UnitLibrary) persistUnit(unit model.UnitDefinition) error {
	if l.store != nil {
		return l.store.Put(unit)
	}
	saved := make([]model.UnitDefinition, 0, len(l.units)+1)
	saved = append(saved, unit)
	for _, existing := range l.units {
		saved = append(saved, existing)
	}
	return l.save(saved)
}
func (l *UnitLibrary) discardUncommittedArtwork(path string) {
	// A MongoDB timeout may follow a committed write; retain that artwork.
	if l.store != nil || path == "" {
		return
	}
	os.Remove(path)
}

func (l *UnitLibrary) save(units []model.UnitDefinition) error {
	return writeJSONFile(filepath.Join(l.dir, "units.json"), unitDocument{Version: unitDocumentVersion, Units: units}, l.limits)
}

// Delete removes an owned definition; placed copies retain their artwork.
func (l *UnitLibrary) Delete(id string) error {
	l.mutex.Lock()
	defer l.mutex.Unlock()

	if _, owned := l.units[id]; !owned {
		return l.defaultDeletionError(id)
	}

	l.limits.lock()
	defer l.limits.unlock()
	if err := l.persistUnitDeletion(id); err != nil {
		l.markDeletePending(id)
		return err
	}
	delete(l.units, id)
	delete(l.pendingDeletes, id)
	return nil
}

func (l *UnitLibrary) defaultUnitByID(id string) (model.UnitDefinition, bool) {
	if l.defaults == nil {
		return model.UnitDefinition{}, false
	}
	return l.defaults.UnitByID(id)
}

func (l *UnitLibrary) defaultDeletionError(id string) error {
	if _, exists := l.defaultUnitByID(id); exists {
		return ErrReadOnly
	}
	return nil
}

// The caller holds the library and storage locks throughout persistence.
func (l *UnitLibrary) persistUnitDeletion(id string) error {
	if l.store != nil {
		return deleteDocument(l.store, id)
	}
	return l.save(l.unitsExcluding(id))
}

func (l *UnitLibrary) unitsExcluding(id string) []model.UnitDefinition {
	units := make([]model.UnitDefinition, 0, len(l.units)-1)
	for _, unit := range l.units {
		if unit.ID == id {
			continue
		}
		units = append(units, unit)
	}
	return units
}

func (l *UnitLibrary) markDeletePending(id string) {
	if l.store == nil {
		return
	}
	if l.pendingDeletes == nil {
		l.pendingDeletes = make(map[string]bool)
	}
	l.pendingDeletes[id] = true
}
