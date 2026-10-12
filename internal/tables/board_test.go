package tables

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"tableTop/main/internal/app"
	"tableTop/main/internal/handlers"
)

func TestBoardDeletionDrainsRequestsBeforeCleanup(t *testing.T) {
	started, release, finished := make(chan struct{}), make(chan struct{}), make(chan struct{})
	board := &managedBoard{sockets: handlers.NewWebSocketHandler(app.NewState()), handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/upload" {
			close(started)
			<-release
		}
		w.WriteHeader(http.StatusOK)
	})}
	go func() {
		board.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("POST", "/upload", nil))
		close(finished)
	}()
	<-started
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	stopped := make(chan error, 1)
	go func() { stopped <- board.stop(ctx) }()
	// Observe admission closing through HTTP while the earlier upload is active.
	for {
		response := httptest.NewRecorder()
		board.ServeHTTP(response, httptest.NewRequest("GET", "/probe", nil))
		if response.Code == http.StatusGone {
			break
		}
		select {
		case <-ctx.Done():
			close(release)
			t.Fatal("board did not close admission")
		case <-time.After(time.Millisecond):
		}
	}
	select {
	case err := <-stopped:
		close(release)
		t.Fatalf("cleanup started before upload finished: %v", err)
	default:
	}
	close(release)
	if err := <-stopped; err != nil {
		t.Fatal(err)
	}
	<-finished
}
