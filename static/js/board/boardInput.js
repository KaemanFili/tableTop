// Route one board gesture at a time for mouse, touch, and pen input.
const boardInput = (() => {
    const listeners = new Map();
    const tools = new Set(['ruler', 'pen', 'eraser', 'fog', 'fog-eraser']);
    let active = null;
    function dispatch(type, event) {
        for (const listener of [...(listeners.get(type) || [])]) listener(event);
    }
    function finish(type, event) {
        if (!active) return;
        const previous = active;
        active = null;
        dispatch(type, event);
        if (previous.target.hasPointerCapture(previous.id)) previous.target.releasePointerCapture(previous.id);
    }
    document.addEventListener('pointerdown', event => {
        if (active || event.button !== 0 || !event.isPrimary) return;
        const unit = event.target.closest('#unitOverlay .unit');
        const board = event.target.closest('#grid-container');
        if (!unit && !board) return;
        // Empty board space remains available for native touch scrolling/zooming.
        if (!tools.has(document.body.dataset.tool) && (!unit || document.body.dataset.tool === 'ping')) return;
        event.preventDefault();
        active = {id: event.pointerId, target: unit || board};
        if (event.isTrusted) active.target.setPointerCapture(event.pointerId);
        dispatch('down', event);
    });
    document.addEventListener('pointermove', event => {
        if (active?.id !== event.pointerId) return;
        event.preventDefault();
        dispatch('move', event);
    });
    document.addEventListener('pointerup', event => {
        if (active?.id === event.pointerId) finish('up', event);
    });
    document.addEventListener('pointercancel', event => {
        if (active?.id === event.pointerId) finish('cancel', event);
    });
    document.addEventListener('lostpointercapture', event => {
        if (active?.id === event.pointerId) finish('cancel', event);
    });
    window.addEventListener('blur', event => finish('cancel', event));
    document.addEventListener('visibilitychange', event => {
        if (document.hidden) finish('cancel', event);
    });
    return {
        cancel() { finish('cancel'); },
        addEventListener(type, callback) {
            if (!listeners.has(type)) listeners.set(type, new Set());
            listeners.get(type).add(callback);
        },
        removeEventListener(type, callback) { listeners.get(type)?.delete(callback); },
    };
})();
