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
`static/boardZoom.js` scales only `#board-world` and keeps the board point under the
zoom gesture in place. Fixed UI elements remain outside that scaled container.
Token dragging and pings convert screen coordinates back to shared board pixels;
drawing, fog, and ruler coordinates remain in grid squares. Zoom and panning are
local to each viewer.

Pointer coordinates are normalized against the entire rendered grid, which avoids
rounding differences between individual cells at fractional zoom levels. Ruler
rendering uses unscaled board pixels because its SVG and label already live inside
the zoomed world. Screen-space cell sizes are reserved for cursor previews and
hit testing, preventing zoom from being applied twice to ruler endpoints.

Pinch zoom ignores the first 10 pixels of span change and uses reduced
sensitivity. Fingers initially less than 80 pixels apart must spread before zoom
starts, avoiding large changes from tiny movements. Updates combine both fingers'
latest positions once per animation frame and use a fixed board anchor to reduce
scroll drift. Small midpoint movements also leave the view unchanged.

One-finger panning ignores initial movements smaller than 6 pixels and updates
once per animation frame. Movement uses the actual current scroll position,
so reversing direction at an edge responds immediately. After a pinch, lifting
one finger resumes panning from the remaining finger when no tool is selected.
Cancelled gestures stop queued movement. The board disables browser scroll
anchoring and overscroll so DOM updates and viewport bouncing do not compete
with its camera.

The camera scrolls a fixed `#board-viewport` container instead of the document.
Map centering, touch panning, right-button panning, and zoom all use that same
container. The document remains stationary, keeping mobile browser chrome and
fixed menus separate from board movement. Viewport padding follows the actual
container size. Mobile controls use at least 44-pixel touch targets (48 pixels for
utility buttons), and form fields use 16-pixel text to reduce focus zoom on iPhone.

## Position updates

Token movement and ruler movement use the same linear interpolation formula.
Both call the shared `movementMath` utility in `static/movement.js`. Each keeps
its own animation loop and display behavior:

- `static/updateClients.js`: `animateUnits` blends token positions over 60 ms.
- `static/utilities.js`: `animateRulers` blends remote ruler endpoints over 50 ms.

Both use `requestAnimationFrame` to draw intermediate positions in time with
browser display frames. Each loop stops when its animations finish. The durations
are configurable constants: `unitBlendDuration` and `rulerBlendDuration`.
The shared utility handles progress and coordinate interpolation; token drag
protection, ruler distance labels, network updates, and cleanup stay with their
respective callers. `index.html` loads the utility before both callers.

## Before and after

Previously, the server broadcast token positions every 100 ms, or 10 times per
second:

```go
ticker := time.NewTicker(100 * time.Millisecond)
```

Browsers assigned each received position directly:

```js
el.style.left = unit.Left + "px";
el.style.top = unit.Top + "px";
```

CSS animated toward those positions:

```css
.unit {
  transition: left 0.4s ease, top 0.4s ease;
}
```

The transition took 400 ms, but updates arrived every 100 ms. New destinations
kept restarting the transition before it finished, which could make movement
feel slow or uneven.

The server now broadcasts token positions every 33 ms, about 30 times per second:

```go
ticker := time.NewTicker(33 * time.Millisecond)
```

Token dragging sends updates at about the same rate, with an immediate final
update on release. Ruler dragging also sends updates about every 33 ms; the server
relays them to all connected boards as they arrive. Browsers animate between the
received positions instead of using the old CSS transition.

## Understanding progress

`progress` tells us how far through an animation we are, from `0` to `1`:

```js
const progress = movementMath.progress(now, started, duration);

// Inside static/movement.js:
progress(now, started, duration) {
  if (duration <= 0) return 1;
  return Math.min(1, Math.max(0, (now - started) / duration));
}
```

- `now` is the current animation-frame timestamp in milliseconds.
- `started` is the timestamp recorded with `performance.now()` when the animation
  began. Both timestamps use the same time origin.
- `now - started` is how many milliseconds have passed.
- Dividing by `duration` gives the fraction of the animation that has elapsed.
- `Math.max(0, ...)` prevents a negative fraction.
- `Math.min(1, ...)` stops the fraction from going above `1`.
- A zero or negative duration completes immediately, avoiding division by zero.

For a token's 60 ms animation:

| Time elapsed | Calculation | Progress |
| --- | --- | --- |
| 0 ms | `0 / 60` | `0`: beginning |
| 15 ms | `15 / 60` | `0.25`: quarter done |
| 30 ms | `30 / 60` | `0.5`: halfway |
| 60 ms | `60 / 60` | `1`: finished |
| 90 ms | capped at `1` | `1`: still finished |

Progress is the percentage of the movement completed, expressed as a decimal.
The ruler uses the same calculation with a 50 ms duration, so its halfway point
is 25 ms.

## Turning progress into a position

For each coordinate, the calculation is:

```js
const position = movementMath.interpolate(start, destination, progress);

// Inside static/movement.js:
interpolate(from, to, progress) {
  return from + (to - from) * progress;
}
```

Suppose a token moves from 100 px to 160 px. That is a distance of 60 px. At
halfway:

```js
position = 100 + (160 - 100) * 0.5;
position = 100 + 30;
position = 130;
```

The same formula handles backward movement: moving from 160 px to 100 px at
halfway gives `160 + (100 - 160) * 0.5`, which is also 130 px.

Tokens apply this calculation independently to their horizontal and vertical
positions:

```js
element.style.left = `${movementMath.interpolate(fromLeft, left, progress)}px`;
element.style.top = `${movementMath.interpolate(fromTop, top, progress)}px`;
```

Rulers apply it to the displayed endpoint:

```js
measurement.displayEnd = {
  x: movementMath.interpolate(transition.from.x, measurement.end.x, progress),
  y: movementMath.interpolate(transition.from.y, measurement.end.y, progress),
};
```

Token positions use board pixels. Ruler endpoints use grid-square coordinates,
which are converted to pixels for drawing. The formula works in either unit.
The ruler's midpoint label and distance are calculated from the displayed
endpoint, so they move with the line. Each grid square represents 5 feet.

## When another update arrives

A new destination can arrive before the animation finishes. We start the next
blend from the position currently displayed, which avoids jumping back to an
older position. This adds a small visual delay on receiving screens.

Repeated token position updates with an unchanged destination let the current animation
finish. A ruler with a new starting point appears immediately at that origin;
it does not animate across the board from the previous measurement.

## Keeping your own drag responsive

Your dragged token follows your mouse immediately:

```js
target.style.left = `${e.clientX - offsetX}px`;
target.style.top = `${e.clientY - offsetY}px`;
```

Incoming positions are ignored while that token is being dragged:

```js
if (element.classList.contains('dragging')) return;
```

Releasing sends the final integer position immediately. Your browser ignores
older position updates until it receives that final position, or until the one-second
guard expires. This prevents an older position update from pulling the token backward
just after release while still allowing later server positions to take effect.

Your own ruler also draws immediately. Echoed ruler updates are ignored on the
sending board; interpolation is applied on the receiving boards.
