(() => {
    const controls = document.getElementById('ui-overlay');
    const toggle = document.getElementById('toggle-ui-controls');
    const mapButton = document.getElementById('utility-map');
    const mapOptions = document.getElementById('map-options');
    const gmButton = document.getElementById('gm-button');
    const gmPanel = document.getElementById('gm-panel');

    const mapMenu = menuUI.popover(mapOptions, mapButton ? [mapButton] : []);
    const closeMapOptions = mapMenu.close;

    toggle.addEventListener('click', () => {
        controls.hidden = !controls.hidden;
        if (controls.hidden) closeMapOptions();
        toggle.textContent = controls.hidden ? 'Show UI' : 'Hide UI';
        toggle.title = controls.hidden ? 'Show controls' : 'Hide controls';
        toggle.setAttribute('aria-expanded', String(!controls.hidden));
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
        if (!event.detail.elt?.matches('[hx-delete^="/unit-library/"]') || event.detail.successful) return;
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
            error.textContent = 'Refresh to reconnect before saving the map.';
            error.hidden = false;
        }
    });
    saveButton.addEventListener('htmx:afterRequest', event => {
        if (event.detail.successful) {
            success.textContent = 'Map saved.';
            success.hidden = false;
        } else {
            error.textContent = event.detail.xhr?.status === 409
                ? 'Create or load a map before saving.' : 'Could not save the map. Try again.';
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
            message.textContent = 'Refresh to reconnect before loading a map.';
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
            error.textContent = 'Refresh to reconnect before clearing the map.';
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
    document.addEventListener('boardCleared', closeMapOptions);
    mapButton.addEventListener('click', () => {
        if (!mapOptions.hidden) {
            closeMapOptions();
            return;
        }
        error.hidden = true;
        success.hidden = true;
        mapMenu.open(mapButton, mapOptions.querySelector('button'));
    });
})();
