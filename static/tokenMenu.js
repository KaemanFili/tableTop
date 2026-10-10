(() => {
    const menu = document.getElementById('token-menu');
    const overlay = document.getElementById('unitOverlay');
    const remove = document.getElementById('remove-token');
    const error = document.getElementById('token-menu-error');
    let previousFocus = null;

    function position(unit) {
        const token = unit.getBoundingClientRect();
        const bounds = menu.getBoundingClientRect();
        const margin = 8;
        const left = Math.min(token.right + 6, window.innerWidth - bounds.width - margin);
        const top = Math.min(token.top + (token.height - bounds.height) / 2,
            window.innerHeight - bounds.height - margin);
        menu.style.left = `${Math.max(margin, left)}px`;
        menu.style.top = `${Math.max(margin, top)}px`;
    }

    function close() {
        if (menu.hidden) return;
        const hadFocus = menu.contains(document.activeElement);
        menu.hidden = true;
        delete menu.dataset.unitId;
        if (hadFocus && previousFocus?.isConnected) {
            previousFocus.focus({preventScroll: true});
        }
        previousFocus = null;
    }

    document.addEventListener('contextmenu', event => {
        const unit = event.target.closest('#unitOverlay .unit');
        if (!unit) {
            close();
            return;
        }
        event.preventDefault();
        if (menu.hidden) previousFocus = document.activeElement;
        menu.dataset.unitId = unit.id;
        error.hidden = true;
        error.textContent = '';
        remove.setAttribute('hx-delete', `/unit/${encodeURIComponent(unit.id)}`);
        htmx.process(remove);
        menu.hidden = false;

        position(unit);
        menu.querySelector('button').focus({preventScroll: true});
    });

    // Capture runs before the existing drag listener. Menu buttons are outside units.
    document.addEventListener('mousedown', event => {
        if (!menu.contains(event.target)) close();
    }, true);
    document.addEventListener('click', event => {
        if (!menu.contains(event.target)) close();
    });
    document.addEventListener('keydown', event => {
        if (event.key === 'Escape' && !menu.hidden) {
            event.preventDefault();
            event.stopPropagation();
            close();
        }
    }, true);
    document.addEventListener('scroll', close, true);
    window.addEventListener('resize', close);
    document.addEventListener('htmx:beforeSwap', event => {
        if (event.detail.target === overlay) close();
    });
    remove.addEventListener('htmx:beforeRequest', event => {
        event.detail.xhr.unitID = menu.dataset.unitId;
    });
    remove.addEventListener('htmx:afterRequest', event => {
        if (event.detail.failed && menu.dataset.unitId === event.detail.xhr.unitID) {
            error.textContent = 'Could not remove. Try again.';
            error.hidden = false;
            const unit = document.getElementById(menu.dataset.unitId);
            if (unit) position(unit);
        }
    });
    document.addEventListener('unitRemoved', event => {
        if (menu.dataset.unitId === event.detail.id) close();
    });
    // Also dismiss if tokens are replaced without HTMX.
    new MutationObserver(close).observe(overlay, {childList: true});
})();
