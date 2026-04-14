package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
)

func fetchMailbox(workspace, actorID string, limit int) ([]mailboxMessage, error) {
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
		return nil, err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "foxctl", "run", "mailbox/manage", "--input", input)
	output, err := cmd.CombinedOutput()
	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return nil, fmt.Errorf("command timed out: %w", err)
		}
		return nil, err
	}

	var envelope struct {
		Data struct {
			Messages []mailboxMessage `json:"messages"`
		} `json:"data"`
	}
	if err := json.Unmarshal(output, &envelope); err != nil {
		return nil, err
	}
	return envelope.Data.Messages, nil
}

func runMailboxView(workspace, actorID string, limit int) {
	messages, err := fetchMailbox(workspace, actorID, limit)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error loading mailbox: %v\n", err)
		os.Exit(1)
	}

	titleStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("170"))
	labelStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("240"))

	fmt.Println(titleStyle.Render(fmt.Sprintf("Mailbox: %s", actorID)))
	fmt.Println(labelStyle.Render(fmt.Sprintf("   %d messages", len(messages))))
	fmt.Println()

	if len(messages) == 0 {
		fmt.Println(labelStyle.Render("   (no messages)"))
		return
	}

	for _, msg := range messages {
		statusIcon := "o"
		if msg.Status == "unread" {
			statusIcon = "*"
		}
		priorityStyle := lipgloss.NewStyle()
		if msg.Priority <= 2 {
			priorityStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("196"))
		}

		fmt.Printf("  %s %s [P%d] %s\n",
			statusIcon,
			priorityStyle.Render(fmt.Sprintf("%-12s", msg.Sender)),
			msg.Priority,
			msg.Subject)
		if msg.Body != "" {
			body := truncate(msg.Body, 60)
			fmt.Printf("    %s\n", labelStyle.Render(body))
		}
	}
}

func fetchReservations(workspace string) ([]reservation, error) {
	workspace = getWorkspace(workspace)
	input, err := marshalSkillInput(skillInput{
		Operation:   "list_reservations",
		WorkspaceID: workspace,
	})
	if err != nil {
		return nil, err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "foxctl", "run", "mailbox/manage", "--input", input)
	output, err := cmd.CombinedOutput()
	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return nil, fmt.Errorf("command timed out: %w", err)
		}
		return nil, err
	}

	var envelope struct {
		Data struct {
			Reservations []reservation `json:"reservations"`
		} `json:"data"`
	}
	if err := json.Unmarshal(output, &envelope); err != nil {
		return nil, err
	}
	return envelope.Data.Reservations, nil
}

func runReservationsView(workspace string) {
	reservations, err := fetchReservations(workspace)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error loading reservations: %v\n", err)
		os.Exit(1)
	}

	titleStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("170"))
	labelStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("240"))

	fmt.Println(titleStyle.Render("File Reservations"))
	fmt.Println(labelStyle.Render(fmt.Sprintf("   %d active", len(reservations))))
	fmt.Println()

	if len(reservations) == 0 {
		fmt.Println(labelStyle.Render("   (no active reservations)"))
		return
	}

	for _, res := range reservations {
		modeIcon := "[shared]"
		if res.Mode == "exclusive" {
			modeIcon = "[exclusive]"
		}
		fmt.Printf("  %s %s\n", modeIcon, res.Path)
		fmt.Printf("    %s | %s\n",
			labelStyle.Render("Holder: "+res.Holder),
			labelStyle.Render("Expires: "+res.ExpiresAt))
	}
}

func fetchInsights(workspace string) (*insightsData, error) {
	workspace = getWorkspace(workspace)
	input, err := marshalSkillInput(skillInput{
		Operation:   "graph_insights",
		WorkspaceID: workspace,
	})
	if err != nil {
		return nil, err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "foxctl", "run", "todo/manage", "--input", input)
	output, err := cmd.Output()
	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return nil, fmt.Errorf("command timed out: %w", err)
		}
		return nil, err
	}

	var envelope struct {
		Data insightsData `json:"data"`
	}
	if err := json.Unmarshal(output, &envelope); err != nil {
		return nil, err
	}
	return &envelope.Data, nil
}

func runInsightsView(workspace string) {
	insights, err := fetchInsights(workspace)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error loading insights: %v\n", err)
		os.Exit(1)
	}

	titleStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("170"))
	panelStyle := lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).Padding(0, 1)
	labelStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("240"))

	fmt.Println(titleStyle.Render("Task Insights Dashboard"))
	fmt.Println()

	fmt.Println(panelStyle.Render(fmt.Sprintf(
		"%s\n  Total Nodes: %d\n  Cycles: %d\n  Topo Order: %d tasks",
		titleStyle.Render("Overview"),
		len(insights.Nodes),
		len(insights.Cycles),
		len(insights.TopologicalOrder))))
	fmt.Println()

	if len(insights.Nodes) > 0 {
		var keystones, bottlenecks strings.Builder
		keystones.WriteString(titleStyle.Render("Keystones (High Critical Path)") + "\n")
		bottlenecks.WriteString(titleStyle.Render("Bottlenecks (High PageRank)") + "\n")

		for i, node := range insights.Nodes {
			if i >= 5 {
				break
			}
			if node.CriticalPathScore > 0 {
				keystones.WriteString(fmt.Sprintf("  %s (CP: %d)\n",
					labelStyle.Render(node.TaskID[:min(12, len(node.TaskID))]),
					node.CriticalPathScore))
			}
			if node.PageRank > 0 {
				bottlenecks.WriteString(fmt.Sprintf("  %s (PR: %.3f)\n",
					labelStyle.Render(node.TaskID[:min(12, len(node.TaskID))]),
					node.PageRank))
			}
		}

		fmt.Println(panelStyle.Render(keystones.String()))
		fmt.Println(panelStyle.Render(bottlenecks.String()))
	}

	if len(insights.Cycles) > 0 {
		fmt.Println(panelStyle.Render(fmt.Sprintf(
			"%s\n  %d circular dependencies detected!",
			lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("196")).Render("Cycles"),
			len(insights.Cycles))))
	}
}

type taskSummary struct {
	TaskID string  `json:"task_id"`
	Title  string  `json:"title"`
	Score  float64 `json:"score"`
	Status string  `json:"status,omitempty"`
}

func fetchTasks(workspace string, limit int) ([]taskSummary, error) {
	workspace = getWorkspace(workspace)
	input, err := marshalSkillInput(skillInput{
		Operation:   "recommend",
		WorkspaceID: workspace,
		Limit:       limit,
	})
	if err != nil {
		return nil, err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "foxctl", "run", "todo/manage", "--input", input)
	output, err := cmd.Output()
	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return nil, fmt.Errorf("command timed out: %w", err)
		}
		return nil, err
	}

	var envelope struct {
		Data struct {
			Tasks []taskSummary `json:"tasks"`
		} `json:"data"`
	}
	if err := json.Unmarshal(output, &envelope); err != nil {
		return nil, err
	}
	return envelope.Data.Tasks, nil
}

func runTasksView(workspace string, limit int) {
	tasks, err := fetchTasks(workspace, limit)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error loading tasks: %v\n", err)
		os.Exit(1)
	}

	if len(tasks) == 0 {
		fmt.Println("No tasks found. Add tasks with 'foxctl todo add'!")
		return
	}

	titleStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("170"))
	fmt.Println(titleStyle.Render("Task Recommendations"))
	fmt.Println()

	for i, task := range tasks {
		scoreStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("214"))
		idStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("240"))

		fmt.Printf("%2d. %s  %s\n",
			i+1,
			scoreStyle.Render(fmt.Sprintf("[%.2f]", task.Score)),
			task.Title)
		fmt.Printf("    %s\n", idStyle.Render(task.TaskID))
	}
}

func applyRecipeFilter(recipe, state *string) {
	switch *recipe {
	case "actionable":
		*state = "pending"
	case "blocked":
		*state = "blocked"
	case "recent":
		*state = ""
	case "errors":
		*state = "error"
	}
}

// computeJobStats aggregates statistics from job list
func computeJobStats(jobs []jobSummary) *jobStats {
	stats := &jobStats{
		Total:     len(jobs),
		ByState:   make(map[string]int),
		ByCommand: make(map[string]int),
		Recent:    recentStats{},
	}

	now := time.Now().UTC()
	oneHourAgo := now.Add(-1 * time.Hour)
	oneDayAgo := now.Add(-24 * time.Hour)

	for _, job := range jobs {
		// Count by state
		stats.ByState[job.State]++

		// Count by command (extract skill name)
		cmd := job.Command
		if parts := strings.SplitN(cmd, " ", 2); len(parts) > 0 {
			cmd = parts[0]
		}
		stats.ByCommand[cmd]++

		// Count recent jobs
		if t, err := time.Parse(time.RFC3339, job.CreatedAt); err == nil {
			if t.After(oneHourAgo) {
				stats.Recent.LastHour++
			}
			if t.After(oneDayAgo) {
				stats.Recent.LastDay++
			}
		}
	}

	return stats
}

// fetchBlackboard retrieves blackboard records using foxctl bb list
func fetchBlackboard(ns, topic string, limit int) ([]blackboardRecord, error) {
	if limit <= 0 {
		limit = 50
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Use foxctl bb list command
	args := []string{"bb", "list", "--ns", ns}
	if topic != "" {
		args = append(args, "--topic", topic)
	}
	args = append(args, "--limit", fmt.Sprintf("%d", limit), "--json")

	cmd := exec.CommandContext(ctx, "foxctl", args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return nil, fmt.Errorf("command timed out: %w", err)
		}
		return nil, fmt.Errorf("bb list failed: %w: %s", err, string(output))
	}

	// Parse the JSON envelope response
	var envelope struct {
		Data struct {
			Records []blackboardRecord `json:"records"`
		} `json:"data"`
	}
	if err := json.Unmarshal(output, &envelope); err != nil {
		// Try direct array format as fallback
		var records []blackboardRecord
		if err2 := json.Unmarshal(output, &records); err2 != nil {
			return nil, fmt.Errorf("failed to parse blackboard response: %w", err)
		}
		return records, nil
	}
	return envelope.Data.Records, nil
}
