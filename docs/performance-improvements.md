# Tabletop performance changes

This work addresses three costs that became noticeable with a large content
library and multiple connected players: loading artwork, delivering shared
updates, and rendering movement. It also removes frontend animation code that
was doing work the browser can handle through CSS.

## Loading the board and artwork

The board previously loaded 4,096 individual grid cells through an additional
HTMX request. It now renders the same 64-by-64 grid as one tiled SVG background.
This removes thousands of DOM elements, their layout work, and the grid request.

Scripts now use `defer`, allowing downloads to overlap while preserving execution
order. Static assets can be cached for an hour, with versioned URLs changed when
scripts or styles change. Dynamic pages and session responses remain uncached.

Plugin artwork, uploaded images, and default token images use private browser
caching for an hour. Plugin images also have content ETags for revalidation.
Token and map images request asynchronous decoding so image decoding can be
scheduled separately from interaction rendering. Missing image responses are
not cached.

Each plugin image request previously checked every file in the plugin and
decoded and re-encoded its artwork. For the 5e library, that meant checking
thousands of unrelated files for a single portrait. Image requests now validate
the plugin metadata and the requested image's content hash. PNGs already
validated during plugin loading are served directly; JPEG and GIF images are
still converted to PNG. Full plugin validation remains available and runs when
entering the board and accessing relevant content listings.

Cached session requests also previously repeated full validation while holding
the global sessions lock. They now check metadata outside that lock. A player's
image requests no longer serialize all other sessions behind full artwork scans.

The local portrait benchmark improved from **20.19 ms to 1.82 ms per request**,
approximately **11 times faster**. This measures portrait processing on the
development host; it is not a measurement of complete browser loading time.
The changes add no server-side cache of image bytes.

## Keeping multiplayer updates responsive

The server previously wrote to connected clients sequentially while holding
the shared board lock. A stalled socket could delay other players' drawings,
rulers, dice messages, and board actions until its write timed out.

Each connection now has its own writer. Board producers enqueue messages rather
than waiting for network writes. Drawing and board messages retain their order
in a bounded queue. A client that falls behind is disconnected instead of
silently losing drawing batches or holding up the other players.

Position snapshots use a one-message queue: a newer unsent snapshot replaces
the older one. This prevents a slow client from accumulating a backlog of stale
positions. Initial board messages are captured in order under the board lock
and sent outside it.

## Smooth receiving animations with less JavaScript

Receiving clients use **100 ms CSS transitions** toward incoming destinations.
JavaScript applies updates once per message rather than calculating and writing
intermediate positions every display frame:

- Tokens transition their `left` and `top` coordinates. The inline destination
  remains the exact server position; unchanged updates are ignored.
- Ruler lines and endpoint circles transition their transforms. Their labels
  transition toward the new midpoint.
- Pen paths retain every received point and use a stroke-dashoffset transition
  to reveal newly received segments. Completed strokes loaded from saved state
  appear immediately.

Local dragging, drawing, and ruler input remain immediate. Grabbing a remotely
moving token freezes its currently displayed position before local dragging
takes over. Local token movement combines pointer events once per animation
frame and uses a temporary transform, committing shared coordinates on release.
Fog updates share a canvas repaint per frame instead of repainting the entire
canvas for every incoming message.

The unused interpolation helper and token/ruler JavaScript animation loops were
removed. The receiving token and ruler modules are each 33 lines smaller than
their checked-in versions. Menu-following animation runs only for an open token
menu, rather than rendering every moving token through JavaScript.

## Dice and frontend cleanup

Dice controls previously stayed disabled until the visual animation finished.
They now unlock when the server's authoritative result arrives. Numerical
results and notifications do not wait for 3D playback.

Dice shadows are disabled. Animations are skipped above 20 physical dice, and
the animation queue has at most three pending rolls. Percentile dice count as
two physical dice. These limits affect animation only; every rolled value and
total is still displayed.

There is an unresolved latency tradeoff: moving renderer initialization from
menu opening to the first roll reduced menu-opening work but introduced a
first-animation startup delay. That change should not be described as a proven
improvement to roll latency. Renderer preloading and animation queue latency
need a separate follow-up.

Popup dismissal and mobile media queries are shared through `menuUI`. Duplicate
board-clear listeners were removed, and library initialization uses HTMX's load
event to initialize incoming content instead of rescanning the document after
every swap.

## Verification and deployment

Regression checks cover real two-browser token and drawing streams, ruler
endpoints, CSS pen reveal, local dragging, zoom geometry, popup interactions,
artwork caching, and content integrity. Server tests deliberately stall one
client and block one session's validation to verify other players and sessions
can continue receiving updates.

Container checks found ample memory headroom and no ongoing app throttling in
the sampled interval. These samples do not establish behavior under peak load,
and no overall FPS or live multiplayer latency improvement has been measured.

The latest cleanup and multiplayer changes are in the workspace and have not
been deployed to the running service. An earlier Docker image was built before
this final pass and needs rebuilding. Restarting the service disconnects players
and resets unsaved board state, so save the current map before deployment.

See [movement and delivery details](movement.md), [plugin loading](plugins.md),
and [menu and dice behavior](menus.md) for the implementation notes.
