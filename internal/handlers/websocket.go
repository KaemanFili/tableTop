package handlers

import (
	"encoding/json"
	"log"
	"net/http"
	"sync"
	"time"

	"tableTop/main/internal/app"

	"github.com/gorilla/websocket"
)

type WebSocketHandler struct {
	state *app.AppState

	upgrader websocket.Upgrader

	positionClients    map[*websocket.Conn]bool
	unitCreatedClients map[*websocket.Conn]bool
	positionMutex      sync.Mutex
	unitCreatedMutex   sync.Mutex
}

func NewWebSocketHandler(state *app.AppState) *WebSocketHandler {
	return &WebSocketHandler{
		state: state,
		upgrader: websocket.Upgrader{
			CheckOrigin: func(r *http.Request) bool {
				return true // Note: Adjust this for production environments! look at changeWebsocket.txt
			},
		},
		positionClients:    make(map[*websocket.Conn]bool),
		unitCreatedClients: make(map[*websocket.Conn]bool),
	}
}

func (h *WebSocketHandler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/ws/updateServer", h.updateServer)
	mux.HandleFunc("/ws/updatePosForClients", h.updateClientPosition)
	mux.HandleFunc("/ws/updateUnitsForClients", h.updateClientUnits)
}

func (h *WebSocketHandler) StartPositionBroadcast() {
	go func() {
		ticker := time.NewTicker(100 * time.Millisecond)
		defer ticker.Stop()

		for range ticker.C {
			data, err := json.Marshal(h.state.UnitsSnapshot())
			if err != nil {
				log.Println("Marshal error:", err)
				continue
			}

			h.positionMutex.Lock()
			for conn := range h.positionClients {
				if err := conn.WriteMessage(websocket.TextMessage, data); err != nil {
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

	h.unitCreatedMutex.Lock()
	for conn := range h.unitCreatedClients {
		if err := conn.WriteMessage(websocket.TextMessage, data); err != nil {
			log.Println("Write error, removing client:", err)
			conn.Close()
			delete(h.unitCreatedClients, conn)
		}
	}
	h.unitCreatedMutex.Unlock()
}

func (h *WebSocketHandler) updateServer(w http.ResponseWriter, r *http.Request) {
	conn, err := h.upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Println("Upgrade failed:", err)
		return
	}
	defer conn.Close()

	for {
		_, data, err := conn.ReadMessage()
		if err != nil {
			log.Println("Read error:", err)
			break
		}

		log.Printf("Received: %s", data)

		var unitData app.UnitData
		if err := json.Unmarshal(data, &unitData); err != nil {
			log.Println("Unmarshal error:", err)
			continue
		}

		h.state.SetUnit(unitData)
		h.logData(unitData)

		if err := conn.WriteMessage(websocket.TextMessage, data); err != nil {
			log.Println("Write error:", err)
			break
		}
	}
}

func (h *WebSocketHandler) updateClientPosition(w http.ResponseWriter, r *http.Request) {
	conn, err := h.upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Println("Upgrade failed:", err)
		return
	}
	defer conn.Close()

	h.positionMutex.Lock()
	h.positionClients[conn] = true
	h.positionMutex.Unlock()

	for {
		if _, _, err := conn.ReadMessage(); err != nil {
			break
		}
	}

	h.positionMutex.Lock()
	delete(h.positionClients, conn)
	h.positionMutex.Unlock()
}

func (h *WebSocketHandler) updateClientUnits(w http.ResponseWriter, r *http.Request) {
	conn, err := h.upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Println("Upgrade failed:", err)
		return
	}
	defer conn.Close()

	h.unitCreatedMutex.Lock()
	h.unitCreatedClients[conn] = true
	h.unitCreatedMutex.Unlock()

	for {
		if _, _, err := conn.ReadMessage(); err != nil {
			break
		}
	}

	h.unitCreatedMutex.Lock()
	delete(h.unitCreatedClients, conn)
	h.unitCreatedMutex.Unlock()
}

func (h *WebSocketHandler) logData(unitData app.UnitData) {
	unit, exists := h.state.UnitByID(unitData.ID)
	if !exists {
		log.Printf("unit %s not found", unitData.ID)
		return
	}

	jsonBytes, err := json.Marshal(unit)
	if err != nil {
		log.Printf("error marshaling: %v", err)
		return
	}

	log.Printf("%s", jsonBytes)
}
