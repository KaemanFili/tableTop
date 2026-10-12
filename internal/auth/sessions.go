package auth

import (
	"bufio"
	"crypto/rand"
	"errors"
	"golang.org/x/time/rate"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

const sessionCookie = "tabletop_session"
const sessionLifetime = 12 * time.Hour

type session struct {
	user                         User
	expires                      time.Time
	timer                        *time.Timer
	connections                  map[*sessionConn]struct{}
	requests, uploads, creations *rate.Limiter
}

func (m *Manager) setCookie(w http.ResponseWriter, name, value string, age int) {
	http.SetCookie(w, &http.Cookie{Name: name, Value: value, Path: "/", MaxAge: age, HttpOnly: true, Secure: m.secure, SameSite: http.SameSiteLaxMode})
}

func (m *Manager) newSession(w http.ResponseWriter, user User) bool {
	token := rand.Text()
	m.mu.Lock()
	count := 0
	for _, existing := range m.sessions {
		if existing.user.ID == user.ID {
			count++
		}
	}
	if len(m.sessions) >= 512 || count >= 4 {
		m.mu.Unlock()
		return false
	}
	record := &session{user: user, expires: time.Now().Add(sessionLifetime), connections: make(map[*sessionConn]struct{})}
	record.requests = rate.NewLimiter(20, 60)
	record.uploads = rate.NewLimiter(rate.Every(10*time.Second), 3)
	record.creations = rate.NewLimiter(rate.Every(10*time.Second), 3)
	m.sessions[token] = record
	record.timer = time.AfterFunc(sessionLifetime, func() { m.removeSession(token) })
	m.mu.Unlock()
	m.setCookie(w, sessionCookie, token, int(sessionLifetime.Seconds()))
	return true
}

func (m *Manager) allowRequest(token string, r *http.Request) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	record := m.sessions[token]
	if record == nil || !record.requests.Allow() {
		return false
	}
	if r.Method == http.MethodPost && (strings.HasSuffix(r.URL.Path, "/unit-library") || strings.HasSuffix(r.URL.Path, "/map-library")) {
		return record.uploads.Allow()
	}
	if r.Method == http.MethodPost && (r.URL.Path == "/sessions" || r.URL.Path == "/admin/invites") {
		return record.creations.Allow()
	}
	return true
}

// Requires mu. Bound all sockets, including multiple logins for one user.
func (m *Manager) canOpenSocketLocked(token string) bool {
	record := m.sessions[token]
	if record == nil || m.socketCount >= 256 {
		return false
	}
	count := 0
	for _, existing := range m.sessions {
		if existing.user.ID == record.user.ID {
			count += len(existing.connections)
		}
	}
	return count < 24
}

func (m *Manager) canOpenSocket(token string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.canOpenSocketLocked(token)
}

func (m *Manager) authenticate(r *http.Request) (string, *User) {
	cookie, err := r.Cookie(sessionCookie)
	if err != nil {
		return "", nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	record := m.sessions[cookie.Value]
	if record == nil || !time.Now().Before(record.expires) {
		return "", nil
	}
	user := record.user
	if m.config.Access != nil && !m.config.Access.Allowed(user.ID) {
		return "", nil
	}
	return cookie.Value, &user
}

func (m *Manager) removeSession(token string) {
	m.mu.Lock()
	record := m.sessions[token]
	delete(m.sessions, token)
	var connections []*sessionConn
	if record != nil {
		for conn := range record.connections {
			connections = append(connections, conn)
		}
	}
	m.mu.Unlock()
	if record == nil {
		return
	}
	record.timer.Stop()
	for _, conn := range connections {
		conn.Close()
	}
}

func (m *Manager) logout(w http.ResponseWriter, r *http.Request) {
	if cookie, err := r.Cookie(sessionCookie); err == nil {
		m.removeSession(cookie.Value)
	}
	m.setCookie(w, sessionCookie, "", -1)
	http.Redirect(w, r, "/login", http.StatusSeeOther)
}

// Track upgraded connections so logout and expiry also revoke open sockets.
type socketWriter struct {
	http.ResponseWriter
	manager *Manager
	token   string
}

// Closing a socket releases its session reference, including failed upgrades.
type sessionConn struct {
	net.Conn
	manager *Manager
	token   string
	once    sync.Once
}

func (conn *sessionConn) Close() error {
	conn.once.Do(func() {
		conn.manager.mu.Lock()
		if record := conn.manager.sessions[conn.token]; record != nil {
			delete(record.connections, conn)
		}
		conn.manager.socketCount--
		conn.manager.mu.Unlock()
	})
	return conn.Conn.Close()
}

func (w *socketWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	hijacker, ok := w.ResponseWriter.(http.Hijacker)
	if !ok {
		return nil, nil, errors.New("websocket connections are unavailable")
	}
	conn, buffer, err := hijacker.Hijack()
	if err != nil {
		return nil, nil, err
	}
	tracked := &sessionConn{Conn: conn, manager: w.manager, token: w.token}
	w.manager.mu.Lock()
	record := w.manager.sessions[w.token]
	valid := record != nil && time.Now().Before(record.expires) && w.manager.canOpenSocketLocked(w.token)
	if valid {
		record.connections[tracked] = struct{}{}
		w.manager.socketCount++
	}
	w.manager.mu.Unlock()
	if !valid {
		conn.Close()
		return nil, nil, errors.New("session expired")
	}
	return tracked, buffer, nil
}
