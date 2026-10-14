// Zoom the shared-coordinate board without scaling the fixed UI.
const boardZoom = (() => {
    const world = document.getElementById('board-world');
    const viewport = document.getElementById('board-viewport');
    const space = document.getElementById('board-space');
    const reset = document.getElementById('board-zoom-reset');
    const pointers = new Map();
    let scale = 1;
    let pinch = null;
    let pan = null;
    let pinched = false;
    let pinchFrame = null;
    let panFrame = null;
    const pinchSensitivity = 0.55;
    const pinchThreshold = 10;
    const minimumPinchSpan = 80;

    function setScale(value, x = viewport.clientWidth / 2, y = viewport.clientHeight / 2, anchor = null) {
        const next = Math.max(0.25, Math.min(3, value));
        const before = world.getBoundingClientRect();
        const point = anchor || {x: (x - before.left) / scale, y: (y - before.top) / scale};
        scale = next;
        world.style.zoom = scale;
        const after = world.getBoundingClientRect();
        viewport.scrollTo({left: viewport.scrollLeft + after.left + point.x * scale - x,
            top: viewport.scrollTop + after.top + point.y * scale - y, behavior: 'instant'});
        reset.textContent = `${Math.round(scale * 100)}%`;
        document.dispatchEvent(new CustomEvent('viewZoomChanged'));
        document.dispatchEvent(new CustomEvent('viewPanChanged'));
    }
    document.getElementById('board-zoom-in').addEventListener('click', () => setScale(scale * 1.2));
    document.getElementById('board-zoom-out').addEventListener('click', () => setScale(scale / 1.2));
    reset.addEventListener('click', () => setScale(1));
    space.addEventListener('wheel', event => {
        event.preventDefault();
        const delta = event.deltaY * (event.deltaMode === 1 ? 16 : event.deltaMode === 2 ? window.innerHeight : 1);
        setScale(scale * Math.exp(-Math.max(-200, Math.min(200, delta)) * 0.003), event.clientX, event.clientY);
    }, {passive: false});

    function pair() {
        const [a, b] = [...pointers.values()];
        return {distance: Math.hypot(a.x - b.x, a.y - b.y), x: (a.x + b.x) / 2, y: (a.y + b.y) / 2};
    }
    function renderPinch() {
        pinchFrame = null;
        if (!pinch || pointers.size < 2) return;
        const next = pair();
        // Close fingers amplify small errors; wait for a useful span before zooming.
        if (pinch.zoomDistance === null && next.distance >= minimumPinchSpan) pinch.zoomDistance = next.distance;
        const delta = pinch.zoomDistance === null ? 0 : next.distance - pinch.zoomDistance;
        const adjusted = Math.sign(delta) * Math.max(0, Math.abs(delta) - pinchThreshold);
        const targetScale = pinch.zoomDistance === null ? pinch.scale :
            pinch.scale * Math.pow((pinch.zoomDistance + adjusted) / pinch.zoomDistance, pinchSensitivity);
        const dx = next.x - pinch.x, dy = next.y - pinch.y;
        const distance = Math.hypot(dx, dy);
        const movement = distance > 6 ? (distance - 6) / distance : 0;
        if (!pinch.changed && !adjusted && !movement) return;
        pinch.changed = true;
        // A fixed board anchor avoids drift from rebasing rounded scroll positions.
        setScale(targetScale, pinch.x + dx * movement, pinch.y + dy * movement, pinch.anchor);
    }
    function flushPinch() {
        if (pinchFrame === null) return;
        cancelAnimationFrame(pinchFrame);
        renderPinch();
    }

    function startPan(id, started = false) {
        const point = pointers.get(id);
        pan = {id, x: point.x, y: point.y, originX: point.x, originY: point.y, started};
    }
    function renderPan() {
        panFrame = null;
        if (!pan) return;
        const point = pointers.get(pan.id);
        if (!point) return;
        if (!pan.started && Math.hypot(point.x - pan.originX, point.y - pan.originY) < 6) return;
        pan.started = true;
        viewport.scrollTo({left: viewport.scrollLeft + pan.x - point.x,
            top: viewport.scrollTop + pan.y - point.y, behavior: 'instant'});
        // Rebase against the actual scroll position, including clamping at edges.
        pan.x = point.x;
        pan.y = point.y;
        document.dispatchEvent(new CustomEvent('viewPanChanged'));
    }
    function flushPan() {
        if (panFrame === null) return;
        cancelAnimationFrame(panFrame);
        renderPan();
    }

    document.addEventListener('pointerdown', event => {
        if (event.pointerType !== 'touch' || !event.target.closest('#board-space')) return;
        if (pointers.size >= 2) return;
        pointers.set(event.pointerId, {x: event.clientX, y: event.clientY});
        if (pointers.size === 1) {
            pinched = false;
            if (!document.body.dataset.tool && !event.target.closest('.unit')) {
                startPan(event.pointerId);
                if (event.isTrusted) space.setPointerCapture(event.pointerId);
            }
        } else if (pointers.size === 2) {
            flushPan();
            boardInput.cancel();
            if (event.isTrusted) for (const id of pointers.keys()) space.setPointerCapture(id);
            pan = null;
            pinched = true;
            const start = pair();
            const bounds = world.getBoundingClientRect();
            pinch = {...start, scale, zoomDistance: start.distance >= minimumPinchSpan ? start.distance : null,
                anchor: {x: (start.x - bounds.left) / scale, y: (start.y - bounds.top) / scale}, changed: false};
            event.preventDefault();
            event.stopImmediatePropagation();
        }
    }, true);
    document.addEventListener('pointermove', event => {
        if (!pointers.has(event.pointerId)) return;
        pointers.set(event.pointerId, {x: event.clientX, y: event.clientY});
        if (pinch && pointers.size >= 2) {
            // Apply both fingers' latest samples together, at most once per frame.
            if (pinchFrame === null) pinchFrame = requestAnimationFrame(renderPinch);
            event.preventDefault();
            event.stopImmediatePropagation();
        } else if (pan && !pinched) {
            if (panFrame === null) panFrame = requestAnimationFrame(renderPan);
            event.preventDefault();
        }
    }, true);
    function finish(event) {
        if (!pointers.has(event.pointerId)) return;
        flushPinch();
        flushPan();
        pointers.delete(event.pointerId);
        const wasPinched = pinched;
        if (event.type === 'pointercancel' || event.type === 'lostpointercapture') {
            clear();
        } else if (pointers.size === 1 && wasPinched) {
            pinch = null;
            if (!document.body.dataset.tool) {
                // Continue from the remaining finger, without a jump or a new press.
                pinched = false;
                startPan(pointers.keys().next().value, true);
            }
        } else if (!pointers.size) {
            pan = null; pinch = null; pinched = false;
        }
        if (wasPinched) event.stopImmediatePropagation();
    }
    document.addEventListener('pointerup', finish, true);
    document.addEventListener('pointercancel', finish, true);
    document.addEventListener('lostpointercapture', event => {
        if (event.target === space) finish(event);
    }, true);
    function clear() {
        if (pinchFrame !== null) cancelAnimationFrame(pinchFrame);
        if (panFrame !== null) cancelAnimationFrame(panFrame);
        pinchFrame = null; panFrame = null;
        pointers.clear(); pinch = null; pan = null; pinched = false;
    }
    window.addEventListener('blur', clear);
    document.addEventListener('visibilitychange', () => { if (document.hidden) clear(); });
    return {setScale, get scale() { return scale; }};
})();
