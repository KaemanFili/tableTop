# Floating menus

`static/menuUI.js` shares positioning and toolbar-menu behavior. Load it before
scripts that use it. Menu actions remain in their owning scripts.

```js
const menu = menuUI.popover(panel, [button]);
button.addEventListener('click', () => {
    if (panel.hidden) menu.open(button, panel.querySelector('input'));
    else menu.close();
});
```

Opening positions the panel beside its button, updates `aria-expanded`, and
focuses the supplied control. Outside mouse presses, Escape, scrolling, resizing,
and tool changes dismiss it. Escape restores focus to the button that opened it.
Multiple buttons can open the same panel, as the fog brush and eraser do.
A missing panel is supported for controls omitted from non-GM pages.

Use `menuUI.position(panel, anchor, options)` separately for menus with their own
lifecycle. The token menu keeps its nested status keyboard navigation and remote
movement handling, while sharing positioning with the toolbar menus.
`placement: 'above'` centers a panel above its anchor and moves it below when
there is insufficient room. The default placement is beside the anchor;
`align: 'center'` centers it vertically. `gap` defaults to eight pixels.
All placements stay within the viewport margins. Pass the same options as the
third argument to `menuUI.popover(panel, buttons, options)` to configure a toolbar
menu, such as the dice roller.


# Notifications

`static/notifications.js` manages popup dismissal, expiry, and the visible limit.
It cancels an expiry timer when a popup is dismissed or evicted. The markup lives
in `#notification-template` in `index.html`, with title, content, and dismiss
slots identified by `data-notification-title`, `data-notification-content`, and
`data-dismiss-notification`.

```js
const notices = notificationUI.create(container, template);
notices.show('Saved', document.createTextNode('Your changes were saved.'));
```

The optional third argument sets `limit` (default 5) and `duration` in milliseconds
(default 30000). Callers supply DOM content; titles are inserted as plain text.
Dice results use the same rendering function in the roller and its notification.

`static/diceAnimation.js` owns engine loading, fixed-result notation, engine
compatibility fixes, animation queues, and fading. Its `create(layer, status)`
returns `load()` and `play(message)`. `static/dice.js` owns form input, shared
requests, duplicate-event handling, privacy, and result presentation.
