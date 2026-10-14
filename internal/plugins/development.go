package plugins

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"

	"tableTop/main/internal/app/model"
	"tableTop/main/internal/app/persistence"
)

func Development(dir, assets string, store persistence.LibraryStore, limits *persistence.StorageLimits) (*Pack, error) {
	bounds, err := BoundsFromEnvironment()
	if err != nil {
		return nil, err
	}
	dirs := Directories(assets)
	binding, err := developmentBinding(dir, dirs, bounds, limits)
	if err != nil {
		return nil, err
	}
	defaults, err := Resolve(dirs, bounds, binding)
	if err != nil {
		return nil, err
	}
	if err := persistence.InitializeOwned(dir, "dev", store, limits); err != nil {
		return nil, err
	}
	return defaults, nil
}

func developmentBinding(dir string, dirs []string, bounds Bounds, limits *persistence.StorageLimits) (model.PluginBinding, error) {
	path := filepath.Join(dir, "plugin-binding.json")
	data, err := os.ReadFile(path)
	if err == nil {
		var binding model.PluginBinding
		err := json.Unmarshal(data, &binding)
		return binding, err
	}
	if !errors.Is(err, os.ErrNotExist) {
		return model.PluginBinding{}, err
	}
	binding, err := builtinBinding(dirs, bounds)
	if err != nil {
		return binding, err
	}
	if err := os.MkdirAll(dir, 0755); err != nil {
		return binding, err
	}
	return binding, persistence.WriteContentJSON(path, binding, limits)
}
func builtinBinding(dirs []string, bounds Bounds) (model.PluginBinding, error) {
	for _, pack := range Discover(dirs, bounds) {
		if pack.ID == "builtin" && pack.Version == 1 {
			return pack.Binding(), nil
		}
	}
	return model.PluginBinding{}, errors.New("built-in plugin unavailable")
}
