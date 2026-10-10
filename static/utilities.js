(() => {
    const pingButton = document.getElementById('utility-ping');
    const grid = document.getElementById('grid-container');
    const layer = document.getElementById('ping-layer');
    const error = document.getElementById('utility-error');

    const rulerButton = document.getElementById('utility-ruler');
    const rulerLayer = document.getElementById('ruler-layer');
    const rulers = new Map();
    let rulerID = null;
    let measuring = false;
    let lastPointer = null;
    let sendTimer = null;
    let animationFrame = null;
    const rulerSendInterval = 33;
    const rulerBlendDuration = 50;

    function sendRuler(clear = false) {
        clearTimeout(sendTimer);
        sendTimer = null;
        if (!rulerID) return;
        const measurement = rulers.get(rulerID);
        if (!clear && !measurement) return;
        if (newUnitUpdateSocket.readyState !== WebSocket.OPEN) {
            error.textContent = 'Could not share ruler. Refresh to reconnect.';
            error.hidden = false;
            return;
        }
        const message = {type: 'rulerChanged', clear};
        if (!clear) {
            message.start = measurement.start;
            message.end = measurement.end;
        }
        newUnitUpdateSocket.send(JSON.stringify(message));
    }

    function queueRuler() {
        if (!sendTimer) sendTimer = setTimeout(() => sendRuler(), rulerSendInterval);
    }

    function removeRuler(id) {
        rulers.get(id)?.element?.remove();
        rulers.delete(id);
        if (![...rulers.values()].some(measurement => measurement.transition)) {
            cancelAnimationFrame(animationFrame);
            animationFrame = null;
        }
    }

    function selectTool(tool) {
        if (rulers.has(rulerID)) {
            sendRuler(true);
            removeRuler(rulerID);
        }
        document.body.dataset.tool = tool;
        pingButton.setAttribute('aria-pressed', String(tool === 'ping'));
        rulerButton.setAttribute('aria-pressed', String(tool === 'ruler'));
        error.hidden = true;
        measuring = false;
    }

    document.getElementById('utilities-controls').addEventListener('click', event => {
        const button = event.target.closest('button');
        if (!button) return;
        const tool = button === pingButton ? 'ping' : button === rulerButton ? 'ruler' : '';
        selectTool(document.body.dataset.tool === tool ? '' : tool);
    });
    document.addEventListener('keydown', event => {
        if (event.key === 'Escape') selectTool('');
    });

    function cellSize() {
        return grid.querySelector('.cell')?.getBoundingClientRect();
    }

    function boardPoint(event) {
        const bounds = grid.getBoundingClientRect();
        const cell = cellSize();
        if (!cell?.width || !cell?.height) return null;
        return {
            x: Math.max(0, Math.min(bounds.width, event.clientX - bounds.left)) / cell.width,
            y: Math.max(0, Math.min(bounds.height, event.clientY - bounds.top)) / cell.height,
        };
    }

    function drawMeasurement(measurement, cell = cellSize()) {
        if (!cell?.width || !cell?.height) return;
        if (!measurement.element) {
            const element = document.createElement('div');
            element.innerHTML = '<svg class="ruler-drawing" aria-hidden="true" focusable="false"><line></line><circle r="4"></circle><circle r="4"></circle></svg><span class="ruler-distance"></span>';
            rulerLayer.append(element);
            measurement.element = element;
            const [first, last] = element.querySelectorAll('circle');
            measurement.parts = {line: element.querySelector('line'), first, last,
                label: element.querySelector('.ruler-distance')};
        }
        const {start, parts} = measurement;
        const end = measurement.displayEnd || measurement.end;
        const x1 = start.x * cell.width, y1 = start.y * cell.height;
        const x2 = end.x * cell.width, y2 = end.y * cell.height;
        const {line, first, last, label} = parts;
        line.setAttribute('x1', x1);
        line.setAttribute('y1', y1);
        line.setAttribute('x2', x2);
        line.setAttribute('y2', y2);
        first.setAttribute('cx', x1);
        first.setAttribute('cy', y1);
        last.setAttribute('cx', x2);
        last.setAttribute('cy', y2);
        const feet = Math.hypot(end.x - start.x, end.y - start.y) * 5;
        label.textContent = `${Number(feet.toFixed(1))} ft`;
        label.style.left = `${(x1 + x2) / 2}px`;
        label.style.top = `${(y1 + y2) / 2}px`;
    }

    function animateRulers(now) {
        animationFrame = null;
        let running = false;
        const cell = cellSize();
        for (const measurement of rulers.values()) {
            const transition = measurement.transition;
            if (!transition) continue;
            const progress = movementMath.progress(now, transition.started, rulerBlendDuration);
            measurement.displayEnd = {
                x: movementMath.interpolate(transition.from.x, measurement.end.x, progress),
                y: movementMath.interpolate(transition.from.y, measurement.end.y, progress),
            };
            drawMeasurement(measurement, cell);
            if (progress === 1) {
                measurement.displayEnd = measurement.end;
                delete measurement.transition;
            } else {
                running = true;
            }
        }
        if (running) animationFrame = requestAnimationFrame(animateRulers);
    }

    document.addEventListener('rulerSession', event => { rulerID = event.detail.id; });
    document.addEventListener('rulerChanged', event => {
        const {id, start, end, clear} = event.detail;
        // Local drawing responds immediately; echoed older updates must not rewind it.
        if (id === rulerID) return;
        if (clear) {
            removeRuler(id);
            return;
        }
        const measurement = rulers.get(id) || {};
        const sameStart = measurement.start?.x === start.x && measurement.start?.y === start.y;
        if (sameStart) {
            measurement.transition = {from: {...(measurement.displayEnd || measurement.end)},
                started: performance.now()};
        } else {
            // A new measurement should appear at its origin rather than sweep across the board.
            measurement.displayEnd = end;
            delete measurement.transition;
        }
        measurement.start = start;
        measurement.end = end;
        rulers.set(id, measurement);
        drawMeasurement(measurement);
        if (measurement.transition && animationFrame === null) {
            animationFrame = requestAnimationFrame(animateRulers);
        }
    });
    document.addEventListener('rulerDisconnected', () => {
        clearTimeout(sendTimer);
        sendTimer = null;
        measuring = false;
        rulerID = null;
        for (const id of rulers.keys()) removeRuler(id);
    });

    document.addEventListener('mousedown', event => {
        if (document.body.dataset.tool !== 'ruler' || event.button !== 0) return;
        if (!event.target.closest('#grid-container, #unitOverlay .unit')) return;
        event.preventDefault();
        if (!rulerID || newUnitUpdateSocket.readyState !== WebSocket.OPEN) {
            error.textContent = 'Could not share ruler. Refresh to reconnect.';
            error.hidden = false;
            return;
        }
        const start = boardPoint(event);
        if (!start) return;
        removeRuler(rulerID);
        const measurement = {start, end: start};
        rulers.set(rulerID, measurement);
        measuring = true;
        lastPointer = {clientX: event.clientX, clientY: event.clientY};
        drawMeasurement(measurement);
        sendRuler();
    });
    document.addEventListener('mousemove', event => {
        if (!measuring) return;
        lastPointer = {clientX: event.clientX, clientY: event.clientY};
        const end = boardPoint(event);
        if (!end) return;
        const measurement = rulers.get(rulerID);
        measurement.end = end;
        drawMeasurement(measurement);
        queueRuler();
    });
    document.addEventListener('mouseup', event => {
        if (!measuring || event.button !== 0) return;
        const measurement = rulers.get(rulerID);
        measurement.end = boardPoint(event) || measurement.end;
        drawMeasurement(measurement);
        measuring = false;
        sendRuler();
    });
    window.addEventListener('blur', () => {
        if (measuring) sendRuler();
        measuring = false;
    });
    function refreshMeasurement() {
        if (measuring && lastPointer) {
            const measurement = rulers.get(rulerID);
            measurement.end = boardPoint(lastPointer) || measurement.end;
            queueRuler();
        }
        for (const measurement of rulers.values()) drawMeasurement(measurement);
    }
    document.addEventListener('scroll', refreshMeasurement, true);
    window.addEventListener('resize', refreshMeasurement);
    document.addEventListener('htmx:afterSwap', refreshMeasurement);

    document.addEventListener('click', async event => {
        if (document.body.dataset.tool !== 'ping' || event.button !== 0) return;
        if (!event.target.closest('#grid-container, #unitOverlay .unit')) return;
        const bounds = grid.getBoundingClientRect();
        const x = Math.round(event.clientX - bounds.left);
        const y = Math.round(event.clientY - bounds.top);
        if (x < 0 || y < 0 || x >= bounds.width || y >= bounds.height) return;
        try {
            if (newUnitUpdateSocket.readyState !== WebSocket.OPEN) throw new Error('Disconnected');
            const response = await fetch('/board/ping', {
                method: 'POST',
                headers: {'Content-Type': 'application/json'},
                body: JSON.stringify({x, y}),
            });
            if (!response.ok) throw new Error('Ping failed');
            error.hidden = true;
        } catch {
            error.textContent = 'Could not send ping. Try again.';
            error.hidden = false;
        }
    });

    document.addEventListener('boardPing', event => {
        const {x, y} = event.detail;
        if (!Number.isFinite(x) || !Number.isFinite(y) || x < 0 || y < 0) return;
        const dot = document.createElement('span');
        dot.className = 'board-ping';
        dot.style.left = `${x}px`;
        dot.style.top = `${y}px`;
        layer.append(dot);
        // Timeout also cleans up when reduced-motion preferences disable blinking.
        setTimeout(() => dot.remove(), 2700);
    });
})();
