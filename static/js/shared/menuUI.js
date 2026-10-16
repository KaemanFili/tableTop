// Shared positioning and dismissal for floating menus. Callers own their actions.
const menuUI = Object.freeze({
    mobile: matchMedia('(max-width: 700px)'),
    touch: matchMedia('(pointer: coarse)'),
    usesTouchControls() { return menuUI.mobile.matches || menuUI.touch.matches; },
    visibleViewport() {
        const viewport = window.visualViewport;
        return {left: viewport?.offsetLeft || 0, top: viewport?.offsetTop || 0,
            width: viewport?.width || window.innerWidth, height: viewport?.height || window.innerHeight};
    },
    position(panel, anchor, {placement = 'right', align = 'start', gap = 8} = {}) {
        const target = anchor.getBoundingClientRect();
        const bounds = panel.getBoundingClientRect();
        const margin = 8;
        const viewport = menuUI.visibleViewport();
        let left = target.right + gap;
        let top = align === 'center' ? target.top + (target.height - bounds.height) / 2 : target.top;
        if (placement === 'below') {
            left = target.left + (target.width - bounds.width) / 2;
            top = target.bottom + gap;
        }
        if (placement === 'above') {
            left = target.left + (target.width - bounds.width) / 2;
            const above = target.top - bounds.height - gap;
            top = above >= margin ? above : target.bottom + gap;
        }
        panel.style.left = `${Math.max(viewport.left + margin, Math.min(left, viewport.left + viewport.width - bounds.width - margin))}px`;
        panel.style.top = `${Math.max(viewport.top + margin, Math.min(top, viewport.top + viewport.height - bounds.height - margin))}px`;
    },

    popover(panel, buttons, positionOptions = {}) {
        let activeButton = null;
        let viewportWidth = window.innerWidth;
        let openedHeight = window.innerHeight;
        let layoutFrame = null;
        function editing() {
            const field = document.activeElement;
            return panel?.contains(field) && field.matches('input, textarea, select, [contenteditable="true"]');
        }
        function layout() {
            layoutFrame = null;
            if (!panel || panel.hidden || !activeButton) return;
            const viewport = menuUI.visibleViewport();
            const keyboard = menuUI.usesTouchControls() && editing() && viewport.height < openedHeight - 80;
            panel.style.setProperty('--popover-bottom', `${Math.max(0, window.innerHeight - viewport.top - viewport.height)}px`);
            panel.style.setProperty('--popover-gap', keyboard ? '8px' : '72px');
            if (keyboard) panel.style.setProperty('--popover-max-height', `${Math.max(44, viewport.height - 16)}px`);
            else panel.style.removeProperty('--popover-max-height');
            menuUI.position(panel, activeButton, positionOptions);
            if (editing()) {
                const field = document.activeElement.getBoundingClientRect();
                const bounds = panel.getBoundingClientRect();
                const closeButton = panel.querySelector('[data-mobile-close-panel]');
                const top = Math.max(bounds.top + 8, (closeButton?.getBoundingClientRect().bottom || bounds.top) + 8);
                // Scroll the sheet, rather than asking the browser to pan the board.
                if (field.bottom > bounds.bottom - 8) panel.scrollTop += field.bottom - bounds.bottom + 8;
                else if (field.top < top) panel.scrollTop -= top - field.top;
            }
        }
        function queueLayout() {
            if (!panel || panel.hidden || layoutFrame !== null) return;
            layoutFrame = requestAnimationFrame(layout);
        }
        function close() {
            if (layoutFrame !== null) cancelAnimationFrame(layoutFrame);
            layoutFrame = null;
            if (panel) panel.hidden = true;
            buttons.forEach(button => button.setAttribute('aria-expanded', 'false'));
            activeButton = null;
            positionOptions.onClose?.();
        }
        function open(button, focusTarget) {
            if (!panel) return;
            close();
            document.dispatchEvent(new CustomEvent('uiPanelOpening', {detail: {id: panel.id}}));
            activeButton = button;
            openedHeight = window.innerHeight;
            panel.hidden = false;
            button.setAttribute('aria-expanded', 'true');
            positionOptions.onOpen?.();
            layout();
            focusTarget?.focus({preventScroll: true});
        }
        if (panel) {
            document.addEventListener('pointerdown', event => {
                if (event.target === positionOptions.backdrop) return;
                if (!panel.contains(event.target) && !buttons.some(button => button.contains(event.target))) close();
            });
            document.addEventListener('keydown', event => {
                if (event.key !== 'Escape' || panel.hidden) return;
                event.preventDefault();
                const button = activeButton;
                close();
                button?.focus({preventScroll: true});
            }, true);
            document.addEventListener('uiDismissed', close);
            document.addEventListener('boardCleared', close);
            document.addEventListener('uiPanelOpening', event => {
                if (menuUI.mobile.matches && event.detail.id !== panel.id) close();
            });
            document.addEventListener('scroll', event => {
                if (menuUI.usesTouchControls() && editing()) { queueLayout(); return; }
                if (menuUI.usesTouchControls() &&
                    event.target !== document.getElementById('board-viewport')) return;
                if (!panel.contains(event.target)) close();
            }, true);
            document.addEventListener('toolChanged', close);
            window.addEventListener('resize', () => {
                const widthChanged = viewportWidth !== window.innerWidth;
                viewportWidth = window.innerWidth;
                // A phone keyboard changes the height without changing layout width.
                if (menuUI.usesTouchControls() && (!widthChanged || editing())) { queueLayout(); return; }
                close();
            });
            window.visualViewport?.addEventListener('resize', queueLayout);
            window.visualViewport?.addEventListener('scroll', queueLayout);
            panel.addEventListener('focusin', queueLayout);
            panel.addEventListener('focusout', queueLayout);
        }
        return {open, close};
    },
});
