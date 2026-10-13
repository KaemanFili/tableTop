package tables

import (
	"context"
	"net/http"
	"sync"

	"tableTop/main/internal/app"
	"tableTop/main/internal/handlers"
)

// Admission and draining prevent an in-flight upload or socket message from
// writing session data after deletion cleanup has completed.
type managedBoard struct {
	defaults app.PluginDefaults
	handler  http.Handler
	sockets  *handlers.WebSocketHandler
	mu       sync.Mutex
	closed   bool
	requests sync.WaitGroup
}

func (b *managedBoard) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	b.mu.Lock()
	if b.closed {
		b.mu.Unlock()
		http.Error(w, "This tabletop has been deleted.", http.StatusGone)
		return
	}
	b.requests.Add(1)
	b.mu.Unlock()
	defer b.requests.Done()
	b.handler.ServeHTTP(w, r)
}

func (b *managedBoard) stop(ctx context.Context) error {
	b.mu.Lock()
	b.closed = true
	b.mu.Unlock()
	b.sockets.Close()
	done := make(chan struct{})
	go func() { b.requests.Wait(); close(done) }()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
