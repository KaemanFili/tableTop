package auth

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"tableTop/main/internal/app/persistence"
)

func TestInvitationOAuthFlowAndAdminAuthorization(t *testing.T) {
	manager, mux := testManager(t)
	access, _ := persistence.NewAccessLibrary(t.TempDir(), []string{"admin"}, nil)
	manager.config.Access = access
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/token" {
			r.ParseForm()
			json.NewEncoder(w).Encode(map[string]string{"access_token": r.Form.Get("code"), "token_type": "Bearer"})
		} else {
			id := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
			json.NewEncoder(w).Encode(map[string]string{"id": id, "username": id})
		}
	}))
	defer provider.Close()
	manager.oauth.Endpoint.TokenURL = provider.URL + "/token"
	manager.provider.(*discordProvider).userURL = provider.URL + "/me"
	handler := manager.Middleware(mux)
	login := func(id, invite string, status int) *http.Cookie {
		t.Helper()
		page := httptest.NewRecorder()
		handler.ServeHTTP(page, httptest.NewRequest("GET", "/login?invite="+url.QueryEscape(invite), nil))
		if invite != "" && !strings.Contains(page.Body.String(), invite) {
			t.Fatal("invitation lost before OAuth login")
		}
		start := httptest.NewRecorder()
		handler.ServeHTTP(start, httptest.NewRequest("GET", manager.loginPath+"?invite="+url.QueryEscape(invite), nil))
		target, _ := url.Parse(start.Header().Get("Location"))
		if target.Query().Get("invite") != "" {
			t.Fatal("server invitation leaked to provider")
		}
		callback := httptest.NewRequest("GET", manager.callbackPath+"?code="+id+"&state="+target.Query().Get("state"), nil)
		callback.AddCookie(start.Result().Cookies()[0])
		result := httptest.NewRecorder()
		handler.ServeHTTP(result, callback)
		if result.Code != status {
			t.Fatalf("%s callback: %d %s", id, result.Code, result.Body.String())
		}
		for _, cookie := range result.Result().Cookies() {
			if cookie.Name == sessionCookie {
				return cookie
			}
		}
		if status == 303 {
			t.Fatal("admitted user has no session")
		}
		return nil
	}
	if cookie := login("stranger", "", 403); cookie != nil {
		t.Fatal("uninvited user got session")
	}
	admin := login("admin", "", 303)
	code, _ := access.CreateInvitation("admin")
	player := login("player", code, 303)
	login("replay", code, 403)
	login("player", "", 303)
	for _, test := range []struct {
		cookie *http.Cookie
		status int
	}{{admin, 200}, {player, 403}} {
		for _, method := range []string{"GET", "POST"} {
			request := httptest.NewRequest(method, "/admin/invites", nil)
			request.AddCookie(test.cookie)
			result := httptest.NewRecorder()
			handler.ServeHTTP(result, request)
			if result.Code != test.status {
				t.Fatalf("%s admin route: %d", method, result.Code)
			}
		}
	}
}
