package persistence

import (
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"tableTop/main/internal/app/model"
)

func TestConcurrentHotkeyEditsPreserveOtherUtilities(t *testing.T) {
	library, err := NewPlayerPreferencesLibrary(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	start := make(chan struct{})
	var workers sync.WaitGroup
	for action, binding := range map[string]string{"pen": "p", "ruler": "r", "fog": "ctrl+f", "ping": "q"} {
		workers.Go(func() {
			<-start
			if err := library.SetHotkey("player", action, binding); err != nil {
				t.Error(err)
			}
		})
	}
	close(start)
	workers.Wait()
	if len(library.ForUser("player").Hotkeys) != 4 {
		t.Fatal("concurrent edits lost an assignment")
	}
	if err := library.SetHotkey("player", "eraser", "p"); !errors.Is(err, ErrPreferenceValidation) {
		t.Fatalf("duplicate assignment accepted: %v", err)
	}
}

func TestPlayerPreferencesPersistAndIsolateAccounts(t *testing.T) {
	dir := t.TempDir()
	library, err := NewPlayerPreferencesLibrary(dir)
	if err != nil {
		t.Fatal(err)
	}
	preferences := model.PlayerPreferences{Hotkeys: map[string]string{"pen": "p", "fog": "ctrl+alt+shift+meta+f"}}
	if err := library.Save("player-one", preferences); err != nil {
		t.Fatal(err)
	}
	preferences.Hotkeys["pen"] = "x"
	copy := library.ForUser("player-one")
	copy.Hotkeys["pen"] = "y"
	if library.ForUser("player-one").Hotkeys["pen"] != "p" {
		t.Fatal("caller mutated persisted preferences")
	}
	if len(library.ForUser("player-two").Hotkeys) != 0 {
		t.Fatal("preferences leaked to another account")
	}
	reloaded, err := NewPlayerPreferencesLibrary(dir)
	if err != nil || reloaded.ForUser("player-one").Hotkeys["pen"] != "p" {
		t.Fatalf("preferences did not survive restart: %v", err)
	}
	if reloaded.ForUser("player-one").Hotkeys["fog"] != "ctrl+alt+shift+meta+f" {
		t.Fatal("modifier combination did not survive restart")
	}
	if err := reloaded.Save("player-two", model.PlayerPreferences{Hotkeys: map[string]string{"pen": "r"}}); err != nil {
		t.Fatal(err)
	}
	if err := reloaded.Save("player-one", model.PlayerPreferences{}); err != nil {
		t.Fatal(err)
	}
	reloaded, err = NewPlayerPreferencesLibrary(dir)
	if err != nil || len(reloaded.ForUser("player-one").Hotkeys) != 0 || reloaded.ForUser("player-two").Hotkeys["pen"] != "r" {
		t.Fatalf("clear changed another account or did not persist: %v", err)
	}
}

func TestPlayerPreferencesRejectInvalidAndFailedSaves(t *testing.T) {
	library, err := NewPlayerPreferencesLibrary(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for _, hotkeys := range []map[string]string{
		{"pen": "p", "ruler": "p"}, {"unknown": "x"}, {"pen": "Escape"},
		{"pen": " "}, {"pen": "P"}, {"pen": ""}, {"pen": "é"},
	} {
		if err := library.Save("player", model.PlayerPreferences{Hotkeys: hotkeys}); err == nil {
			t.Fatalf("accepted invalid preferences: %v", hotkeys)
		}
	}
	if err := library.Save("", model.PlayerPreferences{}); err == nil {
		t.Fatal("accepted an empty account")
	}
	if err := library.Save("player", model.PlayerPreferences{Hotkeys: map[string]string{"pen": "p"}}); err != nil {
		t.Fatal(err)
	}
	library.path = filepath.Join(library.path, "missing", "preferences.json")
	if err := library.Save("player", model.PlayerPreferences{}); err == nil {
		t.Fatal("save unexpectedly succeeded")
	}
	if library.ForUser("player").Hotkeys["pen"] != "p" {
		t.Fatal("failed save published new preferences")
	}
}

func TestPlayerPreferencesAreGlobalSettingsForStorageLimits(t *testing.T) {
	dir := t.TempDir()
	limits, err := NewStorageLimits(dir, 4096, 1)
	if err != nil {
		t.Fatal(err)
	}
	library, err := NewPlayerPreferencesLibrary(dir, limits)
	if err != nil {
		t.Fatal(err)
	}
	if err := library.Save("player", model.PlayerPreferences{Hotkeys: map[string]string{"pen": "p"}}); err != nil {
		t.Fatalf("preferences should not consume a tabletop quota: %v", err)
	}
	limits.total = 1
	if err := library.Save("second", model.PlayerPreferences{Hotkeys: map[string]string{"pen": "p"}}); !errors.Is(err, ErrStorageLimit) {
		t.Fatalf("preferences bypassed global quota: %v", err)
	}
}

func TestPlayerPreferencesCorruptionIsPreserved(t *testing.T) {
	for _, data := range []string{`{`, `null`, `{"schemaVersion":2,"players":{}}`, `{"schemaVersion":1,"players":{"player":{"hotkeys":{"pen":"p","ruler":"p"}}}}`} {
		dir := t.TempDir()
		path := filepath.Join(dir, "preferences.json")
		if err := os.WriteFile(path, []byte(data), 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := NewPlayerPreferencesLibrary(dir); err == nil {
			t.Fatalf("accepted corrupt preferences: %s", data)
		}
		after, err := os.ReadFile(path)
		if err != nil || string(after) != data {
			t.Fatal("corrupt preferences were overwritten")
		}
	}
}
