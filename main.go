package main

import (
	"encoding/json"
	"html/template"
	"log"
	"math/rand"
	"net/http"
	"strconv"

	"github.com/gorilla/websocket"
)

type UnitData struct {
	Left int
	Top  int
	ID   string
}

/*gloabls*/
var (
	upgrader = websocket.Upgrader{
		CheckOrigin: func(r *http.Request) bool {
			return true // Note: Adjust this for production environments! look at changeWebsocket.txt
		},
	}
	unitMap      = make(map[string]UnitData)
	gridTemplate = template.Must(template.ParseFiles("templates/grid.html"))
	unitTemplate = template.Must(template.ParseFiles("templates/unit.html"))
)

func wsHandler(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Println("Upgrade failed:", err)
		return
	}
	defer conn.Close()

	for {
		_, data, err := conn.ReadMessage()
		if err != nil {
			log.Println("Read error:", err)
			break
		}

		log.Printf("Received: %s", data)

		var unitData UnitData
		if err := json.Unmarshal(data, &unitData); err != nil {
			log.Println("Unmarshal error:", err)
			continue
		}
		unitMap[unitData.ID] = unitData

		logData(unitData)

		if err := conn.WriteMessage(websocket.TextMessage, data); err != nil {
			log.Println("Write error:", err)
			break
		}

	}
}

func handler(w http.ResponseWriter, r *http.Request) {
	http.ServeFile(w, r, "index.html")
}

func gridHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html")
	cells := make([]int, 64*64)
	gridTemplate.Execute(w, cells)
}

func newUnitHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html")
	data := UnitData{
		Left: 100,
		Top:  150,
		ID:   generateRandID(),
	}
	unitMap[data.ID] = data
	err := unitTemplate.ExecuteTemplate(w, "unit", data)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
	log.Println("map size: " + strconv.Itoa(len(unitMap)))
}

func existingUnitHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html")
	for key, value := range unitMap {
		data := UnitData{
			Left: value.Left,
			Top:  value.Top,
			ID:   key,
		}
		err := unitTemplate.ExecuteTemplate(w, "unit", data)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
	}
}

func generateRandID() string {
	for {
		id := "U" + strconv.Itoa(rand.Intn(100))
		_, exists := unitMap[id]
		if !exists {
			return id
		}
	}
}

func logData(unitData UnitData) {
	jsonBytes, err := json.Marshal(unitMap[unitData.ID])
	if err != nil {
		log.Printf("error marshaling: %v", err)
		return
	}
	log.Printf("%s", jsonBytes)
}
func main() {
	//load static scripts and html
	http.Handle("/static/", http.StripPrefix("/static/", http.FileServer(http.Dir("static"))))

	//regular http handlers
	http.HandleFunc("/", handler)
	http.HandleFunc("/loadExistingUnits", existingUnitHandler)
	http.HandleFunc("/grid", gridHandler)
	http.HandleFunc("/unit", newUnitHandler)

	//ws handlers
	http.HandleFunc("/ws", wsHandler)

	//handle errors via log
	log.Fatal(http.ListenAndServe(":18080", nil))
}
