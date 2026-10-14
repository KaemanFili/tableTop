package tables

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"tableTop/main/internal/app/model"
	"tableTop/main/internal/app/persistence"
	"tableTop/main/internal/handlers"
)

type validationDefaults struct {
	persistence.PluginDefaults
	validate func() error
}

func (defaults validationDefaults) Validate() error         { return defaults.validate() }
func (defaults validationDefaults) ValidateMetadata() error { return defaults.validate() }

func TestCachedValidationDoesNotBlockOtherSessions(t *testing.T) {
	library, err := persistence.NewTableLibrary(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	slow, err := library.Create("Slow", "owner")
	if err != nil {
		t.Fatal(err)
	}
	fast, err := library.Create("Fast", "owner")
	if err != nil {
		t.Fatal(err)
	}
	started, release := make(chan struct{}), make(chan struct{})
	defer close(release)
	manager := &Manager{library: library, boards: map[string]*managedBoard{
		slow.ID: {defaults: validationDefaults{validate: func() error { close(started); <-release; return nil }}},
		fast.ID: {defaults: validationDefaults{validate: func() error { return nil }}},
	}}
	go manager.handler(slow.ID)
	<-started
	ready := make(chan error, 1)
	go func() { _, err := manager.handler(fast.ID); ready <- err }()
	select {
	case err := <-ready:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(100 * time.Millisecond):
		t.Fatal("one session's validation held the global sessions lock")
	}
}

func TestBoardDeletionDrainsRequestsBeforeCleanup(t *testing.T) {
	started, release, finished := make(chan struct{}), make(chan struct{}), make(chan struct{})
	board := &managedBoard{sockets: handlers.NewWebSocketHandler(model.NewState()), handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
