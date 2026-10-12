package auth

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/endpoints"
)

type discordProvider struct {
	userURL string
}

func NewDiscordProvider() Provider {
	return &discordProvider{userURL: "https://discord.com/api/users/@me"}
}

func (*discordProvider) Name() string      { return "Discord" }
func (*discordProvider) LoginPath() string { return "/auth/discord/login" }
func (*discordProvider) Scopes() []string  { return []string{"identify"} }
func (*discordProvider) Endpoint() oauth2.Endpoint {
	endpoint := endpoints.Discord
	// Discord accepts client credentials in the form body. Avoid probing a
	// different authentication style with a single-use authorization code.
	endpoint.AuthStyle = oauth2.AuthStyleInParams
	return endpoint
}

func (p *discordProvider) User(ctx context.Context, client *http.Client) (User, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, p.userURL, nil)
	if err != nil {
		return User{}, err
	}
	response, err := client.Do(request)
	if err != nil {
		return User{}, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return User{}, fmt.Errorf("Discord returned status %d", response.StatusCode)
	}
	var profile struct {
		ID         string `json:"id"`
		Username   string `json:"username"`
		GlobalName string `json:"global_name"`
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(&profile); err != nil {
		return User{}, err
	}
	if profile.ID == "" || profile.Username == "" {
		return User{}, fmt.Errorf("invalid Discord user response")
	}
	name := profile.GlobalName
	if name == "" {
		name = profile.Username
	}
	return User{ID: profile.ID, Name: name}, nil
}
