package handlers

import (
	"errors"
	"net/http"
	"os"
	"path/filepath"

	"tableTop/main/internal/app/model"
)

// Image IDs belong to artwork, so multiple units can share the same file.
// Resolve IDs here rather than accepting filesystem paths from clients.
var unitImages = map[string]string{
	model.DefaultUnitImageID: "static/images/default-unit.svg",
	model.NPCUnitImageID:     "static/images/goblin.png",
	model.PCUnitImageID:      "static/images/knight.png",
}

func (h *EndpointHandler) unitImage(w http.ResponseWriter, r *http.Request) {
	if data, err := h.library.PluginArtwork(r.PathValue("id")); err == nil {
		servePluginImage(w, r, data)
		return
	} else if !errors.Is(err, os.ErrNotExist) {
		http.Error(w, "The plugin image is unavailable.", http.StatusServiceUnavailable)
		return
	}
	path, exists := unitImages[r.PathValue("id")]
	if exists {
		path = filepath.Join(h.assetRoot, path)
	} else {
		path, exists = h.library.ImagePath(r.PathValue("id"))
	}
	if !exists {
		http.NotFound(w, r)
		return
	}
	serveBoardImageFile(w, r, path)
}
