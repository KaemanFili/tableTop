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

var adminTemplate = template.Must(template.New("admin").Parse(`<!doctype html><html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><title>Invite players · tableTop</title><link rel="stylesheet" href="/static/style.css?v=42"></head><body class="lobby-page"><main><a href="/lobby">Sessions</a><h1>Invite players</h1><p>Each link admits one player to this server and expires after seven days. Share it privately. Players still need a game’s join code.</p>{{if .Error}}<p role="alert">{{.Error}}</p>{{end}}{{if .Link}}<p role="status">Invitation created. Copy this link now; it is only shown once.</p><label>Invite link<input readonly value="{{.Link}}" onclick="this.select()"></label>{{end}}<form action="/admin/invites" method="post"><button>Create invitation</button></form><h2>Pending invitations</h2>{{range .Invites}}<article><span>Expires {{.Expires.Format "2006-01-02 15:04 UTC"}}</span><form action="/admin/invites/{{.ID}}/revoke" method="post"><button>Revoke invitation</button></form></article>{{else}}<p>No pending invitations.</p>{{end}}</main></body></html>`))

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
