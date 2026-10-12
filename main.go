package main

import (
	"context"
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
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func run() error {
	dev := flag.Bool("dev", false, "enable the unauthenticated local development board")
	flag.Parse()
	limits, err := app.StorageLimitsFromEnvironment("data")
	if err != nil {
		return fmt.Errorf("configure storage limits: %w", err)
	}
	authentication, err := configureAuthentication(*dev, "data", limits)
	if err != nil {
		return fmt.Errorf("configure server access: %w", err)
	}

	var store app.LibraryStore
	if uri := os.Getenv("TABLETOP_MONGODB_URI"); uri != "" {
		database := os.Getenv("TABLETOP_MONGODB_DATABASE")
		if database == "" {
			database = "tabletop"
		}
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		mongoStore, err := app.NewMongoStore(ctx, uri, database)
		if err != nil {
			cancel()
			return fmt.Errorf("connect MongoDB: %w", err)
		}
		defer mongoStore.Close(context.Background())
		err = mongoStore.LoadUnitStatuses(ctx)
		cancel()
		if err != nil {
			return fmt.Errorf("load status definitions: %w", err)
		}
		store = mongoStore
	}
	mux := http.NewServeMux()
	mux.Handle("/static/", http.StripPrefix("/static/", http.FileServer(http.Dir("static"))))
	var handler http.Handler = mux
	if authentication != nil {
		sessions, err := tables.NewWithStore("data", ".", store, limits)
		if err != nil {
			return fmt.Errorf("load tabletop sessions: %w", err)
		}
		sessions.RegisterRoutes(mux)
		authentication.RegisterRoutes(mux)
		handler = authentication.Middleware(mux)
	} else {
		var unitStore app.DocumentStore[app.UnitDefinition]
		var mapStore app.DocumentStore[app.MapDefinition]
		if store != nil {
			unitStore = store.Units("dev")
			mapStore = store.Maps("dev")
		}
		library, err := app.NewUnitLibraryWithStore("data", unitStore, limits)
		if err != nil {
			return fmt.Errorf("load unit library: %w", err)
		}
		maps, err := app.NewMapLibraryWithStore("data", mapStore, limits)
		if err != nil {
			return fmt.Errorf("load map library: %w", err)
		}
		state := app.NewState()
		sockets := handlers.NewWebSocketHandler(state)
		handlers.NewEndpointHandler(state, sockets, library, maps).RegisterRoutes(mux)
		sockets.RegisterRoutes(mux)
		sockets.StartPositionBroadcast()
		log.Print("Explicit -dev mode: running the unauthenticated development board.")
	}
	server := &http.Server{Addr: ":18080", Handler: handlers.Guard(handler), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 60 * time.Second, WriteTimeout: 120 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 32 << 10}
	return server.ListenAndServe()
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
