package auth

import (
	"context"
	"net/http"

	"golang.org/x/oauth2"
)

// Provider contains the service-specific parts of OAuth sign-in. OAuth2 itself
// does not define a user-profile endpoint or its response format.
type Provider interface {
	Name() string
	LoginPath() string
	Endpoint() oauth2.Endpoint
	Scopes() []string
	User(context.Context, *http.Client) (User, error)
}
