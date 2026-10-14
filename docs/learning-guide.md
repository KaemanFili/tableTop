# Learning guide: how tableTop works

This project is organized around one simple rule: each tabletop session owns its own live state, permissions, and realtime sockets. The server is small enough to read in one pass, but it has a few layers that are easy to confuse:

- [main.go](../main.go) boots the app and selects authentication or dev mode.
- [internal/tables/tables.go](../internal/tables/tables.go) creates and scopes per-table sessions.
- [internal/handlers/endpoints.go](../internal/handlers/endpoints.go) handles HTTP requests and renders HTML templates.
- [internal/handlers/websocket.go](../internal/handlers/websocket.go) owns every board websocket connection and the shared live state.
- [internal/app/model/state.go](../internal/app/model/state.go) and [internal/app/model/units.go](../internal/app/model/units.go) define the in-memory board model.
- [internal/app/persistence](../internal/app/persistence) loads and saves the stored libraries and per-session documents.

If you keep that mental model in mind, the code becomes much easier to follow.

## 1. The startup path

Startup begins in [main.go](../main.go):

1. The process reads flags and environment variables.
2. It builds a storage limits object and configures authentication.
3. If auth is enabled, it registers the authenticated routes and wraps the mux with the auth middleware.
4. If dev mode is enabled, it creates a local development board instead of the production session flow.
5. It starts an HTTP server on port 18080 with the global request guard.

The key difference between dev mode and the normal app is this:

- dev mode creates a single board directly from plugin defaults and local libraries.
- production mode creates a lobby and per-session table manager through [internal/tables/tables.go](../internal/tables/tables.go).

That split is important. The production app is not one board with many users; it is one server that creates many independent tabletop sessions.

## 2. The session boundary

The session boundary lives in [internal/tables/tables.go](../internal/tables/tables.go).

The manager:

- owns a table library keyed by session ID
- validates ownership and membership
- resolves the plugin binding for a session
- loads per-session storage folders for units and maps
- creates a fresh AppState, WebSocketHandler, and EndpointHandler for each table

This code is the “front door” for a session. When a request arrives at a URL like /sessions/ABC123/, the manager looks up the session, verifies the user can access it, and then serves the board handler for that table.

That means the board is not a global singleton. Every session gets its own:

- live unit state
- socket registries
- map state
- drawing and fog state
- permission context

This is why multiplayer state can be kept isolated and why the same server can support multiple game tables at once.

## 3. The model layer: the real board state

The live board model is in [internal/app/model](../internal/app/model).

The core type is AppState in [internal/app/model/state.go](../internal/app/model/state.go). It keeps:

- a mutex and an in-memory map of units
- a monotonically increasing unit ID counter
- allowed status options for the current table

The interesting behavior is intentionally simple:

- AddUnit creates a new token with defaults and a generated ID
- UpdateUnitPosition mutates the stored token position
- RemoveUnit deletes it
- Units returns a snapshot for rendering and broadcasting
- RestoreUnits is used to reload a saved board without reusing stale client IDs

The token itself is split into two concepts:

- UnitDefinition: a reusable library entry, such as a unit template stored in the library
- UnitData: a placed token in the current session state

The separation is deliberate. Library content is not the same as live board content, and that distinction is one of the biggest conceptual steps to understand in this repo.

## 4. HTTP routes and HTML rendering

Board endpoints are grouped in [internal/handlers/endpoints.go](../internal/handlers/endpoints.go).

This handler does three jobs:

1. It registers all board routes.
2. It renders HTML templates from the templates folder and root HTML page.
3. It applies business logic to the current AppState and WebSocketHandler.

Important examples:

- /: render the main board page
- /loadExistingUnits: send every current placed token back to the browser
- /unit: spawn or manipulate unit templates
- /unit/{id}/size: resize a placed token
- /unit/{id}/status: update a status effect
- /board/clear: remove all board content for that session
- /map-library and /unit-library: manage saved content and upload data

The route registration style is modern Go, using ServeMux patterns like POST /unit and DELETE /unit/{id}. For a board endpoint, the code often follows this pattern:

- validate permissions / defaults
- load or mutate AppState
- write the HTTP response
- broadcast the result with the websocket handler if needed

This is the place where a new feature usually starts when you are adding a board action.

## 5. WebSockets: the realtime layer

The realtime layer is centered on [internal/handlers/websocket.go](../internal/handlers/websocket.go) and related files under [internal/handlers](../internal/handlers).

This handler is intentionally at the center of the board runtime. It owns:

- connection registries for board clients and position clients
- mutexes for ordered writes
- drawing state such as rulers, pen strokes, fog marks, and board generation
- the methods that upgrade HTTP requests to websocket connections

There are three main message categories in practice:

- position updates for moving tokens
- board utility messages for rulers, drawings, and fog
- board snapshot updates for new users and board resets

The key rule is: the board state is updated in the Go server, then synchronized to every connected client via the websocket handler. The browser does not “own” the live board; it reflects a server truth.

## 6. Front-to-back request chains

### A. Starting the server

- [main.go](../main.go) configures environment and builds the root mux.
- If auth is enabled, [internal/auth/auth.go](../internal/auth/auth.go) wraps the mux with a middleware that enforces login and same-origin request checks.
- For each session, [internal/tables/tables.go](../internal/tables/tables.go) creates the board runtime on demand.

### B. Loading a board page

- Browser requests /sessions/{table}/
- [internal/tables/tables.go](../internal/tables/tables.go) checks membership and route scoping
- It creates or reuses the managed board for that session
- [internal/handlers/endpoints.go](../internal/handlers/endpoints.go) renders the main board page and the initial unit set
- Browser opens the websocket connections for live position and board utility state

### C. Spawning a token

1. Browser posts to POST /unit or POST /unit?unitID=...
2. [internal/handlers/endpoints.go](../internal/handlers/endpoints.go) validates the unit definition and board size limit.
3. It calls AppState.AddUnit.
4. It writes a rendered HTML fragment for the token.
5. It calls the websocket broadcast helper so all clients see the new token.
6. The browser receives the socket event and updates the board state.

This is a good example of a simple, consistent pattern: mutate state in the model, write the immediate HTTP response, and broadcast the change.

### D. Moving a token live

- Browser sends a position message over /ws/updateServer.
- [internal/handlers/socket_positions.go](../internal/handlers/socket_positions.go) unmarshals the message.
- It calls AppState.UpdateUnitPosition.
- It echoes the message back to the current client and broadcasts the updated state to other viewers.

This path is intentionally low-level and compact. The model is the source of truth, and the websocket handler is mostly a transport layer plus an event fanout.

### E. Saving or loading a map

- Browser calls POST /map-library or POST /map-library/{id}/load.
- The handler validates the requested map and/or board data.
- It updates or restores the saved board in the per-table storage layer.
- It updates the current AppState and websocket display state so everyone sees the board change.

Map state is treated as session-scoped content, not global app state, which keeps different tables independent.

## 7. The most important design patterns in this repo

### Shared truth

The live board is a server-side state object. Browser Javascript is only a behavior layer and presentation layer. It requests updates and renders them; it does not determine the canonical board state.

### Session isolation

Every tabletop has independent content and socket state. The same app can run multiple independent games without collisions.

### HTTP + websocket split

- HTTP handles form submissions, page loads, and asset retrieval.
- WebSocket handles realtime updates that need low-latency syncing.

### “Model-first” updates

Most logic begins by mutating the AppState or a storage library, then updating the UI or broadcasting the result.

## 8. Learning strategy

If you are new to the codebase, these are the best files to read in order:

1. [main.go](../main.go)
2. [internal/tables/tables.go](../internal/tables/tables.go)
3. [internal/app/model/state.go](../internal/app/model/state.go)
4. [internal/handlers/endpoints.go](../internal/handlers/endpoints.go)
5. [internal/handlers/websocket.go](../internal/handlers/websocket.go)
6. [internal/handlers/socket_positions.go](../internal/handlers/socket_positions.go)
7. [internal/auth/auth.go](../internal/auth/auth.go)
8. one feature-specific file such as [internal/handlers/maps.go](../internal/handlers/maps.go) or [internal/handlers/load_map.go](../internal/handlers/load_map.go)

That order matches the actual runtime: bootstrap, session creation, board model, HTTP actions, realtime sync, then feature work.

## 9. Refactor opportunities

This exercise also surfaces a few places where the code could be simplified to more common interfaces.

### A. Separate the board service from the socket transport

The current WebSocketHandler mixes several responsibilities:

- websocket lifecycle and upgrades
- board state mutation fanout
- feature-specific drawing state
- map/fog/ruler bookkeeping
- permission checks passed from the HTTP request context

This is workable, but the natural next step is a board service with an interface such as:

- CreateSession
- AddUnit
- UpdateTokenPosition
- ClearBoard
- LoadMap
- HandleRealtimeMessage

Then the websocket layer can become a thin transport adapter that calls the board service rather than owning the board rules directly.

### B. Replace ad hoc message maps with typed websocket messages

Many handlers marshal JSON objects with a Type field and then switch on that string. This works, but it is hard to validate and easy to drift. A common interface here would be:

- a small typed message struct per message kind
- a shared message decoder or registry
- a single message validation layer

This would reduce runtime mistakes and improve testability.

### C. Reduce duplication between UnitDefinition and UnitData

The code currently has a clear conceptual split, but the fields overlap heavily. A common pattern would be to isolate shared metadata in a common type or embed a base entity type and then layer board-only fields on top. That would make it easier to build generic library and session APIs without repeated field-by-field conversions.

### D. Narrow the AppState API to explicit board operations

The AppState is already a good fit for the model layer, but it exposes a few methods that are board-specific and a few that are storage/migration-specific. A more common interface would be something like:

- BoardState
- BoardStore
- BoardSnapshot

This would make it clearer which operations are pure board logic versus persistence or session bootstrapping.

### E. Make per-table lifecycle explicit

The manager currently creates a board handler on-demand and stores it in a map. That is reasonable, but the lifecycle is spread across three layers: session manager, websocket handler, and endpoint handler. A more common interface would make a board session a first-class object with a clear lifecycle:

- initialize
- serve HTTP
- open socket connections
- close / cleanup

That would reduce hidden state and make teardown simpler and easier to reason about.

## 10. Bottom line

The main idea to keep in mind is this:

- [main.go](../main.go) boots the app.
- [internal/tables/tables.go](../internal/tables/tables.go) creates isolated sessions.
- [internal/app/model](../internal/app/model) owns the canonical board state.
- [internal/handlers/endpoints.go](../internal/handlers/endpoints.go) handles requests.
- [internal/handlers/websocket.go](../internal/handlers/websocket.go) keeps every client in sync.

Once you understand that layering, the codebase is much easier to navigate and the “where do I add a feature?” question becomes much clearer.
