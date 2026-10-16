// Native title tooltips are unreliable on touch. A stationary hold provides the
// same information without turning a held button into an accidental action.
// Load before boardZoom so a second touch cancels the hold before pinch handlers
// consume the event. Gesture objects are only needed once the hold completes.
(() => {
    const tooltip = document.createElement('div');
    tooltip.id = 'touch-hover-tooltip';
    tooltip.className = 'touch-hover-tooltip';
    tooltip.setAttribute('role', 'tooltip');
    tooltip.hidden = true;
    document.body.append(tooltip);
    let press = null;
    let timer = null;
    let hideTimer = null;
    let anchor = null;
    let suppress = null;

    function hide() {
        clearTimeout(timer);
        clearTimeout(hideTimer);
        timer = hideTimer = null;
        if (anchor) {
            anchor.classList.remove('touch-hover');
            const describedBy = (anchor.getAttribute('aria-describedby') || '').split(/\s+/).filter(id => id && id !== tooltip.id);
            if (describedBy.length) anchor.setAttribute('aria-describedby', describedBy.join(' '));
            else anchor.removeAttribute('aria-describedby');
        }
        anchor = null;
        tooltip.hidden = true;
        press = null;
    }
    function show() {
        if (!press?.target.isConnected) { hide(); return; }
        anchor = press.target;
        const text = anchor.getAttribute('title') || anchor.getAttribute('aria-label');
        if (!text) { hide(); return; }
        boardInput.cancel();
        if (typeof boardZoom !== 'undefined') boardZoom.cancel();
        tooltip.textContent = text;
        tooltip.hidden = false;
        anchor.classList.add('touch-hover');
        const ids = new Set((anchor.getAttribute('aria-describedby') || '').split(/\s+/).filter(Boolean));
        ids.add(tooltip.id);
        anchor.setAttribute('aria-describedby', [...ids].join(' '));
        menuUI.position(tooltip, anchor, {placement: 'above'});
        suppress = anchor;
    }
    document.addEventListener('pointerdown', event => {
        hide();
        suppress = null;
        if (event.pointerType !== 'touch' || !event.isPrimary) return;
        if (event.target.closest?.('input, textarea, select, [contenteditable="true"]')) return;
        const target = event.target.closest?.('[title], .status-circle[aria-label], button[aria-label]');
        if (!target || target.closest('input, textarea, select, [contenteditable="true"]') ||
            target.matches('.unit') && document.body.dataset.tool) return;
        press = {id: event.pointerId, target, x: event.clientX, y: event.clientY};
        timer = setTimeout(show, 500);
    }, true);
    document.addEventListener('pointermove', event => {
        if (event.pointerId === press?.id && Math.hypot(event.clientX - press.x, event.clientY - press.y) > 10) hide();
    }, true);
    document.addEventListener('pointerup', event => {
        if (event.pointerId !== press?.id) return;
        clearTimeout(timer);
        timer = null;
        press = null;
        if (anchor) hideTimer = setTimeout(() => { hide(); suppress = null; }, 1800);
    }, true);
    document.addEventListener('pointercancel', hide, true);
    document.addEventListener('contextmenu', event => {
        if (!press || !press.target.contains(event.target)) return;
        clearTimeout(timer);
        show();
        // Preserve app context-menu listeners, but suppress browser callouts.
        event.preventDefault();
    }, true);
    document.addEventListener('click', event => {
        if (!suppress?.contains(event.target)) return;
        event.preventDefault();
        event.stopImmediatePropagation();
        suppress = null;
    }, true);
    for (const type of ['scroll', 'uiDismissed', 'boardCleared', 'boardResync', 'viewZoomChanged']) document.addEventListener(type, hide, true);
    window.addEventListener('resize', hide);
    window.addEventListener('blur', hide);
    document.addEventListener('visibilitychange', () => { if (document.hidden) hide(); });
})();
