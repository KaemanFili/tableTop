package handlers

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"tableTop/main/internal/app"
)

func TestDiceRollSharedResultsAndValidation(t *testing.T) {
	state := app.NewState()
	ws := NewWebSocketHandler(state)
	mux := http.NewServeMux()
	newTestEndpointHandler(t, state, ws, newTestLibrary(t)).RegisterRoutes(mux)
	ws.RegisterRoutes(mux)
	server := newTestServer(t, mux)
	connect := func() *websocket.Conn {
		t.Helper()
		conn, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(server.URL, "http")+"/ws/updateUnitsForClients", nil)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { conn.Close() })
		readRulerSession(t, conn)
		return conn
	}
	first, second := connect(), connect()
	post := func(body string, status int, gm ...bool) []byte {
		t.Helper()
		path := server.URL + "/dice/roll"
		if len(gm) > 0 && gm[0] {
			path += "?gm=true"
		}
		response, err := http.Post(path, "application/json", strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		defer response.Body.Close()
		data, _ := io.ReadAll(response.Body)
		if response.StatusCode != status {
			t.Fatalf("status %d, want %d: %s", response.StatusCode, status, data)
		}
		return data
	}
	for _, body := range []string{
		`{}`, `{"requestId":"test","groups":[]}`,
		`{"requestId":"test","rollerName":"Imposter","groups":[{"qty":1,"sides":6,"modifier":0}]}`,
		`{"requestId":"test","groups":[{"qty":0,"sides":6,"modifier":0}]}`,
		`{"requestId":"test","groups":[{"qty":1,"sides":7,"modifier":0}]}`,
		`{"requestId":"test","groups":[{"qty":1.5,"sides":6,"modifier":0}]}`,
		`{"requestId":"test","groups":[{"qty":100,"sides":6,"modifier":0},{"qty":1,"sides":20,"modifier":0}]}`,
		`{"requestId":"test","groups":[{"qty":1,"sides":6,"modifier":1000001}]}`,
		`{"requestId":"test","groups":[{"qty":1,"sides":6,"modifier":0,"values":[6]}]}`,
		`{"requestId":"test","groups":[{"qty":1,"sides":6,"modifier":0}]} {}`,
	} {
		post(body, http.StatusBadRequest)
	}
	// Rejected requests produce no notification; both sockets next receive the
	// same valid result, including separate groups for the same die type.
	body := `{"requestId":"mixed","groups":[{"qty":2,"sides":6,"modifier":3},{"qty":1,"sides":20,"modifier":-2},{"qty":2,"sides":6,"modifier":-1}]}`
	data := post(body, http.StatusOK)
	var result diceRoll
	if err := json.Unmarshal(data, &result); err != nil {
		t.Fatal(err)
	}
	if result.Type != "diceRolled" || result.ID == "" || result.RequestID != "mixed" || len(result.Groups) != 3 {
		t.Fatalf("invalid result: %+v", result)
	}
	var total int64
	for index, group := range result.Groups {
		expected := []diceGroup{{Qty: 2, Sides: 6, Modifier: 3}, {Qty: 1, Sides: 20, Modifier: -2}, {Qty: 2, Sides: 6, Modifier: -1}}[index]
		if group.Qty != expected.Qty || group.Sides != expected.Sides || group.Modifier != expected.Modifier || len(group.Values) != group.Qty {
			t.Fatalf("wrong group: %+v", group)
		}
		subtotal := group.Modifier
		for _, value := range group.Values {
			if value < 1 || value > group.Sides {
				t.Fatalf("out-of-range die: %d", value)
			}
			subtotal += int64(value)
		}
		if group.Subtotal != subtotal {
			t.Fatalf("modifier applied incorrectly: %+v", group)
		}
		total += subtotal
	}
	if result.Total != total {
		t.Fatalf("total %d, want %d", result.Total, total)
	}
	for _, conn := range []*websocket.Conn{first, second} {
		conn.SetReadDeadline(time.Now().Add(3 * time.Second))
		_, broadcast, err := conn.ReadMessage()
		if err != nil || !bytes.Equal(broadcast, data) {
			t.Fatalf("broadcast differs: %s, %v", broadcast, err)
		}
	}
	// Private GM results are returned only over the originating HTTP request.
	// The next public roll proves no private event reached any socket.
	privateBody := `{"requestId":"private-gm","private":true,"groups":[{"qty":2,"sides":6,"modifier":3}]}`
	post(privateBody, http.StatusForbidden)
	privateData := post(privateBody, http.StatusOK, true)
	var privateResult diceRoll
	if err := json.Unmarshal(privateData, &privateResult); err != nil {
		t.Fatal(err)
	}
	if !privateResult.Private || privateResult.RequestID != "private-gm" || len(privateResult.Groups) != 1 || len(privateResult.Groups[0].Values) != 2 {
		t.Fatalf("invalid private result: %+v", privateResult)
	}
	// Joining later does not replay old rolls.
	third := connect()
	next := post(body, http.StatusOK)
	var nextRoll diceRoll
	if err := json.Unmarshal(next, &nextRoll); err != nil {
		t.Fatal(err)
	}
	for _, conn := range []*websocket.Conn{first, second, third} {
		conn.SetReadDeadline(time.Now().Add(3 * time.Second))
		var actual diceRoll
		if err := conn.ReadJSON(&actual); err != nil {
			t.Fatal(err)
		}
		if actual.Private || actual.ID == result.ID || !reflect.DeepEqual(actual, nextRoll) {
			t.Fatalf("stale or inconsistent roll: %+v", actual)
		}
	}
}
