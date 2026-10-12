package app

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// writeJSONFile replaces a library only after its complete contents reach disk.
func writeJSONFile(path string, value any, limits ...*StorageLimits) error {
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	return writeStorageFile(path, append(data, '\n'), 0600, firstStorageLimit(limits))
}

// The library holds limits.mu across its image/library transaction, including
// rollback, so concurrent libraries cannot admit growth against the same space.
func writeStorageFile(path string, data []byte, mode os.FileMode, limits *StorageLimits) error {
	if limits != nil {
		if err := limits.check(path, int64(len(data))); err != nil {
			return err
		}
	}
	file, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+"-*")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	defer file.Close()
	if err := file.Chmod(mode); err != nil {
		return err
	}
	if _, err := file.Write(data); err != nil {
		return err
	}
	if err := file.Sync(); err != nil {
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	return os.Rename(file.Name(), path)
}
