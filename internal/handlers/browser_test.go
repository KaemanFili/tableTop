package handlers

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"tableTop/main/internal/app/model"
	"tableTop/main/internal/app/persistence"
	"tableTop/main/internal/plugins"
)

//go:embed testdata/browser.js
var browserTests string

// TestBrowserSync uses the actual app, HTMX, sockets, and DOM in headless Firefox.
func TestBrowserSync(t *testing.T) {
	if testing.Short() {
		t.Skip("browser test disabled by -short")
	}
	firefox, err := exec.LookPath("firefox")
	if err != nil {
		t.Skip("Firefox is required for browser regression tests")
	}
	state := model.NewState()
	library := newTestLibrary(t)
	for _, pack := range plugins.Discover(plugins.Directories(testAssetRoot(t)), plugins.Bounds{Metadata: 1 << 20, Content: 16 << 20}) {
		if pack.ID == "builtin" {
			library.SetDefaults(pack)
			break
		}
	}
	knight, _ := library.UnitByID("knight")
	state.AddUnit(knight)
	ws := NewWebSocketHandler(state)
	maps, err := persistence.NewMapLibrary(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for _, definition := range []model.MapDefinition{
		{Name: "Forest Clearing", Width: 20, Height: 20, OffsetX: -8, OffsetY: -4},
		{Name: "Desert Oasis", Width: 20, Height: 20, OffsetX: 64, OffsetY: 64},
	} {
		if _, err := maps.Create(definition, testPNG(t, 2, 2)); err != nil {
			t.Fatal(err)
		}
	}
	handler := newTestEndpointHandler(t, state, ws, library, maps)
	mux := http.NewServeMux()
	handler.RegisterRoutes(mux)
	ws.RegisterRoutes(mux)
	mux.Handle("/static/", http.StripPrefix("/static/", http.FileServer(http.Dir(filepath.Join(testAssetRoot(t), "static")))))
	results := make(chan string, 1)
	mux.HandleFunc("/browser/result", func(w http.ResponseWriter, r *http.Request) {
		var result struct{ Error string }
		if err := json.NewDecoder(r.Body).Decode(&result); err != nil {
			results <- err.Error()
		} else {
			results <- result.Error
		}
	})
	mux.HandleFunc("/browser/progress", func(w http.ResponseWriter, r *http.Request) { t.Log(r.URL.Query().Get("stage")) })
	mux.HandleFunc("/browser/state", func(w http.ResponseWriter, r *http.Request) { json.NewEncoder(w).Encode(state.Units()) })
	mux.HandleFunc("/browser/gestures", func(w http.ResponseWriter, r *http.Request) {
		ws.boardMutex.Lock()
		defer ws.boardMutex.Unlock()
		json.NewEncoder(w).Encode(map[string]any{"strokes": ws.strokes, "fog": ws.fogMarks, "rulers": ws.rulers})
	})
	mux.HandleFunc("/browser/maps", func(w http.ResponseWriter, r *http.Request) { json.NewEncoder(w).Encode(maps.Maps()) })
	// Send one production-format position update without starting a permanent ticker.
	mux.HandleFunc("/browser/positions", func(w http.ResponseWriter, r *http.Request) {
		data, err := json.Marshal(state.Units())
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		ws.positionMutex.Lock()
		defer ws.positionMutex.Unlock()
		for conn, writer := range ws.positionClients {
			if !writer.send(data) {
				delete(ws.positionClients, conn)
			}
		}
	})
	mux.HandleFunc("/browser/test.js", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/javascript")
		w.Write([]byte(browserTests))
	})
	mux.HandleFunc("/browser", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.Write([]byte(`<iframe id="app" src="/?gm=true" style="width:1200px;height:800px"></iframe><script src="/browser/test.js"></script>`))
	})
	// Hold a response after it captures state, allowing deterministic response ordering.
	var gateMutex sync.Mutex
	var kind string
	var ready, release chan struct{}
	mux.HandleFunc("/browser/arm", func(w http.ResponseWriter, r *http.Request) {
		gateMutex.Lock()
		defer gateMutex.Unlock()
		kind = r.URL.Query().Get("kind")
		ready, release = make(chan struct{}), make(chan struct{})
	})
	mux.HandleFunc("/browser/ready", func(w http.ResponseWriter, r *http.Request) {
		gateMutex.Lock()
		signal := ready
		gateMutex.Unlock()
		select {
		case <-signal:
		case <-r.Context().Done():
		}
	})
	mux.HandleFunc("/browser/release", func(w http.ResponseWriter, r *http.Request) {
		gateMutex.Lock()
		close(release)
		gateMutex.Unlock()
	})
	server := newTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gateMutex.Lock()
		hold := kind != "" && ((kind == "spawn" && r.Method == http.MethodPost && r.URL.Path == "/unit") || (kind == "refresh" && r.URL.Path == "/loadExistingUnits"))
		signal, unblock := ready, release
		if hold {
			kind = ""
		}
		gateMutex.Unlock()
		if !hold {
			mux.ServeHTTP(w, r)
			return
		}
		response := httptest.NewRecorder()
		mux.ServeHTTP(response, r)
		close(signal)
		select {
		case <-unblock:
		case <-r.Context().Done():
			return
		}
		for key, values := range response.Header() {
			w.Header()[key] = values
		}
		w.WriteHeader(response.Code)
		w.Write(response.Body.Bytes())
	}))
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, firefox, "--headless", "--no-remote", "--profile", t.TempDir(), server.URL+"/browser")
	command.WaitDelay = 2 * time.Second
	var output bytes.Buffer
	command.Stdout, command.Stderr = &output, &output
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	select {
	case result := <-results:
		cancel()
		command.Wait()
		if result != "" {
			t.Fatalf("browser regression: %s\n%s", result, output.String())
		}
	case <-ctx.Done():
		command.Wait()
		t.Fatalf("browser tests timed out: %s", output.String())
	}
}
