package handlers

import (
	"html/template"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"tableTop/main/internal/app"
)

func TestIndexRendersSpawnControls(t *testing.T) {
	t.Chdir("../..")
	state := app.NewState()
	handler := NewEndpointHandler(state, NewWebSocketHandler(state), newTestLibrary(t))
	mux := http.NewServeMux()
	handler.RegisterRoutes(mux)
	response := httptest.NewRecorder()
	mux.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/", nil))
	if response.Code != http.StatusOK {
		t.Fatalf("index status = %d, want 200", response.Code)
	}
	if response.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("index must prevent caching outdated controls")
	}
	body := response.Body.String()
	if strings.Contains(body, "{{") || strings.Contains(body, "}}") {
		t.Fatal("index contains unrendered template instructions")
	}
	if !strings.Contains(body, "Spawn unit") || strings.Contains(body, "Create new unit") || strings.Contains(body, `id="pc-list"`) {
		t.Fatal("initial page must show only the Spawn unit entry")
	}
}

func newTestLibrary(t *testing.T) *app.UnitLibrary {
	t.Helper()
	library, err := app.NewUnitLibrary(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return library
}

func TestMenuRenderFailureDoesNotReturnPartialForm(t *testing.T) {
	handler := &EndpointHandler{
		indexTemplate: template.Must(template.New("spawn-controls").Parse(`<form id="create-unit-form"><input value="{{ .MissingField }}"></form>`)),
	}
	response := httptest.NewRecorder()
	handler.renderSpawnMenu(response, spawnMenuData{Open: true, Creating: true})
	if response.Code != http.StatusInternalServerError {
		t.Fatalf("render failure status = %d, want 500", response.Code)
	}
	if strings.Contains(response.Body.String(), "<form") || strings.Contains(response.Body.String(), "<input") {
		t.Fatal("render failure returned a partial form")
	}
	if !strings.Contains(response.Body.String(), "Restart the server") {
		t.Fatal("render failure should explain how to recover")
	}
}
