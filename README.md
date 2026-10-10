# tableTop
ttrpg table top tool meant to run in browser

## Development

Requires Go 1.27.1 or newer. Run the app from the project root with `go run .`,
then open `http://localhost:18080/` (or `http://127.0.0.1:18080/`). To use the
executable, rebuild with `go build -o main .`, then run `./main` from the project
root. Restart a running server after rebuilding; replacing the executable does
not update the running process.

Use `go fix ./...` to apply Go's automated source modernizations, then
`go fmt ./...` to format the code. Validate changes with `go test -race ./...`
and `go vet ./...`.

## Structure

- `main.go`: application entrypoint and route wiring.
- `internal/app`: shared application state and data types.
- `internal/handlers`: HTTP endpoint handlers and websocket handlers.
- `static`: browser JavaScript and CSS.
- `templates`: reusable HTML templates served by handlers.
- `docs/movement.md`: movement smoothing, progress calculations, and worked examples.

## UI approach

Prefer HTMX requests and server-rendered HTML templates for UI interactions.
Keep custom JavaScript for interactions that need it, such as dragging, image
framing previews, and the existing WebSocket synchronization.

Dragging follows your mouse immediately. Position updates are sent and broadcast
about 30 times per second; other boards blend between positions over 60 ms.
Releasing sends the final position immediately, and older position updates are briefly
ignored on your board until the server confirms the drop.

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
Outside clicks, Escape, and successful spawning close the library. The creation
form stays open on outside clicks so choosing an image or interacting with the
board does not accidentally discard input; use Cancel, Back, or the main toggle
to leave it. Form errors preserve the name, type, size, and image framing; an image must be reselected.

## Server storage and unit images

Created unit definitions are stored in `data/units.json`, with uploaded artwork
in `data/images/`. The directory is created automatically and excluded from Git.
Keep the entire `data` directory to preserve the library across server restarts
or to back it up. Run one server process per data directory.

The server validates names, types, upload sizes, image formats, and dimensions.
Catalog updates use an atomic file replacement; failed saves do not publish an
entry. An unreadable or invalid catalog stops startup rather than replacing it.

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
