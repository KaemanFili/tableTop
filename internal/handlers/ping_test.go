package handlers

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"tableTop/main/internal/app/model"
)

func TestPingBroadcastsToAllBoardsWithoutChangingTokens(t *testing.T) {
	state := model.NewState()
	token := state.AddRandomUnit(model.DefaultUnitImageID)
	ws := NewWebSocketHandler(state)
	handler := newTestEndpointHandler(t, state, ws, newTestLibrary(t))
	mux := http.NewServeMux()
	handler.RegisterRoutes(mux)
	ws.RegisterRoutes(mux)
	server := newTestServer(t, mux)
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
	request := func(body string, want int) {
		t.Helper()
		req := httptest.NewRequest("POST", "/board/ping", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		res := httptest.NewRecorder()
		mux.ServeHTTP(res, req)
		if res.Code != want {
			t.Fatalf("ping = %d %s, want %d", res.Code, res.Body.String(), want)
		}
		if res.Header().Get("Cache-Control") != "no-store" {
			t.Fatal("ping response can be cached")
		}
	}
	for _, body := range []string{`{}`, `{"x":1}`, `{"x":null,"y":2}`, `{"x":-1,"y":2}`, `{"x":1000001,"y":2}`, `{"x":1.5,"y":2}`, `{"x":"bad","y":2}`, `{"x":1,"y":2,"extra":3}`, `{"x":1,"y":2} {}`, strings.Repeat("x", 1025)} {
		request(body, http.StatusBadRequest)
	}
	for _, point := range []struct{ X, Y int }{{420, 300}, {0, 0}} {
		body := `{"x":420,"y":300}`
		if point.X == 0 {
			body = `{"x":0,"y":0}`
		}
		request(body, http.StatusNoContent)
		for _, conn := range clients {
			conn.SetReadDeadline(time.Now().Add(3 * time.Second))
			var message struct {
				Type string `json:"type"`
				X    int    `json:"x"`
				Y    int    `json:"y"`
			}
			if err := conn.ReadJSON(&message); err != nil || message.Type != "boardPing" || message.X != point.X || message.Y != point.Y {
				t.Fatalf("incorrect shared ping: %+v %v", message, err)
			}
		}
	}
	if got, _ := state.UnitByID(token.ID); got != token || state.UnitCount() != 1 {
		t.Fatal("ping changed placed tokens")
	}
}
