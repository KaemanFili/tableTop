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
All placements stay within the viewport margins.
