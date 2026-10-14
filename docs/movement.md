# How movement smoothing works

## Mouse, touch, and stylus input

`static/boardInput.js` routes Pointer Events to token dragging, pen/eraser,
fog/eraser, and ruler handlers. One pointer owns a gesture; additional fingers
cannot move the active token or add drawing points. Pointer capture keeps native
gestures connected to the board when a finger or stylus leaves the initial target.
Cancellation, lost capture, or leaving the page finishes the current gesture.

On mobile, drag a token directly to move it. Select a drawing tool before dragging
on the board. With no tool selected, drag empty board space to pan. Pinch with two
fingers to zoom the board; a second finger finishes an active drawing or token drag
before starting the zoom gesture. The board handles these gestures itself so they
do not zoom the surrounding menus. Menus accept touch presses for outside dismissal.

Use the mouse wheel over the board, pinch, or the **− / percentage / +** controls
to zoom between 25% and 300%. Click the percentage to reset to 100%.
`static/boardZoom.js` transforms only `#board-world` and keeps the board point under the
zoom gesture in place. Fixed UI elements remain outside that scaled container.
Token dragging and pings convert screen coordinates back to shared board pixels;
drawing, fog, and ruler coordinates remain in grid squares. Zoom and panning are
local to each viewer.

Pointer coordinates are normalized against the entire rendered grid, which avoids
rounding differences between individual cells at fractional zoom levels. Ruler
rendering uses unscaled board pixels because its SVG and label already live inside
the zoomed world. Screen-space cell sizes are reserved for cursor previews and
hit testing, preventing zoom from being applied twice to ruler endpoints.

The grid retains its original layout dimensions during zoom. `#board-extent` is
an inexpensive spacer that updates scroll bounds, while a CSS transform scales
and positions the board. Native page-pinch events are cancelled for board gestures,
and wheel events are ignored while board touches are active, so only one input
handler controls zoom. This follows Apple's guidance on
[preventing default Safari gesture behavior](https://developer.apple.com/library/archive/documentation/AppleApplications/Reference/SafariWebContent/HandlingEvents/HandlingEvents.html).

Pinch zoom ignores the first 10 pixels of span change and uses reduced
sensitivity. Fingers initially less than 80 pixels apart must spread before zoom
starts, avoiding large changes from tiny movements. Updates combine both fingers'
latest positions once per animation frame and use a fixed board anchor to reduce
scroll drift. The initial midpoint stays anchored throughout a pinch; thumb drift
never pans the board. After either thumb lifts, the remaining thumb stays inactive
until both have lifted, preventing accidental movement at release.

One-finger panning ignores initial movements smaller than 6 pixels and updates
once per animation frame. Movement uses the actual current scroll position,
so reversing direction at an edge responds immediately. Start a new one-finger
gesture to pan after completing a pinch.
Cancelled gestures stop queued movement. The board disables browser scroll
anchoring and overscroll so DOM updates and viewport bouncing do not compete
with its camera.

The camera scrolls a fixed `#board-viewport` container instead of the document.
Map centering, touch panning, right-button panning, and zoom all use that same
container. The document remains stationary, keeping mobile browser chrome and
fixed menus separate from board movement. Viewport padding follows the actual
container size. Mobile controls use at least 44-pixel touch targets (48 pixels for
utility buttons), and form fields use 16-pixel text to reduce focus zoom on iPhone.

On phones, the Tools tray starts collapsed and closes after selecting a tool.
Menus use scrollable sheets capped at 60% of the viewport height, with a Close
button. Hide UI dismisses panels and hides the toolbar, zoom, dice, token menus,
notifications, and brush preview; Show UI remains available. It deselects drawing
tools so a finger can pan the board while controls are hidden.

The grid wrapper has explicit grid dimensions so SVG drawing and canvas layers
share the same bounds as pointer calculations. Selecting a brush clears its old
cursor preview until a new pointer event arrives.

Mouse and touch token movement combine incoming pointer events once per animation frame.
The overlay geometry is cached during a drag and refreshed when the camera moves
or resizes. A compositor transform gives immediate local feedback; release commits
it to shared board coordinates and clears the transform. Network updates remain
throttled independently, so drawing the dragged token does not wait for an ack.

## Receiving updates

Remote tokens use CSS transitions on `left` and `top`, with a 100 ms linear
transition. JavaScript assigns each received destination once. Unchanged updates
are ignored, and the inline coordinates always contain the server's destination.
Local dragging removes the remote transition and freezes the currently displayed
position before taking over, so grabbing a moving token does not jump it ahead.

Remote ruler lines and endpoint circles use 100 ms CSS transform transitions.
Distance labels transition to the new midpoint. Local rulers update immediately.
Pen paths append their exact received points, then reveal the added length using
a 100 ms CSS stroke-dashoffset transition. Completed strokes received on joining
or loading a map appear immediately. Fog updates share one canvas repaint per
animation frame instead of repainting the full canvas for every received message.

CSS owns these receiving animations. JavaScript only tracks a moving token when
its action menu is open, to keep that menu anchored to the displayed position.
The previous interpolation helper and per-token/per-ruler JavaScript animation
loops are no longer loaded.

## Multiplayer delivery

Each connection has its own writer. Position snapshots have a one-message queue
that replaces an older unsent snapshot with the newest state. Drawing and board
messages use a bounded ordered queue; a stalled client is disconnected rather
than blocking all players or silently losing drawing batches. Initial board
messages are captured in order under the board lock, then written outside it.

Cached session requests validate plugin metadata outside the global sessions
lock. Requested images and saved boards validate their own content, while board
entry and full plugin validation still check the complete version.
