package handlers

import (
	"crypto/rand"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"sync"
	"tableTop/main/internal/auth"
	"time"

	"tableTop/main/internal/app"

	"github.com/gorilla/websocket"
	"golang.org/x/time/rate"
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

type WebSocketHandler struct {
	state *app.AppState

	upgrader websocket.Upgrader

	positionClients          map[*websocket.Conn]bool
	boardClients             map[*websocket.Conn]bool
	positionMutex            sync.Mutex
	positionBroadcastEnabled bool
	positionBroadcastRunning bool
	boardMutex               sync.Mutex
	rulers                   map[string]boardRuler
	strokes                  map[string]penStroke
	erasedStrokes            map[string]bool
	fogMarks                 []fogMark
	boardGeneration          uint64
	currentMap               *app.MapDefinition
	drawingPoints            int
	socketSlots              chan struct{}
	socketMutex              sync.Mutex
	socketDone               map[*websocket.Conn]chan struct{}
}

func NewWebSocketHandler(state *app.AppState) *WebSocketHandler {
	return &WebSocketHandler{
		state:       state,
		socketSlots: make(chan struct{}, 96),
		socketDone:  make(map[*websocket.Conn]chan struct{}),
		// Gorilla defaults to accepting absent or same-host Origin headers.
		upgrader:        websocket.Upgrader{HandshakeTimeout: 5 * time.Second},
		positionClients: make(map[*websocket.Conn]bool),
		boardClients:    make(map[*websocket.Conn]bool),
		rulers:          make(map[string]boardRuler),
		strokes:         make(map[string]penStroke),
		erasedStrokes:   make(map[string]bool),
	}
}

func (h *WebSocketHandler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/ws/updateServer", h.updateServer)
	mux.HandleFunc("/ws/updatePosForClients", h.updateClientPosition)
	mux.HandleFunc("/ws/updateUnitsForClients", h.updateBoardUtilityValues)
}

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

			for conn := range h.positionClients {
				if err := writeSocket(conn, data); err != nil {
					log.Println("Write error, removing client:", err)
					conn.Close()
					delete(h.positionClients, conn)
				}
			}
			h.positionMutex.Unlock()
		}
	}()
}

func (h *WebSocketHandler) BroadcastUnitState() {
	data, err := json.Marshal("unit created")
	if err != nil {
		log.Println("Marshal error:", err)
		return
	}
	h.broadcastUnits(data)
}

func (h *WebSocketHandler) BroadcastUnitRemoved(id string) {
	data, err := json.Marshal(map[string]string{"type": "unitRemoved", "id": id})
	if err != nil {
		log.Println("Marshal error:", err)
		return
	}
	h.broadcastUnits(data)
}

func (h *WebSocketHandler) broadcastUnits(data []byte) {
	h.boardMutex.Lock()
	defer h.boardMutex.Unlock()
	h.broadcastUnitsLocked(data)
}

// broadcastUnitsLocked requires boardMutex to serialize writes to each socket.
func (h *WebSocketHandler) broadcastUnitsLocked(data []byte) {
	for conn := range h.boardClients {
		if err := writeSocket(conn, data); err != nil {
			log.Println("Write error, removing client:", err)
			conn.Close()
			delete(h.boardClients, conn)
		}
	}
}

// Upgrade reserves a slot before hijacking. Each handler releases it on exit.
func (h *WebSocketHandler) upgrade(w http.ResponseWriter, r *http.Request) (*websocket.Conn, error) {
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
	h.socketMutex.Lock()
	h.socketDone[conn] = done
	h.socketMutex.Unlock()
	return conn, nil
}

func (h *WebSocketHandler) releaseSocket(conn *websocket.Conn) {
	conn.Close()
	h.socketMutex.Lock()
	close(h.socketDone[conn])
	delete(h.socketDone, conn)
	h.socketMutex.Unlock()
	<-h.socketSlots
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
	h.positionClients[conn] = true
	h.startPositionBroadcastLocked()
	h.positionMutex.Unlock()

	limiter := rate.NewLimiter(10, 20)
	for {
		if _, _, err := conn.ReadMessage(); err != nil || !limiter.Allow() {
			break
		}
	}

	h.positionMutex.Lock()
	delete(h.positionClients, conn)
	h.positionMutex.Unlock()
}

func (h *WebSocketHandler) updateBoardUtilityValues(w http.ResponseWriter, r *http.Request) {
	conn, err := h.upgrade(w, r)
	if err != nil {
		log.Println("Upgrade failed:", err)
		return
	}
	defer h.releaseSocket(conn)
	conn.SetReadLimit(1024)
	sessionID := rand.Text()
	defer h.unregisterBoardUtilitiesClient(conn, sessionID)

	if err := h.registerBoardUtilitiesClient(conn, sessionID); err != nil {
		log.Println("Send initial board state:", err)
		return
	}
	h.readBoardUtilitiesMessages(conn, sessionID, auth.IsGM(r))
}

// Registration and the initial state share a lock so live updates cannot
// arrive before the session identity and existing drawings.
func (h *WebSocketHandler) registerBoardUtilitiesClient(conn *websocket.Conn, sessionID string) error {
	h.boardMutex.Lock()
	defer h.boardMutex.Unlock()
	h.boardClients[conn] = true
	if err := writeSocketJSON(conn, map[string]any{"type": "rulerSession", "id": sessionID, "generation": h.boardGeneration}); err != nil {
		return err
	}
	if h.currentMap != nil {
		if err := writeSocketJSON(conn, map[string]any{"type": "boardMapChanged", "map": h.currentMap}); err != nil {
			return err
		}
	}
	for _, ruler := range h.rulers {
		if err := writeSocketJSON(conn, ruler); err != nil {
			return err
		}
	}
	for _, stroke := range h.strokes {
		if err := writeSocketJSON(conn, stroke); err != nil {
			return err
		}
	}
	if len(h.fogMarks) > 0 {
		if err := writeSocketJSON(conn, map[string]string{"type": "fogReset"}); err != nil {
			return err
		}
		for _, mark := range h.fogMarks {
			if err := writeSocketJSON(conn, mark); err != nil {
				return err
			}
		}
	}
	return nil
}

func (h *WebSocketHandler) readBoardUtilitiesMessages(conn *websocket.Conn, sessionID string, isGM bool) {
	limiter := rate.NewLimiter(120, 240)
	for {
		_, data, err := conn.ReadMessage()
		if err != nil {
			return
		}
		if !limiter.Allow() {
			return
		}
		if err := h.handleBoardUtilitiesMessage(sessionID, data, isGM); err != nil {
			h.boardMutex.Lock()
			writeSocketJSON(conn, map[string]string{"type": "boardLimitReached", "message": err.Error()})
			h.boardMutex.Unlock()
		}
	}
}

func (h *WebSocketHandler) handleBoardUtilitiesMessage(sessionID string, data []byte, isGM bool) error {
	var message struct {
		Type string `json:"type"`
	}
	if json.Unmarshal(data, &message) != nil {
		return nil
	}
	switch message.Type {
	case "fogMark":
		if isGM {
			return h.updateFog(sessionID, data)
		}
	case "rulerChanged":
		return h.updateRuler(sessionID, data)
	case "penStroke":
		return h.updatePen(sessionID, data)
	case "penErase":
		return h.erasePen(data)
	}
	return nil
}

func (h *WebSocketHandler) unregisterBoardUtilitiesClient(conn *websocket.Conn, sessionID string) {
	h.boardMutex.Lock()
	defer h.boardMutex.Unlock()
	delete(h.boardClients, conn)
	if _, exists := h.rulers[sessionID]; exists {
		delete(h.rulers, sessionID)
		data, _ := json.Marshal(boardRuler{Type: "rulerChanged", ID: sessionID, Clear: true})
		h.broadcastUnitsLocked(data)
	}
	for id, stroke := range h.strokes {
		if stroke.Owner == sessionID && !stroke.Finished {
			stroke.Finished = true
			h.strokes[id] = stroke
			message, _ := json.Marshal(penStroke{Type: "penStroke", ID: id, Owner: sessionID, Offset: len(stroke.Points), Finished: true, Color: stroke.Color, Width: stroke.Width, Generation: stroke.Generation})
			h.broadcastUnitsLocked(message)
		}
	}
}
