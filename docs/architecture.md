# Repository architecture

The Go backend is organized by responsibility. Browser JavaScript is grouped by
feature under `static/js`. Tests live beside the Go code they exercise, with
browser regression scenarios under `internal/handlers/testdata`.

See the [Go architecture diagrams](architecture-diagrams.md) for package imports,
struct/interface relationships, and Go file ownership.

## Backend packages

| Package | Responsibility |
| --- | --- |
| `internal/app/model` | Live board state; token, map, drawing, and artwork types; size/status rules; saved-board validation and capacity limits. |
| `internal/app/persistence` | Unit/map libraries, file writes, disk quotas, MongoDB adapters, stored membership, and invitation records. |
| `internal/auth` | OAuth providers, sign-in callbacks, account profiles, login sessions, and server admission checks. |
| `internal/handlers` | Board HTTP requests, HTML rendering, artwork responses, and WebSocket communication. |
| `internal/tables` | Tabletop creation/deletion, membership enforcement, per-table initialization, and routing. |
| `internal/plugins` | Plugin discovery, validation, and resolving bundled/operator content. |
| `internal/artwork` | Image conversion and sizing. |

The bundled content in root `plugins/` is distinct from the Go loader in
`internal/plugins`. The root package embeds bundled defaults for runtime use.

`model` is the shared foundation: it uses the standard library and embedded
bundled content. It has no HTTP, filesystem persistence, or MongoDB dependency.
`persistence` depends on `model`. The authentication, plugin, HTTP, and tabletop
packages consume those lower-level packages. Keep model changes independent of
transport and database code so the same validation applies to live and saved
boards.

Examples:

- A new token size or a change to saved drawing validation belongs in `model`.
- A change to invitation storage or MongoDB serialization belongs in `persistence`.
- A new provider's profile mapping belongs in `auth`.
- A new board endpoint belongs in `handlers`; a new tabletop lifecycle action belongs in `tables`.

## WebSocket responsibilities

The shared `WebSocketHandler` type remains in one Go package so its locks,
connection registries, and drawing state retain their existing ownership.

| File in `internal/handlers` | Responsibility |
| --- | --- |
| `websocket.go` | Shared handler state, construction, route registration, and board broadcasts. |
| `socket_connection.go` | Upgrades, connection limits, heartbeat deadlines, and cleanup. |
| `socket_positions.go` | Receiving token movement and broadcasting position snapshots. |
| `socket_board.go` | Initial board snapshots, drawing-message dispatch, and client registration/disconnection. |
| `socket_writer.go` | Ordered outgoing messages and coalesced position writes. |
| `pen.go`, `fog.go`, `ruler.go`, `presence.go` | Feature-specific updates and rules. |

Go subdirectories form separate packages. Methods on a type stay in the package
that defines that type. Before extracting another package, define the operations
it owns and the API its callers need. A future realtime package will need a board
service API because HTTP map loading/saving currently shares its drawing state
and locks with the socket handler.

## Browser features

```text
static/
  js/
    account/    invitation copying and lobby actions
    board/      input, geometry, zoom/pan, tools, tokens, and board controls
    dice/       dice form/results and animation adapter
    library/    artwork framing, search, and map editor
    realtime/   reconnecting sockets, board synchronization, and online users
    shared/     floating menus and notification lifecycle
  style.css
  images/
  vendor/
```

The board's script tags in root `index.html` define execution order. Scripts use
`defer`, run in that order, and share a small set of globals and document events.
Preserve that order when adding dependencies:

- `realtime/connection.js` provides `boardURL` and `connectBoardSocket`.
- `shared/menuUI.js` loads before scripts that create menus.
- `board/boardInput.js` loads before drag and drawing tools.
- `shared/touchHover.js` loads after board input and before board zoom so
  multi-touch cancels pending hover before the pinch handlers consume events.
- `board/drag.js` defines drag state used by `realtime/boardSync.js`.
- `board/boardGeometry.js` loads before tools that calculate board coordinates.
- `shared/notifications.js` and `dice/diceAnimation.js` load before `dice/dice.js`.

Account pages load their own scripts from the Go page templates. All static
URLs start with `/static/`; HTTP and WebSocket requests for board content use
`boardURL` to retain tabletop scoping. Third-party assets retain their existing
paths under `static/vendor`.

Update every script reference and documentation link when moving a file. Bump a
script or stylesheet's `?v=` value when changing its contents at an existing URL.
A moved asset has a new URL, which also changes its cache identity.

## Verification and further extraction

Run `go test ./...` from the repository root. Firefox enables the browser tests,
which exercise actual script loading, HTMX, menus, drawing, and WebSocket updates.
Container builds copy `internal/` and `static/` recursively, including new
subdirectories.

The large shared stylesheet and browser scenario file are useful candidates for
later feature-based splits. Further backend extraction should separate storage
adapters or board services behind explicit interfaces, keeping transaction and
lock ownership clear. `auth`, `plugins`, and `tables` already group related
responsibilities; introduce subpackages as independent responsibilities emerge.
