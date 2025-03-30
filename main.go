package main

import (
	"html/template"
	"log"
	"net/http"
)

var dictionary map[string]string

type UnitData struct {
	Left int
	Top  int
	ID   string
}

var gridTemplate = template.Must(template.ParseFiles("templates/grid.html"))
var unitTemplate = template.Must(template.ParseFiles("templates/unit.html"))

func handler(w http.ResponseWriter, r *http.Request) {
	http.ServeFile(w, r, "index.html")
}

func gridHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html")
	cells := make([]int, 64*64)
	gridTemplate.Execute(w, cells)
}

func circleHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html")

	data := UnitData{
		Left: 100,
		Top:  150,
		ID:   "U",
	}

	err := unitTemplate.ExecuteTemplate(w, "unit", data)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func main() {
	http.Handle("/static/", http.StripPrefix("/static/", http.FileServer(http.Dir("static"))))
	http.HandleFunc("/", handler)
	http.HandleFunc("/grid", gridHandler)
	http.HandleFunc("/unit", circleHandler)
	log.Fatal(http.ListenAndServe(":18080", nil))
}
