posUpdateSocket = new WebSocket(`${location.protocol === 'https:' ? 'wss:' : 'ws:'}//${location.host}/ws/updatePosForClients`);

newUnitUpdateSocket = new WebSocket(`${location.protocol === 'https:' ? 'wss:' : 'ws:'}//${location.host}/ws/updateUnitsForClients`);

// Remember only deleted IDs so an older in-flight HTML response cannot restore them.
const removedUnitIDs = new Set();
document.addEventListener('unitRemoved', event => {
    const id = event.detail.id;
    if (typeof id !== 'string' || !id) return;
    removedUnitIDs.add(id);
    document.getElementById(id)?.remove();
});
document.addEventListener('htmx:afterSwap', () => {
    removedUnitIDs.forEach(id => document.getElementById(id)?.remove());
});

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
    const message = JSON.parse(event.data);
    if (message.type === 'unitRemoved') {
        document.dispatchEvent(new CustomEvent('unitRemoved', {detail: {id: message.id}}));
        return;
    }
    if(!isDragging){
         htmx.trigger("#loadExistingUnits", "loadUnits");
    }
};
