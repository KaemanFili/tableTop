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
    function startDrag() {
        const token = units()[0];
        const bounds = token.getBoundingClientRect();
        mouse(token, 'mousedown', bounds.left + 10, bounds.top + 10);
        mouse(d, 'mousemove', bounds.left + 50, bounds.top + 60);
        assert(dragging(), 'Token drag did not start');
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
    const dragged = startDrag();
    const left = dragged.style.left, top = dragged.style.top;
    await remoteSpawn();
    await pause(100);
    assert(dragged.isConnected && units().length === 1, 'Refresh replaced an active drag');
    endDrag();
    await expectCount(2);
    await until(async () => {
        const saved = await (await fetch('/browser/state')).json();
        const token = saved.find(unit => unit.ID === dragged.id);
        return token.Left === parseInt(left) && token.Top === parseInt(top);
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
    const held = startDrag();
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
        await until(() => token.style.left === `${left}px` && token.style.top === `${top}px`, 'Remote token movement did not finish');
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
    assert(d.getElementById('ui-overlay').hidden && button.getBoundingClientRect().height > 0, 'Dice roller unavailable with UI hidden');
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
    d.getElementById('roll-all-dice').click();
    await until(() => panel.getAttribute('aria-busy') === 'false', 'Second dice roll did not finish');
    assertDiceResults(d, selected);
    assert(d.getElementById('dice-error').hidden, 'Valid roll did not clear the previous error');
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
    d.dispatchEvent(new w.KeyboardEvent('keydown', {key: 'Escape', bubbles: true, cancelable: true}));
    assert(panel.hidden && d.activeElement === button && button.getAttribute('aria-expanded') === 'false', 'Dice menu did not dismiss and restore focus');
    toggle.click();
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
        if (d.querySelector('#dice-layer canvas')) assertAnimationResults(d, animated);
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

async function testBoardZoomKeepsUIFixed(browser) {
    const {w, d, units, mouse} = browser;
    const frame = document.getElementById('app');
    frame.style.width = '390px';
    frame.style.height = '844px';
    await until(() => d.getElementById('board-viewport').clientWidth === 390 &&
        d.getElementById('board-space').style.getPropertyValue('--board-viewport-width') === '390px', 'Phone viewport did not settle');
    assert(d.getElementById('utility-pen').getBoundingClientRect().width >= 44, 'Mobile utility touch target is too small');
    assert(d.getElementById('board-zoom-in').getBoundingClientRect().height >= 44, 'Mobile zoom touch target is too small');
    assert(parseFloat(w.getComputedStyle(d.querySelector('.dice-group input')).fontSize) >= 16, 'Mobile inputs can trigger focus zoom');

    const zoom = w.eval('boardZoom');
    const grid = d.getElementById('grid-container');
    const button = d.getElementById('spawn-menu-toggle');
    const before = button.getBoundingClientRect();
    zoom.setScale(2);
    assert(Math.abs(grid.querySelector('.cell').getBoundingClientRect().width - 80) < 0.1, 'Board did not scale to 200%');
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
    await pause(40);
    assert(zoom.scale === 2 && w.document.getElementById('board-viewport').scrollLeft === stableLeft && w.document.getElementById('board-viewport').scrollTop === stableTop, 'Minor finger jitter changed the view');
    touch(d, 'pointermove', 300, 200, 21, false);
    touch(d, 'pointerup', 150, 200, 20, true);
    const resumedLeft = w.document.getElementById('board-viewport').scrollLeft, resumedTop = w.document.getElementById('board-viewport').scrollTop;
    touch(d, 'pointermove', 280, 175, 21, false);
    touch(d, 'pointerup', 280, 175, 21, false);
    assert(w.document.getElementById('board-viewport').scrollLeft === resumedLeft + 20 && w.document.getElementById('board-viewport').scrollTop === resumedTop + 25, 'Remaining finger did not resume panning after pinch');
    assert(zoom.scale > 2 && zoom.scale < 2.5, 'Pinch did not use reduced sensitivity');
    const pinchScale = zoom.scale;
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
    ['touch board gestures', testTouchBoardGestures],
    ['board zoom keeps UI fixed', testBoardZoomKeepsUIFixed],
    ['tools across zoom levels', testToolsAcrossZoomLevels],
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
