package plugins

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"

	"tableTop/main/internal/app"
	bundled "tableTop/main/plugins"
)

// InitializeDevelopment installs the bundled pack once for a fresh standalone
// board. Existing libraries (including empty initialized ones) are preserved.
func InitializeDevelopment(dir string, store app.LibraryStore, limits *app.StorageLimits) error {
	progressPath := filepath.Join(dir, ".plugin", "progress.json")
	data, err := os.ReadFile(progressPath)
	step := "maps"
	if err == nil {
		if err := json.Unmarshal(data, &step); err != nil {
			return err
		}
	} else {
		if !errors.Is(err, os.ErrNotExist) {
			return err
		}
		for _, file := range []string{"maps.json", "units.json", "statuses.json"} {
			if _, err := os.Stat(filepath.Join(dir, file)); err == nil {
				return nil
			} else if !errors.Is(err, os.ErrNotExist) {
				return err
			}
		}
		if store != nil {
			var initialized bool
			if lazy, ok := store.Maps("dev").(app.MapDocumentStore); ok {
				_, initialized, err = lazy.List()
			} else {
				_, initialized, err = store.Maps("dev").Load()
			}
			if err != nil {
				return err
			}
			if initialized {
				return nil
			}
			_, initialized, err = store.Units("dev").Load()
			if err != nil {
				return err
			}
			if initialized {
				return nil
			}
		}
		files, _ := fs.Sub(bundled.Builtin, "builtin")
		bounds, err := BoundsFromEnvironment()
		if err != nil {
			return err
		}
		pack, err := openPack(files, "", bounds)
		if err != nil {
			return err
		}
		for _, sub := range []string{filepath.Join(".plugin", "maps"), "maps", "images"} {
			if err := os.MkdirAll(filepath.Join(dir, sub), 0755); err != nil {
				return err
			}
		}
		if err := pack.process("dev", func(path string, data []byte) error {
			return app.WriteContentFile(filepath.Join(dir, path), data, limits)
		}); err != nil {
			return err
		}
		if err := app.WriteContentJSON(progressPath, step, limits); err != nil {
			return err
		}
	}
	for step != "" {
		var next string
		var err error
		switch step {
		case "maps":
			err = InitializeMaps(dir, "dev", store, limits)
			next = "statuses"
		case "statuses":
			err = InitializeStatuses(dir, "dev", store, limits)
			next = "units"
		case "units":
			err = InitializeUnits(dir, "dev", store, limits)
		default:
			return errors.New("invalid development initialization step")
		}
		if err != nil {
			return err
		}
		if err := app.WriteContentJSON(progressPath, next, limits); err != nil {
			return err
		}
		step = next
	}
	return os.RemoveAll(filepath.Join(dir, ".plugin"))
}
