package auth

import (
	"html/template"
	"net/http"
	"net/url"

	"tableTop/main/internal/app"
)

// Access separates server enrollment from each tabletop's membership.
type Access interface {
	Allowed(string) bool
	IsAdmin(string) bool
	Admit(string, string) error
	CreateInvitation(string) (string, error)
	Invitations(string) []app.Invitation
	RevokeInvitation(string, string) error
}

var adminTemplate = template.Must(template.New("admin").Parse(`<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width,initial-scale=1">
  <title>Invite players · tableTop</title>
  <link rel="icon" href="/static/favicon.ico?v=1" sizes="16x16 32x32 48x48" type="image/x-icon">
  <link rel="icon" href="/static/favicon-64.png?v=1" sizes="64x64" type="image/png">
  <link rel="stylesheet" href="/static/style.css?v=52">
</head>
<body class="lobby-page admin-page">
<main>
  <header class="lobby-header">
    <div>
      <h1>Invite players</h1>
      <p>Manage who can join your server.</p>
    </div>
    <a class="page-nav" href="/lobby">← Back to sessions</a>
  </header>
  {{if .Error}}<p class="lobby-error" role="alert">{{.Error}}</p>{{end}}
  <div class="admin-layout">
    <section class="admin-create">
      <h2>Create an invitation</h2>
      <p>Each link admits one player and expires after seven days. Share it privately.</p>
      <form action="/admin/invites" method="post"><button class="primary-button">Create invitation</button></form>
      {{if .Link}}
      <div class="invite-created">
        <p role="status">Invitation created. Copy this link now; it is only shown once.</p>
        <label>Invite link<input readonly value="{{.Link}}" onclick="this.select()"></label>
      </div>
      {{end}}
      <p class="admin-note">Players also need a game’s join code after accepting an invitation.</p>
    </section>
    <section class="admin-pending">
      <div class="section-heading"><h2>Pending invitations</h2><span class="section-count">{{len .Invites}}</span></div>
      <p>Unused links that can still be accepted.</p>
      <div class="invite-list">
        {{range .Invites}}
        <article class="invite-row">
          <div><p>Expires <time datetime="{{.Expires.Format "2006-01-02T15:04:05Z07:00"}}">{{.Expires.Format "2006-01-02 15:04 UTC"}}</time></p></div>
          <form action="/admin/invites/{{.ID}}/revoke" method="post"><button class="danger-button">Revoke invitation</button></form>
        </article>
        {{else}}<p class="lobby-empty">No pending invitations.</p>{{end}}
      </div>
    </section>
  </div>
</main>
</body>
</html>`))

func (m *Manager) admin(w http.ResponseWriter, r *http.Request) {
	user := CurrentUser(r)
	if m.config.Access == nil || user == nil || !m.config.Access.IsAdmin(user.ID) {
		http.Error(w, "Only server administrators can manage invitations.", http.StatusForbidden)
		return
	}
	var link, message string
	if r.Method == http.MethodPost {
		var err error
		if id := r.PathValue("invite"); id != "" {
			err = m.config.Access.RevokeInvitation(user.ID, id)
		} else {
			var code string
			code, err = m.config.Access.CreateInvitation(user.ID)
			if err == nil {
				link = m.origin + "/login?invite=" + url.QueryEscape(code)
			}
		}
		if err != nil {
			message = "Could not update invitations. Check server storage or the pending invitation limit."
		}
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	adminTemplate.Execute(w, struct {
		Link, Error string
		Invites     []app.Invitation
	}{link, message, m.config.Access.Invitations(user.ID)})
}
