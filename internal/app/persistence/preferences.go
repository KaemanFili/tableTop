package persistence

import (
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"sync"

	"tableTop/main/internal/app/model"
)

type playerPreferencesDocument struct {
	Version int                                `json:"schemaVersion"`
	Players map[string]model.PlayerPreferences `json:"players"`
}

var ErrPreferenceValidation = errors.New("invalid preferences")

// PlayerPreferencesLibrary uses the same local settings storage as membership,
// in both file and MongoDB deployments. Publish only successfully committed saves.
type PlayerPreferencesLibrary struct {
	mu     sync.RWMutex
	path   string
	data   playerPreferencesDocument
	limits *StorageLimits
}

func NewPlayerPreferencesLibrary(dir string, limits ...*StorageLimits) (*PlayerPreferencesLibrary, error) {
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, err
	}
	library := &PlayerPreferencesLibrary{
		path: filepath.Join(dir, "preferences.json"), limits: firstStorageLimit(limits),
		data: playerPreferencesDocument{Version: 1, Players: make(map[string]model.PlayerPreferences)},
	}
	data, err := os.ReadFile(library.path)
	if errors.Is(err, os.ErrNotExist) {
		return library, nil
	}
	if err != nil {
		return nil, err
	}
	var saved playerPreferencesDocument
	if err := json.Unmarshal(data, &saved); err != nil {
		return nil, fmt.Errorf("read player preferences: %w", err)
	}
	library.data = saved
	if library.data.Version != 1 || library.data.Players == nil {
		return nil, errors.New("invalid player preferences document")
	}
	for id, preferences := range library.data.Players {
		if id == "" {
			return nil, errors.New("player preferences contain an empty account ID")
		}
		if err := preferences.Validate(); err != nil {
			return nil, fmt.Errorf("invalid saved player preferences: %w", err)
		}
	}
	return library, nil
}

func (library *PlayerPreferencesLibrary) ForUser(id string) model.PlayerPreferences {
	library.mu.RLock()
	defer library.mu.RUnlock()
	preferences := library.data.Players[id]
	preferences.Hotkeys = maps.Clone(preferences.Hotkeys)
	return preferences
}

func (library *PlayerPreferencesLibrary) Save(id string, preferences model.PlayerPreferences) error {
	if id == "" {
		return errors.New("player preferences require an account")
	}
	// Own the caller's map before validating and persisting it.
	preferences.Hotkeys = maps.Clone(preferences.Hotkeys)
	if err := preferences.Validate(); err != nil {
		return err
	}
	library.mu.Lock()
	defer library.mu.Unlock()
	return library.saveLocked(id, preferences)
}

// SetHotkey merges under the same lock as the disk commit so simultaneous
// editors cannot overwrite changes to other utilities on the same account.
func (library *PlayerPreferencesLibrary) SetHotkey(id, action, binding string) error {
	if id == "" {
		return errors.New("player preferences require an account")
	}
	library.mu.Lock()
	defer library.mu.Unlock()
	preferences := library.data.Players[id]
	preferences.Hotkeys = maps.Clone(preferences.Hotkeys)
	if preferences.Hotkeys == nil {
		preferences.Hotkeys = make(map[string]string)
	}
	// Validate the action even when clearing an assignment.
	if err := (model.PlayerPreferences{Hotkeys: map[string]string{action: "p"}}).Validate(); err != nil {
		return fmt.Errorf("%w: %v", ErrPreferenceValidation, err)
	}
	if binding == "" {
		delete(preferences.Hotkeys, action)
	} else {
		preferences.Hotkeys[action] = binding
	}
	if err := preferences.Validate(); err != nil {
		return fmt.Errorf("%w: %v", ErrPreferenceValidation, err)
	}
	return library.saveLocked(id, preferences)
}

func (library *PlayerPreferencesLibrary) saveLocked(id string, preferences model.PlayerPreferences) error {
	proposed := playerPreferencesDocument{Version: 1, Players: maps.Clone(library.data.Players)}
	proposed.Players[id] = preferences
	library.limits.lock()
	defer library.limits.unlock()
	if err := writeJSONFile(library.path, proposed, library.limits); err != nil {
		return err
	}
	library.data = proposed
	return nil
}
