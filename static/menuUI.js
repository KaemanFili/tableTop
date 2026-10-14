// Shared positioning and dismissal for floating menus. Callers own their actions.
const menuUI = Object.freeze({
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
        function close() {
            if (panel) panel.hidden = true;
            buttons.forEach(button => button.setAttribute('aria-expanded', 'false'));
            activeButton = null;
        }
        function open(button, focusTarget) {
            if (!panel) return;
            close();
            activeButton = button;
            panel.hidden = false;
            button.setAttribute('aria-expanded', 'true');
            menuUI.position(panel, button, positionOptions);
            focusTarget?.focus({preventScroll: true});
        }
        if (panel) {
            document.addEventListener('pointerdown', event => {
                if (!panel.contains(event.target) && !buttons.some(button => button.contains(event.target))) close();
            });
            document.addEventListener('keydown', event => {
                if (event.key !== 'Escape' || panel.hidden) return;
                event.preventDefault();
                const button = activeButton;
                close();
                button?.focus({preventScroll: true});
            }, true);
            document.addEventListener('scroll', close, true);
            document.addEventListener('toolChanged', close);
            window.addEventListener('resize', close);
        }
        return {open, close};
    },
});
