package auth

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strings"
	"sync"
	"time"

	"golang.org/x/oauth2"
	"golang.org/x/time/rate"
)

type Config struct {
	ClientID     string
	ClientSecret string
	RedirectURL  string
	HTTPClient   *http.Client
	Provider     Provider
	Access       Access
}

type User struct {
	ID      string
	Name    string
	IsGM    bool
	IsAdmin bool
}

type userKey struct{}

func CurrentUser(r *http.Request) *User {
	user, _ := r.Context().Value(userKey{}).(*User)
	return user
}

// WithTableRole assigns permissions from the selected tabletop's owner.
func WithTableRole(r *http.Request, isGM bool) *http.Request {
	user := *CurrentUser(r)
	user.IsGM = isGM
	return r.WithContext(context.WithValue(r.Context(), userKey{}, &user))
}

// Query-based GM mode remains available only without authentication configured.
func IsGM(r *http.Request) bool {
	if user := CurrentUser(r); user != nil {
		return user.IsGM
	}
	return r.URL.Query().Get("gm") == "true"
}

type Manager struct {
	config                  Config
	origin                  string
	secure                  bool
	client                  *http.Client
	oauth                   oauth2.Config
	provider                Provider
	loginPath, callbackPath string
	mu                      sync.Mutex
	sessions                map[string]*session
	states                  map[string]loginAttempt
	loginLimiter            *rate.Limiter
	socketCount             int
}

func FromEnvironment(access ...Access) (*Manager, error) {
	secret, path := os.Getenv("OAUTH2_CLIENT_SECRET"), os.Getenv("OAUTH2_CLIENT_SECRET_FILE")
	if secret != "" && path != "" {
		return nil, errors.New("set only one of OAUTH2_CLIENT_SECRET and OAUTH2_CLIENT_SECRET_FILE")
	}
	if path != "" {
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("read OAUTH2_CLIENT_SECRET_FILE: %w", err)
		}
		secret = strings.TrimSpace(string(data))
	}
	config := Config{ClientID: os.Getenv("OAUTH2_CLIENT_ID"), ClientSecret: secret, RedirectURL: os.Getenv("OAUTH2_REDIRECT_URL")}
	if len(access) > 0 {
		config.Access = access[0]
	}
	if config.ClientID == "" && config.ClientSecret == "" && config.RedirectURL == "" {
		return nil, nil
	}
	return New(config)
}

func New(config Config) (*Manager, error) {
	if config.ClientID == "" || config.ClientSecret == "" || config.RedirectURL == "" {
		return nil, errors.New("set OAUTH2_CLIENT_ID, OAUTH2_REDIRECT_URL, and either OAUTH2_CLIENT_SECRET_FILE or OAUTH2_CLIENT_SECRET")
	}
	redirect, err := url.Parse(config.RedirectURL)
	if err != nil || redirect.Host == "" || redirect.User != nil || redirect.RawQuery != "" || redirect.Fragment != "" || redirect.RawPath != "" || !callbackPathPattern.MatchString(redirect.Path) {
		return nil, errors.New("OAUTH2_REDIRECT_URL must be an absolute URL with a callback path under /auth/, such as /auth/callback")
	}
	loopback := redirect.Hostname() == "localhost" || redirect.Hostname() == "127.0.0.1" || redirect.Hostname() == "::1"
	if redirect.Scheme != "https" && !(redirect.Scheme == "http" && loopback) {
		return nil, errors.New("use HTTPS for OAUTH2_REDIRECT_URL, or HTTP on localhost for development")
	}
	client := config.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	}
	provider := config.Provider
	if provider == nil {
		provider = NewDiscordProvider()
	}
	if provider.Name() == "" || !loginPathPattern.MatchString(provider.LoginPath()) || provider.LoginPath() == redirect.Path || provider.Endpoint().AuthURL == "" || provider.Endpoint().TokenURL == "" {
		return nil, errors.New("provider must define a name, /auth/.../login path, and OAuth endpoints")
	}
	return &Manager{config: config, origin: redirect.Scheme + "://" + redirect.Host, secure: redirect.Scheme == "https", client: client,
		oauth:    oauth2.Config{ClientID: config.ClientID, ClientSecret: config.ClientSecret, RedirectURL: config.RedirectURL, Endpoint: provider.Endpoint(), Scopes: provider.Scopes()},
		provider: provider, loginPath: provider.LoginPath(), callbackPath: redirect.Path,
		sessions: make(map[string]*session), states: make(map[string]loginAttempt), loginLimiter: rate.NewLimiter(2, 20)}, nil
}

var callbackPathPattern = regexp.MustCompile(`^/auth(?:/[A-Za-z0-9_-]+)*/callback$`)
var loginPathPattern = regexp.MustCompile(`^/auth(?:/[A-Za-z0-9_-]+)*/login$`)

func (m *Manager) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /login", m.loginPage)
	mux.HandleFunc("GET "+m.loginPath, m.login)
	mux.HandleFunc("GET "+m.callbackPath, m.callback)
	mux.HandleFunc("POST /auth/logout", m.logout)
	mux.HandleFunc("GET /admin/invites", m.admin)
	mux.HandleFunc("POST /admin/invites", m.admin)
	mux.HandleFunc("POST /admin/invites/{invite}/revoke", m.admin)
}

func (m *Manager) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		// Keep the Origin header on same-origin form posts while withholding
		// referrers from external destinations, including the OAuth provider.
		w.Header().Set("Referrer-Policy", "same-origin")
		w.Header().Set("X-Frame-Options", "SAMEORIGIN")
		if r.Method != "GET" && r.Method != "HEAD" && r.Method != "OPTIONS" {
			if origin := r.Header.Get("Origin"); (origin != "" && origin != m.origin) || r.Header.Get("Sec-Fetch-Site") == "cross-site" {
				http.Error(w, "Request origin does not match this app.", http.StatusForbidden)
				return
			}
		}
		if r.URL.Path == "/login" || r.URL.Path == m.loginPath || r.URL.Path == m.callbackPath || r.URL.Path == "/auth/logout" || strings.HasPrefix(r.URL.Path, "/static/") {
			next.ServeHTTP(w, r)
			return
		}
		token, user := m.authenticate(r)
		if user == nil {
			if r.Method == "GET" && r.Header.Get("HX-Request") != "true" && !strings.EqualFold(r.Header.Get("Upgrade"), "websocket") {
				http.Redirect(w, r, "/login?next="+url.QueryEscape(r.URL.RequestURI()), http.StatusSeeOther)
			} else {
				w.Header().Set("HX-Redirect", "/login")
				http.Error(w, "Sign in to continue.", http.StatusUnauthorized)
			}
			return
		}
		if strings.EqualFold(r.Header.Get("Upgrade"), "websocket") {
			if !m.canOpenSocket(token) {
				http.Error(w, "Too many open connections. Close unused tabs.", http.StatusTooManyRequests)
				return
			}
			w = &socketWriter{ResponseWriter: w, manager: m, token: token}
		} else if !m.allowRequest(token, r) {
			w.Header().Set("Retry-After", "10")
			http.Error(w, "Too many requests. Try again shortly.", http.StatusTooManyRequests)
			return
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), userKey{}, user)))
	})
}
