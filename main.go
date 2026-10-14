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

	"tableTop/main/internal/app/model"
	"tableTop/main/internal/app/persistence"
	"tableTop/main/internal/auth"
	"tableTop/main/internal/handlers"
	"tableTop/main/internal/plugins"
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
	limits, err := persistence.StorageLimitsFromEnvironment("data")
	if err != nil {
		return fmt.Errorf("configure storage limits: %w", err)
	}
	authentication, err := configureAuthentication(*dev, "data", limits)
	if err != nil {
		return fmt.Errorf("configure server access: %w", err)
	}

	mongoStore, err := connectMongoStore()
	if err != nil {
		return err
	}
	var store persistence.LibraryStore
	if mongoStore != nil {
		defer mongoStore.Close(context.Background())
		store = mongoStore
	}
	mux := http.NewServeMux()
	mux.Handle("/static/", handlers.StaticAssets(http.Dir("static")))
	var handler http.Handler = mux
	if authentication != nil {
		if err := registerAuthenticatedRoutes(mux, authentication, store, limits); err != nil {
			return err
		}
		handler = authentication.Middleware(mux)
	} else if err := registerDevelopmentRoutes(mux, store, limits); err != nil {
		return err
	}
	server := &http.Server{
		Addr:              ":18080",
		Handler:           handlers.Guard(handler),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       60 * time.Second,
		WriteTimeout:      120 * time.Second,
		IdleTimeout:       60 * time.Second,
		MaxHeaderBytes:    32 << 10,
	}
	return server.ListenAndServe()
}

func connectMongoStore() (*persistence.MongoStore, error) {
	uri := os.Getenv("TABLETOP_MONGODB_URI")
	if uri == "" {
		return nil, nil
	}
	database := os.Getenv("TABLETOP_MONGODB_DATABASE")
	if database == "" {
		database = "tabletop"
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	store, err := persistence.NewMongoStore(ctx, uri, database)
	if err != nil {
		return nil, fmt.Errorf("connect MongoDB: %w", err)
	}
	return store, nil
}

func registerAuthenticatedRoutes(mux *http.ServeMux, authentication *auth.Manager, store persistence.LibraryStore, limits *persistence.StorageLimits) error {
	sessions, err := tables.NewWithStore("data", ".", store, limits)
	if err != nil {
		return fmt.Errorf("load tabletop sessions: %w", err)
	}
	sessions.RegisterRoutes(mux)
	authentication.RegisterRoutes(mux)
	return nil
}

func registerDevelopmentRoutes(mux *http.ServeMux, store persistence.LibraryStore, limits *persistence.StorageLimits) error {
	defaults, err := plugins.Development("data", ".", store, limits)
	if err != nil {
		return fmt.Errorf("initialize development content: %w", err)
	}
	var unitStore persistence.DocumentStore[model.UnitDefinition]
	var mapStore persistence.DocumentStore[model.MapDefinition]
	if store != nil {
		unitStore = store.Units("dev")
		mapStore = store.Maps("dev")
	}
	library, err := persistence.NewUnitLibraryWithStore("data", unitStore, limits)
	if err != nil {
		return fmt.Errorf("load unit library: %w", err)
	}
	maps, err := persistence.NewMapLibraryWithStore("data", mapStore, limits)
	if err != nil {
		return fmt.Errorf("load map library: %w", err)
	}
	library.SetDefaults(defaults)
	maps.SetDefaults(defaults)
	statuses, err := persistence.EffectiveStatuses("data", "dev", store, limits, defaults)
	if err != nil {
		return fmt.Errorf("load statuses: %w", err)
	}
	state, err := model.NewStateWithStatuses(statuses)
	if err != nil {
		return err
	}
	sockets := handlers.NewWebSocketHandler(state)
	handlers.NewEndpointHandler(state, sockets, library, maps).RegisterRoutes(mux)
	sockets.RegisterRoutes(mux)
	sockets.StartPositionBroadcast()
	log.Print("Explicit -dev mode: running the unauthenticated development board.")
	return nil
}

func configureAuthentication(dev bool, dir string, limits *persistence.StorageLimits) (*auth.Manager, error) {
	if dev {
		return nil, nil
	}
	admins := strings.FieldsFunc(os.Getenv("TABLETOP_ADMIN_USER_IDS"), func(r rune) bool { return r == ',' || r == ' ' || r == '\n' })
	access, err := persistence.NewAccessLibrary(dir, admins, limits)
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
