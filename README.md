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

## UI approach

Prefer HTMX requests and server-rendered HTML templates for UI interactions.
Keep custom JavaScript for interactions that need it, such as dragging, image
framing previews, and the existing WebSocket synchronization.

The base menu shows one **Spawn unit** button. It opens a library with independently
collapsible PC and NPC lists. Each entry shows a name and image; selecting it
spawns a copy on the tabletop. Knight (PC) and Goblin (NPC) are included initially.

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
