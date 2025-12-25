package main

import (
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/jkatigb/agentctl/cmd/agentctl_web/templates"
)

// Handlers holds the HTTP handlers and their dependencies
type Handlers struct {
	workspace string
	hub       *SSEHub
	data      *DataService
}

// NewHandlers creates a new Handlers instance
func NewHandlers(workspace string, hub *SSEHub) *Handlers {
	return &Handlers{
		workspace: workspace,
		hub:       hub,
		data:      NewDataService(workspace),
	}
}

// Index redirects to jobs list
func (h *Handlers) Index(w http.ResponseWriter, r *http.Request) {
	http.Redirect(w, r, "/jobs", http.StatusTemporaryRedirect)
}

// JobsList renders the jobs list view
func (h *Handlers) JobsList(w http.ResponseWriter, r *http.Request) {
	state := r.URL.Query().Get("state")
	limit := parseLimit(r.URL.Query().Get("limit"), 50)

	jobs, err := h.data.GetJobs(state, limit)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Check if this is an HTMX request for partial update
	if r.Header.Get("HX-Request") == "true" {
		_ = templates.JobsTable(jobs).Render(r.Context(), w)
		return
	}

	_ = templates.JobsList(jobs).Render(r.Context(), w)
}

// JobDetail renders a single job detail view
func (h *Handlers) JobDetail(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	detail, err := h.data.GetJobDetail(id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	if r.Header.Get("HX-Request") == "true" {
		_ = templates.JobDetailViewContent(detail).Render(r.Context(), w)
		return
	}

	_ = templates.JobDetailView(detail).Render(r.Context(), w)
}

// TasksList renders the tasks view
func (h *Handlers) TasksList(w http.ResponseWriter, r *http.Request) {
	limit := parseLimit(r.URL.Query().Get("limit"), 50)

	tasks, err := h.data.GetTasks(limit)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Compute stats
	stats := templates.TaskStats{Total: len(tasks)}
	for _, t := range tasks {
		switch t.Status {
		case "pending":
			stats.Pending++
		case "in_progress":
			stats.InProgress++
		case "completed":
			stats.Completed++
		}
	}

	if r.Header.Get("HX-Request") == "true" {
		_ = templates.TasksTable(tasks).Render(r.Context(), w)
		return
	}

	_ = templates.TasksList(tasks, stats).Render(r.Context(), w)
}

// Stats renders the stats dashboard
func (h *Handlers) Stats(w http.ResponseWriter, r *http.Request) {
	stats, err := h.data.GetStats()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	if r.Header.Get("HX-Request") == "true" {
		_ = templates.StatsContent(stats).Render(r.Context(), w)
		return
	}

	_ = templates.Stats(stats).Render(r.Context(), w)
}

// Insights renders the insights view
func (h *Handlers) Insights(w http.ResponseWriter, r *http.Request) {
	data, err := h.data.GetInsights()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	if r.Header.Get("HX-Request") == "true" {
		_ = templates.InsightsContent(data).Render(r.Context(), w)
		return
	}

	_ = templates.Insights(data).Render(r.Context(), w)
}

// Mailbox renders the mailbox view
func (h *Handlers) Mailbox(w http.ResponseWriter, r *http.Request) {
	actorID := r.URL.Query().Get("actor")
	limit := parseLimit(r.URL.Query().Get("limit"), 50)

	messages, err := h.data.GetMailbox(actorID, limit)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	if r.Header.Get("HX-Request") == "true" {
		_ = templates.MailboxTable(messages).Render(r.Context(), w)
		return
	}

	_ = templates.Mailbox(messages, actorID).Render(r.Context(), w)
}

// Reservations renders the reservations view
func (h *Handlers) Reservations(w http.ResponseWriter, r *http.Request) {
	reservations, err := h.data.GetReservations()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	if r.Header.Get("HX-Request") == "true" {
		_ = templates.ReservationsTable(reservations).Render(r.Context(), w)
		return
	}

	_ = templates.Reservations(reservations).Render(r.Context(), w)
}

// Blackboard renders the blackboard view
func (h *Handlers) Blackboard(w http.ResponseWriter, r *http.Request) {
	ns := r.URL.Query().Get("ns")
	topic := r.URL.Query().Get("topic")
	limit := parseLimit(r.URL.Query().Get("limit"), 50)

	records, err := h.data.GetBlackboard(ns, topic, limit)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	if r.Header.Get("HX-Request") == "true" {
		_ = templates.BlackboardTable(records).Render(r.Context(), w)
		return
	}

	_ = templates.Blackboard(records, ns, topic).Render(r.Context(), w)
}

// SQLiteBrowser renders the SQLite database list
func (h *Handlers) SQLiteBrowser(w http.ResponseWriter, r *http.Request) {
	databases, err := h.data.GetSQLiteDatabases()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	if r.Header.Get("HX-Request") == "true" {
		_ = templates.SQLiteDatabaseList(databases).Render(r.Context(), w)
		return
	}

	_ = templates.SQLiteBrowser(databases).Render(r.Context(), w)
}

// SQLiteTables renders the tables for a database
func (h *Handlers) SQLiteTables(w http.ResponseWriter, r *http.Request) {
	db := chi.URLParam(r, "db")

	tables, err := h.data.GetSQLiteTables(db)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	if r.Header.Get("HX-Request") == "true" {
		_ = templates.SQLiteTableList(tables, db).Render(r.Context(), w)
		return
	}

	_ = templates.SQLiteTables(tables, db).Render(r.Context(), w)
}

// SQLiteData renders the data for a table
func (h *Handlers) SQLiteData(w http.ResponseWriter, r *http.Request) {
	db := chi.URLParam(r, "db")
	table := chi.URLParam(r, "table")
	limit := parseLimit(r.URL.Query().Get("limit"), 100)

	columns, rows, err := h.data.GetSQLiteData(db, table, limit)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	if r.Header.Get("HX-Request") == "true" {
		_ = templates.SQLiteDataTable(columns, rows, db, table).Render(r.Context(), w)
		return
	}

	_ = templates.SQLiteData(columns, rows, db, table, len(rows)).Render(r.Context(), w)
}

func parseLimit(s string, def int) int {
	if s == "" {
		return def
	}
	n, err := strconv.Atoi(s)
	if err != nil || n <= 0 {
		return def
	}
	return n
}
