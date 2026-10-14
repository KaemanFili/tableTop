package auth

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestDiscordProfileAvatar(t *testing.T) {
	for _, avatar := range []string{"", "0123456789abcdef0123456789abcdef", "a_0123456789abcdef0123456789abcdef"} {
		t.Run(avatar, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				fmt.Fprintf(w, `{"id":"123","username":"player","global_name":"Player","avatar":%q}`, avatar)
			}))
			defer server.Close()
			provider := discordProvider{userURL: server.URL}
			user, err := provider.User(context.Background(), server.Client())
			if err != nil {
				t.Fatal(err)
			}
			expected := ""
			if avatar != "" {
				expected = "https://cdn.discordapp.com/avatars/123/" + avatar + ".png?size=64"
			}
			if user.AvatarURL != expected || user.Name != "Player" {
				t.Fatalf("profile = %+v, want avatar %q", user, expected)
			}
		})
	}
}
