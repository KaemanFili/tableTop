(() => {
    const grid = document.getElementById('grid-container');
    const canvas = document.getElementById('fog-layer');
    const context = canvas.getContext('2d');
    const confirmed = document.createElement('canvas');
    confirmed.width = canvas.width;
    confirmed.height = canvas.height;
    const confirmedContext = confirmed.getContext('2d');
    const error = document.getElementById('utility-error');
    const pending = new Map();
    let owner = null;
    let nextMark = 0;
    let drawing = false;
    let lastPoint = null;
    let batch = null;
    let sendTimer = null;
    const settings = document.getElementById('fog-settings');
    const brushWidth = document.getElementById('fog-width');
    const eraserWidth = document.getElementById('fog-eraser-width');
    const buttons = [document.getElementById('utility-fog'), document.getElementById('utility-fog-eraser')].filter(Boolean);

    const settingsMenu = menuUI.popover(settings, buttons);
    const closeSettings = settingsMenu.close;
    for (const button of buttons) {
        button.addEventListener('contextmenu', event => {
            event.preventDefault();
            finish();
            settingsMenu.open(button, button.id === 'utility-fog' ? brushWidth : eraserWidth);
        });
    }
    for (const input of [brushWidth, eraserWidth].filter(Boolean)) {
        input.addEventListener('input', () => {
            document.getElementById(`${input.id}-value`).textContent = `${input.value} px`;
        });
    }

    function paint(target, mark) {
        target.globalCompositeOperation = mark.mode === 'erase' ? 'destination-out' : 'source-over';
        target.strokeStyle = '#111827';
        target.lineWidth = mark.width * canvas.width / 64;
        target.lineCap = 'round';
        target.lineJoin = 'round';
        target.beginPath();
        const pixels = point => ({x: point.x * canvas.width / 64, y: point.y * canvas.height / 64});
        const first = pixels(mark.points[0]);
        target.moveTo(first.x, first.y);
        // A round zero-length segment paints or erases a single click.
        target.lineTo(first.x, first.y);
        for (const point of mark.points.slice(1)) {
            const pixel = pixels(point);
            target.lineTo(pixel.x, pixel.y);
        }
        target.stroke();
        target.globalCompositeOperation = 'source-over';
    }

    function renderPending() {
        context.clearRect(0, 0, canvas.width, canvas.height);
        context.drawImage(confirmed, 0, 0);
        for (const mark of pending.values()) paint(context, mark);
    }

    function boardPoint(event) {
        return boardGeometry.point(grid, event, 3);
    }

    function flush() {
        clearTimeout(sendTimer);
        sendTimer = null;
        if (!batch) return;
        if (newUnitUpdateSocket.readyState !== WebSocket.OPEN) {
            error.textContent = 'Could not share fog. Refresh to reconnect.';
            error.hidden = false;
            pending.clear();
            batch = null;
            drawing = false;
            renderPending();
            return;
        }
        newUnitUpdateSocket.send(JSON.stringify(batch));
        batch = null;
    }

    function addPoint(point) {
        if (!point) return;
        if (lastPoint && Math.hypot(point.x - lastPoint.x, point.y - lastPoint.y) < 0.01) return;
        if (!batch) {
            const tool = document.body.dataset.tool;
            batch = {type: 'fogMark', id: String(++nextMark), mode: tool === 'fog-eraser' ? 'erase' : 'paint',
                generation: boardGeneration,
                width: Number(tool === 'fog-eraser' ? eraserWidth.value : brushWidth.value) / 40,
                points: lastPoint ? [lastPoint] : []};
            pending.set(`${owner}/${batch.id}`, batch);
        }
        batch.points.push(point);
        paint(context, {...batch, points: lastPoint ? [lastPoint, point] : [point]});
        lastPoint = point;
        if (batch.points.length === 16) flush();
        else if (!sendTimer) sendTimer = setTimeout(flush, 33);
    }

    function finish() {
        flush();
        drawing = false;
        lastPoint = null;
    }

    document.addEventListener('fogReset', () => {
        confirmedContext.clearRect(0, 0, canvas.width, canvas.height);
        pending.clear();
        renderPending();
    });
    document.addEventListener('boardCleared', () => {
        clearTimeout(sendTimer);
        sendTimer = null;
        drawing = false;
        lastPoint = null;
        batch = null;
        pending.clear();
        confirmedContext.clearRect(0, 0, canvas.width, canvas.height);
        renderPending();
        closeSettings();
    });
    document.addEventListener('fogMark', event => {
        const mark = event.detail;
        paint(confirmedContext, mark);
        pending.delete(mark.id);
        renderPending();
    });
    document.addEventListener('rulerSession', event => { owner = event.detail.id; });
    document.addEventListener('rulerDisconnected', () => {
        clearTimeout(sendTimer);
        sendTimer = null;
        owner = null;
        drawing = false;
        lastPoint = null;
        batch = null;
        pending.clear();
        renderPending();
    });
    document.addEventListener('toolChanged', finish);
    document.addEventListener('mousedown', event => {
        if (document.body.dataset.gm !== 'true' || !['fog', 'fog-eraser'].includes(document.body.dataset.tool) || event.button !== 0) return;
        if (!event.target.closest('#grid-container, #unitOverlay .unit')) return;
        event.preventDefault();
        if (!owner || newUnitUpdateSocket.readyState !== WebSocket.OPEN) {
            error.textContent = 'Could not share fog. Refresh to reconnect.';
            error.hidden = false;
            return;
        }
        finish();
        drawing = true;
        error.hidden = true;
        addPoint(boardPoint(event));
        flush();
    });
    document.addEventListener('mousemove', event => {
        if (drawing) addPoint(boardPoint(event));
    });
    document.addEventListener('mouseup', event => {
        if (!drawing || event.button !== 0) return;
        addPoint(boardPoint(event));
        finish();
    });
    window.addEventListener('blur', finish);
})();
