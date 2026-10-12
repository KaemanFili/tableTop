package app

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

func TestOversizedSavedBoardDoesNotBlockMapLibrary(t *testing.T) {
	dir := t.TempDir()
	oversized := MapDefinition{ID: "1234567890abcdef1234567890abcdef", Name: "Legacy map", Width: 20, Height: 20, Board: &SavedBoard{Units: make([]UnitData, MaxBoardUnits+1)}}
	other := MapDefinition{ID: "abcdef1234567890abcdef1234567890", Name: "Other map", Width: 20, Height: 20}
	data, err := json.Marshal([]MapDefinition{oversized, other})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "maps.json"), data, 0600); err != nil {
		t.Fatal(err)
	}
	library, err := NewMapLibrary(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(library.Maps()) != 2 {
		t.Fatal("oversized entry hid other saved maps")
	}
	if _, exists := library.MapByID(other.ID); !exists {
		t.Fatal("other map is unavailable")
	}
	if err := library.SaveBoard(oversized, *oversized.Board); err != ErrCapacity {
		t.Fatalf("oversized save: %v", err)
	}
	if err := library.Delete(oversized.ID); err != nil {
		t.Fatal(err)
	}
	if len(library.Maps()) != 1 {
		t.Fatal("oversized map could not be deleted")
	}
}

func TestLibraryPersistsConcurrentCreations(t *testing.T) {
	dir := t.TempDir()
	library, err := NewUnitLibrary(dir)
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for range 12 {
		wg.Go(func() {
			if _, err := library.Create("Scout", "npc", SizeMedium, DefaultImageFit(), nil); err != nil {
				t.Error(err)
			}
		})
	}
	wg.Wait()
	reloaded, err := NewUnitLibrary(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got := len(reloaded.Units()); got != 14 {
		t.Fatalf("saved %d entries, want 12 new units plus 2 defaults", got)
	}
	for _, unit := range library.Units() {
		if got, exists := reloaded.UnitByID(unit.ID); !exists || got != unit {
			t.Fatalf("unit was not restored: %+v", unit)
		}
	}
}

func TestFailedLibrarySaveDoesNotPublishUnit(t *testing.T) {
	dir := t.TempDir()
	library, err := NewUnitLibrary(dir)
	if err != nil {
		t.Fatal(err)
	}
	// A directory at the library destination makes the atomic rename fail.
	if err := os.Mkdir(filepath.Join(dir, "units.json"), 0755); err != nil {
		t.Fatal(err)
	}
	if _, err := library.Create("Scout", "npc", SizeMedium, DefaultImageFit(), []byte("image")); err == nil {
		t.Fatal("save should fail")
	}
	if len(library.Units()) != 2 {
		t.Fatal("failed unit was published")
	}
	images, err := os.ReadDir(filepath.Join(dir, "images"))
	if err != nil || len(images) != 0 {
		t.Fatalf("failed save left orphan images: %v, %v", images, err)
	}
}

func TestInvalidLibraryIsNotOverwritten(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "units.json")
	invalid := []byte(`[{"id":"../../outside","name":"Scout","unitType":"npc","imageID":"../../outside"}]`)
	if err := os.WriteFile(path, invalid, 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := NewUnitLibrary(dir); err == nil {
		t.Fatal("invalid library should not load")
	}
	got, err := os.ReadFile(path)
	if err != nil || string(got) != string(invalid) {
		t.Fatal("invalid library must remain untouched")
	}
}

func TestLibraryMigratesEntriesWithoutSize(t *testing.T) {
	dir := t.TempDir()
	const id = "1234567890abcdef1234567890abcdef"
	legacy := []byte(`[{"id":"` + id + `","name":"Old scout","unitType":"npc","imageID":"goblin"}]`)
	if err := os.WriteFile(filepath.Join(dir, "units.json"), legacy, 0644); err != nil {
		t.Fatal(err)
	}
	library, err := NewUnitLibrary(dir)
	if err != nil {
		t.Fatal(err)
	}
	if unit, exists := library.UnitByID(id); !exists || unit.Size != SizeMedium || unit.ImageFit != DefaultImageFit() {
		t.Fatalf("legacy unit = %+v, want Medium", unit)
	}
	if _, err := library.Create("Giant", "npc", SizeHuge, DefaultImageFit(), nil); err != nil {
		t.Fatal(err)
	}
	reloaded, err := NewUnitLibrary(dir)
	if err != nil {
		t.Fatal(err)
	}
	if unit, _ := reloaded.UnitByID(id); unit.Size != SizeMedium || unit.ImageFit != DefaultImageFit() {
		t.Fatal("saving the library lost the legacy unit's default size")
	}
}
