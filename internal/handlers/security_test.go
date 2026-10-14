package handlers

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"tableTop/main/internal/app/model"
)

func TestWebSocketMessageAndConnectionLimits(t *testing.T) {
	ws := NewWebSocketHandler(model.NewState())
	ws.socketSlots = make(chan struct{}, 1)
	mux := http.NewServeMux()
	ws.RegisterRoutes(mux)
	server := newTestServer(t, mux)
	endpoint := "ws" + strings.TrimPrefix(server.URL, "http") + "/ws/updateServer"
	first, _, err := websocket.DefaultDialer.Dial(endpoint, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()
	if conn, response, err := websocket.DefaultDialer.Dial(endpoint, nil); err == nil || response == nil || response.StatusCode != 429 {
		if conn != nil {
			conn.Close()
		}
		t.Fatal("table connection cap was bypassed")
	} else {
		response.Body.Close()
	}
	first.WriteMessage(websocket.TextMessage, []byte(strings.Repeat("x", 2048)))
	first.SetReadDeadline(time.Now().Add(time.Second))
	if _, _, err := first.ReadMessage(); !websocket.IsCloseError(err, websocket.CloseMessageTooBig) {
		t.Fatalf("oversized message was not rejected: %v", err)
	}
	deadline := time.Now().Add(time.Second)
	for len(ws.socketSlots) != 0 {
		if time.Now().After(deadline) {
			t.Fatal("closed socket retained its slot")
		}
		time.Sleep(time.Millisecond)
	}
	second, _, err := websocket.DefaultDialer.Dial(endpoint, nil)
	if err != nil {
		t.Fatalf("released slot could not be reused: %v", err)
	}
	second.Close()
}

func TestLiveBoardCapsAndReclamation(t *testing.T) {
	state := model.NewState()
	ws := NewWebSocketHandler(state)
	for range model.MaxBoardUnits {
		state.AddRandomUnit(model.PCUnitImageID)
	}
	mux := http.NewServeMux()
	newTestEndpointHandler(t, state, ws, newTestLibrary(t)).RegisterRoutes(mux)
	result := httptest.NewRecorder()
	mux.ServeHTTP(result, httptest.NewRequest("POST", "/unit?unitID=knight", nil))
	if result.Code != 409 || state.UnitCount() != model.MaxBoardUnits {
		t.Fatal("token count limit was bypassed")
	}
	ws.drawingPoints = model.MaxBoardPoints
	stroke, _ := json.Marshal(penStroke{Type: "penStroke", ID: "1", Points: []rulerPoint{{X: 1, Y: 1}}})
	if err := ws.updatePen("player", stroke); !errors.Is(err, model.ErrCapacity) || len(ws.strokes) != 0 {
		t.Fatal("drawing point limit was bypassed")
	}
	fog, _ := json.Marshal(fogMark{Type: "fogMark", ID: "1", Mode: "paint", Width: 1, Points: []rulerPoint{{X: 1, Y: 1}}})
	if err := ws.updateFog("gm", fog); !errors.Is(err, model.ErrCapacity) || len(ws.fogMarks) != 0 {
		t.Fatal("fog point limit was bypassed")
	}
	ws.boardMutex.Lock()
	ws.clearBoardLocked()
	ws.boardMutex.Unlock()
	stroke, _ = json.Marshal(penStroke{Type: "penStroke", ID: "1", Generation: ws.boardGeneration, Points: []rulerPoint{{X: 1, Y: 1}}})
	if err := ws.updatePen("player", stroke); err != nil || ws.drawingPoints != 1 {
		t.Fatal("clearing board did not reclaim capacity")
	}
	erase, _ := json.Marshal(map[string]any{"type": "penErase", "ids": []string{"player/1"}})
	ws.erasePen(erase)
	if ws.drawingPoints != 0 {
		t.Fatal("erasing stroke did not reclaim points")
	}
}

func TestGuardLimitsConcurrentUploads(t *testing.T) {
	entered := make(chan struct{}, 2)
	release := make(chan struct{})
	handler := Guard(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { entered <- struct{}{}; <-release }))
	var workers sync.WaitGroup
	for range 2 {
		workers.Go(func() {
			handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("POST", "/sessions/id/map-library", nil))
		})
	}
	<-entered
	<-entered
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest("POST", "/unit-library", nil))
	close(release)
	workers.Wait()
	if response.Code != 429 {
		t.Fatalf("concurrent upload guard: %d", response.Code)
	}
}
