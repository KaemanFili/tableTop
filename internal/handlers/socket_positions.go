package handlers

import (
	"encoding/json"
	"log"
	"net/http"
	"time"

	"golang.org/x/time/rate"
)

func (h *WebSocketHandler) StartPositionBroadcast() {
	h.positionMutex.Lock()
	defer h.positionMutex.Unlock()
	h.positionBroadcastEnabled = true
	h.startPositionBroadcastLocked()
}

// Start only while clients are connected; retain board state for their return.
func (h *WebSocketHandler) startPositionBroadcastLocked() {
	if !h.positionBroadcastEnabled || h.positionBroadcastRunning || len(h.positionClients) == 0 {
		return
	}
	h.positionBroadcastRunning = true
	go func() {
		ticker := time.NewTicker(33 * time.Millisecond)
		defer ticker.Stop()

		for range ticker.C {
			h.positionMutex.Lock()
			if len(h.positionClients) == 0 {
				h.positionBroadcastRunning = false
				h.positionMutex.Unlock()
				return
			}
			data, err := json.Marshal(h.state.Units())
			if err != nil {
				h.positionMutex.Unlock()
				log.Println("Marshal error:", err)
				continue
			}

			for conn, writer := range h.positionClients {
				if !writer.send(data) {
					delete(h.positionClients, conn)
				}
			}
			h.positionMutex.Unlock()
		}
	}()
}

func (h *WebSocketHandler) updateServer(w http.ResponseWriter, r *http.Request) {
	conn, err := h.upgrade(w, r)
	if err != nil {
		log.Println("Upgrade failed:", err)
		return
	}
	defer h.releaseSocket(conn)

	limiter := rate.NewLimiter(120, 240)
	for {
		_, data, err := conn.ReadMessage()
		if err != nil {
			log.Println("Read error:", err)
			break
		}

		if !limiter.Allow() {
			return
		}
		var unitData struct {
			ID   string
			Left int
			Top  int
		}
		if err := json.Unmarshal(data, &unitData); err != nil {
			log.Println("Unmarshal error:", err)
			continue
		}

		if !h.state.UpdateUnitPosition(unitData.ID, unitData.Left, unitData.Top) {
			log.Printf("unit %s not found", unitData.ID)
			continue
		}

		if err := writeSocket(conn, data); err != nil {
			log.Println("Write error:", err)
			break
		}
	}
}

func (h *WebSocketHandler) updateClientPosition(w http.ResponseWriter, r *http.Request) {
	conn, err := h.upgrade(w, r)
	if err != nil {
		log.Println("Upgrade failed:", err)
		return
	}
	defer h.releaseSocket(conn)

	h.positionMutex.Lock()
	initial, err := json.Marshal(h.state.Units())
	if err != nil {
		h.positionMutex.Unlock()
		return
	}
	h.positionClients[conn] = newSocketWriter(conn, [][]byte{initial}, true)
	h.startPositionBroadcastLocked()
	h.positionMutex.Unlock()

	limiter := rate.NewLimiter(10, 20)
	for {
		if _, _, err := conn.ReadMessage(); err != nil || !limiter.Allow() {
			break
		}
	}

	h.positionMutex.Lock()
	if writer := h.positionClients[conn]; writer != nil {
		writer.stop()
	}
	delete(h.positionClients, conn)
	h.positionMutex.Unlock()
}
