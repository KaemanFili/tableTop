let isDragging = false;
const updateServerSocket = new WebSocket('ws://localhost:18080/ws/updateServer');

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
        document.removeEventListener("mousemove", move);
        document.removeEventListener("mouseup", up);

        // Remove dragging class
        isDragging = false;
        target.classList.remove("dragging");
        target.style.cursor = "grab";

        console.log("up: updating position");
        updateUnitPositionOnServer();
    }

    function updateUnitPositionOnServer() {
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
});
