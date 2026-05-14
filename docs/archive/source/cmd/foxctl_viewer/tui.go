//go:build archived

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	jobstore "github.com/joshka0/foxctl/internal/storage/jobs"
)

type model struct {
	mode          viewMode
	jobs          []jobSummary
	tasks         []taskSummary
	messages      []mailboxMessage
	reservations  []reservation
	insights      *insightsData
	workspace     string
	cursor        int
	selected      *jobDetail
	previewDetail *jobDetail
	activeTab     detailTab
	viewport      viewport.Model
	spinner       spinner.Model
	loading       bool
	ready         bool
	width         int
	height        int

	// Unified selection - cursors per view
	taskCursor        int
	messageCursor     int
	reservationCursor int

	// Configurable actor for mailbox
	actorID string

	// Status/error tracking
	lastError string

	// Detail views for other types
	selectedTask    *taskSummary
	selectedMessage *mailboxMessage

	// Stats view data
	stats *jobStats

	// Blackboard view data
	bbRecords  []blackboardRecord
	bbNS       string
	bbTopic    string
	bbCursor   int
	selectedBB *blackboardRecord

	// SQLite browser state
	sqliteDatabases     []sqliteDBInfo
	sqliteSelectedDB    int
	sqliteTables        []sqliteTableInfo
	sqliteSelectedTable int
	sqliteRows          []sqliteRowData
	sqliteColumns       []string
	sqlitePane          sqlitePane
	sqliteSchema        string

	// Search state
	searchQuery    string
	searchResults  []searchResult
	searchCursor   int
	searchStats    *searchStats
	searchRerank   bool
	searchScopes   []string
	searchLimit    int
	selectedResult *searchResult
}

type (
	mailboxLoadedMsg      []mailboxMessage
	reservationsLoadedMsg []reservation
	insightsLoadedMsg     *insightsData
	statsLoadedMsg        *jobStats
	blackboardLoadedMsg   []blackboardRecord
)

// SQLite browser messages
type (
	sqliteDatabasesLoadedMsg []sqliteDBInfo
	sqliteTablesLoadedMsg    []sqliteTableInfo
	sqliteRowsLoadedMsg      struct {
		columns []string
		rows    []sqliteRowData
	}
	sqliteSchemaLoadedMsg string
)

// Search messages
type searchLoadedMsg struct {
	results []searchResult
	stats   *searchStats
	err     error
}

func loadMailboxCmd(workspace, actorID string) tea.Cmd {
	return func() tea.Msg {
		msgs, err := fetchMailbox(workspace, actorID, 50)
		if err != nil {
			return mailboxLoadedMsg([]mailboxMessage{})
		}
		return mailboxLoadedMsg(msgs)
	}
}

func loadReservationsCmd(workspace string) tea.Cmd {
	return func() tea.Msg {
		res, err := fetchReservations(workspace)
		if err != nil {
			return reservationsLoadedMsg([]reservation{})
		}
		return reservationsLoadedMsg(res)
	}
}

func loadInsightsCmd(workspace string) tea.Cmd {
	return func() tea.Msg {
		data, err := fetchInsights(workspace)
		if err != nil {
			return insightsLoadedMsg(nil)
		}
		return insightsLoadedMsg(data)
	}
}

func loadStatsCmd(jobs []jobSummary) tea.Cmd {
	return func() tea.Msg {
		stats := computeJobStats(jobs)
		return statsLoadedMsg(stats)
	}
}

func loadBlackboardCmd(ns, topic string, limit int) tea.Cmd {
	return func() tea.Msg {
		records, err := fetchBlackboard(ns, topic, limit)
		if err != nil {
			return blackboardLoadedMsg([]blackboardRecord{})
		}
		return blackboardLoadedMsg(records)
	}
}

// SQLite browser commands
func loadSQLiteDatabasesCmd() tea.Cmd {
	return func() tea.Msg {
		dbs, err := discoverDatabases()
		if err != nil {
			return sqliteDatabasesLoadedMsg([]sqliteDBInfo{})
		}
		return sqliteDatabasesLoadedMsg(dbs)
	}
}

func loadSQLiteTablesCmd(dbPath string) tea.Cmd {
	return func() tea.Msg {
		tables, err := listTables(dbPath)
		if err != nil {
			return sqliteTablesLoadedMsg([]sqliteTableInfo{})
		}
		return sqliteTablesLoadedMsg(tables)
	}
}

func loadSQLiteRowsCmd(dbPath, tableName string, limit int) tea.Cmd {
	return func() tea.Msg {
		columns, rows, err := fetchTableRows(dbPath, tableName, limit)
		if err != nil {
			return sqliteRowsLoadedMsg{columns: nil, rows: nil}
		}
		return sqliteRowsLoadedMsg{columns: columns, rows: rows}
	}
}

func loadSQLiteSchemaCmd(dbPath, tableName string) tea.Cmd {
	return func() tea.Msg {
		schema, err := getTableSchema(dbPath, tableName)
		if err != nil {
			return sqliteSchemaLoadedMsg("")
		}
		return sqliteSchemaLoadedMsg(schema)
	}
}

// loadSearchCmd runs semantic search and returns results.
func loadSearchCmd(query string, limit int, rerank bool, scopes []string) tea.Cmd {
	return func() tea.Msg {
		results, stats, err := fetchSearchResults(query, limit, rerank, scopes)
		return searchLoadedMsg{results: results, stats: stats, err: err}
	}
}

type jobPreviewLoadedMsg struct {
	detail *jobDetail
}

func loadJobPreviewCmd(job jobSummary) tea.Cmd {
	return func() tea.Msg {
		jobDir, err := getJobDir(job.ID)
		if err != nil {
			return jobPreviewLoadedMsg{detail: &jobDetail{jobSummary: job}}
		}

		// Resolve symlinks once for TOCTOU protection
		canonicalJobDir, err := filepath.EvalSymlinks(jobDir)
		if err != nil {
			return jobPreviewLoadedMsg{detail: &jobDetail{jobSummary: job}}
		}

		detail := &jobDetail{jobSummary: job}
		if resultData, err := os.ReadFile(filepath.Join(canonicalJobDir, "result.json")); err == nil {
			var envelope map[string]any
			if json.Unmarshal(resultData, &envelope) == nil {
				if data, ok := envelope["data"]; ok {
					detail.ResultData = data
				}
			}
		}
		if stderrData, err := os.ReadFile(filepath.Join(canonicalJobDir, "stderr.log")); err == nil {
			detail.Stderr = string(stderrData)
		}
		return jobPreviewLoadedMsg{detail: detail}
	}
}

func newModel(jobs []jobSummary, workspace string) model {
	return newModelWithMode(jobs, workspace, "", viewJobs)
}

func newModelWithMode(jobs []jobSummary, workspace, actorID string, initialMode viewMode) model {
	s := spinner.New()
	s.Spinner = spinner.Dot
	s.Style = lipgloss.NewStyle().Foreground(lipgloss.Color("205"))

	cursor := 0
	if len(jobs) > 0 {
		cursor = len(jobs) - 1
	}

	// Default actor if not specified
	if actorID == "" {
		actorID = "actor:coder:agent1"
	}

	return model{
		mode:      initialMode,
		jobs:      jobs,
		workspace: workspace,
		cursor:    cursor,
		activeTab: tabInfo,
		spinner:   s,
		actorID:   actorID,
		bbNS:      "default",
		bbTopic:   "tasks",
	}
}

// newSearchModel creates a model initialized for search mode.
func newSearchModel(workspace, query string, limit int, rerank bool, scopes []string) model {
	s := spinner.New()
	s.Spinner = spinner.Dot
	s.Style = lipgloss.NewStyle().Foreground(lipgloss.Color("205"))

	if limit <= 0 {
		limit = 10
	}
	if len(scopes) == 0 {
		scopes = []string{"symbols", "sessions", "memories", "tasks"}
	}

	return model{
		mode:         viewSearch,
		workspace:    workspace,
		searchQuery:  query,
		searchLimit:  limit,
		searchRerank: rerank,
		searchScopes: scopes,
		spinner:      s,
		loading:      true,
	}
}

func (m model) Init() tea.Cmd {
	var cmds []tea.Cmd
	cmds = append(cmds, m.spinner.Tick, tickCmd())

	// Load initial data based on mode
	switch m.mode {
	case viewSQLite:
		cmds = append(cmds, loadSQLiteDatabasesCmd())
	case viewSearch:
		if m.searchQuery != "" {
			cmds = append(cmds, loadSearchCmd(m.searchQuery, m.searchLimit, m.searchRerank, m.searchScopes))
		}
	default:
		if len(m.jobs) > 0 {
			cmds = append(cmds, loadJobPreviewCmd(m.jobs[m.cursor]))
		}
	}
	return tea.Batch(cmds...)
}

func tickCmd() tea.Cmd {
	return tea.Tick(time.Second*5, func(t time.Time) tea.Msg {
		return tickMsg{}
	})
}

type tickMsg struct{}

type jobsListLoadedMsg []jobSummary

func loadJobsCmd(limit int) tea.Cmd {
	return func() tea.Msg {
		jobs, err := loadJobs(limit)
		if err != nil {
			return jobsListLoadedMsg([]jobSummary{})
		}
		return jobsListLoadedMsg(jobs)
	}
}

type jobLoadedMsg struct {
	detail *jobDetail
}

func loadJobDetailCmd(job jobSummary) tea.Cmd {
	return func() tea.Msg {
		if home, err := os.UserHomeDir(); err == nil {
			jobsRoot := filepath.Join(home, ".foxctl", "jobs")
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			store, err := jobstore.Open(ctx, jobsRoot)
			if err == nil {
				if j, err := store.Get(ctx, job.ID); err == nil {
					job = jobSummary{
						ID:        j.ID,
						Command:   j.Command,
						State:     string(j.State),
						CreatedAt: j.CreatedAt.UTC().Format(time.RFC3339),
						Error:     j.Error,
					}
				}
				_ = store.Close() //nolint:errcheck
			}
		}

		jobDir, err := getJobDir(job.ID)
		if err != nil {
			return jobLoadedMsg{detail: &jobDetail{jobSummary: job}}
		}

		// Resolve symlinks once for TOCTOU protection
		canonicalJobDir, err := filepath.EvalSymlinks(jobDir)
		if err != nil {
			return jobLoadedMsg{detail: &jobDetail{jobSummary: job}}
		}

		detail := &jobDetail{
			jobSummary: job,
		}

		if resultData, err := os.ReadFile(filepath.Join(canonicalJobDir, "result.json")); err == nil {
			var envelope map[string]any
			if json.Unmarshal(resultData, &envelope) == nil {
				if data, ok := envelope["data"]; ok {
					detail.ResultData = data
				}
			}
		}

		if stderrData, err := os.ReadFile(filepath.Join(canonicalJobDir, "stderr.log")); err == nil {
			detail.Stderr = string(stderrData)
		}

		if artifactsData, err := os.ReadFile(filepath.Join(canonicalJobDir, "artifacts.json")); err == nil {
			var meta struct {
				Digests []string `json:"digests"`
			}
			if json.Unmarshal(artifactsData, &meta) == nil && len(meta.Digests) > 0 {
				detail.Artifacts = meta.Digests
			} else {
				var artifacts []string
				if json.Unmarshal(artifactsData, &artifacts) == nil {
					detail.Artifacts = artifacts
				}
			}
		}

		return jobLoadedMsg{detail: detail}
	}
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd

	switch msg := msg.(type) {
	case tea.KeyMsg:
		// Handle job detail view
		if m.selected != nil {
			m, cmd = m.updateDetail(msg)
			return m, cmd
		}
		// Handle task/message detail views
		if m.selectedTask != nil || m.selectedMessage != nil {
			switch msg.String() {
			case "q", "ctrl+c":
				return m, tea.Quit
			case "esc":
				m.selectedTask = nil
				m.selectedMessage = nil
				return m, nil
			}
			return m, nil
		}
		m, cmd = m.updateList(msg)
		return m, cmd

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height

		sidebarWidth := 20
		mainWidth := msg.Width - sidebarWidth

		headerHeight := 3
		footerHeight := 3
		verticalMarginHeight := headerHeight + footerHeight

		if !m.ready {
			m.viewport = viewport.New(mainWidth, msg.Height-verticalMarginHeight)
			m.viewport.YPosition = headerHeight
			m.ready = true
		} else {
			m.viewport.Width = mainWidth
			m.viewport.Height = msg.Height - verticalMarginHeight
		}

		if m.selected != nil {
			m.setViewportContent()
		}

	case spinner.TickMsg:
		m.spinner, cmd = m.spinner.Update(msg)
		return m, cmd

	case tickMsg:
		if m.mode == viewJobs {
			return m, tea.Batch(loadJobsCmd(len(m.jobs)), tickCmd())
		}
		return m, tickCmd()

	case jobsListLoadedMsg:
		m.jobs = msg
		if len(m.jobs) > 0 && m.previewDetail == nil {
			return m, loadJobPreviewCmd(m.jobs[m.cursor])
		}
		return m, nil

	case jobPreviewLoadedMsg:
		m.previewDetail = msg.detail
		return m, nil

	case tasksLoadedMsg:
		m.tasks = msg
		m.loading = false
		m.setViewportContent()
		return m, nil

	case mailboxLoadedMsg:
		m.messages = msg
		m.loading = false
		m.setViewportContent()
		return m, nil

	case reservationsLoadedMsg:
		m.reservations = msg
		m.loading = false
		m.setViewportContent()
		return m, nil

	case insightsLoadedMsg:
		m.insights = msg
		m.loading = false
		m.setViewportContent()
		return m, nil

	case statsLoadedMsg:
		m.stats = msg
		m.loading = false
		m.setViewportContent()
		return m, nil

	case blackboardLoadedMsg:
		m.bbRecords = msg
		m.loading = false
		if m.bbCursor >= len(m.bbRecords) && len(m.bbRecords) > 0 {
			m.bbCursor = len(m.bbRecords) - 1
		}
		m.setViewportContent()
		return m, nil

	// SQLite browser message handlers
	case sqliteDatabasesLoadedMsg:
		m.sqliteDatabases = msg
		m.loading = false
		if len(m.sqliteDatabases) > 0 && m.sqliteSelectedDB >= len(m.sqliteDatabases) {
			m.sqliteSelectedDB = 0
		}
		// Auto-load tables for the first database
		if len(m.sqliteDatabases) > 0 {
			return m, loadSQLiteTablesCmd(m.sqliteDatabases[m.sqliteSelectedDB].Path)
		}
		m.setViewportContent()
		return m, nil

	case sqliteTablesLoadedMsg:
		m.sqliteTables = msg
		m.loading = false
		if m.sqliteSelectedTable >= len(m.sqliteTables) && len(m.sqliteTables) > 0 {
			m.sqliteSelectedTable = 0
		}
		m.setViewportContent()
		return m, nil

	case sqliteRowsLoadedMsg:
		m.sqliteColumns = msg.columns
		m.sqliteRows = msg.rows
		m.loading = false
		m.setViewportContent()
		return m, nil

	case sqliteSchemaLoadedMsg:
		m.sqliteSchema = string(msg)
		m.loading = false
		m.setViewportContent()
		return m, nil

	case searchLoadedMsg:
		m.loading = false
		if msg.err != nil {
			m.lastError = fmt.Sprintf("search failed: %v", msg.err)
		} else {
			m.searchResults = msg.results
			m.searchStats = msg.stats
			m.lastError = ""
			if m.searchCursor >= len(m.searchResults) && len(m.searchResults) > 0 {
				m.searchCursor = len(m.searchResults) - 1
			}
		}
		m.setViewportContent()
		return m, nil

	case jobLoadedMsg:
		m.selected = msg.detail
		m.activeTab = tabInfo
		m.loading = false
		m.setViewportContent()
		m.viewport.GotoTop()

	// Action result handlers
	case jobCancelledMsg:
		m.loading = false
		if msg.err != nil {
			m.lastError = fmt.Sprintf("cancel failed: %v", msg.err)
		} else {
			m.lastError = ""
			// Refresh jobs list
			return m, loadJobsCmd(len(m.jobs))
		}

	case taskCompletedMsg:
		m.loading = false
		if msg.err != nil {
			m.lastError = fmt.Sprintf("complete failed: %v", msg.err)
		} else {
			m.lastError = ""
			m.selectedTask = nil
			// Refresh tasks list
			return m, loadTasksCmd(m.workspace, 50)
		}

	case taskSetActiveMsg:
		m.loading = false
		if msg.err != nil {
			m.lastError = fmt.Sprintf("set-active failed: %v", msg.err)
		} else {
			m.lastError = ""
			// Refresh tasks list
			return m, loadTasksCmd(m.workspace, 50)
		}

	case messageAckedMsg:
		m.loading = false
		if msg.err != nil {
			m.lastError = fmt.Sprintf("ack failed: %v", msg.err)
		} else {
			m.lastError = ""
			m.selectedMessage = nil
			// Refresh mailbox
			return m, loadMailboxCmd(m.workspace, m.actorID)
		}

	case reservationReleasedMsg:
		m.loading = false
		if msg.err != nil {
			m.lastError = fmt.Sprintf("release failed: %v", msg.err)
		} else {
			m.lastError = ""
			// Refresh reservations
			return m, loadReservationsCmd(m.workspace)
		}
	}

	return m, nil
}

func (m *model) setViewportContent() {
	var content string
	if m.selected != nil {
		switch m.activeTab {
		case tabInfo:
			content = m.renderTabInfo()
		case tabResult:
			content = m.renderTabResult()
		case tabStderr:
			content = m.renderTabStderr()
		case tabArtifacts:
			content = m.renderTabArtifacts()
		}
	} else {
		switch m.mode {
		case viewTasks:
			content = m.renderTasks()
		case viewInsights:
			content = m.renderInsights()
		case viewMailbox:
			content = m.renderMailbox()
		case viewReservations:
			content = m.renderReservations()
		case viewStats:
			content = m.renderStats()
		case viewBlackboard:
			content = m.renderBlackboard()
		case viewSQLite:
			content = m.renderSQLite()
		case viewSearch:
			content = m.renderSearch()
		default:
			return
		}
	}

	m.viewport.SetContent(content)
}

func (m model) updateList(msg tea.KeyMsg) (model, tea.Cmd) {
	switch msg.String() {
	case "q", "ctrl+c":
		return m, tea.Quit

	// Direct view hotkeys (1-5)
	case "1":
		if m.selected == nil && m.selectedTask == nil && m.selectedMessage == nil {
			m.mode = viewJobs
			return m.handleModeChange()
		}
	case "2":
		if m.selected == nil && m.selectedTask == nil && m.selectedMessage == nil {
			m.mode = viewTasks
			return m.handleModeChange()
		}
	case "3":
		if m.selected == nil && m.selectedTask == nil && m.selectedMessage == nil {
			m.mode = viewInsights
			return m.handleModeChange()
		}
	case "4":
		if m.selected == nil && m.selectedTask == nil && m.selectedMessage == nil {
			m.mode = viewMailbox
			return m.handleModeChange()
		}
	case "5":
		if m.selected == nil && m.selectedTask == nil && m.selectedMessage == nil {
			m.mode = viewReservations
			return m.handleModeChange()
		}
	case "6":
		if m.selected == nil && m.selectedTask == nil && m.selectedMessage == nil && m.selectedBB == nil {
			m.mode = viewStats
			return m.handleModeChange()
		}
	case "7":
		if m.selected == nil && m.selectedTask == nil && m.selectedMessage == nil && m.selectedBB == nil {
			m.mode = viewBlackboard
			return m.handleModeChange()
		}
	case "8":
		if m.selected == nil && m.selectedTask == nil && m.selectedMessage == nil && m.selectedBB == nil {
			m.mode = viewSQLite
			return m.handleModeChange()
		}
	case "9":
		if m.selected == nil && m.selectedTask == nil && m.selectedMessage == nil && m.selectedBB == nil && m.selectedResult == nil {
			m.mode = viewSearch
			return m.handleModeChange()
		}

	// View cycling
	case "[":
		m.mode = (m.mode + viewModeCount - 1) % viewModeCount
		return m.handleModeChange()
	case "]", "tab":
		m.mode = (m.mode + 1) % viewModeCount
		return m.handleModeChange()

	// Actor picker for mailbox
	case "a":
		if m.mode == viewMailbox {
			// Cycle through common actor IDs (simple version)
			actors := []string{
				"actor:coder:agent1",
				"actor:coder:agent2",
				"actor:planner:main",
				"actor:reviewer:main",
			}
			for i, a := range actors {
				if a == m.actorID {
					m.actorID = actors[(i+1)%len(actors)]
					break
				}
			}
			m.loading = true
			return m, tea.Batch(loadMailboxCmd(m.workspace, m.actorID), m.spinner.Tick)
		}

	// Navigation - unified across views
	case "up", "k":
		return m.handleCursorUp()
	case "down", "j":
		return m.handleCursorDown()

	// Selection/Enter
	case "enter":
		return m.handleEnter()

	// Escape from detail views
	case "esc":
		if m.selectedTask != nil {
			m.selectedTask = nil
			return m, nil
		}
		if m.selectedMessage != nil {
			m.selectedMessage = nil
			return m, nil
		}
		if m.selectedBB != nil {
			m.selectedBB = nil
			return m, nil
		}
		if m.selectedResult != nil {
			m.selectedResult = nil
			return m, nil
		}
		// SQLite: go back in pane hierarchy
		if m.mode == viewSQLite {
			switch m.sqlitePane {
			case paneData:
				m.sqlitePane = paneTables
				m.sqliteRows = nil
				m.sqliteColumns = nil
				return m, nil
			case paneTables:
				m.sqlitePane = paneDatabases
				m.sqliteTables = nil
				return m, nil
			}
		}

	// SQLite pane navigation with h/l
	case "h":
		if m.mode == viewSQLite {
			switch m.sqlitePane {
			case paneData:
				m.sqlitePane = paneTables
				return m, nil
			case paneTables:
				m.sqlitePane = paneDatabases
				return m, nil
			}
		}
	case "l":
		if m.mode == viewSQLite {
			switch m.sqlitePane {
			case paneDatabases:
				if len(m.sqliteDatabases) > 0 {
					m.sqlitePane = paneTables
					m.loading = true
					return m, tea.Batch(loadSQLiteTablesCmd(m.sqliteDatabases[m.sqliteSelectedDB].Path), m.spinner.Tick)
				}
			case paneTables:
				if len(m.sqliteTables) > 0 {
					m.sqlitePane = paneData
					m.loading = true
					db := m.sqliteDatabases[m.sqliteSelectedDB]
					table := m.sqliteTables[m.sqliteSelectedTable]
					return m, tea.Batch(loadSQLiteRowsCmd(db.Path, table.Name, 50), m.spinner.Tick)
				}
			}
		}

	// SQLite schema viewer
	case "i":
		if m.mode == viewSQLite && m.sqlitePane == paneTables && len(m.sqliteTables) > 0 {
			db := m.sqliteDatabases[m.sqliteSelectedDB]
			table := m.sqliteTables[m.sqliteSelectedTable]
			m.loading = true
			return m, tea.Batch(loadSQLiteSchemaCmd(db.Path, table.Name), m.spinner.Tick)
		}

	// Refresh current view
	case "r":
		return m.handleModeChange()

	// Action keys
	case "c":
		// Cancel job (Jobs view only)
		if m.mode == viewJobs && m.cursor < len(m.jobs) {
			job := m.jobs[m.cursor]
			if job.State == "running" || job.State == "queued" {
				m.loading = true
				return m, tea.Batch(cancelJobCmd(job.ID), m.spinner.Tick)
			}
		}

	case "d":
		// Complete task (Tasks view)
		if m.mode == viewTasks && m.taskCursor < len(m.tasks) {
			task := m.tasks[m.taskCursor]
			m.loading = true
			return m, tea.Batch(completeTaskCmd(m.workspace, task.TaskID), m.spinner.Tick)
		}

	case "s":
		// Set active task (Tasks view)
		if m.mode == viewTasks && m.taskCursor < len(m.tasks) {
			task := m.tasks[m.taskCursor]
			m.loading = true
			return m, tea.Batch(setActiveTaskCmd(m.workspace, task.TaskID), m.spinner.Tick)
		}

	case "x":
		// Ack message (Mailbox view) or Release reservation (Reservations view)
		if m.mode == viewMailbox && m.messageCursor < len(m.messages) {
			msg := m.messages[m.messageCursor]
			m.loading = true
			return m, tea.Batch(ackMessageCmd(m.workspace, m.actorID, msg.ID), m.spinner.Tick)
		}
		if m.mode == viewReservations && m.reservationCursor < len(m.reservations) {
			res := m.reservations[m.reservationCursor]
			m.loading = true
			return m, tea.Batch(releaseReservationCmd(m.workspace, res.Holder, res.Path), m.spinner.Tick)
		}
	}
	return m, nil
}

// handleCursorUp moves cursor up in the current view
func (m model) handleCursorUp() (model, tea.Cmd) {
	switch m.mode {
	case viewJobs:
		if m.cursor > 0 {
			m.cursor--
			return m, loadJobPreviewCmd(m.jobs[m.cursor])
		}
	case viewTasks:
		if m.taskCursor > 0 {
			m.taskCursor--
		}
	case viewMailbox:
		if m.messageCursor > 0 {
			m.messageCursor--
		}
	case viewReservations:
		if m.reservationCursor > 0 {
			m.reservationCursor--
		}
	case viewBlackboard:
		if m.bbCursor > 0 {
			m.bbCursor--
		}
	case viewSQLite:
		switch m.sqlitePane {
		case paneDatabases:
			if m.sqliteSelectedDB > 0 {
				m.sqliteSelectedDB--
				// Reset table selection when changing database
				m.sqliteSelectedTable = 0
				m.sqliteTables = nil
				return m, loadSQLiteTablesCmd(m.sqliteDatabases[m.sqliteSelectedDB].Path)
			}
		case paneTables:
			if m.sqliteSelectedTable > 0 {
				m.sqliteSelectedTable--
			}
		}
	case viewSearch:
		if m.searchCursor > 0 {
			m.searchCursor--
		}
	}
	return m, nil
}

// handleCursorDown moves cursor down in the current view
func (m model) handleCursorDown() (model, tea.Cmd) {
	switch m.mode {
	case viewJobs:
		if m.cursor < len(m.jobs)-1 {
			m.cursor++
			return m, loadJobPreviewCmd(m.jobs[m.cursor])
		}
	case viewTasks:
		if m.taskCursor < len(m.tasks)-1 {
			m.taskCursor++
		}
	case viewMailbox:
		if m.messageCursor < len(m.messages)-1 {
			m.messageCursor++
		}
	case viewReservations:
		if m.reservationCursor < len(m.reservations)-1 {
			m.reservationCursor++
		}
	case viewBlackboard:
		if m.bbCursor < len(m.bbRecords)-1 {
			m.bbCursor++
		}
	case viewSQLite:
		switch m.sqlitePane {
		case paneDatabases:
			if m.sqliteSelectedDB < len(m.sqliteDatabases)-1 {
				m.sqliteSelectedDB++
				// Reset table selection when changing database
				m.sqliteSelectedTable = 0
				m.sqliteTables = nil
				return m, loadSQLiteTablesCmd(m.sqliteDatabases[m.sqliteSelectedDB].Path)
			}
		case paneTables:
			if m.sqliteSelectedTable < len(m.sqliteTables)-1 {
				m.sqliteSelectedTable++
			}
		}
	case viewSearch:
		if m.searchCursor < len(m.searchResults)-1 {
			m.searchCursor++
		}
	}
	return m, nil
}

// handleEnter handles enter key to select/view details
func (m model) handleEnter() (model, tea.Cmd) {
	switch m.mode {
	case viewJobs:
		if m.cursor < len(m.jobs) {
			m.loading = true
			return m, tea.Batch(
				loadJobDetailCmd(m.jobs[m.cursor]),
				m.spinner.Tick,
			)
		}
	case viewTasks:
		if m.taskCursor < len(m.tasks) {
			task := m.tasks[m.taskCursor]
			m.selectedTask = &task
		}
	case viewMailbox:
		if m.messageCursor < len(m.messages) {
			msg := m.messages[m.messageCursor]
			m.selectedMessage = &msg
		}
	case viewBlackboard:
		if m.bbCursor < len(m.bbRecords) {
			rec := m.bbRecords[m.bbCursor]
			m.selectedBB = &rec
		}
	case viewSQLite:
		switch m.sqlitePane {
		case paneDatabases:
			if len(m.sqliteDatabases) > 0 {
				m.sqlitePane = paneTables
				m.loading = true
				return m, tea.Batch(loadSQLiteTablesCmd(m.sqliteDatabases[m.sqliteSelectedDB].Path), m.spinner.Tick)
			}
		case paneTables:
			if len(m.sqliteTables) > 0 {
				m.sqlitePane = paneData
				m.loading = true
				db := m.sqliteDatabases[m.sqliteSelectedDB]
				table := m.sqliteTables[m.sqliteSelectedTable]
				return m, tea.Batch(loadSQLiteRowsCmd(db.Path, table.Name, 50), m.spinner.Tick)
			}
		}
	case viewSearch:
		if m.searchCursor < len(m.searchResults) {
			result := m.searchResults[m.searchCursor]
			m.selectedResult = &result
		}
	}
	return m, nil
}

func (m *model) handleModeChange() (model, tea.Cmd) {
	m.viewport.GotoTop()
	// Clear any detail selections when switching views
	m.selectedTask = nil
	m.selectedMessage = nil
	m.selectedBB = nil
	m.selectedResult = nil
	m.lastError = ""

	switch m.mode {
	case viewTasks:
		m.loading = true
		return *m, tea.Batch(loadTasksCmd(m.workspace, 50), m.spinner.Tick)
	case viewInsights:
		m.loading = true
		return *m, tea.Batch(loadInsightsCmd(m.workspace), m.spinner.Tick)
	case viewMailbox:
		m.loading = true
		return *m, tea.Batch(loadMailboxCmd(m.workspace, m.actorID), m.spinner.Tick)
	case viewReservations:
		m.loading = true
		return *m, tea.Batch(loadReservationsCmd(m.workspace), m.spinner.Tick)
	case viewStats:
		m.loading = true
		return *m, tea.Batch(loadStatsCmd(m.jobs), m.spinner.Tick)
	case viewBlackboard:
		m.loading = true
		return *m, tea.Batch(loadBlackboardCmd(m.bbNS, m.bbTopic, 50), m.spinner.Tick)
	case viewSQLite:
		m.loading = true
		m.sqlitePane = paneDatabases
		return *m, tea.Batch(loadSQLiteDatabasesCmd(), m.spinner.Tick)
	case viewSearch:
		// If we have a query, search. Otherwise show empty state
		if m.searchQuery != "" {
			m.loading = true
			return *m, tea.Batch(loadSearchCmd(m.searchQuery, m.searchLimit, m.searchRerank, m.searchScopes), m.spinner.Tick)
		}
		m.setViewportContent()
	case viewJobs:
		if len(m.jobs) > 0 && m.cursor < len(m.jobs) {
			return *m, loadJobPreviewCmd(m.jobs[m.cursor])
		}
	}
	return *m, nil
}

type tasksLoadedMsg []taskSummary

func loadTasksCmd(workspace string, limit int) tea.Cmd {
	return func() tea.Msg {
		tasks, err := fetchTasks(workspace, limit)
		if err != nil {
			return tasksLoadedMsg([]taskSummary{})
		}
		return tasksLoadedMsg(tasks)
	}
}

func (m model) updateDetail(msg tea.KeyMsg) (model, tea.Cmd) {
	switch msg.String() {
	case "q", "ctrl+c", "esc":
		m.selected = nil
		return m, nil
	case "tab", "l":
		m.activeTab = (m.activeTab + 1) % 4
		m.setViewportContent()
		m.viewport.GotoTop()
	case "shift+tab", "h":
		m.activeTab = (m.activeTab + 3) % 4
		m.setViewportContent()
		m.viewport.GotoTop()
	case "1":
		m.activeTab = tabInfo
		m.setViewportContent()
		m.viewport.GotoTop()
	case "2":
		m.activeTab = tabResult
		m.setViewportContent()
		m.viewport.GotoTop()
	case "3":
		m.activeTab = tabStderr
		m.setViewportContent()
		m.viewport.GotoTop()
	case "4":
		m.activeTab = tabArtifacts
		m.setViewportContent()
		m.viewport.GotoTop()
	}

	var cmd tea.Cmd
	m.viewport, cmd = m.viewport.Update(msg)
	return m, cmd
}

func (m model) View() string {
	if !m.ready {
		return "\n  Initializing..."
	}

	sidebar := m.renderSidebar()
	var mainContent string

	if m.selected != nil {
		mainContent = m.viewDetail()
	} else if m.selectedTask != nil {
		mainContent = m.viewTaskDetail()
	} else if m.selectedMessage != nil {
		mainContent = m.viewMessageDetail()
	} else if m.selectedBB != nil {
		mainContent = m.viewBlackboardDetail()
	} else if m.selectedResult != nil {
		mainContent = m.viewSearchResultDetail()
	} else {
		switch m.mode {
		case viewJobs:
			// Split view for jobs: List | Preview
			sidebarWidth := 20
			listWidth := (m.width - sidebarWidth) / 2
			previewWidth := m.width - sidebarWidth - listWidth

			list := m.viewList(listWidth)
			preview := m.renderPreview(previewWidth)

			mainContent = lipgloss.JoinHorizontal(lipgloss.Top, list, preview)
		case viewTasks, viewInsights, viewMailbox, viewReservations, viewStats, viewBlackboard, viewSQLite, viewSearch:
			mainContent = m.viewport.View()
		default:
			mainContent = "\n  Coming soon..."
		}
	}

	// Add status bar at the bottom
	statusBar := m.renderStatusBar()

	return lipgloss.JoinVertical(lipgloss.Left,
		lipgloss.JoinHorizontal(lipgloss.Top, sidebar, mainContent),
		statusBar,
	)
}

// renderStatusBar renders the bottom status bar
func (m model) renderStatusBar() string {
	barWidth := m.width

	statusStyle := lipgloss.NewStyle().
		Background(lipgloss.Color("236")).
		Foreground(lipgloss.Color("252")).
		Width(barWidth).
		Padding(0, 1)

	var parts []string

	// View indicator with hotkey
	viewLabel := fmt.Sprintf("[%d] %s", int(m.mode)+1, viewModeLabels[m.mode])
	parts = append(parts, lipgloss.NewStyle().Bold(true).Render(viewLabel))

	// Item count for current view
	switch m.mode {
	case viewJobs:
		parts = append(parts, fmt.Sprintf("%d jobs", len(m.jobs)))
	case viewTasks:
		parts = append(parts, fmt.Sprintf("%d tasks", len(m.tasks)))
	case viewMailbox:
		parts = append(parts, fmt.Sprintf("%d msgs", len(m.messages)))
		parts = append(parts, lipgloss.NewStyle().Foreground(lipgloss.Color("214")).Render(m.actorID))
	case viewReservations:
		parts = append(parts, fmt.Sprintf("%d reservations", len(m.reservations)))
	case viewStats:
		if m.stats != nil {
			parts = append(parts, fmt.Sprintf("%d total jobs", m.stats.Total))
		}
	case viewBlackboard:
		parts = append(parts, fmt.Sprintf("%d records", len(m.bbRecords)))
		parts = append(parts, lipgloss.NewStyle().Foreground(lipgloss.Color("214")).Render(m.bbNS+"/"+m.bbTopic))
	case viewSQLite:
		parts = append(parts, fmt.Sprintf("%d dbs", len(m.sqliteDatabases)))
		if len(m.sqliteDatabases) > 0 && m.sqliteSelectedDB < len(m.sqliteDatabases) {
			dbName := m.sqliteDatabases[m.sqliteSelectedDB].getFriendlyName()
			parts = append(parts, lipgloss.NewStyle().Foreground(lipgloss.Color("214")).Render(dbName))
		}
	case viewSearch:
		parts = append(parts, fmt.Sprintf("%d results", len(m.searchResults)))
		if m.searchRerank {
			parts = append(parts, lipgloss.NewStyle().Foreground(lipgloss.Color("82")).Render("reranked"))
		}
		if m.searchStats != nil {
			parts = append(parts, fmt.Sprintf("%dms", m.searchStats.LatencyMS))
		}
	}

	// Error indicator
	if m.lastError != "" {
		errStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("196"))
		parts = append(parts, errStyle.Render("ERR: "+truncate(m.lastError, 30)))
	}

	// View-specific action hints
	helpStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
	actionStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("214"))

	var actionHints string
	switch m.mode {
	case viewJobs:
		actionHints = actionStyle.Render("c:cancel") + " "
	case viewTasks:
		actionHints = actionStyle.Render("d:done s:set-active") + " "
	case viewMailbox:
		actionHints = actionStyle.Render("a:actor x:ack") + " "
	case viewReservations:
		actionHints = actionStyle.Render("x:release") + " "
	case viewSQLite:
		actionHints = actionStyle.Render("h/l:panes i:schema") + " "
	}

	parts = append(parts, actionHints+helpStyle.Render("j/k:nav enter:view r:refresh"))

	return statusStyle.Render(strings.Join(parts, " | "))
}

// viewTaskDetail renders the task detail view
func (m model) viewTaskDetail() string {
	task := m.selectedTask
	if task == nil {
		return ""
	}

	sidebarWidth := 20
	mainWidth := m.width - sidebarWidth

	var b strings.Builder
	titleStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("170"))
	labelStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("240")).Width(12)
	valueStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("252"))

	b.WriteString(titleStyle.Render("Task Details") + "\n\n")

	b.WriteString(labelStyle.Render("ID:") + valueStyle.Render(task.TaskID) + "\n")
	b.WriteString(labelStyle.Render("Title:") + valueStyle.Render(task.Title) + "\n")
	b.WriteString(labelStyle.Render("Score:") + lipgloss.NewStyle().Foreground(lipgloss.Color("214")).Render(fmt.Sprintf("%.2f", task.Score)) + "\n")
	if task.Status != "" {
		b.WriteString(labelStyle.Render("Status:") + valueStyle.Render(task.Status) + "\n")
	}

	// Footer
	footerStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("240")).
		Border(lipgloss.NormalBorder(), true, false, false, false).
		BorderForeground(lipgloss.Color("238"))

	help := "esc: back"
	b.WriteString("\n\n" + footerStyle.Width(mainWidth-4).Render(help))

	return lipgloss.NewStyle().Padding(1, 2).Render(b.String())
}

// viewMessageDetail renders the message detail view
func (m model) viewMessageDetail() string {
	msg := m.selectedMessage
	if msg == nil {
		return ""
	}

	sidebarWidth := 20
	mainWidth := m.width - sidebarWidth

	var b strings.Builder
	titleStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("170"))
	labelStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("240")).Width(12)
	valueStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("252"))

	b.WriteString(titleStyle.Render("Message Details") + "\n\n")

	b.WriteString(labelStyle.Render("ID:") + valueStyle.Render(msg.ID) + "\n")
	b.WriteString(labelStyle.Render("From:") + valueStyle.Render(msg.Sender) + "\n")
	b.WriteString(labelStyle.Render("Subject:") + valueStyle.Render(msg.Subject) + "\n")
	b.WriteString(labelStyle.Render("Priority:") + valueStyle.Render(fmt.Sprintf("P%d", msg.Priority)) + "\n")
	b.WriteString(labelStyle.Render("Status:") + valueStyle.Render(msg.Status) + "\n")
	b.WriteString(labelStyle.Render("Kind:") + valueStyle.Render(msg.Kind) + "\n")
	b.WriteString(labelStyle.Render("Created:") + valueStyle.Render(msg.CreatedAt) + "\n")

	if msg.Body != "" {
		b.WriteString("\n" + labelStyle.Render("Body:") + "\n")
		b.WriteString(valueStyle.Render(msg.Body) + "\n")
	}

	// Footer
	footerStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("240")).
		Border(lipgloss.NormalBorder(), true, false, false, false).
		BorderForeground(lipgloss.Color("238"))

	help := "esc: back"
	b.WriteString("\n\n" + footerStyle.Width(mainWidth-4).Render(help))

	return lipgloss.NewStyle().Padding(1, 2).Render(b.String())
}

func (m model) renderPreview(width int) string {
	previewStyle := lipgloss.NewStyle().
		Width(width).
		Height(m.height-1).
		Border(lipgloss.NormalBorder(), false, false, false, true).
		BorderForeground(lipgloss.Color("238")).
		Padding(1, 2)

	if len(m.jobs) == 0 || m.cursor >= len(m.jobs) {
		return previewStyle.Render(lipgloss.NewStyle().Foreground(lipgloss.Color("240")).Render("No job selected"))
	}

	job := m.jobs[m.cursor]
	titleStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("170")).MarginBottom(1)
	labelStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("240")).Width(10)

	var b strings.Builder
	b.WriteString(titleStyle.Render("Job Preview") + "\n\n")

	b.WriteString(labelStyle.Render("ID:") + job.ID + "\n")
	b.WriteString(labelStyle.Render("Status:") + stateColor(job.State).Bold(true).Render(strings.ToUpper(job.State)) + "\n")
	b.WriteString(labelStyle.Render("Created:") + job.CreatedAt + "\n\n")

	b.WriteString(labelStyle.Render("Command:") + "\n")
	b.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color("252")).Render(job.Command) + "\n\n")

	// Show result if available in previewDetail
	if m.previewDetail != nil && m.previewDetail.ID == job.ID && m.previewDetail.ResultData != nil {
		b.WriteString(lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("82")).Render("Result:") + "\n")
		resultJSON, err := json.MarshalIndent(m.previewDetail.ResultData, "", "  ")
		if err != nil {
			resultJSON = []byte(fmt.Sprintf("unable to render result: %v", err))
		}

		// Don't truncate too aggressively for JSON, let it wrap or show a decent amount
		lines := strings.Split(string(resultJSON), "\n")
		maxPreviewLines := 15
		if len(lines) > maxPreviewLines {
			lines = append(lines[:maxPreviewLines], "... (press ENTER for full result)")
		}
		b.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color("250")).Render(strings.Join(lines, "\n")) + "\n\n")
	}

	if job.Error != "" {
		b.WriteString(lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("196")).Render("Error:") + "\n")
		b.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color("196")).Render(job.Error) + "\n")
	} else if m.previewDetail != nil && m.previewDetail.ID == job.ID && m.previewDetail.Stderr != "" {
		b.WriteString(lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("240")).Render("Stderr (short):") + "\n")
		b.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color("240")).Render(truncate(m.previewDetail.Stderr, 100)) + "\n")
	}

	return previewStyle.Render(b.String())
}

func (m model) renderSidebar() string {
	sidebarStyle := lipgloss.NewStyle().
		Width(20).
		Height(m.height-1). // Reserve space for status bar
		Border(lipgloss.NormalBorder(), false, true, false, false).
		BorderForeground(lipgloss.Color("238")).
		Padding(1, 1)

	titleStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("170")).
		MarginBottom(1)

	itemStyle := lipgloss.NewStyle().PaddingLeft(1)
	activeItemStyle := itemStyle.
		Bold(true).
		Foreground(lipgloss.Color("255")).
		Background(lipgloss.Color("236"))
	hotkeyStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("214"))

	modes := []struct {
		mode   viewMode
		label  string
		hotkey string
	}{
		{viewJobs, "Jobs", "1"},
		{viewTasks, "Tasks", "2"},
		{viewInsights, "Insights", "3"},
		{viewMailbox, "Mailbox", "4"},
		{viewReservations, "Reservations", "5"},
		{viewStats, "Stats", "6"},
		{viewBlackboard, "Blackboard", "7"},
		{viewSQLite, "SQLite", "8"},
		{viewSearch, "Search", "9"},
	}

	var b strings.Builder
	b.WriteString(titleStyle.Render("foxctl"))
	b.WriteString("\n\n")

	for _, item := range modes {
		label := fmt.Sprintf("%s %s", hotkeyStyle.Render(item.hotkey), item.label)
		if m.mode == item.mode {
			// For active item, we need to render hotkey separately
			activeLabel := fmt.Sprintf("%s %s", hotkeyStyle.Render(item.hotkey), item.label)
			b.WriteString(activeItemStyle.Render(activeLabel) + "\n")
		} else {
			b.WriteString(itemStyle.Render(label) + "\n")
		}
	}

	return sidebarStyle.Render(b.String())
}

func (m model) renderInsights() string {
	if m.insights == nil {
		if m.loading {
			return "\n  Loading insights... " + m.spinner.View()
		}
		return "\n  No insights data available."
	}

	titleStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("170")).MarginBottom(1)
	panelStyle := lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).Padding(0, 1).MarginBottom(1)
	labelStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("240"))

	var b strings.Builder
	b.WriteString(titleStyle.Render("Task Insights Dashboard") + "\n\n")

	b.WriteString(panelStyle.Render(fmt.Sprintf(
		"%s\n  Total Nodes: %d\n  Cycles: %d\n  Topo Order: %d tasks",
		titleStyle.MarginBottom(0).Render("Overview"),
		len(m.insights.Nodes),
		len(m.insights.Cycles),
		len(m.insights.TopologicalOrder))) + "\n")

	if len(m.insights.Nodes) > 0 {
		var keystones strings.Builder
		keystones.WriteString(titleStyle.MarginBottom(0).Render("Keystones (High Critical Path)") + "\n")
		for i, node := range m.insights.Nodes {
			if i >= 5 {
				break
			}
			if node.CriticalPathScore > 0 {
				keystones.WriteString(fmt.Sprintf("  %s (CP: %d)\n",
					labelStyle.Render(safeSlice(node.TaskID, 12)),
					node.CriticalPathScore))
			}
		}
		b.WriteString(panelStyle.Render(keystones.String()) + "\n")

		var bottlenecks strings.Builder
		bottlenecks.WriteString(titleStyle.MarginBottom(0).Render("Bottlenecks (High PageRank)") + "\n")
		for i, node := range m.insights.Nodes {
			if i >= 5 {
				break
			}
			if node.PageRank > 0 {
				bottlenecks.WriteString(fmt.Sprintf("  %s (PR: %.3f)\n",
					labelStyle.Render(safeSlice(node.TaskID, 12)),
					node.PageRank))
			}
		}
		b.WriteString(panelStyle.Render(bottlenecks.String()) + "\n")
	}

	if len(m.insights.Cycles) > 0 {
		b.WriteString(lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("196")).Render(fmt.Sprintf("  %d circular dependencies detected!", len(m.insights.Cycles))) + "\n")
	}

	return b.String()
}

func (m model) renderMailbox() string {
	if m.loading && len(m.messages) == 0 {
		return "\n  Loading mailbox... " + m.spinner.View()
	}

	titleStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("170")).MarginBottom(1)
	labelStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
	highlightStyle := lipgloss.NewStyle().
		Background(lipgloss.Color("226")).
		Foreground(lipgloss.Color("0")).
		Bold(true)

	var b strings.Builder
	b.WriteString(titleStyle.Render(fmt.Sprintf("Mailbox: %s", m.actorID)) + "\n")
	b.WriteString(labelStyle.Render("  Press 'a' to change actor") + "\n\n")

	if len(m.messages) == 0 {
		b.WriteString("  " + labelStyle.Render("(no messages)"))
		return b.String()
	}

	for i, msg := range m.messages {
		statusIcon := "○"
		if msg.Status == "unread" {
			statusIcon = "●"
		}
		priorityStyle := lipgloss.NewStyle()
		if msg.Priority <= 2 {
			priorityStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("196"))
		}

		cursor := "  "
		if i == m.messageCursor {
			cursor = "> "
		}

		line := fmt.Sprintf("%s %s [P%d] %s",
			statusIcon,
			priorityStyle.Render(fmt.Sprintf("%-12s", msg.Sender)),
			msg.Priority,
			msg.Subject)

		if i == m.messageCursor {
			b.WriteString(cursor + highlightStyle.Render(line) + "\n")
		} else {
			b.WriteString(cursor + line + "\n")
		}

		if msg.Body != "" {
			body := truncate(msg.Body, 60)
			b.WriteString(fmt.Sprintf("    %s\n", labelStyle.Render(body)))
		}
		b.WriteString("\n")
	}

	return b.String()
}

func (m model) renderReservations() string {
	if m.loading && len(m.reservations) == 0 {
		return "\n  Loading reservations... " + m.spinner.View()
	}

	titleStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("170")).MarginBottom(1)
	labelStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
	highlightStyle := lipgloss.NewStyle().
		Background(lipgloss.Color("226")).
		Foreground(lipgloss.Color("0")).
		Bold(true)

	var b strings.Builder
	b.WriteString(titleStyle.Render("File Reservations") + "\n\n")

	if len(m.reservations) == 0 {
		b.WriteString("  " + labelStyle.Render("(no active reservations)"))
		return b.String()
	}

	for i, res := range m.reservations {
		modeIcon := "[shared]"
		modeStyle := lipgloss.NewStyle()
		if res.Mode == "exclusive" {
			modeIcon = "[exclusive]"
			modeStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("214"))
		}

		cursor := "  "
		if i == m.reservationCursor {
			cursor = "> "
		}

		line := fmt.Sprintf("%s %s", modeStyle.Render(modeIcon), res.Path)

		if i == m.reservationCursor {
			b.WriteString(cursor + highlightStyle.Render(line) + "\n")
		} else {
			b.WriteString(cursor + line + "\n")
		}

		b.WriteString(fmt.Sprintf("    %s | %s\n\n",
			labelStyle.Render("Holder: "+res.Holder),
			labelStyle.Render("Expires: "+res.ExpiresAt)))
	}

	return b.String()
}

func (m model) renderTasks() string {
	var b strings.Builder
	titleStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("170")).
		MarginBottom(1)

	b.WriteString(titleStyle.Render("Task Recommendations") + "\n\n")

	if len(m.tasks) == 0 {
		if m.loading {
			b.WriteString("  Loading tasks... " + m.spinner.View())
		} else {
			b.WriteString("  No tasks found.")
		}
		return b.String()
	}

	highlightStyle := lipgloss.NewStyle().
		Background(lipgloss.Color("226")).
		Foreground(lipgloss.Color("0")).
		Bold(true)

	for i, task := range m.tasks {
		scoreStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("214"))
		idStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("240"))

		cursor := "  "
		if i == m.taskCursor {
			cursor = "> "
		}

		line := fmt.Sprintf("%s  %s",
			scoreStyle.Render(fmt.Sprintf("[%.2f]", task.Score)),
			task.Title)

		if i == m.taskCursor {
			b.WriteString(cursor + highlightStyle.Render(line) + "\n")
		} else {
			b.WriteString(cursor + line + "\n")
		}
		b.WriteString(fmt.Sprintf("    %s\n", idStyle.Render(task.TaskID)))
	}

	return b.String()
}

func (m model) viewList(width int) string {
	var b strings.Builder

	titleStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("170"))

	header := titleStyle.Render("Jobs")
	if m.loading {
		header += " " + m.spinner.View()
	}
	b.WriteString(header + fmt.Sprintf(" (%d)\n\n", len(m.jobs)))

	// Table header
	headerStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("240"))
	b.WriteString(fmt.Sprintf("  %-8s %-10s %-12s\n",
		headerStyle.Render("STATE"),
		headerStyle.Render("ID"),
		headerStyle.Render("CREATED")))
	b.WriteString(headerStyle.Render("  "+strings.Repeat("─", width-4)) + "\n")

	highlightStyle := lipgloss.NewStyle().
		Background(lipgloss.Color("226")).
		Foreground(lipgloss.Color("0")).
		Bold(true)

	for i, job := range m.jobs {
		cursor := "  "
		if i == m.cursor {
			cursor = "> "
		}

		// Format creation time
		created := job.CreatedAt
		if t, err := time.Parse(time.RFC3339, job.CreatedAt); err == nil {
			created = t.Format("15:04:05")
		}

		lineContent := fmt.Sprintf("%-8s %-10s %-12s",
			job.State,
			safeSlice(job.ID, 10),
			created)

		// Padding to ensure background covers full width
		if len(lineContent) < width-4 {
			lineContent += strings.Repeat(" ", width-4-len(lineContent))
		}

		var line string
		if i == m.cursor {
			line = cursor + highlightStyle.Render(lineContent)
		} else {
			stStyle := stateColor(job.State)
			line = fmt.Sprintf("%s%s %-10s %-12s",
				cursor,
				stStyle.Render(fmt.Sprintf("%-8s", job.State)),
				safeSlice(job.ID, 10),
				created)
		}
		b.WriteString(line + "\n")
	}

	// Fill empty space
	currLines := strings.Count(b.String(), "\n")
	if currLines < m.height-3 {
		b.WriteString(strings.Repeat("\n", m.height-3-currLines))
	}

	footerStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("240")).
		Border(lipgloss.NormalBorder(), true, false, false, false).
		BorderForeground(lipgloss.Color("238"))

	help := "↑/↓: move • enter: select • [: prev mode • ]: next mode"
	b.WriteString("\n" + footerStyle.Width(width).Render(help))

	return b.String()
}

func (m model) viewDetail() string {
	if m.selected == nil {
		return ""
	}
	job := m.selected
	sidebarWidth := 20
	mainWidth := m.width - sidebarWidth

	var b strings.Builder
	titleStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("170"))

	header := titleStyle.Render("Job: " + safeSlice(job.ID, 12))
	header += " " + stateColor(job.State).Render(job.State)
	b.WriteString(header + "\n\n")

	b.WriteString(m.renderTabs())
	b.WriteString("\n\n")

	// Viewport rendering
	b.WriteString(m.viewport.View())

	footerStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("240")).
		Border(lipgloss.NormalBorder(), true, false, false, false).
		BorderForeground(lipgloss.Color("238"))

	help := "tab/1-4: switch • ↑/↓: scroll • esc: back"
	b.WriteString("\n" + footerStyle.Width(mainWidth).Render(help))

	return b.String()
}

func (m model) renderTabs() string {
	tabs := []string{"[1] Info", "[2] Result", "[3] Stderr", "[4] Artifacts"}

	activeStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("255")).
		Background(lipgloss.Color("170")).
		Padding(0, 1)

	inactiveStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("240")).
		Padding(0, 1)

	var parts []string
	for i, tab := range tabs {
		if detailTab(i) == m.activeTab {
			parts = append(parts, activeStyle.Render(tab))
		} else {
			parts = append(parts, inactiveStyle.Render(tab))
		}
	}
	return strings.Join(parts, " ")
}

func (m model) renderTabInfo() string {
	job := m.selected
	labelStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("170")).Width(10)
	valueStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("252"))

	var b strings.Builder
	b.WriteString(labelStyle.Render("ID:") + valueStyle.Render(job.ID) + "\n")
	b.WriteString(labelStyle.Render("Command:") + valueStyle.Render(job.Command) + "\n")
	b.WriteString(labelStyle.Render("State:") + stateColor(job.State).Render(job.State) + "\n")
	b.WriteString(labelStyle.Render("Created:") + valueStyle.Render(job.CreatedAt) + "\n")

	if job.Error != "" {
		b.WriteString("\n" + lipgloss.NewStyle().Foreground(lipgloss.Color("196")).Bold(true).Render("Error:") + "\n")
		b.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color("196")).Render(job.Error) + "\n")
	}

	if len(job.Artifacts) > 0 {
		b.WriteString("\n" + labelStyle.Render("Artifacts:") + valueStyle.Render(fmt.Sprintf("%d", len(job.Artifacts))) + "\n")
	}

	return lipgloss.NewStyle().Padding(1, 2).Render(b.String())
}

func (m model) renderTabResult() string {
	job := m.selected
	if job.ResultData == nil {
		return lipgloss.NewStyle().Padding(1, 2).Foreground(lipgloss.Color("240")).Render("(no result data)")
	}

	resultJSON, err := json.MarshalIndent(job.ResultData, "", "  ")
	if err != nil {
		return fmt.Sprintf("Error formatting result: %v", err)
	}

	return lipgloss.NewStyle().Padding(1, 2).Render(string(resultJSON))
}

func (m model) renderTabStderr() string {
	job := m.selected
	if job.Stderr == "" {
		return lipgloss.NewStyle().Padding(1, 2).Foreground(lipgloss.Color("240")).Render("(no stderr output)")
	}

	return lipgloss.NewStyle().Padding(1, 2).Render(job.Stderr)
}

func (m model) renderTabArtifacts() string {
	job := m.selected
	if len(job.Artifacts) == 0 {
		return lipgloss.NewStyle().Padding(1, 2).Foreground(lipgloss.Color("240")).Render("(no artifacts)")
	}

	var b strings.Builder
	for _, digest := range job.Artifacts {
		b.WriteString(fmt.Sprintf("• %s\n", digest))
	}
	return lipgloss.NewStyle().Padding(1, 2).Render(b.String())
}

// renderStats renders the job statistics view
func (m model) renderStats() string {
	if m.stats == nil {
		if m.loading {
			return "\n  Loading stats... " + m.spinner.View()
		}
		return "\n  No statistics available."
	}

	titleStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("170")).MarginBottom(1)
	panelStyle := lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).Padding(0, 1).MarginBottom(1)
	labelStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
	valueStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("252")).Bold(true)

	var b strings.Builder
	b.WriteString(titleStyle.Render("Job Statistics") + "\n\n")

	// Overview panel
	overviewContent := titleStyle.MarginBottom(0).Render("Overview") + "\n"
	overviewContent += fmt.Sprintf("  Total Jobs: %s\n", valueStyle.Render(fmt.Sprintf("%d", m.stats.Total)))
	overviewContent += fmt.Sprintf("  Last Hour:  %s\n", valueStyle.Render(fmt.Sprintf("%d", m.stats.Recent.LastHour)))
	overviewContent += fmt.Sprintf("  Last Day:   %s\n", valueStyle.Render(fmt.Sprintf("%d", m.stats.Recent.LastDay)))
	b.WriteString(panelStyle.Render(overviewContent) + "\n")

	// By State panel
	if len(m.stats.ByState) > 0 {
		stateContent := titleStyle.MarginBottom(0).Render("By State") + "\n"
		for state, count := range m.stats.ByState {
			stStyle := stateColor(state)
			stateContent += fmt.Sprintf("  %s: %d\n", stStyle.Render(fmt.Sprintf("%-10s", state)), count)
		}
		b.WriteString(panelStyle.Render(stateContent) + "\n")
	}

	// By Command panel (top 10)
	if len(m.stats.ByCommand) > 0 {
		cmdContent := titleStyle.MarginBottom(0).Render("By Command (Top 10)") + "\n"
		count := 0
		for cmd, n := range m.stats.ByCommand {
			if count >= 10 {
				break
			}
			cmdContent += fmt.Sprintf("  %s: %s\n",
				labelStyle.Render(safeSlice(cmd, 20)),
				valueStyle.Render(fmt.Sprintf("%d", n)))
			count++
		}
		b.WriteString(panelStyle.Render(cmdContent) + "\n")
	}

	return b.String()
}

// renderBlackboard renders the blackboard records view
func (m model) renderBlackboard() string {
	if m.loading && len(m.bbRecords) == 0 {
		return "\n  Loading blackboard... " + m.spinner.View()
	}

	titleStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("170")).MarginBottom(1)
	labelStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
	highlightStyle := lipgloss.NewStyle().
		Background(lipgloss.Color("226")).
		Foreground(lipgloss.Color("0")).
		Bold(true)

	var b strings.Builder
	b.WriteString(titleStyle.Render(fmt.Sprintf("Blackboard: %s/%s", m.bbNS, m.bbTopic)) + "\n\n")

	if len(m.bbRecords) == 0 {
		b.WriteString("  " + labelStyle.Render("(no records)"))
		return b.String()
	}

	for i, rec := range m.bbRecords {
		cursor := "  "
		if i == m.bbCursor {
			cursor = "> "
		}

		// Format timestamp
		ts := time.Unix(rec.TS, 0).Format("15:04:05")

		// Show lease indicator if present
		leaseIndicator := ""
		if rec.Lease != nil {
			leaseIndicator = " [LEASED]"
		}

		line := fmt.Sprintf("%-12s %s%s",
			safeSlice(rec.ID, 12),
			ts,
			leaseIndicator)

		if i == m.bbCursor {
			b.WriteString(cursor + highlightStyle.Render(line) + "\n")
		} else {
			b.WriteString(cursor + line + "\n")
		}

		// Show payload preview
		payload := truncate(rec.Payload, 60)
		b.WriteString(fmt.Sprintf("    %s\n\n", labelStyle.Render(payload)))
	}

	return b.String()
}

// viewBlackboardDetail renders the blackboard record detail view
func (m model) viewBlackboardDetail() string {
	rec := m.selectedBB
	if rec == nil {
		return ""
	}

	sidebarWidth := 20
	mainWidth := m.width - sidebarWidth

	var b strings.Builder
	titleStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("170"))
	labelStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("240")).Width(12)
	valueStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("252"))

	b.WriteString(titleStyle.Render("Blackboard Record") + "\n\n")

	b.WriteString(labelStyle.Render("ID:") + valueStyle.Render(rec.ID) + "\n")
	b.WriteString(labelStyle.Render("Namespace:") + valueStyle.Render(rec.NS) + "\n")
	b.WriteString(labelStyle.Render("Topic:") + valueStyle.Render(rec.Topic) + "\n")
	b.WriteString(labelStyle.Render("Timestamp:") + valueStyle.Render(time.Unix(rec.TS, 0).Format(time.RFC3339)) + "\n")
	b.WriteString(labelStyle.Render("TTL:") + valueStyle.Render(fmt.Sprintf("%d sec", rec.TTLSec)) + "\n")

	if rec.CASRef != "" {
		b.WriteString(labelStyle.Render("CAS Ref:") + valueStyle.Render(rec.CASRef) + "\n")
	}

	if rec.Lease != nil {
		leaseStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("214"))
		b.WriteString("\n" + leaseStyle.Bold(true).Render("Lease Info:") + "\n")
		b.WriteString(labelStyle.Render("  Holder:") + valueStyle.Render(rec.Lease.Holder) + "\n")
		b.WriteString(labelStyle.Render("  Until:") + valueStyle.Render(time.Unix(rec.Lease.Until, 0).Format(time.RFC3339)) + "\n")
	}

	if rec.Payload != "" {
		b.WriteString("\n" + labelStyle.Render("Payload:") + "\n")
		// Try to pretty-print JSON
		var prettyPayload string
		var jsonData any
		if err := json.Unmarshal([]byte(rec.Payload), &jsonData); err == nil {
			if pretty, err := json.MarshalIndent(jsonData, "", "  "); err == nil {
				prettyPayload = string(pretty)
			} else {
				prettyPayload = rec.Payload
			}
		} else {
			prettyPayload = rec.Payload
		}
		b.WriteString(valueStyle.Render(prettyPayload) + "\n")
	}

	// Footer
	footerStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("240")).
		Border(lipgloss.NormalBorder(), true, false, false, false).
		BorderForeground(lipgloss.Color("238"))

	help := "esc: back"
	b.WriteString("\n\n" + footerStyle.Width(mainWidth-4).Render(help))

	return lipgloss.NewStyle().Padding(1, 2).Render(b.String())
}

func loadJobs(limit int) ([]jobSummary, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}

	jobsRoot := filepath.Join(home, ".foxctl", "jobs")
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	store, err := jobstore.Open(ctx, jobsRoot)
	if err != nil {
		return nil, err
	}
	defer func() {
		_ = store.Close() //nolint:errcheck
	}()

	jobsList, err := store.List(ctx, limit)
	if err != nil {
		return nil, err
	}

	summaries := make([]jobSummary, 0, len(jobsList))
	for _, job := range jobsList {
		summaries = append(summaries, jobSummary{
			ID:        job.ID,
			Command:   job.Command,
			State:     string(job.State),
			CreatedAt: job.CreatedAt.UTC().Format(time.RFC3339),
			Error:     job.Error,
		})
	}

	return summaries, nil
}

// renderSQLite renders the SQLite browser view
func (m model) renderSQLite() string {
	if m.loading && len(m.sqliteDatabases) == 0 {
		return "\n  Loading SQLite databases... " + m.spinner.View()
	}

	titleStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("170")).MarginBottom(1)
	labelStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
	highlightStyle := lipgloss.NewStyle().
		Background(lipgloss.Color("226")).
		Foreground(lipgloss.Color("0")).
		Bold(true)
	activePane := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("214"))
	inactivePane := lipgloss.NewStyle().Foreground(lipgloss.Color("240"))

	var b strings.Builder

	// Header with pane indicator
	paneLabels := []string{"Databases", "Tables", "Data"}
	var paneHeader []string
	for i, label := range paneLabels {
		if sqlitePane(i) == m.sqlitePane {
			paneHeader = append(paneHeader, activePane.Render("["+label+"]"))
		} else {
			paneHeader = append(paneHeader, inactivePane.Render(label))
		}
	}
	b.WriteString(titleStyle.Render("SQLite Browser") + "  " + strings.Join(paneHeader, " > ") + "\n\n")

	switch m.sqlitePane {
	case paneDatabases:
		b.WriteString(m.renderSQLiteDatabases(labelStyle, highlightStyle))
	case paneTables:
		b.WriteString(m.renderSQLiteTables(labelStyle, highlightStyle))
	case paneData:
		b.WriteString(m.renderSQLiteData(labelStyle))
	}

	// Show schema if available
	if m.sqliteSchema != "" {
		schemaStyle := lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("238")).
			Padding(0, 1).
			MarginTop(1)
		b.WriteString("\n" + schemaStyle.Render(m.sqliteSchema))
	}

	return b.String()
}

// renderSQLiteDatabases renders the database list
func (m model) renderSQLiteDatabases(labelStyle, highlightStyle lipgloss.Style) string {
	if len(m.sqliteDatabases) == 0 {
		return labelStyle.Render("  (no databases found in ~/.foxctl)")
	}

	var b strings.Builder
	b.WriteString(labelStyle.Render(fmt.Sprintf("  %d databases found:\n\n", len(m.sqliteDatabases))))

	for i, db := range m.sqliteDatabases {
		cursor := "  "
		if i == m.sqliteSelectedDB {
			cursor = "> "
		}

		friendlyName := db.getFriendlyName()
		size := formatBytes(db.Size)
		line := fmt.Sprintf("%-20s %s", friendlyName, size)

		if i == m.sqliteSelectedDB {
			b.WriteString(cursor + highlightStyle.Render(line) + "\n")
		} else {
			b.WriteString(cursor + line + "\n")
		}
		b.WriteString(fmt.Sprintf("    %s\n", labelStyle.Render(db.Path)))
	}

	return b.String()
}

// renderSQLiteTables renders the table list for the selected database
func (m model) renderSQLiteTables(labelStyle, highlightStyle lipgloss.Style) string {
	if len(m.sqliteDatabases) == 0 {
		return labelStyle.Render("  (no database selected)")
	}

	db := m.sqliteDatabases[m.sqliteSelectedDB]
	var b strings.Builder
	b.WriteString(lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("214")).Render(db.getFriendlyName()) + "\n")
	b.WriteString(labelStyle.Render(db.Path) + "\n\n")

	if m.loading {
		b.WriteString("  Loading tables... " + m.spinner.View())
		return b.String()
	}

	if len(m.sqliteTables) == 0 {
		b.WriteString(labelStyle.Render("  (no tables)"))
		return b.String()
	}

	b.WriteString(labelStyle.Render(fmt.Sprintf("  %d tables:\n\n", len(m.sqliteTables))))

	for i, table := range m.sqliteTables {
		cursor := "  "
		if i == m.sqliteSelectedTable {
			cursor = "> "
		}

		line := fmt.Sprintf("%-25s %d rows", table.Name, table.RowCount)

		if i == m.sqliteSelectedTable {
			b.WriteString(cursor + highlightStyle.Render(line) + "\n")
		} else {
			b.WriteString(cursor + line + "\n")
		}
	}

	b.WriteString("\n" + labelStyle.Render("  Press 'i' to view table schema"))

	return b.String()
}

// renderSQLiteData renders the data view for the selected table
func (m model) renderSQLiteData(labelStyle lipgloss.Style) string {
	if len(m.sqliteDatabases) == 0 || len(m.sqliteTables) == 0 {
		return labelStyle.Render("  (no table selected)")
	}

	db := m.sqliteDatabases[m.sqliteSelectedDB]
	table := m.sqliteTables[m.sqliteSelectedTable]

	var b strings.Builder
	b.WriteString(lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("214")).Render(
		fmt.Sprintf("%s > %s", db.getFriendlyName(), table.Name)) + "\n")
	b.WriteString(labelStyle.Render(fmt.Sprintf("%d rows", table.RowCount)) + "\n\n")

	if m.loading {
		b.WriteString("  Loading data... " + m.spinner.View())
		return b.String()
	}

	if len(m.sqliteColumns) == 0 || len(m.sqliteRows) == 0 {
		b.WriteString(labelStyle.Render("  (no data)"))
		return b.String()
	}

	// Render table header
	headerStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("170"))
	var headers []string
	for _, col := range m.sqliteColumns {
		headers = append(headers, headerStyle.Render(fmt.Sprintf("%-15s", safeSlice(col, 15))))
	}
	b.WriteString("  " + strings.Join(headers, " | ") + "\n")
	b.WriteString("  " + strings.Repeat("─", min(len(m.sqliteColumns)*18, 80)) + "\n")

	// Render rows (limit to first 20 for display)
	rowLimit := min(len(m.sqliteRows), 20)
	for i := 0; i < rowLimit; i++ {
		row := m.sqliteRows[i]
		var cells []string
		for _, col := range m.sqliteColumns {
			val := formatCellValue(row[col], 15)
			cells = append(cells, fmt.Sprintf("%-15s", val))
		}
		b.WriteString("  " + strings.Join(cells, " | ") + "\n")
	}

	if len(m.sqliteRows) > rowLimit {
		b.WriteString(labelStyle.Render(fmt.Sprintf("\n  ... and %d more rows", len(m.sqliteRows)-rowLimit)))
	}

	return b.String()
}

// viewSearchResultDetail renders the search result detail view
func (m model) viewSearchResultDetail() string {
	result := m.selectedResult
	if result == nil {
		return ""
	}

	sidebarWidth := 20
	mainWidth := m.width - sidebarWidth

	var b strings.Builder
	titleStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("170"))
	labelStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("240")).Width(14)
	valueStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("252"))

	srcStyle := sourceColor(result.Source)
	b.WriteString(titleStyle.Render("Search Result") + " " + srcStyle.Render("["+result.Source+"]") + "\n\n")

	b.WriteString(labelStyle.Render("ID:") + valueStyle.Render(result.ID) + "\n")
	if result.Name != "" {
		b.WriteString(labelStyle.Render("Name:") + valueStyle.Render(result.Name) + "\n")
	}
	b.WriteString(labelStyle.Render("Path:") + valueStyle.Render(result.Path) + "\n")
	b.WriteString(labelStyle.Render("Similarity:") + lipgloss.NewStyle().Foreground(lipgloss.Color("214")).Render(fmt.Sprintf("%.2f%%", result.Similarity*100)) + "\n")
	if result.RerankScore > 0 {
		b.WriteString(labelStyle.Render("Rerank Score:") + lipgloss.NewStyle().Foreground(lipgloss.Color("82")).Render(fmt.Sprintf("%.4f", result.RerankScore)) + "\n")
	}
	b.WriteString(labelStyle.Render("Final Score:") + valueStyle.Render(fmt.Sprintf("%.4f", result.FinalScore)) + "\n")
	b.WriteString(labelStyle.Render("Rank:") + valueStyle.Render(fmt.Sprintf("#%d", result.Rank)) + "\n")
	b.WriteString(labelStyle.Render("Source Rank:") + valueStyle.Render(fmt.Sprintf("#%d", result.SourceRank)) + "\n")

	// Footer
	footerStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("240")).
		Border(lipgloss.NormalBorder(), true, false, false, false).
		BorderForeground(lipgloss.Color("238"))

	help := "esc: back"
	b.WriteString("\n\n" + footerStyle.Width(mainWidth-4).Render(help))

	return lipgloss.NewStyle().Padding(1, 2).Render(b.String())
}

// renderSearch renders the semantic search results view
func (m model) renderSearch() string {
	if m.loading && len(m.searchResults) == 0 {
		return "\n  Searching... " + m.spinner.View()
	}

	titleStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("170")).MarginBottom(1)
	labelStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
	rerankStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("82"))
	highlightStyle := lipgloss.NewStyle().
		Background(lipgloss.Color("226")).
		Foreground(lipgloss.Color("0")).
		Bold(true)

	var b strings.Builder

	// Header with query
	header := fmt.Sprintf("Search: %q", m.searchQuery)
	if m.searchRerank {
		header += " " + rerankStyle.Render("(reranked)")
	}
	b.WriteString(titleStyle.Render(header) + "\n")

	// Stats line
	if m.searchStats != nil {
		b.WriteString(labelStyle.Render(fmt.Sprintf("   %d results | %dms", m.searchStats.TotalResults, m.searchStats.LatencyMS)) + "\n")
	}
	b.WriteString("\n")

	if len(m.searchResults) == 0 {
		b.WriteString("  " + labelStyle.Render("(no results)"))
		return b.String()
	}

	// Group results by source
	bySource := make(map[string][]int) // source -> indices in searchResults
	for i, r := range m.searchResults {
		bySource[r.Source] = append(bySource[r.Source], i)
	}

	// Render results by source group
	sourceOrder := []string{"symbols", "symbol", "sessions", "session", "memories", "memory", "tasks", "task"}
	for _, source := range sourceOrder {
		indices, ok := bySource[source]
		if !ok || len(indices) == 0 {
			continue
		}

		srcStyle := sourceColor(source)
		b.WriteString(srcStyle.Render(fmt.Sprintf("── %s (%d) ──", titleCase(source), len(indices))) + "\n")

		for _, idx := range indices {
			r := m.searchResults[idx]
			cursor := "  "
			if idx == m.searchCursor {
				cursor = "> "
			}

			// Score display
			scoreStr := fmt.Sprintf("%.0f%%", r.Similarity*100)
			if r.RerankScore > 0 {
				scoreStr = fmt.Sprintf("%.0f%%→%.2f", r.Similarity*100, r.RerankScore)
			}

			// Path display (truncate if needed)
			path := r.Path
			if r.Name != "" && r.Name != r.Path {
				path = r.Name + " " + labelStyle.Render(r.Path)
			}
			if len(path) > 60 {
				path = "..." + path[len(path)-57:]
			}

			line := fmt.Sprintf("[%s] %s", scoreStr, path)

			if idx == m.searchCursor {
				b.WriteString(cursor + highlightStyle.Render(line) + "\n")
			} else {
				b.WriteString(cursor + line + "\n")
			}
		}
		b.WriteString("\n")
	}

	return b.String()
}
