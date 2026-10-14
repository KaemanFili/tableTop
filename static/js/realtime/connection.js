// Keep HTTP requests, artwork, and sockets scoped to the board in this tab.
function boardURL(path) {
    return `${document.body.dataset.sessionPrefix || ''}${path}`;
}

// Recreate a dropped socket with bounded backoff. Each caller installs its
// handlers on the replacement so every board feature uses the live connection.
function connectBoardSocket(path, configure) {
    let socket;
    let retryTimer;
    let attempts = 0;
    let stopped = false;
    function connect() {
        clearTimeout(retryTimer);
        if (stopped) return;
        socket = new WebSocket(`${location.protocol === 'https:' ? 'wss:' : 'ws:'}//${location.host}${boardURL(path)}`);
        configure(socket);
        // Reset after a stable connection, so repeated short connections back off too.
        let stableTimer;
        socket.addEventListener('open', () => {
            stableTimer = setTimeout(() => { attempts = 0; }, 10000);
        });
        socket.addEventListener('close', () => {
            clearTimeout(stableTimer);
            if (stopped) return;
            const delay = Math.min(30000, 500 * 2 ** Math.min(attempts++, 6));
            retryTimer = setTimeout(connect, delay * (0.8 + Math.random() * 0.4));
        });
    }
    window.addEventListener('online', () => {
        if (socket.readyState === WebSocket.CLOSED) connect();
    });
    window.addEventListener('pagehide', () => {
        stopped = true;
        clearTimeout(retryTimer);
        socket.close();
    });
    window.addEventListener('pageshow', event => {
        if (event.persisted) {
            stopped = false;
            if (socket.readyState === WebSocket.CLOSED) connect();
        }
    });
    connect();
}
