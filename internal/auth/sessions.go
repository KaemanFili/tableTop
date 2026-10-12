package auth

import (
	"bufio"
	"crypto/rand"
	"errors"
	"net"
	"net/http"
	"time"
)

const sessionCookie = "tabletop_session"
const sessionLifetime = 12 * time.Hour

type session struct {
	user        User
	expires     time.Time
	timer       *time.Timer
	connections map[*sessionConn]struct{}
}

func (m *Manager) setCookie(w http.ResponseWriter, name, value string, age int) {
	http.SetCookie(w, &http.Cookie{Name: name, Value: value, Path: "/", MaxAge: age, HttpOnly: true, Secure: m.secure, SameSite: http.SameSiteLaxMode})
}

func (m *Manager) newSession(w http.ResponseWriter, user User) {
	token := rand.Text()
	m.mu.Lock()
	record := &session{user: user, expires: time.Now().Add(sessionLifetime), connections: make(map[*sessionConn]struct{})}
	m.sessions[token] = record
	record.timer = time.AfterFunc(sessionLifetime, func() { m.removeSession(token) })
	m.mu.Unlock()
	m.setCookie(w, sessionCookie, token, int(sessionLifetime.Seconds()))
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
}

func (conn *sessionConn) Close() error {
	conn.manager.mu.Lock()
	if record := conn.manager.sessions[conn.token]; record != nil {
		delete(record.connections, conn)
	}
	conn.manager.mu.Unlock()
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
	valid := record != nil && time.Now().Before(record.expires)
	if valid {
		record.connections[tracked] = struct{}{}
	}
	w.manager.mu.Unlock()
	if !valid {
		conn.Close()
		return nil, nil, errors.New("session expired")
	}
	return tracked, buffer, nil
}
