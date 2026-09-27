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
}

func NewEndpointHandler(state *app.AppState, webSocketHandler *WebSocketHandler) *EndpointHandler {
	return &EndpointHandler{
		state:            state,
		webSocketHandler: webSocketHandler,
		gridTemplate:     template.Must(template.ParseFiles("templates/grid.html")),
		unitTemplate:     template.Must(template.ParseFiles("templates/unit.html")),
	}
}

func (h *EndpointHandler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/", h.index)
	mux.HandleFunc("/loadExistingUnits", h.existingUnit)
	mux.HandleFunc("/grid", h.grid)
	mux.HandleFunc("/unit", h.newUnit)
	mux.HandleFunc("GET /images/{id}", h.unitImage)
}

func (h *EndpointHandler) index(w http.ResponseWriter, r *http.Request) {
	http.ServeFile(w, r, "index.html")
}

func (h *EndpointHandler) grid(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html")

	cells := make([]int, 64*64)
	if err := h.gridTemplate.Execute(w, cells); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func (h *EndpointHandler) newUnit(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html")

	data := h.state.AddRandomUnit()
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
