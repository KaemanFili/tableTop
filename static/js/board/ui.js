(() => {
    const controls = document.getElementById('ui-overlay');
    const toggle = document.getElementById('toggle-ui-controls');
    const mapButton = document.getElementById('utility-map');
    const mapOptions = document.getElementById('map-options');
    const gmButton = document.getElementById('gm-button');
    const gmPanel = document.getElementById('gm-panel');

    const mapMenu = menuUI.popover(mapOptions, mapButton ? [mapButton] : []);
    const closeMapOptions = mapMenu.close;

    const boardControls = document.getElementById('board-controls');
    const toolsToggle = document.getElementById('mobile-tools-toggle');
    const mobile = menuUI.mobile;
    const account = document.getElementById('signed-in-user');
    const accountSlot = document.createComment('account controls');
    account?.before(accountSlot);
    let toolsOpen = false;
    function setToolsOpen(open) {
        toolsOpen = open;
        document.body.dataset.mobileTools = open ? 'open' : 'closed';
        toolsToggle.setAttribute('aria-expanded', String(open));
    }
    function applyMobileLayout() {
        setToolsOpen(false);
        if (account) {
            if (mobile.matches) controls.prepend(account);
            else accountSlot.after(account);
        }
        if (mobile.matches && gmPanel) {
            gmPanel.hidden = true;
            gmButton.setAttribute('aria-expanded', 'false');
            gmButton.setAttribute('aria-label', 'Show GM controls');
            gmButton.title = 'Show GM controls';
        }
    }
    applyMobileLayout();
    mobile.addEventListener('change', applyMobileLayout);
    toolsToggle.addEventListener('click', () => setToolsOpen(!toolsOpen));
    document.addEventListener('toolChanged', () => {
        if (mobile.matches) setToolsOpen(false);
        const tool = document.body.dataset.tool;
        toolsToggle.textContent = tool ? `${tool.replaceAll('-', ' ')} · Tools` : 'Tools';
    });
    const panelIDs = ['spawn-menu', 'map-editor', 'dice-menu', 'pen-settings', 'fog-settings', 'map-options'];
    function addCloseButtons() {
        for (const id of panelIDs) {
            const panel = document.getElementById(id);
            if (!panel || panel.querySelector('[data-mobile-close-panel]')) continue;
            const button = document.createElement('button');
            button.type = 'button';
            button.className = 'mobile-sheet-close';
            button.dataset.mobileClosePanel = '';
            button.textContent = 'Close';
            button.setAttribute('aria-label', 'Close panel');
            panel.prepend(button);
        }
    }
    function closePanel(id) {
        const panel = document.getElementById(id);
        if (!panel || panel.hidden) return;
        panel.hidden = true;
        if (id === 'spawn-menu') htmx.ajax('GET', boardURL('/spawn-menu'), {target: '#spawn-controls', swap: 'outerHTML'});
    }
    function closePanels() {
        document.dispatchEvent(new CustomEvent('uiDismissed'));
        panelIDs.forEach(closePanel);
    }
    addCloseButtons();
    document.addEventListener('uiPanelOpening', event => {
        if (!mobile.matches || event.detail.id === 'session-users') return;
        setToolsOpen(false);
        for (const id of ['spawn-menu', 'map-editor']) {
            if (id !== event.detail.id) closePanel(id);
        }
    });
    document.addEventListener('click', event => {
        if (event.target.closest('[data-mobile-close-panel]')) {
            closePanels();
            toolsToggle.focus({preventScroll: true});
        }
    });
    document.getElementById('mobile-brush-settings').addEventListener('click', () => {
        const tool = document.body.dataset.tool;
        const button = document.getElementById(tool === 'fog' || tool === 'fog-eraser' ? `utility-${tool}` : 'utility-pen');
        button.dispatchEvent(new MouseEvent('contextmenu', {bubbles: true, cancelable: true, button: 2}));
    });
    toggle.addEventListener('click', () => {
        const hidden = document.body.dataset.uiHidden !== 'true';
        closePanels();
        boardInput.cancel();
        document.body.dataset.uiHidden = String(hidden);
        controls.hidden = hidden;
        boardControls.hidden = hidden;
        toolsToggle.hidden = hidden;
        setToolsOpen(false);
        document.dispatchEvent(new CustomEvent('uiVisibilityChanged', {detail: {visible: !hidden}}));
        toggle.textContent = hidden ? 'Show UI' : 'Hide UI';
        toggle.title = hidden ? 'Show controls' : 'Hide controls';
        toggle.setAttribute('aria-expanded', String(!hidden));
    });
    document.addEventListener('htmx:afterSwap', event => {
        addCloseButtons();
        if (event.detail.target.id === 'spawn-controls' && !document.getElementById('spawn-menu').hidden) {
            document.dispatchEvent(new CustomEvent('uiPanelOpening', {detail: {id: 'spawn-menu'}}));
        }
    });

    gmButton?.addEventListener('click', () => {
        gmPanel.hidden = !gmPanel.hidden;
        if (gmPanel.hidden) closeMapOptions();
        const label = gmPanel.hidden ? 'Show GM controls' : 'Hide GM controls';
        gmButton.title = label;
        gmButton.setAttribute('aria-label', label);
        gmButton.setAttribute('aria-expanded', String(!gmPanel.hidden));
    });

    document.addEventListener('htmx:afterRequest', event => {
        if (!event.detail.elt?.matches('[hx-delete*="/unit-library/"]') || event.detail.successful) return;
        const menu = document.getElementById('spawn-menu');
        if (!menu) return;
        let message = menu.querySelector('[data-delete-error]');
        if (!message) {
            message = document.createElement('p');
            message.className = 'menu-error';
            message.dataset.deleteError = '';
            message.setAttribute('role', 'alert');
            menu.append(message);
        }
        message.textContent = 'Could not delete the unit. Try again.';
    });

    if (!mapButton || !mapOptions) return;
    const clearButton = document.getElementById('clear-map');
    const error = document.getElementById('map-options-error');
    const saveButton = document.getElementById('save-map');
    const success = document.getElementById('map-options-success');
    saveButton.addEventListener('htmx:beforeRequest', event => {
        error.hidden = true;
        success.hidden = true;
        if (newUnitUpdateSocket.readyState !== WebSocket.OPEN) {
            event.preventDefault();
            error.textContent = 'Reconnecting automatically. Try saving the map again shortly.';
            error.hidden = false;
        }
    });
    saveButton.addEventListener('htmx:afterRequest', event => {
        if (event.detail.successful) {
            success.textContent = 'Map saved.';
            success.hidden = false;
        } else {
            const response = event.detail.xhr;
            error.textContent = response?.status === 507
                ? response.responseText.trim()
                : response?.status === 409
                    ? response.responseText.trim() : 'Could not save the map. Try again.';
            error.hidden = false;
        }
    });
    const editor = document.getElementById('map-editor');
    for (const id of ['create-map', 'load-map']) document.getElementById(id).addEventListener('htmx:afterRequest', event => {
        if (!event.detail.successful) {
            error.textContent = 'Could not open maps. Try again.';
            error.hidden = false;
        }
    });
    document.addEventListener('htmx:afterSwap', event => {
        if (event.detail.target !== editor) return;
        closeMapOptions();
        editor.hidden = Boolean(editor.querySelector('[data-map-saved]'));
        if (!editor.hidden) document.dispatchEvent(new CustomEvent('uiPanelOpening', {detail: {id: 'map-editor'}}));
        if (!editor.hidden) {
            const firstControl = editor.querySelector('input') || editor.querySelector('[data-load-map]') || editor.querySelector('button');
            firstControl?.focus({preventScroll: true});
        }
    });
    editor.addEventListener('htmx:beforeRequest', event => {
        if (!event.detail.elt?.matches('[data-load-map]')) return;
        const message = document.getElementById('map-picker-error');
        message.hidden = true;
        if (newUnitUpdateSocket.readyState !== WebSocket.OPEN) {
            event.preventDefault();
            message.textContent = 'Reconnecting automatically. Try loading a map again shortly.';
            message.hidden = false;
        }
    });
    editor.addEventListener('htmx:afterRequest', event => {
        if (!event.detail.elt?.matches('[data-load-map], [data-delete-map]') || event.detail.successful) return;
        const message = document.getElementById('map-picker-error');
        if (!message) return;
        message.textContent = event.detail.elt.matches('[data-delete-map]')
            ? 'Could not delete the saved map. Try again.' : 'Could not load the saved map. Try again.';
        message.hidden = false;
    });
    editor.addEventListener('click', event => {
        if (event.target.closest('[data-close-map-editor]')) {
            editor.hidden = true;
            editor.replaceChildren();
            mapButton.focus({preventScroll: true});
        }
    });
    clearButton.addEventListener('htmx:beforeRequest', event => {
        error.hidden = true;
        if (newUnitUpdateSocket.readyState !== WebSocket.OPEN) {
            event.preventDefault();
            error.textContent = 'Reconnecting automatically. Try clearing the map again shortly.';
            error.hidden = false;
        }
    });
    clearButton.addEventListener('htmx:afterRequest', event => {
        if (event.detail.successful) closeMapOptions();
        else {
            error.textContent = 'Could not clear the map. Try again.';
            error.hidden = false;
        }
    });
    mapButton.addEventListener('click', () => {
        if (!mapOptions.hidden) {
            closeMapOptions();
            return;
        }
        error.hidden = true;
        success.hidden = true;
        mapMenu.open(mapButton, mapOptions.querySelector('button:not([data-mobile-close-panel])'));
    });
})();
