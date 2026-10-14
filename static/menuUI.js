// Shared positioning and dismissal for floating menus. Callers own their actions.
const menuUI = Object.freeze({
    mobile: matchMedia('(max-width: 700px)'),
    touch: matchMedia('(pointer: coarse)'),
    usesTouchControls() { return menuUI.mobile.matches || menuUI.touch.matches; },
    position(panel, anchor, {placement = 'right', align = 'start', gap = 8} = {}) {
        const target = anchor.getBoundingClientRect();
        const bounds = panel.getBoundingClientRect();
        const margin = 8;
        let left = target.right + gap;
        let top = align === 'center' ? target.top + (target.height - bounds.height) / 2 : target.top;
        if (placement === 'above') {
            left = target.left + (target.width - bounds.width) / 2;
            const above = target.top - bounds.height - gap;
            top = above >= margin ? above : target.bottom + gap;
        }
        panel.style.left = `${Math.max(margin, Math.min(left, window.innerWidth - bounds.width - margin))}px`;
        panel.style.top = `${Math.max(margin, Math.min(top, window.innerHeight - bounds.height - margin))}px`;
    },

    popover(panel, buttons, positionOptions = {}) {
        let activeButton = null;
        let viewportWidth = window.innerWidth;
        function close() {
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
            panel.hidden = false;
            button.setAttribute('aria-expanded', 'true');
            positionOptions.onOpen?.();
            menuUI.position(panel, button, positionOptions);
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
                if (menuUI.usesTouchControls() &&
                    event.target !== document.getElementById('board-viewport')) return;
                if (!panel.contains(event.target)) close();
            }, true);
            document.addEventListener('toolChanged', close);
            window.addEventListener('resize', () => {
                const widthChanged = viewportWidth !== window.innerWidth;
                viewportWidth = window.innerWidth;
                // A phone keyboard changes the height without changing layout width.
                if (!widthChanged && menuUI.usesTouchControls()) return;
                close();
            });
        }
        return {open, close};
    },
});
