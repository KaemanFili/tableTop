package handlers

import (
	"encoding/json"
	"html/template"
	"log"
	"net/http"
	"os"
	"strconv"

	"tableTop/main/internal/app"
)

type EndpointHandler struct {
	state            *app.AppState
	webSocketHandler *WebSocketHandler
	gridTemplate     *template.Template
	unitTemplate     *template.Template
	indexTemplate    *template.Template
	library          *app.UnitLibrary
	mapLibrary       *app.MapLibrary
	assetRoot        string
}

func NewEndpointHandler(state *app.AppState, webSocketHandler *WebSocketHandler, library *app.UnitLibrary, maps ...*app.MapLibrary) *EndpointHandler {
	return newEndpointHandler(state, webSocketHandler, library, ".", maps...)
}

func newEndpointHandler(state *app.AppState, webSocketHandler *WebSocketHandler, library *app.UnitLibrary, assetRoot string, maps ...*app.MapLibrary) *EndpointHandler {
	assets := os.DirFS(assetRoot)
	handler := &EndpointHandler{
		library:          library,
		assetRoot:        assetRoot,
		state:            state,
		webSocketHandler: webSocketHandler,
		gridTemplate:     template.Must(template.ParseFS(assets, "templates/grid.html")),
		unitTemplate:     template.Must(template.ParseFS(assets, "templates/unit.html")),
		indexTemplate:    template.Must(template.ParseFS(assets, "index.html", "templates/spawn-menu.html", "templates/utilities.html", "templates/map-editor.html", "templates/map-picker.html")),
	}
	if len(maps) > 0 {
		handler.mapLibrary = maps[0]
	}
	return handler
}

func (h *EndpointHandler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/", h.index)
	mux.HandleFunc("/loadExistingUnits", h.existingUnit)
	mux.HandleFunc("/grid", h.grid)
	mux.HandleFunc("/unit", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Allow", "POST")
		http.Error(w, "Use POST to spawn a unit.", http.StatusMethodNotAllowed)
	})
	mux.HandleFunc("POST /unit", h.newUnit)
	mux.HandleFunc("DELETE /unit/{id}", h.removeUnit)
	mux.HandleFunc("POST /unit/{id}/size", h.resizeUnit)
	mux.HandleFunc("POST /unit/{id}/status", h.setUnitStatus)
	mux.HandleFunc("POST /board/ping", h.pingBoard)
	mux.HandleFunc("POST /board/clear", h.clearBoard)
	mux.HandleFunc("POST /board/save", h.saveBoard)
	mux.HandleFunc("GET /map-library/new", h.newMapForm)
	mux.HandleFunc("GET /map-library", h.mapPicker)
	mux.HandleFunc("POST /map-library/{id}/load", h.loadMap)
	mux.HandleFunc("POST /map-library", h.createMap)
	mux.HandleFunc("DELETE /map-library/{id}", h.deleteMap)
	mux.HandleFunc("GET /maps/images/{id}", h.mapImage)
	mux.HandleFunc("GET /spawn-menu", h.spawnMenu)
	mux.HandleFunc("GET /unit-library/new", h.newUnitForm)
	mux.HandleFunc("POST /unit-library", h.createUnit)
	mux.HandleFunc("DELETE /unit-library/{id}", h.deleteLibraryUnit)
	mux.HandleFunc("GET /images/{id}", h.unitImage)
}

func (h *EndpointHandler) index(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	sizes, _ := json.Marshal(app.UnitSizes())
	data := struct {
		spawnMenuData
		UnitSizes    template.JS
		UnitStatuses []app.UnitStatusOption
		IsGM         bool
	}{UnitSizes: template.JS(sizes), UnitStatuses: app.UnitStatuses(), IsGM: r.URL.Query().Get("gm") == "true"}
	if err := h.indexTemplate.ExecuteTemplate(w, "index.html", data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func (h *EndpointHandler) spawnMenu(w http.ResponseWriter, r *http.Request) {
	h.renderSpawnMenu(w, h.menuData(r.URL.Query().Get("open") == "true"))
}

func (h *EndpointHandler) grid(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html")

	cells := make([]int, 64*64)
	if err := h.gridTemplate.Execute(w, cells); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func (h *EndpointHandler) newUnit(w http.ResponseWriter, r *http.Request) {
	definition := app.UnitDefinition{ImageID: app.DefaultUnitImageID}
	if id := r.URL.Query().Get("unitID"); id != "" {
		var exists bool
		definition, exists = h.library.UnitByID(id)
		if !exists {
			http.Error(w, "unit not found", http.StatusNotFound)
			return
		}
	} else {
		switch r.URL.Query().Get("unitType") {
		case "":
			// Keep the original /unit endpoint's neutral default.
		case "npc":
			definition = app.UnitDefinition{ImageID: app.NPCUnitImageID, Size: app.SizeSmall, ImageFit: app.DefaultImageFit()}
		case "pc":
			definition = app.UnitDefinition{ImageID: app.PCUnitImageID, Size: app.SizeMedium, ImageFit: app.DefaultImageFit()}
		default:
			http.Error(w, "unknown unit type", http.StatusBadRequest)
			return
		}
	}

	w.Header().Set("Content-Type", "text/html")
	w.Header().Set("HX-Trigger", "unitSpawned")
	w.Header().Set("Cache-Control", "no-store")

	data := h.state.AddUnit(definition)
	if err := h.unitTemplate.ExecuteTemplate(w, "unit", data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	h.webSocketHandler.BroadcastUnitState()
	log.Println("map size: " + strconv.Itoa(h.state.UnitCount()))
}

func (h *EndpointHandler) existingUnit(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html")
	w.Header().Set("Cache-Control", "no-store")

	for _, data := range h.state.Units() {
		if err := h.unitTemplate.ExecuteTemplate(w, "unit", data); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	}
}

func (h *EndpointHandler) removeUnit(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if h.state.RemoveUnit(id) {
		h.webSocketHandler.BroadcastUnitRemoved(id)
	}
	// Repeated removal succeeds, including when another client removed it first.
	trigger, _ := json.Marshal(map[string]any{"unitRemoved": map[string]string{"id": id}})
	w.Header().Set("HX-Trigger", string(trigger))
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusNoContent)
}
