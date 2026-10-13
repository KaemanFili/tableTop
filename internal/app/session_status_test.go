package app

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestSessionStatusDefinitionsAreIsolated(t *testing.T) {
	first, err := NewStateWithStatuses([]UnitStatusOption{{Value: "Marked", Color: "#123456"}})
	if err != nil {
		t.Fatal(err)
	}
	second, err := NewStateWithStatuses([]UnitStatusOption{{Value: "Marked", Color: "#ABCDEF"}})
	if err != nil {
		t.Fatal(err)
	}
	a, b := first.AddRandomUnit(PCUnitImageID), second.AddRandomUnit(PCUnitImageID)
	for _, entry := range []struct {
		s  *AppState
		id string
	}{{first, a.ID}, {second, b.ID}} {
		if _, err := entry.s.SetUnitStatus(entry.id, "Marked"); err != nil {
			t.Fatal(err)
		}
	}
	if first.StatusColor("Marked") != "#123456" || second.StatusColor("Marked") != "#ABCDEF" {
		t.Fatal("colors crossed tabletop boundaries")
	}
	if _, err := first.SetUnitStatus(a.ID, "Poisoned"); !errors.Is(err, ErrInvalidStatus) {
		t.Fatal("accepted bundled status outside session definitions")
	}
}
func TestSessionStatusesPersistAndDoNotReseed(t *testing.T) {
	dir := t.TempDir()
	before, err := LoadStatusAdditions(dir, "session", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "statuses.json")
	custom := []UnitStatusOption{{Value: "Marked", Color: "#123456"}}
	if err := writeJSONFile(path, statusDocument{Version: 2, Options: custom}); err != nil {
		t.Fatal(err)
	}
	after, err := LoadStatusAdditions(dir, "session", nil, nil)
	if err != nil || !reflect.DeepEqual(after, custom) {
		t.Fatalf("persisted definitions replaced: %v", err)
	}
	if reflect.DeepEqual(before, after) {
		t.Fatal("test did not exercise custom definitions")
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatal(err)
	}
}
func TestStateOwnsItsStatusDefinitions(t *testing.T) {
	values := []UnitStatusOption{{Value: "Marked", Color: "#123456"}}
	state, err := NewStateWithStatuses(values)
	if err != nil {
		t.Fatal(err)
	}
	values[0].Color = "#ABCDEF"
	copy := state.UnitStatuses()
	copy[0].Color = "#ABCDEF"
	if state.StatusColor("Marked") != "#123456" {
		t.Fatal("caller changed active definitions")
	}
}
