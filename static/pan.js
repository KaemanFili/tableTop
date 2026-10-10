(() => {
    let drag = null;
    let suppressMenuUntil = 0;
    let openingUnitMenu = false;

    function finish(openMenu = false) {
        if (!drag) return;
        const previous = drag;
        drag = null;
        suppressMenuUntil = performance.now() + 500;
        document.body.classList.remove('map-panning');
        document.dispatchEvent(new CustomEvent('viewPanChanged'));
        if (openMenu && !previous.moved && previous.unit?.isConnected) {
            // Browsers may fire contextmenu on press or release. Open only after
            // release, when we know this was a click rather than a pan.
            openingUnitMenu = true;
            try {
                previous.unit.dispatchEvent(new MouseEvent('contextmenu', {
                    bubbles: true, cancelable: true, button: 2,
                    clientX: previous.x, clientY: previous.y,
                }));
            } finally {
                openingUnitMenu = false;
            }
        }
    }

    document.addEventListener('mousedown', event => {
        suppressMenuUntil = 0;
        if (event.button !== 2 || (event.buttons & 1)) return;
        if (!event.target.closest('#grid-container, #unitOverlay .unit')) return;
        event.preventDefault();
        drag = {x: event.clientX, y: event.clientY, left: window.scrollX, top: window.scrollY,
            unit: event.target.closest('#unitOverlay .unit'), moved: false};
    }, true);

    document.addEventListener('mousemove', event => {
        if (!drag) return;
        if (!(event.buttons & 2)) {
            finish();
            return;
        }
        const dx = event.clientX - drag.x, dy = event.clientY - drag.y;
        if (!drag.moved && Math.hypot(dx, dy) < 4) return;
        if (!drag.moved) {
            drag.moved = true;
            document.body.classList.add('map-panning');
            document.dispatchEvent(new CustomEvent('viewPanChanged'));
        }
        event.preventDefault();
        window.scrollTo({left: drag.left - dx, top: drag.top - dy, behavior: 'instant'});
    }, true);

    document.addEventListener('mouseup', event => {
        if (event.button === 2) finish(true);
    }, true);
    document.addEventListener('contextmenu', event => {
        if (openingUnitMenu || !event.target.closest('#grid-container, #unitOverlay .unit')) return;
        if (drag || performance.now() < suppressMenuUntil) {
            event.preventDefault();
            event.stopImmediatePropagation();
        } else if (!event.target.closest('#unitOverlay .unit')) {
            event.preventDefault();
        }
    }, true);
    document.addEventListener('keydown', event => {
        if (event.key === 'Escape') finish();
    });
    window.addEventListener('blur', () => finish());
    document.addEventListener('visibilitychange', () => {
        if (document.hidden) finish();
    });
})();
