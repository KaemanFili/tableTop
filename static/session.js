// Keep HTTP requests, artwork, and sockets scoped to the board in this tab.
function boardURL(path) {
    return `${document.body.dataset.sessionPrefix || ''}${path}`;
}
