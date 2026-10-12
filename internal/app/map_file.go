package app

import (
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
)

// scanMapFile bounds memory to one document and accepts legacy array files.
func scanMapRaw(path string, yield func(json.RawMessage) error) error {
	f, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	defer f.Close()
	d := json.NewDecoder(f)
	token, err := d.Token()
	if err != nil {
		return err
	}
	if token != json.Delim('[') {
		return errors.New("invalid map list")
	}
	for d.More() {
		var m json.RawMessage
		if err := d.Decode(&m); err != nil {
			return err
		}
		if err := yield(m); err != nil {
			return err
		}
	}
	if _, err := d.Token(); err != nil {
		return err
	}
	var extra any
	if err := d.Decode(&extra); err != io.EOF {
		return errors.New("unexpected data after map list")
	}
	return nil
}

func scanMapFile(path string, yield func(MapDefinition) error) error {
	return scanMapRaw(path, func(raw json.RawMessage) error {
		var m MapDefinition
		if err := json.Unmarshal(raw, &m); err != nil {
			return err
		}
		return yield(m)
	})
}
func scanMapMetadata(path string, yield func(MapDefinition) error) error {
	return scanMapRaw(path, func(raw json.RawMessage) error {
		type metadata MapDefinition
		var m struct {
			metadata
			Board json.RawMessage `json:"board"`
		}
		if err := json.Unmarshal(raw, &m); err != nil {
			return err
		}
		result := MapDefinition(m.metadata)
		result.HasSavedBoard = len(m.Board) > 0 && string(m.Board) != "null"
		return yield(result)
	})
}

// Rewrites preserve untouched documents, including fields unknown to this version.
func rewriteMapFile(path, id string, replacement *MapDefinition, limits *StorageLimits) error {
	return writeMapRawList(path, func(emit func(json.RawMessage) error) error {
		if err := scanMapRaw(path, func(raw json.RawMessage) error {
			var m struct {
				ID string `json:"id"`
			}
			if err := json.Unmarshal(raw, &m); err != nil {
				return err
			}
			if m.ID == id {
				return nil
			}
			return emit(raw)
		}); err != nil {
			return err
		}
		if replacement != nil {
			raw, err := json.Marshal(replacement)
			if err != nil {
				return err
			}
			return emit(raw)
		}
		return nil
	}, limits)
}
func writeMapList(path string, visit func(func(MapDefinition) error) error, limits *StorageLimits) error {
	return writeMapRawList(path, func(emit func(json.RawMessage) error) error {
		return visit(func(m MapDefinition) error {
			raw, err := json.Marshal(m)
			if err != nil {
				return err
			}
			return emit(raw)
		})
	}, limits)
}

// Match atomic file replacement quota semantics: admit the complete replacement
// against its destination before writing, while staged plugin files count normally.
func writeMapRawList(path string, visit func(func(json.RawMessage) error) error, limits *StorageLimits) error {
	var size int64 = 3
	first := true
	if err := visit(func(raw json.RawMessage) error {
		if !first {
			size++
		}
		first = false
		size += int64(len(raw))
		return nil
	}); err != nil {
		return err
	}
	if limits != nil {
		if err := limits.check(path, size); err != nil {
			return err
		}
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".maps-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	defer f.Close()
	if _, err := f.WriteString("["); err != nil {
		return err
	}
	first = true
	if err := visit(func(raw json.RawMessage) error {
		if !first {
			if _, err := f.WriteString(","); err != nil {
				return err
			}
		}
		first = false
		_, err := f.Write(raw)
		return err
	}); err != nil {
		return err
	}
	if _, err := f.WriteString("]\n"); err != nil {
		return err
	}
	if err := f.Sync(); err != nil {
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(f.Name(), path)
}

type ContentSource struct {
	PluginID string `json:"pluginID" bson:"pluginID"`
	Version  int    `json:"version" bson:"version"`
	Key      string `json:"key" bson:"key"`
}

// InitializeMapFile streams the prepared documents into one atomic JSON list.
func InitializeMapFile(dir string, visit func(func(MapDefinition) error) error, limits *StorageLimits) error {
	limits.lock()
	defer limits.unlock()
	path := filepath.Join(dir, "maps.json")
	return writeMapList(path, visit, limits)
}
