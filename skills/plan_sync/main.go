// Package main implements the plan/sync skill for importing Claude Code plans as tasks.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/jkatigb/agentctl/internal/domain/envelope"
	errs "github.com/jkatigb/agentctl/internal/platform/errors"
	"github.com/jkatigb/agentctl/internal/platform/timeutil"
	"github.com/jkatigb/agentctl/internal/storage/memory"
	"github.com/jkatigb/agentctl/internal/storage/plans"
	"github.com/jkatigb/agentctl/internal/storage/tasks"
)

// Input defines the skill input parameters.
type Input struct {
	Workspace   string `json:"workspace"`              // Project path
	PlanFile    string `json:"plan_file,omitempty"`    // Specific plan file to sync (optional)
	ImportTasks bool   `json:"import_tasks,omitempty"` // Whether to create tasks from plan steps
	DryRun      bool   `json:"dry_run,omitempty"`      // Preview changes without applying
	Force       bool   `json:"force,omitempty"`        // Re-sync even if hash unchanged
}

// SyncResult represents the result of syncing a single plan.
type SyncResult struct {
	PlanFile     string       `json:"plan_file"`
	PlanTitle    string       `json:"plan_title"`
	ContentHash  string       `json:"content_hash"`
	Status       string       `json:"status"` // "synced", "unchanged", "created", "error"
	TasksCreated int          `json:"tasks_created,omitempty"`
	TasksSkipped int          `json:"tasks_skipped,omitempty"`
	Steps        []StepResult `json:"steps,omitempty"`
	Error        string       `json:"error,omitempty"`
}

// StepResult represents the result of syncing a single step.
type StepResult struct {
	Title   string `json:"title"`
	Section string `json:"section"`
	Status  string `json:"status"` // "created", "exists", "skipped"
	TaskID  string `json:"task_id,omitempty"`
}

// Output defines the skill output.
type Output struct {
	PlansProcessed int          `json:"plans_processed"`
	PlansChanged   int          `json:"plans_changed"`
	TasksCreated   int          `json:"tasks_created"`
	DryRun         bool         `json:"dry_run"`
	Results        []SyncResult `json:"results"`
	Message        string       `json:"message"`
}

// PlanSyncState tracks the last synced state of a plan.
type PlanSyncState struct {
	PlanFile    string    `json:"plan_file"`
	ContentHash string    `json:"content_hash"`
	SyncedAt    time.Time `json:"synced_at"`
	TasksLinked []string  `json:"tasks_linked,omitempty"`
}

const command = "plan/sync"

func main() {
	ctx := context.Background()

	// Read input from stdin
	var input Input
	if err := json.NewDecoder(os.Stdin).Decode(&input); err != nil {
		fail("EPARSE", fmt.Errorf("decode input: %w", err), "Ensure valid JSON on stdin")
	}

	// Default workspace to current directory
	if input.Workspace == "" {
		wd, err := os.Getwd()
		if err != nil {
			fmt.Fprintf(os.Stderr, "plan_sync: warning: failed to get working directory: %v, using '.' as fallback\n", err)
			input.Workspace = "."
		} else {
			input.Workspace = wd
		}
	}

	// Get agentctl home
	home := os.Getenv("AGENTCTL_HOME")
	if home == "" {
		homeDir, _ := os.UserHomeDir()
		home = filepath.Join(homeDir, ".agentctl")
	}

	// Open stores - memory uses cache path (matches CLI), tasks uses storage
	cachePath := filepath.Join(home, "cache")
	storageRoot := filepath.Join(home, "storage")
	casPath := filepath.Join(home, "cas")

	memStore, err := memory.Open(ctx, cachePath, casPath)
	if err != nil {
		fail("EIO", fmt.Errorf("open memory store: %w", err), "Check AGENTCTL_HOME permissions and disk space")
	}
	defer func() { errs.Ignore(memStore.Close(), "close memory store") }()

	var taskStore tasks.Store
	if input.ImportTasks && !input.DryRun {
		taskStore, err = tasks.Open(ctx, storageRoot)
		if err != nil {
			fail("EIO", fmt.Errorf("open task store: %w", err), "Check AGENTCTL_HOME permissions and disk space")
		}
		defer func() { errs.Ignore(taskStore.Close(), "close task store") }()
	}

	// Load previous sync states from memory
	syncStates := loadSyncStates(ctx, memStore, input.Workspace)

	// Get plans to process
	homeDir, _ := os.UserHomeDir()
	plansDir := filepath.Join(homeDir, ".claude", "plans")
	detector := plans.NewDetector(plansDir)
	parser := plans.NewParser(plans.ParseOptions{
		MaxSectionDepth: 4,
		IncludeContent:  true,
	})

	var plansToProcess []plans.PlanInfo

	if input.PlanFile != "" {
		// Process specific plan
		plan, err := parser.ParseFile(input.PlanFile)
		if err != nil {
			fail("EPARSE", fmt.Errorf("parse plan file: %w", err), "Ensure the plan file exists and is valid markdown")
		}
		plansToProcess = append(plansToProcess, *plan)
	} else {
		// Detect all active plans
		detected, err := detector.Detect(plans.DetectOptions{
			IncludeArchived: false,
		})
		if err != nil {
			fail("ERUNTIME", fmt.Errorf("detect plans: %w", err), "Check ~/.claude/plans directory exists and is accessible")
		}
		plansToProcess = detected
	}

	// Process each plan
	output := Output{
		DryRun:  input.DryRun,
		Results: []SyncResult{},
	}

	for _, plan := range plansToProcess {
		result := processPlan(ctx, &plan, parser, syncStates, taskStore, memStore, input)
		output.Results = append(output.Results, result)
		output.PlansProcessed++

		if result.Status == "synced" || result.Status == "created" {
			output.PlansChanged++
		}
		output.TasksCreated += result.TasksCreated
	}

	// Build message
	if input.DryRun {
		output.Message = fmt.Sprintf("Dry run: %d plans processed, %d would change, %d tasks would be created",
			output.PlansProcessed, output.PlansChanged, output.TasksCreated)
	} else {
		output.Message = fmt.Sprintf("Synced %d plans, %d changed, %d tasks created",
			output.PlansProcessed, output.PlansChanged, output.TasksCreated)
	}

	env := envelope.OK(command, output)
	errs.Ignore(envelope.Write(os.Stdout, env), "emit plan/sync result")
}

func processPlan(
	ctx context.Context,
	plan *plans.PlanInfo,
	parser *plans.Parser,
	syncStates map[string]PlanSyncState,
	taskStore tasks.Store,
	memStore *memory.Store,
	input Input,
) SyncResult {
	result := SyncResult{
		PlanFile:    plan.FilePath,
		PlanTitle:   plan.Title,
		ContentHash: plan.ContentHash,
	}

	// Check if plan has changed
	prevState, exists := syncStates[plan.FilePath]
	if exists && prevState.ContentHash == plan.ContentHash && !input.Force {
		result.Status = "unchanged"
		return result
	}

	if exists {
		result.Status = "synced"
	} else {
		result.Status = "created"
	}

	// Extract steps from the plan
	steps := parser.ExtractSteps(plan)

	// Import tasks if requested
	if input.ImportTasks {
		for _, step := range steps {
			sectionPath := strings.Join(step.SectionPath, " > ")
			stepResult := StepResult{
				Title:   step.Title,
				Section: sectionPath,
			}

			if input.DryRun {
				stepResult.Status = "would_create"
				result.TasksCreated++
			} else if taskStore != nil {
				// Check if task already exists for this step
				existingTasks, err := taskStore.ListByPlanFile(ctx, plan.FilePath)
				if err != nil {
					// Log the error and skip task creation for this step to avoid duplicates
					fmt.Fprintf(os.Stderr, "plan_sync: warning: failed to check existing tasks for %s: %v, skipping to avoid duplicates\n", plan.FilePath, err)
					stepResult.Status = "skipped"
					result.TasksSkipped++
					result.Steps = append(result.Steps, stepResult)
					continue
				}
				var existingTask *tasks.Task
				for _, t := range existingTasks {
					if t.PlanSection == sectionPath {
						existingTask = &t
						break
					}
				}

				if existingTask != nil {
					stepResult.Status = "exists"
					stepResult.TaskID = existingTask.ID
					result.TasksSkipped++
				} else {
					// Create new task
					task := tasks.Task{
						WorkspaceID: input.Workspace,
						Title:       step.Title,
						Description: step.Description,
						Status:      tasks.StatusPending,
						PlanFile:    plan.FilePath,
						PlanSection: sectionPath,
					}

					// Set dependencies if available
					if len(step.DependsOn) > 0 {
						task.DependsOn = step.DependsOn
					}

					created, err := taskStore.Add(ctx, task)
					if err != nil {
						stepResult.Status = "error"
					} else {
						stepResult.Status = "created"
						stepResult.TaskID = created.ID
						result.TasksCreated++
					}
				}
			}

			result.Steps = append(result.Steps, stepResult)
		}
	}

	// Save sync state (unless dry run)
	if !input.DryRun && memStore != nil {
		newState := PlanSyncState{
			PlanFile:    plan.FilePath,
			ContentHash: plan.ContentHash,
			SyncedAt:    timeutil.NowUTC(),
		}

		stateJSON, err := json.Marshal(newState)
		if err != nil {
			fmt.Fprintf(os.Stderr, "plan_sync: marshal state: %v\n", err)
		} else {
			stateName := fmt.Sprintf("plan-sync-%s", sanitizeFileName(plan.FileName))

			_, saveErr := memStore.SaveResult(ctx, memory.SaveOptions{
				Name:      stateName,
				Type:      "plan_sync_state",
				Workspace: input.Workspace,
				Summary:   fmt.Sprintf("Plan sync state: %s", plan.Title),
				Result:    stateJSON,
			})
			if saveErr != nil {
				fmt.Fprintf(os.Stderr, "plan_sync: save state for %s: %v\n", plan.FileName, saveErr)
			}
		}
	}

	return result
}

func loadSyncStates(ctx context.Context, memStore *memory.Store, workspace string) map[string]PlanSyncState {
	states := make(map[string]PlanSyncState)

	// Search for plan-sync entries by name prefix
	items, err := memStore.Search(ctx, workspace, "plan-sync-", 100)
	if err != nil {
		return states
	}

	for _, item := range items {
		// Only process plan_sync_state type entries
		if item.Entry.Type != "plan_sync_state" {
			continue
		}

		var state PlanSyncState
		if err := json.Unmarshal(item.Entry.Result, &state); err != nil {
			continue
		}
		states[state.PlanFile] = state
	}

	return states
}

func sanitizeFileName(name string) string {
	// Remove extension and replace special chars
	name = strings.TrimSuffix(name, filepath.Ext(name))
	name = strings.ReplaceAll(name, " ", "-")
	name = strings.ToLower(name)
	return name
}

func fail(code string, err error, hint string) {
	data := map[string]any{
		"hint": hint,
	}
	env := envelope.Error(command, code, err.Error(), data)
	errs.Ignore(envelope.Write(os.Stdout, env), "emit plan/sync failure")
	os.Exit(1)
}
