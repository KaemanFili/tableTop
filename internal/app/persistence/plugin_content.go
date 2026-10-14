package persistence

import (
	"errors"
	"os"
	"path/filepath"

	"tableTop/main/internal/app/model"
)

var ErrReadOnly = errors.New("plugin defaults are read-only; create a copy to change them")

// PluginDefaults is read-only; libraries persist only owned additions and copies.
type PluginDefaults interface {
	Validate() error
	Binding() model.PluginBinding
	Maps() []model.MapDefinition
	MetadataByID(string) (model.MapDefinition, bool)
	Units() []model.UnitDefinition
	UnitByID(string) (model.UnitDefinition, bool)
	LoadMap(string) (model.MapDefinition, error)
	Image(model.ArtworkReference) ([]byte, error)
	Statuses() []model.UnitStatusOption
}

// InitializeOwned creates explicit empty libraries; retries never overwrite entries.
func InitializeOwned(dir, scope string, store LibraryStore, limits *StorageLimits) error {
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	if err := initializeOwnedLibraries(dir, scope, store, limits); err != nil {
		return err
	}
	_, err := LoadStatusAdditions(dir, scope, store, limits)
	return err
}
func initializeOwnedLibraries(dir, scope string, store LibraryStore, limits *StorageLimits) error {
	if store != nil {
		if err := initializeEmptyStore(store.Maps(scope)); err != nil {
			return err
		}
		return initializeEmptyStore(store.Units(scope))
	}
	if err := createJSONIfMissing(filepath.Join(dir, "maps.json"), []model.MapDefinition{}, limits); err != nil {
		return err
	}
	return createJSONIfMissing(filepath.Join(dir, "units.json"), unitDocument{Version: 1, Units: []model.UnitDefinition{}}, limits)
}
func initializeEmptyStore[T any](store DocumentStore[T]) error {
	_, ready, err := store.Load()
	if err != nil {
		return err
	}
	if ready {
		return nil
	}
	return store.Initialize(nil)
}
func createJSONIfMissing(path string, value any, limits *StorageLimits) error {
	_, err := os.Stat(path)
	if err == nil {
		return nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return WriteContentJSON(path, value, limits)
}
func pluginImage(defaults PluginDefaults, ref *model.ArtworkReference) ([]byte, error) {
	if defaults == nil || ref == nil {
		return nil, os.ErrNotExist
	}
	if err := defaults.Validate(); err != nil {
		return nil, err
	}
	return defaults.Image(*ref)
}
