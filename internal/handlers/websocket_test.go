package handlers

import (
	"net"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"tableTop/main/internal/app"
)

func TestStalledBoardClientIsDisconnected(t *testing.T) {
	ws := NewWebSocketHandler(app.NewState())
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
	ws.boardMutex.Lock()
	remaining := len(ws.boardClients)
	ws.boardMutex.Unlock()
	if remaining != 0 {
		t.Fatal("stalled client was retained")
	}
}

func TestWebSocketOrigins(t *testing.T) {
	ws := NewWebSocketHandler(app.NewState())
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
