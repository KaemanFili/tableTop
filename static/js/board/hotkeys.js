(() => {
    const hotkeys = JSON.parse(document.getElementById('player-hotkeys')?.textContent || 'null') || {};
    const actions = new Map();
    for (const tool of ['ruler', 'pen', 'eraser', 'ping', 'fog', 'fog-eraser']) {
        const key = hotkeys[tool];
        const button = document.getElementById(`utility-${tool}`);
        if (!button || !/^(ctrl\+)?(alt\+)?(shift\+)?(meta\+)?[a-z0-9]$/.test(key || '')) continue;
        actions.set(key, button);
        const names = {ctrl: 'Control', alt: 'Alt', shift: 'Shift', meta: 'Meta'};
        const parts = key.split('+');
        button.setAttribute('aria-keyshortcuts', parts.map(part => names[part] || part.toUpperCase()).join('+'));
        const labels = {ctrl: 'Ctrl', alt: 'Alt/Option', shift: 'Shift', meta: 'Meta (Win/Cmd/Super)'};
        button.title += ` (Hotkey: ${parts.map(part => labels[part] || part.toUpperCase()).join('+')})`;
    }
    document.addEventListener('keydown', event => {
        if (event.defaultPrevented || event.repeat || event.isComposing || event.keyCode === 229 ||
            event.getModifierState('AltGraph') ||
            document.body.dataset.uiHidden === 'true') return;
        // composedPath also protects editable controls inside shadow roots.
        const editable = node => node instanceof Element &&
            (node.isContentEditable || node.closest('input, textarea, select, [role="textbox"], [role="combobox"]'));
        if (event.composedPath().some(editable) || editable(document.activeElement)) return;
        const button = actions.get(utilityHotkeys.fromEvent(event));
        if (!button || button.disabled) return;
        event.preventDefault();
        boardInput.cancel();
        // Reuse click behavior for toggling, tool cleanup, ARIA, and mobile trays.
        // Missing GM buttons are never bound, regardless of saved preferences.
        button.click();
    });
})();
