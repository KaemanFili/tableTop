package handlers

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"tableTop/main/internal/app"
)

func TestUnitImageLifecycle(t *testing.T) {
	// Production resolves templates and static files from the project root.
	t.Chdir("../..")
	state := app.NewState()
	ws := NewWebSocketHandler(state)
	h := NewEndpointHandler(state, ws)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)
	ws.RegisterRoutes(mux)

	spawn := httptest.NewRecorder()
	mux.ServeHTTP(spawn, httptest.NewRequest(http.MethodGet, "/unit", nil))
	if spawn.Code != http.StatusOK || !strings.Contains(spawn.Body.String(), `src="/images/default-unit"`) {
		t.Fatalf("spawn response = %d %s", spawn.Code, spawn.Body.String())
	}
	units := state.UnitsSnapshot()
	if len(units) != 1 {
		t.Fatalf("spawned %d units, want 1", len(units))
	}

	image := httptest.NewRecorder()
	mux.ServeHTTP(image, httptest.NewRequest(http.MethodGet, "/images/default-unit", nil))
	if image.Code != http.StatusOK || !strings.HasPrefix(image.Header().Get("Content-Type"), "image/svg+xml") || !strings.Contains(image.Body.String(), "<svg") {
		t.Fatalf("image response = %d, type %q", image.Code, image.Header().Get("Content-Type"))
	}
	missing := httptest.NewRecorder()
	mux.ServeHTTP(missing, httptest.NewRequest(http.MethodGet, "/images/unknown", nil))
	if missing.Code != http.StatusNotFound {
		t.Fatalf("unknown image status = %d, want 404", missing.Code)
	}

	server := httptest.NewServer(mux)
	defer server.Close()
	conn, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(server.URL, "http")+"/ws/updateServer", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	// Even a client-supplied image ID must not replace the stored artwork.
	if err := conn.WriteJSON(map[string]any{"ID": units[0].ID, "Left": 250, "Top": 300, "ImageID": "unknown"}); err != nil {
		t.Fatal(err)
	}
	if err := conn.SetReadDeadline(time.Now().Add(3 * time.Second)); err != nil {
		t.Fatal(err)
	}
	if _, _, err := conn.ReadMessage(); err != nil {
		t.Fatal(err)
	}
	unit, _ := state.UnitByID(units[0].ID)
	if unit.Left != 250 || unit.Top != 300 || unit.ImageID != app.DefaultUnitImageID {
		t.Fatalf("unit after websocket movement = %+v", unit)
	}

	reload := httptest.NewRecorder()
	mux.ServeHTTP(reload, httptest.NewRequest(http.MethodGet, "/loadExistingUnits", nil))
	if reload.Code != http.StatusOK || !strings.Contains(reload.Body.String(), `src="/images/default-unit"`) || !strings.Contains(reload.Body.String(), "left: 250px; top: 300px;") {
		t.Fatalf("reload response = %d %s", reload.Code, reload.Body.String())
	}
}
