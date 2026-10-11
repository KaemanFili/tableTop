package app

import (
	"os"
	"path/filepath"
	"testing"
)

func TestUnitDeletionPersistsAndPreservesCopyArtwork(t *testing.T) {
	dir := t.TempDir()
	library, err := NewUnitLibrary(dir)
	if err != nil {
		t.Fatal(err)
	}
	unit, err := library.Create("Scout", "npc", SizeMedium, DefaultImageFit(), []byte("artwork"))
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{unit.ID, "knight", "goblin", "missing"} {
		if err := library.Delete(id); err != nil {
			t.Fatal(err)
		}
	}
	// Creating another unit must not resurrect the deleted starter entries.
	survivor, err := library.Create("Mage", "pc", SizeSmall, DefaultImageFit(), nil)
	if err != nil {
		t.Fatal(err)
	}
	reloaded, err := NewUnitLibrary(dir)
	if err != nil {
		t.Fatal(err)
	}
	if units := reloaded.Units(); len(units) != 1 || units[0].ID != survivor.ID {
		t.Fatalf("unexpected units: %+v", units)
	}
	path, ok := reloaded.ImagePath(unit.ID)
	if !ok {
		t.Fatal("placed copies lost artwork path")
	}
	if data, err := os.ReadFile(path); err != nil || string(data) != "artwork" {
		t.Fatal("placed copies lost artwork")
	}
	if _, ok := reloaded.ImagePath("../../outside"); ok {
		t.Fatal("unsafe artwork path accepted")
	}
}

func TestFailedUnitDeletionPreservesEntry(t *testing.T) {
	dir := t.TempDir()
	library, err := NewUnitLibrary(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(dir, "units.json"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := library.Delete("knight"); err == nil {
		t.Fatal("expected save failure")
	}
	if _, exists := library.UnitByID("knight"); !exists {
		t.Fatal("failed deletion removed entry")
	}
}

func TestMapDeletionPersistsAndRemovesArtwork(t *testing.T) {
	dir := t.TempDir()
	library, err := NewMapLibrary(dir)
	if err != nil {
		t.Fatal(err)
	}
	removed, err := library.Create(MapDefinition{Name: "Forest", Width: 20, Height: 20}, []byte("image"))
	if err != nil {
		t.Fatal(err)
	}
	survivor, err := library.Create(MapDefinition{Name: "Dungeon", Width: 20, Height: 20}, []byte("image"))
	if err != nil {
		t.Fatal(err)
	}
	path, _ := library.ImagePath(removed.ID)
	for range 2 {
		if err := library.Delete(removed.ID); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("deleted image still exists: %v", err)
	}
	reloaded, err := NewMapLibrary(dir)
	if err != nil {
		t.Fatal(err)
	}
	if maps := reloaded.Maps(); len(maps) != 1 || maps[0].ID != survivor.ID {
		t.Fatalf("unexpected maps: %+v", maps)
	}
}

func TestFailedMapDeletionPreservesEntryAndArtwork(t *testing.T) {
	dir := t.TempDir()
	library, err := NewMapLibrary(dir)
	if err != nil {
		t.Fatal(err)
	}
	definition, err := library.Create(MapDefinition{Name: "Forest", Width: 20, Height: 20}, []byte("image"))
	if err != nil {
		t.Fatal(err)
	}
	path, _ := library.ImagePath(definition.ID)
	if err := os.Rename(filepath.Join(dir, "maps.json"), filepath.Join(dir, "original.json")); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(dir, "maps.json"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := library.Delete(definition.ID); err == nil {
		t.Fatal("expected save failure")
	}
	if _, ok := library.MapByID(definition.ID); !ok {
		t.Fatal("failed deletion removed entry")
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatal("failed deletion removed artwork")
	}
}
