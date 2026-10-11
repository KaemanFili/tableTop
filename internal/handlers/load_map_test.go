package handlers

import (
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"tableTop/main/internal/app"
)

func TestLoadSavedMapRestoresSharedBoard(t *testing.T) {
	dir := t.TempDir()
	maps, err := app.NewMapLibrary(dir)
	if err != nil {
		t.Fatal(err)
	}
	definition, err := maps.Create(app.MapDefinition{Name: "Forest <ruins>", Width: 17.5, Height: 10, OffsetX: 1.5}, testPNG(t, 4, 2))
	if err != nil {
		t.Fatal(err)
	}
	board := app.SavedBoard{
		Units:   []app.UnitData{{ID: "old-token", Name: "Mage", UnitType: "pc", ImageID: app.PCUnitImageID, Left: 240, Top: 360, Size: app.SizeLarge, Status: "Poisoned", ImageFit: app.ImageFit{Zoom: 2, OffsetX: 12, OffsetY: -8}}},
		Strokes: []app.PenStroke{{Type: "penStroke", ID: "old/1", Owner: "old", Width: 5, Color: "#123456", Points: []app.BoardPoint{{X: 1, Y: 2}, {X: 3, Y: 4}}, Finished: true}},
		Fog: []app.FogMark{
			{Type: "fogMark", ID: "old/1", Owner: "old", Mode: "paint", Width: 2, Points: []app.BoardPoint{{X: 2, Y: 3}}},
			{Type: "fogMark", ID: "old/2", Owner: "old", Mode: "erase", Width: 1, Points: []app.BoardPoint{{X: 2, Y: 3}}},
		},
		Rulers: []app.BoardRuler{{Type: "rulerChanged", ID: "old", Start: &app.BoardPoint{X: 1, Y: 1}, End: &app.BoardPoint{X: 4, Y: 5}}},
	}
	if err := maps.SaveBoard(definition, board); err != nil {
		t.Fatal(err)
	}
	maps, err = app.NewMapLibrary(dir) // Loading must work after restart.
	if err != nil {
		t.Fatal(err)
	}
	state := app.NewState()
	oldToken := state.AddRandomUnit(app.DefaultUnitImageID)
	ws := NewWebSocketHandler(state)
	handler := newTestEndpointHandler(t, state, ws, newTestLibrary(t), maps)
	mux := http.NewServeMux()
	handler.RegisterRoutes(mux)
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
	request := func(method, path string, want int) *httptest.ResponseRecorder {
		t.Helper()
		response := httptest.NewRecorder()
		mux.ServeHTTP(response, httptest.NewRequest(method, path, nil))
		if response.Code != want {
			t.Fatalf("%s: %d: %s", path, response.Code, response.Body.String())
		}
		return response
	}
	request(http.MethodGet, "/map-library", http.StatusForbidden)
	picker := request(http.MethodGet, "/map-library?gm=true", http.StatusOK)
	if !strings.Contains(picker.Body.String(), "Forest &lt;ruins&gt;") || !strings.Contains(picker.Body.String(), definition.ID+"/load?gm=true") {
		t.Fatal("picker missing escaped map and load button")
	}
	path := "/map-library/" + definition.ID + "/load"
	request(http.MethodPost, path, http.StatusForbidden)
	request(http.MethodPost, "/map-library/missing/load?gm=true", http.StatusNotFound)
	if state.UnitCount() != 1 {
		t.Fatal("failed load changed board")
	}
	request(http.MethodPost, path+"?gm=true", http.StatusOK)
	read := func(conn *websocket.Conn, value any) {
		t.Helper()
		conn.SetReadDeadline(time.Now().Add(3 * time.Second))
		if err := conn.ReadJSON(value); err != nil {
			t.Fatal(err)
		}
	}
	for _, conn := range []*websocket.Conn{first, second} {
		var cleared struct {
			Type       string
			IDs        []string
			Generation uint64
		}
		read(conn, &cleared)
		if cleared.Type != "boardCleared" || cleared.Generation != 1 || !reflect.DeepEqual(cleared.IDs, []string{oldToken.ID}) {
			t.Fatalf("wrong clear: %+v", cleared)
		}
		var mapEvent struct {
			Type string
			Map  app.MapDefinition
		}
		read(conn, &mapEvent)
		if mapEvent.Type != "boardMapChanged" || mapEvent.Map != definition || mapEvent.Map.Board != nil {
			t.Fatal("wrong loaded map")
		}
		var stroke penStroke
		read(conn, &stroke)
		if stroke.Type != "penStroke" || stroke.ID == "old/1" || stroke.Generation != 1 || stroke.Color != "#123456" || stroke.Width != 5 || !reflect.DeepEqual(stroke.Points, board.Strokes[0].Points) {
			t.Fatalf("wrong loaded pen: %+v", stroke)
		}
		for _, mode := range []string{"paint", "erase"} {
			var mark fogMark
			read(conn, &mark)
			if mark.Type != "fogMark" || mark.Mode != mode || mark.Generation != 1 {
				t.Fatal("fog order or generation lost")
			}
		}
		var ruler boardRuler
		read(conn, &ruler)
		if ruler.Type != "rulerChanged" || ruler.ID == "old" || ruler.Generation != 1 || *ruler.End != *board.Rulers[0].End {
			t.Fatal("ruler lost")
		}
		var created string
		read(conn, &created)
		if created != "unit created" {
			t.Fatal("tokens not refreshed")
		}
	}
	units := state.Units()
	if len(units) != 1 || units[0].ID == oldToken.ID {
		t.Fatal("restored units missing or old IDs reused")
	}
	restored := units[0]
	restored.ID = board.Units[0].ID
	if restored != board.Units[0] {
		t.Fatalf("restored unit changed: %+v", restored)
	}
	if state.UpdateUnitPosition(oldToken.ID, 1, 1) {
		t.Fatal("late drag changed restored token")
	}
	html := request(http.MethodGet, "/loadExistingUnits", http.StatusOK).Body.String()
	if !strings.Contains(html, "Mage") || !strings.Contains(html, "Poisoned") || !strings.Contains(html, `data-size="large"`) {
		t.Fatal("restored token not rendered with saved values")
	}
	// Joining receives the same loaded map and utilities.
	joined := connect()
	var initial struct {
		Type string
		Map  app.MapDefinition
	}
	read(joined, &initial)
	if initial.Type != "boardMapChanged" || initial.Map.ID != definition.ID {
		t.Fatal("joiner missed loaded map")
	}
	// Old-generation drawings cannot reappear after load.
	ws.updatePen("old", []byte(`{"type":"penStroke","id":"9","points":[{"x":1,"y":1}],"generation":0}`))
	ws.boardMutex.Lock()
	if len(ws.strokes) != 1 {
		t.Error("late stroke changed restored board")
	}
	ws.boardMutex.Unlock()
	// The original saved record remains reusable and gets overwritten by Save Map.
	request(http.MethodPost, "/board/save?gm=true", http.StatusNoContent)
	reloaded, err := app.NewMapLibrary(dir)
	if err != nil {
		t.Fatal(err)
	}
	saved, _ := reloaded.MapByID(definition.ID)
	if saved.Board == nil || saved.Board.Units[0].ID != units[0].ID {
		t.Fatal("saving a loaded map failed")
	}
}

func TestLoadImageOnlyMapAndUnavailableArtwork(t *testing.T) {
	maps, err := app.NewMapLibrary(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	definition, err := maps.Create(app.MapDefinition{Name: "Old map", Width: 20, Height: 10}, testPNG(t, 4, 2))
	if err != nil {
		t.Fatal(err)
	}
	state := app.NewState()
	state.AddRandomUnit(app.DefaultUnitImageID)
	ws := NewWebSocketHandler(state)
	handler := newTestEndpointHandler(t, state, ws, newTestLibrary(t), maps)
	mux := http.NewServeMux()
	handler.RegisterRoutes(mux)
	response := httptest.NewRecorder()
	mux.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/map-library/"+definition.ID+"/load?gm=true", nil))
	if response.Code != 200 || state.UnitCount() != 0 || ws.currentMap == nil || ws.currentMap.ID != definition.ID {
		t.Fatal("image-only map not loaded onto empty board")
	}
	state.AddRandomUnit(app.PCUnitImageID)
	path, _ := maps.ImagePath(definition.ID)
	if err := os.Rename(path, path+".missing"); err != nil {
		t.Fatal(err)
	}
	response = httptest.NewRecorder()
	mux.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/map-library/"+definition.ID+"/load?gm=true", nil))
	if response.Code != 500 || state.UnitCount() != 1 || ws.boardGeneration != 1 {
		t.Fatal("missing artwork changed current board")
	}
}
