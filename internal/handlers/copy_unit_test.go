package handlers

import (
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"tableTop/main/internal/app"
	"tableTop/main/internal/plugins"
)

func defaultUnitHandler(t *testing.T) (*http.ServeMux, *app.UnitLibrary, app.UnitDefinition) {
	t.Helper()
	dir := t.TempDir()
	if err := app.InitializeOwned(dir, "test", nil, nil); err != nil {
		t.Fatal(err)
	}
	library, err := app.NewUnitLibrary(dir)
	if err != nil {
		t.Fatal(err)
	}
	packs := plugins.Discover(plugins.Directories(testAssetRoot(t)), plugins.Bounds{Metadata: 1 << 20, Content: 16 << 20})
	var defaults *plugins.Pack
	for _, p := range packs {
		if p.ID == "builtin" && p.Version == 1 {
			copy := p
			defaults = &copy
			break
		}
	}
	if defaults == nil {
		t.Fatal("built-in plugin unavailable")
	}
	library.SetDefaults(defaults)
	source, ok := library.UnitByID(plugins.StableID("builtin", "unit", "knight"))
	if !ok {
		t.Fatal("default knight missing")
	}
	state := app.NewState()
	mux := http.NewServeMux()
	newTestEndpointHandler(t, state, NewWebSocketHandler(state), library).RegisterRoutes(mux)
	return mux, library, source
}
func TestUnitCopyFormKeepsChosenMetadataAndOriginalImage(t *testing.T) {
	mux, library, source := defaultUnitHandler(t)
	form := httptest.NewRecorder()
	mux.ServeHTTP(form, httptest.NewRequest("GET", "/unit-library/new?copy="+source.ID, nil))
	if !strings.Contains(form.Body.String(), "/unit-library/"+source.ID+"/copy") {
		t.Fatal("copy form submits to new-unit route")
	}
	request := libraryRequest(t, "Owned knight", "npc", "large", nil)
	request.URL.Path = "/unit-library/" + source.ID + "/copy"
	response := httptest.NewRecorder()
	mux.ServeHTTP(response, request)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), "Owned knight saved") {
		t.Fatalf("copy response: %d %s", response.Code, response.Body.String())
	}
	for _, unit := range library.Units() {
		if unit.ID == source.ID {
			continue
		}
		if unit.Name == "Owned knight" {
			if unit.ReadOnly || unit.Source == nil || unit.ImageID != source.ImageID || unit.UnitType != "npc" || unit.Size != app.SizeLarge {
				t.Fatal("copy lost ownership, image, or chosen metadata")
			}
			return
		}
	}
	t.Fatal("owned copy missing")
}
func TestUnitCopyUploadStoresReplacementLocally(t *testing.T) {
	mux, library, source := defaultUnitHandler(t)
	request := libraryRequest(t, "Portrait knight", "pc", "medium", testPNG(t, 2, 2))
	request.URL.Path = "/unit-library/" + source.ID + "/copy"
	response := httptest.NewRecorder()
	mux.ServeHTTP(response, request)
	for _, u := range library.Units() {
		if u.Name != "Portrait knight" {
			continue
		}
		if u.Artwork != nil || u.ImageID != u.ID {
			t.Fatal("upload retained shared artwork reference")
		}
		path, ok := library.ImagePath(u.ImageID)
		if !ok {
			t.Fatal("local image path unavailable")
		}
		if _, err := os.Stat(path); err != nil {
			t.Fatal(err)
		}
		return
	}
	t.Fatal("copy with replacement missing")
}
