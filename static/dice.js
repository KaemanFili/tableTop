(() => {
    const button = document.getElementById('roll-dice');
    const panel = document.getElementById('dice-menu');
    const result = document.getElementById('dice-result');
    const animation = diceAnimation.create(document.getElementById('dice-layer'), document.getElementById('dice-status'));
    const notifications = notificationUI.create(document.getElementById('dice-notifications'), document.getElementById('notification-template'));
    const form = document.getElementById('dice-form');
    const inputs = document.getElementById('dice-inputs');
    const groups = document.getElementById('dice-groups');
    const errorMessage = document.getElementById('dice-error');
    const shareRolls = document.getElementById('share-dice-rolls');
    const menu = menuUI.popover(panel, [button], {placement: 'above'});
    let busy = false;
    const ownRequests = new Set();
    const received = new Map();

    function addGroup() {
        const template = document.getElementById('dice-group-template');
        groups.append(template.content.cloneNode(true));
        updateRemoveButtons();
        return groups.lastElementChild;
    }

    function updateRemoveButtons() {
        groups.querySelectorAll('[data-remove-dice]').forEach(button => {
            button.disabled = groups.children.length === 1;
        });
    }

    function readGroups() {
        const selected = [...groups.children].map(row => ({
            qty: Number(row.querySelector('[name="quantity"]').value),
            sides: Number(row.querySelector('[name="sides"]').value),
            modifier: Number(row.querySelector('[name="modifier"]').value),
        }));
        if (selected.some(group => !Number.isInteger(group.qty) || group.qty < 1 ||
            ![4, 6, 8, 10, 12, 20, 100].includes(group.sides) || !Number.isSafeInteger(group.modifier) || Math.abs(group.modifier) > 1000000)) {
            throw new Error('Use whole numbers for counts and modifiers.');
        }
        if (selected.reduce((total, group) => total + group.qty, 0) > 100) {
            throw new Error('Roll up to 100 dice at a time.');
        }
        return selected;
    }

    function showResults(target, message) {
        target.replaceChildren();
        message.groups.forEach(group => {
            const modifier = group.modifier === 0 ? '' : ` ${group.modifier > 0 ? '+' : '−'}${Math.abs(group.modifier)}`;
            const line = document.createElement('div');
            line.className = 'dice-group-result';
            line.textContent = `${group.qty}d${group.sides}${modifier}: [${group.values.join(', ')}]${modifier} = ${group.subtotal}`;
            target.append(line);
        });
        const summary = document.createElement('strong');
        summary.textContent = `Total: ${message.total}`;
        target.append(summary);
    }

    function notify(message) {
        const title = message.private ? 'Your private roll' :
            ownRequests.has(message.requestId) ? 'You rolled' : 'A player rolled';
        const details = document.createElement('div');
        showResults(details, message);
        notifications.show(title, details);
    }

    function receiveRoll(message) {
        if (received.has(message.id)) return received.get(message.id);
        notify(message);
        showResults(result, message);
        const task = animation.play(message);
        received.set(message.id, task);
        if (received.size > 1000) received.delete(received.keys().next().value);
        return task;
    }

    async function roll(selected) {
        if (busy) return;
        busy = true;
        inputs.disabled = true;
        panel.setAttribute('aria-busy', 'true');
        result.textContent = 'Rolling dice…';
        const requestId = Array.from(crypto.getRandomValues(new Uint8Array(16)),
            value => value.toString(16).padStart(2, '0')).join('');
        ownRequests.add(requestId);
        try {
            if (newUnitUpdateSocket.readyState !== WebSocket.OPEN) {
                throw new Error('Refresh to reconnect before rolling dice.');
            }
            const response = await fetch(`/dice/roll${shareRolls ? '?gm=true' : ''}`, {
                method: 'POST',
                headers: {'Content-Type': 'application/json'},
                body: JSON.stringify({requestId, groups: selected, private: shareRolls ? !shareRolls.checked : false}),
                signal: AbortSignal.timeout(10000),
            });
            if (!response.ok) throw new Error((await response.text()).trim());
            await receiveRoll(await response.json());
        } catch (error) {
            errorMessage.textContent = error.message || 'Could not roll. Try again.';
            errorMessage.hidden = false;
            if (result.textContent === 'Rolling dice…') result.textContent = 'Roll failed.';
        } finally {
            ownRequests.delete(requestId);
            busy = false;
            inputs.disabled = false;
            panel.setAttribute('aria-busy', 'false');
        }
    }

    document.addEventListener('diceRolled', event => receiveRoll(event.detail));
    shareRolls?.addEventListener('change', () => {
        document.getElementById('dice-visibility-hint').textContent = shareRolls.checked
            ? 'Everyone sees your results and animations.'
            : 'Only you see your results and animations.';
    });

    button.addEventListener('click', () => {
        if (panel.hidden) {
            menu.open(button, groups.querySelector('input'));
            animation.load();
        }
        else menu.close();
    });
    document.getElementById('add-dice-group').addEventListener('click', () => {
        const row = addGroup();
        menuUI.position(panel, button, {placement: 'above'});
        row.querySelector('input').focus({preventScroll: true});
    });
    groups.addEventListener('click', event => {
        const remove = event.target.closest('[data-remove-dice]');
        if (!remove || groups.children.length === 1) return;
        remove.closest('.dice-group').remove();
        updateRemoveButtons();
        groups.querySelector('input').focus({preventScroll: true});
    });
    form.addEventListener('submit', event => {
        event.preventDefault();
        if (busy || !form.reportValidity()) return;
        errorMessage.hidden = true;
        try {
            roll(readGroups());
        } catch (error) {
            errorMessage.textContent = error.message;
            errorMessage.hidden = false;
        }
    });
    addGroup();
})();
