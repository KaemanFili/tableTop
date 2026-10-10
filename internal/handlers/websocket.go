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
	strokes            map[string]penStroke
	erasedStrokes      map[string]bool
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
		strokes:            make(map[string]penStroke),
		erasedStrokes:      make(map[string]bool),
	}
}

func (h *WebSocketHandler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/ws/updateServer", h.updateServer)
	mux.HandleFunc("/ws/updatePosForClients", h.updateClientPosition)
	mux.HandleFunc("/ws/updateUnitsForClients", h.updateBoardUtilityValues)
}

func (h *WebSocketHandler) StartPositionBroadcast() {
	go func() {
		ticker := time.NewTicker(33 * time.Millisecond)
		defer ticker.Stop()

		for range ticker.C {
			data, err := json.Marshal(h.state.Units())
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

func (h *WebSocketHandler) updateBoardUtilityValues(w http.ResponseWriter, r *http.Request) {
	conn, err := h.upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Println("Upgrade failed:", err)
		return
	}
	defer conn.Close()
	conn.SetReadLimit(1024)
	sessionID := rand.Text()
	defer h.unregisterBoardUtilitiesClient(conn, sessionID)

	if err := h.registerBoardUtilitiesClient(conn, sessionID); err != nil {
		log.Println("Send initial board state:", err)
		return
	}
	h.readBoardUtilitiesMessages(conn, sessionID)
}

// Registration and the initial state share a lock so live updates cannot
// arrive before the session identity and existing drawings.
func (h *WebSocketHandler) registerBoardUtilitiesClient(conn *websocket.Conn, sessionID string) error {
	h.unitCreatedMutex.Lock()
	defer h.unitCreatedMutex.Unlock()
	h.unitCreatedClients[conn] = true
	if err := conn.WriteJSON(map[string]string{"type": "rulerSession", "id": sessionID}); err != nil {
		return err
	}
	for _, ruler := range h.rulers {
		if err := conn.WriteJSON(ruler); err != nil {
			return err
		}
	}
	for _, stroke := range h.strokes {
		if err := conn.WriteJSON(stroke); err != nil {
			return err
		}
	}
	return nil
}

func (h *WebSocketHandler) readBoardUtilitiesMessages(conn *websocket.Conn, sessionID string) {
	for {
		_, data, err := conn.ReadMessage()
		if err != nil {
			return
		}
		h.handleBoardUtilitiesMessage(sessionID, data)
	}
}

func (h *WebSocketHandler) handleBoardUtilitiesMessage(sessionID string, data []byte) {
	var message struct {
		Type string `json:"type"`
	}
	if json.Unmarshal(data, &message) != nil {
		return
	}
	switch message.Type {
	case "rulerChanged":
		h.updateRuler(sessionID, data)
	case "penStroke":
		h.updatePen(sessionID, data)
	case "penErase":
		h.erasePen(data)
	}
}

func (h *WebSocketHandler) unregisterBoardUtilitiesClient(conn *websocket.Conn, sessionID string) {
	h.unitCreatedMutex.Lock()
	defer h.unitCreatedMutex.Unlock()
	delete(h.unitCreatedClients, conn)
	if _, exists := h.rulers[sessionID]; exists {
		delete(h.rulers, sessionID)
		data, _ := json.Marshal(boardRuler{Type: "rulerChanged", ID: sessionID, Clear: true})
		h.broadcastUnitsLocked(data)
	}
	for id, stroke := range h.strokes {
		if stroke.Owner == sessionID && !stroke.Finished {
			stroke.Finished = true
			h.strokes[id] = stroke
			message, _ := json.Marshal(penStroke{Type: "penStroke", ID: id, Owner: sessionID, Offset: len(stroke.Points), Finished: true, Color: stroke.Color, Width: stroke.Width})
			h.broadcastUnitsLocked(message)
		}
	}
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
