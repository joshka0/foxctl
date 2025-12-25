package main

import (
	"embed"
	"fmt"
	"io/fs"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
)

//go:embed static/*
var staticFiles embed.FS

// NewServer creates and configures the HTTP server
func NewServer(workspace string) http.Handler {
	r := chi.NewRouter()

	// Middleware
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)
	r.Use(middleware.Compress(5))

	// SSE hub for real-time updates
	hub := NewSSEHub()
	go hub.Run()

	// Static files (embedded)
	staticFS, err := fs.Sub(staticFiles, "static")
	if err != nil {
		panic(fmt.Sprintf("failed to load static files: %v", err))
	}
	r.Handle("/static/*", http.StripPrefix("/static/", http.FileServer(http.FS(staticFS))))

	// Create handlers
	h := NewHandlers(workspace, hub)

	// Routes
	r.Get("/", h.Index)

	// Jobs routes
	r.Get("/jobs", h.JobsList)
	r.Get("/jobs/{id}", h.JobDetail)

	// Tasks routes
	r.Get("/tasks", h.TasksList)

	// Stats routes
	r.Get("/stats", h.Stats)

	// Insights routes
	r.Get("/insights", h.Insights)

	// Mailbox routes
	r.Get("/mailbox", h.Mailbox)

	// Reservations routes
	r.Get("/reservations", h.Reservations)

	// Blackboard routes
	r.Get("/blackboard", h.Blackboard)

	// SQLite browser routes
	r.Get("/sqlite", h.SQLiteBrowser)
	r.Get("/sqlite/{db}", h.SQLiteTables)
	r.Get("/sqlite/{db}/{table}", h.SQLiteData)

	// SSE endpoint
	r.Get("/events", hub.Handler)

	return r
}
