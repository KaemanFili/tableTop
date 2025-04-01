posUpdateSocket = new WebSocket("ws://localhost:18080/ws/updatePosForClients");

newUnitUpdateSocket = new WebSocket("ws://localhost:18080/ws/updateUnitsForClients");

posUpdateSocket.onmessage = function(event) {
    const units = JSON.parse(event.data);
    units.forEach(unit => {
        const el = document.getElementById(unit.ID);
        if (el && el.style.cursor !== "grabbing") {
            el.style.left = unit.Left + "px";
            el.style.top = unit.Top + "px";
        }
    });
};

posUpdateSocket.onerror = function(err) {
    console.error("WebSocket error:", err);
};

newUnitUpdateSocket.onmessage = function(event) {
    console.log(JSON.parse(event.data));
    if(!isDragging){
         htmx.trigger("#loadExistingUnits", "loadUnits");
    }
};