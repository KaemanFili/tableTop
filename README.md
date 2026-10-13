# tableTop
ttrpg table top tool meant to run in browser

## Development

Requires Go 1.27.1 or newer. Run the development board from the project root with `go run . -dev`,
then open `http://localhost:18080/` (or `http://127.0.0.1:18080/`). To use the
executable, rebuild with `go build -o main .`, then run `./main` from the project
root with `./main -dev`. Restart a running server after rebuilding; replacing the executable does
not update the running process.

Use `go fix ./...` to apply Go's automated source modernizations, then
`go fmt ./...` to format the code. Validate changes with `go test -race ./...`
and `go vet ./...`.

The full suite runs browser regression checks in headless Firefox when Firefox is
installed. Run them directly with `go test ./internal/handlers -run TestBrowserSync -v`.
They check actual dragging, spawning, and deliberately overlapping HTTP responses.
Use `go test -short ./...` to skip browser checks. Shared test fixtures locate
project assets without changing the process working directory.

HTMX 2.0.11 is vendored in `static/vendor/`, including its license and checksum.
The app serves it locally; dependency updates are explicit.

## Containers

Run `./compose/rebuild.sh` to rebuild both the local `main` executable and the
Docker image. The script works from any working directory and stops on failure.

Set the client ID and redirect URL in `compose/.env` and prepare
`compose/secrets/oauth2_client_secret` using the [container setup guide](docs/containers.md).
The client secret is mounted as a file; Compose loads the client ID and redirect URL
from `compose/.env`. Run `docker compose -f compose/compose.yaml up --build -d`. The app is available at
`http://localhost:18080/`; runtime credentials stay outside the image and Git.
Compose includes MongoDB with persistent volumes; the app uses it by default.
Artwork and local settings persist in the app volume. See [container setup](docs/containers.md) for
requirements, configuration updates, and using your existing host data directory.
Storage defaults to 5 GiB total and 512 MiB per tabletop; the container guide
explains how to configure these limits.
Normal startup requires OAuth and `TABLETOP_ADMIN_USER_IDS`. Administrators
invite players through the lobby's **Invite players** link. Run
`./compose/compose.sh -dev up --build -d` for a development board using a separate
volume. See [public hosting](docs/public-hosting.md) for limits and backup/restore.

## Discord sign-in

Players use **Sign in with Discord**, then create or join a tabletop session.
The creator is its GM; joining users are players. Each session has an independent
board and libraries. Owners can use **Delete tabletop** in the lobby, review the
confirmation page, and permanently remove a tabletop. Deletion disconnects its
players and removes its saved maps, units, and uploaded artwork. If cleanup fails,
only the owner sees the pending entry and can retry deletion. See [Discord setup and sessions](docs/authentication.md) for
the one-time application credentials and redirect URL configuration.

With Discord configured, session ownership controls GM access. Explicit `-dev`
mode enables the local development board.

## Structure

- `main.go`: application entrypoint and route wiring.
- `internal/app`: shared application state and data types.
- `internal/handlers`: HTTP endpoint handlers and websocket handlers.
- `static`: browser JavaScript and CSS; `boardGeometry.js` shares drawing coordinate calculations.
- [docs/menus.md](docs/menus.md): shared floating-menu positioning, dismissal, and focus handling.
- `internal/app/storage.go`: atomic JSON library replacement shared by both libraries.
- `templates`: reusable HTML templates served by handlers.
- `docs/movement.md`: movement smoothing, progress calculations, and worked examples.
- [docs/erasers.md](docs/erasers.md): pen stroke deletion versus partial fog erasure,
  with code examples and shared-state behavior.

## UI approach

Prefer HTMX requests and server-rendered HTML templates for UI interactions.
Keep custom JavaScript for interactions that need it, such as dragging, image
framing previews, and the existing WebSocket synchronization.

Hold the right mouse button on the map or a token and drag to pan your view.
Panning scrolls only your browser and leaves token positions unchanged. A right-click
without dragging still opens a token's menu. Toolbar right-click settings remain
available, and the brush outline hides while panning.

Dragging follows your mouse immediately. Position updates are sent and broadcast
about 30 times per second; other boards blend between positions over 60 ms.
Releasing sends the final position immediately, and older position updates are briefly
ignored on your board until the server confirms the drop. Unit refreshes received
during dragging are deferred until release, including HTTP responses already in
flight. Initial loading and subsequent refreshes share one serialized HTMX path;
spawn responses do not append a second copy of a token.

See [How movement smoothing works](docs/movement.md) for the before/after code,
the progress calculation, and how token and ruler animations share
`static/movement.js` while keeping their own drawing and lifecycle code.

Right-click a placed unit to show a small menu beside it. The ↑ and ↓ arrows
increase or decrease its size by one existing category, stopping at Tiny and
Gargantuan. Resizing updates only that placed token in memory and on connected
boards; the saved library unit and newly spawned copies keep their original size.
The menu stays open beside the resized token. `POST /unit/{id}/size` accepts
`direction=up` or `direction=down`.

Remove deletes that placed token from server memory and connected boards,
and closes its menu. Status toggles a grid of colored D&D condition
circles above the token (below it when there is no room above). Hover or focus a
circle to see its name. The grid has six statuses per row, with the remaining
statuses on the last row. Opening the picker leaves names hidden; press Down on
Status to focus the first circle, then use Left/Right, Home, and End to move
between circles. Selecting a circle applies that status to the placed token,
adds a border in its color, and shows the name, size, and status on separate
lines on hover. Blinded uses deep blue, Deafened coral, Incapacitated burgundy,
Invisible pale cyan, and Petrified sandstone to distinguish the former gray tones. Choosing another status replaces it; choosing the selected status again
clears it. Status changes synchronize across connected boards and page reloads,
and reset with placed tokens on server restart. Escape closes the status row first and returns focus to
Status; pressing Escape again closes the action menu. Outside
clicks, Escape, dragging, scrolling, resizing the window, and board replacement
close the menu. Removal keeps the saved library entry, shared artwork, and other
copies available. `DELETE /unit/{id}` removes a placed token; repeating it succeeds.

The base menu shows one **Spawn unit** button. It opens a library with independently
collapsible PC and NPC lists. Each entry shows a name and image; selecting it
spawns a copy on the tabletop. Knight (PC) and Goblin (NPC) are included initially.
Each list separates **Your units** from a collapsed **Defaults** dropdown. The
map library similarly separates **Your maps** and **Defaults**. Owned copies
appear with your content; searching opens any matching dropdowns.

**Search units** and **Search maps** filter their libraries as you type. Searches
match names without regard to case or accents; multiple words can appear in any
order. Small libraries filter immediately in the browser and keep search text
when copying or deleting refreshes a menu. Unit catalogs with more than 240 entries
search on the server after a 200 ms typing pause, showing up to 60 entries in each
owned/default group. **Previous** and **Next** browse the remaining results;
clearing the search returns to the first page. Counts include all matches.

The spawn button shows a loading state while opening its menu. The lobby shows
progress immediately when creating, joining, or opening a session. Plugin discovery
reuses decoded content in memory while checking for changed or removed files,
so repeated session loads avoid decoding the entire monster and map collection.

Loading a map or reopening a board centers the map in each viewer's screen.
Panning stays local to that viewer, and updates or saves of the same background
preserve their view. Maps, tokens, and drawings keep their shared board coordinates.

Open `http://localhost:18080/?gm=true` to show the **GM** button below the utilities.
GM fog controls appear beneath it. Click **GM** to hide or show its map and fog
controls, including an open map editor. This affects only your browser and
preserves the selected tool and any form input. Without `gm=true`, GM controls are omitted
from the page. This URL flag selects GM mode only when Discord sign-in is not configured.
Authenticated sessions assign the GM role to their creator.

The GM controls include a square folded-map button with a **Map Options** tooltip.
Click it to open **Load Map**, **Save Map**, **Create New Map**, and **Clear Map**,
with Clear Map spaced apart at the bottom. Clear Map removes
all placed tokens, pen drawings, fog, rulers, and active pings for every connected
user, including the displayed map background. Saved maps, the unit library, and
artwork remain available. Load Map opens the saved map library. Its **Delete**
buttons permanently remove saved maps, their saved board content, and artwork,
after confirmation. Deleting the active map removes its background for everyone;
placed tokens and drawings remain. Map deletion requires GM mode and uses
`DELETE /map-library/{id}?gm=true`. Selecting a map
replaces the board for all connected users with its saved image and contents.
Maps created before saving board content load their image onto an empty board. Click the icon
again, click outside, or press Escape
to close the menu.

Clearing advances the board's generation number. Drawing and ping messages carry
that number so delayed updates from the previous board are discarded instead of
restoring cleared content. Connected clients cancel active gestures and pending
drawing batches when they receive `boardCleared`.

**Create New Map** opens a form with a name, image upload, and a grid preview.
Drag the preview image to align it with the grid, and use the Zoom slider to resize
it while preserving its proportions, like the unit image editor. Arrow keys move
the image; Shift moves it farther. Fit image resets the size and position, and
Center image centers it on the board. The map fits within the current 64 × 64 board
without preset size categories. **Save and Use Map** stores the
image and dimensions, then displays it beneath the grid, drawings, and tokens on
every board. Existing tokens and drawings remain in place. Cancel discards the form.
PNG, JPEG, and GIF images up to 10 MB and 4096 × 4096 pixels are supported.
Saved maps live in `data/maps.json` and `data/maps/`; the currently displayed map
is kept in memory and resets when the server restarts.

**Save Map** updates the currently displayed map's saved entry with all placed
tokens (including positions, sizes, statuses, and image framing), pen strokes,
ordered fog paint and erasure, and visible rulers. It retains the map artwork and
grid alignment. Repeated saves replace the previous board content for that map;
Clear Map leaves the saved entry intact. Temporary pings and personal UI settings
are excluded. Create a map before saving. The board content is stored alongside
its map definition in `data/maps.json`, with the original image in `data/maps/`
and token artwork in `data/images/` or `static/images/`. Keep these together when
backing up. Use Load Map to restore this content, including after a server restart.

Use **Hide UI** in the top-right corner to hide Spawn unit, the utilities, and GM
controls. The button stays visible as **Show UI** to bring them back. This affects
only your browser and leaves your selected tool active.

GM users can select **Fog of war** (the fog icon) and drag to paint an 80 px brush
over the grid and tokens. **Erase fog** removes just the brushed area with a 40 px
eraser, leaving the rest of the fog in place. Pen drawings have their own eraser.
Right-click either fog button to adjust brush and eraser thickness.
The pen, fog brush, and fog eraser show an outline at the cursor matching their
selected thickness. It scales with the grid and stays visible over fog and tokens.
Fog painting and partial erasure sync to every board and remain after refresh or
GM disconnection. Brush sizes scale with the grid. Fog lives in server memory and
resets on server restart. Fog is translucent (35% opacity) for GMs and fully opaque
for players. Only the session GM may edit fog when signed in; local development mode uses
`gm=true`.

**Ruler**, **Pen**, **Eraser**, and **Ping** appear as separate square icon buttons
in a vertical toolbar below Spawn unit, with Eraser immediately below Pen.
Hover a button to see its name. Select **Ping**, then click the board or a token
to show a blinking, translucent red glow with soft edges at that board position
for every connected party. The glow disappears after 2.7 seconds. Ping stays selected for repeated clicks;
click Ping again, choose another utility, or press Escape to return to moving
tokens. Ping coordinates stay aligned when boards have different scroll positions.
Pings are temporary and do not alter tokens or persist on reload. Reduced-motion
users see a steady glow for the same duration.

Select **Ruler**, press on the board or a token, and drag while holding the mouse
button to measure a straight line. The label at the midpoint shows feet, using
5 feet per rendered grid square, including fractional and diagonal distances.
Release to keep the measurement visible; start another drag to replace it.
Click Ruler again, choose another tool, or press Escape to clear it. Measurements update live on every connected board, including users who join
after a line is drawn. Updates are sent about 30 times per second, and receiving
boards animate between positions over 50 ms to smooth movement. Each user has their own ruler; clearing yours leaves
other rulers visible. Disconnecting removes your ruler. Coordinates use grid
squares so all boards show the same distance and position. Rulers do not move tokens.

Select **Pen**, then hold the mouse button and drag to draw a freehand stroke.
Right-click the Pen icon to choose a color and thickness (1–12 px). Settings apply
to new strokes, and everyone sees the same color and thickness. The default is blue
with a 3 px thickness.
Strokes appear beneath the grid lines and tokens, and sync live to every connected
board. Grid-square coordinates keep drawings aligned when scrolling or changing
the cell size. Release, press Escape, or switch tools to finish a stroke. Drawing
over a token does not move it. Strokes remain after the author disconnects and
are sent to users joining or refreshing the board. Drawings live in server memory
and reset on server restart.

Select **Eraser**, then click or drag over a drawing to remove its entire pen
stroke. The eraser can remove anyone's strokes and syncs removals to all boards.
It catches strokes crossed between mouse samples during fast drags, and has an
8-pixel brush radius plus half the stroke width. Removed strokes stay removed
after refresh, including when another user was still drawing them. Tokens,
grid lines, rulers, and pings are unaffected. Click Eraser again, select another
tool, or press Escape to return to moving tokens.

**Create new unit** opens a form for a name, unit type (`unitType=pc` or
`unitType=npc`), D&D size, and optional artwork. Saving returns to the library without
spawning a token. Cancel or Back returns without saving. PNG, JPEG, and GIF uploads
are supported up to 5 MB and 4096 × 4096 pixels. Uploads are decoded and saved as
static PNGs (the first frame for GIFs); omitted artwork uses the type's default.

The form includes a circular **Fit image to token** preview for both uploaded and
default artwork. Use the zoom slider (25%–400%), drag the image, or use arrow keys
on the preview to position it (Shift+arrow moves faster). **Fit image** resets
zoom and position; **Center image** resets position while keeping the zoom.
Framing is saved separately from the original artwork and used in library
thumbnails and spawned tokens, independently of the D&D size. Older entries
without framing stay centered at 100%.

Menus and forms use HTMX and server-rendered `templates/spawn-menu.html`.
Native HTML `details` elements handle collapsing the lists. `/spawn-menu?open=true`
opens the library, `/spawn-menu` closes it, and `/unit-library/new` opens the form.
`POST /unit-library` saves a definition; `POST /unit?unitID=...` spawns a copy.
Spawning requires POST; GET, HEAD, and other methods return 405 without changing
the board.
Owned library entries have a **Delete** button with a permanent-deletion confirmation.
Plugin defaults are read-only and offer **Create copy**.
`DELETE /unit-library/{id}` removes its definition across restarts, including
owned copies. Existing placed copies and saved boards keep their artwork.
Outside clicks, Escape, and successful spawning close the library. The creation
form stays open on outside clicks so choosing an image or interacting with the
board does not accidentally discard input; use Cancel, Back, or the main toggle
to leave it. Form errors preserve the name, type, size, and image framing; an image must be reselected.

## Server storage and unit images

[MongoDB storage](docs/mongodb.md) stores map and unit definitions as documents,
embeds each saved board in its map, and reads status names and colors from a
status additions document. Plugin defaults are read directly from retained
version folders. Compose enables MongoDB by default; for a host process, set
`TABLETOP_MONGODB_URI` to enable it. New tabletops initialize empty owned libraries.
Artwork stays in the data directory. The following file-storage details apply
when MongoDB is not configured.

Created unit definitions are stored in `data/units.json`, with uploaded artwork
in `data/images/`. The directory is created automatically and excluded from Git.
Keep the entire `data` directory to preserve the library across server restarts
or to back it up. Run one server process per data directory.

WebSockets accept same-host browser origins; clients without an Origin header
remain supported. Each socket write has a two-second deadline, and stalled
broadcast clients are disconnected so they cannot hold shared locks indefinitely.

The server validates names, types, upload sizes, image formats, and dimensions.
Library updates use an atomic file replacement; failed saves do not publish an
entry. An unreadable or invalid library stops startup rather than replacing it.

Built-in image files live in `static/images`. The `unitImages` registry in
`internal/handlers/images.go` maps their image IDs to files. Both built-in and
uploaded artwork is served through `/images/{id}`; unknown image IDs return 404.
Clients never choose a server filesystem path.

Each placed unit has a distinct `ID`, a name, a `UnitType`, a `Size`, and an `ImageID`.
Multiple copies can share artwork. Movement preserves their name, type, size, image, and framing.
Library entries and uploads persist, while placed tokens and their positions are
still held in memory and reset when the server restarts.

## Unit sizes

The creation form defaults to Medium and offers all six D&D size categories.
Saved entries display their size in the library. With each grid square representing
5 feet, tokens use these square footprints:

| Size | Space | Grid footprint |
| --- | --- | --- |
| Tiny | 2½ × 2½ ft | ½ × ½ square |
| Small | 5 × 5 ft | 1 × 1 square |
| Medium | 5 × 5 ft | 1 × 1 square |
| Large | 10 × 10 ft | 2 × 2 squares |
| Huge | 15 × 15 ft | 3 × 3 squares |
| Gargantuan | 20 × 20 ft | 4 × 4 squares |

These follow the [D&D Creature Size and Space table](https://www.dndbeyond.com/sources/dnd/br-2024/playing-the-game#CreatureSize).
The built-in Knight is Medium and Goblin is Small. Existing saved definitions
without a size load as Medium, preserving their previous one-square footprint.
Sizes persist with the library and are retained when tokens move or reload.

## Todo

- [ ] Explore dedicated Docker/Go workers for sessions and a shared Redis plugin
  metadata cache; see [proposed session workers](docs/session-workers.md).
- [x] Add an option to delete saved units from the unit library.
- [x] Add an option to delete saved maps from the map library.
- [ ] Optimize large saved boards by separating strokes and ordered fog into
  referenced documents or bounded batches if board limits increase; see
  [MongoDB storage](docs/mongodb.md).

Choose one installed plugin version when creating a tabletop. Its defaults stay
read-only; tabletop additions and copies are persisted separately. See
[plugin deployment](docs/plugins.md) and the [plugin spec](spec/plugin-system-spec.md).
