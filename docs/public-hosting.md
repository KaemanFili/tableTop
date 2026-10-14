# Public hosting and server access

Normal startup requires OAuth configuration and `TABLETOP_ADMIN_USER_IDS` in
`compose/.env`. Set administrator IDs to a comma-separated list of numeric
Discord user IDs, not display names. Administrators can sign in without an
invitation and use **Invite players** in the lobby. Server administration does
not grant GM privileges in another user's tabletop.

Each generated invitation link admits one new Discord account and expires after
seven days. Share it privately. Only its hash is persisted, so the link is shown
once. Admins can revoke pending invitations. Admitted players are stored in
`access.json` in the active data directory and can return without another
invitation. Default Compose stores it at `/app/data/access.json` in the app's
named volume; the repository-root `data/` is used only for a host process started
there or the host-data override. See
[deployment storage locations](containers.md#persistent-data). Existing game
membership does not automatically enroll someone in the server: invite existing
players once after this change. There are at most 256 admitted accounts and
100 pending invitations. Game membership still requires its separate join code.

## Explicit development mode

Docker Compose itself cannot accept custom flags. The repository wrapper accepts
`-dev` and loads the development override, passing `-dev` to the app:

```sh
./compose/compose.sh -dev up --build -d
./compose/compose.sh -dev logs -f tabletop
./compose/compose.sh -dev down
```

This mode runs the unauthenticated development board with `?gm=true`. It uses a
separate volume and omits OAuth settings and secret mounts. Use normal
`./compose/compose.sh up -d` to return to authenticated operation. Without
explicit `-dev`, missing authentication configuration stops startup.
For a host process, use `go run . -dev` or `./main -dev`. Never route the
development board through a public tunnel.

## Network and resource limits

### Cloudflare tunnel (VTT)

The optional `compose/compose.tunnel.yaml` runs `cloudflared` alongside the app.
The tunnel name in Cloudflare can be **VTT**; its token identifies the tunnel,
so the name does not need to appear in the connector command.

Create the token file locally:

```sh
mkdir -p compose/secrets
chmod 700 compose/secrets
touch compose/secrets/cloudflare_tunnel_token
chmod 644 compose/secrets/cloudflare_tunnel_token
```

Open that file in your editor and paste only the tunnel token, without quotes,
`--token`, or the Docker command. This directory is excluded from Git and Docker
builds. Its private host permissions protect the files while the file permissions
allow the container to read its mounted secret. The connector uses Cloudflare's
[`--token-file` option](https://developers.cloudflare.com/cloudflare-one/networks/connectors/cloudflare-tunnel/configure-tunnels/run-parameters/#token-file),
keeping the token out of Compose YAML, container arguments, and environment variables.
If a token was shared in chat or otherwise exposed, rotate it before using it;
see [Cloudflare token rotation](https://developers.cloudflare.com/tunnel/reference/tunnel-tokens/).

In Cloudflare's **VTT** tunnel, add a published application route for
**vtt.kaeman.dev**, with service type **HTTP** and service URL **tabletop:18080**. Leave
the path and HTTP Host Header override unset. The connector shares the app's
Docker network; `localhost` inside it would refer to the connector itself.

Set `OAUTH2_REDIRECT_URL` in `compose/.env` to
`https://vtt.kaeman.dev/auth/discord/callback` and register the identical URL in
Discord's OAuth2 settings. Keep the existing client ID, secret, and administrator
IDs configured. Start normal authenticated hosting from the repository root:

```sh
./compose/compose.sh -tunnel up -d
./compose/compose.sh -tunnel logs -f cloudflared
```

The tunnel stays running across container restarts. If using host-backed artwork
storage, combine `-host-data -tunnel`. After replacing a rotated token file, run
`./compose/compose.sh -tunnel up -d --force-recreate cloudflared`.
Stop the connector with `./compose/compose.sh -tunnel stop cloudflared` before
switching to development mode. The wrapper rejects combining `-dev` and `-tunnel`.

### Origin network and limits

Compose publishes port 18080 only on `127.0.0.1`. A proxy or `cloudflared` running
on the host can forward to `http://localhost:18080`. Players connect through the
public HTTPS hostname; register that exact callback URL with Discord too.
Preserve the public Host header when forwarding WebSockets so Origin validation
works. A tunnel container should share the app's Docker network and forward to
`tabletop:18080`; its own `localhost` refers to the tunnel container.

The app container is capped at 1 GiB RAM, one CPU, and 128 processes. Docker logs
rotate at 10 MiB with three files retained. Upload decoding is limited to two
concurrent requests. HTTP headers have a 5-second timeout and 32 KiB limit;
request reads, response writes, and idle connections have bounded lifetimes.

WebSocket messages are limited to 1 KiB. Drawing/movement messages allow 120 per
second with a burst of 240. Connection limits are 96 per tabletop, 24 per user,
and 256 server-wide. Heartbeats close dead connections. Login attempts,
authenticated requests, uploads, and creation requests are rate-limited too.
Each account can have four simultaneous browser logins, with 512 server-wide.

There are at most 32 tabletops with 64 members each. Live boards permit 500
tokens, 1,000 strokes, 5,000 fog marks, and 50,000 drawing/fog points combined.
Erasing strokes reclaims their points; clearing the board resets drawing history
and erased IDs. Saved boards over these limits are refused on load and their
files are preserved. These bounds complement the [disk quota](containers.md#storage-limits).

## Backups and restore

The file backup below covers artwork and local settings. Map/unit documents and
status definitions live in MongoDB and require a separate database backup; see
[MongoDB backup and restore](mongodb.md#imports-backups-and-limits).

Back up the default app volume:

```sh
./compose/backup.sh
```

The script pauses a running app while archiving `/app/data`, then unpauses it
even if backup fails. It preserves in-memory state, but clients may need to
reconnect if a large backup takes longer than a heartbeat timeout. Archives
default to `backups/` with private file permissions. Run one backup at a time;
copy successful archives to another machine or storage service. These tools
require Docker and gzip; restore validation also requires Python 3.

Restore a matching file and MongoDB backup into **new volumes**, retaining the
original volumes. For the bundled MongoDB service:

```sh
./compose/compose.sh stop tabletop mongo
export TABLETOP_DATA_VOLUME=tabletop-restored-data
export TABLETOP_MONGODB_DATA_VOLUME=tabletop-restored-mongodb-data
export TABLETOP_MONGODB_CONFIG_VOLUME=tabletop-restored-mongodb-config
./compose/restore.sh backups/tabletop-YOUR-BACKUP.tar.gz
./compose/compose.sh up -d --wait mongo
./compose/compose.sh exec -T mongo mongorestore --archive --gzip --stopOnError < backups/tabletop-YOUR-BACKUP.archive.gz
./compose/compose.sh up -d tabletop
```

Choose unused volume names and keep the database name used by the backup. Finish
both restores successfully before starting the app. Restoring only the files
would pair older artwork and session membership with the current database's
map/unit documents. See [paired backup instructions](mongodb.md#imports-backups-and-limits).
For an external MongoDB server, restore into a new database and configure
`TABLETOP_MONGODB_URI` and `TABLETOP_MONGODB_DATABASE` before starting the app.

Save live board state before stopping. The app must be stopped and the file
restore target empty. File restore validates archive paths and refuses links or
special files. It creates a stopped app container to initialize volume ownership
before extracting the archive. Keep all selected volume names in `compose/.env`
afterward. A restore requires signing in again. Verify admitted membership,
saved maps, status definitions, and artwork.

For the host-data override, export `TABLETOP_HOST_DATA=1`, `TABLETOP_UID`, and
`TABLETOP_GID` before using the wrapper or backup scripts; the helper uses the
same bind mount. Restore still requires an empty host data directory.
Development and host-data modes cannot be combined.
