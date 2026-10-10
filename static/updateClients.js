posUpdateSocket = new WebSocket(`${location.protocol === 'https:' ? 'wss:' : 'ws:'}//${location.host}/ws/updatePosForClients`);

newUnitUpdateSocket = new WebSocket(`${location.protocol === 'https:' ? 'wss:' : 'ws:'}//${location.host}/ws/updateUnitsForClients`);

// Remember only deleted IDs so an older in-flight HTML response cannot restore them.
const removedUnitIDs = new Set();
const unitSizes = JSON.parse(document.getElementById('unit-sizes').textContent);

function updateUnitTitle(unit) {
    const size = unitSizes.find(option => option.Value === unit.dataset.size);
    unit.title = `${unit.querySelector('.unit-image').alt}\nSize: ${size?.Label || unit.dataset.size}\nStatus: ${unit.dataset.status || 'None'}`;
}

const statusColors = new Map([...document.querySelectorAll('.status-circle')].map(circle =>
    [circle.getAttribute('aria-label'), circle.style.getPropertyValue('--status-color')]));

document.addEventListener('unitStatusChanged', event => {
    const {id, status} = event.detail;
    if (removedUnitIDs.has(id)) return;
    const unit = document.getElementById(id);
    if (!unit) return;
    unit.dataset.status = status;
    unit.style.setProperty('--unit-status-color', statusColors.get(status) || 'transparent');
    updateUnitTitle(unit);
});

document.addEventListener('unitSizeChanged', event => {
    const {id, size} = event.detail;
    if (removedUnitIDs.has(id)) return;
    const unit = document.getElementById(id);
    const option = unitSizes.find(option => option.Value === size);
    if (!unit || !option) return;
    unit.dataset.size = size;
    unit.style.setProperty('--unit-squares', option.Squares);
    updateUnitTitle(unit);
});
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
        if (el && el.dataset.size !== unit.Size && !removedUnitIDs.has(unit.ID)) {
            document.dispatchEvent(new CustomEvent('unitSizeChanged', {detail: {id: unit.ID, size: unit.Size}}));
        }
        if (el && el.dataset.status !== (unit.Status || '') && !removedUnitIDs.has(unit.ID)) {
            document.dispatchEvent(new CustomEvent('unitStatusChanged', {detail: {id: unit.ID, status: unit.Status || ''}}));
        }
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
    if (message.type === 'unitSizeChanged' || message.type === 'unitStatusChanged') {
        document.dispatchEvent(new CustomEvent(message.type, {detail: message}));
        return;
    }
    if(!isDragging){
         htmx.trigger("#loadExistingUnits", "loadUnits");
    }
};
