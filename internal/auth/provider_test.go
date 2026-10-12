package auth

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"golang.org/x/oauth2"
)

type exampleProvider struct{ baseURL string }

func (exampleProvider) Name() string      { return "Example" }
func (exampleProvider) LoginPath() string { return "/auth/login" }
func (exampleProvider) Scopes() []string  { return []string{"profile", "read"} }
func (p exampleProvider) Endpoint() oauth2.Endpoint {
	return oauth2.Endpoint{AuthURL: p.baseURL + "/authorize", TokenURL: p.baseURL + "/token", AuthStyle: oauth2.AuthStyleInHeader}
}
func (p exampleProvider) User(ctx context.Context, client *http.Client) (User, error) {
	request, err := http.NewRequestWithContext(ctx, "GET", p.baseURL+"/profile", nil)
	if err != nil {
		return User{}, err
	}
	response, err := client.Do(request)
	if err != nil {
		return User{}, err
	}
	defer response.Body.Close()
	var user User
	err = json.NewDecoder(response.Body).Decode(&user)
	return user, err
}

func TestProviderIndependentOAuthFlow(t *testing.T) {
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/token":
			id, secret, ok := r.BasicAuth()
			r.ParseForm()
			if !ok || id != "example-client" || secret != "example-secret" || r.Method != "POST" || r.Form.Get("code") != "good" || r.Form.Get("redirect_uri") != "http://localhost:18080/auth/callback" {
				t.Error("invalid OAuth exchange for example provider")
			}
			// Exercise the library's form-encoded token response support.
			w.Header().Set("Content-Type", "application/x-www-form-urlencoded")
			w.Write([]byte("access_token=example-token&token_type=Bearer"))
		case "/profile":
			if r.Header.Get("Authorization") != "Bearer example-token" {
				t.Error("profile request lacks library-managed bearer token")
			}
			w.Write([]byte(`{"ID":"example:123","Name":"Example player","IsGM":true}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer provider.Close()
	manager, err := New(Config{ClientID: "example-client", ClientSecret: "example-secret", RedirectURL: "http://localhost:18080/auth/callback", Provider: exampleProvider{provider.URL}})
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	manager.RegisterRoutes(mux)
	mux.HandleFunc("GET /sessions/test/", func(w http.ResponseWriter, r *http.Request) {
		user := CurrentUser(r)
		if user.ID != "example:123" || user.Name != "Example player" || IsGM(r) {
			t.Errorf("unexpected authenticated user: %+v", user)
		}
	})
	handler := manager.Middleware(mux)
	page := httptest.NewRecorder()
	handler.ServeHTTP(page, httptest.NewRequest("GET", "/login", nil))
	if !strings.Contains(page.Body.String(), "Sign in with Example") || !strings.Contains(page.Body.String(), `href="/auth/login"`) {
		t.Fatal("login page assumes Discord")
	}
	login := httptest.NewRecorder()
	handler.ServeHTTP(login, httptest.NewRequest("GET", "/auth/login", nil))
	target, _ := url.Parse(login.Header().Get("Location"))
	if target.Scheme+"://"+target.Host+target.Path != provider.URL+"/authorize" || target.Query().Get("scope") != "profile read" {
		t.Fatal("authorization URL does not use provider configuration")
	}
	callback := httptest.NewRequest("GET", "/auth/callback?code=good&state="+target.Query().Get("state"), nil)
	callback.AddCookie(login.Result().Cookies()[0])
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, callback)
	if response.Code != http.StatusSeeOther {
		t.Fatalf("callback failed: %d %s", response.Code, response.Body.String())
	}
	for _, cookie := range response.Result().Cookies() {
		if cookie.Name != sessionCookie {
			continue
		}
		defer manager.removeSession(cookie.Value)
		request := httptest.NewRequest("GET", "/sessions/test/?gm=true", nil)
		request.AddCookie(cookie)
		authenticated := httptest.NewRecorder()
		handler.ServeHTTP(authenticated, request)
		if authenticated.Code != http.StatusOK {
			t.Fatalf("provider session did not authenticate: %d", authenticated.Code)
		}
		return
	}
	t.Fatal("callback did not create an application session")
}
