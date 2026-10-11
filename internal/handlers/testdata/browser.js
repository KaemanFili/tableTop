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

// These scenarios share one board: each spawn changes the count expected by
// the next scenario, and the movement check leaves the status menu open.
const scenarios = [
    ['drag with remote spawn', testRemoteSpawnDuringDrag],
    ['spawn response ordering', testSpawnResponseOrdering],
    ['unit refresh ordering', testUnitRefreshOrdering],
    ['unit refresh during drag', testUnitRefreshDuringDrag],
    ['status menu follows remote movement', testStatusMenuFollowsRemoteMovement],
    ['shared menu dismissal and focus', testMenuDismissalAndFocus],
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
