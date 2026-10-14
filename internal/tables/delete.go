package tables

import (
	"context"
	"errors"
	"html/template"
	"log"
	"net/http"
	"time"

	"tableTop/main/internal/app"
	"tableTop/main/internal/auth"
)

var deleteTemplate = template.Must(template.New("delete-tabletop").Parse(`<!doctype html><html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><title>Delete tabletop · tableTop</title><link rel="icon" href="/static/favicon.ico?v=1" sizes="16x16 32x32 48x48" type="image/x-icon"><link rel="icon" href="/static/favicon-64.png?v=1" sizes="64x64" type="image/png"><link rel="stylesheet" href="/static/style.css?v=52"></head><body class="lobby-page"><main><section class="lobby-delete-confirmation"><h1>Delete {{.Name}}?</h1><p>This permanently deletes the tabletop for everyone, including its saved maps, unit library, and uploaded artwork. Connected players will be disconnected.</p><form action="/sessions/{{.ID}}/delete" method="post"><input type="hidden" name="confirm" value="delete"><button class="danger-button">Delete tabletop permanently</button> <a href="/lobby">Cancel</a></form></section></main></body></html>`))

func (m *Manager) confirmDelete(w http.ResponseWriter, r *http.Request) {
	table, exists := m.library.ByID(r.PathValue("table"))
	if !exists {
		http.NotFound(w, r)
		return
	}
	if table.OwnerID != auth.CurrentUser(r).ID {
		http.Error(w, app.ErrTableNotOwner.Error(), http.StatusForbidden)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	if err := deleteTemplate.Execute(w, table); err != nil {
		log.Println("Render tabletop deletion:", err)
	}
}

func (m *Manager) delete(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 4096)
	if err := r.ParseForm(); err != nil || r.PostForm.Get("confirm") != "delete" {
		http.Error(w, "Confirm tabletop deletion before continuing.", http.StatusBadRequest)
		return
	}
	m.deleteMu.Lock()
	defer m.deleteMu.Unlock()
	id, owner := r.PathValue("table"), auth.CurrentUser(r).ID
	if err := m.library.BeginDelete(id, owner); err != nil {
		switch {
		case errors.Is(err, app.ErrTableNotFound):
			http.NotFound(w, r)
		case errors.Is(err, app.ErrTableNotOwner):
			http.Error(w, err.Error(), http.StatusForbidden)
		default:
			log.Println("Begin tabletop deletion:", err)
			m.renderLobby(w, r, "Could not start deletion. Please try again.", http.StatusInternalServerError)
		}
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	m.mu.Lock()
	board := m.boards[id]
	m.mu.Unlock()
	var err error
	if board != nil {
		err = board.stop(ctx)
	}
	if err == nil && m.store != nil {
		err = m.store.DeleteScope(id)
	}
	if err == nil {
		err = app.RemoveTableFiles(m.dir, id, m.limits)
	}
	if err == nil {
		err = m.library.FinishDelete(id, owner)
	}
	if err != nil {
		log.Println("Delete tabletop:", err)
		m.renderLobby(w, r, "Deletion is pending. Retry Delete tabletop to finish removing its data.", http.StatusInternalServerError)
		return
	}
	m.mu.Lock()
	delete(m.boards, id)
	m.mu.Unlock()
	http.Redirect(w, r, "/lobby", http.StatusSeeOther)
}
