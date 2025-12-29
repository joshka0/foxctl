package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	jobstore "github.com/jkatigb/agentctl/internal/storage/jobs"
)

func handleRobotJobs(workspace, state string, limit int) {
	home, err := os.UserHomeDir()
	if err != nil {
		writeJSON(newErrorEnvelope("viewer/jobs", "HOME_DIR_ERROR", fmt.Sprintf("cannot get home directory: %v", err)))
		os.Exit(1)
	}
	jobsRoot := filepath.Join(home, ".agentctl", "jobs")
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	store, err := jobstore.Open(ctx, jobsRoot)
	if err != nil {
		writeJSON(newErrorEnvelope("viewer/jobs", "OPEN_STORE_ERROR", err.Error()))
		os.Exit(1)
	}
	defer func() {
		_ = store.Close() //nolint:errcheck
	}()

	if limit <= 0 {
		limit = 50
	}

	jobs := make([]jobSummary, 0, limit)
	batch := limit
	if state != "" && batch < 100 {
		batch = 100
	}
	const maxBatch = 5000

	for {
		jobsList, err := store.List(ctx, batch)
		if err != nil {
			writeJSON(newErrorEnvelope("viewer/jobs", "LIST_ERROR", err.Error()))
			os.Exit(1)
		}

		jobs = jobs[:0] // Reset length but keep capacity
		for _, job := range jobsList {
			summary := jobSummary{
				ID:        job.ID,
				Command:   job.Command,
				State:     string(job.State),
				CreatedAt: job.CreatedAt.UTC().Format(time.RFC3339),
				Error:     job.Error,
			}
			if state != "" && summary.State != state {
				continue
			}
			jobs = append(jobs, summary)
			if len(jobs) >= limit {
				break
			}
		}

		if state == "" || len(jobs) >= limit || len(jobsList) < batch || batch >= maxBatch {
			break
		}
		batch *= 2
		if batch > maxBatch {
			batch = maxBatch
		}
	}

	// Compute summary counts from filtered jobs
	var okCount, errCount, runCount, queuedCount, canceledCount int
	for _, job := range jobs {
		switch job.State {
		case "ok":
			okCount++
		case "error":
			errCount++
		case "running":
			runCount++
		case "queued":
			queuedCount++
		case "canceled":
			canceledCount++
		}
	}

	meta := map[string]any{
		"workspace": getWorkspace(workspace),
		"filter":    state,
		"summary": map[string]int{
			"total":    len(jobs),
			"ok":       okCount,
			"error":    errCount,
			"running":  runCount,
			"queued":   queuedCount,
			"canceled": canceledCount,
		},
	}

	writeJSON(newEnvelope("viewer/jobs", jobs, meta))
}

func handleRobotJobDetail(jobID string) {
	jobDir, err := getJobDir(jobID)
	if err != nil {
		writeJSON(newErrorEnvelope("viewer/job", "INVALID_ID", err.Error()))
		os.Exit(1)
	}

	// Resolve symlinks once for TOCTOU protection
	canonicalJobDir, err := filepath.EvalSymlinks(jobDir)
	if err != nil {
		writeJSON(newErrorEnvelope("viewer/job", "PATH_ERROR", fmt.Sprintf("cannot resolve job path: %v", err)))
		os.Exit(1)
	}

	home, err := os.UserHomeDir()
	if err != nil {
		writeJSON(newErrorEnvelope("viewer/job", "HOME_DIR_ERROR", fmt.Sprintf("cannot get home directory: %v", err)))
		os.Exit(1)
	}
	jobsRoot := filepath.Join(home, ".agentctl", "jobs")

	// Validate canonical path is within jobs root
	canonicalRoot, err := filepath.EvalSymlinks(jobsRoot)
	if err != nil {
		// If symlink evaluation fails, use the original path
		canonicalRoot = jobsRoot
	}
	if canonicalRoot != "" && !strings.HasPrefix(canonicalJobDir, canonicalRoot) {
		writeJSON(newErrorEnvelope("viewer/job", "PATH_ERROR", "job path escapes jobs directory"))
		os.Exit(1)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	store, err := jobstore.Open(ctx, jobsRoot)
	if err != nil {
		writeJSON(newErrorEnvelope("viewer/job", "OPEN_STORE_ERROR", err.Error()))
		os.Exit(1)
	}
	defer func() {
		_ = store.Close() //nolint:errcheck
	}()

	job, err := store.Get(ctx, jobID)
	if err != nil {
		code := "GET_ERROR"
		if errors.Is(err, jobstore.ErrNotFound) {
			code = "NOT_FOUND"
		}
		writeJSON(newErrorEnvelope("viewer/job", code, err.Error()))
		os.Exit(1)
	}

	data := map[string]any{
		"id":         job.ID,
		"command":    job.Command,
		"state":      string(job.State),
		"created_at": job.CreatedAt.UTC().Format(time.RFC3339),
	}
	if job.Error != "" {
		data["error"] = job.Error
	}

	if resultData, err := os.ReadFile(filepath.Join(canonicalJobDir, "result.json")); err == nil {
		var envelope map[string]any
		if json.Unmarshal(resultData, &envelope) == nil {
			if d, ok := envelope["data"].(map[string]any); ok {
				data["result_data"] = d
			}
			if errObj, ok := envelope["error"].(map[string]any); ok {
				if msg, ok := errObj["message"].(string); ok && msg != "" {
					if _, exists := data["error"]; !exists {
						data["error"] = msg
					}
				}
			}
		}
	}

	if stderrData, err := os.ReadFile(filepath.Join(canonicalJobDir, "stderr.log")); err == nil {
		data["stderr"] = string(stderrData)
	}

	if artifactsData, err := os.ReadFile(filepath.Join(canonicalJobDir, "artifacts.json")); err == nil {
		var meta struct {
			Digests []string `json:"digests"`
		}
		if json.Unmarshal(artifactsData, &meta) == nil && len(meta.Digests) > 0 {
			data["artifacts"] = meta.Digests
		} else {
			var artifacts []string
			if json.Unmarshal(artifactsData, &artifacts) == nil {
				data["artifacts"] = artifacts
			}
		}
	}

	writeJSON(newEnvelope("viewer/job", data, nil))
}

func handleRobotInsights(workspace string) {
	workspace = getWorkspace(workspace)
	input, err := marshalSkillInput(skillInput{
		Operation:   "graph_insights",
		WorkspaceID: workspace,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "agentctl", "run", "todo/manage", "--input", input)
	output, err := cmd.Output()
	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			fmt.Fprintf(os.Stderr, "Error getting insights: command timed out\n")
		} else {
			fmt.Fprintf(os.Stderr, "Error getting insights: %v\n", err)
		}
		os.Exit(1)
	}
	fmt.Print(string(output))
}

func handleRobotPlan(workspace string, limit int) {
	workspace = getWorkspace(workspace)
	input, err := marshalSkillInput(skillInput{
		Operation:   "recommend",
		WorkspaceID: workspace,
		Limit:       limit,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "agentctl", "run", "todo/manage", "--input", input)
	output, err := cmd.Output()
	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			fmt.Fprintf(os.Stderr, "Error getting plan: command timed out\n")
		} else {
			fmt.Fprintf(os.Stderr, "Error getting plan: %v\n", err)
		}
		os.Exit(1)
	}
	fmt.Print(string(output))
}

func handleRobotTasks(workspace string, _ int) {
	workspace = getWorkspace(workspace)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "agentctl", "todo", "list", "--workspace", workspace)
	output, err := cmd.Output()
	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			fmt.Fprintf(os.Stderr, "Error getting tasks: command timed out\n")
		} else {
			fmt.Fprintf(os.Stderr, "Error getting tasks: %v\n", err)
		}
		os.Exit(1)
	}
	fmt.Print(string(output))
}

func handleRobotMailbox(workspace, actorID string, limit int) {
	workspace = getWorkspace(workspace)
	if limit <= 0 {
		limit = 20
	}

	input, err := marshalSkillInput(skillInput{
		Operation:   "inbox",
		WorkspaceID: workspace,
		Inbox: &inboxReq{
			ActorID:    actorID,
			OnlyUnread: false,
			Limit:      limit,
		},
	})
	if err != nil {
		writeJSON(newErrorEnvelope("viewer/mailbox", "MARSHAL_ERROR", err.Error()))
		os.Exit(1)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "agentctl", "run", "mailbox/manage", "--input", input)
	output, err := cmd.CombinedOutput()
	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			writeJSON(newErrorEnvelope("viewer/mailbox", "TIMEOUT_ERROR", "command timed out"))
			os.Exit(1)
		}
		if strings.Contains(string(output), "skill") && strings.Contains(string(output), "not found") {
			emptyResult("viewer/mailbox", actorID, workspace, "mailbox")
			return
		}
		writeJSON(newErrorEnvelope("viewer/mailbox", "SKILL_ERROR", err.Error()))
		os.Exit(1)
	}

	var envelope struct {
		Data struct {
			Messages []struct {
				ID        string `json:"id"`
				Sender    string `json:"sender"`
				Recipient string `json:"recipient"`
				Subject   string `json:"subject"`
				Body      string `json:"body"`
				Kind      string `json:"kind"`
				Priority  int    `json:"priority"`
				Status    string `json:"status"`
				CreatedAt string `json:"created_at"`
				TaskID    string `json:"task_id,omitempty"`
			} `json:"messages"`
			Count int `json:"count"`
		} `json:"data"`
	}
	if err := json.Unmarshal(output, &envelope); err != nil {
		fmt.Fprintf(os.Stderr, "failed to parse mailbox response: %v\n", err)
		writeJSON(newErrorEnvelope("viewer/mailbox", "PARSE_ERROR", err.Error()))
		return
	}

	var unread, urgent int
	for _, m := range envelope.Data.Messages {
		if m.Status == "unread" {
			unread++
		}
		if m.Priority <= 2 {
			urgent++
		}
	}

	meta := map[string]any{
		"actor_id":  actorID,
		"workspace": workspace,
		"summary": map[string]int{
			"total":  envelope.Data.Count,
			"unread": unread,
			"urgent": urgent,
		},
	}

	writeJSON(newEnvelope("viewer/mailbox", envelope.Data.Messages, meta))
}

func handleRobotReservations(workspace string) {
	workspace = getWorkspace(workspace)

	input, err := marshalSkillInput(skillInput{
		Operation:   "list_reservations",
		WorkspaceID: workspace,
	})
	if err != nil {
		writeJSON(newErrorEnvelope("viewer/reservations", "MARSHAL_ERROR", err.Error()))
		os.Exit(1)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "agentctl", "run", "mailbox/manage", "--input", input)
	output, err := cmd.CombinedOutput()
	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			writeJSON(newErrorEnvelope("viewer/reservations", "TIMEOUT_ERROR", "command timed out"))
			os.Exit(1)
		}
		if strings.Contains(string(output), "skill") && strings.Contains(string(output), "not found") {
			emptyResult("viewer/reservations", "", workspace, "reservations")
			return
		}
		writeJSON(newErrorEnvelope("viewer/reservations", "SKILL_ERROR", err.Error()))
		os.Exit(1)
	}

	var envelope struct {
		Data struct {
			Reservations []struct {
				ID        string `json:"id"`
				Path      string `json:"path"`
				Holder    string `json:"holder"`
				Mode      string `json:"mode"`
				ExpiresAt string `json:"expires_at"`
				CreatedAt string `json:"created_at"`
			} `json:"reservations"`
			Count int `json:"count"`
		} `json:"data"`
	}
	if err := json.Unmarshal(output, &envelope); err != nil {
		fmt.Fprintf(os.Stderr, "failed to parse reservations response: %v\n", err)
		writeJSON(newErrorEnvelope("viewer/reservations", "PARSE_ERROR", err.Error()))
		return
	}

	var exclusive, shared int
	holders := make(map[string]int)
	for _, r := range envelope.Data.Reservations {
		if r.Mode == "exclusive" {
			exclusive++
		} else {
			shared++
		}
		holders[r.Holder]++
	}

	meta := map[string]any{
		"workspace": workspace,
		"summary": map[string]any{
			"total":     envelope.Data.Count,
			"exclusive": exclusive,
			"shared":    shared,
			"by_holder": holders,
		},
	}

	writeJSON(newEnvelope("viewer/reservations", envelope.Data.Reservations, meta))
}

func handleRobotPriority(workspace string, limit int) {
	workspace = getWorkspace(workspace)

	input, err := marshalSkillInput(skillInput{
		Operation:   "recommend",
		WorkspaceID: workspace,
		Limit:       limit,
	})
	if err != nil {
		writeJSON(newErrorEnvelope("viewer/priority", "MARSHAL_ERROR", err.Error()))
		os.Exit(1)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "agentctl", "run", "todo/manage", "--input", input)
	output, err := cmd.Output()
	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			writeJSON(newErrorEnvelope("viewer/priority", "TIMEOUT_ERROR", "command timed out"))
		} else {
			writeJSON(newErrorEnvelope("viewer/priority", "SKILL_ERROR", err.Error()))
		}
		os.Exit(1)
	}

	var envelope struct {
		Data struct {
			Tasks []struct {
				TaskID            string  `json:"task_id"`
				Title             string  `json:"title"`
				Score             float64 `json:"score"`
				CriticalPathScore float64 `json:"critical_path_score"`
				PageRank          float64 `json:"pagerank"`
				UnreadAdmin       int     `json:"unread_admin"`
				UnreadOverseer    int     `json:"unread_overseer"`
			} `json:"tasks"`
			TopRecommended struct {
				TaskID string  `json:"task_id"`
				Title  string  `json:"title"`
				Score  float64 `json:"score"`
			} `json:"top_recommended"`
		} `json:"data"`
	}
	if err := json.Unmarshal(output, &envelope); err != nil {
		fmt.Fprintf(os.Stderr, "failed to parse priority response: %v\n", err)
		writeJSON(newErrorEnvelope("viewer/priority", "PARSE_ERROR", err.Error()))
		return
	}

	recs := make([]recommendation, 0)
	var highConfidence int
	for _, t := range envelope.Data.Tasks {
		confidence := t.Score
		if confidence > 1.0 {
			confidence = 1.0
		}
		if confidence >= 0.7 {
			highConfidence++
		}

		var reasons []string
		if t.CriticalPathScore > 0.5 {
			reasons = append(reasons, "high critical path score")
		}
		if t.PageRank > 0.1 {
			reasons = append(reasons, "high PageRank (blocks others)")
		}
		if t.UnreadAdmin > 0 {
			reasons = append(reasons, fmt.Sprintf("%d unread admin messages", t.UnreadAdmin))
		}
		if t.UnreadOverseer > 0 {
			reasons = append(reasons, fmt.Sprintf("%d unread overseer messages", t.UnreadOverseer))
		}
		if len(reasons) == 0 {
			reasons = append(reasons, "standard priority")
		}

		recs = append(recs, recommendation{
			TaskID:     t.TaskID,
			Title:      t.Title,
			Score:      t.Score,
			Confidence: confidence,
			Reasoning:  strings.Join(reasons, "; "),
		})
	}

	meta := map[string]any{
		"workspace": workspace,
		"summary": map[string]any{
			"total":           len(recs),
			"top_task":        envelope.Data.TopRecommended.Title,
			"high_confidence": highConfidence,
		},
	}

	writeJSON(newEnvelope("viewer/priority", recs, meta))
}

func handleRobotGraph(workspace string) {
	workspace = getWorkspace(workspace)

	input, err := marshalSkillInput(skillInput{
		Operation:   "graph_insights",
		WorkspaceID: workspace,
	})
	if err != nil {
		writeJSON(newErrorEnvelope("viewer/graph", "MARSHAL_ERROR", err.Error()))
		os.Exit(1)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "agentctl", "run", "todo/manage", "--input", input)
	output, err := cmd.Output()
	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			writeJSON(newErrorEnvelope("viewer/graph", "TIMEOUT_ERROR", "command timed out"))
		} else {
			writeJSON(newErrorEnvelope("viewer/graph", "SKILL_ERROR", err.Error()))
		}
		os.Exit(1)
	}

	var envelope struct {
		Data struct {
			Nodes []struct {
				TaskID            string  `json:"task_id"`
				PageRank          float64 `json:"pagerank"`
				CriticalPathScore int     `json:"critical_path_score"`
				InDegree          int     `json:"in_degree"`
				OutDegree         int     `json:"out_degree"`
			} `json:"nodes"`
			TopologicalOrder []string   `json:"topological_order"`
			Cycles           [][]string `json:"cycles"`
		} `json:"data"`
	}
	if err := json.Unmarshal(output, &envelope); err != nil {
		fmt.Fprintf(os.Stderr, "failed to parse graph response: %v\n", err)
		writeJSON(newErrorEnvelope("viewer/graph", "PARSE_ERROR", err.Error()))
		return
	}

	topNodes := make([]map[string]any, 0)
	for i, node := range envelope.Data.Nodes {
		if i >= 10 {
			break
		}
		topNodes = append(topNodes, map[string]any{
			"task_id":       node.TaskID,
			"pagerank":      node.PageRank,
			"critical_path": node.CriticalPathScore,
			"in_degree":     node.InDegree,
			"out_degree":    node.OutDegree,
		})
	}

	data := map[string]any{
		"topological_order": envelope.Data.TopologicalOrder,
		"cycles":            envelope.Data.Cycles,
		"top_nodes":         topNodes,
	}

	meta := map[string]any{
		"workspace":   workspace,
		"node_count":  len(envelope.Data.Nodes),
		"cycle_count": len(envelope.Data.Cycles),
	}

	writeJSON(newEnvelope("viewer/graph", data, meta))
}

func emptyResult(command, actorID, workspace, resultType string) {
	data := map[string]any{
		"note": fmt.Sprintf("No %s data available. Skill may not be installed.", resultType),
	}
	meta := map[string]any{
		"workspace": workspace,
	}
	if actorID != "" {
		meta["actor_id"] = actorID
	}
	writeJSON(newEnvelope(command, data, meta))
}

func handleRobotSQLite(dbName, tableName string, limit int) {
	// Case 1: No database specified - list all databases
	if dbName == "" {
		databases, err := discoverDatabases()
		if err != nil {
			writeJSON(newErrorEnvelope("viewer/sqlite", "DISCOVER_ERROR", err.Error()))
			os.Exit(1)
		}

		dbList := make([]map[string]any, 0, len(databases))
		for _, db := range databases {
			// Lazy load table count if needed
			tableCount := db.Tables
			if tableCount < 0 {
				if count, err := getTableCount(db.Path); err == nil {
					tableCount = count
				}
			}
			dbList = append(dbList, map[string]any{
				"name":        db.Name,
				"path":        db.Path,
				"size":        db.Size,
				"size_human":  formatBytes(db.Size),
				"table_count": tableCount,
			})
		}

		meta := map[string]any{
			"summary": map[string]int{
				"total": len(databases),
			},
		}
		writeJSON(newEnvelope("viewer/sqlite", map[string]any{"databases": dbList}, meta))
		return
	}

	// Find the database path
	databases, err := discoverDatabases()
	if err != nil {
		writeJSON(newErrorEnvelope("viewer/sqlite", "DISCOVER_ERROR", err.Error()))
		os.Exit(1)
	}

	var dbPath string
	for _, db := range databases {
		if db.Name == dbName || db.Name+".db" == dbName {
			dbPath = db.Path
			break
		}
	}
	if dbPath == "" {
		writeJSON(newErrorEnvelope("viewer/sqlite", "NOT_FOUND", fmt.Sprintf("database %q not found", dbName)))
		os.Exit(1)
	}

	// Case 2: Database specified but no table - list tables
	if tableName == "" {
		tables, err := listTables(dbPath)
		if err != nil {
			writeJSON(newErrorEnvelope("viewer/sqlite", "LIST_TABLES_ERROR", err.Error()))
			os.Exit(1)
		}

		tableList := make([]map[string]any, 0, len(tables))
		for _, t := range tables {
			tableList = append(tableList, map[string]any{
				"name":      t.Name,
				"row_count": t.RowCount,
			})
		}

		meta := map[string]any{
			"database": dbName,
			"summary": map[string]int{
				"total": len(tables),
			},
		}
		writeJSON(newEnvelope("viewer/sqlite", map[string]any{"tables": tableList}, meta))
		return
	}

	// Case 3: Database and table specified - get rows
	if limit <= 0 {
		limit = defaultRowLimit
	}
	if limit > maxRowLimit {
		limit = maxRowLimit
	}

	columns, rows, err := fetchTableRows(dbPath, tableName, limit)
	if err != nil {
		writeJSON(newErrorEnvelope("viewer/sqlite", "FETCH_ROWS_ERROR", err.Error()))
		os.Exit(1)
	}

	// Convert rows to serializable format
	rowList := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		rowData := make(map[string]any)
		for _, col := range columns {
			rowData[col] = row[col]
		}
		rowList = append(rowList, rowData)
	}

	// Get total row count
	tables, _ := listTables(dbPath)
	var totalRows int64
	for _, t := range tables {
		if t.Name == tableName {
			totalRows = t.RowCount
			break
		}
	}

	meta := map[string]any{
		"database": dbName,
		"table":    tableName,
		"summary": map[string]any{
			"columns":    len(columns),
			"rows":       len(rowList),
			"total_rows": totalRows,
		},
	}
	writeJSON(newEnvelope("viewer/sqlite", map[string]any{
		"columns": columns,
		"rows":    rowList,
	}, meta))
}

func handleRobotSearch(workspace, query string, limit int, rerank bool, scopes []string) {
	workspace = getWorkspace(workspace)

	if query == "" {
		writeJSON(newErrorEnvelope("viewer/search", "INVALID_QUERY", "search query is required"))
		os.Exit(1)
	}

	if limit <= 0 {
		limit = 10
	}
	if len(scopes) == 0 {
		scopes = []string{"symbols", "sessions", "memories", "tasks"}
	}

	input := searchInput{
		Query:         query,
		Scope:         scopes,
		Limit:         limit,
		Summarize:     false,
		RerankEnabled: rerank,
	}

	inputJSON, err := json.Marshal(input)
	if err != nil {
		writeJSON(newErrorEnvelope("viewer/search", "MARSHAL_ERROR", err.Error()))
		os.Exit(1)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "agentctl", "run", "code/semantic_search", "--input", string(inputJSON))
	output, err := cmd.CombinedOutput()
	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			writeJSON(newErrorEnvelope("viewer/search", "TIMEOUT_ERROR", "search timed out"))
			os.Exit(1)
		}
		writeJSON(newErrorEnvelope("viewer/search", "SKILL_ERROR", fmt.Sprintf("%v: %s", err, string(output))))
		os.Exit(1)
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
		writeJSON(newErrorEnvelope("viewer/search", "PARSE_ERROR", fmt.Sprintf("failed to parse search response: %v", err)))
		os.Exit(1)
	}

	if envelope.Status == "error" && envelope.Error.Message != "" {
		writeJSON(newErrorEnvelope("viewer/search", "SEARCH_ERROR", envelope.Error.Message))
		os.Exit(1)
	}

	// Convert to our result format
	results := make([]searchResult, 0, len(envelope.Data.Results))
	for _, r := range envelope.Data.Results {
		results = append(results, searchResult{
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

	meta := map[string]any{
		"workspace": workspace,
		"query":     query,
		"reranked":  rerank,
		"scopes":    scopes,
		"summary": map[string]any{
			"total":            envelope.Data.Stats.TotalResults,
			"source_counts":    envelope.Data.Stats.SourceCounts,
			"embedding_dims":   envelope.Data.Stats.EmbeddingDims,
			"source_latencies": envelope.Data.Stats.SourceLatencies,
		},
	}

	writeJSON(newEnvelope("viewer/search", map[string]any{
		"results": results,
	}, meta))
}
