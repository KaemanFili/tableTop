const pause = ms => new Promise(resolve => setTimeout(resolve, ms));
async function until(predicate, message) {
    const deadline = performance.now() + 10000;
    while (performance.now() < deadline) {
        if (await predicate()) return;
        await pause(20);
    }
    throw new Error(message);
}
function assert(condition, message) {
    if (!condition) throw new Error(message);
}
async function control(action, kind = '') {
    const response = await fetch(`/browser/${action}?kind=${kind}`, {signal: AbortSignal.timeout(5000)});
    assert(response.ok, `Control failed: ${action}`);
}
const progress = stage => fetch(`/browser/progress?stage=${encodeURIComponent(stage)}`);

async function connectBrowser() {
    const frame = document.getElementById('app');
    await until(() => frame.contentDocument?.querySelector('#unitOverlay .unit'), 'Initial token missing');
    const w = frame.contentWindow;
    const d = frame.contentDocument;
    await until(() => w.eval('newUnitUpdateSocket.readyState === WebSocket.OPEN && updateServerSocket.readyState === WebSocket.OPEN'), 'Sockets did not connect');
    const units = () => [...d.querySelectorAll('#unitOverlay .unit')];
    const dragging = () => w.eval('isDragging');
    const mouse = (target, type, x, y) => target.dispatchEvent(new w.PointerEvent(({mousedown: 'pointerdown', mousemove: 'pointermove', mouseup: 'pointerup'})[type], {
        pointerId: 1, pointerType: 'mouse', isPrimary: true,
        bubbles: true, cancelable: true, button: 0, buttons: type === 'mouseup' ? 0 : 1,
        clientX: x, clientY: y,
    }));
    async function startDrag() {
        const token = units()[0];
        const bounds = token.getBoundingClientRect();
        const left = token.style.left, top = token.style.top;
        mouse(token, 'mousedown', bounds.left + 10, bounds.top + 10);
        mouse(d, 'mousemove', bounds.left + 50, bounds.top + 60);
        assert(dragging(), 'Token drag did not start');
        await pause(40);
        const moved = token.getBoundingClientRect();
        assert(token.style.left === left && token.style.top === top && token.style.transform,
            'Desktop dragging rewrites layout coordinates on every frame');
        assert(Math.abs(moved.left - bounds.left - 40) < 1 && Math.abs(moved.top - bounds.top - 50) < 1,
            'Desktop token did not visibly follow the pointer');
        return token;
    }
    function endDrag() {
        mouse(d, 'mouseup', 200, 200);
        assert(!dragging(), 'Token drag did not finish');
    }
    async function remoteSpawn() {
        const response = await fetch('/unit?unitID=knight', {method: 'POST'});
        assert(response.ok, 'Remote spawn failed');
    }
    async function expectCount(count) {
        await until(() => units().length === count, `Expected ${count} rendered tokens, got ${units().length}`);
        await until(() => !d.querySelector('#loadExistingUnits.htmx-request, #unitOverlay.htmx-settling'), 'Unit refresh did not finish settling');
        assert(new Set(units().map(unit => unit.id)).size === count, 'Duplicate token IDs');
    }
    await expectCount(1);
    return {w, d, units, mouse, startDrag, endDrag, remoteSpawn, expectCount};
}

async function testRemoteSpawnDuringDrag(browser) {
    const {units, startDrag, endDrag, remoteSpawn, expectCount} = browser;
    // A remote spawn during a real mouse drag must appear after release.
    const dragged = await startDrag();
    const left = parseInt(dragged.style.left) + 40, top = parseInt(dragged.style.top) + 50;
    await remoteSpawn();
    await pause(100);
    assert(dragged.isConnected && units().length === 1, 'Refresh replaced an active drag');
    endDrag();
    await expectCount(2);
    await until(async () => {
        const saved = await (await fetch('/browser/state')).json();
        const token = saved.find(unit => unit.ID === dragged.id);
        return token.Left === left && token.Top === top;
    }, 'Final drag position did not reach the server');
}

async function testSpawnResponseOrdering(browser) {
    const {d, expectCount} = browser;
    // Deliver the broadcast refresh before the originating spawn response.
    await control('arm', 'spawn');
    const menuReady = new Promise(resolve => {
        function settled(event) {
            if (event.detail.target.id !== 'spawn-controls') return;
            d.removeEventListener('htmx:afterSettle', settled);
            resolve();
        }
        d.addEventListener('htmx:afterSettle', settled);
    });
    d.getElementById('spawn-menu-toggle').click();
    await menuReady;
    assert(d.querySelector('.library-unit'), 'Unit library did not open');
    d.querySelector('.library-unit').click();
    await control('ready');
    await expectCount(3);
    await control('release');
    await until(() => d.getElementById('spawn-menu').hidden, 'Spawn did not close the menu');
    await pause(100);
    await expectCount(3);
}

async function testUnitRefreshOrdering(browser) {
    const {w, remoteSpawn, expectCount} = browser;
    // An older unit response must be followed by the queued refresh.
    await control('arm', 'refresh');
    w.htmx.trigger('#loadExistingUnits', 'loadUnits');
    await control('ready');
    await remoteSpawn();
    await control('release');
    await expectCount(4);
}

async function testUnitRefreshDuringDrag(browser) {
    const {w, startDrag, endDrag, expectCount} = browser;
    // A unit refresh requested before dragging must not interrupt that drag.
    await control('arm', 'refresh');
    w.htmx.trigger('#loadExistingUnits', 'loadUnits');
    await control('ready');
    const held = await startDrag();
    await control('release');
    await pause(100);
    assert(held.isConnected && held.classList.contains('dragging'), 'In-flight unit refresh replaced the dragged token');
    endDrag();
    await until(() => !held.isConnected, 'Deferred unit refresh never completed');
    await expectCount(4);
}

async function testStatusMenuFollowsRemoteMovement(browser) {
    const {w, d, units} = browser;
    await control('positions');
    await until(() => w.eval('pendingUnitPositions.size === 0'), 'Local drops were not confirmed');
    const token = units()[0];
    token.dispatchEvent(new w.MouseEvent('contextmenu', {bubbles: true, cancelable: true, button: 2}));
    const menu = d.getElementById('token-menu');
    const statuses = d.getElementById('token-statuses');
    const tooltip = d.getElementById('token-status-tooltip');
    d.getElementById('toggle-token-statuses').click();
    statuses.querySelector('button').focus({preventScroll: true});
    assert(!menu.hidden && !statuses.hidden && !tooltip.hidden, 'Status menu did not open');
    const origin = token.getBoundingClientRect();
    const anchored = [menu, statuses, tooltip].map(element => ({element, bounds: element.getBoundingClientRect()}));
    const remote = new w.WebSocket(`${w.location.protocol === 'https:' ? 'wss:' : 'ws:'}//${w.location.host}/ws/updateServer`);
    try {
        await new Promise((resolve, reject) => {
            remote.onopen = resolve;
            remote.onerror = reject;
        });
        const confirmed = new Promise(resolve => { remote.onmessage = resolve; });
        const left = parseInt(token.style.left) + 120;
        const top = parseInt(token.style.top) + 90;
        remote.send(JSON.stringify({ID: token.id, Left: left, Top: top}));
        await confirmed;
        await control('positions');
        await until(() => token.style.left === `${left}px` && token.style.top === `${top}px` && !token.getAnimations().some(animation => animation.playState === 'running'), 'Remote token movement did not finish');
        assert(!menu.hidden && !statuses.hidden && !tooltip.hidden, 'Remote movement closed the status menu');
        const destination = token.getBoundingClientRect();
        for (const {element, bounds} of anchored) {
            const moved = element.getBoundingClientRect();
            assert(Math.abs((moved.left - bounds.left) - (destination.left - origin.left)) < 1 &&
                Math.abs((moved.top - bounds.top) - (destination.top - origin.top)) < 1,
                `${element.id} did not follow the remotely moved unit`);
        }
    } finally {
        remote.close();
    }
}

async function testMenuDismissalAndFocus(browser) {
    const {w, d, mouse} = browser;
    const menu = d.getElementById('token-menu');
    const statuses = d.getElementById('token-statuses');
    const escape = () => d.dispatchEvent(new w.KeyboardEvent('keydown', {key: 'Escape', bubbles: true, cancelable: true}));
    escape();
    assert(statuses.hidden && !menu.hidden, 'Escape did not close the status picker first');
    escape();
    assert(menu.hidden, 'Second Escape did not close the token menu');
    for (const [buttonID, panelID, inputID] of [
        ['utility-pen', 'pen-settings', 'pen-color'],
        ['utility-fog', 'fog-settings', 'fog-width'],
        ['utility-fog-eraser', 'fog-settings', 'fog-eraser-width'],
    ]) {
        const button = d.getElementById(buttonID);
        const panel = d.getElementById(panelID);
        button.dispatchEvent(new w.MouseEvent('contextmenu', {bubbles: true, cancelable: true, button: 2}));
        assert(!panel.hidden && button.getAttribute('aria-expanded') === 'true', `${panelID} did not open`);
        assert(d.activeElement.id === inputID, `${panelID} did not focus its input`);
        escape();
        assert(panel.hidden && button.getAttribute('aria-expanded') === 'false', `${panelID} did not close`);
        assert(d.activeElement === button, `${panelID} did not restore focus`);
    }
    const mapButton = d.getElementById('utility-map');
    const mapMenu = d.getElementById('map-options');
    mapButton.click();
    assert(!mapMenu.hidden && d.activeElement.id === 'load-map', 'Map menu did not open and focus its first action');
    mouse(d.body, 'mousedown', 0, 0);
    assert(mapMenu.hidden, 'Outside click did not dismiss the map menu');
    const pen = d.getElementById('utility-pen');
    pen.dispatchEvent(new w.MouseEvent('contextmenu', {bubbles: true, cancelable: true, button: 2}));
    d.dispatchEvent(new w.CustomEvent('toolChanged', {detail: {tool: ''}}));
    assert(d.getElementById('pen-settings').hidden, 'Changing tools did not dismiss settings');
}

function configureDice(d, groups) {
    const rows = d.getElementById('dice-groups');
    while (rows.children.length > groups.length) rows.lastElementChild.querySelector('[data-remove-dice]').click();
    groups.forEach((group, index) => {
        if (!rows.children[index]) d.getElementById('add-dice-group').click();
        const row = rows.children[index];
        row.querySelector('[name="quantity"]').value = group.qty;
        row.querySelector('[name="sides"]').value = group.sides;
        row.querySelector('[name="modifier"]').value = group.modifier;
    });
}

function assertDiceResults(d, groups) {
    const lines = [...d.querySelectorAll('#dice-result .dice-group-result')];
    assert(lines.length === groups.length, 'Not every dice group has a result');
    let total = 0;
    groups.forEach((group, index) => {
        const line = lines[index].textContent;
        const match = line.match(/\[(.*?)\].*= (-?\d+)$/);
        assert(match && line.startsWith(`${group.qty}d${group.sides}`), `Wrong dice group: ${line}`);
        const values = match[1].split(', ').map(Number);
        assert(values.length === group.qty && values.every(value => Number.isInteger(value) && value >= 1 && value <= group.sides), `Invalid dice values: ${line}`);
        const subtotal = values.reduce((sum, value) => sum + value, 0) + group.modifier;
        assert(Number(match[2]) === subtotal, `Group modifier was not applied once: ${line}`);
        total += subtotal;
    });
    assert(d.querySelector('#dice-result strong').textContent === `Total: ${total}`, 'Combined dice total is incorrect');
}

function assertAnimationResults(d, animated) {
    const byType = new Map();
    d.querySelectorAll('#dice-result .dice-group-result').forEach(line => {
        const sides = Number(line.textContent.match(/^\d+d(\d+)/)[1]);
        const add = (type, value) => {
            if (!byType.has(type)) byType.set(type, []);
            byType.get(type).push(value);
        };
        line.textContent.match(/\[(.*?)\]/)[1].split(', ').map(Number).forEach(value => {
            if (sides === 100) {
                add(100, Math.floor(value / 10) % 10 * 10 || 100);
                add(10, value % 10 || 10);
            } else add(sides, value);
        });
    });
    const expected = [...byType].flatMap(([sides, values]) => values.map(value => ({sides, value})));
    const actual = animated.sets.flatMap(group => group.rolls.map(die => ({sides: group.sides, value: die.value})));
    assert(JSON.stringify(actual) === JSON.stringify(expected), `Animation outcomes differ from shared results: ${JSON.stringify(actual)} vs ${JSON.stringify(expected)}`);

}

async function testDiceRoller(browser) {
    const {w, d} = browser;
    const toggle = d.getElementById('toggle-ui-controls');
    const button = d.getElementById('roll-dice');
    const panel = d.getElementById('dice-menu');
    assert(button.getBoundingClientRect().top >= toggle.getBoundingClientRect().bottom, 'Dice button is not below Hide UI');
    toggle.click();
    assert(d.getElementById('ui-overlay').hidden && button.getBoundingClientRect().height === 0, 'Hide UI left the dice controls visible');
    assert(toggle.getBoundingClientRect().height > 0 && toggle.textContent === 'Show UI', 'Show UI is unavailable');
    toggle.click();
    assert(!w.performance.getEntriesByType('resource').some(entry => entry.name.includes('/vendor/dice-box-')), 'Dice animation loaded before opening the roller');
    button.click();
    assert(!panel.hidden && button.getAttribute('aria-expanded') === 'true', 'Dice menu did not open');
    assert(panel.getBoundingClientRect().top >= button.getBoundingClientRect().bottom, 'Dice menu overlaps its button');
    assert(d.activeElement === panel.querySelector('[name="quantity"]'), 'Dice menu did not focus its count');
    assert(d.querySelector('[data-remove-dice]').disabled, 'Only dice group can be removed');
    d.getElementById('add-dice-group').click();
    d.querySelectorAll('[data-remove-dice]')[1].click();
    assert(d.querySelectorAll('.dice-group').length === 1, 'Dice group was not removed');
    const selected = [
        {qty: 2, sides: 4, modifier: 3},
        {qty: 2, sides: 6, modifier: -2},
        {qty: 2, sides: 8, modifier: 0},
        {qty: 2, sides: 10, modifier: 1},
        {qty: 2, sides: 12, modifier: -1},
        {qty: 2, sides: 20, modifier: 5},
        {qty: 2, sides: 100, modifier: 0},
        {qty: 1, sides: 6, modifier: 4},
    ];
    configureDice(d, selected);
    let animated;
    d.addEventListener('rollComplete', event => { animated = event.detail; });
    d.getElementById('roll-all-dice').click();
    assert(panel.getAttribute('aria-busy') === 'true' && d.getElementById('dice-inputs').disabled, 'Dice controls remained active during a roll');
    await until(() => panel.getAttribute('aria-busy') === 'false', 'Dice roll did not finish');
    assertDiceResults(d, selected);
    assert(!animated, 'Dice controls waited for the 3D animation to finish');
    await until(() => animated || d.getElementById('dice-status').textContent.includes('unavailable') ||
        w.matchMedia('(prefers-reduced-motion: reduce)').matches, 'Dice animation did not complete or fall back');
    if (animated) assertAnimationResults(d, animated);
    assert(d.querySelectorAll('.dice-notification').length === 1, 'Origin received duplicate notifications');
    const previousResult = d.getElementById('dice-result').textContent;
    const quantity = d.querySelector('[name="quantity"]');
    quantity.value = '1.5';
    assert(!d.getElementById('dice-form').checkValidity(), 'Fractional dice count was accepted');
    quantity.value = '100';
    d.getElementById('roll-all-dice').click();
    assert(!d.getElementById('dice-error').hidden && d.getElementById('dice-error').textContent.includes('100'), 'Oversized roll was not rejected');
    assert(d.getElementById('dice-result').textContent === previousResult && !d.getElementById('dice-inputs').disabled, 'Invalid roll changed the result or locked controls');
    quantity.value = '2';
    animated = null;
    d.getElementById('roll-all-dice').click();
    await until(() => panel.getAttribute('aria-busy') === 'false', 'Second dice roll did not finish');
    assertDiceResults(d, selected);
    assert(d.getElementById('dice-error').hidden, 'Valid roll did not clear the previous error');
    await until(() => animated || d.getElementById('dice-status').textContent.includes('unavailable') ||
        w.matchMedia('(prefers-reduced-motion: reduce)').matches, 'Second dice animation did not finish');
    if (animated) assertAnimationResults(d, animated);
    const canvas = d.getElementById('dice-layer').querySelector('canvas');
    const animationLoaded = w.performance.getEntriesByType('resource').some(entry => entry.name.includes('/vendor/dice-box-'));
    assert(!animationLoaded || canvas, 'Dice animation failed after loading its module');
    if (canvas) {
        assert(canvas.width > 0 && canvas.height > 0, 'Dice animation canvas has no size');
        assert(d.getElementById('dice-layer').style.opacity === '1', 'Dice animation was not shown');
        await until(() => d.getElementById('dice-layer').style.opacity === '0', 'Dice animation did not fade away');
        await progress('3D dice animation verified');
    } else {
        assert(d.getElementById('dice-status').textContent.includes('unavailable') || w.matchMedia('(prefers-reduced-motion: reduce)').matches, 'Missing dice animation without an explanation');
        await progress('dice fallback verified');
    }
    const large = [{qty: 100, sides: 6, modifier: 3}];
    configureDice(d, large);
    animated = null;
    d.getElementById('roll-all-dice').click();
    await until(() => panel.getAttribute('aria-busy') === 'false', 'Large roll kept dice controls locked');
    assertDiceResults(d, large);
    await until(() => d.getElementById('dice-status').textContent.includes('over 20 dice'), 'Large roll started expensive physics instead of showing results');
    assert(!animated && d.getElementById('dice-layer').style.opacity !== '1', 'Large roll animated despite the visual budget');
    d.dispatchEvent(new w.KeyboardEvent('keydown', {key: 'Escape', bubbles: true, cancelable: true}));
    assert(panel.hidden && d.activeElement === button && button.getAttribute('aria-expanded') === 'false', 'Dice menu did not dismiss and restore focus');
}

async function testDiceWithoutWebGL() {
    const frame = document.createElement('iframe');
    frame.src = '/';
    document.body.append(frame);
    try {
        await until(() => frame.contentDocument?.getElementById('roll-dice'), 'Fallback test page did not load');
        const w = frame.contentWindow;
        const d = frame.contentDocument;
        await until(() => w.document.readyState === 'complete', 'Fallback test scripts did not load');
        await until(() => w.eval('newUnitUpdateSocket.readyState === WebSocket.OPEN'), 'Fallback board socket did not connect');
        assert(!d.getElementById('share-dice-rolls'), 'Player was given a private roll toggle');
        const getContext = w.HTMLCanvasElement.prototype.getContext;
        w.HTMLCanvasElement.prototype.getContext = function(type, ...args) {
            if (type === 'webgl' || type === 'experimental-webgl') return null;
            return getContext.call(this, type, ...args);
        };
        d.getElementById('roll-dice').click();
        const selected = [
            {qty: 2, sides: 6, modifier: 3},
            {qty: 1, sides: 20, modifier: -2},
            {qty: 2, sides: 6, modifier: -1},
        ];
        configureDice(d, selected);
        let replay;
        document.getElementById('app').contentDocument.addEventListener('rollComplete', event => { replay = event.detail; });
        const observer = document.getElementById('app').contentDocument;
        const previousNotifications = observer.querySelectorAll('.dice-notification').length;
        d.getElementById('roll-all-dice').click();
        await until(() => d.getElementById('dice-menu').getAttribute('aria-busy') === 'false', 'Fallback dice did not finish rolling');
        assertDiceResults(d, selected);
        await until(() => observer.querySelectorAll('.dice-notification').length === previousNotifications + 1, 'Other client did not receive a dice notification');
        assert(observer.getElementById('dice-result').textContent === d.getElementById('dice-result').textContent, 'Clients showed different dice results');
        assert(observer.getElementById('dice-menu').hidden, 'Remote roll opened the observer’s menu');
        if (observer.querySelector('#dice-layer canvas')) {
            await until(() => replay, 'Other client did not animate the shared roll');
            assertAnimationResults(observer, replay);
        }
        assert(d.getElementById('dice-status').textContent.includes('unavailable') || w.matchMedia('(prefers-reduced-motion: reduce)').matches, 'Unavailable animations were not explained');
        assert(!d.querySelector('#dice-layer canvas'), 'Fallback unexpectedly created a 3D canvas');
        assert(!w.performance.getEntriesByType('resource').some(entry => entry.name.includes('/vendor/dice-box-')), 'Fallback unnecessarily loaded animation assets');
    } finally {
        frame.remove();
    }
}

async function testPrivateGMRolls(browser) {
    const {w, d} = browser;
    const frame = document.createElement('iframe');
    frame.src = '/';
    document.body.append(frame);
    try {
        await until(() => frame.contentDocument?.readyState === 'complete' &&
            frame.contentDocument.getElementById('roll-dice'), 'Observer did not load');
        await until(() => frame.contentWindow.eval('newUnitUpdateSocket.readyState === WebSocket.OPEN'), 'Observer socket did not connect');
        const observer = frame.contentDocument;
        const events = [];
        observer.addEventListener('diceRolled', event => events.push(event.detail));
        d.getElementById('roll-dice').click();
        const share = d.getElementById('share-dice-rolls');
        assert(share?.checked, 'GM rolls were not public by default');
        share.checked = false;
        share.dispatchEvent(new w.Event('change', {bubbles: true}));
        assert(d.getElementById('dice-visibility-hint').textContent.includes('Only you'), 'Private roll visibility was not explained');
        while (d.querySelectorAll('.dice-group').length > 1) {
            const buttons = d.querySelectorAll('[data-remove-dice]');
            buttons[buttons.length - 1].click();
        }
        const selected = [{qty: 2, sides: 20, modifier: 5}];
        configureDice(d, selected);
        let animated;
        d.addEventListener('rollComplete', event => { animated = event.detail; });
        d.getElementById('roll-all-dice').click();
        await until(() => d.getElementById('dice-menu').getAttribute('aria-busy') === 'false', 'Private GM roll did not finish');
        assertDiceResults(d, selected);
        assert(d.querySelector('#dice-notifications article:last-child > strong').textContent === 'Your private roll', 'GM notification was not marked private');
        if (d.querySelector('#dice-layer canvas')) {
            await until(() => animated, 'Private roll animation did not finish');
            assertAnimationResults(d, animated);
        }
        assert(events.length === 0 && !observer.querySelector('.dice-notification'), 'Private roll leaked to another client');
        assert(!observer.querySelector('#dice-layer canvas'), 'Private roll triggered another client’s animation');
        share.checked = true;
        share.dispatchEvent(new w.Event('change', {bubbles: true}));
        d.getElementById('roll-all-dice').click();
        await until(() => d.getElementById('dice-menu').getAttribute('aria-busy') === 'false', 'Public GM roll did not finish');
        await until(() => events.length > 0, 'GM could not switch back to public rolls');
        assert(events.length === 1 && !events[0].private, 'Observer received a private or duplicate GM roll');
        assert(observer.getElementById('dice-result').textContent === d.getElementById('dice-result').textContent, 'Public GM results differed between clients');
        d.getElementById('roll-dice').click();
    } finally {
        frame.remove();
    }
}

async function testNotificationLifecycle(browser) {
    const {w, d} = browser;
    const container = d.createElement('div');
    const notices = w.eval('notificationUI').create(container, d.getElementById('notification-template'), {limit: 2, duration: 100});
    for (const title of ['First', 'Second', 'Third']) {
        notices.show(title, d.createTextNode('Details'));
    }
    assert(container.children.length === 2 && container.firstElementChild.querySelector('strong').textContent === 'Second', 'Notification limit did not evict the oldest popup');
    container.firstElementChild.querySelector('button').click();
    assert(container.children.length === 1 && container.firstElementChild.querySelector('strong').textContent === 'Third', 'Dismiss button removed the wrong notification');
    await until(() => container.children.length === 0, 'Notification did not expire');
}

async function testLibrarySearch(browser) {
    const {w, d} = browser;
    function search(id, query) {
        const input = d.getElementById(id);
        input.value = query;
        input.dispatchEvent(new w.Event('input', {bubbles: true}));
    }
    const visibleEntries = kind => [...d.querySelectorAll(`[data-library-search="${kind}"] [data-library-name]`)]
        .filter(entry => entry.getClientRects().length > 0);

    d.getElementById('spawn-menu-toggle').click();
    await until(() => d.getElementById('unit-search-results')?.textContent === '4 units', 'Unit search did not initialize');
    const defaults = d.querySelector('#pc-list [data-library-defaults]');
    const owned = d.querySelector('#pc-list [data-library-owned]');
    assert(defaults && !defaults.open && owned?.open, 'Defaults and owned units were not separated into dropdowns');
    assert(defaults.querySelectorAll('[data-library-name]').length === 1 && owned.querySelectorAll('[data-library-name]').length === 1, 'Unit dropdowns mixed owned content with defaults');
    search('unit-library-search', '  KnIgHt  ');
    let entries = visibleEntries('units');
    assert(entries.length === 2 && entries.every(entry => entry.dataset.libraryName === 'Knight'), 'Unit search did not filter both owned and default names case-insensitively');
    assert(defaults.open && d.getElementById('npc-list').hidden && d.querySelector('#pc-list .unit-count').textContent === '2', 'Unit search did not open matching defaults or update groups and counts');
    await w.htmx.ajax('GET', '/spawn-menu?open=true', {target: '#spawn-controls', swap: 'outerHTML'});
    await until(() => d.getElementById('unit-library-search')?.value === '  KnIgHt  ', 'Unit search was lost during menu refresh');
    assert(visibleEntries('units').length === 2, 'Refreshed unit results were not filtered');
    search('unit-library-search', 'nothing-matches-this-name');
    assert(visibleEntries('units').length === 0 && !d.querySelector('[data-library-search="units"] [data-library-search-empty]').hidden, 'Unit search did not show the empty state');
    search('unit-library-search', '');
    assert(d.querySelectorAll('[data-library-search="units"] [data-library-name]:not([hidden])').length === 4 && !d.getElementById('npc-list').hidden, 'Clearing unit search did not restore entries');
    await until(() => !d.querySelector('#spawn-controls.htmx-settling'), 'Unit menu refresh did not settle');
    d.getElementById('spawn-menu-toggle').click();
    await until(() => d.getElementById('spawn-menu').hidden, 'Unit menu did not close');

    d.getElementById('utility-map').click();
    d.getElementById('load-map').click();
    await until(() => d.getElementById('map-search-results')?.textContent === '2 maps', 'Map search did not initialize');
    search('map-library-search', 'OASIS desert');
    entries = visibleEntries('maps');
    assert(entries.length === 1 && entries[0].dataset.libraryName === 'Desert Oasis', 'Map search did not match all name terms');
    assert(d.getElementById('map-search-results').textContent === '1 map found', 'Map search result count is incorrect');
    await w.htmx.ajax('GET', '/map-library?gm=true', {target: '#map-editor', swap: 'innerHTML'});
    await until(() => d.getElementById('map-library-search')?.value === 'OASIS desert', 'Map search was lost during menu refresh');
    assert(visibleEntries('maps').length === 1, 'Refreshed map results were not filtered');
    search('map-library-search', 'nothing-matches-this-name');
    assert(visibleEntries('maps').length === 0 && !d.querySelector('[data-library-search="maps"] [data-library-search-empty]').hidden, 'Map search did not show the empty state');
    search('map-library-search', '');
    assert(visibleEntries('maps').length === 2, 'Clearing map search did not restore entries');
    await until(() => !d.querySelector('#map-editor.htmx-settling'), 'Map menu refresh did not settle');
    d.querySelector('#map-editor [data-close-map-editor]').click();
}

async function testMapViewCentering(browser) {
    const frame = document.getElementById('app');
    const maps = await (await fetch('/browser/maps')).json();
    function centered(w) {
        const d = w.document;
        const map = d.getElementById('map-layer');
        if (!map || map.hidden) return false;
        const bounds = map.getBoundingClientRect();
        return Math.abs(bounds.left + bounds.width / 2 - d.documentElement.clientWidth / 2) < 2 &&
            Math.abs(bounds.top + bounds.height / 2 - d.documentElement.clientHeight / 2) < 2;
    }
    async function load(name) {
        const map = maps.find(map => map.name === name);
        const response = await fetch(`/map-library/${map.id}/load?gm=true`, {method: 'POST'});
        assert(response.ok, 'Map could not load for view test');
        try {
            await until(() => browser.d.getElementById('map-layer').alt === name && centered(browser.w), 'Loaded map was not centered in the viewport');
        } catch (error) {
            const bounds = browser.d.getElementById('map-layer').getBoundingClientRect();
            throw new Error(`${name}: ${error.message}; bounds=${JSON.stringify(bounds.toJSON())}; viewport=${browser.d.documentElement.clientWidth}x${browser.d.documentElement.clientHeight}; scroll=${browser.d.getElementById('board-viewport').scrollLeft},${browser.d.getElementById('board-viewport').scrollTop}; world=${browser.d.getElementById('board-world').style.cssText}`);
        }
        return map;
    }
    const first = await load('Forest Clearing');
    browser.d.getElementById('board-viewport').scrollBy(100, 80);
    const left = browser.d.getElementById('board-viewport').scrollLeft, top = browser.d.getElementById('board-viewport').scrollTop;
    browser.d.dispatchEvent(new browser.w.CustomEvent('boardMapChanged', {detail: {map: first}}));
    await pause(50);
    assert(browser.d.getElementById('board-viewport').scrollLeft === left && browser.d.getElementById('board-viewport').scrollTop === top, 'An update to the same map reset manual panning');
    await browser.remoteSpawn();
    await until(() => browser.units().length === 1, 'Token did not appear on the centered map');
    const token = browser.units()[0];
    const originalLeft = parseInt(token.style.left), originalTop = parseInt(token.style.top);
    const bounds = token.getBoundingClientRect();
    browser.mouse(token, 'mousedown', bounds.left + 5, bounds.top + 5);
    browser.mouse(browser.d, 'mousemove', bounds.left + 45, bounds.top + 40);
    browser.mouse(browser.d, 'mouseup', bounds.left + 45, bounds.top + 40);
    await until(async () => {
        const units = await (await fetch('/browser/state')).json();
        const moved = units.find(unit => unit.ID === token.id);
        return moved?.Left === originalLeft + 40 && moved?.Top === originalTop + 35;
    }, 'Dragging after centering changed shared token coordinates');
    const grid = browser.d.getElementById('grid-container');
    const gridBounds = grid.getBoundingClientRect();
    const point = browser.w.eval('boardGeometry').point(grid, {clientX: gridBounds.left + 200, clientY: gridBounds.top + 200});
    assert(point.x === 5 && point.y === 5, 'Drawing coordinates changed after centering and panning');
    await load('Desert Oasis');

    browser.d.getElementById('board-viewport').scrollBy(85, 65);
    const oldDocument = frame.contentDocument;
    frame.style.width = '360px';
    frame.style.height = '640px';
    frame.src = '/?gm=true';
    try {
        await until(() => frame.contentDocument !== oldDocument && frame.contentDocument?.getElementById('map-layer')?.alt === 'Desert Oasis' && centered(frame.contentWindow), 'Reloaded map was not centered in the smaller viewport');
    } catch (error) {
        const w = frame.contentWindow, d = w.document;
        const map = d.getElementById('map-layer');
        throw new Error(`${error.message}; map=${map?.alt}; hidden=${map?.hidden}; bounds=${JSON.stringify(map?.getBoundingClientRect().toJSON())}; viewport=${d.documentElement.clientWidth}x${d.documentElement.clientHeight}; scroll=${w.document.getElementById('board-viewport').scrollLeft},${w.document.getElementById('board-viewport').scrollTop}; ready=${d.readyState}`);
    }
}

async function testMobileMenus(browser) {
    const {w, d, mouse} = browser;
    assert(!d.querySelector('#grid-container .cell') && !d.getElementById('loadGrid'), 'Board still creates thousands of grid cells');
    const frame = document.getElementById('app');
    frame.style.width = '390px';
    frame.style.height = '844px';
    await until(() => d.getElementById('board-viewport').clientWidth === 390 && d.body.dataset.mobileTools === 'closed', 'Mobile layout did not settle');
    const tools = d.getElementById('mobile-tools-toggle');
    const pen = d.getElementById('utility-pen');
    assert(pen.getBoundingClientRect().height === 0 && tools.getBoundingClientRect().height >= 44, 'Mobile tray did not start collapsed');
    tools.click();
    assert(pen.getBoundingClientRect().height >= 44, 'Mobile tools did not open');
    const grid = d.getElementById('grid-container');
    const bounds = grid.getBoundingClientRect();
    mouse(grid, 'mousemove', bounds.left + 100, bounds.top + 180);
    pen.click();
    assert(d.body.dataset.mobileTools === 'closed', 'Selecting a tool left the tray covering the board');
    assert(d.getElementById('brush-cursor').hidden, 'Selecting Pen reused an old pointer position');
    const drawing = d.getElementById('drawing-layer').getBoundingClientRect();
    assert(Math.abs(drawing.top - bounds.top) < 0.1 && Math.abs(drawing.height - bounds.height) < 0.1, 'Drawing layer differs from grid bounds');
    tools.click();
    d.getElementById('mobile-brush-settings').click();
    const settings = d.getElementById('pen-settings');
    assert(!settings.hidden && settings.getBoundingClientRect().height <= 844 * 0.6 + 1, 'Brush settings are not a bounded mobile sheet');
    settings.querySelector('[data-mobile-close-panel]').click();
    assert(settings.hidden, 'Mobile sheet Close did not work');
    tools.click();
    d.getElementById('spawn-menu-toggle').click();
    await until(() => !d.getElementById('spawn-menu').hidden, 'Mobile spawn menu did not open');
    assert(d.body.dataset.mobileTools === 'closed', 'Mobile spawn menu left the tray open');
    assert(d.getElementById('spawn-menu').getBoundingClientRect().height <= 844 * 0.6 + 1, 'Spawn sheet blocks too much of the board');
    d.getElementById('toggle-ui-controls').click();
    for (const id of ['ui-overlay', 'board-controls', 'mobile-tools-toggle', 'spawn-menu', 'dice-notifications', 'token-menu']) {
        assert(d.getElementById(id).getBoundingClientRect().height === 0, `Hide UI left ${id} visible`);
    }
    assert(!d.body.dataset.tool, 'Hide UI left a drawing tool active');
    d.getElementById('toggle-ui-controls').click();
    await until(() => d.getElementById('spawn-menu').hidden, 'Spawn menu did not stay dismissed');
    d.getElementById('roll-dice').click();
    const dice = d.getElementById('dice-menu');
    assert(!dice.hidden && !d.activeElement.matches('input, select'), 'Opening mobile dice focused a keyboard field');
    assert(dice.parentElement === d.body, 'Dice panel is trapped inside the toolbar stacking context');
    const tap = target => {
        target.scrollIntoView({block: 'nearest'});
        const bounds = target.getBoundingClientRect();
        const x = bounds.left + bounds.width / 2, y = bounds.top + bounds.height / 2;
        const hit = d.elementFromPoint(x, y);
        assert(hit === target || target.contains(hit), `Tap passes through ${target.getAttribute('aria-label') || target.id}`);
        for (const type of ['pointerdown', 'pointerup']) hit.dispatchEvent(new w.PointerEvent(type, {
            pointerId: 99, pointerType: 'touch', isPrimary: true, bubbles: true, cancelable: true,
            button: 0, buttons: type === 'pointerdown' ? 1 : 0, clientX: x, clientY: y,
        }));
        hit.dispatchEvent(new w.MouseEvent('click', {bubbles: true, cancelable: true, clientX: x, clientY: y}));
    };
    const row = d.querySelector('.dice-group');
    const quantity = row.querySelector('[name="quantity"]');
    const modifier = row.querySelector('[name="modifier"]');
    quantity.value = '1';
    modifier.value = '0';
    tap(row.querySelector('[aria-label="Increase dice count"]'));
    assert(quantity.value === '2', 'Mobile count increment failed');
    tap(row.querySelector('[aria-label="Decrease dice count"]'));
    tap(row.querySelector('[aria-label="Decrease dice count"]'));
    assert(quantity.value === '1', 'Mobile count decrement passed the minimum');
    tap(row.querySelector('[aria-label="Decrease dice modifier"]'));
    assert(modifier.value === '-1', 'Mobile modifier cannot go negative');
    tap(row.querySelector('[aria-label="Increase dice modifier"]'));
    assert(modifier.value === '0', 'Mobile modifier increment failed');
    const modifierBounds = modifier.getBoundingClientRect();
    assert(d.elementFromPoint(modifierBounds.left + modifierBounds.width / 2,
        modifierBounds.top + modifierBounds.height / 2) === modifier, 'Modifier is covered by another control');
    w.dispatchEvent(new w.Event('resize'));
    assert(!dice.hidden, 'Phone height change dismissed the dice editor');
    modifier.focus({preventScroll: true});
    d.dispatchEvent(new w.Event('scroll'));
    assert(!dice.hidden && d.activeElement === modifier, 'Keyboard scrolling closed the editor');
    modifier.blur();
    const count = d.querySelectorAll('.dice-group').length;
    d.getElementById('add-dice-group').click();
    assert(d.querySelectorAll('.dice-group').length === count + 1 && !d.activeElement.matches('input, select'), 'Adding mobile dice focused a keyboard field');
    d.querySelector('.dice-group:last-child [data-remove-dice]').click();
    assert(!d.activeElement.matches('input, select'), 'Removing mobile dice focused a keyboard field');
    tap(dice.querySelector('[data-mobile-close-panel]'));
    assert(d.getElementById('dice-backdrop').hidden, 'Closing dice left a shield blocking the UI');
    d.getElementById('roll-dice').click();
    const shield = d.getElementById('dice-backdrop');
    const shieldHit = d.elementFromPoint(100, 100);
    assert(shieldHit === shield, 'Outside popup taps hit the board');
    shield.dispatchEvent(new w.PointerEvent('pointerdown', {pointerId: 99, pointerType: 'touch', isPrimary: true,
        bubbles: true, cancelable: true, button: 0, buttons: 1, clientX: 100, clientY: 100}));
    assert(!shield.hidden && !dice.hidden, 'Dismissal removed the shield before the touch ended');
    shield.dispatchEvent(new w.PointerEvent('pointerup', {pointerId: 99, pointerType: 'touch', isPrimary: true,
        bubbles: true, cancelable: true, button: 0, buttons: 0, clientX: 100, clientY: 100}));
    shield.dispatchEvent(new w.MouseEvent('click', {bubbles: true, cancelable: true, clientX: 100, clientY: 100}));
    assert(shield.hidden && dice.hidden, 'Outside dismissal left the UI blocked');
    d.getElementById('roll-dice').click();
    tap(d.getElementById('toggle-ui-controls'));
    assert(shield.hidden && dice.hidden, 'Hide UI left the popup shield active');
    tap(d.getElementById('toggle-ui-controls'));
    tools.click();
    assert(pen.getBoundingClientRect().height > 0, 'Dice interaction left Tools unusable');
    tools.click();
    frame.style.width = '1200px';
    frame.style.height = '800px';
    await until(() => d.getElementById('board-viewport').clientWidth === 1200 &&
        d.getElementById('board-space').style.getPropertyValue('--board-viewport-width') === '1200px' &&
        pen.getBoundingClientRect().height > 0, 'Desktop layout did not restore');
}

async function testTouchBoardGestures(browser) {
    const {w, d, units} = browser;
    const pointer = (target, type, x, y, id = 7, primary = true) => target.dispatchEvent(new w.PointerEvent(type, {
        pointerId: id, pointerType: 'touch', isPrimary: primary,
        bubbles: true, cancelable: true, button: type === 'pointermove' ? -1 : 0,
        buttons: type === 'pointerup' || type === 'pointercancel' ? 0 : 1,
        clientX: x, clientY: y,
    }));
    const select = tool => {
        if (d.body.dataset.tool !== tool) d.getElementById(`utility-${tool}`).click();
    };
    const gestures = async () => (await fetch('/browser/gestures')).json();
    d.dispatchEvent(new w.KeyboardEvent('keydown', {key: 'Escape', bubbles: true}));
    const token = units()[0];
    const left = parseInt(token.style.left), top = parseInt(token.style.top);
    const bounds = token.getBoundingClientRect();
    assert(w.getComputedStyle(token).touchAction === 'none', 'Token touch scrolling is not disabled');
    pointer(token, 'pointerdown', bounds.left + 10, bounds.top + 10);
    pointer(d, 'pointermove', bounds.left + 44, bounds.top + 32);
    await pause(40);
    const movedBounds = token.getBoundingClientRect();
    assert(Math.abs(movedBounds.left - bounds.left - 34) < 1 && Math.abs(movedBounds.top - bounds.top - 22) < 1,
        'Touch token did not visibly follow finger before release');
    pointer(d, 'pointerup', bounds.left + 44, bounds.top + 32);
    assert(!w.eval('isDragging'), 'Touch drag did not finish');
    await until(async () => {
        const saved = (await (await fetch('/browser/state')).json()).find(unit => unit.ID === token.id);
        return saved.Left === left + 34 && saved.Top === top + 22;
    }, 'Touch drag did not reach server');
    const grid = d.getElementById('grid-container');
    const rectangle = grid.getBoundingClientRect();
    const x = rectangle.left + 120, y = rectangle.top + 120;
    pointer(grid, 'pointerdown', x, y);
    pointer(d, 'pointerup', x, y);
    select('pen');
    assert(w.getComputedStyle(grid).touchAction === 'none', 'Drawing still allows native touch scrolling');
    pointer(grid, 'pointerdown', x, y);
    pointer(d, 'pointermove', x + 80, y + 40);
    pointer(d, 'pointercancel', 0, 0);
    await until(async () => Object.values((await gestures()).strokes).some(stroke => stroke.finished && stroke.points.length === 2), 'Cancelled touch stroke did not finish cleanly');
    assert(d.querySelector('#drawing-layer .pen-stroke'), 'Touch drawing was not rendered');
    select('eraser');
    pointer(grid, 'pointerdown', x, y);
    pointer(d, 'pointerup', x, y);
    await until(async () => Object.keys((await gestures()).strokes).length === 0, 'Touch eraser did not remove stroke');
    select('fog');
    pointer(grid, 'pointerdown', x, y);
    pointer(d, 'pointermove', x + 40, y + 20);
    pointer(d, 'pointerup', x + 40, y + 20);
    await until(async () => (await gestures()).fog?.some(mark => mark.mode === 'paint' && mark.points.length >= 2), 'Touch fog painting did not reach server');
    select('fog-eraser');
    pointer(grid, 'pointerdown', x, y);
    pointer(d, 'pointercancel', 0, 0);
    await until(async () => (await gestures()).fog?.some(mark => mark.mode === 'erase'), 'Touch fog eraser did not reach server');
    select('ruler');
    pointer(grid, 'pointerdown', x, y);
    pointer(d, 'pointermove', x + 80, y);
    pointer(d, 'pointerup', x + 80, y);
    await until(async () => Object.values((await gestures()).rulers).some(ruler => ruler.end.x - ruler.start.x === 2), 'Touch ruler did not reach server');
    d.dispatchEvent(new w.KeyboardEvent('keydown', {key: 'Escape', bubbles: true}));
    assert(w.getComputedStyle(grid).touchAction !== 'none', 'Leaving drawing mode did not restore touch scrolling');
}

async function testRemoteStreaming(browser) {
    const {w, d, units, mouse} = browser;
    await control('positions');
    await until(() => w.eval('pendingUnitPositions.size === 0'), 'Local drops were not acknowledged before streaming');
    const frame = document.createElement('iframe');
    frame.style.cssText = 'position:fixed;top:0;left:0;width:700px;height:500px;z-index:10';
    frame.src = '/?gm=true';
    document.body.append(frame);
    let remote;
    try {
        await until(() => frame.contentDocument?.readyState === 'complete' &&
            frame.contentDocument.querySelector('#unitOverlay .unit'), 'Remote player did not load');
        const observer = frame.contentWindow, view = frame.contentDocument;
        await until(() => observer.eval('newUnitUpdateSocket.readyState === WebSocket.OPEN && posUpdateSocket.readyState === WebSocket.OPEN'), 'Remote sockets did not connect');
        const source = units()[0], token = view.getElementById(source.id);
        const left = parseInt(source.style.left), top = parseInt(source.style.top);
        remote = new w.WebSocket(`ws://${w.location.host}/ws/updateServer`);
        await until(() => remote.readyState === w.WebSocket.OPEN, 'Streaming sender did not connect');
        const samples = [];
        let sampleFrame;
        const sample = () => { samples.push(token.getBoundingClientRect().left); sampleFrame = observer.requestAnimationFrame(sample); };
        sampleFrame = observer.requestAnimationFrame(sample);
        try {
            for (let index = 1; index <= 12; index++) {
                const acknowledged = new Promise(resolve => { remote.onmessage = resolve; });
                remote.send(JSON.stringify({ID: source.id, Left: left + index * 10, Top: top}));
                await acknowledged;
                await control('positions');
                await pause(33);
                if (index === 6) assert(observer.getComputedStyle(token).transitionProperty.includes('left'),
                    'Remote movement is not using CSS transitions');
            }
            await until(() => token.style.left === `${left + 120}px` && !token.getAnimations().some(animation => animation.playState === 'running'), 'Remote movement did not commit its final coordinates');
        } finally { observer.cancelAnimationFrame(sampleFrame); }
        assert(samples.filter((value, index) => index && Math.abs(value - samples[index - 1]) > 0.1).length >= 8,
            'Remote movement arrived as a few jumps instead of display frames');
        assert(samples.every((value, index) => !index || value >= samples[index - 1] - 0.5), 'Remote token moved backwards between consecutive updates');
        const grid = d.getElementById('grid-container'), bounds = grid.getBoundingClientRect();
        const oldPaths = new Set(view.querySelectorAll('#drawing-layer path'));
        let penAnimated = false;
        if (d.body.dataset.tool !== 'pen') d.getElementById('utility-pen').click();
        mouse(grid, 'mousedown', bounds.left + 80, bounds.top + 120);
        for (let index = 1; index <= 32; index++) {
            mouse(d, 'mousemove', bounds.left + 80 + index * 4, bounds.top + 120);
            await pause(4);
            penAnimated ||= [...view.querySelectorAll('#drawing-layer path')].some(path =>
                !oldPaths.has(path) && path.getAnimations().some(animation => animation.playState === 'running'));
        }
        mouse(d, 'mouseup', bounds.left + 208, bounds.top + 120);
        await until(() => [...view.querySelectorAll('#drawing-layer path')].some(path => !oldPaths.has(path) &&
            Math.abs(path.getPointAtLength(path.getTotalLength()).x - 5.2) < 0.001), 'Remote drawing lost or reordered streamed points');
        const path = [...view.querySelectorAll('#drawing-layer path')].find(path => !oldPaths.has(path));
        assert(penAnimated, 'Receiving pen strokes were not smoothly revealed with CSS');
        await until(() => !path.getAnimations().some(animation => animation.playState === 'running'), 'Pen reveal did not finish');
        assert(Math.abs(Number.parseFloat(observer.getComputedStyle(path).strokeDashoffset) - (10000 - path.getTotalLength())) < 0.01,
            'Pen reveal did not finish at the full received stroke');
        d.getElementById('utility-ruler').click();
        mouse(grid, 'mousedown', bounds.left + 80, bounds.top + 80);
        let rulerAnimated = false;
        for (let index = 1; index <= 8; index++) {
            mouse(d, 'mousemove', bounds.left + 80 + index * 10, bounds.top + 80);
            await pause(33);
            rulerAnimated ||= [...view.querySelectorAll('#ruler-layer circle')].some(circle =>
                circle.getAnimations().some(animation => animation.playState === 'running'));
        }
        mouse(d, 'mouseup', bounds.left + 160, bounds.top + 80);
        assert(rulerAnimated, 'Receiving rulers did not use CSS interpolation');
        const end = view.querySelector('#ruler-layer .ruler-remote circle:last-child');
        await until(() => end && !end.getAnimations().some(animation => animation.playState === 'running') &&
            Math.abs(end.getBoundingClientRect().left + end.getBoundingClientRect().width / 2 -
                (view.getElementById('grid-container').getBoundingClientRect().left + 160)) < 0.5,
            'Remote ruler did not finish at the source endpoint');
        d.dispatchEvent(new w.KeyboardEvent('keydown', {key: 'Escape', bubbles: true}));
        await until(() => source.style.left === `${left + 120}px`, 'Source did not settle after remote movement');
    } finally {
        remote?.close();
        frame.remove();
    }
}

async function testBoardZoomKeepsUIFixed(browser) {
    const {w, d, units, mouse} = browser;
    const frame = document.getElementById('app');
    frame.style.width = '390px';
    frame.style.height = '844px';
    await until(() => d.getElementById('board-viewport').clientWidth === 390 &&
        d.getElementById('board-space').style.getPropertyValue('--board-viewport-width') === '390px', 'Phone viewport did not settle');
    d.getElementById('mobile-tools-toggle').click();
    assert(d.getElementById('utility-pen').getBoundingClientRect().width >= 44, 'Mobile utility touch target is too small');
    assert(d.getElementById('board-zoom-in').getBoundingClientRect().height >= 44, 'Mobile zoom touch target is too small');
    assert(parseFloat(w.getComputedStyle(d.querySelector('.dice-group input')).fontSize) >= 16, 'Mobile inputs can trigger focus zoom');

    const zoom = w.eval('boardZoom');
    const grid = d.getElementById('grid-container');
    const button = d.getElementById('mobile-tools-toggle');
    const before = button.getBoundingClientRect();
    zoom.setScale(2);
    assert(Math.abs(grid.getBoundingClientRect().width / 64 - 80) < 0.1, 'Board did not scale to 200%');
    const after = button.getBoundingClientRect();
    assert(before.width === after.width && before.height === after.height, 'Board zoom scaled the UI');
    const token = units()[0];
    const left = parseInt(token.style.left), top = parseInt(token.style.top);
    const bounds = token.getBoundingClientRect();
    mouse(token, 'mousedown', bounds.left + 10, bounds.top + 10);
    mouse(d, 'mousemove', bounds.left + 90, bounds.top + 70);
    mouse(d, 'mouseup', bounds.left + 90, bounds.top + 70);
    await until(async () => {
        const saved = (await (await fetch('/browser/state')).json()).find(unit => unit.ID === token.id);
        return saved.Left === left + 40 && saved.Top === top + 30;
    }, 'Zoomed drag changed shared coordinates');
    const gridBounds = grid.getBoundingClientRect();
    const point = w.eval('boardGeometry').point(grid, {clientX: gridBounds.left + 160, clientY: gridBounds.top + 240});
    assert(point.x === 2 && point.y === 3, 'Drawing geometry did not account for board zoom');
    d.getElementById('utility-pen').click();
    mouse(grid, 'mousedown', gridBounds.left + 160, gridBounds.top + 240);
    mouse(d, 'mousemove', gridBounds.left + 320, gridBounds.top + 320);
    mouse(d, 'mouseup', gridBounds.left + 320, gridBounds.top + 320);
    await until(async () => {
        const strokes = Object.values((await (await fetch('/browser/gestures')).json()).strokes);
        return strokes.some(stroke => stroke.finished && stroke.points[0].x === 2 && stroke.points[0].y === 3 && stroke.points.at(-1).x === 4 && stroke.points.at(-1).y === 4);
    }, 'Zoomed drawing did not preserve shared grid coordinates');
    d.getElementById('utility-ping').click();
    let ping = null;
    d.addEventListener('boardPing', event => { ping = event.detail; }, {once: true});
    grid.dispatchEvent(new w.MouseEvent('click', {bubbles: true, cancelable: true, button: 0,
        clientX: gridBounds.left + 160, clientY: gridBounds.top + 240}));
    await until(() => ping, 'Zoomed ping did not reach browser');
    assert(ping.x === 80 && ping.y === 120, 'Zoomed ping changed shared pixel coordinates');
    d.dispatchEvent(new w.KeyboardEvent('keydown', {key: 'Escape', bubbles: true}));
    const touch = (target, type, x, y, id, primary) => target.dispatchEvent(new w.PointerEvent(type, {
        pointerId: id, pointerType: 'touch', isPrimary: primary,
        bubbles: true, cancelable: true, button: type === 'pointermove' ? -1 : 0,
        buttons: type === 'pointerup' ? 0 : 1, clientX: x, clientY: y,
    }));

    const touchBounds = token.getBoundingClientRect();
    const touchLeft = parseInt(token.style.left), touchTop = parseInt(token.style.top);
    touch(token, 'pointerdown', touchBounds.left + 5, touchBounds.top + 5, 20, true);
    let styleWrites = 0;
    const observer = new w.MutationObserver(records => { styleWrites += records.length; });
    observer.observe(token, {attributes: true, attributeFilter: ['style']});
    for (let i = 1; i <= 20; i++) touch(d, 'pointermove', touchBounds.left + 5 + i * 3, touchBounds.top + 5 + i * 2, 20, true);
    await pause(40);
    observer.disconnect();
    const visual = token.getBoundingClientRect();
    assert(styleWrites <= 1, 'Touch movement caused a style/layout write for every event');
    assert(Math.abs(visual.left - touchBounds.left - 60) < 1 && Math.abs(visual.top - touchBounds.top - 40) < 1,
        'Zoomed touch token did not track the latest finger position');
    touch(d, 'pointerup', touchBounds.left + 65, touchBounds.top + 45, 20, true);
    assert(!token.style.transform, 'Touch drag transform survived release');
    await until(async () => {
        const saved = (await (await fetch('/browser/state')).json()).find(unit => unit.ID === token.id);
        return saved.Left === touchLeft + 30 && saved.Top === touchTop + 20;
    }, 'Accelerated touch drag committed incorrect zoomed coordinates');
    const panLeft = w.document.getElementById('board-viewport').scrollLeft, panTop = w.document.getElementById('board-viewport').scrollTop;
    touch(grid, 'pointerdown', 200, 200, 20, true);
    touch(d, 'pointermove', 160, 165, 20, true);
    touch(d, 'pointerup', 160, 165, 20, true);
    assert(w.document.getElementById('board-viewport').scrollLeft === panLeft + 40 && w.document.getElementById('board-viewport').scrollTop === panTop + 35, 'One-finger board panning failed');
    assert(w.scrollX === 0 && w.scrollY === 0, 'Board panning scrolled the document');
    assert(button.getBoundingClientRect().top === before.top && button.getBoundingClientRect().left === before.left, 'Board panning moved fixed UI');
    const stillLeft = w.document.getElementById('board-viewport').scrollLeft, stillTop = w.document.getElementById('board-viewport').scrollTop;
    touch(grid, 'pointerdown', 200, 200, 20, true);
    touch(d, 'pointermove', 202, 201, 20, true);
    await pause(40);
    assert(w.document.getElementById('board-viewport').scrollLeft === stillLeft && w.document.getElementById('board-viewport').scrollTop === stillTop, 'Resting finger jitter moved the board');
    touch(d, 'pointerup', 202, 201, 20, true);
    w.document.getElementById('board-viewport').scrollTo({left: 0, top: 0, behavior: 'instant'});
    touch(grid, 'pointerdown', 100, 100, 20, true);
    touch(d, 'pointermove', 200, 200, 20, true);
    await pause(40);
    assert(w.document.getElementById('board-viewport').scrollLeft === 0 && w.document.getElementById('board-viewport').scrollTop === 0, 'Edge pan did not clamp');
    touch(d, 'pointermove', 180, 170, 20, true);
    touch(d, 'pointerup', 180, 170, 20, true);
    assert(w.document.getElementById('board-viewport').scrollLeft === 20 && w.document.getElementById('board-viewport').scrollTop === 30, 'Reversing a pan at an edge was stuck');
    w.document.getElementById('board-viewport').scrollTo({left: stillLeft, top: stillTop, behavior: 'instant'});

    touch(grid, 'pointerdown', 150, 200, 20, true);
    touch(grid, 'pointerdown', 250, 200, 21, false);
    const stableLeft = w.document.getElementById('board-viewport').scrollLeft, stableTop = w.document.getElementById('board-viewport').scrollTop;
    touch(d, 'pointermove', 254, 200, 21, false);
    grid.dispatchEvent(new w.WheelEvent('wheel', {bubbles: true, cancelable: true, deltaY: -150, clientX: 200, clientY: 200}));
    assert(zoom.scale === 2, 'Compatibility wheel events competed with touch zoom');
    for (const type of ['gesturestart', 'gesturechange', 'gestureend']) {
        assert(!grid.dispatchEvent(new w.Event(type, {bubbles: true, cancelable: true})), 'Native page pinch was not suppressed on board');
    }
    await pause(40);
    assert(zoom.scale === 2 && w.document.getElementById('board-viewport').scrollLeft === stableLeft && w.document.getElementById('board-viewport').scrollTop === stableTop, 'Minor finger jitter changed the view');
    assert(!d.body.dispatchEvent(new w.Event('gesturechange', {bubbles: true, cancelable: true})),
        'Native gesture on a shared ancestor competed with board touch');
    const world = d.getElementById('board-world');
    const anchor = w.eval('boardGeometry').localPoint(world, {clientX: 200, clientY: 200});
    for (const thumbX of [280, 220, 300, 210, 250]) {
        touch(d, 'pointermove', thumbX, 200, 21, false);
        await pause(40);
        const view = world.getBoundingClientRect();
        assert(Math.abs(view.left + anchor.x * zoom.scale - 200) <= 1 &&
            Math.abs(view.top + anchor.y * zoom.scale - 200) <= 1, 'Pinch round-trip moved its board anchor');
        assert(world.offsetWidth === 2560, 'Pinch round-trip changed grid layout');
    }
    assert(zoom.scale === 2, 'Pinch round-trip failed to return to original zoom');

    touch(d, 'pointermove', 170, 220, 20, true);
    touch(d, 'pointermove', 274, 220, 21, false);
    await pause(40);
    assert(zoom.scale === 2 && w.document.getElementById('board-viewport').scrollLeft === stableLeft &&
        w.document.getElementById('board-viewport').scrollTop === stableTop, 'Thumb midpoint drift panned the map');
    touch(d, 'pointermove', 300, 200, 21, false);
    touch(d, 'pointerup', 150, 200, 20, true);
    const resumedLeft = w.document.getElementById('board-viewport').scrollLeft, resumedTop = w.document.getElementById('board-viewport').scrollTop;
    touch(d, 'pointermove', 280, 175, 21, false);
    touch(d, 'pointerup', 280, 175, 21, false);
    assert(w.document.getElementById('board-viewport').scrollLeft === resumedLeft && w.document.getElementById('board-viewport').scrollTop === resumedTop, 'Lift-off thumb accidentally panned the board');
    assert(zoom.scale > 2 && zoom.scale < 2.5, 'Pinch did not use reduced sensitivity');
    const pinchScale = zoom.scale;
    assert(!d.getElementById('board-world').style.zoom && d.getElementById('board-world').offsetWidth === 2560,
        'Pinch reflowed the grid through CSS zoom');
    assert(button.getBoundingClientRect().width === before.width, 'Pinch scaled UI controls');
    // Fingers placed close together must not produce a large zoom jump.
    touch(grid, 'pointerdown', 200, 200, 20, true);
    touch(grid, 'pointerdown', 210, 200, 21, false);
    touch(d, 'pointermove', 230, 200, 21, false);
    touch(d, 'pointerup', 200, 200, 20, true);
    touch(d, 'pointerup', 230, 200, 21, false);
    assert(zoom.scale === pinchScale, 'Close fingers caused an amplified zoom jump');
    const cancelledLeft = w.document.getElementById('board-viewport').scrollLeft, cancelledTop = w.document.getElementById('board-viewport').scrollTop;
    touch(grid, 'pointerdown', 200, 200, 20, true);
    touch(d, 'pointermove', 180, 175, 20, true);
    touch(d, 'pointercancel', 0, 0, 20, true);
    const stoppedLeft = w.document.getElementById('board-viewport').scrollLeft, stoppedTop = w.document.getElementById('board-viewport').scrollTop;
    touch(d, 'pointermove', 80, 75, 20, true);
    await pause(40);
    assert(w.document.getElementById('board-viewport').scrollLeft === stoppedLeft && w.document.getElementById('board-viewport').scrollTop === stoppedTop, 'Cancelled pan kept moving');
    assert(stoppedLeft === cancelledLeft + 20 && stoppedTop === cancelledTop + 25, 'Cancelling pan applied unrelated coordinates');


    grid.dispatchEvent(new w.WheelEvent('wheel', {bubbles: true, cancelable: true, deltaY: 100, clientX: 200, clientY: 200}));
    assert(zoom.scale < pinchScale, 'Wheel did not zoom board');
    d.getElementById('board-zoom-reset').click();
    assert(zoom.scale === 1, 'Reset zoom did not restore 100%');
}

async function testToolsAcrossZoomLevels(browser) {
    const {w, d, mouse} = browser;
    const zoom = w.eval('boardZoom');
    const viewport = d.getElementById('board-viewport');
    const grid = d.getElementById('grid-container');
    for (const scale of [0.5, 1.25, 2.4, 0.75, 1]) {
        zoom.setScale(scale);
        viewport.scrollBy({left: 27, top: 19, behavior: 'instant'});
        let bounds = grid.getBoundingClientRect();
        const cellX = bounds.width / 64, cellY = bounds.height / 64;
        const start = {x: bounds.left + 2 * cellX, y: bounds.top + 3 * cellY};
        const end = {x: bounds.left + 4 * cellX, y: bounds.top + 3 * cellY};
        if (d.body.dataset.tool !== 'ruler') d.getElementById('utility-ruler').click();
        mouse(grid, 'mousedown', start.x, start.y);
        mouse(d, 'mousemove', end.x, end.y);
        mouse(d, 'mouseup', end.x, end.y);
        const circles = [...d.querySelectorAll('#ruler-layer circle')];
        assert(circles.length >= 2, 'Ruler was not rendered');
        const first = circles[0].getBoundingClientRect(), last = circles[1].getBoundingClientRect();
        assert(Math.abs(first.left + first.width / 2 - start.x) < 1 && Math.abs(first.top + first.height / 2 - start.y) < 1,
            `Ruler start drifted from pointer at ${scale}x`);
        assert(Math.abs(last.left + last.width / 2 - end.x) < 1 && Math.abs(last.top + last.height / 2 - end.y) < 1,
            `Ruler end drifted from pointer at ${scale}x`);
        assert(d.querySelector('#ruler-layer .ruler-distance').textContent === '10 ft', `Ruler distance changed at ${scale}x`);
        d.getElementById('utility-pen').click();
        const oldIDs = Object.keys((await (await fetch('/browser/gestures')).json()).strokes);
        mouse(grid, 'mousedown', start.x, start.y);
        mouse(d, 'mousemove', end.x, end.y);
        mouse(d, 'mouseup', end.x, end.y);
        await until(async () => {
            const strokes = (await (await fetch('/browser/gestures')).json()).strokes;
            return Object.entries(strokes).some(([id, stroke]) => !oldIDs.includes(id) && stroke.finished &&
                stroke.points[0].x === 2 && stroke.points[0].y === 3 && stroke.points.at(-1).x === 4 && stroke.points.at(-1).y === 3);
        }, `Drawing coordinates drifted at ${scale}x`);
        const path = d.querySelector('#drawing-layer path:last-child');
        // A horizontal round-capped stroke extends half its height past each endpoint.
        // Use the rendered box; SVG screen matrices can omit inherited CSS zoom.
        const drawing = path.getBoundingClientRect();
        const radius = drawing.height / 2;
        assert(Math.abs(drawing.left + radius - start.x) < 1 && Math.abs(drawing.right - radius - end.x) < 1 &&
            Math.abs(drawing.top + radius - start.y) < 1,
            `Rendered drawing missed pointer at ${scale}x: ${JSON.stringify(drawing.toJSON())}`);
    }
    d.dispatchEvent(new w.KeyboardEvent('keydown', {key: 'Escape', bubbles: true}));
}

async function testAutomaticReconnect({w, d, units, remoteSpawn}) {
    const oldSockets = w.eval('[posUpdateSocket, newUnitUpdateSocket, updateServerSocket]');
    const count = units().length;
    let resynced = false;
    d.addEventListener('boardResync', () => { resynced = true; }, {once: true});
    // A local-only stroke must disappear when the authoritative snapshot returns.
    d.dispatchEvent(new w.CustomEvent('penStroke', {detail: {
        id: 'stale-offline-stroke', owner: 'offline', offset: 0,
        points: [{x: 1, y: 1}, {x: 2, y: 2}], finished: true,
    }}));
    const stalePath = d.querySelector('#drawing-layer path:last-child');
    assert(stalePath, 'Reconnect fixture was not drawn');
    oldSockets.forEach(socket => socket.close());
    await until(() => oldSockets.every(socket => socket.readyState === w.WebSocket.CLOSED), 'Sockets did not close');
    await remoteSpawn();
    await until(() => resynced && w.eval('[posUpdateSocket, newUnitUpdateSocket, updateServerSocket]')
        .every((socket, i) => socket !== oldSockets[i] && socket.readyState === w.WebSocket.OPEN), 'Sockets did not reconnect automatically');
    await until(() => units().length === count + 1, 'Reconnect missed a unit created while offline');
    assert(!stalePath.isConnected, 'Reconnect retained stale drawing');
    const shared = await (await fetch('/browser/gestures')).json();
    await until(() => d.querySelectorAll('#drawing-layer path').length === Object.keys(shared.strokes).length,
        'Reconnect did not restore the shared drawings');
    assert(document.getElementById('app').contentDocument === d, 'Reconnect reloaded the page');
    // Verify the replacement send socket still moves units on the server.
    const token = units()[0];
    w.eval('updateServerSocket').send(JSON.stringify({ID: token.id, Left: 123, Top: 234}));
    await until(async () => (await (await fetch('/browser/state')).json()).some(unit =>
        unit.ID === token.id && unit.Left === 123 && unit.Top === 234), 'Replacement movement socket did not send');
}

async function testFogBoardActions({w, d, units}) {
    const max = d.getElementById('fog-width').max;
    assert(d.getElementById('pen-width').max === max && d.getElementById('fog-eraser-width').max === max,
        'Brush maximum thickness does not match fog');
    const canvas = d.getElementById('fog-layer');
    const context = canvas.getContext('2d');
    const alpha = (x, y) => context.getImageData(x, y, 1, 1).data[3];
    const covered = () => [[0, 0], [2559, 0], [0, 2559], [2559, 2559], [1280, 1280]]
        .every(([x, y]) => alpha(x, y) === 255);
    const count = units().length;
    const openFogMenu = () => d.getElementById('utility-fog').dispatchEvent(new w.MouseEvent('contextmenu', {bubbles: true, cancelable: true}));
    openFogMenu();
    assert(!d.getElementById('fog-settings').hidden, 'Fog right-click menu did not open');
    d.getElementById('fog-cover-board').click();
    await until(covered, 'Cover board did not fill all fog pixels');
    let sessionReturned = false;
    d.addEventListener('rulerSession', () => { sessionReturned = true; }, {once: true});
    w.eval('newUnitUpdateSocket').close();
    await until(() => sessionReturned && covered(), 'Full fog was lost on reconnect');
    w.eval('newUnitUpdateSocket').send(JSON.stringify({type: 'fogMark', id: '90001', mode: 'erase',
        width: 6, points: [{x: 32, y: 32}], generation: w.eval('boardGeneration')}));
    await until(() => alpha(1280, 1280) === 0 && alpha(0, 0) === 255, 'Maximum-size eraser did not cut through full fog');
    openFogMenu();
    d.getElementById('fog-clear-board').click();
    await until(() => [[0, 0], [2559, 0], [0, 2559], [2559, 2559], [1280, 1280]]
        .every(([x, y]) => alpha(x, y) === 0), 'Remove all fog did not clear the board');
    assert(units().length === count, 'Fog action removed units');
}

// These scenarios share one board: each spawn changes the count expected by
// the next scenario, and the movement check leaves the status menu open.
const scenarios = [
    ['drag with remote spawn', testRemoteSpawnDuringDrag],
    ['spawn response ordering', testSpawnResponseOrdering],
    ['unit refresh ordering', testUnitRefreshOrdering],
    ['unit refresh during drag', testUnitRefreshDuringDrag],
    ['status menu follows remote movement', testStatusMenuFollowsRemoteMovement],
    ['shared menu dismissal and focus', testMenuDismissalAndFocus],
    ['dice roller', testDiceRoller],
    ['dice without WebGL', testDiceWithoutWebGL],
    ['private GM rolls', testPrivateGMRolls],
    ['notification lifecycle', testNotificationLifecycle],
    ['unit and map library search', testLibrarySearch],
    ['mobile menus and drawing bounds', testMobileMenus],
    ['touch board gestures', testTouchBoardGestures],
    ['remote player movement and drawing streams', testRemoteStreaming],
    ['board zoom keeps UI fixed', testBoardZoomKeepsUIFixed],
    ['tools across zoom levels', testToolsAcrossZoomLevels],
    ['automatic reconnect and resync', testAutomaticReconnect],
    ['fog thickness and whole board actions', testFogBoardActions],
    ['map view centers on load and reload', testMapViewCentering],
];

async function runBrowserTests() {
    let failure = '';
    let stage = 'browser setup';
    try {
        const browser = await connectBrowser();
        for (const [name, test] of scenarios) {
            stage = name;
            await progress(stage);
            await test(browser);
        }
    } catch (error) {
        failure = `${stage}: ${error.message || error}\n${error.stack || ''}`;
    }
    await fetch('/browser/result', {method: 'POST', headers: {'Content-Type': 'application/json'}, body: JSON.stringify({error: failure})});
}

runBrowserTests();
