// Share keyboard normalization between preference capture and board activation.
const utilityHotkeys = (() => {
    function fromEvent(event) {
        if (event.isComposing || event.keyCode === 229 || event.getModifierState('AltGraph')) return null;
        let key = event.key.toLowerCase();
        if (!/^[a-z0-9]$/.test(key) && /^Digit[0-9]$/.test(event.code)) key = event.code.slice(-1);
        if (!/^[a-z0-9]$/.test(key) && (event.ctrlKey || event.altKey || event.metaKey) && /^Key[A-Z]$/.test(event.code)) key = event.code.slice(3).toLowerCase();
        if (!/^[a-z0-9]$/.test(key)) return null;
        const parts = [];
        if (event.ctrlKey) parts.push('ctrl');
        if (event.altKey) parts.push('alt');
        if (event.shiftKey) parts.push('shift');
        if (event.metaKey) parts.push('meta');
        return [...parts, key].join('+');
    }
    function label(binding) {
        const names = {ctrl: 'Ctrl', alt: 'Alt', shift: 'Shift', meta: 'Meta'};
        return binding.split('+').map(part => names[part] || part.toUpperCase()).join('+');
    }
    return {fromEvent, label};
})();
