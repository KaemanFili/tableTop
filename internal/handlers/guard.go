package handlers

import (
	"net/http"
	"strings"

	"golang.org/x/time/rate"
)

// Global backstops complement per-login and per-WebSocket limits. Do not use
// forwarded IP headers for admission: the direct origin must not trust them.
func Guard(next http.Handler) http.Handler {
	requests := rate.NewLimiter(100, 200)
	slots := make(chan struct{}, 32)
	uploads := make(chan struct{}, 2)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !requests.Allow() {
			w.Header().Set("Retry-After", "1")
			http.Error(w, "The server is busy. Try again shortly.", http.StatusTooManyRequests)
			return
		}
		if !strings.EqualFold(r.Header.Get("Upgrade"), "websocket") {
			select {
			case slots <- struct{}{}:
				defer func() { <-slots }()
			default:
				http.Error(w, "The server is busy. Try again shortly.", http.StatusServiceUnavailable)
				return
			}
		}
		copyUpload := strings.HasSuffix(r.URL.Path, "/copy") && (strings.Contains(r.URL.Path, "/unit-library/") || strings.Contains(r.URL.Path, "/map-library/"))
		if r.Method == http.MethodPost && (copyUpload || strings.HasSuffix(r.URL.Path, "/unit-library") || strings.HasSuffix(r.URL.Path, "/map-library")) {
			select {
			case uploads <- struct{}{}:
				defer func() { <-uploads }()
			default:
				http.Error(w, "Two uploads are already being processed. Try again shortly.", http.StatusTooManyRequests)
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}
