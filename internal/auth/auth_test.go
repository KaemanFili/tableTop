package auth

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

func testManager(t *testing.T) (*Manager, *http.ServeMux) {
	t.Helper()
	manager, err := New(Config{ClientID: "client-id", ClientSecret: "test-secret", RedirectURL: "http://localhost:18080/auth/discord/callback"})
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	manager.RegisterRoutes(mux)
	t.Cleanup(func() {
		manager.mu.Lock()
		tokens := make([]string, 0, len(manager.sessions))
		for token := range manager.sessions {
			tokens = append(tokens, token)
		}
		manager.mu.Unlock()
		for _, token := range tokens {
			manager.removeSession(token)
		}
	})
	return manager, mux
}

func beginLogin(t *testing.T, m *Manager, next string) (*http.Cookie, string) {
	t.Helper()
	response := httptest.NewRecorder()
	m.login(response, httptest.NewRequest("GET", "/auth/discord/login?next="+url.QueryEscape(next), nil))
	target, err := url.Parse(response.Header().Get("Location"))
	if err != nil {
		t.Fatal(err)
	}
	query := target.Query()
	if query.Get("client_id") != "client-id" || query.Get("scope") != "identify" || query.Get("redirect_uri") != m.config.RedirectURL || query.Get("response_type") != "code" {
		t.Fatalf("invalid OAuth request: %s", target)
	}
	cookies := response.Result().Cookies()
	if len(cookies) != 1 || !cookies[0].HttpOnly || cookies[0].SameSite != http.SameSiteLaxMode || cookies[0].MaxAge != 600 {
		t.Fatalf("invalid state cookie: %+v", cookies)
	}
	return cookies[0], query.Get("state")
}

func TestDiscordFlowStateProfileSessionAndReplay(t *testing.T) {
	manager, mux := testManager(t)
	var calls atomic.Int32
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/token" {
			r.ParseForm()
			if r.Method != "POST" || r.Header.Get("Content-Type") != "application/x-www-form-urlencoded" || r.Form.Get("client_secret") != "test-secret" || r.Form.Get("grant_type") != "authorization_code" || r.Form.Get("code") != "good" || r.Form.Get("redirect_uri") != manager.config.RedirectURL {
				t.Error("invalid token exchange")
			}
			w.Write([]byte(`{"access_token":"test-token","token_type":"Bearer"}`))
		} else {
			if r.Header.Get("Authorization") != "Bearer test-token" {
				t.Error("missing bearer token")
			}
			w.Write([]byte(`{"id":"123","username":"player","global_name":"Player <name>"}`))
		}
	}))
	defer provider.Close()
	manager.oauth.Endpoint.TokenURL = provider.URL + "/token"
	manager.provider.(*discordProvider).userURL = provider.URL + "/me"
	stateCookie, state := beginLogin(t, manager, "/sessions/ABCDEFGHIJKLMNOP/")
	invalid := httptest.NewRequest("GET", "/auth/discord/callback?code=good&state=wrong", nil)
	invalid.AddCookie(stateCookie)
	response := httptest.NewRecorder()
	manager.callback(response, invalid)
	if response.Code != 400 || calls.Load() != 0 {
		t.Fatal("invalid state reached Discord")
	}
	callback := httptest.NewRequest("GET", "/auth/discord/callback?code=good&state="+state, nil)
	callback.AddCookie(stateCookie)
	response = httptest.NewRecorder()
	manager.callback(response, callback)
	if response.Code != 303 || response.Header().Get("Location") != "/sessions/ABCDEFGHIJKLMNOP/" || calls.Load() != 2 {
		t.Fatalf("callback failed: %d %s", response.Code, response.Body.String())
	}
	var session *http.Cookie
	for _, cookie := range response.Result().Cookies() {
		if cookie.Name == sessionCookie {
			session = cookie
		}
	}
	if session == nil || session.Value == "test-token" || !session.HttpOnly || session.MaxAge != 43200 {
		t.Fatalf("invalid session cookie: %+v", session)
	}
	mux.HandleFunc("GET /board", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(CurrentUser(r))
		if IsGM(r) {
			t.Error("query parameter elevated a player")
		}
	})
	request := httptest.NewRequest("GET", "/board?gm=true", nil)
	request.AddCookie(session)
	response = httptest.NewRecorder()
	manager.Middleware(mux).ServeHTTP(response, request)
	if response.Code != 200 || !strings.Contains(response.Body.String(), "123") {
		t.Fatal("session did not authenticate")
	}
	response = httptest.NewRecorder()
	manager.callback(response, callback)
	if response.Code != 400 || calls.Load() != 2 {
		t.Fatal("OAuth state replay accepted")
	}
	// Logout invalidates server state, including a copied old cookie.
	logout := httptest.NewRequest("POST", "/auth/logout", nil)
	logout.AddCookie(session)
	response = httptest.NewRecorder()
	manager.Middleware(mux).ServeHTTP(response, logout)
	response = httptest.NewRecorder()
	manager.Middleware(mux).ServeHTTP(response, request)
	if response.Code != 303 {
		t.Fatal("logged-out session remained valid")
	}
}

func TestMiddlewareFormOriginPolicy(t *testing.T) {
	manager, _ := testManager(t)
	handler := manager.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	page := httptest.NewRecorder()
	handler.ServeHTTP(page, httptest.NewRequest("GET", "http://localhost:18080/login", nil))
	// no-referrer causes browser form submissions to send Origin: null.
	if got := page.Header().Get("Referrer-Policy"); got != "same-origin" {
		t.Fatalf("form page referrer policy = %q, want same-origin", got)
	}
	for _, test := range []struct {
		name, origin, fetchSite string
		status                  int
	}{
		{"local form", "http://localhost:18080", "same-origin", http.StatusNoContent},
		{"opaque origin", "null", "same-origin", http.StatusForbidden},
		{"foreign origin", "https://another-site.example", "cross-site", http.StatusForbidden},
		{"wrong port", "http://localhost:8080", "same-site", http.StatusForbidden},
		{"cross-site metadata", "http://localhost:18080", "cross-site", http.StatusForbidden},
	} {
		t.Run(test.name, func(t *testing.T) {
			request := httptest.NewRequest("POST", "http://localhost:18080/auth/logout", nil)
			request.Header.Set("Origin", test.origin)
			request.Header.Set("Sec-Fetch-Site", test.fetchSite)
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			if response.Code != test.status {
				t.Fatalf("status = %d, want %d", response.Code, test.status)
			}
		})
	}
}

func TestMiddlewareDeniesAnonymousMutationSocketsAndCrossOrigin(t *testing.T) {
	manager, mux := testManager(t)
	calls := 0
	mux.HandleFunc("/board", func(w http.ResponseWriter, r *http.Request) { calls++ })
	for _, test := range []struct {
		method  string
		upgrade bool
		status  int
	}{{"GET", false, 303}, {"POST", false, 401}, {"GET", true, 401}} {
		request := httptest.NewRequest(test.method, "/board", nil)
		if test.upgrade {
			request.Header.Set("Upgrade", "websocket")
		}
		response := httptest.NewRecorder()
		manager.Middleware(mux).ServeHTTP(response, request)
		if response.Code != test.status {
			t.Fatalf("anonymous status %d", response.Code)
		}
	}
	request := httptest.NewRequest("POST", "/auth/logout", nil)
	request.Header.Set("Origin", "https://another-site.example")
	response := httptest.NewRecorder()
	manager.Middleware(mux).ServeHTTP(response, request)
	if response.Code != 403 || calls != 0 {
		t.Fatal("unauthorized action reached handler")
	}
	cookie, state := beginLogin(t, manager, "https://another-site.example")
	manager.mu.Lock()
	attempt := manager.states[state]
	attempt.Expires = time.Now().Add(-time.Minute)
	manager.states[state] = attempt
	manager.mu.Unlock()
	request = httptest.NewRequest("GET", "/auth/discord/callback?state="+state+"&code=anything", nil)
	request.AddCookie(cookie)
	response = httptest.NewRecorder()
	manager.callback(response, request)
	if response.Code != 400 {
		t.Fatal("expired OAuth state accepted")
	}
	for _, unsafe := range []string{"https://bad.example/", "//bad.example/", "/auth/logout", "/sessions/\\\\bad"} {
		if safeReturn(unsafe) != "" {
			t.Fatalf("unsafe return URL accepted: %s", unsafe)
		}
	}
}

func TestLogoutAndExpiryCloseExistingSockets(t *testing.T) {
	for _, action := range []string{"logout", "expiry"} {
		t.Run(action, func(t *testing.T) {
			manager, mux := testManager(t)
			mux.HandleFunc("/ws", func(w http.ResponseWriter, r *http.Request) {
				conn, err := (&websocket.Upgrader{}).Upgrade(w, r, nil)
				if err != nil {
					return
				}
				defer conn.Close()
				for {
					if _, _, err := conn.ReadMessage(); err != nil {
						return
					}
				}
			})
			response := httptest.NewRecorder()
			manager.newSession(response, User{ID: "123", Name: "Player"})
			cookie := response.Result().Cookies()[0]
			server := httptest.NewServer(manager.Middleware(mux))
			defer server.Close()
			headers := http.Header{"Cookie": {cookie.Name + "=" + cookie.Value}}
			conn, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(server.URL, "http")+"/ws", headers)
			if err != nil {
				t.Fatal(err)
			}
			defer conn.Close()
			if action == "logout" {
				request := httptest.NewRequest("POST", "/auth/logout", nil)
				request.AddCookie(cookie)
				manager.logout(httptest.NewRecorder(), request)
			} else {
				manager.removeSession(cookie.Value)
			}
			conn.SetReadDeadline(time.Now().Add(time.Second))
			if _, _, err := conn.ReadMessage(); err == nil {
				t.Fatal("revoked websocket remained open")
			}
		})
	}
}

func TestClosedSocketsReleaseSessionReferences(t *testing.T) {
	manager, mux := testManager(t)
	mux.HandleFunc("/ws", func(w http.ResponseWriter, r *http.Request) {
		conn, err := (&websocket.Upgrader{}).Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				return
			}
		}
	})
	response := httptest.NewRecorder()
	manager.newSession(response, User{ID: "123", Name: "Player"})
	cookie := response.Result().Cookies()[0]
	server := httptest.NewServer(manager.Middleware(mux))
	defer server.Close()
	for range 3 {
		conn, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(server.URL, "http")+"/ws", http.Header{"Cookie": {cookie.String()}})
		if err != nil {
			t.Fatal(err)
		}
		manager.mu.Lock()
		count := len(manager.sessions[cookie.Value].connections)
		manager.mu.Unlock()
		if count != 1 {
			conn.Close()
			t.Fatalf("tracked connections = %d, want 1", count)
		}
		conn.Close()
		deadline := time.Now().Add(time.Second)
		for {
			manager.mu.Lock()
			count = len(manager.sessions[cookie.Value].connections)
			manager.mu.Unlock()
			if count == 0 {
				break
			}
			if time.Now().After(deadline) {
				t.Fatal("closed socket remained attached to the login session")
			}
			time.Sleep(time.Millisecond)
		}
	}
}

func TestConfigurationAndSecureCookie(t *testing.T) {
	for _, config := range []Config{{ClientID: "one"}, {ClientID: "one", ClientSecret: "secret", RedirectURL: "http://public.example/auth/discord/callback"}, {ClientID: "one", ClientSecret: "secret", RedirectURL: "https://public.example/wrong"}} {
		if _, err := New(config); err == nil {
			t.Fatal("invalid configuration accepted")
		}
	}
	manager, err := New(Config{ClientID: "one", ClientSecret: "secret", RedirectURL: "https://table.example/auth/discord/callback"})
	if err != nil {
		t.Fatal(err)
	}
	response := httptest.NewRecorder()
	manager.setCookie(response, sessionCookie, "opaque", 43200)
	if !response.Result().Cookies()[0].Secure {
		t.Fatal("HTTPS session cookie was not secure")
	}
}

func TestDiscordFailureDoesNotCreateSession(t *testing.T) {
	for _, test := range []struct {
		name, token, profile string
		status               int
	}{
		{"provider error", `{}`, `{}`, http.StatusBadGateway},
		{"invalid token JSON", `not-json`, `{}`, http.StatusOK},
		{"empty token", `{"token_type":"Bearer"}`, `{}`, http.StatusOK},
		{"missing token type", `{"access_token":"token"}`, `{}`, http.StatusOK},
		{"unsupported token type", `{"access_token":"token","token_type":"MAC"}`, `{}`, http.StatusOK},
		{"missing user identity", `{"access_token":"token","token_type":"Bearer"}`, `{"username":"player"}`, http.StatusOK},
	} {
		t.Run(test.name, func(t *testing.T) {
			manager, _ := testManager(t)
			provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/token" {
					w.WriteHeader(test.status)
					w.Write([]byte(test.token))
				} else {
					w.Write([]byte(test.profile))
				}
			}))
			defer provider.Close()
			manager.oauth.Endpoint.TokenURL = provider.URL + "/token"
			manager.provider.(*discordProvider).userURL = provider.URL + "/me"
			cookie, state := beginLogin(t, manager, "")
			request := httptest.NewRequest("GET", "/auth/discord/callback?code=test&state="+state, nil)
			request.AddCookie(cookie)
			response := httptest.NewRecorder()
			manager.callback(response, request)
			if response.Code != http.StatusBadGateway || !strings.Contains(response.Body.String(), "Sign in with Discord") {
				t.Fatal("failed login did not offer retry")
			}
			for _, cookie := range response.Result().Cookies() {
				if cookie.Name == sessionCookie {
					t.Fatal("failed provider login created a session")
				}
			}
		})
	}
}

func TestEnvironmentSecretFile(t *testing.T) {
	for _, name := range []string{"OAUTH2_CLIENT_ID", "OAUTH2_CLIENT_SECRET", "OAUTH2_CLIENT_SECRET_FILE", "OAUTH2_REDIRECT_URL"} {
		t.Setenv(name, "")
	}
	manager, err := FromEnvironment()
	if err != nil || manager != nil {
		t.Fatal("empty configuration should retain local mode")
	}
	path := filepath.Join(t.TempDir(), "oauth2-secret")
	if err := os.WriteFile(path, []byte("test$secret\n"), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("OAUTH2_CLIENT_SECRET_FILE", path)
	t.Setenv("OAUTH2_CLIENT_ID", "client")
	t.Setenv("OAUTH2_REDIRECT_URL", "http://localhost:18080/auth/discord/callback")
	manager, err = FromEnvironment()
	if err != nil || manager == nil || manager.config.ClientSecret != "test$secret" {
		t.Fatalf("secret-file configuration failed: %v", err)
	}
	t.Setenv("OAUTH2_CLIENT_SECRET", "direct-secret")
	if _, err := FromEnvironment(); err == nil {
		t.Fatal("conflicting secret sources accepted")
	}
	t.Setenv("OAUTH2_CLIENT_SECRET", "")
	t.Setenv("OAUTH2_CLIENT_SECRET_FILE", path+"-missing")
	if _, err := FromEnvironment(); err == nil {
		t.Fatal("missing secret file accepted")
	}
	t.Setenv("OAUTH2_CLIENT_SECRET_FILE", path)
	if err := os.WriteFile(path, []byte("\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := FromEnvironment(); err == nil {
		t.Fatal("empty secret accepted with Discord configured")
	}
	t.Setenv("OAUTH2_CLIENT_ID", "")
	t.Setenv("OAUTH2_REDIRECT_URL", "")
	manager, err = FromEnvironment()
	if err != nil || manager != nil {
		t.Fatal("empty mounted secret should retain local mode when Discord is disabled")
	}
	t.Setenv("OAUTH2_CLIENT_SECRET_FILE", "")
	t.Setenv("OAUTH2_CLIENT_ID", "client")
	t.Setenv("OAUTH2_REDIRECT_URL", "http://localhost:18080/auth/discord/callback")
	t.Setenv("OAUTH2_CLIENT_SECRET", "direct-secret")
	manager, err = FromEnvironment()
	if err != nil || manager.config.ClientSecret != "direct-secret" {
		t.Fatal("host environment configuration no longer works")
	}
}
