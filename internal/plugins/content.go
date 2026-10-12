package plugins

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"tableTop/main/internal/app"
)

type mapEntry struct {
	Key   string `json:"key"`
	Image string `json:"image"`
	app.MapDefinition
}
type unitEntry struct {
	Key   string `json:"key"`
	Image string `json:"image"`
	app.UnitDefinition
}

func StableID(plugin, kind, key string) string {
	sum := sha256.Sum256([]byte(plugin + "\x00" + kind + "\x00" + key))
	return hex.EncodeToString(sum[:16])
}
func (p Pack) validate() error { return p.process("preflight", nil) }

// process holds unit metadata and only one map document/image at a time.
func (p Pack) process(scope string, emit contentWriter) error {
	statuses, err := p.readStatuses()
	if err != nil {
		return fmt.Errorf("statuses: %w", err)
	}
	units, byKey, err := p.readUnits(scope, emit)
	if err != nil {
		return fmt.Errorf("units: %w", err)
	}
	if err := p.readMaps(scope, byKey, statuses, emit); err != nil {
		return fmt.Errorf("maps: %w", err)
	}
	if err := emit.json(filepath.Join(".plugin", "statuses.json"), statuses); err != nil {
		return err
	}
	return emit.json(filepath.Join(".plugin", "units.json"), struct {
		Version int                  `json:"version"`
		Units   []app.UnitDefinition `json:"units"`
	}{1, units})
}

// Stage creates no session metadata or database documents. On failure it removes
// its files; successful staging can initialize without the installed source.
func (p Pack) Stage(dataRoot, id string, limits *app.StorageLimits) error {
	dir := filepath.Join(dataRoot, "sessions", id)
	if _, err := os.Lstat(dir); !errors.Is(err, os.ErrNotExist) {
		if err != nil {
			return err
		}
		return errors.New("session staging destination exists")
	}
	for _, sub := range []string{filepath.Join(".plugin", "maps"), "maps", "images"} {
		if err := os.MkdirAll(filepath.Join(dir, sub), 0755); err != nil {
			app.RemoveTableFiles(dataRoot, id, limits)
			return err
		}
	}
	err := p.process(id, func(path string, data []byte) error {
		return app.WriteContentFile(filepath.Join(dir, path), data, limits)
	})
	if err != nil {
		return errors.Join(err, app.RemoveTableFiles(dataRoot, id, limits))
	}
	return nil
}
func VisitStagedMaps(dir string, yield func(app.MapDefinition) error) error {
	entries, err := os.ReadDir(filepath.Join(dir, ".plugin", "maps"))
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if entry.IsDir() {
			return errors.New("invalid staged map")
		}
		data, err := os.ReadFile(filepath.Join(dir, ".plugin", "maps", entry.Name()))
		if err != nil {
			return err
		}
		var m app.MapDefinition
		if err := json.Unmarshal(data, &m); err != nil {
			return err
		}
		if err := yield(m); err != nil {
			return err
		}
	}
	return nil
}
func InitializeMaps(dir, scope string, store app.LibraryStore, limits *app.StorageLimits) error {
	visit := func(yield func(app.MapDefinition) error) error { return VisitStagedMaps(dir, yield) }
	if store != nil {
		if lazy, ok := store.Maps(scope).(app.MapDocumentStore); ok {
			return lazy.InitializeEach(visit)
		}
		entries := []app.MapDefinition{}
		if err := visit(func(m app.MapDefinition) error { entries = append(entries, m); return nil }); err != nil {
			return err
		}
		return store.Maps(scope).Initialize(entries)
	}
	return app.InitializeMapFile(dir, visit, limits)
}
func InitializeStatuses(dir, scope string, store app.LibraryStore, limits *app.StorageLimits) error {
	data, err := os.ReadFile(filepath.Join(dir, ".plugin", "statuses.json"))
	if err != nil {
		return err
	}
	var values []app.UnitStatusOption
	if err := json.Unmarshal(data, &values); err != nil {
		return err
	}
	if err := app.ValidateUnitStatuses(values); err != nil {
		return err
	}
	if db, ok := store.(app.StatusStore); ok {
		return db.InitializeStatuses(scope, values)
	}
	return app.WriteContentJSON(filepath.Join(dir, "statuses.json"), struct {
		Version int                    `json:"schemaVersion"`
		Options []app.UnitStatusOption `json:"options"`
	}{1, values}, limits)
}
func InitializeUnits(dir, scope string, store app.LibraryStore, limits *app.StorageLimits) error {
	data, err := os.ReadFile(filepath.Join(dir, ".plugin", "units.json"))
	if err != nil {
		return err
	}
	var doc struct {
		Version int                  `json:"version"`
		Units   []app.UnitDefinition `json:"units"`
	}
	if err := json.Unmarshal(data, &doc); err != nil {
		return err
	}
	if doc.Version != 1 {
		return errors.New("invalid staged units")
	}
	if store != nil {
		return store.Units(scope).Initialize(doc.Units)
	}
	return app.WriteContentFile(filepath.Join(dir, "units.json"), data, limits)
}
