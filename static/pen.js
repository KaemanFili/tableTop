(() => {
    const grid = document.getElementById('grid-container');
    const layer = document.getElementById('drawing-layer');
    const error = document.getElementById('utility-error');
    const button = document.getElementById('utility-pen');
    const settings = document.getElementById('pen-settings');
    const colorInput = document.getElementById('pen-color');
    const widthInput = document.getElementById('pen-width');
    const widthValue = document.getElementById('pen-width-value');
    const strokes = new Map();
    const erasedIDs = new Set();
    let owner = null;
    let nextStroke = 0;
    let active = null;
    let sendTimer = null;
    let erasing = false;
    let lastErasePoint = null;

    const settingsMenu = menuUI.popover(settings, [button]);
    const closeSettings = settingsMenu.close;
    button.addEventListener('contextmenu', event => {
        event.preventDefault();
        finishStroke();
        settingsMenu.open(button, colorInput);
    });
    widthInput.addEventListener('input', () => {
        widthValue.textContent = `${widthInput.value} px`;
    });

    function boardPoint(event) {
        return boardGeometry.point(grid, event, 3);
    }

    function appendPoints(id, points, style = {}) {
        let stroke = strokes.get(id);
        if (!stroke) {
            const path = document.createElementNS('http://www.w3.org/2000/svg', 'path');
            path.setAttribute('class', 'pen-stroke');
            const color = style.color || '#1d4ed8';
            const width = style.width || 3;
            path.style.stroke = color;
            // SVG coordinates are grid squares; 40px is the standard square size.
            path.style.strokeWidth = width / 40;
            layer.append(path);
            stroke = {path, width, count: 0, commands: '', points: []};
            strokes.set(id, stroke);
        }
        for (const point of points) {
            // A zero-length first segment with a round cap also draws a single click.
            stroke.commands += stroke.count ? ` L ${point.x} ${point.y}` : `M ${point.x} ${point.y} L ${point.x} ${point.y}`;
            stroke.count++;
            stroke.points.push(point);
        }
        stroke.path.setAttribute('d', stroke.commands);
    }

    function flushStroke(finished = false) {
        clearTimeout(sendTimer);
        sendTimer = null;
        if (!active) return;
        if (newUnitUpdateSocket.readyState !== WebSocket.OPEN) {
            error.textContent = 'Could not share drawing. Refresh to reconnect.';
            error.hidden = false;
            return;
        }
        // Small append-only batches stay within the socket's message-size limit.
        do {
            const points = active.pending.splice(0, 16);
            if (!points.length && !finished) break;
            newUnitUpdateSocket.send(JSON.stringify({
                type: 'penStroke', id: active.clientID, offset: active.sent, points,
                finished: finished && active.pending.length === 0,
                color: active.color, width: active.width,
                generation: boardGeneration,
            }));
            active.sent += points.length;
        } while (active.pending.length);
    }

    function finishStroke() {
        if (!active) return;
        flushStroke(true);
        active = null;
    }

    function addPoint(event) {
        const point = boardPoint(event);
        if (!active || !point) return;
        if (Math.hypot(point.x - active.last.x, point.y - active.last.y) < 0.01) return;
        appendPoints(active.id, [point]);
        active.pending.push(point);
        active.last = point;
        active.count++;
        if (active.count >= 8192) {
            finishStroke();
            return;
        }
        if (!sendTimer) sendTimer = setTimeout(() => flushStroke(), 33);
    }

    function removeStroke(id) {
        erasedIDs.add(id);
        strokes.get(id)?.path.remove();
        strokes.delete(id);
        if (active?.id === id) {
            clearTimeout(sendTimer);
            sendTimer = null;
            active = null;
        }
    }

    function pointSegmentDistanceSquared(point, start, end) {
        const dx = end.x - start.x, dy = end.y - start.y;
        const length = dx * dx + dy * dy;
        const t = length ? Math.max(0, Math.min(1,
            ((point.x - start.x) * dx + (point.y - start.y) * dy) / length)) : 0;
        return (point.x - start.x - t * dx) ** 2 + (point.y - start.y - t * dy) ** 2;
    }

    function segmentDistanceSquared(a, b, c, d) {
        const rx = b.x - a.x, ry = b.y - a.y;
        const sx = d.x - c.x, sy = d.y - c.y;
        const determinant = rx * sy - ry * sx;
        if (Math.abs(determinant) > 1e-9) {
            const qx = c.x - a.x, qy = c.y - a.y;
            const t = (qx * sy - qy * sx) / determinant;
            const u = (qx * ry - qy * rx) / determinant;
            if (t >= 0 && t <= 1 && u >= 0 && u <= 1) return 0;
        }
        return Math.min(pointSegmentDistanceSquared(a, c, d),
            pointSegmentDistanceSquared(b, c, d), pointSegmentDistanceSquared(c, a, b),
            pointSegmentDistanceSquared(d, a, b));
    }

    function eraseAt(point) {
        if (!point) return;
        if (newUnitUpdateSocket.readyState !== WebSocket.OPEN) {
            error.textContent = 'Could not erase drawing. Refresh to reconnect.';
            error.hidden = false;
            return;
        }
        const cell = boardGeometry.cellSize(grid);
        if (!cell?.width || !cell?.height) return;
        const pixels = point => ({x: point.x * cell.width, y: point.y * cell.height});
        const from = pixels(lastErasePoint || point), to = pixels(point);
        // An 8px brush plus half the pen width. Sweep between samples to catch fast drags.
        const ids = [];
        for (const [id, stroke] of strokes) {
            const radius = 8 + (stroke.width / 40) * Math.max(cell.width, cell.height) / 2;
            for (let i = 0; i < stroke.points.length; i++) {
                const a = pixels(stroke.points[i]);
                const b = pixels(stroke.points[i + 1] || stroke.points[i]);
                if (segmentDistanceSquared(from, to, a, b) <= radius * radius) {
                    ids.push(id);
                    break;
                }
            }
        }
        for (let i = 0; i < ids.length; i += 16) {
            const batch = ids.slice(i, i + 16);
            newUnitUpdateSocket.send(JSON.stringify({type: 'penErase', ids: batch}));
            batch.forEach(removeStroke);
        }
        lastErasePoint = point;
    }

    document.addEventListener('rulerSession', event => { owner = event.detail.id; });
    document.addEventListener('penStroke', event => {
        const stroke = event.detail;
        if (erasedIDs.has(stroke.id)) return;
        // Local strokes are drawn immediately; their broadcasts must not duplicate points.
        if (stroke.owner === owner) return;
        if ((strokes.get(stroke.id)?.count || 0) !== stroke.offset) return;
        if (stroke.points?.length) appendPoints(stroke.id, stroke.points, stroke);
    });
    document.addEventListener('penErased', event => event.detail.ids.forEach(removeStroke));
    document.addEventListener('boardCleared', () => {
        clearTimeout(sendTimer);
        sendTimer = null;
        active = null;
        erasing = false;
        lastErasePoint = null;
        strokes.clear();
        erasedIDs.clear();
        layer.replaceChildren();
        closeSettings();
    });
    document.addEventListener('rulerDisconnected', () => {
        clearTimeout(sendTimer);
        sendTimer = null;
        active = null;
        owner = null;
        erasing = false;
        lastErasePoint = null;
    });
    document.addEventListener('toolChanged', event => {
        if (event.detail.tool !== 'pen') finishStroke();
        erasing = false;
        lastErasePoint = null;
    });

    boardInput.addEventListener('down', event => {
        if (document.body.dataset.tool !== 'pen' || event.button !== 0) return;
        if (!event.target.closest('#grid-container, #unitOverlay .unit')) return;
        event.preventDefault();
        if (!owner || newUnitUpdateSocket.readyState !== WebSocket.OPEN) {
            error.textContent = 'Could not share drawing. Refresh to reconnect.';
            error.hidden = false;
            return;
        }
        const point = boardPoint(event);
        if (!point) return;
        finishStroke();
        const clientID = String(++nextStroke);
        const id = `${owner}/${clientID}`;
        active = {id, clientID, pending: [point], sent: 0, last: point, count: 1,
            color: colorInput.value, width: Number(widthInput.value)};
        appendPoints(id, [point], active);
        error.hidden = true;
        flushStroke();
    });
    boardInput.addEventListener('move', event => {
        if (active) addPoint(event);
        if (erasing) eraseAt(boardPoint(event));
    });
    boardInput.addEventListener('up', event => {
        if (erasing && event.button === 0) {
            eraseAt(boardPoint(event));
            erasing = false;
            lastErasePoint = null;
        }
        if (!active || event.button !== 0) return;
        addPoint(event);
        finishStroke();
    });
    boardInput.addEventListener('down', event => {
        if (document.body.dataset.tool !== 'eraser' || event.button !== 0) return;
        if (!event.target.closest('#grid-container, #unitOverlay .unit')) return;
        event.preventDefault();
        erasing = true;
        lastErasePoint = null;
        error.hidden = true;
        eraseAt(boardPoint(event));
    });
    boardInput.addEventListener('cancel', () => {
        finishStroke();
        erasing = false;
        lastErasePoint = null;
    });
    window.addEventListener('blur', () => {
        finishStroke();
        erasing = false;
        lastErasePoint = null;
    });
})();
