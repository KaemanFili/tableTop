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
}

func NewEndpointHandler(state *app.AppState, webSocketHandler *WebSocketHandler) *EndpointHandler {
	return &EndpointHandler{
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
	mux.HandleFunc("/unit", h.newUnit)
	mux.HandleFunc("GET /spawn-menu", h.spawnMenu)
	mux.HandleFunc("GET /images/{id}", h.unitImage)
}

func (h *EndpointHandler) index(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := h.indexTemplate.ExecuteTemplate(w, "index.html", false); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func (h *EndpointHandler) spawnMenu(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	open := r.URL.Query().Get("open") == "true"
	if err := h.indexTemplate.ExecuteTemplate(w, "spawn-controls", open); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func (h *EndpointHandler) grid(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html")

	cells := make([]int, 64*64)
	if err := h.gridTemplate.Execute(w, cells); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func (h *EndpointHandler) newUnit(w http.ResponseWriter, r *http.Request) {
	imageID := app.DefaultUnitImageID
	switch r.URL.Query().Get("kind") {
	case "":
		// Keep the original /unit endpoint's neutral default.
	case "npc":
		imageID = app.NPCUnitImageID
	case "pc":
		imageID = app.PCUnitImageID
	default:
		http.Error(w, "unknown unit kind", http.StatusBadRequest)
		return
	}

	w.Header().Set("Content-Type", "text/html")
	w.Header().Set("HX-Trigger-After-Swap", "unitSpawned")

	data := h.state.AddRandomUnit(imageID)
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
