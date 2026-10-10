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

    function closeSettings() {
        if (settings) settings.hidden = true;
        buttons.forEach(button => button.setAttribute('aria-expanded', 'false'));
    }
    for (const button of buttons) {
        button.addEventListener('contextmenu', event => {
            event.preventDefault();
            finish();
            closeSettings();
            settings.hidden = false;
            button.setAttribute('aria-expanded', 'true');
            const bounds = button.getBoundingClientRect();
            settings.style.left = `${Math.max(8, Math.min(bounds.right + 8, window.innerWidth - settings.offsetWidth - 8))}px`;
            settings.style.top = `${Math.max(8, Math.min(bounds.top, window.innerHeight - settings.offsetHeight - 8))}px`;
            (button.id === 'utility-fog' ? brushWidth : eraserWidth).focus();
        });
    }
    for (const input of [brushWidth, eraserWidth].filter(Boolean)) {
        input.addEventListener('input', () => {
            document.getElementById(`${input.id}-value`).textContent = `${input.value} px`;
        });
    }
    document.addEventListener('mousedown', event => {
        if (!settings?.contains(event.target) && !buttons.some(button => button.contains(event.target))) closeSettings();
    });
    document.addEventListener('keydown', event => {
        if (event.key === 'Escape' && settings && !settings.hidden) {
            const button = buttons.find(button => button.getAttribute('aria-expanded') === 'true');
            closeSettings();
            button?.focus();
        }
    });
    document.addEventListener('scroll', closeSettings, true);
    window.addEventListener('resize', closeSettings);

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
        const bounds = grid.getBoundingClientRect();
        if (!bounds.width || !bounds.height) return null;
        return {
            x: Number((Math.max(0, Math.min(64, (event.clientX - bounds.left) / bounds.width * 64))).toFixed(3)),
            y: Number((Math.max(0, Math.min(64, (event.clientY - bounds.top) / bounds.height * 64))).toFixed(3)),
        };
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
    document.addEventListener('toolChanged', () => {
        finish();
        closeSettings();
    });
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
