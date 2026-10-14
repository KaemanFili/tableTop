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

func TestResizeHTTPAndConnectedBoardsPreserveLibrary(t *testing.T) {
	state := model.NewState()
	library := newTestLibrary(t)
	ws := NewWebSocketHandler(state)
	handler := newTestEndpointHandler(t, state, ws, library)
	mux := http.NewServeMux()
	handler.RegisterRoutes(mux)
	ws.RegisterRoutes(mux)
	definition, _ := library.UnitByID("goblin")
	token, other := state.AddUnit(definition), state.AddUnit(definition)
	server := newTestServer(t, mux)
	var connections []*websocket.Conn
	for range 2 {
		conn, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(server.URL, "http")+"/ws/updateUnitsForClients", nil)
		if err != nil {
			t.Fatal(err)
		}
		defer conn.Close()
		readRulerSession(t, conn)
		connections = append(connections, conn)
	}
	request := func(id, body string, want int) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequest("POST", "/unit/"+id+"/size", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		res := httptest.NewRecorder()
		mux.ServeHTTP(res, req)
		if res.Code != want {
			t.Fatalf("resize = %d %s, want %d", res.Code, res.Body.String(), want)
		}
		return res
	}
	res := request(token.ID, "direction=up", 204)
	var trigger map[string]unitSizeChange
	if err := json.Unmarshal([]byte(res.Header().Get("HX-Trigger")), &trigger); err != nil {
		t.Fatal(err)
	}
	change := trigger["unitSizeChanged"]
	if change.ID != token.ID || change.Size != model.SizeMedium || change.Label != "Medium" || change.Squares != 1 || res.Body.Len() != 0 || res.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("wrong initiating-client response: %+v", change)
	}
	for _, conn := range connections {
		conn.SetReadDeadline(time.Now().Add(3 * time.Second))
		var event struct {
			Type string `json:"type"`
			unitSizeChange
		}
		if err := conn.ReadJSON(&event); err != nil || event.Type != "unitSizeChanged" || event.unitSizeChange != change {
			t.Fatalf("wrong connected-board response: %+v %v", event, err)
		}
	}
	request(token.ID, "direction=up", 204)
	got, _ := state.UnitByID(token.ID)
	if got.Size != model.SizeLarge || got.ImageFit != token.ImageFit || got.Left != token.Left || got.Top != token.Top {
		t.Fatalf("resize lost token data: %+v", got)
	}
	request(token.ID, "direction=sideways", 400)
	request(token.ID, "", 400)
	request("missing", "direction=up", 404)
	// Limit requests succeed without inventing a size outside the category list.
	for range 8 {
		request(token.ID, "direction=up", 204)
	}
	got, _ = state.UnitByID(token.ID)
	if got.Size != model.SizeGargantuan {
		t.Fatal("upper limit did not hold")
	}
	for range 8 {
		request(token.ID, "direction=down", 204)
	}
	got, _ = state.UnitByID(token.ID)
	if got.Size != model.SizeTiny {
		t.Fatal("lower limit did not hold")
	}
	if gotOther, _ := state.UnitByID(other.ID); gotOther != other {
		t.Fatal("resizing changed another copy")
	}
	if saved, _ := library.UnitByID("goblin"); saved != definition {
		t.Fatal("resizing changed saved library unit")
	}
	if spawned := state.AddUnit(definition); spawned.Size != definition.Size {
		t.Fatal("new copy inherited placed token resize")
	}
	request(token.ID, "direction=up", 204)
	board := httptest.NewRecorder()
	mux.ServeHTTP(board, httptest.NewRequest("GET", "/loadExistingUnits", nil))
	if !strings.Contains(board.Body.String(), `data-size="small"`) {
		t.Fatal("reload lost placed size")
	}
	state.RemoveUnit(token.ID)
	request(token.ID, "direction=down", 404)
}
