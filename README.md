# tableTop
ttrpg table top tool meant to run in browser

## Development

Requires Go 1.27.1 or newer. Run the app from the project root with `go run .`.

Use `go fix ./...` to apply Go's automated source modernizations, then
`go fmt ./...` to format the code. Validate changes with `go test -race ./...`
and `go vet ./...`.

## Structure

- `main.go`: application entrypoint and route wiring.
- `internal/app`: shared application state and data types.
- `internal/handlers`: HTTP endpoint handlers and websocket handlers.
- `static`: browser JavaScript and CSS.
- `templates`: reusable HTML templates served by handlers.

## UI approach

Prefer HTMX requests and server-rendered HTML templates for UI interactions.
Keep custom JavaScript for interactions that need it, such as dragging and
the existing WebSocket synchronization.

The Spawn Unit menu is rendered by `templates/spawn-menu.html`. HTMX requests
`/spawn-menu?open=true` to open it and `/spawn-menu` to close it. Event filters
handle outside clicks and Escape. After spawning, the server sends
`HX-Trigger-After-Swap: unitSpawned` to close the menu through HTMX.
SPAWN NPC requests `/unit?kind=npc` and uses the cartoon goblin image.
SPAWN PC requests `/unit?kind=pc` and uses the cartoon knight image.

## Unit images

Each unit has its own `ID` and an `ImageID` identifying its artwork. Multiple
units can share an image. NPCs default to `goblin` and PCs default to `knight`.
Requests to `/unit` without a kind retain the neutral `default-unit` image.

Image files live in `static/images`. The `unitImages` registry in
`internal/handlers/images.go` maps image IDs to files, served through
`/images/{id}`. To register more artwork, add a file and a registry entry.
Unknown image IDs return 404. The registry can later be replaced with a SQLite
lookup without changing the unit's image reference or template.

Unit state is still held in memory and resets when the server restarts.
Movement updates change only coordinates, preserving the unit's image reference.
