package tables

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"tableTop/main/internal/auth"
)

// Use real browser navigation so Referrer-Policy controls form Origin headers.
func TestAuthenticatedBrowserForms(t *testing.T) {
	if testing.Short() {
		t.Skip("browser test disabled by -short")
	}
	firefox, err := exec.LookPath("firefox")
	if err != nil {
		t.Skip("Firefox is required for browser regression tests")
	}
	_, file, _, _ := runtime.Caller(0)
	assets := filepath.Clean(filepath.Join(filepath.Dir(file), "../.."))
	tables, err := New(t.TempDir(), assets)
	if err != nil {
		t.Fatal(err)
	}
	outer := http.NewServeMux()
	server := httptest.NewUnstartedServer(outer)
	defer server.Close()
	base := "http://" + server.Listener.Addr().String()
	manager, err := auth.New(auth.Config{ClientID: "client", ClientSecret: "test-secret", RedirectURL: base + "/auth/discord/callback", HTTPClient: &http.Client{Transport: discordTransport{}}})
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	tables.RegisterRoutes(mux)
	manager.RegisterRoutes(mux)
	mux.Handle("/static/", http.StripPrefix("/static/", http.FileServer(http.Dir(filepath.Join(assets, "static")))))
	app := manager.Middleware(mux)
	outer.Handle("/", app)
	// Complete simulated provider login, then pass its real session cookie to Firefox.
	outer.HandleFunc("GET /browser/bootstrap", func(w http.ResponseWriter, r *http.Request) {
		login := httptest.NewRecorder()
		app.ServeHTTP(login, httptest.NewRequest("GET", base+"/auth/discord/login", nil))
		target, _ := url.Parse(login.Header().Get("Location"))
		callback := httptest.NewRequest("GET", base+"/auth/discord/callback?code=browser-user&state="+target.Query().Get("state"), nil)
		for _, cookie := range login.Result().Cookies() {
			callback.AddCookie(cookie)
		}
		result := httptest.NewRecorder()
		app.ServeHTTP(result, callback)
		for _, cookie := range result.Result().Cookies() {
			http.SetCookie(w, cookie)
		}
		http.Redirect(w, r, "/lobby", http.StatusSeeOther)
	})
	results := make(chan string, 1)
	outer.HandleFunc("POST /browser/result", func(w http.ResponseWriter, r *http.Request) {
		select {
		case results <- r.URL.Query().Get("error"):
		default:
		}
	})
	outer.HandleFunc("GET /browser", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.Write([]byte(`<!doctype html><iframe id="app"></iframe><script>
const frame = document.getElementById('app');
let stage = 0;
frame.addEventListener('load', () => {
  try {
    const doc = frame.contentDocument;
    const path = frame.contentWindow.location.pathname;
    if (stage === 0) {
      if (path !== '/lobby') throw new Error('Login did not reach lobby: ' + path);
      const form = doc.querySelector('form[action="/sessions"]');
      form.elements.name.value = 'Browser session';
      stage = 1;
      form.requestSubmit();
    } else if (stage === 1) {
      if (!path.startsWith('/sessions/') || doc.body.dataset.gm !== 'true') {
        throw new Error('Create form failed: ' + doc.body.textContent);
      }
      stage = 2;
      doc.querySelector('form[action="/auth/logout"]').requestSubmit();
    } else {
      if (path !== '/login') throw new Error('Logout form failed: ' + path);
      fetch('/browser/result', {method: 'POST'});
    }
  } catch (error) {
    fetch('/browser/result?error=' + encodeURIComponent(error.message), {method: 'POST'});
  }
});
frame.src = '/browser/bootstrap';
</script>`))
	})
	server.Start()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, firefox, "--headless", "--no-remote", "--profile", t.TempDir(), base+"/browser")
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
			t.Fatalf("authenticated browser forms: %s\n%s", result, output.String())
		}
	case <-ctx.Done():
		command.Wait()
		t.Fatalf("authenticated browser forms timed out: %s", output.String())
	}
}
