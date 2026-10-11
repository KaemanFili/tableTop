package handlers

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"runtime"
	"testing"

	"tableTop/main/internal/app"
)

func testAssetRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("could not locate test assets")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(file), "../.."))
}

func newTestEndpointHandler(t *testing.T, state *app.AppState, ws *WebSocketHandler, library *app.UnitLibrary, maps ...*app.MapLibrary) *EndpointHandler {
	t.Helper()
	return newEndpointHandler(state, ws, library, testAssetRoot(t), maps...)
}

func newTestServer(t *testing.T, handler http.Handler) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	return server
}
