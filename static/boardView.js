(() => {
    const world = document.getElementById('board-world');
    const viewport = document.getElementById('board-viewport');
    const map = document.getElementById('map-layer');
    // The board chooses its initial view instead of restoring an old scroll position.
    history.scrollRestoration = 'manual';
    let currentMap = null;
    let pendingFrame = null;

    function centerMap() {
        pendingFrame = null;
        if (!currentMap || map.hidden) return;
        const bounds = map.getBoundingClientRect();
        viewport.scrollTo({
            left: viewport.scrollLeft + bounds.left + bounds.width / 2 - viewport.clientWidth / 2,
            top: viewport.scrollTop + bounds.top + bounds.height / 2 - viewport.clientHeight / 2,
            behavior: 'instant',
        });
        document.dispatchEvent(new CustomEvent('viewPanChanged'));
    }

    function scheduleCenter() {
        if (pendingFrame !== null) cancelAnimationFrame(pendingFrame);
        pendingFrame = requestAnimationFrame(centerMap);
    }

    function clearMap() {
        currentMap = null;
        if (pendingFrame !== null) cancelAnimationFrame(pendingFrame);
        pendingFrame = null;
    }

    function artworkKey(definition) {
        const artwork = definition.artwork;
        return artwork ? `${artwork.pluginID}/${artwork.version}/${artwork.kind}/${artwork.imageID}` : definition.id;
    }

    function allowRoomForMap(definition) {
        world.style.marginLeft = `calc(var(--cell-size) * ${Math.max(0, -definition.offsetX)})`;
        world.style.marginTop = `calc(var(--cell-size) * ${Math.max(0, -definition.offsetY)})`;
        world.style.width = `calc(var(--cell-size) * ${Math.max(64, definition.offsetX + definition.width)})`;
        world.style.height = `calc(var(--cell-size) * ${Math.max(64, definition.offsetY + definition.height)})`;
    }

    document.addEventListener('boardMapChanged', event => {
        const next = event.detail.map;
        if (!next) {
            clearMap();
            return;
        }
        const changed = !currentMap || artworkKey(currentMap) !== artworkKey(next) ||
            ['width', 'height', 'offsetX', 'offsetY'].some(key => currentMap[key] !== next[key]);
        currentMap = next;
        if (changed) {
            allowRoomForMap(next);
            scheduleCenter();
        }
    });
    document.addEventListener('boardCleared', clearMap);

    function initialView() {
        if (currentMap) scheduleCenter();
        else viewport.scrollTo({left: world.offsetLeft, top: world.offsetTop, behavior: 'instant'});
    }

    // Space around the board permits centering maps smaller than the viewport at (0, 0).
    // Start an empty board at its origin, then center its first map when it arrives.
    function sizeViewport() {
        const space = document.getElementById('board-space');
        space.style.setProperty('--board-viewport-width', `${viewport.clientWidth}px`);
        space.style.setProperty('--board-viewport-height', `${viewport.clientHeight}px`);
    }
    sizeViewport();
    new ResizeObserver(sizeViewport).observe(viewport);
    initialView();
    window.addEventListener('load', initialView, {once: true});
})();
