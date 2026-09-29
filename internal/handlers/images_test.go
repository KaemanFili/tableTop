package handlers

import (
	"image/png"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"tableTop/main/internal/app"
)

func TestSpawnUnitImageChoice(t *testing.T) {
	t.Chdir("../..")
	for _, test := range []struct {
		unitType string
		imageID  string
	}{
		{unitType: "npc", imageID: app.NPCUnitImageID},
		{unitType: "pc", imageID: app.PCUnitImageID},
	} {
		t.Run(test.unitType, func(t *testing.T) {
			state := app.NewState()
			h := NewEndpointHandler(state, NewWebSocketHandler(state), newTestLibrary(t))
			mux := http.NewServeMux()
			h.RegisterRoutes(mux)
			spawn := httptest.NewRecorder()
			mux.ServeHTTP(spawn, httptest.NewRequest(http.MethodGet, "/unit?unitType="+test.unitType, nil))
			if spawn.Code != http.StatusOK || !strings.Contains(spawn.Body.String(), `src="/images/`+test.imageID+`"`) {
				t.Fatalf("spawn response = %d %s", spawn.Code, spawn.Body.String())
			}
			if spawn.Header().Get("HX-Trigger-After-Swap") != "unitSpawned" {
				t.Fatal("spawn must tell HTMX to close the menu")
			}
			units := state.UnitsSnapshot()
			if len(units) != 1 || units[0].ImageID != test.imageID {
				t.Fatalf("unexpected units: %+v", units)
			}
			state.UpdateUnitPosition(units[0].ID, 250, 300)
			unit, _ := state.UnitByID(units[0].ID)
			if unit.ImageID != test.imageID {
				t.Fatal("movement changed the selected artwork")
			}
			image := httptest.NewRecorder()
			mux.ServeHTTP(image, httptest.NewRequest(http.MethodGet, "/images/"+test.imageID, nil))
			if image.Code != http.StatusOK || image.Header().Get("Content-Type") != "image/png" {
				t.Fatalf("image response = %d %q", image.Code, image.Header().Get("Content-Type"))
			}
			if _, err := png.DecodeConfig(image.Body); err != nil {
				t.Fatalf("invalid PNG artwork: %v", err)
			}
			invalid := httptest.NewRecorder()
			mux.ServeHTTP(invalid, httptest.NewRequest(http.MethodGet, "/unit?unitType=unknown", nil))
			if invalid.Code != http.StatusBadRequest || state.UnitCount() != 1 {
				t.Fatal("invalid unit type must be rejected without spawning a unit")
			}
		})
	}
}

func TestUnitImageLifecycle(t *testing.T) {
	// Production resolves templates and static files from the project root.
	t.Chdir("../..")
	state := app.NewState()
	ws := NewWebSocketHandler(state)
	h := NewEndpointHandler(state, ws, newTestLibrary(t))
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
