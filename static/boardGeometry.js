// Drawing coordinates use grid squares; the DOM owns the rendered grid size.
const boardGeometry = Object.freeze({
    cellSize(grid) {
        return grid.querySelector('.cell')?.getBoundingClientRect();
    },

    point(grid, event, precision) {
        const bounds = grid.getBoundingClientRect();
        const cell = this.cellSize(grid);
        if (!cell?.width || !cell?.height) return null;
        const round = value => precision === undefined ? value : Number(value.toFixed(precision));
        return {
            x: round(Math.max(0, Math.min(bounds.width, event.clientX - bounds.left)) / cell.width),
            y: round(Math.max(0, Math.min(bounds.height, event.clientY - bounds.top)) / cell.height),
        };
    },
});
