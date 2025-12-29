package main

import (
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/jkatigb/agentctl/cmd/agentctl_web/templates"
)

const workspaceCookieName = "agentctl_workspace"

// Handlers holds the HTTP handlers and their dependencies
type Handlers struct {
	defaultWorkspace string
	hub              *SSEHub
	data             *DataService
}

// NewHandlers creates a new Handlers instance
func NewHandlers(workspace string, hub *SSEHub) *Handlers {
	return &Handlers{
		defaultWorkspace: workspace,
		hub:              hub,
		data:             NewDataService(workspace),
	}
}

// getWorkspaceAndList returns current workspace and list of all workspaces
func (h *Handlers) getWorkspaceAndList(r *http.Request) (string, []templates.Workspace) {
	// Check cookie first, then fall back to default
	workspace := h.defaultWorkspace
	if cookie, err := r.Cookie(workspaceCookieName); err == nil && cookie.Value != "" {
		workspace = cookie.Value
	}

	// Get list of known workspaces
	workspaces, _ := h.data.GetWorkspaces()

	return workspace, workspaces
}

// SwitchWorkspace handles workspace switching
func (h *Handlers) SwitchWorkspace(w http.ResponseWriter, r *http.Request) {
	newWorkspace := r.URL.Query().Get("workspace")

	// Set cookie
	http.SetCookie(w, &http.Cookie{
		Name:     workspaceCookieName,
		Value:    newWorkspace,
		Path:     "/",
		MaxAge:   86400 * 365, // 1 year
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	})

	// Update the data service workspace
	h.data = NewDataService(newWorkspace)

	// Redirect to jobs page (or referer if available)
	referer := r.Header.Get("Referer")
	if referer == "" {
		referer = "/jobs"
	}
	http.Redirect(w, r, referer, http.StatusSeeOther)
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

	workspace, workspaces := h.getWorkspaceAndList(r)
	_ = templates.JobsList(jobs, workspace, workspaces).Render(r.Context(), w)
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

	workspace, workspaces := h.getWorkspaceAndList(r)
	_ = templates.JobDetailView(detail, workspace, workspaces).Render(r.Context(), w)
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

	workspace, workspaces := h.getWorkspaceAndList(r)
	_ = templates.TasksList(tasks, stats, workspace, workspaces).Render(r.Context(), w)
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

	workspace, workspaces := h.getWorkspaceAndList(r)
	_ = templates.Stats(stats, workspace, workspaces).Render(r.Context(), w)
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

	workspace, workspaces := h.getWorkspaceAndList(r)
	_ = templates.Insights(data, workspace, workspaces).Render(r.Context(), w)
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

	workspace, workspaces := h.getWorkspaceAndList(r)
	_ = templates.Mailbox(messages, actorID, workspace, workspaces).Render(r.Context(), w)
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

	workspace, workspaces := h.getWorkspaceAndList(r)
	_ = templates.Reservations(reservations, workspace, workspaces).Render(r.Context(), w)
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

	workspace, workspaces := h.getWorkspaceAndList(r)
	_ = templates.Blackboard(records, ns, topic, workspace, workspaces).Render(r.Context(), w)
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

	workspace, workspaces := h.getWorkspaceAndList(r)
	_ = templates.SQLiteBrowser(databases, workspace, workspaces).Render(r.Context(), w)
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

	workspace, workspaces := h.getWorkspaceAndList(r)
	_ = templates.SQLiteTables(tables, db, workspace, workspaces).Render(r.Context(), w)
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

	workspace, workspaces := h.getWorkspaceAndList(r)
	_ = templates.SQLiteData(columns, rows, db, table, len(rows), workspace, workspaces).Render(r.Context(), w)
}

// Search renders the semantic search view
func (h *Handlers) Search(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query().Get("q")
	limit := parseLimit(r.URL.Query().Get("limit"), 50)
	rerank := r.URL.Query().Get("rerank") == "true"
	scope := r.URL.Query().Get("scope")

	var scopes []string
	if scope != "" && scope != "all" {
		scopes = []string{scope}
	}

	results, stats, err := h.data.GetSearch(query, limit, rerank, scopes)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	if r.Header.Get("HX-Request") == "true" {
		_ = templates.SearchContent(results, stats, query, rerank, scopes).Render(r.Context(), w)
		return
	}

	workspace, workspaces := h.getWorkspaceAndList(r)
	_ = templates.Search(results, stats, query, rerank, scopes, workspace, workspaces).Render(r.Context(), w)
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
