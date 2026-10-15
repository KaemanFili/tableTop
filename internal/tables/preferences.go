package tables

import (
	"errors"
	"html/template"
	"log"
	"net/http"

	"tableTop/main/internal/app/model"
	"tableTop/main/internal/app/persistence"
	"tableTop/main/internal/auth"
)

var preferencesTemplate = template.Must(template.New("preferences").Parse(`<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width,initial-scale=1">
  <title>Your preferences · tableTop</title>
  <link rel="icon" href="/static/favicon.ico?v=1" sizes="16x16 32x32 48x48" type="image/x-icon">
  <link rel="icon" href="/static/favicon-64.png?v=1" sizes="64x64" type="image/png">
  <link rel="stylesheet" href="/static/style.css?v=75">
</head>
<body class="lobby-page preferences-page">
<main>
  <header class="lobby-header">
    <div><h1>Your preferences</h1><p>Personal settings for every tabletop.</p></div>
    <div class="lobby-account"><span>{{.User.Name}}</span><form action="/auth/logout" method="post"><button>Sign out</button></form></div>
  </header>
  <a class="page-nav" href="/lobby">← Back to sessions</a>
  <section id="player-preferences" aria-labelledby="preferences-heading">
    <h2 id="preferences-heading">Utility hotkeys</h2>
    {{if .PreferenceError}}<p class="lobby-error" role="alert">{{.PreferenceError}}</p>{{end}}
    {{if .PreferencesSaved}}<p role="status">Hotkey saved.</p>{{end}}
    <form id="hotkey-editor" action="/preferences" method="post">
      <label for="hotkey-action">Utility<select id="hotkey-action" name="action">
        {{range .Hotkeys}}<option value="{{.ID}}" data-hotkey="{{.Binding}}" {{if eq .ID $.Editor.Action}}selected{{end}}>{{.Label}}</option>{{end}}
      </select></label>
      <label for="hotkey-input">Hotkey<input id="hotkey-input" name="hotkey" value="{{.Editor.Hotkey}}" placeholder="Press a key combination" autocomplete="off" spellcheck="false" maxlength="80"></label>
      <p id="hotkey-capture-error" role="alert" hidden></p>
      <div class="preferences-actions"><button type="submit" class="primary-button">Save hotkey</button><button type="button" id="clear-hotkey">Clear hotkey</button></div>
    </form>
    <dl class="hotkey-assignments" aria-label="Current hotkeys">{{range .Hotkeys}}<div><dt>{{.Label}}</dt><dd>{{if .Binding}}<kbd>{{.Display}}</kbd>{{else}}Unassigned{{end}}</dd></div>{{end}}</dl>
    <form action="/preferences" method="post"><button type="submit" name="reset" value="hotkeys">Clear all hotkeys</button></form>
  </section>
</main>
<script src="/static/js/shared/hotkeys.js?v=1" defer></script>
<script src="/static/js/account/preferences.js?v=1" defer></script>
</body>
</html>`))

func (m *Manager) showPreferences(w http.ResponseWriter, r *http.Request) {
	user := auth.CurrentUser(r)
	if user == nil {
		http.Error(w, "Sign in to view preferences.", http.StatusUnauthorized)
		return
	}
	preferences := m.preferences.ForUser(user.ID)
	action := r.URL.Query().Get("action")
	if !validHotkeyAction(action) {
		action = model.HotkeyActions()[0].ID
	}
	m.renderPreferences(w, r, preferences, preferenceEditor{action, model.HotkeyLabel(preferences.Hotkeys[action])}, "", http.StatusOK)
}

type preferenceEditor struct{ Action, Hotkey string }

func (m *Manager) renderPreferences(w http.ResponseWriter, r *http.Request, preferences model.PlayerPreferences, editor preferenceEditor, message string, status int) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	if err := preferencesTemplate.Execute(w, struct {
		User             *auth.User
		Hotkeys          []hotkeyPreferenceRow
		Editor           preferenceEditor
		PreferenceError  string
		PreferencesSaved bool
	}{auth.CurrentUser(r), hotkeyPreferenceRows(preferences), editor, message, r.URL.Query().Get("saved") == "1" && message == ""}); err != nil {
		log.Println("Render player preferences:", err)
	}
}

type hotkeyPreferenceRow struct {
	model.HotkeyAction
	Binding, Display string
}

func hotkeyPreferenceRows(preferences model.PlayerPreferences) []hotkeyPreferenceRow {
	var rows []hotkeyPreferenceRow
	for _, action := range model.HotkeyActions() {
		binding := preferences.Hotkeys[action.ID]
		rows = append(rows, hotkeyPreferenceRow{action, binding, model.HotkeyLabel(binding)})
	}
	return rows
}

func validHotkeyAction(id string) bool {
	for _, action := range model.HotkeyActions() {
		if action.ID == id {
			return true
		}
	}
	return false
}

func (m *Manager) savePreferences(w http.ResponseWriter, r *http.Request) {
	user := auth.CurrentUser(r)
	if user == nil {
		http.Error(w, "Sign in to save preferences.", http.StatusUnauthorized)
		return
	}
	preferences := m.preferences.ForUser(user.ID)
	editor := preferenceEditor{Action: model.HotkeyActions()[0].ID}
	fail := func(message string, status int) {
		m.renderPreferences(w, r, m.preferences.ForUser(user.ID), editor, message, status)
	}
	r.Body = http.MaxBytesReader(w, r.Body, 4096)
	if err := r.ParseForm(); err != nil {
		fail("The preferences form could not be read. Try again.", http.StatusBadRequest)
		return
	}
	editor = preferenceEditor{r.PostForm.Get("action"), r.PostForm.Get("hotkey")}
	allowed := map[string]bool{"action": true, "hotkey": true, "reset": true}
	for key, values := range r.PostForm {
		if !allowed[key] || len(values) != 1 {
			fail("The preferences form contains an unknown or repeated field.", http.StatusBadRequest)
			return
		}
	}
	if reset := r.PostForm.Get("reset"); reset != "" {
		if reset != "hotkeys" {
			fail("Choose Save hotkey or Clear all hotkeys.", http.StatusBadRequest)
			return
		}
		preferences.Hotkeys = nil
		editor = preferenceEditor{Action: model.HotkeyActions()[0].ID}
	} else {
		if !validHotkeyAction(editor.Action) {
			fail("Choose a utility to change its hotkey.", http.StatusBadRequest)
			return
		}
		binding, err := model.NormalizeHotkey(editor.Hotkey)
		if err != nil {
			fail(err.Error(), http.StatusBadRequest)
			return
		}
		if preferences.Hotkeys == nil {
			preferences.Hotkeys = make(map[string]string)
		}
		if binding == "" {
			delete(preferences.Hotkeys, editor.Action)
		} else {
			preferences.Hotkeys[editor.Action] = binding
		}
	}
	if err := preferences.Validate(); err != nil {
		fail(err.Error(), http.StatusBadRequest)
		return
	}
	var saveErr error
	if r.PostForm.Get("reset") == "hotkeys" {
		saveErr = m.preferences.Save(user.ID, preferences)
	} else {
		saveErr = m.preferences.SetHotkey(user.ID, editor.Action, preferences.Hotkeys[editor.Action])
	}
	if err := saveErr; err != nil {
		if errors.Is(err, persistence.ErrPreferenceValidation) {
			fail("That hotkey is already assigned to another utility.", http.StatusBadRequest)
			return
		}
		log.Println("Save player preferences:", err)
		message, status := "Your preferences could not be saved. Try again.", http.StatusInternalServerError
		if errors.Is(err, persistence.ErrStorageLimit) {
			message, status = "Your preferences could not be saved because server storage is full.", http.StatusInsufficientStorage
		}
		fail(message, status)
		return
	}
	http.Redirect(w, r, "/preferences?saved=1&action="+editor.Action, http.StatusSeeOther)
}
