package tables

import (
	"encoding/json"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"tableTop/main/internal/app"
	"tableTop/main/internal/plugins"
)

func (f tableTestFixture) installPlugin(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	dir := filepath.Join(root, "forest", "1")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	art, err := os.ReadFile(filepath.Join(f.assets, "static", "images", "goblin.png"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "art.png"), art, 0600); err != nil {
		t.Fatal(err)
	}
	content := map[string]any{
		"plugin.json":   map[string]any{"schemaVersion": 2, "id": "forest", "version": 1, "name": "Forest", "maps": "maps.json", "units": "units.json", "statuses": "statuses.json"},
		"units.json":    []map[string]any{{"key": "scout", "name": "Scout", "unitType": "npc", "image": "art.png"}},
		"maps.json":     []map[string]any{{"key": "clearing", "name": "Clearing", "width": 20, "height": 20, "image": "art.png", "boardFile": "board.json"}},
		"statuses.json": []app.UnitStatusOption{{Value: "Marked", Color: "#123456"}},
		"board.json":    app.SavedBoard{Units: []app.UnitData{{Name: "Scout", UnitType: "npc", ImageID: "scout", Status: "Marked", Left: 100, Top: 150}}},
	}
	for name, value := range content {
		data, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, name), data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	f.manager.pluginDirs = []string{root}
	return dir
}
func (f tableTestFixture) createPluginSession(t *testing.T, owner *http.Client) testSession {
	t.Helper()
	response, _ := f.request(owner, "POST", "/sessions", url.Values{"name": {"Forest"}, "plugin": {"forest/1"}}.Encode(), http.StatusSeeOther)
	path := response.Header.Get("Location")
	return testSession{path, strings.Split(path, "/")[2]}
}
func TestSessionCreationStoresOnlyOwnedEmptyLibraries(t *testing.T) {
	f := newTableTestFixture(t)
	f.installPlugin(t)
	session := f.createPluginSession(t, f.login("owner"))
	dir := filepath.Join(f.dir, "sessions", session.id)
	if _, err := os.Stat(filepath.Join(dir, ".plugin")); !os.IsNotExist(err) {
		t.Fatal("session import staging exists")
	}
	raw, err := os.ReadFile(filepath.Join(dir, "maps.json"))
	if err != nil || string(raw) != "[]\n" {
		t.Fatal("defaults imported into owned maps")
	}
	images, err := os.ReadDir(filepath.Join(dir, "images"))
	if err == nil && len(images) != 0 {
		t.Fatal("default art copied into session")
	}
	table, ok := f.manager.library.ByID(session.id)
	if !ok || table.PluginBinding == nil || table.PluginBinding.PluginID != "forest" {
		t.Fatal("selected version not bound")
	}
}
func TestDefaultControlsAndDeleteRequests(t *testing.T) {
	f := newTableTestFixture(t)
	f.installPlugin(t)
	owner := f.login("owner")
	session := f.createPluginSession(t, owner)
	mapID := plugins.StableID("forest", "map", "clearing")
	unitID := plugins.StableID("forest", "unit", "scout")
	_, html := f.request(owner, "GET", session.path+"map-library", "", http.StatusOK)
	if !strings.Contains(html, "Plugin default") || strings.Contains(html, "hx-delete=") {
		t.Fatal("default map has deletion controls")
	}
	_, html = f.request(owner, "GET", session.path+"spawn-menu?open=true", "", http.StatusOK)
	if !strings.Contains(html, "Create copy") || strings.Contains(html, "hx-delete=") {
		t.Fatal("default unit has deletion controls")
	}
	f.request(owner, "DELETE", session.path+"map-library/"+mapID, "", http.StatusConflict)
	f.request(owner, "DELETE", session.path+"unit-library/"+unitID, "", http.StatusConflict)
}
func TestDefaultMapSaveChangesActiveIDAndPreservesDefault(t *testing.T) {
	f := newTableTestFixture(t)
	f.installPlugin(t)
	owner := f.login("owner")
	session := f.createPluginSession(t, owner)
	id := plugins.StableID("forest", "map", "clearing")
	f.request(owner, "POST", session.path+"map-library/"+id+"/load", "", http.StatusOK)
	response, _ := f.request(owner, "POST", session.path+"board/save", "", http.StatusNoContent)
	copyID := response.Header.Get("X-Map-ID")
	if copyID == "" || copyID == id {
		t.Fatal("save did not activate an owned copy")
	}
	response, _ = f.request(owner, "POST", session.path+"board/save", "", http.StatusNoContent)
	if response.Header.Get("X-Map-ID") != copyID {
		t.Fatal("repeated save created another copy")
	}
	_, picker := f.request(owner, "GET", session.path+"map-library", "", http.StatusOK)
	sections := strings.SplitN(picker, "data-library-defaults", 2)
	if len(sections) != 2 || !strings.Contains(sections[0], "data-library-owned") ||
		!strings.Contains(sections[0], copyID) || strings.Contains(sections[0], id) ||
		!strings.Contains(sections[1], id) || strings.Contains(sections[1], copyID) {
		t.Fatal("owned maps and defaults were not separated into dropdowns")
	}
	f.request(owner, "GET", session.path+"maps/images/"+copyID, "", http.StatusOK)
	f.request(owner, "DELETE", session.path+"map-library/"+copyID, "", http.StatusOK)
	f.request(owner, "POST", session.path+"map-library/"+id+"/load", "", http.StatusOK)
}
func TestMissingPluginBlocksCachedSession(t *testing.T) {
	f := newTableTestFixture(t)
	dir := f.installPlugin(t)
	owner := f.login("owner")
	session := f.createPluginSession(t, owner)
	f.request(owner, "GET", session.path, "", http.StatusOK)
	if err := os.RemoveAll(dir); err != nil {
		t.Fatal(err)
	}
	f.request(owner, "GET", session.path, "", http.StatusInternalServerError)
}
func TestDeletingTabletopDoesNotRemoveInstalledVersion(t *testing.T) {
	f := newTableTestFixture(t)
	dir := f.installPlugin(t)
	owner := f.login("owner")
	session := f.createPluginSession(t, owner)
	f.request(owner, "POST", session.path+"delete", "confirm=delete", http.StatusSeeOther)
	if _, err := os.Stat(filepath.Join(dir, "plugin.json")); err != nil {
		t.Fatal("session deletion removed installed version")
	}
}

func TestOwnedInitializationFailureIsPrivateAndRetryable(t *testing.T) {
	f := newTableTestFixture(t)
	owner, player := f.login("owner"), f.login("player")
	limits, err := app.NewStorageLimits(f.dir, 4096, 10)
	if err != nil {
		t.Fatal(err)
	}
	f.manager.limits = limits
	_, html := f.request(owner, "POST", "/sessions", url.Values{"name": {"Pending"}, "plugin": {"builtin/1"}}.Encode(), http.StatusInternalServerError)
	match := regexp.MustCompile(`/sessions/([A-Za-z0-9_-]+)/initialize`).FindStringSubmatch(html)
	if len(match) != 2 {
		t.Fatal("pending session has no retry action")
	}
	id := match[1]
	f.request(player, "POST", "/sessions/"+id+"/initialize", "", http.StatusForbidden)
	f.request(player, "POST", "/sessions/join", "code="+id, http.StatusBadRequest)
	_, other := f.request(player, "GET", "/lobby", "", http.StatusOK)
	if strings.Contains(other, "Pending") {
		t.Fatal("pending session visible to non-owner")
	}
	f.request(owner, "GET", "/sessions/"+id+"/", "", http.StatusNotFound)
	f.manager.limits, err = app.NewStorageLimits(f.dir, 4096, 1024)
	if err != nil {
		t.Fatal(err)
	}
	f.request(owner, "POST", "/sessions/"+id+"/initialize", "", http.StatusSeeOther)
	table, ok := f.manager.library.ByID(id)
	if !ok || table.Initialization != "" {
		t.Fatal("retry did not finish owned initialization")
	}
	f.request(owner, "GET", "/sessions/"+id+"/", "", http.StatusOK)
}
