# Running tableTop in containers

Requires Docker with Compose. The Go build runs in a builder image;
the final image contains the static executable, browser assets, templates, and
HTTPS certificate bundle. It runs as UID/GID 10001 without a shell or compiler.

## Start the app

Run the commands below from the repository root. Docker configuration lives in
`compose/`. The repository-root `.dockerignore` excludes runtime credentials
and generated binaries from the build context.

Prepare the runtime configuration and secret file:

```sh
mkdir -p compose/secrets
chmod 700 compose/secrets
touch compose/secrets/oauth2_client_secret
chmod 644 compose/secrets/oauth2_client_secret
```

The private host directory restricts access to your user; the secret file must
also be readable by the container's UID 10001. Compose file secrets preserve
host file permissions.

Put only the Discord client secret in `compose/secrets/oauth2_client_secret`, with no
variable name or surrounding quotes. Create `compose/.env` with the public
client ID and redirect URL:

```dotenv
OAUTH2_CLIENT_ID=your-application-client-id
OAUTH2_REDIRECT_URL=http://localhost:18080/auth/discord/callback
TABLETOP_ADMIN_USER_IDS=your-numeric-discord-user-id
```

Compose mounts the client secret read-only as a file; its contents are not put
in the container's environment. The app reads it at startup. `compose/secrets/` is excluded from Git and the Docker build context. See
[Docker's Compose secrets guide](https://docs.docker.com/compose/how-tos/use-secrets/).
Normal startup requires complete OAuth settings and administrator IDs. Run
`./compose/compose.sh -dev up --build -d` for an explicit development board;
it uses a separate volume and no OAuth secret mount. See
[public hosting](public-hosting.md) for development mode, server invitations,
runtime limits, tunnel configuration, and backup/restore commands.

Compose interpolates these values into `compose/compose.yaml` and automatically
loads `.env` beside that file when using the commands below. Exported shell
variables take precedence. To keep the name `compose/tabletop.env`, pass
`--env-file compose/tabletop.env` on each Compose command instead:

```sh
docker compose --env-file compose/tabletop.env -f compose/compose.yaml up --build -d
```

The secret remains in its mounted file. [Discord setup](authentication.md)
covers application registration.

Stop an existing host server using port 18080 before starting Compose:

```sh
docker compose -f compose/compose.yaml up --build -d
docker compose -f compose/compose.yaml logs -f tabletop
```

Open `http://localhost:18080/`. For hosted Discord login, use the public HTTPS
callback URL and place an HTTPS reverse proxy in front of the app.

After changing `compose/.env`, `compose/compose.yaml`, or the secret file, recreate the service to load
the new values:

```sh
docker compose -f compose/compose.yaml up -d --force-recreate
```

After changing application code or assets, rebuild both executables and the image:

```sh
./compose/rebuild.sh
```

The script builds the local `main` executable, then runs `docker build` to create
the `tabletop-tabletop` image used by Compose. Go and a running Docker daemon are
required; runtime configuration is needed only when starting the container. The script checks
these prerequisites before building, stops on failure, and works from any
working directory.
To replace the running container afterward:

```sh
docker compose -f compose/compose.yaml up -d --no-build --force-recreate
```

The Dockerfile uses ordinary build steps and Docker layer caching. The rebuild
script selects the classic builder, so Buildx is not required.
`docker compose -f compose/compose.yaml down` stops the app and MongoDB and retains
their data volumes.

## Persistent data

The default configuration mounts a named volume at `/app/data`. Session
membership, unit libraries, saved maps, and artwork survive container replacement.
The volume starts independently of an existing host `data/` directory.
Live board state and login sessions still reset when the process restarts, as
they do outside a container. Save maps to retain live board content.

Compose includes MongoDB for map/unit definitions and status definitions. The
app waits for the database health check before starting. MongoDB has no published
host port and uses its own persistent volumes; development mode uses separate
volumes for both services. Artwork and session membership stay in the app volume.
See [MongoDB storage](mongodb.md) for imports, settings, and database backups.
The file backup scripts cover the app volume only; the limits below count local
files only.

### Storage limits

The app defaults to **5 GiB total** under `/app/data` and **512 MiB per tabletop**.
Stored PNG images, unit/map libraries, and saved boards count toward both
limits; the root session/membership library counts toward the total. Image
sizes are measured after conversion to PNG. The app
rejects growth that would cross either limit and explains the storage problem
in the form or save response. Existing data stays readable; deletions and
shrinking saves remain available when storage is full.

Override the limits in `compose/.env` using positive byte counts:

```dotenv
TABLETOP_STORAGE_LIMIT_BYTES=5368709120
TABLETOP_SESSION_STORAGE_LIMIT_BYTES=536870912
```

Recreate the service after changing these values. For a host process, export
the same variables before starting it. The tabletop limit must not exceed the
total limit. The standalone development board also uses the tabletop limit.

All libraries in one process share the quota check, so concurrent uploads
cannot bypass the global limit. Disk usage is measured before growing writes,
including existing files after restart and artwork retained for placed units
or saved boards. Deleting a unit definition preserves its artwork; deleting a
map removes that map's image. Existing data is never automatically deleted.

These limits cap committed file bytes written by the app. Atomic library/image
updates temporarily keep both old and new files on disk, so leave additional
free space. Filesystem overhead, Docker images/logs, backups, and writes from
other processes are outside this cap. An ordinary Docker named volume has no
configured size limit of its own; for an OS-enforced physical ceiling, use a
filesystem quota or a dedicated filesystem for the data directory. Run one app
process per data root, as the app quota lock is held within that process.

To use your existing host `data/` directory instead, use the optional override
on Linux with your host UID and GID:

```sh
mkdir -p data
TABLETOP_UID="$(id -u)" TABLETOP_GID="$(id -g)" \
  docker compose -f compose/compose.yaml -f compose/compose.host-data.yaml up --build -d
```

Use those same environment values and Compose files for subsequent commands.
This keeps ownership compatible with your host user. Run one app process per
data directory; stop the host app before sharing that directory with a container.

Compose keeps the image filesystem read-only and mounts `/tmp` separately for
transient files. Persistent writes go to `/app/data`. The client ID and redirect
URL are ordinary configuration; the client secret uses the separate read-only
mount.

## Verification

The static Go build and Compose YAML can be checked locally. To validate the
actual container on a machine with Docker:

```sh
docker compose -f compose/compose.yaml config --quiet
DOCKER_BUILDKIT=0 docker build -f compose/Dockerfile -t tabletop-tabletop .
docker compose -f compose/compose.yaml up -d
curl --fail http://localhost:18080/
```

With Discord configured, the root URL redirects to the login page. Finish a
Discord login, create a tabletop, and reconnect after recreating the container
to check persisted membership and saved maps. Keep runtime credentials out of
build arguments and Dockerfile `ENV` instructions.

## Possible future architecture

Session creation could launch a dedicated container and Go process for each
tabletop, with shared plugin metadata cached in Redis. This would require worker
orchestration, session routing, and live-state persistence. See
[proposed session workers](session-workers.md) for the design and tradeoffs.
