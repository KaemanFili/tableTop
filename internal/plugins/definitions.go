package plugins

import (
	"encoding/json"
	"errors"
	"path/filepath"

	"tableTop/main/internal/app"
	"tableTop/main/internal/artwork"
)

type contentWriter func(string, []byte) error

func (write contentWriter) json(path string, value any) error {
	if write == nil {
		return nil
	}
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	return write(path, append(data, '\n'))
}
func (p Pack) copyImage(source, target string, limit int64, write contentWriter) error {
	data, err := p.read(source, limit)
	if err != nil {
		return err
	}
	normalized, err := artwork.Normalize(data)
	if err != nil {
		return err
	}
	if write != nil {
		return write(target, normalized)
	}
	return nil
}
func (p Pack) readStatuses() ([]app.UnitStatusOption, error) {
	values := []app.UnitStatusOption{}
	err := p.list(p.Statuses, func(raw json.RawMessage) error {
		var value app.UnitStatusOption
		if err := json.Unmarshal(raw, &value); err != nil {
			return err
		}
		values = append(values, value)
		if len(values) > 256 {
			return errors.New("too many statuses")
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	if err := app.ValidateUnitStatuses(values); err != nil {
		return nil, err
	}
	return values, nil
}
func (p Pack) readUnits(scope string, write contentWriter) ([]app.UnitDefinition, map[string]app.UnitDefinition, error) {
	units := []app.UnitDefinition{}
	byKey := map[string]app.UnitDefinition{}
	err := p.list(p.Units, func(raw json.RawMessage) error {
		var entry unitEntry
		if err := json.Unmarshal(raw, &entry); err != nil {
			return err
		}
		if !slug.MatchString(entry.Key) || byKey[entry.Key].ID != "" {
			return errors.New("invalid or duplicate unit key")
		}
		unit, err := p.normalizeUnit(entry)
		if err != nil {
			return err
		}
		if err := app.ValidateContentDocument(scope, "units", unit.ID, unit); err != nil {
			return err
		}
		if entry.Image != "" {
			if err := p.copyImage(entry.Image, filepath.Join("images", unit.ID+".png"), 5<<20, write); err != nil {
				return err
			}
		}
		units = append(units, unit)
		byKey[entry.Key] = unit
		return nil
	})
	return units, byKey, err
}
func (p Pack) normalizeUnit(entry unitEntry) (app.UnitDefinition, error) {
	unit := entry.UnitDefinition
	unit.ID = StableID(p.ID, "unit", entry.Key)
	unit.Source = &app.ContentSource{PluginID: p.ID, Version: p.Version, Key: entry.Key}
	if unit.Size == "" {
		unit.Size = app.SizeMedium
	}
	if unit.ImageFit == (app.ImageFit{}) {
		unit.ImageFit = app.DefaultImageFit()
	}
	if err := app.ValidateUnitDefinition(unit.Name, unit.UnitType, unit.Size); err != nil {
		return unit, err
	}
	if err := unit.ImageFit.Validate(); err != nil {
		return unit, err
	}
	unit.ImageID = app.NPCUnitImageID
	if unit.UnitType == "pc" {
		unit.ImageID = app.PCUnitImageID
	}
	if entry.Image != "" {
		unit.ImageID = unit.ID
	}
	return unit, nil
}
func (p Pack) readMaps(scope string, units map[string]app.UnitDefinition, statuses []app.UnitStatusOption, write contentWriter) error {
	seen := map[string]bool{}
	return p.list(p.Maps, func(raw json.RawMessage) error {
		var entry mapEntry
		if err := json.Unmarshal(raw, &entry); err != nil {
			return err
		}
		if !slug.MatchString(entry.Key) || seen[entry.Key] {
			return errors.New("invalid or duplicate map key")
		}
		seen[entry.Key] = true
		definition, err := p.normalizeMap(entry, units, statuses)
		if err != nil {
			return err
		}
		if err := app.ValidateContentDocument(scope, "maps", definition.ID, definition); err != nil {
			return err
		}
		if err := p.copyImage(entry.Image, filepath.Join("maps", definition.ID+".png"), 10<<20, write); err != nil {
			return err
		}
		return write.json(filepath.Join(".plugin", "maps", definition.ID+".json"), definition)
	})
}
func (p Pack) normalizeMap(entry mapEntry, units map[string]app.UnitDefinition, statuses []app.UnitStatusOption) (app.MapDefinition, error) {
	definition := entry.MapDefinition
	definition.ID = StableID(p.ID, "map", entry.Key)
	definition.Source = &app.ContentSource{PluginID: p.ID, Version: p.Version, Key: entry.Key}
	if err := definition.Validate(); err != nil {
		return definition, err
	}
	if definition.Board == nil {
		return definition, nil
	}
	for index := range definition.Board.Units {
		if err := resolveSavedUnit(&definition.Board.Units[index], units, statuses); err != nil {
			return definition, err
		}
	}
	return definition, definition.Board.Validate()
}
func resolveSavedUnit(unit *app.UnitData, definitions map[string]app.UnitDefinition, statuses []app.UnitStatusOption) error {
	if source, ok := definitions[unit.ImageID]; ok {
		unit.ImageID = source.ImageID
	}
	if !knownImage(unit.ImageID, definitions) {
		return errors.New("unknown saved token image reference")
	}
	if unit.Status != "" && !knownStatus(unit.Status, statuses) {
		return errors.New("unknown saved token status")
	}
	if unit.Size == "" {
		unit.Size = app.SizeMedium
	}
	if unit.ImageFit == (app.ImageFit{}) {
		unit.ImageFit = app.DefaultImageFit()
	}
	return nil
}
func knownImage(id string, definitions map[string]app.UnitDefinition) bool {
	if id == app.PCUnitImageID || id == app.NPCUnitImageID || id == app.DefaultUnitImageID {
		return true
	}
	for _, definition := range definitions {
		if id == definition.ImageID {
			return true
		}
	}
	return false
}
func knownStatus(value app.UnitStatus, statuses []app.UnitStatusOption) bool {
	for _, status := range statuses {
		if status.Value == value {
			return true
		}
	}
	return false
}
