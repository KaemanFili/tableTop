package app

import (
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
	mutex sync.RWMutex
	dir   string
	units map[string]UnitDefinition
}

func NewUnitLibrary(dir string) (*UnitLibrary, error) {
	library := &UnitLibrary{dir: dir, units: map[string]UnitDefinition{
		"knight": {ID: "knight", Name: "Knight", UnitType: "pc", ImageID: PCUnitImageID, Size: SizeMedium, ImageFit: DefaultImageFit()},
		"goblin": {ID: "goblin", Name: "Goblin", UnitType: "npc", ImageID: NPCUnitImageID, Size: SizeSmall, ImageFit: DefaultImageFit()},
	}}
	if err := os.MkdirAll(filepath.Join(dir, "images"), 0755); err != nil {
		return nil, err
	}
	data, err := os.ReadFile(filepath.Join(dir, "units.json"))
	if errors.Is(err, os.ErrNotExist) {
		return library, nil
	}
	if err != nil {
		return nil, err
	}
	var saved []UnitDefinition
	if err := json.Unmarshal(data, &saved); err != nil {
		return nil, fmt.Errorf("read unit library: %w", err)
	}
	for _, unit := range saved {
		// Entries created before sizes were introduced occupied one grid square.
		if unit.Size == "" {
			unit.Size = SizeMedium
		}
		if unit.ImageFit == (ImageFit{}) {
			unit.ImageFit = DefaultImageFit()
		}
		id, err := hex.DecodeString(unit.ID)
		if err != nil || len(id) != 16 || ValidateUnitDefinition(unit.Name, unit.UnitType, unit.Size) != nil || unit.ImageFit.Validate() != nil {
			return nil, fmt.Errorf("invalid saved unit %q", unit.ID)
		}
		if unit.ImageID != unit.ID && unit.ImageID != PCUnitImageID && unit.ImageID != NPCUnitImageID {
			return nil, fmt.Errorf("invalid image for unit %q", unit.ID)
		}
		if _, exists := library.units[unit.ID]; exists {
			return nil, fmt.Errorf("duplicate saved unit %q", unit.ID)
		}
		library.units[unit.ID] = unit
	}
	return library, nil
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

func (l *UnitLibrary) Snapshot() []UnitDefinition {
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
	unit, exists := l.units[id]
	if !exists || unit.ImageID != id || id == "knight" || id == "goblin" {
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
		if err := os.WriteFile(imagePath, pngImage, 0644); err != nil {
			return UnitDefinition{}, err
		}
	}
	saved := []UnitDefinition{unit}
	for _, existing := range l.units {
		if existing.ID != "knight" && existing.ID != "goblin" {
			saved = append(saved, existing)
		}
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
	file, err := os.CreateTemp(l.dir, ".units-*.json")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	defer file.Close()
	if err := json.NewEncoder(file).Encode(units); err != nil {
		return err
	}
	if err := file.Sync(); err != nil {
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	return os.Rename(file.Name(), filepath.Join(l.dir, "units.json"))
}
