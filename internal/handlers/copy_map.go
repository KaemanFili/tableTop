package handlers

import (
	"log"
	"net/http"
)

func (h *EndpointHandler) copyMap(w http.ResponseWriter, r *http.Request) {
	if !h.requireMapGM(w, r) {
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 4096)
	if err := r.ParseForm(); err != nil {
		http.Error(w, "Choose a valid copy name.", http.StatusBadRequest)
		return
	}
	if _, err := h.mapLibrary.Copy(r.PathValue("id"), r.PostForm.Get("name")); err != nil {
		log.Println("Copy map:", err)
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	h.mapPicker(w, r)
}
