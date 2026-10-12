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
	ID      string   `json:"id"`
	Name    string   `json:"name"`
	OwnerID string   `json:"ownerID"`
	Members []string `json:"members"`
}

func (table TableDefinition) HasMember(id string) bool { return slices.Contains(table.Members, id) }

var tableIDPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{16,64}$`)

type TableLibrary struct {
	mu     sync.RWMutex
	path   string
	tables []TableDefinition
}

func NewTableLibrary(dir string) (*TableLibrary, error) {
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, err
	}
	library := &TableLibrary{path: filepath.Join(dir, "sessions.json")}
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
			return nil, errors.New("invalid tabletop session catalog")
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
		if table.HasMember(id) {
			table.Members = slices.Clone(table.Members)
			tables = append(tables, table)
		}
	}
	return tables
}

func (library *TableLibrary) Create(name, owner string) (TableDefinition, error) {
	name = strings.TrimSpace(name)
	if name == "" || !utf8.ValidString(name) || utf8.RuneCountInString(name) > 80 || owner == "" {
		return TableDefinition{}, errors.New("Enter a session name between 1 and 80 characters.")
	}
	library.mu.Lock()
	defer library.mu.Unlock()
	table := TableDefinition{ID: rand.Text(), Name: name, OwnerID: owner, Members: []string{owner}}
	proposed := append(slices.Clone(library.tables), table)
	if err := writeJSONFile(library.path, proposed); err != nil {
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
	for index, table := range library.tables {
		if table.ID != id {
			continue
		}
		if !table.HasMember(user) {
			table.Members = append(slices.Clone(table.Members), user)
			proposed := slices.Clone(library.tables)
			proposed[index] = table
			if err := writeJSONFile(library.path, proposed); err != nil {
				return TableDefinition{}, err
			}
			library.tables = proposed
		}
		table.Members = slices.Clone(table.Members)
		return table, nil
	}
	return TableDefinition{}, errors.New("Session not found. Check the join code.")
}
