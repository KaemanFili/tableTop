package handlers

import (
	"net/http"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"tableTop/main/internal/app/model"
)

func TestFogGMEditsSharedOrderAndInitialState(t *testing.T) {
	state := model.NewState()
	token := state.AddRandomUnit(model.DefaultUnitImageID)
	handler := NewWebSocketHandler(state)
	mux := http.NewServeMux()
	handler.RegisterRoutes(mux)
	server := newTestServer(t, mux)
	connect := func(query string) (*websocket.Conn, string) {
		t.Helper()
		conn, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(server.URL, "http")+"/ws/updateUnitsForClients"+query, nil)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { conn.Close() })
		return conn, readRulerSession(t, conn)
	}
	read := func(conn *websocket.Conn, target any) {
		t.Helper()
		conn.SetReadDeadline(time.Now().Add(3 * time.Second))
		if err := conn.ReadJSON(target); err != nil {
			t.Fatal(err)
		}
	}
	gm, gmID := connect("?gm=true")
	player, _ := connect("")
	mark := fogMark{Type: "fogMark", ID: "1", Owner: "forged", Mode: "paint", Width: 2, Points: []rulerPoint{{X: 5, Y: 5}, {X: 10, Y: 5}}}
	if err := player.WriteJSON(mark); err != nil {
		t.Fatal(err)
	}
	for _, invalid := range []string{
		`{"type":"fogMark","id":"1","mode":"clear","width":2,"points":[{"x":1,"y":1}]}`,
		`{"type":"fogMark","id":"1","mode":"paint","width":7,"points":[{"x":1,"y":1}]}`,
		`{"type":"fogMark","id":"1","mode":"paint","width":0,"points":[{"x":1,"y":1}]}`,
		`{"type":"fogMark","id":"1","mode":"paint","width":2,"points":[{"x":65,"y":1}]}`,
		`{"type":"fogMark","id":"1","mode":"paint","width":2,"points":[]}`,
		`{"type":"fogMark","id":"bad","mode":"paint","width":2,"points":[{"x":1,"y":1}]}`,
	} {
		if err := gm.WriteMessage(websocket.TextMessage, []byte(invalid)); err != nil {
			t.Fatal(err)
		}
	}
	// Player's following message confirms its forbidden edit has been handled.
	if err := player.WriteJSON(boardRuler{Type: "rulerChanged", Clear: true}); err != nil {
		t.Fatal(err)
	}
	for _, conn := range []*websocket.Conn{gm, player} {
		var barrier boardRuler
		read(conn, &barrier)
		if barrier.Type != "rulerChanged" {
			t.Fatal("player changed fog")
		}
	}
	var expected []fogMark
	for i, mode := range []string{"paint", "erase", "paint"} {
		mark.ID, mark.Mode = []string{"1", "2", "3"}[i], mode
		if mode == "erase" {
			mark.Width, mark.Points = 1, []rulerPoint{{X: 7.5, Y: 5}}
		}
		if err := gm.WriteJSON(mark); err != nil {
			t.Fatal(err)
		}
		want := mark
		want.ID, want.Owner = gmID+"/"+mark.ID, gmID
		expected = append(expected, want)
		for _, conn := range []*websocket.Conn{gm, player} {
			var actual fogMark
			read(conn, &actual)
			if !reflect.DeepEqual(actual, want) {
				t.Fatalf("fog mark = %+v, want %+v", actual, want)
			}
		}
	}
	gm.Close()
	joined, _ := connect("")
	var reset struct{ Type string }
	read(joined, &reset)
	if reset.Type != "fogReset" {
		t.Fatal("missing fog reset before initial state")
	}
	for _, want := range expected {
		var actual fogMark
		read(joined, &actual)
		if !reflect.DeepEqual(actual, want) {
			t.Fatalf("initial fog mark = %+v, want %+v", actual, want)
		}
	}
	if got, _ := state.UnitByID(token.ID); got != token {
		t.Fatal("fog changed token")
	}
}
