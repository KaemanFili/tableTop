package handlers

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"testing/fstest"
)

func TestAssetCaching(t *testing.T) {
	files := StaticAssets(http.FS(fstest.MapFS{"style.css": &fstest.MapFile{Data: []byte("body {}")}}))
	response := httptest.NewRecorder()
	files.ServeHTTP(response, httptest.NewRequest("GET", "/static/style.css?v=65", nil))
	if response.Code != 200 || response.Header().Get("Cache-Control") != "public, max-age=3600" {
		t.Fatal("static asset cannot be cached")
	}
	missing := httptest.NewRecorder()
	files.ServeHTTP(missing, httptest.NewRequest("GET", "/static/missing.css", nil))
	if missing.Code != 404 || missing.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("missing asset was cacheable")
	}
	image := httptest.NewRecorder()
	servePluginImage(image, httptest.NewRequest("GET", "/images/portrait", nil), testPNG(t, 2, 2))
	request := httptest.NewRequest("GET", "/images/portrait", nil)
	request.Header.Set("If-None-Match", image.Header().Get("ETag"))
	cached := httptest.NewRecorder()
	servePluginImage(cached, request, testPNG(t, 2, 2))
	if cached.Code != 304 || cached.Body.Len() != 0 || cached.Header().Get("Cache-Control") != "private, max-age=3600" {
		t.Fatal("artwork cache did not revalidate privately")
	}
	path := filepath.Join(t.TempDir(), "token.png")
	if err := os.WriteFile(path, testPNG(t, 2, 2), 0600); err != nil {
		t.Fatal(err)
	}
	uploaded := httptest.NewRecorder()
	serveBoardImageFile(uploaded, httptest.NewRequest("GET", "/images/uploaded", nil), path)
	if uploaded.Code != 200 || uploaded.Header().Get("Cache-Control") != "private, max-age=3600" {
		t.Fatal("uploaded board image is not cached privately")
	}
	absent := httptest.NewRecorder()
	serveBoardImageFile(absent, httptest.NewRequest("GET", "/images/absent", nil), path+".missing")
	if absent.Code != 404 || absent.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("missing board image was cacheable")
	}
}
