package handlers

import (
	"html/template"
	"log"
	"net/http"
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
}

func NewEndpointHandler(state *app.AppState, webSocketHandler *WebSocketHandler, library *app.UnitLibrary) *EndpointHandler {
	return &EndpointHandler{
		library:          library,
		state:            state,
		webSocketHandler: webSocketHandler,
		gridTemplate:     template.Must(template.ParseFiles("templates/grid.html")),
		unitTemplate:     template.Must(template.ParseFiles("templates/unit.html")),
		indexTemplate:    template.Must(template.ParseFiles("index.html", "templates/spawn-menu.html")),
	}
}

func (h *EndpointHandler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/", h.index)
	mux.HandleFunc("/loadExistingUnits", h.existingUnit)
	mux.HandleFunc("/grid", h.grid)
	mux.HandleFunc("GET /unit", h.newUnit)
	mux.HandleFunc("POST /unit", h.newUnit)
	mux.HandleFunc("GET /spawn-menu", h.spawnMenu)
	mux.HandleFunc("GET /unit-library/new", h.newUnitForm)
	mux.HandleFunc("POST /unit-library", h.createUnit)
	mux.HandleFunc("GET /images/{id}", h.unitImage)
}

func (h *EndpointHandler) index(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	if err := h.indexTemplate.ExecuteTemplate(w, "index.html", spawnMenuData{}); err != nil {
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
			definition, _ = h.library.UnitByID("goblin")
		case "pc":
			definition, _ = h.library.UnitByID("knight")
		default:
			http.Error(w, "unknown unit type", http.StatusBadRequest)
			return
		}
	}

	w.Header().Set("Content-Type", "text/html")
	w.Header().Set("HX-Trigger-After-Swap", "unitSpawned")
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

	for _, data := range h.state.UnitsSnapshot() {
		if err := h.unitTemplate.ExecuteTemplate(w, "unit", data); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	}
}
