const posUpdateSocket = new WebSocket(`${location.protocol === 'https:' ? 'wss:' : 'ws:'}//${location.host}/ws/updatePosForClients`);
let boardGeneration = 0;

const newUnitUpdateSocket = new WebSocket(`${location.protocol === 'https:' ? 'wss:' : 'ws:'}//${location.host}/ws/updateUnitsForClients${document.body.dataset.gm === 'true' ? '?gm=true' : ''}`);

// Coalesce refreshes during a drag; unit refreshes must never replace a dragged token.
let unitsRefreshPending = false;
function refreshUnits() {
    unitsRefreshPending = true;
    if (isDragging) return;
    unitsRefreshPending = false;
    htmx.trigger('#loadExistingUnits', 'loadUnits');
}
document.addEventListener('unitDragEnded', () => {
    if (unitsRefreshPending) refreshUnits();
});
document.addEventListener('unitSpawned', refreshUnits);
document.addEventListener('htmx:beforeSwap', event => {
    if (event.detail.target.id !== 'unitOverlay' || !isDragging) return;
    event.detail.shouldSwap = false;
    unitsRefreshPending = true;
});

// Remember only deleted IDs so an older in-flight HTML response cannot restore them.
const removedUnitIDs = new Set();
const unitSizes = JSON.parse(document.getElementById('unit-sizes').textContent);

const unitAnimations = new Map();
const pendingUnitPositions = new Map();
const unitBlendDuration = 60;
let unitAnimationFrame = null;

function stopUnitAnimation(id) {
    unitAnimations.delete(id);
    if (unitAnimations.size === 0) {
        cancelAnimationFrame(unitAnimationFrame);
        unitAnimationFrame = null;
    }
}

function animateUnits(now) {
    unitAnimationFrame = null;
    for (const [id, animation] of unitAnimations) {
        const {element, fromLeft, fromTop, left, top, started} = animation;
        if (!element.isConnected || element.classList.contains('dragging') || removedUnitIDs.has(id)) {
            unitAnimations.delete(id);
            continue;
        }
        const progress = movementMath.progress(now, started, unitBlendDuration);
        element.style.left = `${movementMath.interpolate(fromLeft, left, progress)}px`;
        element.style.top = `${movementMath.interpolate(fromTop, top, progress)}px`;
        document.dispatchEvent(new CustomEvent('unitPositionChanged', {detail: {id}}));
        if (progress === 1) unitAnimations.delete(id);
    }
    if (unitAnimations.size) unitAnimationFrame = requestAnimationFrame(animateUnits);
}

function updateUnitPosition(element, left, top) {
    const id = element.id;
    if (removedUnitIDs.has(id) || element.classList.contains('dragging')) return;
    const pending = pendingUnitPositions.get(id);
    if (pending) {
        if (left === pending.left && top === pending.top) {
            pendingUnitPositions.delete(id);
        } else if (performance.now() < pending.expires) {
            // Wait for the server to observe the final drop before accepting older position updates.
            return;
        } else {
            pendingUnitPositions.delete(id);
        }
    }
    const previous = unitAnimations.get(id);
    // Repeated stationary position updates must let an existing animation finish.
    if (previous?.element === element && previous.left === left && previous.top === top) return;
    const fromLeft = parseFloat(element.style.left) || 0;
    const fromTop = parseFloat(element.style.top) || 0;
    if (fromLeft === left && fromTop === top) {
        stopUnitAnimation(id);
        return;
    }
    unitAnimations.set(id, {element, fromLeft, fromTop, left, top, started: performance.now()});
    if (unitAnimationFrame === null) unitAnimationFrame = requestAnimationFrame(animateUnits);
}

document.addEventListener('unitDragStarted', event => {
    stopUnitAnimation(event.detail.id);
    pendingUnitPositions.delete(event.detail.id);
});
document.addEventListener('unitDragFinished', event => {
    const {id, left, top} = event.detail;
    pendingUnitPositions.set(id, {left, top, expires: performance.now() + 1000});
});

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
    stopUnitAnimation(id);
    pendingUnitPositions.delete(id);
    document.getElementById(id)?.remove();
});
document.addEventListener('htmx:afterSwap', () => {
    removedUnitIDs.forEach(id => document.getElementById(id)?.remove());
});

document.addEventListener('boardCleared', event => {
    const map = document.getElementById('map-layer');
    map.hidden = true;
    map.removeAttribute('src');
    const ids = new Set([...event.detail.ids, ...document.querySelectorAll('#unitOverlay .unit')].map(item =>
        typeof item === 'string' ? item : item.id));
    ids.forEach(id => document.dispatchEvent(new CustomEvent('unitRemoved', {detail: {id}})));
    document.getElementById('unitOverlay').replaceChildren();
    cancelAnimationFrame(unitAnimationFrame);
    unitAnimationFrame = null;
    unitAnimations.clear();
    pendingUnitPositions.clear();
});

document.addEventListener('boardMapChanged', event => {
    const definition = event.detail.map;
    const map = document.getElementById('map-layer');
    if (!definition) {
        map.hidden = true;
        map.removeAttribute('src');
        map.alt = '';
        return;
    }
    map.src = `/maps/images/${encodeURIComponent(definition.id)}`;
    map.alt = definition.name;
    for (const [property, value] of Object.entries({width: definition.width, height: definition.height,
        left: definition.offsetX, top: definition.offsetY})) {
        map.style[property] = `calc(var(--cell-size) * ${value})`;
    }
    map.hidden = false;
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
        if (el) updateUnitPosition(el, unit.Left, unit.Top);
    });
};

posUpdateSocket.onerror = function(err) {
    console.error("WebSocket error:", err);
};

newUnitUpdateSocket.onmessage = function(event) {
    const message = JSON.parse(event.data);
    if (message.type === 'boardMapChanged') {
        document.dispatchEvent(new CustomEvent('boardMapChanged', {detail: message}));
        return;
    }
    if (message.type === 'boardCleared') {
        boardGeneration = message.generation;
        document.dispatchEvent(new CustomEvent('boardCleared', {detail: message}));
        return;
    }
    if (message.type === 'rulerSession') boardGeneration = message.generation || 0;
    if (message.type === 'fogMark' || message.type === 'fogReset') {
        document.dispatchEvent(new CustomEvent(message.type, {detail: message}));
        return;
    }
    if (message.type === 'penStroke' || message.type === 'penErased') {
        document.dispatchEvent(new CustomEvent(message.type, {detail: message}));
        return;
    }
    if (['rulerSession', 'rulerChanged'].includes(message.type)) {
        document.dispatchEvent(new CustomEvent(message.type, {detail: message}));
        return;
    }
    if (message.type === 'boardPing') {
        document.dispatchEvent(new CustomEvent('boardPing', {detail: message}));
        return;
    }
    if (message.type === 'unitRemoved') {
        document.dispatchEvent(new CustomEvent('unitRemoved', {detail: {id: message.id}}));
        return;
    }
    if (message.type === 'unitSizeChanged' || message.type === 'unitStatusChanged') {
        document.dispatchEvent(new CustomEvent(message.type, {detail: message}));
        return;
    }
    if (message === 'unit created') refreshUnits();
};

newUnitUpdateSocket.addEventListener('close', () => {
    document.dispatchEvent(new CustomEvent('rulerDisconnected'));
});
