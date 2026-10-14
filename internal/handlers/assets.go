package handlers

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"net/http"
	"time"
)

// Versioned script and stylesheet URLs change when their contents change.
func StaticAssets(root http.FileSystem) http.Handler {
	files := http.StripPrefix("/static/", http.FileServer(root))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "public, max-age=3600")
		files.ServeHTTP(staticResponse{w}, r)
	})
}

type staticResponse struct{ http.ResponseWriter }

func (w staticResponse) WriteHeader(status int) {
	if status >= 400 {
		w.Header().Set("Cache-Control", "no-store")
	}
	w.ResponseWriter.WriteHeader(status)
}

func servePluginImage(w http.ResponseWriter, r *http.Request, data []byte) {
	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("Cache-Control", "private, max-age=3600")
	w.Header().Set("ETag", fmt.Sprintf(`"%x"`, sha256.Sum256(data)))
	http.ServeContent(w, r, "image.png", time.Time{}, bytes.NewReader(data))
}

func serveBoardImageFile(w http.ResponseWriter, r *http.Request, path string) {
	w.Header().Set("Cache-Control", "private, max-age=3600")
	http.ServeFile(staticResponse{w}, r, path)
}
