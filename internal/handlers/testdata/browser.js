(async () => {
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
    let failure = '';
    try {
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
            assert(new Set(units().map(unit => unit.id)).size === count, 'Duplicate token IDs');
        }
        await progress('drag with remote spawn');
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
        await progress('spawn response ordering');
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
        await progress('snapshot ordering');
        // An old snapshot must be followed by the queued, newer snapshot.
        await control('arm', 'snapshot');
        w.htmx.trigger('#loadExistingUnits', 'loadUnits');
        await control('ready');
        await remoteSpawn();
        await control('release');
        await expectCount(4);
        await progress('snapshot during drag');
        // A snapshot requested before dragging must not interrupt that drag.
        await control('arm', 'snapshot');
        w.htmx.trigger('#loadExistingUnits', 'loadUnits');
        await control('ready');
        const held = startDrag();
        await control('release');
        await pause(100);
        assert(held.isConnected && held.classList.contains('dragging'), 'In-flight snapshot replaced the dragged token');
        endDrag();
        await until(() => !held.isConnected, 'Deferred snapshot was never refreshed');
        await expectCount(4);
    } catch (error) {
        failure = error.stack || String(error);
    }
    await fetch('/browser/result', {method: 'POST', headers: {'Content-Type': 'application/json'}, body: JSON.stringify({error: failure})});
})();
