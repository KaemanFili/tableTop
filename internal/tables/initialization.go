package tables

import (
	"errors"
	"log"
	"net/http"
	"path/filepath"

	"tableTop/main/internal/app"
	"tableTop/main/internal/auth"
	"tableTop/main/internal/plugins"
)

// Initialization and deletion share deleteMu; handlers cannot serve pending boards.
func (m *Manager) initialize(id string) error {
	table, ok := m.library.ByID(id)
	if !ok || table.Deleting || table.PluginBinding == nil {
		return app.ErrTableNotFound
	}
	if _, err := plugins.Resolve(m.pluginDirs, m.pluginBounds, *table.PluginBinding); err != nil {
		return err
	}
	if table.Initialization == "" {
		return nil
	}
	if table.Initialization != "owned" {
		return errors.New("unsupported initialization state")
	}
	if err := app.InitializeOwned(filepath.Join(m.dir, "sessions", id), id, m.store, m.limits); err != nil {
		return err
	}
	return m.library.FinishInitialization(id)
}
func (m *Manager) retryInitialization(w http.ResponseWriter, r *http.Request) {
	m.deleteMu.Lock()
	defer m.deleteMu.Unlock()
	table, ok := m.library.ByID(r.PathValue("table"))
	if !ok || table.Deleting {
		http.NotFound(w, r)
		return
	}
	if table.OwnerID != auth.CurrentUser(r).ID {
		http.Error(w, "Only the owner can initialize this tabletop.", http.StatusForbidden)
		return
	}
	if err := m.initialize(table.ID); err != nil {
		log.Println("Retry tabletop initialization:", err)
		m.renderLobby(w, r, "Initialization is still pending. Retry initialization or delete the tabletop.", http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, "/sessions/"+table.ID+"/", http.StatusSeeOther)
}
