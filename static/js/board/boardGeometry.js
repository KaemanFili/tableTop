// Drawing coordinates use grid squares; the DOM owns the rendered grid size.
const boardGeometry = Object.freeze({
    localPoint(element, event) {
        const bounds = element.getBoundingClientRect();
        return {
            x: (event.clientX - bounds.left) * element.offsetWidth / bounds.width,
            y: (event.clientY - bounds.top) * element.offsetHeight / bounds.height,
        };
    },

    // Screen-space sizes are for hit testing and cursor previews.
    cellSize(grid) {
        const bounds = grid.getBoundingClientRect();
        return {width: bounds.width / 64, height: bounds.height / 64};
    },

    // Renderers inside the zoomed world must use unscaled board pixels.
    localCellSize(grid) {
        return {width: grid.offsetWidth / 64, height: grid.offsetHeight / 64};
    },

    point(grid, event, precision) {
        const bounds = grid.getBoundingClientRect();
        if (!bounds.width || !bounds.height) return null;
        const round = value => precision === undefined ? value : Number(value.toFixed(precision));
        // Normalize against the whole grid: individual cells may round differently
        // at fractional zoom levels. This also matches the SVG's 64-square viewBox.
        return {
            x: round(Math.max(0, Math.min(64, (event.clientX - bounds.left) / bounds.width * 64))),
            y: round(Math.max(0, Math.min(64, (event.clientY - bounds.top) / bounds.height * 64))),
        };
    },
});
