# How the pen and fog erasers work

The pen eraser deletes a drawing object when you touch it. The fog eraser clears
pixels along your brush path, leaving the rest of the fog in place. They share
mouse input and WebSocket delivery, but use different rendering and storage.

| Detail | Pen eraser | Fog eraser |
| --- | --- | --- |
| Drawing surface | SVG paths beneath the grid | Canvas above the grid and tokens |
| What gets erased | Every touched stroke, in full | Only pixels covered by the eraser |
| How a hit is determined | Distance between the eraser path and stroke segments | Canvas applies the eraser path to existing pixels |
| Message sent | `penErase` with stroke IDs | `fogMark` with `mode: "erase"`, points, and width |
| Server change | Delete strokes and remember their removed IDs | Append an erase mark to the ordered fog history |
| Restoring a refreshed board | Send remaining strokes | Replay paint and erase marks in order |
| Who can erase | Any connected user | GM clients using the temporary `gm=true` flag |

## Pen: find touched strokes and delete them

In [static/pen.js](../static/pen.js), `appendPoints()` creates an SVG path for each
stroke. The client keeps that path, its width, and its points in the `strokes` map.
This means a stroke remains an identifiable object after it is drawn.

`eraseAt()` converts the stored grid coordinates to screen pixels. For each
stroke, it checks the distance between two line segments:

- The eraser's movement from its previous mouse position to its current position.
- A segment connecting two adjacent points of the stroke.

Checking the whole movement segment catches drawings crossed during a fast drag,
even if no mouse event lands directly on the drawing. On a stationary click,
the eraser segment has zero length and the same calculation still works.

The contact radius includes the eraser's 8 px radius and half the drawing's width:

```js
const radius = 8 + (stroke.width / 40) * Math.max(cell.width, cell.height) / 2;

if (segmentDistanceSquared(from, to, a, b) <= radius * radius) {
    ids.push(id);
    break;
}
```

`segmentDistanceSquared()` returns the minimum squared distance between the
segments. Comparing it with `radius * radius` avoids taking a square root.
Intersecting segments have a distance of zero. At the standard 40 px grid size,
a 3 px pen stroke produces a contact radius of `8 + 3 / 2 = 9.5 px`.

Once any segment qualifies, the client adds the **whole stroke's ID** to the
removal list. It does not cut the path into smaller pieces. Touching the middle
of a long line therefore removes the entire line.

The client sends up to 16 IDs per message and removes them locally immediately:

```js
newUnitUpdateSocket.send(JSON.stringify({type: 'penErase', ids: batch}));
batch.forEach(removeStroke);
```

`removeStroke()` removes the SVG element, deletes the client map entry, and
records the ID in `erasedIDs`. If someone is still drawing that stroke, its local
drawing also stops when the removal is received.

On the server, `erasePen()` in [internal/handlers/pen.go](../internal/handlers/pen.go)
deletes each known stroke and records its ID:

```go
delete(h.strokes, id)
h.erasedStrokes[id] = true
```

The server broadcasts a `penErased` message containing the removed IDs. Both
server and client remember those IDs so a delayed drawing batch cannot recreate
an erased stroke. Repeated deletion is supported; unknown IDs are ignored.

## Fog: draw an eraser path into the canvas

In [static/fog.js](../static/fog.js), `paint()` draws both fog and erasure into a
canvas. The operation changes according to the mark's mode:

```js
target.globalCompositeOperation = mark.mode === 'erase'
    ? 'destination-out'
    : 'source-over';

target.lineWidth = mark.width * canvas.width / 64;
target.lineCap = 'round';
target.lineJoin = 'round';
// Build the path from mark.points, then apply it.
target.stroke();
target.globalCompositeOperation = 'source-over';
```

`source-over` adds fog to the canvas. `destination-out` removes existing pixels
where the brush covers them, making those areas transparent. The eraser does not
paint a background-colored line; transparency reveals the grid and tokens below
the fog. Resetting the operation afterward makes ordinary rendering use
`source-over` again.

The browser handles the pixel coverage, rounded ends, and smooth edges. There
is no search for touched stroke IDs. Joining consecutive mouse points makes the
eraser sweep continuously between samples, including during fast drags.

For example, draw fog from grid point `(5, 5)` to `(10, 5)` with an 80 px brush.
Erase once at `(7.5, 5)` with a 40 px brush. This makes a round hole near the
middle; the surrounding fog remains. Painting over that hole later covers it
again.

The erase action is sent as a brush mark. This example uses a client-local ID:

```json
{
  "type": "fogMark",
  "id": "7",
  "mode": "erase",
  "width": 1,
  "points": [{"x": 7.5, "y": 5}]
}
```

Fog widths are transmitted in **grid squares**: `40 px / 40 = 1 square`.
The canvas covers 64 squares and currently has a 2560 px backing image, so this
mark erases a 40 px diameter at the standard grid size. Changing the displayed
grid size scales the fog and its holes together. Pen stroke widths, by contrast,
are transmitted as pixel values at the standard 40 px grid size and converted
when rendered or checked for contact.

Fog batches contain at most 16 points. During a drag, batches are sent on a
33 ms timer or when they fill, and releasing flushes any remaining points. A new
batch includes the previous point so the path continues without a gap.

## Shared fog depends on operation order

`updateFog()` in [internal/handlers/fog.go](../internal/handlers/fog.go) validates
the mark, attaches the server-assigned owner and ID prefix, then stores and
broadcasts it under the board utilities lock:

```go
h.fogMarks = append(h.fogMarks, mark)
message, _ := json.Marshal(mark)
h.broadcastUnitsLocked(message)
```

The lock ensures connected clients receive accepted fog operations in the same
order. Order matters: **paint, then erase** leaves a hole; **erase, then paint**
can cover that hole. Unlike pen deletion, fog erasure intentionally allows later
painting in the same place.

The local browser draws immediately and keeps unconfirmed marks in `pending`.
A separate offscreen canvas, `confirmed`, holds operations received from the
server. When a broadcast arrives, the client applies it to that canvas, removes
the matching pending mark, and displays the confirmed canvas plus any remaining
pending marks. This keeps local input responsive while reconciling edits from
other GMs with the server's ordering.

For a new or refreshed client, `registerBoardUtilitiesClient()` in
[internal/handlers/websocket.go](../internal/handlers/websocket.go) sends
`fogReset` followed by the stored marks in order, when fog history exists.
Replaying them rebuilds both the fog and its partially erased areas. Pen
restoration instead sends only the strokes still present in `h.strokes`.

Both systems keep their state in server memory, survive the author's
disconnection, and reset when the server restarts. Fog stores its operation
history, which grows with painting and erasing; pen deletion removes the stroke
data while retaining its removed ID.

GM and player fog opacity is a CSS setting on the same canvas: 35% for GMs and
100% for players. It changes how visible the remaining fog is, not which pixels
have been erased. Fog erasure never modifies the pen's SVG paths, and the pen
eraser never modifies the fog canvas.
