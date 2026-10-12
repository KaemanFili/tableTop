package plugins

import (
	"bytes"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"strings"
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
	dir := filepath.Join(root, "forest")
	if err := os.Mkdir(dir, 0755); err != nil {
		t.Fatal(err)
	}
	var data bytes.Buffer
	if err := png.Encode(&data, image.NewRGBA(image.Rect(0, 0, 2, 2))); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "forest.png"), data.Bytes(), 0600); err != nil {
		t.Fatal(err)
	}
	manifest := Manifest{SchemaVersion: 1, ID: "forest", Version: 1, Name: "Forest", Maps: "maps.json", Units: "units.json", Statuses: "statuses.json"}
	f := packFixture{root, dir, manifest}
	f.write(t, "plugin.json", manifest)
	f.write(t, "statuses.json", []app.UnitStatusOption{{Value: "Marked", Color: "#123456"}})
	f.write(t, "units.json", []unitEntry{{Key: "scout", Image: "forest.png", UnitDefinition: app.UnitDefinition{Name: "Scout", UnitType: "npc"}}})
	f.write(t, "maps.json", []mapEntry{{Key: "clearing", Image: "forest.png", MapDefinition: app.MapDefinition{Name: "Clearing", Width: 20, Height: 20, Board: &app.SavedBoard{Units: []app.UnitData{{Name: "Scout", UnitType: "npc", ImageID: "scout", Status: "Marked"}}}}}})
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

func TestPluginImportsDefaultsAndResolvesSavedImages(t *testing.T) {
	f := newPackFixture(t)
	p := f.pack(t)
	root := t.TempDir()
	id := rand.Text()
	if err := p.Stage(root, id, nil); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(root, "sessions", id)
	for _, initialize := range []func(string, string, app.LibraryStore, *app.StorageLimits) error{InitializeMaps, InitializeStatuses, InitializeUnits} {
		if err := initialize(dir, id, nil, nil); err != nil {
			t.Fatal(err)
		}
	}
	maps, err := app.NewMapLibrary(dir)
	if err != nil {
		t.Fatal(err)
	}
	list := maps.Maps()
	if len(list) != 1 || list[0].Board != nil || !list[0].HasSavedBoard {
		t.Fatalf("invalid metadata listing: %#v", list)
	}
	m, err := maps.LoadMap(StableID("forest", "map", "clearing"))
	if err != nil {
		t.Fatal(err)
	}
	if m.Board.Units[0].ImageID != StableID("forest", "unit", "scout") || m.Board.Units[0].Status != "Marked" {
		t.Fatal("saved token references were lost")
	}
	units, err := app.NewUnitLibrary(dir)
	if err != nil {
		t.Fatal(err)
	}
	u, ok := units.UnitByID(StableID("forest", "unit", "scout"))
	if !ok || u.Source.PluginID != "forest" || u.Size != app.SizeMedium {
		t.Fatal("unit defaults/provenance were lost")
	}
}
func TestPluginSourceRemovalDoesNotPreventInitialization(t *testing.T) {
	f := newPackFixture(t)
	p := f.pack(t)
	root := t.TempDir()
	id := rand.Text()
	if err := p.Stage(root, id, nil); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(f.dir); err != nil {
		t.Fatal(err)
	}
	if err := InitializeMaps(filepath.Join(root, "sessions", id), id, nil, nil); err != nil {
		t.Fatal(err)
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
			if err := f.pack(t).validate(); err == nil {
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
	if err := p.validate(); err == nil {
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
	if err := f.pack(t).validate(); err != nil {
		t.Fatal(err)
	}
}
func TestOversizedPluginDocumentRejected(t *testing.T) {
	f := newPackFixture(t)
	f.write(t, "maps.json", []mapEntry{{Key: "large", Image: "forest.png", MapDefinition: app.MapDefinition{Name: "Large", Width: 20, Height: 20, Board: &app.SavedBoard{Strokes: []app.PenStroke{{Type: "penStroke", Color: "#123456", Width: 2, Points: []app.BoardPoint{{X: 1, Y: 1}}, Owner: strings.Repeat("x", 16<<20)}}}}}})
	p := f.pack(t)
	p.bounds.Content = 32 << 20
	if err := p.validate(); err == nil {
		t.Fatal("oversized document accepted")
	}
}
func TestFailedPluginStagingCleansUp(t *testing.T) {
	f := newPackFixture(t)
	root := t.TempDir()
	limits, err := app.NewStorageLimits(root, 100, 100)
	if err != nil {
		t.Fatal(err)
	}
	id := rand.Text()
	if err := f.pack(t).Stage(root, id, limits); err == nil {
		t.Fatal("quota ignored")
	}
	if _, err := os.Stat(filepath.Join(root, "sessions", id)); !os.IsNotExist(err) {
		t.Fatal("failed preparation retained files")
	}
}
func TestPluginIDsRemainStableAcrossVersions(t *testing.T) {
	f := newPackFixture(t)
	first, err := f.pack(t).normalizeUnit(unitEntry{Key: "scout", UnitDefinition: app.UnitDefinition{Name: "Scout", UnitType: "npc"}})
	if err != nil {
		t.Fatal(err)
	}
	p := f.pack(t)
	p.Version = 2
	second, err := p.normalizeUnit(unitEntry{Key: "scout", UnitDefinition: app.UnitDefinition{Name: "Renamed", UnitType: "npc"}})
	if err != nil {
		t.Fatal(err)
	}
	if first.ID != second.ID || first.Source.Version == second.Source.Version {
		t.Fatal("identity follows editable fields or release version")
	}
	if first.ID == StableID("other", "unit", "scout") || first.ID == StableID("forest", "map", "scout") {
		t.Fatal("plugin/kind identity collision")
	}
}
