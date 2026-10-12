(() => {
    const menu = document.getElementById('token-menu');
    const overlay = document.getElementById('unitOverlay');
    const remove = document.getElementById('remove-token');
    const increase = document.getElementById('increase-token-size');
    const decrease = document.getElementById('decrease-token-size');
    const sizes = JSON.parse(document.getElementById('unit-sizes').textContent);
    const error = document.getElementById('token-menu-error');
    const statusToggle = document.getElementById('toggle-token-statuses');
    const statuses = document.getElementById('token-statuses');
    const tooltip = document.getElementById('token-status-tooltip');
    const circles = [...statuses.querySelectorAll('button')];
    let hoveredStatus = null;
    let previousFocus = null;
    let pendingRequest = null;

    function updateTooltip() {
        const circle = hoveredStatus || (statuses.contains(document.activeElement) ? document.activeElement : null);
        tooltip.hidden = statuses.hidden || !circle;
        if (tooltip.hidden) return;
        tooltip.textContent = circle.getAttribute('aria-label');
        menuUI.position(tooltip, circle, {placement: 'above', gap: 6});
    }

    function closeStatuses(restoreFocus = false) {
        statuses.hidden = true;
        tooltip.hidden = true;
        hoveredStatus = null;
        statusToggle.setAttribute('aria-expanded', 'false');
        if (restoreFocus) statusToggle.focus({preventScroll: true});
    }

    function positionStatuses(unit) {
        if (statuses.hidden) return;
        menuUI.position(statuses, unit, {placement: 'above'});
        updateTooltip();
    }

    statusToggle.addEventListener('click', () => {
        if (!statuses.hidden) {
            closeStatuses(true);
            return;
        }
        const unit = document.getElementById(menu.dataset.unitId);
        if (!unit) return;
        statuses.hidden = false;
        statuses.scrollLeft = 0;
        statusToggle.setAttribute('aria-expanded', 'true');
        positionStatuses(unit);
        statusToggle.focus({preventScroll: true});
    });
    statusToggle.addEventListener('keydown', event => {
        if (statuses.hidden || event.key !== 'ArrowDown') return;
        event.preventDefault();
        circles[0].focus({preventScroll: true});
    });
    for (const circle of circles) {
        circle.addEventListener('pointerenter', () => {
            hoveredStatus = circle;
            updateTooltip();
        });
        circle.addEventListener('pointerleave', () => {
            hoveredStatus = null;
            updateTooltip();
        });
        circle.addEventListener('focus', updateTooltip);
        circle.addEventListener('blur', () => queueMicrotask(updateTooltip));
    }
    statuses.addEventListener('keydown', event => {
        const index = circles.indexOf(document.activeElement);
        if (index < 0) return;
        let next;
        if (event.key === 'ArrowRight') next = (index + 1) % circles.length;
        if (event.key === 'ArrowLeft') next = (index + circles.length - 1) % circles.length;
        if (event.key === 'Home') next = 0;
        if (event.key === 'End') next = circles.length - 1;
        if (next === undefined) return;
        event.preventDefault();
        circles[next].focus({preventScroll: true});
        circles[next].scrollIntoView({block: 'nearest', inline: 'nearest'});
        updateTooltip();
    });

    function updateControls() {
        const unit = document.getElementById(menu.dataset.unitId);
        if (!unit) return;
        const index = sizes.findIndex(size => size.Value === unit.dataset.size);
        const label = sizes[index]?.Label || unit.dataset.size;
        increase.disabled = !!pendingRequest || index < 0 || index === sizes.length - 1;
        decrease.disabled = !!pendingRequest || index <= 0;
        remove.disabled = !!pendingRequest;
        for (const circle of circles) {
            const selected = unit.dataset.status === circle.getAttribute('aria-label');
            circle.setAttribute('aria-pressed', String(selected));
            circle.disabled = !!pendingRequest;
            circle.setAttribute('hx-vals', JSON.stringify({status: selected ? '' : circle.getAttribute('aria-label')}));
        }
        increase.title = `Increase size (currently ${label})`;
        decrease.title = `Decrease size (currently ${label})`;
    }

    function position(unit) {
        menuUI.position(menu, unit, {align: 'center', gap: 6});
        positionStatuses(unit);
    }

    function close() {
        if (menu.hidden) return;
        const hadFocus = menu.contains(document.activeElement) || statuses.contains(document.activeElement);
        closeStatuses();
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
        closeStatuses();
        menu.dataset.unitId = unit.id;
        error.hidden = true;
        error.textContent = '';
        remove.setAttribute('hx-delete', boardURL(`/unit/${encodeURIComponent(unit.id)}`));
        htmx.process(remove);
        for (const button of [increase, decrease, ...circles]) {
            button.setAttribute('hx-post', boardURL(`/unit/${encodeURIComponent(unit.id)}/${circles.includes(button) ? 'status' : 'size'}`));
            htmx.process(button);
        }
        menu.hidden = false;

        updateControls();
        position(unit);
        menu.querySelector('button').focus({preventScroll: true});
    });

    // Capture runs before the existing drag listener. Menu buttons are outside units.
    document.addEventListener('mousedown', event => {
        if (!menu.contains(event.target) && !statuses.contains(event.target)) close();
    }, true);
    document.addEventListener('click', event => {
        if (!menu.contains(event.target) && !statuses.contains(event.target)) close();
    });
    document.addEventListener('keydown', event => {
        if (event.key === 'Escape' && !menu.hidden) {
            event.preventDefault();
            event.stopPropagation();
            if (!statuses.hidden) {
                closeStatuses(true);
                return;
            }
            close();
        }
    }, true);
    document.addEventListener('scroll', event => {
        if (statuses.contains(event.target)) {
            hoveredStatus = null;
            updateTooltip();
            return;
        }
        close();
    }, true);
    window.addEventListener('resize', close);
    document.addEventListener('boardCleared', close);
    document.addEventListener('htmx:beforeSwap', event => {
        if (event.detail.target === overlay) close();
    });
    for (const button of [remove, increase, decrease, ...circles]) {
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
                    error.textContent = button === remove ? 'Could not remove. Try again.' : circles.includes(button) ? 'Could not set status. Try again.' : 'Could not resize. Try again.';
                    error.hidden = false;
                    const unit = document.getElementById(menu.dataset.unitId);
                    if (unit) position(unit);
                }
            }
            updateControls();
        });
    }
    document.addEventListener('unitStatusChanged', () => queueMicrotask(updateControls));
    document.addEventListener('unitPositionChanged', event => {
        if (menu.hidden || menu.dataset.unitId !== event.detail.id) return;
        const unit = document.getElementById(event.detail.id);
        if (unit) position(unit);
    });
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
