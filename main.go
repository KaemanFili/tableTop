package main

import (
	"html/template"
	"log"
	"net/http"
)

var gridTemplate = template.Must(template.New("grid").Parse(`
{{- range $i := . }}
  <div class="cell"></div>
{{- end }}
`))

func handler(w http.ResponseWriter, r *http.Request) {
	http.ServeFile(w, r, "index.html")
}

func gridHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html")
	cells := make([]int, 64*64)
	gridTemplate.Execute(w, cells)
}

func main() {
	http.Handle("/static/", http.StripPrefix("/static/", http.FileServer(http.Dir("static"))))
	http.HandleFunc("/", handler)
	http.HandleFunc("/grid", gridHandler)
	log.Fatal(http.ListenAndServe(":18080", nil))
}
