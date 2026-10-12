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
	ID       string   `json:"id"`
	Name     string   `json:"name"`
	UnitType string   `json:"unitType"`
	ImageID  string   `json:"imageID"`
	Size     UnitSize `json:"size"`
	ImageFit ImageFit `json:"imageFit"`
}

type UnitLibrary struct {
	mutex  sync.RWMutex
	dir    string
	units  map[string]UnitDefinition
	limits *StorageLimits
}

const unitCatalogVersion = 1

type unitCatalog struct {
	Version int              `json:"version"`
	Units   []UnitDefinition `json:"units"`
}

func NewUnitLibrary(dir string, limits ...*StorageLimits) (*UnitLibrary, error) {
	if err := os.MkdirAll(filepath.Join(dir, "images"), 0755); err != nil {
		return nil, err
	}
	data, err := os.ReadFile(filepath.Join(dir, "units.json"))
	var saved []UnitDefinition
	switch {
	case errors.Is(err, os.ErrNotExist):
		saved = starterUnits()
	case err != nil:
		return nil, err
	default:
		saved, err = decodeUnitCatalog(data)
		if err != nil {
			return nil, fmt.Errorf("read unit library: %w", err)
		}
	}

	library := &UnitLibrary{dir: dir, units: make(map[string]UnitDefinition, len(saved)), limits: firstStorageLimit(limits)}
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
	return library, nil
}

func starterUnits() []UnitDefinition {
	return []UnitDefinition{
		{ID: "knight", Name: "Knight", UnitType: "pc", ImageID: PCUnitImageID, Size: SizeMedium, ImageFit: DefaultImageFit()},
		{ID: "goblin", Name: "Goblin", UnitType: "npc", ImageID: NPCUnitImageID, Size: SizeSmall, ImageFit: DefaultImageFit()},
	}
}

func decodeUnitCatalog(data []byte) ([]UnitDefinition, error) {
	data = bytes.TrimSpace(data)
	if len(data) == 0 {
		return nil, errors.New("empty unit catalog")
	}
	if data[0] == '[' {
		// Legacy catalogs contain custom entries only; supply their starter units.
		var units []UnitDefinition
		if err := json.Unmarshal(data, &units); err != nil {
			return nil, err
		}
		return append(starterUnits(), units...), nil
	}

	var catalog unitCatalog
	if err := json.Unmarshal(data, &catalog); err != nil {
		return nil, err
	}
	if catalog.Version != unitCatalogVersion {
		return nil, fmt.Errorf("unsupported unit catalog version %d", catalog.Version)
	}
	// Versioned catalogs contain the entire library, including starter deletions.
	return catalog.Units, nil
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

// Create commits the image and catalog before making the new unit visible.
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
	saved := []UnitDefinition{unit}
	for _, existing := range l.units {
		saved = append(saved, existing)
	}
	if err := l.save(saved); err != nil {
		if imagePath != "" {
			os.Remove(imagePath)
		}
		return UnitDefinition{}, err
	}
	l.units[unit.ID] = unit
	return unit, nil
}

func (l *UnitLibrary) save(units []UnitDefinition) error {
	return writeJSONFile(filepath.Join(l.dir, "units.json"), unitCatalog{Version: unitCatalogVersion, Units: units}, l.limits)
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
	if err := l.save(units); err != nil {
		return err
	}
	delete(l.units, id)
	return nil
}
