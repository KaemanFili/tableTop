let isDragging = false;
let updateServerSocket;
connectBoardSocket('/ws/updateServer', socket => { updateServerSocket = socket; });

boardInput.addEventListener("down", function (e) {
    if (e.button !== 0) return;
    if (['ping', 'ruler', 'pen', 'eraser', 'fog', 'fog-eraser'].includes(document.body.dataset.tool)) return;
    const target = e.target.closest(".unit");
    if (!target) return;
    // Artwork and other children should drag their containing unit.
    e.preventDefault();

    document.dispatchEvent(new CustomEvent('unitDragStarted', {detail: {id: target.id}}));
    const overlay = target.parentElement;
    const initialLeft = target.offsetLeft, initialTop = target.offsetTop;
    let geometry = null;
    let geometryDirty = true;
    function readGeometry() {
        if (!geometryDirty) return;
        const bounds = overlay.getBoundingClientRect();
        geometry = {left: bounds.left, top: bounds.top,
            scaleX: bounds.width / overlay.offsetWidth, scaleY: bounds.height / overlay.offsetHeight};
        geometryDirty = false;
    }
    readGeometry();
    const offsetX = (e.clientX - geometry.left) / geometry.scaleX - initialLeft;
    const offsetY = (e.clientY - geometry.top) / geometry.scaleY - initialTop;
    let position = {left: initialLeft, top: initialTop};
    let pendingPoint = null;
    let renderFrame = null;
    let sendTimer = null;

    isDragging = true;
    target.classList.add("dragging");
    target.style.cursor = "grabbing";

    function renderMove() {
        renderFrame = null;
        if (!pendingPoint || !target.isConnected) return;
        readGeometry();
        position = {left: (pendingPoint.x - geometry.left) / geometry.scaleX - offsetX,
            top: (pendingPoint.y - geometry.top) / geometry.scaleY - offsetY};
        pendingPoint = null;
        target.style.transform = `translate3d(${position.left - initialLeft}px, ${position.top - initialTop}px, 0)`;
        if (!sendTimer) sendTimer = setTimeout(updateUnitPositionOnServer, 33);
    }
    function move(e) {
        pendingPoint = {x: e.clientX, y: e.clientY};
        if (renderFrame === null) renderFrame = requestAnimationFrame(renderMove);
    }
    function invalidateGeometry() { geometryDirty = true; }

    function up() {
        finish(true);
    }

    function removed(event) {
        if (event.detail.id === target.id) finish(false);
    }

    function finish(sendPosition) {
        boardInput.removeEventListener("move", move);
        boardInput.removeEventListener("up", up);
        document.removeEventListener("unitRemoved", removed);
        boardInput.removeEventListener("cancel", up);
        window.removeEventListener("blur", up);
        document.removeEventListener('scroll', invalidateGeometry, true);
        document.removeEventListener('viewZoomChanged', invalidateGeometry);
        window.removeEventListener('resize', invalidateGeometry);
        if (renderFrame !== null) {
            cancelAnimationFrame(renderFrame);
            renderMove();
        }
        clearTimeout(sendTimer);
        sendTimer = null;
        // Commit compositor movement before clearing it; other clients use board pixels.
        if (target.isConnected) {
            target.style.left = `${Math.trunc(position.left)}px`;
            target.style.top = `${Math.trunc(position.top)}px`;
            target.style.removeProperty('transform');
        }

        // Remove dragging class
        isDragging = false;
        target.classList.remove("dragging");
        target.style.cursor = "grab";

        if (sendPosition && target.isConnected) {
            const left = parseInt(target.style.left, 10);
            const top = parseInt(target.style.top, 10);
            // Match the integer server position at drop to prevent a fractional correction.
            target.style.left = `${left}px`;
            target.style.top = `${top}px`;
            document.dispatchEvent(new CustomEvent('unitDragFinished', {detail: {id: target.id, left, top}}));
            updateUnitPositionOnServer();
        }
        document.dispatchEvent(new CustomEvent('unitDragEnded', {detail: {id: target.id}}));
    }

    function updateUnitPositionOnServer() {
        sendTimer = null;
        if (!target.isConnected) return;
        if (updateServerSocket.readyState === WebSocket.OPEN) {
            let data = {
                Left: Math.trunc(position.left),
                Top: Math.trunc(position.top),
                ID: target.id
            };
            updateServerSocket.send(JSON.stringify(data));
        } else {
            console.log("can't connect to server via websocket");
        }
    }

    boardInput.addEventListener("move", move);
    boardInput.addEventListener("up", up);
    boardInput.addEventListener("cancel", up);
    document.addEventListener('scroll', invalidateGeometry, true);
    document.addEventListener('viewZoomChanged', invalidateGeometry);
    window.addEventListener('resize', invalidateGeometry);
    document.addEventListener("unitRemoved", removed);
    window.addEventListener("blur", up);
});
