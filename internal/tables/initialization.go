package tables

import (
	"errors"
	"log"
	"net/http"
	"os"
	"path/filepath"

	"tableTop/main/internal/app"
	"tableTop/main/internal/auth"
	"tableTop/main/internal/plugins"
)

// Initialization and deletion share deleteMu; handlers cannot serve pending boards.
func (m *Manager) initialize(id string) error {
	for {
		table, ok := m.library.ByID(id)
		if !ok || table.Deleting {
			return app.ErrTableNotFound
		}
		dir := filepath.Join(m.dir, "sessions", id)
		var next string
		var err error
		switch table.Initialization {
		case "maps":
			err = plugins.InitializeMaps(dir, id, m.store, m.limits)
			next = "statuses"
		case "statuses":
			err = plugins.InitializeStatuses(dir, id, m.store, m.limits)
			next = "units"
		case "units":
			err = plugins.InitializeUnits(dir, id, m.store, m.limits)
		case "":
			if err := os.RemoveAll(filepath.Join(dir, ".plugin")); err != nil {
				log.Println("Clean initialized plugin staging:", err)
			}
			return nil
		default:
			return errors.New("unsupported initialization step")
		}
		if err != nil {
			return err
		}
		if err := m.library.AdvanceInitialization(id, table.Initialization, next); err != nil {
			return err
		}
	}
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

// Remove abandoned preparation and completed staging after a process interruption.
func (m *Manager) cleanupStaging() error {
	entries, err := os.ReadDir(filepath.Join(m.dir, "sessions"))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		dir := filepath.Join(m.dir, "sessions", entry.Name())
		info, err := os.Lstat(filepath.Join(dir, ".plugin"))
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return err
		}
		if !info.IsDir() {
			return errors.New("invalid plugin staging directory")
		}
		table, exists := m.library.ByID(entry.Name())
		if exists && table.Initialization != "" {
			continue
		}
		if !exists {
			if err := app.RemoveTableFiles(m.dir, entry.Name(), m.limits); err != nil {
				return err
			}
		} else if err := os.RemoveAll(filepath.Join(dir, ".plugin")); err != nil {
			return err
		}
	}
	return nil
}
