package handlers

import (
	"encoding/json"
	"sort"
)

// Use the shared sign-in identity so presence works with any auth provider.
type sessionMember struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	AvatarURL string `json:"avatarUrl,omitempty"`
}

// Each browser subscribes through its board socket. Deduplicate accounts with
// multiple open tabs, and remove a member only after their final tab disconnects.
// The caller holds boardMutex, preserving join/leave order with board messages.
func (h *WebSocketHandler) broadcastPresenceLocked() {
	unique := make(map[string]sessionMember)
	for _, member := range h.presenceClients {
		unique[member.ID] = member
	}
	members := make([]sessionMember, 0, len(unique))
	for _, member := range unique {
		members = append(members, member)
	}
	sort.Slice(members, func(i, j int) bool {
		if members[i].Name == members[j].Name {
			return members[i].ID < members[j].ID
		}
		return members[i].Name < members[j].Name
	})
	message := struct {
		Type    string          `json:"type"`
		Members []sessionMember `json:"members"`
	}{"sessionPresence", members}
	// Presence is opt-in so other board socket consumers retain their protocol.
	data, _ := json.Marshal(message)
	for conn := range h.presenceClients {
		if writer := h.boardClients[conn]; writer != nil {
			writer.send(data)
		}
	}
}
