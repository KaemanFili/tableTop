package handlers

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"tableTop/main/internal/app/model"
	"tableTop/main/internal/app/persistence"
)

func TestUploadFormsExplainStorageLimits(t *testing.T) {
	root := t.TempDir()
	limits, err := persistence.NewStorageLimits(root, 1024, 64)
	if err != nil {
		t.Fatal(err)
	}
	units, err := persistence.NewUnitLibrary(root, limits)
	if err != nil {
		t.Fatal(err)
	}
	maps, err := persistence.NewMapLibrary(root, limits)
	if err != nil {
		t.Fatal(err)
	}
	state := model.NewState()
	handler := newTestEndpointHandler(t, state, NewWebSocketHandler(state), units, maps)
	mux := http.NewServeMux()
	handler.RegisterRoutes(mux)
	for _, path := range []string{"/unit-library", "/map-library?gm=true"} {
		request := libraryRequest(t, "Scout", "npc", "medium", testPNG(t, 2, 2), map[string]string{"width": "20", "height": "20", "offsetX": "0", "offsetY": "0"})
		request.URL, _ = url.Parse(path)
		response := httptest.NewRecorder()
		mux.ServeHTTP(response, request)
		if response.Code != 200 || !strings.Contains(response.Body.String(), "storage is full") || !strings.Contains(response.Body.String(), `role="alert"`) {
			t.Fatalf("%s does not explain quota failure: %d %s", path, response.Code, response.Body.String())
		}
	}
	if len(units.Units()) != 2 || len(maps.Maps()) != 0 || state.UnitCount() != 0 {
		t.Fatal("rejected upload changed the library or board")
	}
}

func TestSavedBoardStorageFailurePreservesPreviousSave(t *testing.T) {
	root := t.TempDir()
	maps, err := persistence.NewMapLibrary(root)
	if err != nil {
		t.Fatal(err)
	}
	definition, err := maps.Create(model.MapDefinition{Name: "Forest", Width: 20, Height: 20}, testPNG(t, 2, 2))
	if err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(filepath.Join(root, "maps.json"))
	image, _ := os.ReadFile(filepath.Join(root, "maps", definition.ID+".png"))
	used := int64(len(before) + len(image))
	limits, err := persistence.NewStorageLimits(root, used, used)
	if err != nil {
		t.Fatal(err)
	}
	maps, err = persistence.NewMapLibrary(root, limits)
	if err != nil {
		t.Fatal(err)
	}
	units, err := persistence.NewUnitLibrary(root, limits)
	if err != nil {
		t.Fatal(err)
	}
	state := model.NewState()
	state.AddRandomUnit(model.PCUnitImageID)
	ws := NewWebSocketHandler(state)
	ws.currentMap = &definition
	handler := newTestEndpointHandler(t, state, ws, units, maps)
	mux := http.NewServeMux()
	handler.RegisterRoutes(mux)
	response := httptest.NewRecorder()
	mux.ServeHTTP(response, httptest.NewRequest("POST", "/board/save?gm=true", nil))
	if response.Code != http.StatusInsufficientStorage || !strings.Contains(response.Body.String(), "storage is full") {
		t.Fatalf("save response: %d %s", response.Code, response.Body.String())
	}
	after, _ := os.ReadFile(filepath.Join(root, "maps.json"))
	if !bytes.Equal(before, after) || state.UnitCount() != 1 {
		t.Fatal("failed save changed the saved or live board")
	}
}
