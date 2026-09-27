# tableTop
ttrpg table top tool meant to run in browser

## Structure

- `main.go`: application entrypoint and route wiring.
- `internal/app`: shared application state and data types.
- `internal/handlers`: HTTP endpoint handlers and websocket handlers.
- `static`: browser JavaScript and CSS.
- `templates`: reusable HTML templates served by handlers.

## Unit images

Each unit has its own `ID` and an `ImageID` identifying its artwork. Multiple
units can share an image. Spawned units currently use `default-unit`.

Image files live in `static/images`. The `unitImages` registry in
`internal/handlers/images.go` maps image IDs to files, served through
`/images/{id}`. To register more artwork, add a file and a registry entry.
Unknown image IDs return 404. The registry can later be replaced with a SQLite
lookup without changing the unit's image reference or template.

Unit state is still held in memory and resets when the server restarts.
Movement updates change only coordinates, preserving the unit's image reference.
