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
    const mouse = (target, type, x, y) => target.dispatchEvent(new w.MouseEvent(type, {
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
            throw new Error(`${name}: ${error.message}; bounds=${JSON.stringify(bounds.toJSON())}; viewport=${browser.d.documentElement.clientWidth}x${browser.d.documentElement.clientHeight}; scroll=${browser.w.scrollX},${browser.w.scrollY}; world=${browser.d.getElementById('board-world').style.cssText}`);
        }
        return map;
    }
    const first = await load('Forest Clearing');
    browser.w.scrollBy(100, 80);
    const left = browser.w.scrollX, top = browser.w.scrollY;
    browser.d.dispatchEvent(new browser.w.CustomEvent('boardMapChanged', {detail: {map: first}}));
    await pause(50);
    assert(browser.w.scrollX === left && browser.w.scrollY === top, 'An update to the same map reset manual panning');
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

    browser.w.scrollBy(85, 65);
    const oldDocument = frame.contentDocument;
    frame.style.width = '360px';
    frame.style.height = '640px';
    frame.src = '/?gm=true';
    try {
        await until(() => frame.contentDocument !== oldDocument && frame.contentDocument?.getElementById('map-layer')?.alt === 'Desert Oasis' && centered(frame.contentWindow), 'Reloaded map was not centered in the smaller viewport');
    } catch (error) {
        const w = frame.contentWindow, d = w.document;
        const map = d.getElementById('map-layer');
        throw new Error(`${error.message}; map=${map?.alt}; hidden=${map?.hidden}; bounds=${JSON.stringify(map?.getBoundingClientRect().toJSON())}; viewport=${d.documentElement.clientWidth}x${d.documentElement.clientHeight}; scroll=${w.scrollX},${w.scrollY}; ready=${d.readyState}`);
    }
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
