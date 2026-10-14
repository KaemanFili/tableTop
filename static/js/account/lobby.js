(() => {
    const status = document.querySelector('[data-lobby-loading]');
    if (!status) return;
    function show(message) {
        status.textContent = message;
        status.hidden = false;
    }
    document.addEventListener('submit', event => {
        const form = event.target;
        if (!form.matches('form[action="/sessions"], form[action="/sessions/join"], form[action$="/initialize"]')) return;
        if (event.defaultPrevented) return;
        show(form.action.endsWith('/join') ? 'Joining session…' : 'Preparing your session…');
        const button = event.submitter;
        if (button) {
            button.dataset.idleLabel = button.textContent;
            button.textContent = 'Loading…';
            button.disabled = true;
        }
    });
    document.addEventListener('click', event => {
        const link = event.target.closest('.lobby-table-name[href]');
        if (!link || event.defaultPrevented || event.button !== 0 || event.ctrlKey || event.metaKey || event.shiftKey || event.altKey) return;
        show('Opening session…');
    });
    window.addEventListener('pageshow', () => {
        status.hidden = true;
        document.querySelectorAll('[data-idle-label]').forEach(button => {
            button.textContent = button.dataset.idleLabel;
            button.disabled = false;
            delete button.dataset.idleLabel;
        });
    });
})();
