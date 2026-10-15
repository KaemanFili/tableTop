(() => {
    const select = document.getElementById('hotkey-action');
    const input = document.getElementById('hotkey-input');
    const error = document.getElementById('hotkey-capture-error');
    if (!select || !input) return;
    select.addEventListener('change', () => {
        input.value = utilityHotkeys.label(select.selectedOptions[0].dataset.hotkey || '');
        input.setCustomValidity('');
        error.hidden = true;
    });
    input.addEventListener('keydown', event => {
        if (event.key === 'Tab') return;
        if (event.key === 'Escape') { input.blur(); return; }
        if (event.isComposing || event.keyCode === 229 || event.getModifierState('AltGraph')) return;
        event.preventDefault();
        if (event.repeat) return;
        if (['Control', 'Alt', 'Shift', 'Meta', 'AltGraph'].includes(event.key)) return;
        if ((event.key === 'Backspace' || event.key === 'Delete') && !event.ctrlKey && !event.altKey && !event.metaKey && !event.shiftKey) {
            input.value = ''; input.setCustomValidity(''); error.hidden = true; return;
        }
        const binding = utilityHotkeys.fromEvent(event);
        if (!binding) {
            error.textContent = 'Use a letter or number with optional modifier keys.';
            input.setCustomValidity(error.textContent);
            error.hidden = false;
            return;
        }
        input.value = utilityHotkeys.label(binding);
        input.setCustomValidity('');
        error.hidden = true;
    });
    input.addEventListener('input', () => { input.setCustomValidity(''); error.hidden = true; });
    document.getElementById('clear-hotkey').addEventListener('click', () => {
        input.value = '';
        input.setCustomValidity('');
        error.hidden = true;
        input.focus();
    });
})();
