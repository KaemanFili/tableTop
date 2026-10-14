package tables

import (
	"github.com/gorilla/websocket"
	"net/http"
	"testing"
	"time"
)

func TestSessionPresenceDeduplicatesAccountsAndTracksDisconnects(t *testing.T) {
	f := newTableTestFixture(t)
	owner, player := f.login("owner"), f.login("player")
	first := f.createSession(t, owner, "First")
	f.joinSession(player, first)
	read := func(conn *websocket.Conn, names ...string) {
		t.Helper()
		conn.SetReadDeadline(time.Now().Add(3 * time.Second))
		var event struct {
			Type    string
			Members []struct{ ID, Name, AvatarURL string }
		}
		if err := conn.ReadJSON(&event); err != nil {
			t.Fatal(err)
		}
		if event.Type != "sessionPresence" || len(event.Members) != len(names) {
			t.Fatalf("presence = %+v, want %v", event, names)
		}
		for i, name := range names {
			wantAvatar := ""
			if name == "owner" {
				wantAvatar = "https://cdn.discordapp.com/avatars/owner/0123456789abcdef0123456789abcdef.png?size=64"
			}
			if event.Members[i].AvatarURL != wantAvatar {
				t.Fatalf("presence avatar = %q, want %q", event.Members[i].AvatarURL, wantAvatar)
			}
			if event.Members[i].Name != name || event.Members[i].ID == "" {
				t.Fatalf("member = %+v, want %s", event.Members[i], name)
			}
		}
	}
	a := f.connect(owner, first.path, true)
	read(a, "owner")
	b := f.connect(owner, first.path, true)
	read(a, "owner")
	read(b, "owner")
	c := f.connect(player, first.path, true)
	read(a, "owner", "player")
	read(b, "owner", "player")
	read(c, "owner", "player")
	b.Close()
	read(a, "owner", "player")
	read(c, "owner", "player")
	c.Close()
	read(a, "owner")
	second := f.createSession(t, owner, "Second")
	other := f.connect(owner, second.path, true)
	read(other, "owner")
	// A new tabletop must not add a presence event to the first tabletop.
	f.request(owner, "POST", first.path+"dice/roll", `{"requestId":"barrier","groups":[{"qty":1,"sides":6,"modifier":0}]}`, http.StatusOK)
	var event struct{ Type, RollerName string }
	if err := a.ReadJSON(&event); err != nil || event.Type != "diceRolled" || event.RollerName != "owner" {
		t.Fatalf("unexpected event: %+v, %v", event, err)
	}
}
