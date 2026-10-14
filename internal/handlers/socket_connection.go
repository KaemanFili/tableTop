package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/gorilla/websocket"
)

const socketWriteTimeout = 2 * time.Second

// Callers serialize writes to a connection; each write has a bounded lifetime.
func writeSocket(conn *websocket.Conn, data []byte) error {
	if err := conn.SetWriteDeadline(time.Now().Add(socketWriteTimeout)); err != nil {
		return err
	}
	return conn.WriteMessage(websocket.TextMessage, data)
}

func writeSocketJSON(conn *websocket.Conn, value any) error {
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	return writeSocket(conn, data)
}

// Upgrade reserves a slot before hijacking. Each handler releases it on exit.
func (h *WebSocketHandler) upgrade(w http.ResponseWriter, r *http.Request) (*websocket.Conn, error) {
	h.socketMutex.Lock()
	defer h.socketMutex.Unlock()
	if h.socketClosed {
		http.Error(w, "This tabletop has been deleted.", http.StatusGone)
		return nil, fmt.Errorf("tabletop sockets are closed")
	}
	select {
	case h.socketSlots <- struct{}{}:
	default:
		http.Error(w, "This tabletop has too many connections. Close unused tabs.", http.StatusTooManyRequests)
		return nil, fmt.Errorf("tabletop connection limit reached")
	}
	conn, err := h.upgrader.Upgrade(w, r, nil)
	if err != nil {
		<-h.socketSlots
		return nil, err
	}
	conn.SetReadLimit(1024)
	conn.SetReadDeadline(time.Now().Add(90 * time.Second))
	conn.SetPongHandler(func(string) error { return conn.SetReadDeadline(time.Now().Add(90 * time.Second)) })
	done := make(chan struct{})
	go func() {
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-done:
				return
			case <-ticker.C:
				if err := conn.WriteControl(websocket.PingMessage, nil, time.Now().Add(socketWriteTimeout)); err != nil {
					conn.Close()
					return
				}
			}
		}
	}()
	// Keep cleanup scoped to the handler via releaseSocket.
	h.socketDone[conn] = done
	return conn, nil
}

// Close prevents new upgrades and disconnects every socket on this tabletop.
func (h *WebSocketHandler) Close() {
	h.socketMutex.Lock()
	defer h.socketMutex.Unlock()
	h.socketClosed = true
	for conn := range h.socketDone {
		conn.Close()
	}
}

func (h *WebSocketHandler) releaseSocket(conn *websocket.Conn) {
	conn.Close()
	h.socketMutex.Lock()
	close(h.socketDone[conn])
	delete(h.socketDone, conn)
	h.socketMutex.Unlock()
	<-h.socketSlots
}
