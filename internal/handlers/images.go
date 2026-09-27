package handlers

import (
	"net/http"

	"tableTop/main/internal/app"
)

// Image IDs belong to artwork, so multiple units can share the same file.
// Resolve IDs here rather than accepting filesystem paths from clients.
var unitImages = map[string]string{
	app.DefaultUnitImageID: "static/images/default-unit.svg",
}

func (h *EndpointHandler) unitImage(w http.ResponseWriter, r *http.Request) {
	path, exists := unitImages[r.PathValue("id")]
	if !exists {
		http.NotFound(w, r)
		return
	}
	http.ServeFile(w, r, path)
}
