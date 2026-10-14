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

func TestClearBoardSharedStateAndDelayedUpdates(t *testing.T) {
	state := model.NewState()
	firstToken := state.AddRandomUnit(model.DefaultUnitImageID)
	secondToken := state.AddRandomUnit(model.PCUnitImageID)
	ws := NewWebSocketHandler(state)
	library := newTestLibrary(t)
	handler := newTestEndpointHandler(t, state, ws, library)
	mux := http.NewServeMux()
	handler.RegisterRoutes(mux)
	ws.RegisterRoutes(mux)
	server := newTestServer(t, mux)
	read := func(conn *websocket.Conn, target any) {
		t.Helper()
		conn.SetReadDeadline(time.Now().Add(3 * time.Second))
		if err := conn.ReadJSON(target); err != nil {
			t.Fatal(err)
		}
	}
	connect := func(query string, generation uint64) (*websocket.Conn, string) {
		t.Helper()
		conn, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(server.URL, "http")+"/ws/updateUnitsForClients"+query, nil)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { conn.Close() })
		var session struct {
			Type, ID   string
			Generation uint64
		}
		read(conn, &session)
		if session.Type != "rulerSession" || session.Generation != generation {
			t.Fatalf("incorrect initial session: %+v", session)
		}
		return conn, session.ID
	}
	gm, owner := connect("?gm=true", 0)
	player, _ := connect("", 0)
	ws.boardMutex.Lock()
	ws.strokes[owner+"/1"] = penStroke{Type: "penStroke", ID: owner + "/1", Owner: owner, Points: []rulerPoint{{X: 1, Y: 1}}}
	ws.erasedStrokes[owner+"/2"] = true
	ws.rulers[owner] = boardRuler{Type: "rulerChanged", ID: owner, Start: &rulerPoint{X: 1, Y: 1}, End: &rulerPoint{X: 2, Y: 2}}
	ws.fogMarks = []fogMark{{Type: "fogMark", ID: owner + "/1", Mode: "paint", Width: 2, Points: []rulerPoint{{X: 1, Y: 1}}}}
	ws.boardMutex.Unlock()
	request := func(path string, want int) {
		t.Helper()
		response := httptest.NewRecorder()
		mux.ServeHTTP(response, httptest.NewRequest(http.MethodPost, path, nil))
		if response.Code != want {
			t.Fatalf("%s: status = %d, want %d", path, response.Code, want)
		}
	}
	request("/board/clear", http.StatusForbidden)
	if state.UnitCount() != 2 {
		t.Fatal("non-GM cleared tokens")
	}
	request("/board/clear?gm=true", http.StatusNoContent)
	for _, conn := range []*websocket.Conn{gm, player} {
		var message struct {
			Type       string
			IDs        []string
			Generation uint64
		}
		read(conn, &message)
		ids := make(map[string]bool)
		for _, id := range message.IDs {
			ids[id] = true
		}
		if message.Type != "boardCleared" || message.Generation != 1 || len(ids) != 2 || !ids[firstToken.ID] || !ids[secondToken.ID] {
			t.Fatalf("incorrect clear event: %+v", message)
		}
	}
	ws.boardMutex.Lock()
	if len(ws.strokes)+len(ws.erasedStrokes)+len(ws.rulers)+len(ws.fogMarks) != 0 {
		t.Error("clear left utility state behind")
	}
	ws.boardMutex.Unlock()
	if state.UnitCount() != 0 || state.UpdateUnitPosition(firstToken.ID, 100, 100) {
		t.Fatal("clear left tokens behind")
	}
	if _, exists := library.UnitByID("knight"); !exists {
		t.Fatal("clear removed library units")
	}
	newToken := state.AddRandomUnit(model.PCUnitImageID)
	if newToken.ID == firstToken.ID || newToken.ID == secondToken.ID {
		t.Fatal("clear reused token IDs")
	}
	joined, _ := connect("", 1)
	for _, old := range []string{
		`{"type":"penStroke","id":"3","points":[{"x":1,"y":1}]}`,
		`{"type":"fogMark","id":"3","mode":"paint","width":2,"points":[{"x":1,"y":1}]}`,
		`{"type":"rulerChanged","start":{"x":1,"y":1},"end":{"x":2,"y":2}}`,
	} {
		if err := gm.WriteMessage(websocket.TextMessage, []byte(old)); err != nil {
			t.Fatal(err)
		}
	}
	response := httptest.NewRecorder()
	mux.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/board/ping", strings.NewReader(`{"x":1,"y":1,"generation":0}`)))
	if response.Code != http.StatusConflict {
		t.Fatal("stale ping accepted after clear")
	}
	// A new-generation stroke is a barrier: no stale drawing or initial state may precede it.
	if err := gm.WriteJSON(penStroke{Type: "penStroke", ID: "4", Points: []rulerPoint{{X: 3, Y: 3}}, Generation: 1}); err != nil {
		t.Fatal(err)
	}
	for _, conn := range []*websocket.Conn{gm, player, joined} {
		var stroke penStroke
		read(conn, &stroke)
		if stroke.Type != "penStroke" || stroke.ID != owner+"/4" || stroke.Generation != 1 {
			t.Fatalf("stale update restored cleared content: %+v", stroke)
		}
	}
	if err := gm.WriteJSON(fogMark{Type: "fogMark", ID: "4", Mode: "paint", Width: 2, Points: []rulerPoint{{X: 3, Y: 3}}, Generation: 1}); err != nil {
		t.Fatal(err)
	}
	for _, conn := range []*websocket.Conn{gm, player, joined} {
		var mark fogMark
		read(conn, &mark)
		if mark.Type != "fogMark" || mark.Generation != 1 {
			t.Fatal("new fog cannot be drawn after clear")
		}
	}
}
