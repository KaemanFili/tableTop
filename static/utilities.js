(() => {
    const pingButton = document.getElementById('utility-ping');
    const grid = document.getElementById('grid-container');
    const layer = document.getElementById('ping-layer');
    const error = document.getElementById('utility-error');

    const rulerButton = document.getElementById('utility-ruler');
    const penButton = document.getElementById('utility-pen');
    const eraserButton = document.getElementById('utility-eraser');
    const fogButton = document.getElementById('utility-fog');
    const fogEraserButton = document.getElementById('utility-fog-eraser');
    const rulerLayer = document.getElementById('ruler-layer');
    const rulers = new Map();
    let rulerID = null;
    let measuring = false;
    let lastPointer = null;
    let sendTimer = null;
    const rulerSendInterval = 33;

    function sendRuler(clear = false) {
        clearTimeout(sendTimer);
        sendTimer = null;
        if (!rulerID) return;
        const measurement = rulers.get(rulerID);
        if (!clear && !measurement) return;
        if (newUnitUpdateSocket.readyState !== WebSocket.OPEN) {
            error.textContent = 'Could not share ruler. Reconnecting automatically; try again shortly.';
            error.hidden = false;
            return;
        }
        const message = {type: 'rulerChanged', clear, generation: boardGeneration};
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

    }

    function selectTool(tool) {
        if (rulers.has(rulerID)) {
            sendRuler(true);
            removeRuler(rulerID);
        }
        document.body.dataset.tool = tool;
        pingButton.setAttribute('aria-pressed', String(tool === 'ping'));
        rulerButton.setAttribute('aria-pressed', String(tool === 'ruler'));
        penButton.setAttribute('aria-pressed', String(tool === 'pen'));
        eraserButton.setAttribute('aria-pressed', String(tool === 'eraser'));
        fogButton?.setAttribute('aria-pressed', String(tool === 'fog'));
        fogEraserButton?.setAttribute('aria-pressed', String(tool === 'fog-eraser'));
        error.hidden = true;
        measuring = false;
        document.dispatchEvent(new CustomEvent('toolChanged', {detail: {tool}}));
    }

    document.getElementById('utilities-controls').addEventListener('click', event => {
        const button = event.target.closest('.utility-button');
        if (!button) return;
        const tool = button === pingButton ? 'ping' : button === rulerButton ? 'ruler' : button === penButton ? 'pen' : button === eraserButton ? 'eraser' : '';
        selectTool(document.body.dataset.tool === tool ? '' : tool);
    });
    document.getElementById('gm-controls')?.addEventListener('click', event => {
        const button = event.target.closest('.utility-button');
        if (!button || ![fogButton, fogEraserButton].includes(button)) return;
        const tool = button === fogButton ? 'fog' : 'fog-eraser';
        selectTool(document.body.dataset.tool === tool ? '' : tool);
    });
    document.addEventListener('uiVisibilityChanged', event => { if (!event.detail.visible) selectTool(''); });
    document.addEventListener('keydown', event => {
        if (event.key === 'Escape') selectTool('');
    });

    function cellSize() {
        return boardGeometry.localCellSize(grid);
    }

    function boardPoint(event) {
        return boardGeometry.point(grid, event);
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
        const end = measurement.end;
        const x1 = start.x * cell.width, y1 = start.y * cell.height;
        const x2 = end.x * cell.width, y2 = end.y * cell.height;
        const {line, first, last, label} = parts;
        measurement.element.classList.toggle('ruler-remote', Boolean(measurement.remote));
        line.setAttribute('x1', 0);
        line.setAttribute('y1', 0);
        line.setAttribute('x2', 1);
        line.setAttribute('y2', 0);
        line.setAttribute('vector-effect', 'non-scaling-stroke');
        const length = Math.max(0.001, Math.hypot(x2 - x1, y2 - y1));
        const angle = Math.atan2(y2 - y1, x2 - x1) * 180 / Math.PI;
        line.style.transform = `translate(${x1}px, ${y1}px) rotate(${angle}deg) scaleX(${length})`;
        first.style.transform = `translate(${x1}px, ${y1}px)`;
        last.style.transform = `translate(${x2}px, ${y2}px)`;
        const feet = Math.hypot(end.x - start.x, end.y - start.y) * 5;
        label.textContent = `${Number(feet.toFixed(1))} ft`;
        label.style.left = `${(x1 + x2) / 2}px`;
        label.style.top = `${(y1 + y2) / 2}px`;
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
        measurement.remote = sameStart;
        measurement.start = start;
        measurement.end = end;
        rulers.set(id, measurement);
        drawMeasurement(measurement);

    });
    document.addEventListener('rulerDisconnected', () => {
        clearTimeout(sendTimer);
        sendTimer = null;
        measuring = false;
        rulerID = null;
        for (const id of rulers.keys()) removeRuler(id);
    });
    document.addEventListener('boardCleared', () => {
        clearTimeout(sendTimer);
        sendTimer = null;
        measuring = false;
        lastPointer = null;
        for (const id of rulers.keys()) removeRuler(id);
        document.body.dataset.tool = '';
        for (const button of [pingButton, rulerButton, penButton, eraserButton, fogButton, fogEraserButton]) {
            button?.setAttribute('aria-pressed', 'false');
        }
        layer.replaceChildren();
        error.hidden = true;
    });

    boardInput.addEventListener('down', event => {
        if (document.body.dataset.tool !== 'ruler' || event.button !== 0) return;
        if (!event.target.closest('#grid-container, #unitOverlay .unit')) return;
        event.preventDefault();
        if (!rulerID || newUnitUpdateSocket.readyState !== WebSocket.OPEN) {
            error.textContent = 'Could not share ruler. Reconnecting automatically; try again shortly.';
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
    boardInput.addEventListener('move', event => {
        if (!measuring) return;
        lastPointer = {clientX: event.clientX, clientY: event.clientY};
        const end = boardPoint(event);
        if (!end) return;
        const measurement = rulers.get(rulerID);
        measurement.end = end;
        drawMeasurement(measurement);
        queueRuler();
    });
    boardInput.addEventListener('up', event => {
        if (!measuring || event.button !== 0) return;
        const measurement = rulers.get(rulerID);
        measurement.end = boardPoint(event) || measurement.end;
        drawMeasurement(measurement);
        measuring = false;
        sendRuler();
    });
    boardInput.addEventListener('cancel', () => {
        if (measuring) sendRuler();
        measuring = false;
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
    document.addEventListener('viewZoomChanged', refreshMeasurement);
    document.addEventListener('htmx:afterSwap', refreshMeasurement);

    document.addEventListener('click', async event => {
        if (document.body.dataset.tool !== 'ping' || event.button !== 0) return;
        if (!event.target.closest('#grid-container, #unitOverlay .unit')) return;
        const point = boardGeometry.localPoint(grid, event);
        const x = Math.round(point.x), y = Math.round(point.y);
        if (x < 0 || y < 0 || x >= grid.offsetWidth || y >= grid.offsetHeight) return;
        try {
            if (newUnitUpdateSocket.readyState !== WebSocket.OPEN) throw new Error('Disconnected');
            const response = await fetch(boardURL('/board/ping'), {
                method: 'POST',
                headers: {'Content-Type': 'application/json'},
                body: JSON.stringify({x, y, generation: boardGeneration}),
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
