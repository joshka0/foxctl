package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// Action result messages
type (
	jobCancelledMsg struct {
		jobID string
		err   error
	}
	taskCompletedMsg struct {
		taskID string
		err    error
	}
	taskSetActiveMsg struct {
		taskID string
		err    error
	}
	messageAckedMsg struct {
		messageID string
		err       error
	}
	reservationReleasedMsg struct {
		path string
		err  error
	}
)

// cancelJobCmd cancels a job by ID
func cancelJobCmd(jobID string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		cmd := exec.CommandContext(ctx, "agentctl", "jobs", "cancel", jobID)
		output, err := cmd.CombinedOutput()
		if err != nil {
			return jobCancelledMsg{jobID: jobID, err: fmt.Errorf("%w: %s", err, string(output))}
		}
		return jobCancelledMsg{jobID: jobID, err: nil}
	}
}

// completeTaskCmd marks a task as complete
func completeTaskCmd(workspace, taskID string) tea.Cmd {
	return func() tea.Msg {
		workspace = getWorkspace(workspace)
		input, err := json.Marshal(map[string]any{
			"operation":    "complete",
			"workspace_id": workspace,
			"task_id":      taskID,
		})
		if err != nil {
			return taskCompletedMsg{taskID: taskID, err: err}
		}

		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		cmd := exec.CommandContext(ctx, "agentctl", "run", "todo/manage", "--input", string(input))
		output, err := cmd.CombinedOutput()
		if err != nil {
			return taskCompletedMsg{taskID: taskID, err: fmt.Errorf("%w: %s", err, string(output))}
		}
		return taskCompletedMsg{taskID: taskID, err: nil}
	}
}

// setActiveTaskCmd sets a task as the active task
func setActiveTaskCmd(workspace, taskID string) tea.Cmd {
	return func() tea.Msg {
		workspace = getWorkspace(workspace)
		input, err := json.Marshal(map[string]any{
			"operation":    "set_active",
			"workspace_id": workspace,
			"task_id":      taskID,
		})
		if err != nil {
			return taskSetActiveMsg{taskID: taskID, err: err}
		}

		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		cmd := exec.CommandContext(ctx, "agentctl", "run", "todo/manage", "--input", string(input))
		output, err := cmd.CombinedOutput()
		if err != nil {
			return taskSetActiveMsg{taskID: taskID, err: fmt.Errorf("%w: %s", err, string(output))}
		}
		return taskSetActiveMsg{taskID: taskID, err: nil}
	}
}

// ackMessageCmd acknowledges a message
func ackMessageCmd(workspace, actorID, messageID string) tea.Cmd {
	return func() tea.Msg {
		workspace = getWorkspace(workspace)
		input, err := json.Marshal(map[string]any{
			"operation":    "ack",
			"workspace_id": workspace,
			"ack": map[string]any{
				"actor_id":    actorID,
				"message_ids": []string{messageID},
			},
		})
		if err != nil {
			return messageAckedMsg{messageID: messageID, err: err}
		}

		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		cmd := exec.CommandContext(ctx, "agentctl", "run", "mailbox/manage", "--input", string(input))
		output, err := cmd.CombinedOutput()
		if err != nil {
			return messageAckedMsg{messageID: messageID, err: fmt.Errorf("%w: %s", err, string(output))}
		}
		return messageAckedMsg{messageID: messageID, err: nil}
	}
}

// releaseReservationCmd releases a file reservation
func releaseReservationCmd(workspace, holder, path string) tea.Cmd {
	return func() tea.Msg {
		workspace = getWorkspace(workspace)
		input, err := json.Marshal(map[string]any{
			"operation":    "release",
			"workspace_id": workspace,
			"release": map[string]any{
				"holder": holder,
				"path":   path,
			},
		})
		if err != nil {
			return reservationReleasedMsg{path: path, err: err}
		}

		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		cmd := exec.CommandContext(ctx, "agentctl", "run", "mailbox/manage", "--input", string(input))
		output, err := cmd.CombinedOutput()
		if err != nil {
			return reservationReleasedMsg{path: path, err: fmt.Errorf("%w: %s", err, string(output))}
		}
		return reservationReleasedMsg{path: path, err: nil}
	}
}

// fetchSearchResults runs semantic search and returns results.
func fetchSearchResults(query string, limit int, rerank bool, scopes []string) ([]searchResult, *searchStats, error) {
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
		return nil, nil, fmt.Errorf("marshal input: %w", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

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

	// Calculate total latency
	var totalLatency int64
	for _, lat := range envelope.Data.Stats.SourceLatencies {
		totalLatency += int64(lat)
	}

	stats := &searchStats{
		TotalResults:  envelope.Data.Stats.TotalResults,
		SourceCounts:  envelope.Data.Stats.SourceCounts,
		Reranked:      rerank,
		EmbeddingDims: envelope.Data.Stats.EmbeddingDims,
		LatencyMS:     totalLatency,
	}

	return results, stats, nil
}
