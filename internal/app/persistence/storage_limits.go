package persistence

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
)

var ErrStorageLimit = errors.New("storage limit reached")

// StorageLimits caps committed file bytes for one app process. All libraries
// sharing a data root must share this instance to serialize admission and writes.
type StorageLimits struct {
	mu           sync.Mutex
	root         string
	total, table int64
}

func StorageLimitsFromEnvironment(root string) (*StorageLimits, error) {
	read := func(name string, fallback int64) (int64, error) {
		value := os.Getenv(name)
		if value == "" {
			return fallback, nil
		}
		limit, err := strconv.ParseInt(value, 10, 64)
		if err != nil || limit <= 0 {
			return 0, fmt.Errorf("%s must be a positive byte count", name)
		}
		return limit, nil
	}
	total, err := read("TABLETOP_STORAGE_LIMIT_BYTES", 5<<30)
	if err != nil {
		return nil, err
	}
	table, err := read("TABLETOP_SESSION_STORAGE_LIMIT_BYTES", 512<<20)
	if err != nil {
		return nil, err
	}
	return NewStorageLimits(root, total, table)
}

func NewStorageLimits(root string, total, table int64) (*StorageLimits, error) {
	if total <= 0 || table <= 0 || table > total {
		return nil, errors.New("storage limits must be positive, with the tabletop limit no larger than the total")
	}
	root, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	return &StorageLimits{root: root, total: total, table: table}, nil
}

func firstStorageLimit(limits []*StorageLimits) *StorageLimits {
	if len(limits) == 0 {
		return nil
	}
	return limits[0]
}

func (limits *StorageLimits) lock() {
	if limits != nil {
		limits.mu.Lock()
	}
}

func (limits *StorageLimits) unlock() {
	if limits != nil {
		limits.mu.Unlock()
	}
}

// check requires mu. Shrinking writes remain possible even when an operator
// lowers a limit below existing usage, so deleting maps can reclaim capacity.
func (limits *StorageLimits) check(path string, size int64) error {
	path, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	relative, err := filepath.Rel(limits.root, path)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return errors.New("storage path is outside the configured data root")
	}
	var oldSize int64
	info, err := os.Lstat(path)
	if err == nil {
		if !info.Mode().IsRegular() {
			return errors.New("storage destination is not a regular file")
		}
		oldSize = info.Size()
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	growth := size - oldSize
	if growth <= 0 {
		return nil
	}
	// A local development board shares the root. The sessions library counts
	// only toward the global limit; session files count toward both limits.
	tableRoot := limits.root
	parts := strings.Split(relative, string(filepath.Separator))
	if parts[0] == "sessions" && len(parts) >= 3 {
		tableRoot = filepath.Join(limits.root, parts[0], parts[1])
	} else if relative == "sessions.json" || relative == "access.json" {
		tableRoot = ""
	}
	var totalUsed, tableUsed int64
	err = filepath.WalkDir(limits.root, func(filePath string, entry fs.DirEntry, err error) error {
		// Files removed outside the app during the scan no longer occupy space.
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		info, err := entry.Info()
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return errors.New("data directory contains a non-regular file")
		}
		totalUsed += info.Size()
		inTable := tableRoot != "" && strings.HasPrefix(filePath, tableRoot+string(filepath.Separator))
		if tableRoot == limits.root && (filePath == filepath.Join(limits.root, "sessions.json") || filePath == filepath.Join(limits.root, "access.json") || strings.HasPrefix(filePath, filepath.Join(limits.root, "sessions")+string(filepath.Separator))) {
			inTable = false
		}
		if inTable {
			tableUsed += info.Size()
		}
		return nil
	})
	if err != nil {
		return err
	}
	if growth > limits.total-totalUsed {
		return fmt.Errorf("%w: the server's storage is full. Ask the host to free space or increase its limit", ErrStorageLimit)
	}
	if tableRoot != "" && growth > limits.table-tableUsed {
		return fmt.Errorf("%w: this tabletop's storage is full. Delete unused maps or ask the host to increase its limit", ErrStorageLimit)
	}
	return nil
}
