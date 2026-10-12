package auth

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"html/template"
	"net/http"
	"net/url"
	"strings"
	"time"

	"golang.org/x/oauth2"
)

const stateCookie = "tabletop_oauth_state"
const stateLifetime = 10 * time.Minute

type loginAttempt struct {
	Expires  time.Time
	ReturnTo string
	Invite   string
}
type loginData struct{ Error, LoginURL, ProviderName string }

func safeReturn(path string) string {
	parsed, err := url.Parse(path)
	if err != nil || parsed.IsAbs() || parsed.Host != "" || !strings.HasPrefix(parsed.Path, "/sessions/") || strings.Contains(parsed.Path, "\\") {
		return ""
	}
	return parsed.RequestURI()
}

var loginTemplate = template.Must(template.New("login").Parse(`<!doctype html><html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><title>Sign in · tableTop</title><link rel="stylesheet" href="/static/style.css?v=42"></head><body class="login-page"><main><h1>tableTop</h1><p>Sign in to join the tabletop.</p>{{if .Error}}<p role="alert">{{.Error}}</p>{{end}}<a class="oauth-login" href="{{.LoginURL}}">Sign in with {{.ProviderName}}</a></main></body></html>`))

func (m *Manager) loginPage(w http.ResponseWriter, r *http.Request) {
	if _, user := m.authenticate(r); user != nil {
		http.Redirect(w, r, "/lobby", http.StatusSeeOther)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	link := m.loginPath
	query := url.Values{}
	if invite := r.URL.Query().Get("invite"); invite != "" && len(invite) <= 128 {
		query.Set("invite", invite)
	}
	if next := safeReturn(r.URL.Query().Get("next")); next != "" {
		query.Set("next", next)
	}
	if len(query) > 0 {
		link += "?" + query.Encode()
	}
	loginTemplate.Execute(w, loginData{LoginURL: link, ProviderName: m.provider.Name()})
}

func (m *Manager) login(w http.ResponseWriter, r *http.Request) {
	if !m.loginLimiter.Allow() {
		w.Header().Set("Retry-After", "1")
		http.Error(w, "Too many sign-in attempts. Try again shortly.", http.StatusTooManyRequests)
		return
	}
	state := rand.Text()
	now := time.Now()
	m.mu.Lock()
	for key, attempt := range m.states {
		if !now.Before(attempt.Expires) {
			delete(m.states, key)
		}
	}
	invite := r.URL.Query().Get("invite")
	if len(invite) > 128 {
		invite = ""
	}
	if len(m.states) >= 1000 {
		m.mu.Unlock()
		http.Error(w, "Too many sign-in attempts. Try again shortly.", http.StatusTooManyRequests)
		return
	}
	m.states[state] = loginAttempt{Expires: now.Add(stateLifetime), ReturnTo: safeReturn(r.URL.Query().Get("next")), Invite: invite}
	m.mu.Unlock()
	m.setCookie(w, stateCookie, state, int(stateLifetime.Seconds()))
	http.Redirect(w, r, m.oauth.AuthCodeURL(state), http.StatusFound)
}

func (m *Manager) callback(w http.ResponseWriter, r *http.Request) {
	if !m.loginLimiter.Allow() {
		w.Header().Set("Retry-After", "1")
		http.Error(w, "Too many sign-in attempts. Try again shortly.", http.StatusTooManyRequests)
		return
	}
	cookie, err := r.Cookie(stateCookie)
	state := r.URL.Query().Get("state")
	returnTo := "/lobby"
	invite := ""
	valid := err == nil && state != "" && subtle.ConstantTimeCompare([]byte(cookie.Value), []byte(state)) == 1
	if valid {
		m.mu.Lock()
		attempt, exists := m.states[state]
		delete(m.states, state)
		m.mu.Unlock()
		valid = exists && time.Now().Before(attempt.Expires)
		if valid {
			invite = attempt.Invite
		}
		if valid && attempt.ReturnTo != "" {
			returnTo = attempt.ReturnTo
		}
	}
	m.setCookie(w, stateCookie, "", -1)
	if !valid {
		http.Error(w, "Sign-in expired or invalid. Start again from /login.", http.StatusBadRequest)
		return
	}
	if r.URL.Query().Get("error") != "" || r.URL.Query().Get("code") == "" {
		m.loginError(w, "Sign-in was cancelled. You can try again.", http.StatusBadRequest)
		return
	}
	ctx := context.WithValue(r.Context(), oauth2.HTTPClient, m.client)
	token, err := m.oauth.Exchange(ctx, r.URL.Query().Get("code"))
	if err != nil || token.AccessToken == "" || !strings.EqualFold(token.TokenType, "Bearer") {
		m.loginError(w, "Sign-in failed. Please try again.", http.StatusBadGateway)
		return
	}
	// Profile retrieval uses the library's authenticated transport with the
	// same timeout and redirect policy as the token exchange.
	client := oauth2.NewClient(ctx, oauth2.StaticTokenSource(token))
	user, err := m.provider.User(ctx, client)
	if err != nil || user.ID == "" || user.Name == "" {
		m.loginError(w, "Sign-in failed. Please try again.", http.StatusBadGateway)
		return
	}
	if m.config.Access != nil {
		if err := m.config.Access.Admit(user.ID, invite); err != nil {
			m.loginError(w, "An administrator invitation is required to join this server. Ask your host for a new invitation link.", http.StatusForbidden)
			return
		}
		user.IsAdmin = m.config.Access.IsAdmin(user.ID)
	}
	if old, err := r.Cookie(sessionCookie); err == nil {
		m.removeSession(old.Value)
	}
	user.IsGM = false
	if !m.newSession(w, user) {
		m.loginError(w, "Too many login sessions. Sign out of an unused browser before trying again.", http.StatusTooManyRequests)
		return
	}
	http.Redirect(w, r, returnTo, http.StatusSeeOther)
}

func (m *Manager) loginError(w http.ResponseWriter, message string, status int) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	loginTemplate.Execute(w, loginData{Error: message, LoginURL: m.loginPath, ProviderName: m.provider.Name()})
}
