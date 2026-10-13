package plugins

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"tableTop/main/internal/app"
)

func boundLibraries(t *testing.T) (packFixture, Pack, *app.MapLibrary, *app.UnitLibrary, string) {
	t.Helper()
	f := newPackFixture(t)
	p := f.pack(t)
	dir := t.TempDir()
	if err := app.InitializeOwned(dir, "test", nil, nil); err != nil {
		t.Fatal(err)
	}
	maps, err := app.NewMapLibrary(dir)
	if err != nil {
		t.Fatal(err)
	}
	units, err := app.NewUnitLibrary(dir)
	if err != nil {
		t.Fatal(err)
	}
	maps.SetDefaults(p)
	units.SetDefaults(p)
	return f, p, maps, units, dir
}
func TestOwnedInitializationWritesNoDefaultContent(t *testing.T) {
	_, p, maps, units, dir := boundLibraries(t)
	if len(maps.Maps()) != 1 || len(units.Units()) != 1 {
		t.Fatal("defaults missing from combined libraries")
	}
	raw, err := os.ReadFile(filepath.Join(dir, "maps.json"))
	if err != nil || string(raw) != "[]\n" {
		t.Fatal("default maps were persisted")
	}
	raw, err = os.ReadFile(filepath.Join(dir, "units.json"))
	if err != nil || string(raw) != "{\"version\":1,\"units\":[]}\n" {
		t.Fatal("default units were persisted")
	}
	if _, err := os.Stat(filepath.Join(dir, ".plugin")); !os.IsNotExist(err) {
		t.Fatal("plugin staging created")
	}
	if len(p.Maps()) != 1 {
		t.Fatal("reader changed")
	}
}
func TestDefaultDeletionIsRejected(t *testing.T) {
	_, p, maps, units, _ := boundLibraries(t)
	if err := maps.Delete(p.Maps()[0].ID); !errors.Is(err, app.ErrReadOnly) {
		t.Fatalf("map delete: %v", err)
	}
	if err := units.Delete(p.Units()[0].ID); !errors.Is(err, app.ErrReadOnly) {
		t.Fatalf("unit delete: %v", err)
	}
}
func TestSaveDefaultCreatesOwnedMapAndReusesArtwork(t *testing.T) {
	_, p, maps, _, dir := boundLibraries(t)
	original := p.Maps()[0]
	board := app.SavedBoard{Units: []app.UnitData{{Name: "Changed", ImageID: app.PCUnitImageID}}}
	copy, err := maps.SaveMap(original, board)
	if err != nil {
		t.Fatal(err)
	}
	if copy.ID == original.ID || copy.ReadOnly || copy.Artwork == nil {
		t.Fatal("save did not create owned reference")
	}
	if _, err := os.Stat(filepath.Join(dir, "maps", copy.ID+".png")); !os.IsNotExist(err) {
		t.Fatal("shared map art copied into session")
	}
	if len(maps.Maps()) != 2 {
		t.Fatal("owned copy replaced default")
	}
	saved, err := maps.LoadMap(copy.ID)
	if err != nil || saved.Board.Units[0].Name != "Changed" {
		t.Fatal("owned board not saved")
	}
	unmodified, err := maps.LoadMap(original.ID)
	if err != nil || unmodified.Board.Units[0].Name != "Scout" {
		t.Fatal("default board was changed")
	}
	copy.Board = nil
	again, err := maps.SaveMap(copy, board)
	if err != nil || again.ID != copy.ID || len(maps.Maps()) != 2 {
		t.Fatalf("subsequent save created another copy: %v", err)
	}
	if err := maps.Delete(copy.ID); err != nil {
		t.Fatal(err)
	}
	if len(maps.Maps()) != 1 {
		t.Fatal("owned delete lost default")
	}
}
func TestCopiedUnitRetainsSharedArtworkAcrossRestart(t *testing.T) {
	_, p, _, units, dir := boundLibraries(t)
	u := p.Units()[0]
	copy, err := units.CreateCopy(u.ID, "Owned scout", "npc", app.SizeLarge, app.DefaultImageFit(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if copy.ReadOnly || copy.ID == u.ID || copy.Artwork == nil || copy.ImageID != u.ImageID {
		t.Fatal("invalid copy ownership/artwork")
	}
	reloaded, err := app.NewUnitLibrary(dir)
	if err != nil {
		t.Fatal(err)
	}
	reloaded.SetDefaults(p)
	got, ok := reloaded.UnitByID(copy.ID)
	if !ok || got.Artwork == nil || got.Name != "Owned scout" {
		t.Fatal("owned copy lost on restart")
	}
	if _, err := reloaded.PluginArtwork(got.ImageID); err != nil {
		t.Fatal(err)
	}
	if err := reloaded.Delete(copy.ID); err != nil {
		t.Fatal(err)
	}
	if len(reloaded.Units()) != 1 {
		t.Fatal("copy deletion removed default")
	}
}
func TestEffectiveStatusesUseDefaultsAndOwnedAdditions(t *testing.T) {
	_, p, _, _, dir := boundLibraries(t)
	if err := app.WriteContentJSON(filepath.Join(dir, "statuses.json"), map[string]any{"schemaVersion": 2, "options": []app.UnitStatusOption{{Value: "Owned", Color: "#ABCDEF"}}}, nil); err != nil {
		t.Fatal(err)
	}
	values, err := app.EffectiveStatuses(dir, "test", nil, nil, p)
	if err != nil || len(values) != 2 || values[0].Value != "Marked" || values[1].Value != "Owned" {
		t.Fatalf("effective statuses: %v", err)
	}
}
func TestOwnedStatusCannotReplaceDefault(t *testing.T) {
	_, p, _, _, dir := boundLibraries(t)
	if err := app.WriteContentJSON(filepath.Join(dir, "statuses.json"), map[string]any{"schemaVersion": 2, "options": []app.UnitStatusOption{{Value: "Marked", Color: "#ABCDEF"}}}, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := app.EffectiveStatuses(dir, "test", nil, nil, p); err == nil {
		t.Fatal("default status replaced")
	}
}
func TestFailedDefaultSaveDoesNotCreateCopy(t *testing.T) {
	f, p, maps, _, _ := boundLibraries(t)
	if err := os.Remove(filepath.Join(f.dir, "forest.png")); err != nil {
		t.Fatal(err)
	}
	if _, err := maps.SaveMap(p.Maps()[0], app.SavedBoard{}); err == nil {
		t.Fatal("missing art allowed save")
	}
	if len(maps.Maps()) != 1 {
		t.Fatal("failed save published copy")
	}
}
func TestVersionFoldersCanCoexist(t *testing.T) {
	f := newPackFixture(t)
	dir := filepath.Join(f.root, "forest", "2")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(f.dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		data, err := os.ReadFile(filepath.Join(f.dir, e.Name()))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, e.Name()), data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	f.dir = dir
	f.manifest.Version = 2
	f.write(t, "plugin.json", f.manifest)
	count := 0
	for _, p := range Discover([]string{f.root}, Bounds{1 << 20, 16 << 20}) {
		if p.ID == "forest" {
			count++
		}
	}
	if count != 2 {
		t.Fatal("versions excluded as duplicate IDs")
	}
}
