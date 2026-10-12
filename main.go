package main

import (
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"tableTop/main/internal/app"
	"tableTop/main/internal/auth"
	"tableTop/main/internal/handlers"
	"tableTop/main/internal/tables"
)

func main() {
	dev := flag.Bool("dev", false, "enable the unauthenticated local development board")
	flag.Parse()
	limits, err := app.StorageLimitsFromEnvironment("data")
	if err != nil {
		log.Fatal("Configure storage limits: ", err)
	}
	authentication, err := configureAuthentication(*dev, "data", limits)
	if err != nil {
		log.Fatal("Configure server access: ", err)
	}

	mux := http.NewServeMux()
	mux.Handle("/static/", http.StripPrefix("/static/", http.FileServer(http.Dir("static"))))
	var handler http.Handler = mux
	if authentication != nil {
		sessions, err := tables.New("data", ".", limits)
		if err != nil {
			log.Fatal("Load tabletop sessions: ", err)
		}
		sessions.RegisterRoutes(mux)
		authentication.RegisterRoutes(mux)
		handler = authentication.Middleware(mux)
	} else {
		library, err := app.NewUnitLibrary("data", limits)
		if err != nil {
			log.Fatal("Load unit library: ", err)
		}
		maps, err := app.NewMapLibrary("data", limits)
		if err != nil {
			log.Fatal("Load map library: ", err)
		}
		state := app.NewState()
		sockets := handlers.NewWebSocketHandler(state)
		handlers.NewEndpointHandler(state, sockets, library, maps).RegisterRoutes(mux)
		sockets.RegisterRoutes(mux)
		sockets.StartPositionBroadcast()
		log.Print("Explicit -dev mode: running the unauthenticated development board.")
	}
	server := &http.Server{Addr: ":18080", Handler: handlers.Guard(handler), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 60 * time.Second, WriteTimeout: 120 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 32 << 10}
	log.Fatal(server.ListenAndServe())
}

func configureAuthentication(dev bool, dir string, limits *app.StorageLimits) (*auth.Manager, error) {
	if dev {
		return nil, nil
	}
	admins := strings.FieldsFunc(os.Getenv("TABLETOP_ADMIN_USER_IDS"), func(r rune) bool { return r == ',' || r == ' ' || r == '\n' })
	access, err := app.NewAccessLibrary(dir, admins, limits)
	if err != nil {
		return nil, err
	}
	manager, err := auth.FromEnvironment(access)
	if err != nil {
		return nil, err
	}
	if manager == nil {
		return nil, fmt.Errorf("authentication is required; configure OAuth settings, or explicitly use -dev for a local development board")
	}
	return manager, nil
}
