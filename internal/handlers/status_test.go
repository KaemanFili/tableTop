package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"tableTop/main/internal/app"
)

func TestStatusUpdatesConnectedBoardAndReload(t *testing.T) {
	t.Chdir("../..")
	state := app.NewState()
	library := newTestLibrary(t)
	ws := NewWebSocketHandler(state)
	handler := NewEndpointHandler(state, ws, library)
	mux := http.NewServeMux()
	handler.RegisterRoutes(mux)
	ws.RegisterRoutes(mux)
	definition, _ := library.UnitByID("goblin")
	token, other := state.AddUnit(definition), state.AddUnit(definition)
	server := httptest.NewServer(mux)
	defer server.Close()
	conn, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(server.URL, "http")+"/ws/updateUnitsForClients", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	request := func(id, body string, want int) unitStatusChange {
		t.Helper()
		req := httptest.NewRequest("POST", "/unit/"+id+"/status", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		res := httptest.NewRecorder()
		mux.ServeHTTP(res, req)
		if res.Code != want {
			t.Fatalf("status = %d %s, want %d", res.Code, res.Body.String(), want)
		}
		if want != 204 {
			return unitStatusChange{}
		}
		var trigger map[string]unitStatusChange
		if err := json.Unmarshal([]byte(res.Header().Get("HX-Trigger")), &trigger); err != nil {
			t.Fatal(err)
		}
		change := trigger["unitStatusChanged"]
		conn.SetReadDeadline(time.Now().Add(3 * time.Second))
		var event struct {
			Type string `json:"type"`
			unitStatusChange
		}
		if err := conn.ReadJSON(&event); err != nil || event.Type != "unitStatusChanged" || event.unitStatusChange != change {
			t.Fatalf("wrong board update: %+v %v", event, err)
		}
		return change
	}
	change := request(token.ID, "status=Poisoned", 204)
	if change.ID != token.ID || change.Status != "Poisoned" || change.Color != "#84cc16" {
		t.Fatalf("wrong status: %+v", change)
	}
	state.UpdateUnitPosition(token.ID, 200, 250)
	state.ResizeUnit(token.ID, "up")
	board := httptest.NewRecorder()
	mux.ServeHTTP(board, httptest.NewRequest("GET", "/loadExistingUnits", nil))
	for _, expected := range []string{`data-status="Poisoned"`, `--unit-status-color: #84cc16`, `Goblin&#10;Size: Medium&#10;Status: Poisoned`} {
		if !strings.Contains(board.Body.String(), expected) {
			t.Fatalf("reload missing %q: %s", expected, board.Body.String())
		}
	}
	request(token.ID, "status=Dead", 204)
	request(token.ID, "status=made-up", 400)
	request(token.ID, "", 400)
	request("missing", "status=Dead", 404)
	got, _ := state.UnitByID(token.ID)
	if got.Status != "Dead" || got.Left != 200 || got.Size != app.SizeMedium || got.ImageFit != token.ImageFit {
		t.Fatalf("lost token data: %+v", got)
	}
	change = request(token.ID, "status=", 204)
	if change.Status != "" || change.Color != "transparent" {
		t.Fatalf("clear failed: %+v", change)
	}
	if got, _ := state.UnitByID(other.ID); got != other {
		t.Fatal("changed another token")
	}
	if saved, _ := library.UnitByID("goblin"); saved != definition {
		t.Fatal("changed library definition")
	}
}
