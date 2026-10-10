(() => {
    const menu = document.getElementById('token-menu');
    const overlay = document.getElementById('unitOverlay');
    const remove = document.getElementById('remove-token');
    const increase = document.getElementById('increase-token-size');
    const decrease = document.getElementById('decrease-token-size');
    const sizes = JSON.parse(document.getElementById('unit-sizes').textContent);
    const error = document.getElementById('token-menu-error');
    let previousFocus = null;
    let pendingRequest = null;

    function updateControls() {
        const unit = document.getElementById(menu.dataset.unitId);
        if (!unit) return;
        const index = sizes.findIndex(size => size.Value === unit.dataset.size);
        const label = sizes[index]?.Label || unit.dataset.size;
        increase.disabled = !!pendingRequest || index < 0 || index === sizes.length - 1;
        decrease.disabled = !!pendingRequest || index <= 0;
        remove.disabled = !!pendingRequest;
        increase.title = `Increase size (currently ${label})`;
        decrease.title = `Decrease size (currently ${label})`;
    }

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
        for (const button of [increase, decrease]) {
            button.setAttribute('hx-post', `/unit/${encodeURIComponent(unit.id)}/size`);
            htmx.process(button);
        }
        menu.hidden = false;

        updateControls();
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
    for (const button of [remove, increase, decrease]) {
        button.addEventListener('htmx:beforeRequest', event => {
            if (pendingRequest || !menu.dataset.unitId) {
                event.preventDefault();
                return;
            }
            pendingRequest = event.detail.xhr;
            pendingRequest.unitID = menu.dataset.unitId;
            error.hidden = true;
            updateControls();
        });
        button.addEventListener('htmx:afterRequest', event => {
            const xhr = event.detail.xhr;
            if (pendingRequest === xhr) pendingRequest = null;
            if (event.detail.failed && menu.dataset.unitId === xhr.unitID) {
                if (button !== remove && xhr.status === 404) {
                    document.dispatchEvent(new CustomEvent('unitRemoved', {detail: {id: xhr.unitID}}));
                } else {
                    error.textContent = button === remove ? 'Could not remove. Try again.' : 'Could not resize. Try again.';
                    error.hidden = false;
                    const unit = document.getElementById(menu.dataset.unitId);
                    if (unit) position(unit);
                }
            }
            updateControls();
        });
    }
    document.addEventListener('unitSizeChanged', event => {
        // The display listener applies the new size before this task runs.
        queueMicrotask(() => {
            if (menu.hidden || menu.dataset.unitId !== event.detail.id) return;
            const unit = document.getElementById(menu.dataset.unitId);
            if (!unit) return;
            updateControls();
            position(unit);
        });
    });
    document.addEventListener('unitRemoved', event => {
        if (menu.dataset.unitId === event.detail.id) close();
    });
    // Also dismiss if tokens are replaced without HTMX.
    new MutationObserver(close).observe(overlay, {childList: true});
})();
