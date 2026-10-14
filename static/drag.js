let isDragging = false;
const updateServerSocket = new WebSocket(`${location.protocol === 'https:' ? 'wss:' : 'ws:'}//${location.host}${boardURL('/ws/updateServer')}`);

updateServerSocket.onopen = () => {
    console.log("webSocket connection established");
};

boardInput.addEventListener("down", function (e) {
    if (e.button !== 0) return;
    if (['ping', 'ruler', 'pen', 'eraser', 'fog', 'fog-eraser'].includes(document.body.dataset.tool)) return;
    const target = e.target.closest(".unit");
    if (!target) return;
    // Artwork and other children should drag their containing unit.
    e.preventDefault();

    const overlay = target.parentElement;
    const start = boardGeometry.localPoint(overlay, e);
    const offsetX = start.x - target.offsetLeft;
    const offsetY = start.y - target.offsetTop;

    //Add dragging class
    isDragging = true;
    target.classList.add("dragging");
    target.style.cursor = "grabbing";
    document.dispatchEvent(new CustomEvent('unitDragStarted', {detail: {id: target.id}}));
    let sendTimer = null;

    function move(e) {
        const point = boardGeometry.localPoint(overlay, e);
        target.style.left = `${point.x - offsetX}px`;
        target.style.top = `${point.y - offsetY}px`;
        if (!sendTimer) sendTimer = setTimeout(updateUnitPositionOnServer, 33);
    }

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
        clearTimeout(sendTimer);
        sendTimer = null;

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
                Left: parseInt(target.style.left, 10),
                Top: parseInt(target.style.top, 10),
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
    document.addEventListener("unitRemoved", removed);
    window.addEventListener("blur", up);
});
