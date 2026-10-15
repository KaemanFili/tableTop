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
	outer.HandleFunc("GET /browser/progress", func(w http.ResponseWriter, r *http.Request) { t.Log(r.URL.Query().Get("stage")) })
	outer.HandleFunc("GET /browser", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.Write([]byte(`<!doctype html><iframe id="app"></iframe><script>
const frame = document.getElementById('app');
let stage = 'lobby';
let bindingIndex = 0;
const bindings = [
  {action: 'pen', key: 'p', label: 'P'},
  {action: 'ruler', key: 'r', label: 'R'},
  {action: 'fog', key: 'f', label: 'F'},
  {action: 'eraser', key: 'e', label: 'Ctrl+E', ctrlKey: true},
  {action: 'ping', key: 'G', label: 'Alt+Shift+G', altKey: true, shiftKey: true},
  {action: 'fog-eraser', key: '!', code: 'Digit1', label: 'Shift+Meta+1', shiftKey: true, metaKey: true},
];
frame.addEventListener('load', () => {
  try {
    const doc = frame.contentDocument;
    const path = frame.contentWindow.location.pathname;
    fetch('/browser/progress?stage=' + encodeURIComponent(stage + ':' + bindingIndex + ':' + path));
    if (stage === 'lobby') {
      if (path !== '/lobby') throw new Error('Login did not reach lobby: ' + path);
      if (doc.querySelector('form[action="/preferences"]')) throw new Error('Preferences editor is still in the lobby');
      stage = 'preferences';
      doc.querySelector('a[href="/preferences"]').click();
    } else if (stage === 'preferences' || stage === 'saved') {
      if (path !== '/preferences') throw new Error('Preferences dashboard did not open');
      const form = doc.querySelector('#hotkey-editor');
      if (stage === 'saved') {
        if (!doc.body.textContent.includes('Hotkey saved.')) throw new Error('Hotkey form failed: ' + doc.body.textContent);
        if (form.elements.action.value !== bindings[bindingIndex - 1].action || form.elements.hotkey.value !== bindings[bindingIndex - 1].label) throw new Error('Saved hotkey was not restored');
      }
      if (bindingIndex === bindings.length) {
        stage = 'create';
        doc.querySelector('a[href="/lobby"]').click();
      } else {
        const binding = bindings[bindingIndex++];
        form.elements.action.value = binding.action;
        form.elements.action.dispatchEvent(new frame.contentWindow.Event('change'));
        const input = form.elements.hotkey;
        input.focus();
        input.dispatchEvent(new frame.contentWindow.KeyboardEvent('keydown', {bubbles: true, cancelable: true, ...binding}));
        if (input.value !== binding.label) throw new Error('Key capture failed: ' + input.value);
        stage = 'saved';
        form.requestSubmit();
      }
    } else if (stage === 'create') {
      if (path !== '/lobby') throw new Error('Preferences navigation did not return to the lobby');
      const form = doc.querySelector('form[action="/sessions"]');
      form.elements.name.value = 'Browser session';
      stage = 'board';
      form.requestSubmit();
    } else if (stage === 'board') {
      if (!path.startsWith('/sessions/') || doc.body.dataset.gm !== 'true') {
        throw new Error('Create form failed: ' + doc.body.textContent);
      }
      const w = frame.contentWindow;
      const board = doc.getElementById('grid-container');
      const press = (key, options = {}, target = board) => target.dispatchEvent(new w.KeyboardEvent('keydown', {key, bubbles: true, cancelable: true, ...options}));
      const expectTool = expected => {
        if ((doc.body.dataset.tool || '') !== expected) throw new Error('Expected tool ' + expected + ', got ' + doc.body.dataset.tool);
      };
      board.tabIndex = -1;
      board.focus();
      press('p'); expectTool('pen');
      if (doc.getElementById('utility-pen').getAttribute('aria-pressed') !== 'true') throw new Error('Hotkey did not update ARIA');
      if (doc.getElementById('utility-pen').getAttribute('aria-keyshortcuts') !== 'P') throw new Error('Shortcut not exposed');
      press('p', {repeat: true}); expectTool('pen');
      press('p'); expectTool('');
      press('r'); expectTool('ruler');
      for (const modifier of ['ctrlKey', 'metaKey', 'altKey', 'shiftKey', 'isComposing']) {
        press('p', {[modifier]: true}); expectTool('ruler');
      }
      if (press('e', {ctrlKey: true}) !== false) throw new Error('Matched combination did not prevent browser default');
      expectTool('eraser');
      press('e'); expectTool('eraser');
      press('e', {ctrlKey: true, shiftKey: true}); expectTool('eraser');
      press('Control', {ctrlKey: true}); expectTool('eraser');
      press('e', {ctrlKey: true, repeat: true}); expectTool('eraser');
      press('e', {ctrlKey: true, isComposing: true}); expectTool('eraser');
      press('e', {ctrlKey: true}); expectTool('');
      press('G', {altKey: true, shiftKey: true}); expectTool('ping');
      press('g', {altKey: true}); expectTool('ping');
      press('!', {code: 'Digit1', metaKey: true, shiftKey: true}); expectTool('fog-eraser');
      if (doc.getElementById('utility-fog-eraser').getAttribute('aria-keyshortcuts') !== 'Shift+Meta+1') throw new Error('Modifier ARIA shortcut missing');
      if (press('z', {ctrlKey: true}) !== true) throw new Error('Unassigned combination suppressed browser default');
      press('r'); expectTool('ruler');
      const cancelled = new w.KeyboardEvent('keydown', {key: 'p', bubbles: true, cancelable: true});
      cancelled.preventDefault(); board.dispatchEvent(cancelled); expectTool('ruler');
      for (const tag of ['input', 'textarea', 'select', 'div']) {
        const field = doc.createElement(tag);
        if (tag === 'div') field.contentEditable = 'true';
        doc.body.append(field); field.focus();
        press('p', {}, field); expectTool('ruler');
        press('e', {ctrlKey: true}, field); expectTool('ruler');
        field.remove(); board.focus();
      }
      doc.getElementById('toggle-ui-controls').click();
      press('p'); expectTool('');
      doc.getElementById('toggle-ui-controls').click();
      board.focus(); press('f'); expectTool('fog');
      press('Escape'); expectTool('');
      press('x'); expectTool('');
      stage = 'reload';
      w.location.reload();
    } else if (stage === 'reload') {
      if (!path.startsWith('/sessions/') || doc.getElementById('utility-pen').getAttribute('aria-keyshortcuts') !== 'P') {
        throw new Error('Hotkeys did not survive board reload');
      }
      stage = 'signed-out';
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
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
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
