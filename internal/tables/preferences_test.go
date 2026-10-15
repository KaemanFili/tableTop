package tables

import (
	"html"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

func TestLobbyPreferencesArePrivateAndApplyAcrossSessions(t *testing.T) {
	f := newTableTestFixture(t)
	owner, player := f.login("owner"), f.login("player")
	first := f.createSession(t, owner, "First")
	second := f.createSession(t, owner, "Second")
	f.joinSession(player, first)
	form := url.Values{"action": {"pen"}, "hotkey": {"P"}}
	response, _ := f.request(player, "POST", "/preferences", form.Encode(), http.StatusSeeOther)
	if response.Header.Get("Location") != "/preferences?saved=1&action=pen" {
		t.Fatal("save did not return to the preferences dashboard")
	}
	f.request(player, "POST", "/preferences", "action=fog&hotkey=F", http.StatusSeeOther)
	_, lobby := f.request(player, "GET", "/lobby", "", http.StatusOK)
	if !strings.Contains(lobby, `href="/preferences"`) || strings.Contains(lobby, `id="player-preferences"`) || strings.Contains(lobby, `name="hotkey_pen"`) {
		t.Fatal("lobby should link to the dashboard without showing the editor")
	}
	_, lobby = f.request(player, "GET", "/preferences?saved=1", "", http.StatusOK)
	if !strings.Contains(lobby, "Hotkey saved.") || !strings.Contains(lobby, `data-hotkey="p"`) {
		t.Fatal("preferences dashboard did not display saved hotkeys")
	}
	_, otherPreferences := f.request(owner, "GET", "/preferences", "", http.StatusOK)
	if strings.Contains(otherPreferences, `data-hotkey="p"`) || !strings.Contains(otherPreferences, `href="/lobby"`) {
		t.Fatal("dashboard leaked preferences or is missing navigation back to sessions")
	}
	_, board := f.request(player, "GET", first.path, "", http.StatusOK)
	if !strings.Contains(board, `{"fog":"f","pen":"p"}`) || strings.Contains(board, `id="utility-fog"`) {
		t.Fatal("player board did not get their preferences or gained GM controls")
	}
	_, board = f.request(owner, "GET", first.path, "", http.StatusOK)
	if strings.Contains(board, `{"fog":"f","pen":"p"}`) {
		t.Fatal("preferences leaked across users sharing one board handler")
	}
	f.joinSession(player, second)
	_, board = f.request(player, "GET", second.path, "", http.StatusOK)
	if !strings.Contains(board, `{"fog":"f","pen":"p"}`) {
		t.Fatal("preferences were scoped to one tabletop")
	}
	reloaded, err := New(f.dir, f.assets)
	if err != nil || reloaded.preferences.ForUser("player").Hotkeys["pen"] != "p" {
		t.Fatalf("preferences did not survive manager restart: %v", err)
	}
	f.request(player, "POST", "/preferences", "reset=hotkeys", http.StatusSeeOther)
	_, board = f.request(player, "GET", first.path, "", http.StatusOK)
	if strings.Contains(board, `{"fog":"f","pen":"p"}`) {
		t.Fatal("clear hotkeys did not update an existing board handler")
	}
}

func TestPreferencesValidationCannotOverwriteSavedBindings(t *testing.T) {
	f := newTableTestFixture(t)
	player := f.login("player")
	f.request(player, "POST", "/preferences", "action=pen&hotkey=p", http.StatusSeeOther)
	for _, form := range []string{
		"action=pen&hotkey=Escape", "action=unknown&hotkey=x", "action=pen&hotkey=ctrl%2Bctrl%2Bp",
		"action=pen&hotkey=p&hotkey=r", "action=pen&hotkey=r&userID=owner", "reset=unknown",
	} {
		f.request(player, "POST", "/preferences", form, http.StatusBadRequest)
		if f.manager.preferences.ForUser("player").Hotkeys["pen"] != "p" || len(f.manager.preferences.ForUser("owner").Hotkeys) != 0 {
			t.Fatalf("invalid request changed preferences: %s", form)
		}
	}
	f.request(player, "POST", "/preferences", "action=pen&hotkey="+strings.Repeat("x", 5000), http.StatusBadRequest)
	f.request(&http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}, "POST", "/preferences", "action=pen&hotkey=r", http.StatusUnauthorized)
}

func TestSingleHotkeyEditorPreservesOtherBindings(t *testing.T) {
	f := newTableTestFixture(t)
	player := f.login("player")
	set := func(action, hotkey string, status int) string {
		_, body := f.request(player, "POST", "/preferences", url.Values{"action": {action}, "hotkey": {hotkey}}.Encode(), status)
		return body
	}
	set("pen", "P", http.StatusSeeOther)
	set("ruler", "Shift + Control + P", http.StatusSeeOther)
	prefs := f.manager.preferences.ForUser("player")
	if prefs.Hotkeys["pen"] != "p" || prefs.Hotkeys["ruler"] != "ctrl+shift+p" {
		t.Fatalf("wrong saved bindings: %v", prefs.Hotkeys)
	}
	body := set("pen", "Ctrl + Shift + P", http.StatusBadRequest)
	if !strings.Contains(html.UnescapeString(body), `value="Ctrl + Shift + P"`) {
		t.Fatal("failed edit did not preserve the typed value")
	}
	if f.manager.preferences.ForUser("player").Hotkeys["pen"] != "p" {
		t.Fatal("duplicate binding overwrote saved preferences")
	}
	set("ruler", "", http.StatusSeeOther)
	prefs = f.manager.preferences.ForUser("player")
	if len(prefs.Hotkeys) != 1 || prefs.Hotkeys["pen"] != "p" {
		t.Fatal("clearing one binding changed another")
	}
	_, dashboard := f.request(player, "GET", "/preferences?action=pen", "", http.StatusOK)
	if !strings.Contains(dashboard, `name="hotkey" value="P"`) {
		t.Fatal("selected utility did not show its saved binding")
	}
}
