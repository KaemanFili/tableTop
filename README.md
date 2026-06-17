# tableTop
ttrpg table top tool meant to run in browser

## Structure

- `main.go`: application entrypoint and route wiring.
- `internal/app`: shared application state and data types.
- `internal/handlers`: HTTP endpoint handlers and websocket handlers.
- `static`: browser JavaScript and CSS.
- `templates`: reusable HTML templates served by handlers.
