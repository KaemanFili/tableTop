# Discord sign-in and tabletop sessions

Players click **Sign in with Discord**, approve the request, and arrive at **Your
sessions**. Creating a session makes that user its GM. Joining with its code makes
a user a player. A creator remains the GM when returning to their own session.
The board's **Sessions** link returns to the lobby; **Sign out** ends the login.

Players do not supply application credentials or access tokens. The server uses
Discord's OAuth2 authorization-code flow with the `identify` scope to retrieve
the user's ID and display name. The browser receives an opaque session cookie
and automatically includes it in HTTP and WebSocket connections.

## One-time server setup

Create an application in the [Discord Developer Portal](https://discord.com/developers/applications).
In its OAuth2 settings, register the exact redirect URL used by the server. For
local development, use `http://localhost:18080/auth/discord/callback` and open the
app using the same `localhost` hostname.

For containers, put the client secret in `compose/secrets/oauth2_client_secret` and
the client ID and redirect URL in `compose/.env`, as described in
[container setup](containers.md). For a host process, save the secret in a local
file and set these environment variables before starting the server:

```sh
export OAUTH2_CLIENT_ID='your application client ID'
export OAUTH2_CLIENT_SECRET_FILE='/path/to/oauth2_client_secret'
export OAUTH2_REDIRECT_URL='http://localhost:18080/auth/discord/callback'
export TABLETOP_ADMIN_USER_IDS='your-numeric-discord-user-id'
go run .
```

For a hosted app, use its HTTPS URL, such as
`https://tabletop.example.com/auth/discord/callback`, in both Discord and
`OAUTH2_REDIRECT_URL`. A reverse proxy can terminate HTTPS and forward requests
to the Go server on port 18080. Keep the client secret in its mounted file.
Only the app operator performs this setup; players use the sign-in button.

See [Discord's OAuth2 documentation](https://discord.com/developers/docs/topics/oauth2)
for application registration, authorization-code exchange, and the `identify`
scope. No bot installation, email permission, or Discord server membership is
required for this login flow.

The direct `OAUTH2_CLIENT_SECRET` variable remains supported for host-process
compatibility; set either it or `OAUTH2_CLIENT_SECRET_FILE`, never both.
An unreadable secret file stops startup.

Normal startup requires all OAuth settings and `TABLETOP_ADMIN_USER_IDS`.
Use `go run . -dev` or `./compose/compose.sh -dev up -d` to explicitly enable the
local development board, including `?gm=true`. Partial configuration stops normal startup. When Discord
is configured, login is required and GM permissions come from session ownership;
adding `?gm=true` does not grant access.

Administrators sign in without invitations and use **Invite players** in the
lobby to admit other Discord accounts. Each invitation admits one account,
expires after seven days, and can be revoked before use. Admission persists in
`data/access.json`; returning members do not need new invitations. Invited
players still need each game's join code. See [public hosting](public-hosting.md)
for configuration, limits, and backups.

## Session behavior and storage

Login sessions last 12 hours and are stored in server memory. Cookies are
HttpOnly and SameSite=Lax; hosted HTTPS sessions also use Secure cookies. OAuth
state is checked against the browser's cookie and a single-use server record.
Logout or expiry revokes the cookie's server session and closes its open sockets.
A server restart requires users to sign in again.

The OAuth protocol uses `golang.org/x/oauth2` for authorization URLs, code
exchange, and authenticated HTTP requests. Discord's endpoints, `identify`
scope, and profile mapping live in a provider adapter. The authentication
manager accepts an `auth.Provider` through `auth.Config.Provider`; the
environment-based app setup defaults to Discord. Additional adapters must
return stable user IDs unique to their provider to avoid identity collisions.

The callback route comes from `OAUTH2_REDIRECT_URL`, with a path under `/auth/`
ending in `/callback` (for example `/auth/callback`). The existing
`/auth/discord/callback` remains supported, so existing Discord registrations
and environment settings continue to work. Register any changed callback URL
with the provider as well.

Tabletop names, join codes, owners, and membership persist in `data/sessions.json`.
Each tabletop has independent unit/map libraries and artwork in
`data/sessions/<join-code>/`. Live tokens, drawings, fog, and rulers reset on
restart; save a map to retain board content using the existing map workflow.
The existing local development libraries remain in `data/`.

Board URLs have the form `/sessions/<join-code>/`. Requests, artwork, and sockets
use that same prefix, so multiple tabs can use different sessions. Nonmembers
cannot access a session's endpoints or WebSockets. Opening a shared board link
offers the join form after sign-in. A join code grants membership, so share it
with the people joining that tabletop.

The authentication tests use a simulated Discord provider to check state,
identity, cookie handling, replay rejection, failed requests, and revocation.
Tabletop tests cover creation, joining, ownership, stored membership, request
scoping, and independent broadcasts. A real Discord application is required to
verify the live provider login.
