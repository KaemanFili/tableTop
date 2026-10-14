(() => {
    const button = document.getElementById('session-users-toggle');
    const panel = document.getElementById('session-users');
    const icons = document.getElementById('session-user-icons');
    const status = document.getElementById('session-users-status');
    if (!button || !panel) return;
    const menu = menuUI.popover(panel, [button], {placement: 'below'});
    function toggle() {
        if (panel.hidden) menu.open(button);
        else menu.close();
    }
    button.addEventListener('contextmenu', event => {
        event.preventDefault();
        toggle();
    });
    // Also support touch, keyboard activation, and an ordinary click.
    button.addEventListener('click', toggle);
    document.addEventListener('sessionPresence', event => {
        icons.replaceChildren();
        for (const member of event.detail.members) {
            const icon = document.createElement('span');
            icon.className = 'session-user-icon';
            icon.setAttribute('role', 'listitem');
            icon.tabIndex = 0;
            icon.setAttribute('aria-label', member.name);
            icon.title = member.name;
            // Keep initials visible until the provider's profile photo loads.
            // Names always use text, never HTML.
            const initials = document.createElement('span');
            initials.setAttribute('aria-hidden', 'true');
            initials.textContent = [...member.name.trim()].slice(0, 2).join('').toLocaleUpperCase() || '?';
            const tooltip = document.createElement('span');
            tooltip.className = 'session-user-name';
            tooltip.textContent = member.name;
            tooltip.setAttribute('aria-hidden', 'true');
            icon.append(initials, tooltip);
            if (member.avatarUrl) {
                let url;
                try { url = new URL(member.avatarUrl, location.href); }
                catch { /* An invalid profile URL leaves the initials visible. */ }
                if (url && (url.protocol === 'https:' || url.origin === location.origin)) {
                    const avatar = document.createElement('img');
                    avatar.className = 'session-user-avatar';
                    avatar.alt = '';
                    avatar.hidden = true;
                    avatar.decoding = 'async';
                    avatar.referrerPolicy = 'no-referrer';
                    avatar.addEventListener('load', () => {
                        avatar.hidden = false;
                        initials.hidden = true;
                    });
                    avatar.addEventListener('error', () => {
                        avatar.remove();
                        initials.hidden = false;
                    });
                    avatar.src = url.href;
                    icon.append(avatar);
                }
            }
            icons.append(icon);
        }
        status.textContent = event.detail.disconnected ? 'Reconnecting…' :
            event.detail.members.length ? '' : 'No players connected.';
        status.hidden = !status.textContent;
        if (!panel.hidden) menuUI.position(panel, button, {placement: 'below'});
    });
})();
