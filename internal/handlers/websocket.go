package handlers

import (
	"encoding/json"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"

	"tableTop/main/internal/app/model"
)

type WebSocketHandler struct {
	state *model.AppState

	upgrader websocket.Upgrader

	positionClients          map[*websocket.Conn]*socketWriter
	boardClients             map[*websocket.Conn]*socketWriter
	presenceClients          map[*websocket.Conn]sessionMember
	positionMutex            sync.Mutex
	positionBroadcastEnabled bool
	positionBroadcastRunning bool
	boardMutex               sync.Mutex
	rulers                   map[string]boardRuler
	strokes                  map[string]penStroke
	erasedStrokes            map[string]bool
	fogMarks                 []fogMark
	boardGeneration          uint64
	currentMap               *model.MapDefinition
	drawingPoints            int
	socketSlots              chan struct{}
	socketMutex              sync.Mutex
	socketDone               map[*websocket.Conn]chan struct{}
	socketClosed             bool
}

func NewWebSocketHandler(state *model.AppState) *WebSocketHandler {
	return &WebSocketHandler{
		state:       state,
		socketSlots: make(chan struct{}, 96),
		socketDone:  make(map[*websocket.Conn]chan struct{}),
		// Gorilla defaults to accepting absent or same-host Origin headers.
		upgrader:        websocket.Upgrader{HandshakeTimeout: 5 * time.Second},
		positionClients: make(map[*websocket.Conn]*socketWriter),
		boardClients:    make(map[*websocket.Conn]*socketWriter),
		presenceClients: make(map[*websocket.Conn]sessionMember),
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

// broadcastUnitsLocked requires boardMutex to preserve message order in each queue.
func (h *WebSocketHandler) broadcastUnitsLocked(data []byte) {
	for conn, writer := range h.boardClients {
		if !writer.send(data) {
			delete(h.boardClients, conn)
		}
	}
}
