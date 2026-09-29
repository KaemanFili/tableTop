package app

import (
	"os"
	"path/filepath"
	"sync"
	"testing"
)

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
	if got := len(reloaded.Snapshot()); got != 14 {
		t.Fatalf("saved %d entries, want 12 new units plus 2 defaults", got)
	}
	for _, unit := range library.Snapshot() {
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
	// A directory at the catalog destination makes the atomic rename fail.
	if err := os.Mkdir(filepath.Join(dir, "units.json"), 0755); err != nil {
		t.Fatal(err)
	}
	if _, err := library.Create("Scout", "npc", SizeMedium, DefaultImageFit(), []byte("image")); err == nil {
		t.Fatal("save should fail")
	}
	if len(library.Snapshot()) != 2 {
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
		t.Fatal("invalid catalog should not load")
	}
	got, err := os.ReadFile(path)
	if err != nil || string(got) != string(invalid) {
		t.Fatal("invalid catalog must remain untouched")
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
		t.Fatal("saving the catalog lost the legacy unit's default size")
	}
}
