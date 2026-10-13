package handlers

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"tableTop/main/internal/auth"

	"tableTop/main/internal/app"
)

const maxMapImageBytes = 10 << 20

type mapEditorData struct {
	app.MapDefinition
	Error string
	Saved bool
}

func (h *EndpointHandler) requireMapGM(w http.ResponseWriter, r *http.Request) bool {
	w.Header().Set("Cache-Control", "no-store")
	if !auth.IsGM(r) {
		http.Error(w, "Only a GM can manage maps.", http.StatusForbidden)
		return false
	}
	if h.mapLibrary == nil {
		http.Error(w, "Map storage is unavailable. Restart the server.", http.StatusServiceUnavailable)
		return false
	}
	return true
}

func (h *EndpointHandler) renderMapEditor(w http.ResponseWriter, data mapEditorData) {
	var rendered bytes.Buffer
	if err := h.indexTemplate.ExecuteTemplate(&rendered, "map-editor", data); err != nil {
		log.Println("Render map editor:", err)
		http.Error(w, "The map form could not be opened. Restart the server.", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write(rendered.Bytes())
}

func (h *EndpointHandler) newMapForm(w http.ResponseWriter, r *http.Request) {
	if !h.requireMapGM(w, r) {
		return
	}
	h.renderMapEditor(w, mapEditorData{MapDefinition: app.MapDefinition{Width: 20, Height: 20}})
}

func (h *EndpointHandler) createMap(w http.ResponseWriter, r *http.Request) {
	if !h.requireMapGM(w, r) {
		return
	}
	data := mapEditorData{MapDefinition: app.MapDefinition{Width: 20, Height: 20}}
	fail := func(message string) {
		data.Error = message
		h.renderMapEditor(w, data)
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxMapImageBytes+(64<<10))
	if err := r.ParseMultipartForm(maxMapImageBytes + (64 << 10)); err != nil {
		if r.MultipartForm != nil {
			r.MultipartForm.RemoveAll()
		}
		fail("Choose a PNG, JPEG, or GIF map image up to 10 MB.")
		return
	}
	defer r.MultipartForm.RemoveAll()
	if err := parseMapForm(r, &data.MapDefinition); err != nil {
		fail(err.Error())
		return
	}
	image, err := readMapImage(r)
	if err != nil {
		fail(err.Error())
		return
	}
	ws := h.webSocketHandler
	ws.boardMutex.Lock()
	defer ws.boardMutex.Unlock()
	definition, err := h.mapLibrary.Create(data.MapDefinition, image)
	if err != nil {
		log.Println("Save map:", err)
		if errors.Is(err, app.ErrStorageLimit) {
			fail(err.Error())
			return
		}
		fail("The map could not be saved. Please try again.")
		return
	}
	ws.currentMap = &definition
	message, _ := json.Marshal(struct {
		Type string            `json:"type"`
		Map  app.MapDefinition `json:"map"`
	}{"boardMapChanged", definition})
	ws.broadcastUnitsLocked(message)
	w.Header().Set("HX-Trigger", "mapSaved")
	h.renderMapEditor(w, mapEditorData{MapDefinition: definition, Saved: true})
}

// Update the definition before validation so errors preserve submitted values.
func parseMapForm(r *http.Request, definition *app.MapDefinition) error {
	definition.Name = strings.TrimSpace(r.FormValue("name"))
	for name, value := range map[string]*float64{
		"width": &definition.Width, "height": &definition.Height,
		"offsetX": &definition.OffsetX, "offsetY": &definition.OffsetY,
	} {
		parsed, err := strconv.ParseFloat(r.FormValue(name), 64)
		if err != nil {
			return errors.New("Enter valid grid dimensions and position.")
		}
		*value = parsed
	}
	return definition.Validate()
}

func readMapImage(r *http.Request) ([]byte, error) {
	file, header, err := r.FormFile("image")
	if err != nil {
		return nil, errors.New("Choose a map image.")
	}
	defer file.Close()
	if header.Size > maxMapImageBytes {
		return nil, errors.New("The map image must be 10 MB or smaller.")
	}
	upload, err := io.ReadAll(io.LimitReader(file, maxMapImageBytes+1))
	if err != nil || len(upload) > maxMapImageBytes {
		return nil, errors.New("The map image could not be read or is larger than 10 MB.")
	}
	return normalizeUnitImage(upload)
}

func (h *EndpointHandler) mapImage(w http.ResponseWriter, r *http.Request) {
	if h.mapLibrary == nil {
		http.NotFound(w, r)
		return
	}
	path, data, err := h.mapLibrary.Artwork(r.PathValue("id"))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			http.NotFound(w, r)
		} else {
			http.Error(w, "The map image is unavailable.", http.StatusServiceUnavailable)
		}
		return
	}
	if data != nil {
		w.Header().Set("Content-Type", "image/png")
		w.Write(data)
		return
	}
	http.ServeFile(w, r, path)
}

func (h *EndpointHandler) saveBoard(w http.ResponseWriter, r *http.Request) {
	if !h.requireMapGM(w, r) {
		return
	}
	ws := h.webSocketHandler
	ws.boardMutex.Lock()
	defer ws.boardMutex.Unlock()
	if ws.currentMap == nil {
		http.Error(w, "Create or load a map before saving.", http.StatusConflict)
		return
	}
	board := app.SavedBoard{Units: h.state.Units(), Fog: ws.fogMarks}
	for _, stroke := range ws.strokes {
		// A saved stroke is complete even if its author is still drawing live.
		stroke.Finished = true
		board.Strokes = append(board.Strokes, stroke)
	}
	for _, ruler := range ws.rulers {
		board.Rulers = append(board.Rulers, ruler)
	}
	saved, err := h.mapLibrary.SaveMap(*ws.currentMap, board)
	if err != nil {
		log.Println("Save board:", err)
		if errors.Is(err, app.ErrCapacity) {
			http.Error(w, err.Error(), http.StatusConflict)
			return
		}
		if errors.Is(err, app.ErrStorageLimit) {
			http.Error(w, err.Error(), http.StatusInsufficientStorage)
			return
		}
		http.Error(w, "The map could not be saved. Please try again.", http.StatusInternalServerError)
		return
	}
	if saved.ID != ws.currentMap.ID {
		saved.Board = nil
		ws.currentMap = &saved
		ws.broadcastValueLocked(map[string]any{"type": "boardMapChanged", "map": saved})
	}
	w.Header().Set("X-Map-ID", saved.ID)
	w.WriteHeader(http.StatusNoContent)
}
