package plugins

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"tableTop/main/internal/app"
)

func TestDND5ePluginMatchesCatalog(t *testing.T) {
	dir := filepath.Join("..", "..", "plugins", "dnd-5e", "1")
	data, err := os.ReadFile(filepath.Join(dir, "catalog.json"))
	if err != nil {
		t.Fatal(err)
	}
	var catalog struct {
		Monsters          int `json:"monsters"`
		IndividualArtwork int `json:"individualArtwork"`
		Entries           []struct {
			Key         string `json:"key"`
			DisplayName string `json:"displayName"`
			Size        string `json:"size"`
			Image       string `json:"image"`
		} `json:"entries"`
	}
	if err := json.Unmarshal(data, &catalog); err != nil {
		t.Fatal(err)
	}
	pack, err := openVersion(dir, "dnd-5e", "1", Bounds{Metadata: 1 << 20, Content: 16 << 20})
	if err != nil {
		t.Fatal(err)
	}
	// Loading validates every definition, condition, and local image.
	if err := pack.load(); err != nil {
		t.Fatal(err)
	}
	if len(catalog.Entries) != 3617 || catalog.Monsters != len(catalog.Entries) || len(pack.Units()) != catalog.Monsters {
		t.Fatalf("incomplete catalog: entries=%d declared=%d units=%d", len(catalog.Entries), catalog.Monsters, len(pack.Units()))
	}
	mapKeys := []string{"field", "desert", "city-square", "mountain-region", "village-center", "forest", "river-crossing", "boat", "wagon-road", "ancient-ruins", "rocky-outcropping"}
	if len(pack.Maps()) != len(mapKeys) {
		t.Fatalf("incomplete environment maps: got %d, want %d", len(pack.Maps()), len(mapKeys))
	}
	for _, key := range mapKeys {
		m, err := pack.LoadMap(StableID("dnd-5e", "map", key))
		if err != nil {
			t.Fatalf("cannot load environment map %s: %v", key, err)
		}
		if !m.ReadOnly || m.Artwork == nil || m.Board != nil || m.HasSavedBoard {
			t.Fatalf("invalid default environment map: %s", key)
		}
	}
	names := make(map[string]bool)
	artworkCount := 0
	for _, entry := range catalog.Entries {
		unit, ok := pack.UnitByID(StableID("dnd-5e", "unit", entry.Key))
		if !ok || unit.Name != entry.DisplayName || !unit.ReadOnly || unit.UnitType != "npc" {
			t.Fatalf("catalog entry missing or changed: %s", entry.Key)
		}
		if names[unit.Name] {
			t.Fatalf("ambiguous monster label: %s", unit.Name)
		}
		names[unit.Name] = true
		size := strings.ToLower(entry.Size)
		if size == "medium or small" {
			size = "medium"
		}
		if unit.Size != app.UnitSize(size) {
			t.Fatalf("incorrect monster size: %s", entry.Key)
		}
		if entry.Image == "" {
			if unit.Artwork != nil || unit.ImageID != app.NPCUnitImageID {
				t.Fatalf("missing-artwork fallback changed: %s", entry.Key)
			}
			continue
		}
		artworkCount++
		if unit.Artwork == nil || unit.ImageID != unit.ID || unit.Artwork.PluginID != "dnd-5e" || unit.Artwork.Version != 1 {
			t.Fatalf("individual artwork reference missing: %s", entry.Key)
		}
	}
	if artworkCount != 2760 || artworkCount != catalog.IndividualArtwork {
		t.Fatalf("incomplete artwork: got %d, declared %d", artworkCount, catalog.IndividualArtwork)
	}
	aboleth, ok := pack.UnitByID(StableID("dnd-5e", "unit", "monster-5194867"))
	if !ok || aboleth.Artwork == nil {
		t.Fatal("Aboleth portrait missing")
	}
	if _, err := pack.Image(*aboleth.Artwork); err != nil {
		t.Fatalf("cannot serve monster portrait: %v", err)
	}
}
