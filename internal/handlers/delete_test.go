package handlers

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"tableTop/main/internal/app"
	"testing"
)

func TestPermanentLibraryDeletion(t *testing.T) {
	library := newTestLibrary(t)
	maps, err := app.NewMapLibrary(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	definition, err := maps.Create(app.MapDefinition{Name: "Forest", Width: 20, Height: 20}, testPNG(t, 4, 2))
	if err != nil {
		t.Fatal(err)
	}
	state := app.NewState()
	token := state.AddRandomUnit(app.PCUnitImageID)
	ws := NewWebSocketHandler(state)
	ws.currentMap = &definition
	handler := newTestEndpointHandler(t, state, ws, library, maps)
	mux := http.NewServeMux()
	handler.RegisterRoutes(mux)
	request := func(method, path string, want int) string {
		t.Helper()
		response := httptest.NewRecorder()
		mux.ServeHTTP(response, httptest.NewRequest(method, path, nil))
		if response.Code != want {
			t.Fatalf("%s: %d: %s", path, response.Code, response.Body.String())
		}
		return response.Body.String()
	}
	menu := request(http.MethodGet, "/spawn-menu?open=true", 200)
	if !strings.Contains(menu, `hx-delete="/unit-library/knight"`) || !strings.Contains(menu, "hx-confirm=") {
		t.Fatal("unit delete control missing")
	}
	request(http.MethodDelete, "/unit-library/knight", 200)
	request(http.MethodPost, "/unit?unitID=knight", 404)
	if _, ok := state.UnitByID(token.ID); !ok {
		t.Fatal("library deletion removed placed unit")
	}
	picker := request(http.MethodGet, "/map-library?gm=true", 200)
	if !strings.Contains(picker, `hx-delete="/map-library/`+definition.ID+`?gm=true"`) {
		t.Fatal("map delete control missing")
	}
	request(http.MethodDelete, "/map-library/"+definition.ID, http.StatusForbidden)
	if _, ok := maps.MapByID(definition.ID); !ok {
		t.Fatal("non-GM deleted map")
	}
	request(http.MethodDelete, "/map-library/"+definition.ID+"?gm=true", 200)
	if ws.currentMap != nil {
		t.Fatal("deleted map remains active")
	}
	if _, ok := state.UnitByID(token.ID); !ok {
		t.Fatal("map deletion removed placed unit")
	}
	request(http.MethodGet, "/maps/images/"+definition.ID, 404)
	request(http.MethodPost, "/map-library/"+definition.ID+"/load?gm=true", 404)
	request(http.MethodPost, "/board/save?gm=true", http.StatusConflict)
	request(http.MethodDelete, "/map-library/"+definition.ID+"?gm=true", 200)
}
