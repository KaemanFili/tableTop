package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"tableTop/main/internal/app/model"
)

func TestRemoveUnitUpdatesStateAndConnectedClients(t *testing.T) {
	state := model.NewState()
	ws := NewWebSocketHandler(state)
	library := newTestLibrary(t)
	handler := newTestEndpointHandler(t, state, ws, library)
	mux := http.NewServeMux()
	handler.RegisterRoutes(mux)
	ws.RegisterRoutes(mux)
	definition, _ := library.UnitByID("goblin")
	removed := state.AddUnit(definition)
	other := state.AddUnit(definition)
	server := newTestServer(t, mux)

	// Both browsers must receive the removed placed-token ID.
	var clients []*websocket.Conn
	for range 2 {
		conn, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(server.URL, "http")+"/ws/updateUnitsForClients", nil)
		if err != nil {
			t.Fatal(err)
		}
		defer conn.Close()
		readRulerSession(t, conn)
		clients = append(clients, conn)
	}
	response := httptest.NewRecorder()
	mux.ServeHTTP(response, httptest.NewRequest(http.MethodDelete, "/unit/"+removed.ID, nil))
	if response.Code != http.StatusNoContent || response.Body.Len() != 0 || response.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("remove response = %d %s", response.Code, response.Body.String())
	}
	var trigger map[string]struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal([]byte(response.Header().Get("HX-Trigger")), &trigger); err != nil || trigger["unitRemoved"].ID != removed.ID {
		t.Fatalf("initiating browser did not receive removal: %v %v", trigger, err)
	}
	for _, conn := range clients {
		conn.SetReadDeadline(time.Now().Add(3 * time.Second))
		var message struct {
			Type string `json:"type"`
			ID   string `json:"id"`
		}
		if err := conn.ReadJSON(&message); err != nil || message.Type != "unitRemoved" || message.ID != removed.ID {
			t.Fatalf("connected browser removal = %+v %v", message, err)
		}
	}
	if _, exists := state.UnitByID(removed.ID); exists || state.UpdateUnitPosition(removed.ID, 10, 20) {
		t.Fatal("removed token remains in server state or accepts late movement")
	}
	if unit, exists := state.UnitByID(other.ID); !exists || unit != other {
		t.Fatal("removal changed another copy")
	}
	if entry, exists := library.UnitByID("goblin"); !exists || entry != definition {
		t.Fatal("removal changed the unit library")
	}
	board := httptest.NewRecorder()
	mux.ServeHTTP(board, httptest.NewRequest(http.MethodGet, "/loadExistingUnits", nil))
	if strings.Contains(board.Body.String(), `id="`+removed.ID+`"`) || !strings.Contains(board.Body.String(), `id="`+other.ID+`"`) || board.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("board reload retained removed token or lost the other copy")
	}
	image := httptest.NewRecorder()
	mux.ServeHTTP(image, httptest.NewRequest(http.MethodGet, "/images/"+definition.ImageID, nil))
	if image.Code != http.StatusOK {
		t.Fatal("removal deleted shared artwork")
	}
	for _, id := range []string{removed.ID, "missing"} {
		repeated := httptest.NewRecorder()
		mux.ServeHTTP(repeated, httptest.NewRequest(http.MethodDelete, "/unit/"+id, nil))
		if repeated.Code != http.StatusNoContent || state.UnitCount() != 1 {
			t.Fatal("repeated removal must succeed without changing other tokens")
		}
	}
}
