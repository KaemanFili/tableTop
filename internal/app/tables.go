package app

import (
	"crypto/rand"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync"
	"unicode/utf8"
)

type TableDefinition struct {
	ID             string         `json:"id"`
	Name           string         `json:"name"`
	OwnerID        string         `json:"ownerID"`
	Members        []string       `json:"members"`
	PluginBinding  *PluginBinding `json:"pluginBinding,omitempty"`
	Initialization string         `json:"initialization,omitempty"`
	Deleting       bool           `json:"deleting,omitempty"`
}

func (table TableDefinition) HasMember(id string) bool { return slices.Contains(table.Members, id) }

var tableIDPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{16,64}$`)

type TableLibrary struct {
	mu     sync.RWMutex
	path   string
	tables []TableDefinition
	limits *StorageLimits
}

func NewTableLibrary(dir string, limits ...*StorageLimits) (*TableLibrary, error) {
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, err
	}
	library := &TableLibrary{path: filepath.Join(dir, "sessions.json"), limits: firstStorageLimit(limits)}
	data, err := os.ReadFile(library.path)
	if errors.Is(err, os.ErrNotExist) {
		return library, nil
	}
	if err != nil {
		return nil, err
	}
	if err = json.Unmarshal(data, &library.tables); err != nil {
		return nil, err
	}
	seen := make(map[string]bool)
	for _, table := range library.tables {
		if !tableIDPattern.MatchString(table.ID) || seen[table.ID] || strings.TrimSpace(table.Name) == "" || table.OwnerID == "" || !table.HasMember(table.OwnerID) {
			return nil, errors.New("invalid tabletop session library")
		}
		seen[table.ID] = true
	}
	return library, nil
}

func (library *TableLibrary) ByID(id string) (TableDefinition, bool) {
	library.mu.RLock()
	defer library.mu.RUnlock()
	for _, table := range library.tables {
		if table.ID == id {
			table.Members = slices.Clone(table.Members)
			return table, true
		}
	}
	return TableDefinition{}, false
}

func (library *TableLibrary) ForUser(id string) []TableDefinition {
	library.mu.RLock()
	defer library.mu.RUnlock()
	var tables []TableDefinition
	for _, table := range library.tables {
		if table.HasMember(id) && ((!table.Deleting && table.Initialization == "") || table.OwnerID == id) {
			table.Members = slices.Clone(table.Members)
			tables = append(tables, table)
		}
	}
	return tables
}

func (library *TableLibrary) Create(name, owner string) (TableDefinition, error) {
	return library.create(name, owner, TableDefinition{ID: rand.Text()})
}
func (library *TableLibrary) CreateBound(name, owner string, binding PluginBinding) (TableDefinition, error) {
	if binding.PluginID == "" || binding.Version < 1 || len(binding.Fingerprint) != 64 {
		return TableDefinition{}, errors.New("invalid plugin binding")
	}
	return library.create(name, owner, TableDefinition{ID: rand.Text(), PluginBinding: &binding, Initialization: "owned"})
}
func (library *TableLibrary) create(name, owner string, prepared TableDefinition) (TableDefinition, error) {
	name = strings.TrimSpace(name)
	if name == "" || !utf8.ValidString(name) || utf8.RuneCountInString(name) > 80 || owner == "" {
		return TableDefinition{}, errors.New("Enter a session name between 1 and 80 characters.")
	}
	library.mu.Lock()
	defer library.mu.Unlock()
	library.limits.lock()
	defer library.limits.unlock()
	if len(library.tables) >= MaxTabletops {
		return TableDefinition{}, ErrCapacity
	}
	table := prepared
	table.Name, table.OwnerID, table.Members = name, owner, []string{owner}
	for _, existing := range library.tables {
		if existing.ID == table.ID {
			return TableDefinition{}, errors.New("duplicate tabletop ID")
		}
	}
	proposed := append(slices.Clone(library.tables), table)
	if err := writeJSONFile(library.path, proposed, library.limits); err != nil {
		return TableDefinition{}, err
	}
	library.tables = proposed
	table.Members = slices.Clone(table.Members)
	return table, nil
}

func (library *TableLibrary) Join(id, user string) (TableDefinition, error) {
	if user == "" {
		return TableDefinition{}, errors.New("Sign in before joining a session.")
	}
	library.mu.Lock()
	defer library.mu.Unlock()
	library.limits.lock()
	defer library.limits.unlock()
	for index, table := range library.tables {
		if table.ID != id {
			continue
		}
		if table.Deleting || table.Initialization != "" {
			return TableDefinition{}, errors.New("This tabletop is unavailable.")
		}
		if !table.HasMember(user) {
			if len(table.Members) >= MaxTabletopMembers {
				return TableDefinition{}, ErrCapacity
			}
			table.Members = append(slices.Clone(table.Members), user)
			proposed := slices.Clone(library.tables)
			proposed[index] = table
			if err := writeJSONFile(library.path, proposed, library.limits); err != nil {
				return TableDefinition{}, err
			}
			library.tables = proposed
		}
		table.Members = slices.Clone(table.Members)
		return table, nil
	}
	return TableDefinition{}, errors.New("Session not found. Check the join code.")
}

var ErrTableNotFound = errors.New("tabletop not found")
var ErrTableNotOwner = errors.New("only the tabletop owner can delete it")

// Persist deletion intent before removing files or documents. Failed cleanup
// remains retryable by the owner and cannot reopen a partially deleted board.
func (library *TableLibrary) BeginDelete(id, owner string) error {
	library.mu.Lock()
	defer library.mu.Unlock()
	library.limits.lock()
	defer library.limits.unlock()
	for index, table := range library.tables {
		if table.ID != id {
			continue
		}
		if owner == "" || table.OwnerID != owner {
			return ErrTableNotOwner
		}
		if table.Deleting {
			return nil
		}
		proposed := slices.Clone(library.tables)
		proposed[index].Deleting = true
		// Deletion must remain available even when the configured quota is full.
		if err := writeJSONFile(library.path, proposed); err != nil {
			return err
		}
		library.tables = proposed
		return nil
	}
	return ErrTableNotFound
}

func (library *TableLibrary) FinishDelete(id, owner string) error {
	library.mu.Lock()
	defer library.mu.Unlock()
	library.limits.lock()
	defer library.limits.unlock()
	for index, table := range library.tables {
		if table.ID != id {
			continue
		}
		if owner == "" || table.OwnerID != owner {
			return ErrTableNotOwner
		}
		if !table.Deleting {
			return errors.New("tabletop deletion has not started")
		}
		proposed := slices.Delete(slices.Clone(library.tables), index, index+1)
		if err := writeJSONFile(library.path, proposed, library.limits); err != nil {
			return err
		}
		library.tables = proposed
		return nil
	}
	return ErrTableNotFound
}

func RemoveTableFiles(dir, id string, limits *StorageLimits) error {
	if !tableIDPattern.MatchString(id) {
		return errors.New("invalid tabletop ID")
	}
	limits.lock()
	defer limits.unlock()
	return os.RemoveAll(filepath.Join(dir, "sessions", id))
}

// FinishInitialization exposes the session only after its owned persistence exists.
func (library *TableLibrary) FinishInitialization(id string) error {
	library.mu.Lock()
	defer library.mu.Unlock()
	library.limits.lock()
	defer library.limits.unlock()
	for i, t := range library.tables {
		if t.ID != id {
			continue
		}
		if t.Deleting || t.Initialization != "owned" {
			return errors.New("session is unavailable")
		}
		proposed := slices.Clone(library.tables)
		proposed[i].Initialization = ""
		if err := writeJSONFile(library.path, proposed, library.limits); err != nil {
			return err
		}
		library.tables = proposed
		return nil
	}
	return ErrTableNotFound
}
