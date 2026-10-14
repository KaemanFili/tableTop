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
focuses the supplied control. Outside mouse presses, Escape, board scrolling,
layout changes, and tool changes dismiss it. Scrolling inside a panel does not
close it; phone keyboard scrolling and height changes also keep it open. Escape
restores focus to the button that opened it.
Multiple buttons can open the same panel, as the fog brush and eraser do.
A missing panel is supported for controls omitted from non-GM pages.

Use `menuUI.position(panel, anchor, options)` separately for menus with their own
lifecycle. The token menu keeps its nested status keyboard navigation and remote
movement handling, while sharing positioning with the toolbar menus.
`placement: 'above'` centers a panel above its anchor and moves it below when
there is insufficient room. `placement: 'below'` centers a panel below its
anchor, as used by the session user list. The default placement is beside the anchor;
`align: 'center'` centers it vertically. `gap` defaults to eight pixels.
All placements stay within the viewport margins. Pass the same options as the
third argument to `menuUI.popover(panel, buttons, options)` to configure a toolbar
menu, such as the dice roller.

The dice panel sits directly under the document body, above the board and
toolbar layers. On phones, a transparent backdrop catches outside taps and
dismisses the panel after the click, keeping that touch from reaching the board.
Closing, hiding UI, or switching panels removes the backdrop. The `onOpen` and
`onClose` popover hooks keep it in sync. Opening dice cancels active board
gestures, and mobile dice never automatically focuses a field. Counts and
modifiers support both touch steppers and explicit keyboard entry.


Right-click the account name to see a horizontal list of connected session
users. Clicking or keyboard activation also opens the list. Profile photos from
the sign-in provider appear when available, including Discord avatars. Initial
icons remain the fallback when a photo is absent or fails to load. Hovering or
focusing an icon shows its display name. The list updates on joins, disconnects, and reconnects, and multiple tabs
for the same signed-in account share one icon. On phones, opening the list keeps
the account controls visible.

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
The 3D engine loads on the first roll; opening the menu does not download or
initialize it.
Dice inputs unlock as soon as the HTTP response provides the authoritative
result, independently of animation loading or playback. Animation has no shadows,
is skipped for more than 20 physical dice (percentile dice count as two), and has
at most three pending rolls. All numerical results and notifications still appear.

Dice notifications and the dice result panel show the roller's display name.
The server supplies this name from the signed-in account, independently of the
OAuth provider. Development mode uses GM or Player. Private rolls include the
name only in the originating client's result and are not broadcast.

Discord profile photos are read during sign-in. After deploying avatar support,
existing users must sign out and sign back in to populate their photo. Other
sign-in providers can supply the optional `AvatarURL` on the shared user identity.
