package handlers

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"tableTop/main/internal/app"
)

func TestMapCreatePersistShareAndClear(t *testing.T) {
	t.Chdir("../..")
	dir := t.TempDir()
	maps, err := app.NewMapLibrary(dir)
	if err != nil {
		t.Fatal(err)
	}
	state := app.NewState()
	token := state.AddRandomUnit(app.PCUnitImageID)
	ws := NewWebSocketHandler(state)
	handler := NewEndpointHandler(state, ws, newTestLibrary(t), maps)
	mux := http.NewServeMux()
	handler.RegisterRoutes(mux)
	ws.RegisterRoutes(mux)
	server := httptest.NewServer(mux)
	defer server.Close()
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
	readMap := func(conn *websocket.Conn) app.MapDefinition {
		t.Helper()
		conn.SetReadDeadline(time.Now().Add(3 * time.Second))
		var message struct {
			Type string
			Map  app.MapDefinition
		}
		if err := conn.ReadJSON(&message); err != nil || message.Type != "boardMapChanged" {
			t.Fatalf("map update = %+v, error %v", message, err)
		}
		return message.Map
	}
	first, second := connect(), connect()
	for _, path := range []string{"/map-library/new", "/map-library/new?gm=true"} {
		response := httptest.NewRecorder()
		mux.ServeHTTP(response, httptest.NewRequest(http.MethodGet, path, nil))
		if strings.Contains(path, "gm=true") {
			if response.Code != 200 || !strings.Contains(response.Body.String(), `id="map-width"`) || !strings.Contains(response.Body.String(), `id="map-preview"`) {
				t.Fatal("map form missing dimensions or preview")
			}
		} else if response.Code != http.StatusForbidden {
			t.Fatal("non-GM opened map form")
		}
	}
	create := func(query string, fields map[string]string, image []byte) *httptest.ResponseRecorder {
		t.Helper()
		request := libraryRequest(t, "Forest <ruins>", "", "", image, fields)
		request.URL, _ = url.Parse("/map-library" + query)
		response := httptest.NewRecorder()
		mux.ServeHTTP(response, request)
		return response
	}
	fields := map[string]string{"width": "17.5", "height": "10.25", "offsetX": "1.5", "offsetY": "-0.25"}
	if response := create("", fields, testPNG(t, 4, 2)); response.Code != http.StatusForbidden {
		t.Fatal("non-GM created map")
	}
	for _, invalid := range []string{"NaN", "Infinity", "0", "65"} {
		bad := map[string]string{"width": invalid, "height": "10", "offsetX": "0", "offsetY": "0"}
		response := create("?gm=true", bad, testPNG(t, 4, 2))
		if response.Header().Get("HX-Trigger") != "" || !strings.Contains(response.Body.String(), `role="alert"`) {
			t.Fatal("invalid dimensions accepted")
		}
	}
	for _, invalid := range [][]byte{nil, []byte("not an image")} {
		response := create("?gm=true", fields, invalid)
		if response.Header().Get("HX-Trigger") != "" || !strings.Contains(response.Body.String(), `role="alert"`) {
			t.Fatal("missing or broken image accepted")
		}
	}
	response := create("?gm=true", fields, testPNG(t, 4, 2))
	if response.Code != http.StatusOK || response.Header().Get("HX-Trigger") != "mapSaved" {
		t.Fatalf("save failed: %s", response.Body.String())
	}
	definition := readMap(first)
	if other := readMap(second); !reflect.DeepEqual(other, definition) {
		t.Fatal("boards received different maps")
	}
	if definition.Name != "Forest <ruins>" || definition.Width != 17.5 || definition.Height != 10.25 || definition.OffsetX != 1.5 || definition.OffsetY != -0.25 {
		t.Fatalf("wrong saved fit: %+v", definition)
	}
	if got, _ := state.UnitByID(token.ID); got != token {
		t.Fatal("creating map changed token")
	}
	joined := connect()
	if initial := readMap(joined); initial != definition {
		t.Fatal("joining board did not receive map")
	}
	reloaded, err := app.NewMapLibrary(dir)
	if err != nil {
		t.Fatal(err)
	}
	if saved, exists := reloaded.MapByID(definition.ID); !exists || saved != definition {
		t.Fatal("map did not survive library reload")
	}
	image := httptest.NewRecorder()
	mux.ServeHTTP(image, httptest.NewRequest(http.MethodGet, "/maps/images/"+definition.ID, nil))
	if image.Code != 200 || image.Header().Get("Content-Type") != "image/png" {
		t.Fatal("map image not served")
	}
	cleared := httptest.NewRecorder()
	mux.ServeHTTP(cleared, httptest.NewRequest(http.MethodPost, "/board/clear?gm=true", nil))
	if cleared.Code != http.StatusNoContent {
		t.Fatal("clear map failed")
	}
	ws.unitCreatedMutex.Lock()
	if ws.currentMap != nil {
		t.Error("clear left map background")
	}
	ws.unitCreatedMutex.Unlock()
	if _, exists := maps.MapByID(definition.ID); !exists {
		t.Fatal("clear removed saved map")
	}
}
