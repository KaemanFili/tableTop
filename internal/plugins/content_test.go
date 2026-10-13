package plugins

import (
	"bytes"
	"encoding/json"
	"fmt"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"testing"

	"tableTop/main/internal/app"
)

type packFixture struct {
	root, dir string
	manifest  Manifest
}

func newPackFixture(t *testing.T) packFixture {
	t.Helper()
	root := t.TempDir()
	dir := filepath.Join(root, "forest", "1")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	var data bytes.Buffer
	if err := png.Encode(&data, image.NewRGBA(image.Rect(0, 0, 2, 2))); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "forest.png"), data.Bytes(), 0600); err != nil {
		t.Fatal(err)
	}
	manifest := Manifest{SchemaVersion: 2, ID: "forest", Version: 1, Name: "Forest", Maps: "maps.json", Units: "units.json", Statuses: "statuses.json"}
	f := packFixture{root, dir, manifest}
	f.write(t, "plugin.json", manifest)
	f.write(t, "statuses.json", []app.UnitStatusOption{{Value: "Marked", Color: "#123456"}})
	f.write(t, "units.json", []unitEntry{{Key: "scout", Image: "forest.png", UnitDefinition: app.UnitDefinition{Name: "Scout", UnitType: "npc"}}})
	f.write(t, "maps.json", []mapEntry{{Key: "clearing", Image: "forest.png", BoardFile: "board.json", MapDefinition: app.MapDefinition{Name: "Clearing", Width: 20, Height: 20}}})
	f.write(t, "board.json", app.SavedBoard{Units: []app.UnitData{{Name: "Scout", UnitType: "npc", ImageID: "scout", Status: "Marked"}}})
	return f
}
func (f packFixture) write(t *testing.T, name string, value any) {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(f.dir, name), data, 0600); err != nil {
		t.Fatal(err)
	}
}
func (f packFixture) pack(t *testing.T) Pack {
	t.Helper()
	p, err := openPack(os.DirFS(f.dir), f.dir, Bounds{1 << 20, 16 << 20})
	if err != nil {
		t.Fatal(err)
	}
	if err := p.load(); err != nil {
		t.Fatal(err)
	}
	return p
}
func findPack(packs []Pack, id string) bool {
	for _, p := range packs {
		if p.ID == id {
			return true
		}
	}
	return false
}

func TestPluginReadsDefaultsAndResolvesSavedImages(t *testing.T) {
	f := newPackFixture(t)
	p := f.pack(t)
	maps := p.Maps()
	if len(maps) != 1 || maps[0].Board != nil || !maps[0].HasSavedBoard || !maps[0].ReadOnly {
		t.Fatal("invalid default metadata")
	}
	m, err := p.LoadMap(StableID("forest", "map", "clearing"))
	if err != nil {
		t.Fatal(err)
	}
	if m.Board.Units[0].ImageID != StableID("forest", "unit", "scout") || m.Board.Units[0].Artwork == nil {
		t.Fatal("token artwork not bound to plugin")
	}
	if len(p.Units()) != 1 || !p.Units()[0].ReadOnly {
		t.Fatal("units are not defaults")
	}
}
func TestRemovedPluginCannotBeRead(t *testing.T) {
	f := newPackFixture(t)
	p := f.pack(t)
	if err := os.RemoveAll(f.dir); err != nil {
		t.Fatal(err)
	}
	if err := p.Validate(); err == nil {
		t.Fatal("removed source remained usable")
	}
}
func TestPluginDiscoveryRefreshes(t *testing.T) {
	f := newPackFixture(t)
	bounds := Bounds{1 << 20, 16 << 20}
	if !findPack(Discover([]string{f.root}, bounds), "forest") {
		t.Fatal("valid plugin missing")
	}
	f.manifest.Name = "Updated forest"
	f.write(t, "plugin.json", f.manifest)
	packs := Discover([]string{f.root}, bounds)
	for _, p := range packs {
		if p.ID == "forest" && p.Name == "Updated forest" {
			return
		}
	}
	t.Fatal("discovery retained stale metadata")
}
func TestDuplicatePluginIDsAreUnavailable(t *testing.T) {
	first, second := newPackFixture(t), newPackFixture(t)
	if findPack(Discover([]string{first.root, second.root}, Bounds{1 << 20, 16 << 20}), "forest") {
		t.Fatal("duplicate ID remained selectable")
	}
}
func TestPluginRejectsMalformedContent(t *testing.T) {
	for _, tc := range []struct{ name, file, data string }{
		{"duplicate unit", "units.json", `[{"key":"same","name":"Scout","unitType":"npc"},{"key":"same","name":"Scout","unitType":"npc"}]`},
		{"unknown saved image", "maps.json", `[{"key":"map","name":"Map","image":"forest.png","width":20,"height":20,"board":{"units":[{"imageID":"missing"}]}}]`},
		{"unknown status", "maps.json", `[{"key":"map","name":"Map","image":"forest.png","width":20,"height":20,"board":{"units":[{"imageID":"scout","status":"Unknown"}]}}]`},
		{"invalid statuses", "statuses.json", `[{"value":"Marked","color":"bad"}]`},
		{"trailing content", "maps.json", `[] []`},
		{"damaged image", "forest.png", `not an image`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newPackFixture(t)
			if err := os.WriteFile(filepath.Join(f.dir, tc.file), []byte(tc.data), 0600); err != nil {
				t.Fatal(err)
			}
			if err := validateFixture(f); err == nil {
				t.Fatal("invalid content accepted")
			}
		})
	}
}
func TestPluginRejectsEscapingPaths(t *testing.T) {
	for _, path := range []string{"../outside.png", "/outside.png", "escape.png"} {
		t.Run(path, func(t *testing.T) {
			f := newPackFixture(t)
			if err := os.Symlink(filepath.Join(f.dir, "..", "outside.png"), filepath.Join(f.dir, "escape.png")); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(f.dir, "..", "outside.png"), []byte("outside"), 0600); err != nil {
				t.Fatal(err)
			}
			if _, err := f.pack(t).read(path, 1024); err == nil {
				t.Fatal("escaping path accepted")
			}
		})
	}
}
func TestPluginFileLimitsAreConfigurable(t *testing.T) {
	f := newPackFixture(t)
	p := f.pack(t)
	p.bounds.Content = 1
	if err := p.load(); err == nil {
		t.Fatal("content bound ignored")
	}
	t.Setenv("TABLETOP_PLUGIN_METADATA_LIMIT_BYTES", "2097152")
	t.Setenv("TABLETOP_PLUGIN_CONTENT_LIMIT_BYTES", "33554432")
	bounds, err := BoundsFromEnvironment()
	if err != nil || bounds.Metadata != 2<<20 || bounds.Content != 32<<20 {
		t.Fatalf("configured limits: %+v %v", bounds, err)
	}
}
func TestPluginHasNoMapOrUnitCountCaps(t *testing.T) {
	f := newPackFixture(t)
	units := []unitEntry{}
	maps := []mapEntry{}
	for i := range 600 {
		units = append(units, unitEntry{Key: fmt.Sprintf("unit-%d", i), UnitDefinition: app.UnitDefinition{Name: "Scout", UnitType: "npc"}})
	}
	for i := range 140 {
		maps = append(maps, mapEntry{Key: fmt.Sprintf("map-%d", i), Image: "forest.png", MapDefinition: app.MapDefinition{Name: "Map", Width: 20, Height: 20}})
	}
	f.write(t, "units.json", units)
	f.write(t, "maps.json", maps)
	if err := validateFixture(f); err != nil {
		t.Fatal(err)
	}
}
func TestDefaultBoardLimitEnforced(t *testing.T) {
	f := newPackFixture(t)
	f.write(t, "board.json", app.SavedBoard{Units: make([]app.UnitData, app.MaxBoardUnits+1)})
	if err := validateFixture(f); err == nil {
		t.Fatal("oversized board accepted")
	}
}
func TestPluginIDsRemainStableAcrossVersions(t *testing.T) {
	id := StableID("forest", "unit", "scout")
	if len(id) != 32 || id == StableID("other", "unit", "scout") || id == StableID("forest", "map", "scout") {
		t.Fatal("plugin/kind identity collision")
	}
}
func TestPluginModificationInvalidatesBoundReader(t *testing.T) {
	f := newPackFixture(t)
	p := f.pack(t)
	f.write(t, "statuses.json", []app.UnitStatusOption{{Value: "Changed", Color: "#ABCDEF"}})
	if err := p.Validate(); err == nil {
		t.Fatal("modified folder accepted")
	}
}

func validateFixture(f packFixture) error {
	p, err := openPack(os.DirFS(f.dir), f.dir, Bounds{1 << 20, 16 << 20})
	if err != nil {
		return err
	}
	return p.load()
}

func TestRestoringPluginFilesRecoversCachedReader(t *testing.T) {
	f := newPackFixture(t)
	p := f.pack(t)
	path := filepath.Join(f.dir, "statuses.json")
	original, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	f.write(t, "statuses.json", []app.UnitStatusOption{{Value: "Changed", Color: "#ABCDEF"}})
	if err := p.Validate(); err == nil {
		t.Fatal("changed file accepted")
	}
	if err := os.WriteFile(path, original, 0600); err != nil {
		t.Fatal(err)
	}
	if err := p.Validate(); err != nil {
		t.Fatalf("original version not recovered: %v", err)
	}
}
