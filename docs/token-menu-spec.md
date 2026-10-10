# Token right-click menu

Scope: the menu UI only. Status, resizing, and removal behavior will be implemented later.

Implemented follow-up: Remove now sends `DELETE /unit/{id}`, deletes that placed
token from server memory, and removes it from connected boards. Its menu and any
active drag are cleared. Older HTML responses cannot restore a deleted token.
Saved library entries and artwork remain available. Status and Resize are still
placeholders; the UI-only requirements below describe the original menu step.

## Appearance

Right-clicking a unit shows three small buttons in a vertical stack immediately to the right of its circular icon:

```text
              [Status]
    (unit)    [Resize]
              [Remove]
```

- Order: Status, Resize, Remove.
- Use compact text buttons so each action is clear without learning icons.
- Leave a 6px gap between the unit and the menu. Center the stack vertically against the unit.
- Use the existing dark menu styling, with rounded corners and a visible hover/focus state. Each button is approximately 28px tall.
- Keep the menu above nearby tokens. It must not be clipped by the unit's circular artwork.
- Keep all three buttons visible near the viewport edge: shift the menu inward when necessary.

## Interaction

- Right-click a unit to open its menu and suppress the browser context menu for that unit.
- Only one unit menu is open at a time. Right-clicking another unit moves the menu to that unit.
- Clicking outside the menu or pressing Escape closes it.
- Starting a left-button drag closes it. Existing dragging continues to work as it does today.
- Scrolling or resizing the browser closes it so it cannot become detached from its unit.
- Clicking Status, Resize, or Remove has no action yet. These buttons are UI placeholders and do not send requests or change the board.
- A right-click anywhere else retains the normal browser context menu.

## Implementation

Add one shared menu element beside `#unitOverlay` in `index.html`, hidden initially. Use three native buttons with their visible names as accessible labels.

Add a small script, `static/tokenMenu.js`, with a delegated `contextmenu` listener that finds the containing `.unit`. Position the shared menu using that unit's `getBoundingClientRect()` and fixed positioning. Remember the unit ID while the menu is open. Clear it when closing.

Add the menu styles to `static/style.css`. The menu needs pointer events enabled, and interacting with its buttons must not start a token drag. Close it if a board HTML refresh replaces or removes its unit.

## Acceptance checks

- Right-clicking either the unit or its artwork opens the three buttons on its right.
- Opening another unit's menu closes the previous one.
- Outside click, Escape, left drag, scrolling, browser resizing, and board replacement close the menu.
- The menu remains readable near the viewport edges and above overlapping units.
- Buttons support keyboard focus and have visible hover/focus feedback.
- Clicking any of the three buttons leaves the unit unchanged and sends no request.
- Existing spawning, dragging, and the browser context menu outside units still work.
