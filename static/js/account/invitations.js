(() => {
    const input = document.getElementById('invite-link');
    const button = document.getElementById('copy-invite-link');
    const status = document.getElementById('copy-invite-status');
    if (!input || !button || !status) return;

    input.addEventListener('click', () => input.select());

    async function copyLink() {
        if (navigator.clipboard?.writeText) {
            try {
                await navigator.clipboard.writeText(input.value);
                return true;
            } catch { /* Try copying the selected text if clipboard access fails. */ }
        }
        input.focus({preventScroll: true});
        input.select();
        try { return document.execCommand('copy'); }
        catch { return false; }
    }

    button.addEventListener('click', async () => {
        button.disabled = true;
        status.textContent = '';
        const copied = await copyLink();
        button.disabled = false;
        button.textContent = copied ? 'Copied!' : 'Copy link';
        status.textContent = copied ? 'Invite link copied to clipboard.' :
            'Could not copy automatically. Select the link and copy it manually.';
        if (copied) button.focus({preventScroll: true});
    });
})();
