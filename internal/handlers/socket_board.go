package handlers

import (
	"crypto/rand"
	"encoding/json"
	"log"
	"net/http"

	"github.com/gorilla/websocket"
	"golang.org/x/time/rate"

	"tableTop/main/internal/auth"
)

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

	if err := h.registerBoardUtilitiesClient(conn, sessionID, r); err != nil {
		log.Println("Send initial board state:", err)
		return
	}
	h.readBoardUtilitiesMessages(conn, sessionID, auth.IsGM(r))
}

// Registration and the initial state share a lock so live updates cannot
// arrive before the session identity and existing drawings.
func (h *WebSocketHandler) registerBoardUtilitiesClient(conn *websocket.Conn, sessionID string, r *http.Request) error {
	h.boardMutex.Lock()
	defer h.boardMutex.Unlock()
	initial := make([][]byte, 0, 2+len(h.rulers)+len(h.strokes)+len(h.fogMarks))
	appendInitial := func(value any) error {
		data, err := json.Marshal(value)
		if err == nil {
			initial = append(initial, data)
		}
		return err
	}
	if err := appendInitial(map[string]any{"type": "rulerSession", "id": sessionID, "generation": h.boardGeneration, "map": h.currentMap}); err != nil {
		return err
	}
	if h.currentMap != nil {
		if err := appendInitial(map[string]any{"type": "boardMapChanged", "map": h.currentMap}); err != nil {
			return err
		}
	}
	for _, ruler := range h.rulers {
		if err := appendInitial(ruler); err != nil {
			return err
		}
	}
	for _, stroke := range h.strokes {
		if err := appendInitial(stroke); err != nil {
			return err
		}
	}
	if len(h.fogMarks) > 0 {
		if err := appendInitial(map[string]string{"type": "fogReset"}); err != nil {
			return err
		}
		for _, mark := range h.fogMarks {
			if err := appendInitial(mark); err != nil {
				return err
			}
		}
	}
	h.boardClients[conn] = newSocketWriter(conn, initial, false)
	if r.URL.Query().Get("presence") == "true" {
		member := sessionMember{ID: sessionID, Name: "Player"}
		if user := auth.CurrentUser(r); user != nil {
			member.ID, member.Name, member.AvatarURL = user.ID, user.Name, user.AvatarURL
		} else if auth.IsGM(r) {
			member.Name = "GM"
		}
		h.presenceClients[conn] = member
		h.broadcastPresenceLocked()
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
			data, _ := json.Marshal(map[string]string{"type": "boardLimitReached", "message": err.Error()})
			if writer := h.boardClients[conn]; writer != nil {
				writer.send(data)
			}
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
	case "fogSet":
		if isGM {
			h.setFog(sessionID, data)
		}
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
	if writer := h.boardClients[conn]; writer != nil {
		writer.stop()
	}
	delete(h.boardClients, conn)
	if _, exists := h.presenceClients[conn]; exists {
		delete(h.presenceClients, conn)
		h.broadcastPresenceLocked()
	}
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
