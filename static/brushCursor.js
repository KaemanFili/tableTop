(() => {
    const grid = document.getElementById('grid-container');
    const cursor = document.getElementById('brush-cursor');
    const widths = {
        pen: document.getElementById('pen-width'),
        fog: document.getElementById('fog-width'),
        'fog-eraser': document.getElementById('fog-eraser-width'),
    };
    let pointer = null;

    function hide() {
        cursor.hidden = true;
        document.body.classList.remove('brush-cursor-visible');
    }

    function refresh() {
        const tool = document.body.dataset.tool;
        const input = widths[tool];
        if (!pointer || !input || document.body.classList.contains('map-panning')) {
            hide();
            return;
        }
        const target = document.elementFromPoint(pointer.x, pointer.y);
        if (!target?.closest('#grid-container, #unitOverlay .unit')) {
            hide();
            return;
        }
        const bounds = grid.getBoundingClientRect();
        const cell = grid.querySelector('.cell')?.getBoundingClientRect();
        if (!cell?.width || !cell?.height || pointer.x < bounds.left || pointer.x > bounds.right ||
            pointer.y < bounds.top || pointer.y > bounds.bottom) {
            hide();
            return;
        }
        const squares = Number(input.value) / 40;
        // Match each renderer: SVG pen uses cells; canvas fog spans the whole grid.
        const width = squares * (tool === 'pen' ? cell.width : bounds.width / 64);
        const height = squares * (tool === 'pen' ? cell.height : bounds.height / 64);
        cursor.style.width = `${width}px`;
        cursor.style.height = `${height}px`;
        cursor.style.left = `${pointer.x}px`;
        cursor.style.top = `${pointer.y}px`;
        cursor.hidden = false;
        document.body.classList.add('brush-cursor-visible');
    }

    document.addEventListener('mousemove', event => {
        pointer = {x: event.clientX, y: event.clientY};
        refresh();
    });
    for (const input of Object.values(widths).filter(Boolean)) {
        input.addEventListener('input', refresh);
    }
    document.addEventListener('toolChanged', refresh);
    document.addEventListener('viewPanChanged', refresh);
    document.addEventListener('boardCleared', hide);
    document.addEventListener('scroll', refresh, true);
    document.addEventListener('htmx:afterSwap', refresh);
    window.addEventListener('resize', refresh);
    new ResizeObserver(refresh).observe(grid);

    function forgetPointer() {
        pointer = null;
        hide();
    }
    document.addEventListener('mouseleave', forgetPointer);
    window.addEventListener('blur', forgetPointer);
    document.addEventListener('visibilitychange', () => {
        if (document.hidden) forgetPointer();
    });
})();
