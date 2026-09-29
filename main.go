package main

import (
	"log"
	"net/http"

	"tableTop/main/internal/app"
	"tableTop/main/internal/handlers"
)

func main() {
	library, err := app.NewUnitLibrary("data")
	if err != nil {
		log.Fatal("Load unit library: ", err)
	}
	state := app.NewState()
	webSocketHandler := handlers.NewWebSocketHandler(state)
	endpointHandler := handlers.NewEndpointHandler(state, webSocketHandler, library)

	mux := http.NewServeMux()
	mux.Handle("/static/", http.StripPrefix("/static/", http.FileServer(http.Dir("static"))))

	endpointHandler.RegisterRoutes(mux)
	webSocketHandler.RegisterRoutes(mux)
	webSocketHandler.StartPositionBroadcast()

	log.Fatal(http.ListenAndServe(":18080", mux))
}
