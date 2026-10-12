package main

import (
	"log"
	"net/http"

	"tableTop/main/internal/app"
	"tableTop/main/internal/auth"
	"tableTop/main/internal/handlers"
	"tableTop/main/internal/tables"
)

func main() {
	authentication, err := auth.FromEnvironment()
	if err != nil {
		log.Fatal("Configure Discord sign-in: ", err)
	}

	mux := http.NewServeMux()
	mux.Handle("/static/", http.StripPrefix("/static/", http.FileServer(http.Dir("static"))))
	var handler http.Handler = mux
	if authentication != nil {
		sessions, err := tables.New("data", ".")
		if err != nil {
			log.Fatal("Load tabletop sessions: ", err)
		}
		sessions.RegisterRoutes(mux)
		authentication.RegisterRoutes(mux)
		handler = authentication.Middleware(mux)
	} else {
		library, err := app.NewUnitLibrary("data")
		if err != nil {
			log.Fatal("Load unit library: ", err)
		}
		maps, err := app.NewMapLibrary("data")
		if err != nil {
			log.Fatal("Load map library: ", err)
		}
		state := app.NewState()
		sockets := handlers.NewWebSocketHandler(state)
		handlers.NewEndpointHandler(state, sockets, library, maps).RegisterRoutes(mux)
		sockets.RegisterRoutes(mux)
		sockets.StartPositionBroadcast()
		log.Print("Discord sign-in is not configured; running in local development mode.")
	}
	log.Fatal(http.ListenAndServe(":18080", handler))
}
