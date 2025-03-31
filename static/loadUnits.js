clientUpdateSocket = new WebSocket("ws://localhost:18080/ws/updateClients");

clientUpdateSocket.onmessage = function(event) {
    const units = JSON.parse(event.data);
    units.forEach(unit => {
        const el = document.getElementById(unit.ID);
        if (el.style.cursor !== "grabbing") {
            el.style.left = unit.Left + "px";
            el.style.top = unit.Top + "px";
        }
    });
};

clientUpdateSocket.onerror = function(err) {
    console.error("WebSocket error:", err);
};