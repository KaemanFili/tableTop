let isDragging = false;
const updateServerSocket = new WebSocket(`${location.protocol === 'https:' ? 'wss:' : 'ws:'}//${location.host}/ws/updateServer`);

updateServerSocket.onopen = () => {
    console.log("webSocket connection established");
};

document.addEventListener("mousedown", function (e) {
    if (e.button !== 0) return;
    const target = e.target.closest(".unit");
    if (!target) return;
    // Artwork and other children should drag their containing unit.
    e.preventDefault();

    let offsetX = e.clientX - target.offsetLeft;
    let offsetY = e.clientY - target.offsetTop;

    //Add dragging class
    isDragging = true;
    target.classList.add("dragging");
    target.style.cursor = "grabbing";

    function move(e) {
        target.style.left = `${e.clientX - offsetX}px`;
        target.style.top = `${e.clientY - offsetY}px`;
        updateUnitPositionOnServer();
    }

    function up() {
        finish(true);
    }

    function removed(event) {
        if (event.detail.id === target.id) finish(false);
    }

    function finish(sendPosition) {
        document.removeEventListener("mousemove", move);
        document.removeEventListener("mouseup", up);
        document.removeEventListener("unitRemoved", removed);

        // Remove dragging class
        isDragging = false;
        target.classList.remove("dragging");
        target.style.cursor = "grab";

        if (sendPosition) updateUnitPositionOnServer();
    }

    function updateUnitPositionOnServer() {
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

    document.addEventListener("mousemove", move);
    document.addEventListener("mouseup", up);
    document.addEventListener("unitRemoved", removed);
});
