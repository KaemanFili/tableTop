package handlers

import (
	"fmt"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"tableTop/main/internal/app/model"
)

func TestPositionBroadcastStopsAndRestarts(t *testing.T) {
	ws := NewWebSocketHandler(model.NewState())
	ws.StartPositionBroadcast()
	mux := http.NewServeMux()
	ws.RegisterRoutes(mux)
	server := newTestServer(t, mux)
	waitIdle := func() {
		t.Helper()
		deadline := time.Now().Add(time.Second)
		for {
			ws.positionMutex.Lock()
			idle := !ws.positionBroadcastRunning && len(ws.positionClients) == 0
			ws.positionMutex.Unlock()
			if idle {
				return
			}
			if time.Now().After(deadline) {
				t.Fatal("position broadcast did not stop after the last client left")
			}
			time.Sleep(time.Millisecond)
		}
	}
	waitIdle()
	for range 2 {
		client, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(server.URL, "http")+"/ws/updatePosForClients", nil)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { client.Close() })
		client.SetReadDeadline(time.Now().Add(time.Second))
		var units []model.UnitData
		if err := client.ReadJSON(&units); err != nil {
			t.Fatalf("connected client received no broadcast: %v", err)
		}
		client.Close()
		waitIdle()
	}
}

func TestStalledBoardClientIsDisconnected(t *testing.T) {
	ws := NewWebSocketHandler(model.NewState())
	mux := http.NewServeMux()
	ws.RegisterRoutes(mux)
	server := newTestServer(t, mux)
	client, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(server.URL, "http")+"/ws/updateUnitsForClients", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	readRulerSession(t, client)
	ws.boardMutex.Lock()
	for conn := range ws.boardClients {
		if err := conn.UnderlyingConn().(*net.TCPConn).SetWriteBuffer(1024); err != nil {
			t.Fatal(err)
		}
	}
	ws.boardMutex.Unlock()
	// Stop reading and fill the socket buffer. The write must release the board lock.
	done := make(chan struct{})
	go func() { ws.broadcastUnits([]byte(strings.Repeat("x", 8<<20))); close(done) }()
	select {
	case <-done:
	case <-time.After(socketWriteTimeout + 3*time.Second):
		t.Fatal("stalled client blocked the board")
	}
	deadline := time.Now().Add(socketWriteTimeout + time.Second)
	for {
		ws.boardMutex.Lock()
		remaining := len(ws.boardClients)
		ws.boardMutex.Unlock()
		if remaining == 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("stalled client was retained")
		}
		time.Sleep(time.Millisecond)
	}
}

func TestSlowClientDoesNotDelayOtherPlayers(t *testing.T) {
	ws := NewWebSocketHandler(model.NewState())
	mux := http.NewServeMux()
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
	slow := connect()
	healthy := connect()
	ws.boardMutex.Lock()
	for conn := range ws.boardClients {
		if err := conn.UnderlyingConn().(*net.TCPConn).SetWriteBuffer(1024); err != nil {
			t.Fatal(err)
		}
	}
	ws.boardMutex.Unlock()
	// A blocked write is much longer than a frame, but must not hold the board lock.
	received := make(chan error, 1)
	go func() {
		healthy.SetReadDeadline(time.Now().Add(time.Second))
		_, _, err := healthy.ReadMessage()
		if err == nil {
			_, message, readErr := healthy.ReadMessage()
			err = readErr
			if err == nil && string(message) != "next drawing batch" {
				err = fmt.Errorf("messages arrived out of order")
			}
		}
		received <- err
	}()
	_ = slow // Deliberately leave this connection unread.
	start := time.Now()
	ws.broadcastUnits([]byte(strings.Repeat("x", 1<<20)))
	ws.broadcastUnits([]byte("next drawing batch"))
	if time.Since(start) > 100*time.Millisecond {
		t.Fatal("network write blocked board producers")
	}
	select {
	case err := <-received:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("slow player delayed a healthy player's drawing")
	}
}

func TestWebSocketOrigins(t *testing.T) {
	ws := NewWebSocketHandler(model.NewState())
	mux := http.NewServeMux()
	ws.RegisterRoutes(mux)
	server := newTestServer(t, mux)
	for _, path := range []string{"/ws/updateServer", "/ws/updatePosForClients", "/ws/updateUnitsForClients"} {
		for _, origin := range []string{"", server.URL, "https://other.example"} {
			t.Run(path+"/"+origin, func(t *testing.T) {
				headers := http.Header{}
				if origin != "" {
					headers.Set("Origin", origin)
				}
				conn, response, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(server.URL, "http")+path, headers)
				if origin == "https://other.example" {
					if conn != nil {
						conn.Close()
						t.Fatal("foreign origin was accepted")
					}
					if err == nil || response == nil || response.StatusCode != http.StatusForbidden {
						t.Fatalf("foreign origin: %v, %v", response, err)
					}
					response.Body.Close()
					return
				}
				if err != nil {
					t.Fatal(err)
				}
				conn.Close()
			})
		}
	}
}
