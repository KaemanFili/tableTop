(() => {
    const controls = document.getElementById('ui-overlay');
    const toggle = document.getElementById('toggle-ui-controls');

    toggle.addEventListener('click', () => {
        controls.hidden = !controls.hidden;
        toggle.textContent = controls.hidden ? 'Show UI' : 'Hide UI';
        toggle.title = controls.hidden ? 'Show controls' : 'Hide controls';
        toggle.setAttribute('aria-expanded', String(!controls.hidden));
    });
})();
