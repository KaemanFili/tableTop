package handlers

import (
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"tableTop/main/internal/app"
)

func readRulerSession(t *testing.T, conn *websocket.Conn) string {
	t.Helper()
	conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	var message struct {
		Type string `json:"type"`
		ID   string `json:"id"`
	}
	if err := conn.ReadJSON(&message); err != nil || message.Type != "rulerSession" || message.ID == "" {
		t.Fatalf("invalid ruler session: %+v %v", message, err)
	}
	return message.ID
}

func TestSharedRulersLiveUpdatesJoinClearAndDisconnect(t *testing.T) {
	state := app.NewState()
	token := state.AddRandomUnit(app.DefaultUnitImageID)
	ws := NewWebSocketHandler(state)
	mux := http.NewServeMux()
	ws.RegisterRoutes(mux)
	server := httptest.NewServer(mux)
	defer server.Close()
	connect := func() (*websocket.Conn, string) {
		t.Helper()
		conn, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(server.URL, "http")+"/ws/updateUnitsForClients", nil)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { conn.Close() })
		return conn, readRulerSession(t, conn)
	}
	read := func(conn *websocket.Conn, expected boardRuler) {
		t.Helper()
		conn.SetReadDeadline(time.Now().Add(3 * time.Second))
		var actual boardRuler
		if err := conn.ReadJSON(&actual); err != nil || !reflect.DeepEqual(actual, expected) {
			t.Fatalf("ruler = %+v, want %+v, error %v", actual, expected, err)
		}
	}
	first, firstID := connect()
	second, secondID := connect()
	if firstID == secondID {
		t.Fatal("users share ruler identity")
	}
	// Invalid updates must not broadcast; the next message received must be the valid line.
	for _, message := range []string{`{}`, `{"type":"rulerChanged"}`, `{"type":"rulerChanged","start":{"x":-1,"y":0},"end":{"x":1,"y":1}}`, `{"type":"rulerChanged","start":{"x":0,"y":0},"end":{"x":65,"y":1}}`} {
		if err := first.WriteMessage(websocket.TextMessage, []byte(message)); err != nil {
			t.Fatal(err)
		}
	}
	line := boardRuler{Type: "rulerChanged", ID: firstID, Start: &rulerPoint{0, 0}, End: &rulerPoint{3, 4}}
	spoof := line
	spoof.ID = secondID
	if err := first.WriteJSON(spoof); err != nil {
		t.Fatal(err)
	}
	read(first, line)
	read(second, line)
	// New users receive the state of rulers currently displayed.
	third, _ := connect()
	read(third, line)
	line.End = &rulerPoint{6, 8}
	if err := first.WriteJSON(line); err != nil {
		t.Fatal(err)
	}
	for _, conn := range []*websocket.Conn{first, second, third} {
		read(conn, line)
	}
	other := boardRuler{Type: "rulerChanged", ID: secondID, Start: &rulerPoint{2.5, 3}, End: &rulerPoint{4, 5}}
	if err := second.WriteJSON(other); err != nil {
		t.Fatal(err)
	}
	for _, conn := range []*websocket.Conn{first, second, third} {
		read(conn, other)
	}
	clear := boardRuler{Type: "rulerChanged", ID: firstID, Clear: true}
	if err := first.WriteJSON(clear); err != nil {
		t.Fatal(err)
	}
	for _, conn := range []*websocket.Conn{first, second, third} {
		read(conn, clear)
	}
	ws.unitCreatedMutex.Lock()
	if len(ws.rulers) != 1 || ws.rulers[secondID].ID != secondID {
		t.Error("clearing changed another user's ruler")
	}
	ws.unitCreatedMutex.Unlock()
	second.Close()
	read(first, boardRuler{Type: "rulerChanged", ID: secondID, Clear: true})
	read(third, boardRuler{Type: "rulerChanged", ID: secondID, Clear: true})
	if got, _ := state.UnitByID(token.ID); got != token {
		t.Fatal("ruler changed a token")
	}
}
