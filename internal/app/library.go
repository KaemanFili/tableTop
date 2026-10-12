package app

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
)

// UnitDefinition is a reusable unit in the library, separate from a placed token.
type UnitDefinition struct {
	Source   *ContentSource `json:"source,omitempty" bson:"source,omitempty"`
	ID       string         `json:"id" bson:"id"`
	Name     string         `json:"name" bson:"name"`
	UnitType string         `json:"unitType" bson:"unitType"`
	ImageID  string         `json:"imageID" bson:"imageID"`
	Size     UnitSize       `json:"size" bson:"size"`
	ImageFit ImageFit       `json:"imageFit" bson:"imageFit"`
}

type UnitLibrary struct {
	mutex          sync.RWMutex
	dir            string
	units          map[string]UnitDefinition
	limits         *StorageLimits
	store          DocumentStore[UnitDefinition]
	pendingDeletes map[string]bool
}

const unitDocumentVersion = 1

type unitDocument struct {
	Version int              `json:"version" bson:"version"`
	Units   []UnitDefinition `json:"units" bson:"units"`
}

func NewUnitLibrary(dir string, limits ...*StorageLimits) (*UnitLibrary, error) {
	return NewUnitLibraryWithStore(dir, nil, limits...)
}

func NewUnitLibraryWithStore(dir string, store DocumentStore[UnitDefinition], limits ...*StorageLimits) (*UnitLibrary, error) {
	if err := os.MkdirAll(filepath.Join(dir, "images"), 0755); err != nil {
		return nil, err
	}
	var saved []UnitDefinition
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
	library := &UnitLibrary{dir: dir, units: make(map[string]UnitDefinition, len(saved)), limits: firstStorageLimit(limits), store: store}

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

func starterUnits() []UnitDefinition {
	return []UnitDefinition{
		{ID: "knight", Name: "Knight", UnitType: "pc", ImageID: PCUnitImageID, Size: SizeMedium, ImageFit: DefaultImageFit()},
		{ID: "goblin", Name: "Goblin", UnitType: "npc", ImageID: NPCUnitImageID, Size: SizeSmall, ImageFit: DefaultImageFit()},
	}
}

func decodeUnitDocument(data []byte) ([]UnitDefinition, error) {
	data = bytes.TrimSpace(data)
	if len(data) == 0 {
		return nil, errors.New("empty unit library")
	}
	if data[0] == '[' {
		// Legacy libraries contain custom entries only; supply their starter units.
		var units []UnitDefinition
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

func normalizeSavedUnit(unit UnitDefinition) (UnitDefinition, error) {
	// Entries created before sizes and image framing occupied one grid square.
	if unit.Size == "" {
		unit.Size = SizeMedium
	}
	if unit.ImageFit == (ImageFit{}) {
		unit.ImageFit = DefaultImageFit()
	}
	if unit.ID != "knight" && unit.ID != "goblin" {
		id, err := hex.DecodeString(unit.ID)
		if err != nil || len(id) != 16 {
			return UnitDefinition{}, errors.New("invalid unit ID")
		}
	}
	if err := ValidateUnitDefinition(unit.Name, unit.UnitType, unit.Size); err != nil {
		return UnitDefinition{}, err
	}
	if err := unit.ImageFit.Validate(); err != nil {
		return UnitDefinition{}, err
	}
	if unit.ImageID != unit.ID && unit.ImageID != PCUnitImageID && unit.ImageID != NPCUnitImageID {
		return UnitDefinition{}, errors.New("invalid image ID")
	}
	return unit, nil
}

func ValidateUnitDefinition(name, unitType string, size UnitSize) error {
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

func (l *UnitLibrary) Units() []UnitDefinition {
	l.mutex.RLock()
	defer l.mutex.RUnlock()
	units := make([]UnitDefinition, 0, len(l.units))
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

func (l *UnitLibrary) UnitByID(id string) (UnitDefinition, bool) {
	l.mutex.RLock()
	defer l.mutex.RUnlock()
	if l.pendingDeletes[id] {
		return UnitDefinition{}, false
	}
	unit, exists := l.units[id]
	return unit, exists
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
func (l *UnitLibrary) Create(name, unitType string, size UnitSize, fit ImageFit, pngImage []byte) (UnitDefinition, error) {
	name = strings.TrimSpace(name)
	if err := ValidateUnitDefinition(name, unitType, size); err != nil {
		return UnitDefinition{}, err
	}
	if err := fit.Validate(); err != nil {
		return UnitDefinition{}, err
	}
	l.mutex.Lock()
	defer l.mutex.Unlock()
	l.limits.lock()
	defer l.limits.unlock()
	var randomID [16]byte
	if _, err := rand.Read(randomID[:]); err != nil {
		return UnitDefinition{}, err
	}
	unit := UnitDefinition{ID: hex.EncodeToString(randomID[:]), Name: name, UnitType: unitType, ImageID: NPCUnitImageID, Size: size, ImageFit: fit}
	if unitType == "pc" {
		unit.ImageID = PCUnitImageID
	}
	imagePath := ""
	if len(pngImage) > 0 {
		unit.ImageID = unit.ID
		imagePath = filepath.Join(l.dir, "images", unit.ID+".png")
		if err := writeStorageFile(imagePath, pngImage, 0644, l.limits); err != nil {
			return UnitDefinition{}, err
		}
	}
	var err error
	if l.store != nil {
		err = l.store.Put(unit)
	} else {
		saved := []UnitDefinition{unit}
		for _, existing := range l.units {
			saved = append(saved, existing)
		}
		err = l.save(saved)
	}
	if err != nil {
		// A MongoDB timeout may follow a committed write; retain its artwork.
		if imagePath != "" && l.store == nil {
			os.Remove(imagePath)
		}
		return UnitDefinition{}, err
	}
	l.units[unit.ID] = unit
	return unit, nil
}

func (l *UnitLibrary) save(units []UnitDefinition) error {
	return writeJSONFile(filepath.Join(l.dir, "units.json"), unitDocument{Version: unitDocumentVersion, Units: units}, l.limits)
}

// Delete removes a reusable definition; placed copies retain their artwork.
func (l *UnitLibrary) Delete(id string) error {
	l.mutex.Lock()
	defer l.mutex.Unlock()
	l.limits.lock()
	defer l.limits.unlock()
	if _, exists := l.units[id]; !exists {
		return nil
	}
	units := make([]UnitDefinition, 0, len(l.units)-1)
	for _, unit := range l.units {
		if unit.ID != id {
			units = append(units, unit)
		}
	}
	var err error
	if l.store != nil {
		err = deleteDocument(l.store, id)
	} else {
		err = l.save(units)
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
	delete(l.units, id)
	delete(l.pendingDeletes, id)
	return nil
}
