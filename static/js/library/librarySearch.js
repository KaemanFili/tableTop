(() => {
    const initialized = new WeakSet();
    const queries = new Map();
    const normalize = value => value.normalize('NFKD').replace(/\p{M}/gu, '').toLowerCase();

    // A response can arrive after more typing but before the next debounced
    // request starts. Keep the live input (and its pending request) in that case.
    document.addEventListener('htmx:beforeSwap', event => {
        const request = event.detail.requestConfig;
        const source = request?.elt;
        if (!source?.matches('[data-library-search-input][hx-get]')) return;
        const current = document.getElementById(source.id);
        if (source !== current || request.parameters.q !== current.value) {
            event.detail.shouldSwap = false;
        }
    });

    function initialize(container) {
        if (initialized.has(container)) return;
        const input = container.querySelector('[data-library-search-input]');
        const results = container.querySelector('[data-library-search-results]');
        const empty = container.querySelector('[data-library-search-empty]');
        if (!input || !results || !empty) return;
        initialized.add(container);
        const kind = container.dataset.librarySearch;
        if (container.hasAttribute('data-library-server-search')) {
            input.addEventListener('htmx:afterRequest', event => {
                if (!event.detail.successful) results.textContent = 'Could not search units. Try again.';
            });
            return;
        }
        const noun = kind === 'maps' ? 'map' : 'unit';
        const entries = [...container.querySelectorAll('[data-library-name]')].map(element => ({
            element,
            name: normalize(element.dataset.libraryName),
        }));
        const groups = [...container.querySelectorAll('[data-library-group]')].map(element => ({
            element,
            count: element.querySelector(':scope > summary [data-library-count]'),
            entries: entries.filter(entry => element.contains(entry.element)),
        }));
        input.value = queries.get(kind) || '';

        function filter() {
            queries.set(kind, input.value);
            const terms = normalize(input.value).trim().split(/\s+/).filter(Boolean);
            let count = 0;
            for (const entry of entries) {
                entry.element.hidden = !terms.every(term => entry.name.includes(term));
                if (!entry.element.hidden) count++;
            }
            for (const group of groups) {
                const matches = group.entries.filter(entry => !entry.element.hidden).length;
                group.count.textContent = matches;
                group.element.hidden = terms.length > 0 && matches === 0;
                if (terms.length && matches) group.element.open = true;
            }
            results.textContent = `${count} ${noun}${count === 1 ? '' : 's'}${terms.length ? ' found' : ''}`;
            empty.hidden = terms.length === 0 || count > 0;
        }

        input.addEventListener('input', filter);
        filter();
    }

    function initializeWithin(root) {
        if (root.matches?.('[data-library-search]')) initialize(root);
        root.querySelectorAll?.('[data-library-search]').forEach(initialize);
    }

    document.addEventListener('htmx:load', event => initializeWithin(event.detail.elt));
    initializeWithin(document);
})();
