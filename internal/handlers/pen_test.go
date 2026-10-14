package handlers

import (
	"net/http"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"tableTop/main/internal/app"
)

func TestPenLiveStrokesInitialStateAndDisconnect(t *testing.T) {
	state := app.NewState()
	token := state.AddRandomUnit(app.DefaultUnitImageID)
	ws := NewWebSocketHandler(state)
	mux := http.NewServeMux()
	ws.RegisterRoutes(mux)
	server := newTestServer(t, mux)
	connect := func() (*websocket.Conn, string) {
		t.Helper()
		conn, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(server.URL, "http")+"/ws/updateUnitsForClients", nil)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { conn.Close() })
		return conn, readRulerSession(t, conn)
	}
	read := func(conn *websocket.Conn, expected penStroke) {
		t.Helper()
		conn.SetReadDeadline(time.Now().Add(3 * time.Second))
		var actual penStroke
		if err := conn.ReadJSON(&actual); err != nil || !reflect.DeepEqual(actual, expected) {
			t.Fatalf("stroke = %+v, want %+v, error %v", actual, expected, err)
		}
	}
	first, firstID := connect()
	second, secondID := connect()
	for _, invalid := range []string{
		`{"type":"penStroke","id":"bad","points":[{"x":1,"y":1}]}`,
		`{"type":"penStroke","id":"1","offset":1,"points":[{"x":1,"y":1}]}`,
		`{"type":"penStroke","id":"1","points":[{"x":-1,"y":1}]}`,
		`{"type":"penStroke","id":"1","points":[]}`,
		`{"type":"penStroke","id":"1","points":[{"x":1,"y":1}],"color":"red"}`,
		`{"type":"penStroke","id":"1","points":[{"x":1,"y":1}],"width":241}`,
		`{"type":"penStroke","id":"1","points":[{"x":1,"y":1}],"width":-1}`,
	} {
		if err := first.WriteMessage(websocket.TextMessage, []byte(invalid)); err != nil {
			t.Fatal(err)
		}
	}
	points := []rulerPoint{{X: 1, Y: 1}, {X: 1.5, Y: 2}}
	update := penStroke{Type: "penStroke", ID: "1", Owner: secondID, Points: points, Color: "#ff3344", Width: 240}
	if err := first.WriteJSON(update); err != nil {
		t.Fatal(err)
	}
	expected := update
	expected.ID, expected.Owner = firstID+"/1", firstID
	read(first, expected)
	read(second, expected)
	// Duplicate or out-of-order batches must not alter the path or broadcast.
	if err := first.WriteJSON(update); err != nil {
		t.Fatal(err)
	}
	update.Offset, update.Points, update.Finished = 2, []rulerPoint{{X: 3, Y: 4}}, true
	// Changing settings during an existing stroke must not restyle it.
	update.Color, update.Width = "#00ff00", 1
	if err := first.WriteJSON(update); err != nil {
		t.Fatal(err)
	}
	expected.Offset, expected.Points, expected.Finished = 2, update.Points, true
	read(first, expected)
	read(second, expected)
	third, _ := connect()
	initialStroke := penStroke{Type: "penStroke", ID: firstID + "/1", Owner: firstID, Points: append(points, rulerPoint{X: 3, Y: 4}), Finished: true, Color: expected.Color, Width: expected.Width}
	read(third, initialStroke)
	// Another user's identical client stroke number produces an independent path.
	other := penStroke{Type: "penStroke", ID: "1", Points: []rulerPoint{{X: 6, Y: 7}}}
	if err := second.WriteJSON(other); err != nil {
		t.Fatal(err)
	}
	other.ID, other.Owner = secondID+"/1", secondID
	other.Color, other.Width = "#1d4ed8", 3
	for _, conn := range []*websocket.Conn{first, second, third} {
		read(conn, other)
	}
	second.Close()
	completed := penStroke{Type: "penStroke", ID: other.ID, Owner: secondID, Offset: 1, Finished: true, Color: other.Color, Width: other.Width}
	read(first, completed)
	read(third, completed)
	ws.boardMutex.Lock()
	if len(ws.strokes) != 2 || !ws.strokes[other.ID].Finished || len(ws.strokes[initialStroke.ID].Points) != 3 {
		t.Error("disconnect lost or corrupted drawings")
	}
	ws.boardMutex.Unlock()
	if got, _ := state.UnitByID(token.ID); got != token {
		t.Fatal("drawing moved a token")
	}
}

func TestEraserSyncsRemovalAndPreventsLateStrokeResurrection(t *testing.T) {
	state := app.NewState()
	token := state.AddRandomUnit(app.DefaultUnitImageID)
	ws := NewWebSocketHandler(state)
	mux := http.NewServeMux()
	ws.RegisterRoutes(mux)
	server := newTestServer(t, mux)
	connect := func() (*websocket.Conn, string) {
		t.Helper()
		conn, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(server.URL, "http")+"/ws/updateUnitsForClients", nil)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { conn.Close() })
		return conn, readRulerSession(t, conn)
	}
	first, firstID := connect()
	second, _ := connect()
	read := func(conn *websocket.Conn, target any) {
		t.Helper()
		conn.SetReadDeadline(time.Now().Add(3 * time.Second))
		if err := conn.ReadJSON(target); err != nil {
			t.Fatal(err)
		}
	}
	for _, id := range []string{"1", "2"} {
		update := penStroke{Type: "penStroke", ID: id, Points: []rulerPoint{{X: 2, Y: 3}}}
		if err := first.WriteJSON(update); err != nil {
			t.Fatal(err)
		}
		for _, conn := range []*websocket.Conn{first, second} {
			var stroke penStroke
			read(conn, &stroke)
			if stroke.ID != firstID+"/"+id {
				t.Fatal("incorrect stroke identity")
			}
		}
	}
	removedID := firstID + "/1"
	for range 2 {
		// Repeated erasure succeeds; unknown IDs neither delete another stroke nor add tombstones.
		if err := second.WriteJSON(map[string]any{"type": "penErase", "ids": []string{removedID, removedID, "missing"}}); err != nil {
			t.Fatal(err)
		}
		for _, conn := range []*websocket.Conn{first, second} {
			var message struct {
				Type string   `json:"type"`
				IDs  []string `json:"ids"`
			}
			read(conn, &message)
			if message.Type != "penErased" || !reflect.DeepEqual(message.IDs, []string{removedID}) {
				t.Fatalf("incorrect removal: %+v", message)
			}
		}
	}
	for _, offset := range []int{0, 1} {
		if err := first.WriteJSON(penStroke{Type: "penStroke", ID: "1", Offset: offset, Points: []rulerPoint{{X: 4, Y: 5}}}); err != nil {
			t.Fatal(err)
		}
	}
	// A following valid event is a barrier: no late stroke update may precede it.
	if err := first.WriteJSON(boardRuler{Type: "rulerChanged", Clear: true}); err != nil {
		t.Fatal(err)
	}
	for _, conn := range []*websocket.Conn{first, second} {
		var message boardRuler
		read(conn, &message)
		if message.Type != "rulerChanged" {
			t.Fatal("late batch resurrected erased stroke")
		}
	}
	third, _ := connect()
	var initialStroke penStroke
	read(third, &initialStroke)
	if initialStroke.ID != firstID+"/2" {
		t.Fatal("initial board state includes erased stroke")
	}
	ws.boardMutex.Lock()
	if len(ws.strokes) != 1 || len(ws.erasedStrokes) != 1 || !ws.erasedStrokes[removedID] {
		t.Error("incorrect eraser state")
	}
	ws.boardMutex.Unlock()
	if got, _ := state.UnitByID(token.ID); got != token {
		t.Fatal("eraser changed token")
	}
}
