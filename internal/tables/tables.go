package tables

import (
	"crypto/rand"
	"errors"
	"html/template"
	"log"
	"net/http"
	"net/url"
	"path/filepath"
	"strings"
	"sync"

	"tableTop/main/internal/app"
	"tableTop/main/internal/auth"
	"tableTop/main/internal/handlers"
	"tableTop/main/internal/plugins"
)

type Manager struct {
	library      *app.TableLibrary
	dir, assets  string
	mu           sync.Mutex
	boards       map[string]*managedBoard
	deleteMu     sync.Mutex
	limits       *app.StorageLimits
	store        app.LibraryStore
	pluginDirs   []string
	pluginBounds plugins.Bounds
}

func New(dir, assets string, limits ...*app.StorageLimits) (*Manager, error) {
	return NewWithStore(dir, assets, nil, limits...)
}

func NewWithStore(dir, assets string, store app.LibraryStore, limits ...*app.StorageLimits) (*Manager, error) {
	var storage *app.StorageLimits
	if len(limits) > 0 {
		storage = limits[0]
	}
	library, err := app.NewTableLibrary(dir, storage)
	if err != nil {
		return nil, err
	}
	if err := library.MigrateLegacyProvenance(); err != nil {
		return nil, err
	}
	bounds, err := plugins.BoundsFromEnvironment()
	if err != nil {
		return nil, err
	}
	manager := &Manager{library: library, dir: dir, assets: assets, boards: make(map[string]*managedBoard), limits: storage, store: store, pluginDirs: plugins.Directories(assets), pluginBounds: bounds}
	if err := manager.cleanupStaging(); err != nil {
		return nil, err
	}
	return manager, nil
}

func (m *Manager) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /lobby", m.lobby)
	mux.HandleFunc("POST /sessions", m.create)
	mux.HandleFunc("POST /sessions/join", m.join)
	mux.HandleFunc("POST /sessions/{table}/initialize", m.retryInitialization)
	mux.HandleFunc("GET /sessions/{table}/delete", m.confirmDelete)
	mux.HandleFunc("POST /sessions/{table}/delete", m.delete)
	mux.HandleFunc("/sessions/{table}/{path...}", m.board)
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		http.Redirect(w, r, "/lobby", http.StatusSeeOther)
	})
}

var lobbyTemplate = template.Must(template.New("lobby").Parse(`<!doctype html><html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><title>Your sessions · tableTop</title><link rel="stylesheet" href="/static/style.css?v=42"></head><body class="lobby-page"><main><header><h1>Your sessions</h1><span>{{.User.Name}}</span>{{if .User.IsAdmin}}<a href="/admin/invites">Invite players</a>{{end}}<form action="/auth/logout" method="post"><button>Sign out</button></form></header>{{if .Error}}<p role="alert">{{.Error}}</p>{{end}}<section><h2>Create a session</h2><p>You will be the GM.</p><form action="/sessions" method="post"><label>Session name<input name="name" maxlength="80" required></label><label>Plugin<select name="plugin" required>{{range .Plugins}}<option value="{{.ID}}" {{if eq .ID "builtin"}}selected{{end}}>{{.Name}} — {{.Description}}</option>{{end}}</select></label><button>Create session</button></form></section><section><h2>Join a session</h2><p>Enter the code shared by your GM.</p><form action="/sessions/join" method="post"><label>Join code<input name="code" value="{{.Invite}}" required autocomplete="off"></label><button>Join as player</button></form></section><section><h2>Your tabletops</h2>{{range .Tables}}<article>{{if .Initialization}}<span>{{.Name}}</span><p>Initialization pending: {{.Initialization}}</p><form action="/sessions/{{.ID}}/initialize" method="post"><button>Retry initialization</button></form>{{else}}<a href="/sessions/{{.ID}}/">{{.Name}}</a>{{end}}<span>{{if eq .OwnerID $.User.ID}}GM{{else}}Player{{end}}</span><label>Join code<input readonly value="{{.ID}}" aria-label="Join code for {{.Name}}"></label>{{if eq .OwnerID $.User.ID}}<a href="/sessions/{{.ID}}/delete">{{if .Deleting}}Retry deletion{{else}}Delete tabletop{{end}}</a>{{end}}</article>{{else}}<p>Create or join a session to get started.</p>{{end}}</section></main></body></html>`))

func (m *Manager) renderLobby(w http.ResponseWriter, r *http.Request, message string, status int) {
	user := auth.CurrentUser(r)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	lobbyTemplate.Execute(w, struct {
		User          *auth.User
		Tables        []app.TableDefinition
		Plugins       []plugins.Pack
		Error, Invite string
	}{user, m.library.ForUser(user.ID), plugins.Discover(m.pluginDirs, m.pluginBounds), message, r.URL.Query().Get("invite")})
}

func (m *Manager) lobby(w http.ResponseWriter, r *http.Request) {
	m.renderLobby(w, r, "", http.StatusOK)
}

func (m *Manager) create(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 4096)
	if err := r.ParseForm(); err != nil {
		m.renderLobby(w, r, "Enter a session name.", http.StatusBadRequest)
		return
	}
	m.deleteMu.Lock()
	defer m.deleteMu.Unlock()
	var selected *plugins.Pack
	for _, pack := range plugins.Discover(m.pluginDirs, m.pluginBounds) {
		if pack.ID == r.PostForm.Get("plugin") {
			copy := pack
			selected = &copy
			break
		}
	}
	if selected == nil {
		m.renderLobby(w, r, "Choose an available plugin.", http.StatusBadRequest)
		return
	}
	id := rand.Text()
	if err := selected.Stage(m.dir, id, m.limits); err != nil {
		log.Println("Stage plugin:", err)
		status := http.StatusBadRequest
		if errors.Is(err, app.ErrStorageLimit) {
			status = http.StatusInsufficientStorage
		}
		m.renderLobby(w, r, "Could not prepare plugin content: "+err.Error(), status)
		return
	}
	table, err := m.library.CreatePrepared(r.Form.Get("name"), auth.CurrentUser(r).ID, id, selected.ID, selected.Version)
	if err != nil {
		app.RemoveTableFiles(m.dir, id, m.limits)
		if errors.Is(err, app.ErrCapacity) {
			m.renderLobby(w, r, err.Error(), http.StatusConflict)
			return
		}
		if errors.Is(err, app.ErrStorageLimit) {
			m.renderLobby(w, r, err.Error(), http.StatusInsufficientStorage)
			return
		}
		m.renderLobby(w, r, "Could not create the session. Use a name between 1 and 80 characters.", http.StatusBadRequest)
		return
	}
	if err := m.initialize(table.ID); err != nil {
		log.Println("Initialize tabletop:", err)
		m.renderLobby(w, r, "Initialization is pending. Retry initialization to finish creating this tabletop.", http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, "/sessions/"+table.ID+"/", http.StatusSeeOther)
}

func (m *Manager) join(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 4096)
	if err := r.ParseForm(); err != nil {
		m.renderLobby(w, r, "Enter a join code.", http.StatusBadRequest)
		return
	}
	table, err := m.library.Join(strings.TrimSpace(r.Form.Get("code")), auth.CurrentUser(r).ID)
	if err != nil {
		if errors.Is(err, app.ErrCapacity) {
			m.renderLobby(w, r, err.Error(), http.StatusConflict)
			return
		}
		if errors.Is(err, app.ErrStorageLimit) {
			m.renderLobby(w, r, err.Error(), http.StatusInsufficientStorage)
			return
		}
		m.renderLobby(w, r, "Could not join that session. Check the join code and try again.", http.StatusBadRequest)
		return
	}
	http.Redirect(w, r, "/sessions/"+table.ID+"/", http.StatusSeeOther)
}

func (m *Manager) board(w http.ResponseWriter, r *http.Request) {
	table, exists := m.library.ByID(r.PathValue("table"))
	if !exists || table.Deleting || table.Initialization != "" {
		http.NotFound(w, r)
		return
	}
	user := auth.CurrentUser(r)
	if !table.HasMember(user.ID) {
		if r.Method == "GET" && r.PathValue("path") == "" {
			http.Redirect(w, r, "/lobby?invite="+url.QueryEscape(table.ID), http.StatusSeeOther)
		} else {
			http.Error(w, "Join this session before accessing its board.", http.StatusForbidden)
		}
		return
	}
	handler, err := m.handler(table.ID)
	if err != nil {
		if errors.Is(err, app.ErrTableNotFound) {
			http.NotFound(w, r)
			return
		}
		log.Println("Load tabletop:", err)
		http.Error(w, "Could not load this session.", http.StatusInternalServerError)
		return
	}
	request := auth.WithTableRole(r, table.OwnerID == user.ID)
	request.URL = new(url.URL)
	*request.URL = *r.URL
	request.URL.Path = "/" + r.PathValue("path")
	request.URL.RawPath = ""
	handler.ServeHTTP(w, request)
}

func (m *Manager) handler(id string) (http.Handler, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if table, exists := m.library.ByID(id); !exists || table.Deleting || table.Initialization != "" {
		return nil, app.ErrTableNotFound
	}
	if handler := m.boards[id]; handler != nil {
		return handler, nil
	}
	dir := filepath.Join(m.dir, "sessions", id)
	var unitStore app.DocumentStore[app.UnitDefinition]
	var mapStore app.DocumentStore[app.MapDefinition]
	if m.store != nil {
		unitStore = m.store.Units(id)
		mapStore = m.store.Maps(id)
	}
	units, err := app.NewUnitLibraryWithStore(dir, unitStore, m.limits)
	if err != nil {
		return nil, err
	}
	maps, err := app.NewMapLibraryWithStore(dir, mapStore, m.limits)
	if err != nil {
		return nil, err
	}
	statuses, err := app.LoadSessionStatuses(dir, id, m.store, m.limits)
	if err != nil {
		return nil, err
	}
	state, err := app.NewStateWithStatuses(statuses)
	if err != nil {
		return nil, err
	}
	sockets := handlers.NewWebSocketHandler(state)
	endpoints := handlers.NewTableEndpointHandler(state, sockets, units, maps, m.assets, "/sessions/"+id)
	mux := http.NewServeMux()
	endpoints.RegisterRoutes(mux)
	sockets.RegisterRoutes(mux)
	sockets.StartPositionBroadcast()
	board := &managedBoard{handler: mux, sockets: sockets}
	m.boards[id] = board
	return board, nil
}
