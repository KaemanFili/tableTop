package handlers

import (
	"crypto/rand"
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
	rulers             map[string]boardRuler
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
		rulers:             make(map[string]boardRuler),
	}
}

func (h *WebSocketHandler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/ws/updateServer", h.updateServer)
	mux.HandleFunc("/ws/updatePosForClients", h.updateClientPosition)
	mux.HandleFunc("/ws/updateUnitsForClients", h.updateClientUnits)
}

func (h *WebSocketHandler) StartPositionBroadcast() {
	go func() {
		ticker := time.NewTicker(33 * time.Millisecond)
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
	h.unitCreatedMutex.Lock()
	defer h.unitCreatedMutex.Unlock()
	h.broadcastUnitsLocked(data)
}

// broadcastUnitsLocked requires unitCreatedMutex to serialize writes to each socket.
func (h *WebSocketHandler) broadcastUnitsLocked(data []byte) {
	for conn := range h.unitCreatedClients {
		if err := conn.WriteMessage(websocket.TextMessage, data); err != nil {
			log.Println("Write error, removing client:", err)
			conn.Close()
			delete(h.unitCreatedClients, conn)
		}
	}
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
		h.logData(unitData.ID)

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
	conn.SetReadLimit(1024)
	rulerID := rand.Text()

	h.unitCreatedMutex.Lock()
	h.unitCreatedClients[conn] = true
	conn.WriteJSON(map[string]string{"type": "rulerSession", "id": rulerID})
	for _, ruler := range h.rulers {
		conn.WriteJSON(ruler)
	}
	h.unitCreatedMutex.Unlock()

	for {
		_, data, err := conn.ReadMessage()
		if err != nil {
			break
		}
		h.updateRuler(rulerID, data)
	}

	h.unitCreatedMutex.Lock()
	delete(h.unitCreatedClients, conn)
	if _, exists := h.rulers[rulerID]; exists {
		delete(h.rulers, rulerID)
		data, _ := json.Marshal(boardRuler{Type: "rulerChanged", ID: rulerID, Clear: true})
		h.broadcastUnitsLocked(data)
	}
	h.unitCreatedMutex.Unlock()
}

func (h *WebSocketHandler) logData(id string) {
	unit, exists := h.state.UnitByID(id)
	if !exists {
		log.Printf("unit %s not found", id)
		return
	}

	jsonBytes, err := json.Marshal(unit)
	if err != nil {
		log.Printf("error marshaling: %v", err)
		return
	}

	log.Printf("%s", jsonBytes)
}
