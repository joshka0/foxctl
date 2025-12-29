package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/jkatigb/agentctl/cmd/agentctl_web/templates"
	jobstore "github.com/jkatigb/agentctl/internal/storage/jobs"
)

// DataService provides data fetching for the web UI
type DataService struct {
	workspace string
}

// NewDataService creates a new data service
func NewDataService(workspace string) *DataService {
	return &DataService{workspace: workspace}
}

// getJobsRoot returns the path to the jobs directory
func getJobsRoot() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".agentctl", "jobs"), nil
}

// GetJobs fetches job summaries, filtered by workspace if set
func (d *DataService) GetJobs(state string, limit int) ([]templates.JobSummary, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	jobsRoot, err := getJobsRoot()
	if err != nil {
		return nil, err
	}

	store, err := jobstore.Open(ctx, jobsRoot)
	if err != nil {
		return nil, err
	}
	defer store.Close()

	// Fetch more jobs than limit to account for filtering
	fetchLimit := limit
	if d.workspace != "" {
		fetchLimit = limit * 5 // Fetch more to filter
	}

	jobs, err := store.List(ctx, fetchLimit)
	if err != nil {
		return nil, err
	}

	var result []templates.JobSummary
	for _, j := range jobs {
		// Filter by workspace if set
		if d.workspace != "" {
			jobWorkspace := d.getJobWorkspace(jobsRoot, j.ID)
			if jobWorkspace != "" && jobWorkspace != d.workspace {
				continue
			}
		}

		summary := templates.JobSummary{
			ID:        j.ID,
			Command:   j.Command,
			State:     string(j.State),
			CreatedAt: j.CreatedAt.Format(time.RFC3339),
			Error:     j.Error,
		}

		// Split command info
		summary.Type, summary.Category, summary.Skill = parseCommand(j.Command)

		// Apply state filter
		if state != "" && summary.State != state {
			continue
		}

		result = append(result, summary)

		// Stop if we have enough
		if len(result) >= limit {
			break
		}
	}

	return result, nil
}

// getJobWorkspace reads the workspace file for a job
func (d *DataService) getJobWorkspace(jobsRoot, jobID string) string {
	data, err := os.ReadFile(filepath.Join(jobsRoot, jobID, "workspace"))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}

// GetJobDetail fetches full job details
func (d *DataService) GetJobDetail(id string) (*templates.JobDetail, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	jobsRoot, err := getJobsRoot()
	if err != nil {
		return nil, err
	}

	store, err := jobstore.Open(ctx, jobsRoot)
	if err != nil {
		return nil, err
	}
	defer store.Close()

	job, err := store.Get(ctx, id)
	if err != nil {
		return nil, err
	}

	detail := &templates.JobDetail{
		JobSummary: templates.JobSummary{
			ID:        job.ID,
			Command:   job.Command,
			State:     string(job.State),
			CreatedAt: job.CreatedAt.Format(time.RFC3339),
			Error:     job.Error,
		},
	}
	detail.Type, detail.Category, detail.Skill = parseCommand(job.Command)

	// Try to load result
	resultBytes, err := store.Result(ctx, id)
	if err == nil && len(resultBytes) > 0 {
		var envelope struct {
			Data any `json:"data"`
		}
		if err := json.Unmarshal(resultBytes, &envelope); err == nil {
			detail.ResultData = envelope.Data
		}
	}

	// Load stderr
	jobDir := filepath.Join(jobsRoot, id)
	stderrBytes, _ := os.ReadFile(filepath.Join(jobDir, "stderr.log"))
	detail.Stderr = string(stderrBytes)

	// List artifacts
	if entries, err := os.ReadDir(jobDir); err == nil {
		for _, entry := range entries {
			name := entry.Name()
			if name != "result.json" && name != "input.json" && name != "stderr.log" && name != "workspace" && name != "progress.ndjson" {
				detail.Artifacts = append(detail.Artifacts, name)
			}
		}
	}

	return detail, nil
}

func parseCommand(cmd string) (typ, cat, skill string) {
	// Format: type:category/skill
	// Example: skill:code/symbols

	parts := strings.SplitN(cmd, ":", 2)
	if len(parts) == 2 {
		typ = parts[0]
		subParts := strings.SplitN(parts[1], "/", 2)
		if len(subParts) == 2 {
			cat = subParts[0]
			skill = subParts[1]
		} else {
			skill = subParts[0]
		}
	} else {
		// Fallback for commands without ":"
		typ = "cmd"
		skill = cmd
	}
	return
}

// GetTasks fetches task recommendations using todo/manage skill
func (d *DataService) GetTasks(limit int) ([]templates.TaskSummary, error) {
	if limit <= 0 {
		limit = 100
	}

	input := map[string]any{
		"action": "list",
		"limit":  limit,
	}

	// Add workspace filter if set
	if d.workspace != "" {
		input["workspace"] = d.workspace
	}

	output, err := d.runAgentctl("todo/manage", input)
	if err != nil {
		return []templates.TaskSummary{}, nil // Return empty on error
	}

	var envelope struct {
		Data struct {
			Tasks []templates.TaskSummary `json:"tasks"`
		} `json:"data"`
	}
	if err := json.Unmarshal(output, &envelope); err != nil {
		return []templates.TaskSummary{}, nil
	}

	return envelope.Data.Tasks, nil
}

// GetStats computes job statistics
func (d *DataService) GetStats() (*templates.JobStats, error) {
	jobs, err := d.GetJobs("", 1000)
	if err != nil {
		return nil, err
	}

	stats := &templates.JobStats{
		Total:     len(jobs),
		ByState:   make(map[string]int),
		ByCommand: make(map[string]int),
	}

	now := time.Now()
	for _, j := range jobs {
		stats.ByState[j.State]++
		stats.ByCommand[j.Command]++

		if created, err := time.Parse(time.RFC3339, j.CreatedAt); err == nil {
			if now.Sub(created) < time.Hour {
				stats.Recent.LastHour++
			}
			if now.Sub(created) < 24*time.Hour {
				stats.Recent.LastDay++
			}
		}
	}

	return stats, nil
}

// GetInsights fetches graph insights from todo/manage skill
func (d *DataService) GetInsights() (*templates.InsightsData, error) {
	// Get graph insights
	output, err := d.runAgentctl("todo/manage", map[string]any{
		"operation": "graph_insights",
	})
	if err != nil {
		return &templates.InsightsData{}, nil
	}

	var envelope struct {
		Data struct {
			Insights templates.InsightsData `json:"insights"`
		} `json:"data"`
	}
	if err := json.Unmarshal(output, &envelope); err != nil {
		return &templates.InsightsData{}, nil
	}

	// Get task list to map IDs to titles
	tasks, _ := d.GetTasks(200)
	taskTitles := make(map[string]string)
	for _, t := range tasks {
		taskTitles[t.TaskID] = t.Title
	}

	// Enrich nodes with titles
	for i := range envelope.Data.Insights.Nodes {
		if title, ok := taskTitles[envelope.Data.Insights.Nodes[i].TaskID]; ok {
			envelope.Data.Insights.Nodes[i].Title = title
		}
	}

	return &envelope.Data.Insights, nil
}

// GetMailbox fetches mailbox messages
func (d *DataService) GetMailbox(actorID string, limit int) ([]templates.MailboxMessage, error) {
	if actorID == "" {
		return []templates.MailboxMessage{}, nil
	}

	output, err := d.runAgentctl("harness/actor", map[string]any{
		"operation":    "inbox",
		"workspace_id": d.workspace,
		"inbox": map[string]any{
			"actor_id":    actorID,
			"only_unread": false,
			"limit":       limit,
		},
	})
	if err != nil {
		return []templates.MailboxMessage{}, nil
	}

	var envelope struct {
		Data struct {
			Messages []templates.MailboxMessage `json:"messages"`
		} `json:"data"`
	}
	if err := json.Unmarshal(output, &envelope); err != nil {
		return []templates.MailboxMessage{}, nil
	}

	return envelope.Data.Messages, nil
}

// GetReservations fetches file reservations
func (d *DataService) GetReservations() ([]templates.Reservation, error) {
	output, err := d.runAgentctl("harness/locksmith", map[string]any{
		"operation":    "list",
		"workspace_id": d.workspace,
	})
	if err != nil {
		return []templates.Reservation{}, nil
	}

	var envelope struct {
		Data struct {
			Reservations []templates.Reservation `json:"reservations"`
		} `json:"data"`
	}
	if err := json.Unmarshal(output, &envelope); err != nil {
		return []templates.Reservation{}, nil
	}

	return envelope.Data.Reservations, nil
}

// GetBlackboard fetches blackboard records using agentctl bb list
func (d *DataService) GetBlackboard(ns, topic string, limit int) ([]templates.BlackboardRecord, error) {
	if limit <= 0 {
		limit = 50
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Use agentctl bb list command directly
	args := []string{"bb", "list", "--ns", ns}
	if topic != "" {
		args = append(args, "--topic", topic)
	}
	args = append(args, "--limit", fmt.Sprintf("%d", limit), "--json")

	cmd := exec.CommandContext(ctx, "agentctl", args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return []templates.BlackboardRecord{}, nil
	}

	// Parse the JSON envelope response
	var envelope struct {
		Data struct {
			Records []templates.BlackboardRecord `json:"records"`
		} `json:"data"`
	}
	if err := json.Unmarshal(output, &envelope); err != nil {
		// Try direct array format as fallback
		var records []templates.BlackboardRecord
		if err2 := json.Unmarshal(output, &records); err2 != nil {
			return []templates.BlackboardRecord{}, nil
		}
		return records, nil
	}
	return envelope.Data.Records, nil
}

// GetSQLiteDatabases lists available SQLite databases
func (d *DataService) GetSQLiteDatabases() ([]templates.SQLiteDatabase, error) {
	dbs, err := discoverDatabases()
	if err != nil {
		return []templates.SQLiteDatabase{}, nil
	}

	var result []templates.SQLiteDatabase
	for _, db := range dbs {
		result = append(result, templates.SQLiteDatabase{
			Name:         db.Name,
			FriendlyName: db.FriendlyName,
			Path:         db.Path,
			Size:         db.Size,
		})
	}

	return result, nil
}

// GetSQLiteTables lists tables in a database
func (d *DataService) GetSQLiteTables(dbName string) ([]templates.SQLiteTable, error) {
	dbPath, err := resolveDatabasePath(dbName)
	if err != nil {
		return []templates.SQLiteTable{}, nil
	}

	tables, err := listTables(dbPath)
	if err != nil {
		return []templates.SQLiteTable{}, nil
	}

	var result []templates.SQLiteTable
	for _, t := range tables {
		result = append(result, templates.SQLiteTable{
			Name:     t.Name,
			RowCount: int(t.RowCount),
		})
	}

	return result, nil
}

// GetSQLiteData fetches data from a table
func (d *DataService) GetSQLiteData(dbName, tableName string, limit int) ([]string, []map[string]any, error) {
	dbPath, err := resolveDatabasePath(dbName)
	if err != nil {
		return nil, nil, err
	}

	columns, rows, err := fetchTableRows(dbPath, tableName, limit)
	if err != nil {
		return nil, nil, err
	}

	// Convert SQLiteRowData to map[string]any
	var result []map[string]any
	for _, row := range rows {
		result = append(result, map[string]any(row))
	}

	return columns, result, nil
}

// GetSQLiteSchema returns the CREATE TABLE statement for a table
func (d *DataService) GetSQLiteSchema(dbName, tableName string) (string, error) {
	dbPath, err := resolveDatabasePath(dbName)
	if err != nil {
		return "", err
	}

	return getTableSchema(dbPath, tableName)
}

// GetSearch runs semantic search with optional reranking
func (d *DataService) GetSearch(query string, limit int, rerank bool, scopes []string) ([]templates.SearchResult, *templates.SearchStats, error) {
	if query == "" {
		return []templates.SearchResult{}, &templates.SearchStats{}, nil
	}

	if len(scopes) == 0 {
		scopes = []string{"symbols", "sessions", "memories", "tasks"}
	}

	input := map[string]any{
		"query":          query,
		"scope":          scopes,
		"limit":          limit,
		"summarize":      false,
		"rerank_enabled": rerank,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	inputJSON, err := json.Marshal(input)
	if err != nil {
		return nil, nil, err
	}

	cmd := exec.CommandContext(ctx, "agentctl", "run", "code/semantic_search", "--input", string(inputJSON))
	output, err := cmd.CombinedOutput()
	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return nil, nil, fmt.Errorf("search timed out")
		}
		return nil, nil, fmt.Errorf("search failed: %w: %s", err, string(output))
	}

	var envelope struct {
		Status string `json:"status"`
		Data   struct {
			Query   string `json:"query"`
			Results []struct {
				Source      string  `json:"source"`
				ID          string  `json:"id"`
				Name        string  `json:"name"`
				Path        string  `json:"path"`
				Similarity  float64 `json:"similarity"`
				RerankScore float64 `json:"rerank_score"`
				FinalScore  float64 `json:"final_score"`
				Rank        int     `json:"rank"`
				SourceRank  int     `json:"source_rank"`
			} `json:"results"`
			Stats struct {
				TotalResults    int            `json:"total_results"`
				SourceCounts    map[string]int `json:"source_counts"`
				EmbeddingDims   int            `json:"embedding_dimensions"`
				SourceLatencies map[string]int `json:"source_latencies_ms"`
			} `json:"stats"`
		} `json:"data"`
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
	}

	if err := json.Unmarshal(output, &envelope); err != nil {
		return nil, nil, fmt.Errorf("parse response: %w", err)
	}

	if envelope.Status == "error" && envelope.Error.Message != "" {
		return nil, nil, fmt.Errorf("search error: %s", envelope.Error.Message)
	}

	results := make([]templates.SearchResult, 0, len(envelope.Data.Results))
	for _, r := range envelope.Data.Results {
		results = append(results, templates.SearchResult{
			Source:      r.Source,
			ID:          r.ID,
			Name:        r.Name,
			Path:        r.Path,
			Similarity:  r.Similarity,
			RerankScore: r.RerankScore,
			FinalScore:  r.FinalScore,
			Rank:        r.Rank,
			SourceRank:  r.SourceRank,
		})
	}

	// Calculate total latency
	var totalLatency int64
	for _, lat := range envelope.Data.Stats.SourceLatencies {
		totalLatency += int64(lat)
	}

	stats := &templates.SearchStats{
		TotalResults:  envelope.Data.Stats.TotalResults,
		SourceCounts:  envelope.Data.Stats.SourceCounts,
		Reranked:      rerank,
		EmbeddingDims: envelope.Data.Stats.EmbeddingDims,
		LatencyMS:     totalLatency,
	}

	return results, stats, nil
}

// GetWorkspaces discovers known workspaces from sessions database
func (d *DataService) GetWorkspaces() ([]templates.Workspace, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}

	dbPath := filepath.Join(home, ".agentctl", "storage", "sessions.db")
	if _, err := os.Stat(dbPath); os.IsNotExist(err) {
		return []templates.Workspace{}, nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Query distinct workspaces with session counts and last used
	query := `
		SELECT
			workspace_path,
			COUNT(*) as session_count,
			MAX(started_at) as last_used
		FROM sessions
		WHERE workspace_path != '' AND workspace_path NOT LIKE '/tmp/%'
		GROUP BY workspace_path
		ORDER BY last_used DESC
		LIMIT 20
	`

	cmd := exec.CommandContext(ctx, "sqlite3", "-json", dbPath, query)
	output, err := cmd.Output()
	if err != nil {
		return []templates.Workspace{}, nil
	}

	var rows []struct {
		WorkspacePath string `json:"workspace_path"`
		SessionCount  int    `json:"session_count"`
		LastUsed      string `json:"last_used"`
	}
	if err := json.Unmarshal(output, &rows); err != nil {
		return []templates.Workspace{}, nil
	}

	workspaces := make([]templates.Workspace, 0, len(rows))
	for _, row := range rows {
		workspaces = append(workspaces, templates.Workspace{
			Path:         row.WorkspacePath,
			Name:         filepath.Base(row.WorkspacePath),
			SessionCount: row.SessionCount,
			LastUsed:     row.LastUsed,
		})
	}

	return workspaces, nil
}

// runAgentctl executes an agentctl skill and returns the output
func (d *DataService) runAgentctl(skill string, input map[string]any) ([]byte, error) {
	inputJSON, err := json.Marshal(input)
	if err != nil {
		return nil, err
	}

	cmd := exec.Command("agentctl", "run", skill, "--input", string(inputJSON))
	output, err := cmd.Output()
	if err != nil {
		return nil, err
	}

	// Strip any debug output before the JSON
	idx := strings.Index(string(output), "{")
	if idx > 0 {
		output = output[idx:]
	}

	return output, nil
}
