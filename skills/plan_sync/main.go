// Package main implements the plan/sync skill for importing Claude Code plans as tasks.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/rs/zerolog"

	"github.com/jkatigb/agentctl/internal/adapters/skillslib/skillerr"
	"github.com/jkatigb/agentctl/internal/adapters/skillslib/skillmain"
	"github.com/jkatigb/agentctl/internal/adapters/skillslib/skillout"
	"github.com/jkatigb/agentctl/internal/adapters/skillslib/workspaceutil"
	"github.com/jkatigb/agentctl/internal/indexing/atomic"
	"github.com/jkatigb/agentctl/internal/platform/timeutil"
	"github.com/jkatigb/agentctl/internal/sessionkit"
	"github.com/jkatigb/agentctl/internal/storage/memory"
	"github.com/jkatigb/agentctl/internal/storage/plans"
	"github.com/jkatigb/agentctl/internal/storage/tasks"
)

// Input defines the skill input parameters for plan synchronization with task import.
type Input struct {
	Workspace     string `json:"workspace"`
	WorkspaceRoot string `json:"workspace_root,omitempty"`
	SessionID     string `json:"session_id,omitempty"`
	PlanFile      string `json:"plan_file,omitempty"`
	ImportTasks   bool   `json:"import_tasks,omitempty"`
	DryRun        bool   `json:"dry_run,omitempty"`
	Force         bool   `json:"force,omitempty"`
	Provider      string `json:"provider,omitempty"`
}

// SyncResult represents the result of syncing a single plan with task creation statistics.
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

// StepResult represents the result of syncing a single step with task status.
type StepResult struct {
	Title   string `json:"title"`
	Section string `json:"section"`
	Status  string `json:"status"` // "created", "exists", "skipped"
	TaskID  string `json:"task_id,omitempty"`
}

// Output defines the skill output with sync statistics and per-plan results.
type Output struct {
	PlansProcessed int          `json:"plans_processed"`
	PlansChanged   int          `json:"plans_changed"`
	TasksCreated   int          `json:"tasks_created"`
	DryRun         bool         `json:"dry_run"`
	Provider       string       `json:"provider"`
	Results        []SyncResult `json:"results"`
	Message        string       `json:"message"`
}

// PlanSyncState tracks the last synced state of a plan for change detection.
type PlanSyncState struct {
	PlanFile    string    `json:"plan_file"`
	ContentHash string    `json:"content_hash"`
	SyncedAt    time.Time `json:"synced_at"`
	TasksLinked []string  `json:"tasks_linked,omitempty"`
}

const command = "plan/sync"

// main is the skill entry point for plan/sync.
func main() {
	skillmain.Main(command, run)
}

// run orchestrates plan synchronization with task import and change detection across providers.
//
// Index:
// - Purpose: Import Claude Code plans as tasks with change detection and multi-provider support
// - Flow: resolve workspace → open stores → load sync states → detect plans → process each plan → import tasks → save states
// - SideEffects: task creation; plan state tracking; atomic fact processing; memory store updates
// - FailureModes: store access failures, plan parsing errors, task creation failures, provider detection errors
// - Observability: emits sync statistics, per-plan results, task creation counts, and provider information
// - Related: processPlan, loadSyncStates, sanitizeFileName, plans.UnifiedDetector, atomic.NewProcessorWithConfig
// - Keywords: plan/sync, plan_import, task_creation, change_detection, claude_code, opencode
func run(ctx context.Context, rc *skillmain.RunContext, input Input) error {
	// Default workspace
	input.Workspace = workspaceutil.Resolve(input.Workspace, input.WorkspaceRoot, rc.Workspace)

	// Open stores - memory uses cache path (matches CLI), tasks uses storage
	memStore, err := rc.Stores.MemoryInCache(ctx)
	if err != nil {
		return skillerr.IO("open memory store", skillerr.WithCause(err),
			skillerr.WithHint("Check AGENTCTL_HOME permissions and disk space"))
	}

	var taskStore tasks.Store
	if input.ImportTasks && !input.DryRun {
		taskStore, err = rc.Stores.Tasks(ctx)
		if err != nil {
			return skillerr.IO("open task store", skillerr.WithCause(err),
				skillerr.WithHint("Check AGENTCTL_HOME permissions and disk space"))
		}
	}

	// Load previous sync states from memory
	syncStates := loadSyncStates(ctx, memStore, input.Workspace)

	// Determine provider (auto-detect if not specified)
	var provider plans.Provider
	if input.Provider != "" {
		provider = plans.Provider(input.Provider)
	} else {
		provider, _ = plans.DetectProvider(input.Workspace)
	}

	// Create unified detector
	unifiedDetector := plans.NewUnifiedDetector(input.Workspace, provider)

	// Claude-specific parser (only used for Claude plans)
	parser := plans.NewParser(plans.ParseOptions{
		MaxSectionDepth: 4,
		IncludeContent:  true,
	})

	var plansToProcess []plans.PlanInfo

	if input.PlanFile != "" {
		// Process specific plan file
		if provider == plans.ProviderOpenCode && strings.HasSuffix(input.PlanFile, ".json") {
			// Parse OpenCode JSON file
			openCodeParser := plans.NewOpenCodeParser("")
			todoFile, parseErr := openCodeParser.ParseFile(input.PlanFile)
			if parseErr != nil {
				return skillerr.Parse("opencode todo file", skillerr.WithCause(parseErr),
					skillerr.WithHint("Ensure the JSON file exists and contains valid OpenCode todos"))
			}
			plansToProcess = append(plansToProcess, *todoFile.ToPlanInfo())
		} else {
			// Parse Claude markdown file
			plan, parseErr := parser.ParseFile(input.PlanFile)
			if parseErr != nil {
				return skillerr.Parse("plan file", skillerr.WithCause(parseErr),
					skillerr.WithHint("Ensure the plan file exists and is valid markdown"))
			}
			plansToProcess = append(plansToProcess, *plan)
		}
	} else {
		// Detect all active plans using unified detector
		detected, detectErr := unifiedDetector.Detect(plans.DetectOptions{
			IncludeArchived: false,
		})
		if detectErr != nil {
			providerHint := "Check ~/.claude/plans directory exists and is accessible"
			if provider == plans.ProviderOpenCode {
				providerHint = "Check .opencode/storage/todo directory exists and is accessible"
			}
			return skillerr.Runtime("detect plans", skillerr.WithCause(detectErr),
				skillerr.WithHint(providerHint))
		}
		plansToProcess = detected
	}

	// Process each plan
	sessionID := sessionkit.ResolveSessionID(input.Workspace, input.SessionID)

	output := Output{
		DryRun:   input.DryRun,
		Provider: string(provider),
		Results:  []SyncResult{},
	}

	for _, plan := range plansToProcess {
		atomicCfg := struct{ APIKey, Endpoint, Model string }{
			rc.Config.LLM.AtomicAPIKey,
			rc.Config.LLM.AtomicEndpoint,
			rc.Config.LLM.AtomicModel,
		}
		result := processPlan(ctx, rc.Logger, &plan, unifiedDetector, syncStates, taskStore, memStore, input, sessionID, atomicCfg)
		output.Results = append(output.Results, result)
		output.PlansProcessed++

		if result.Status == "synced" || result.Status == "created" {
			output.PlansChanged++
		}
		output.TasksCreated += result.TasksCreated
	}

	// Build message
	if input.DryRun {
		output.Message = fmt.Sprintf("Dry run (%s): %d plans processed, %d would change, %d tasks would be created",
			provider, output.PlansProcessed, output.PlansChanged, output.TasksCreated)
	} else {
		output.Message = fmt.Sprintf("Synced %d %s plans, %d changed, %d tasks created",
			output.PlansProcessed, provider, output.PlansChanged, output.TasksCreated)
	}

	return skillout.Emit(rc, command, output)
}

// processPlan handles individual plan processing with step extraction and task creation.
func processPlan(
	ctx context.Context,
	logger zerolog.Logger,
	plan *plans.PlanInfo,
	detector *plans.UnifiedDetector,
	syncStates map[string]PlanSyncState,
	taskStore tasks.Store,
	memStore *memory.Store,
	input Input,
	sessionID string,
	atomicCfg struct{ APIKey, Endpoint, Model string },
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

	// Extract steps from the plan using the appropriate parser
	steps := detector.ExtractSteps(plan)

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
					logger.Warn().
						Err(err).
						Str("plan_file", plan.FilePath).
						Msg("plan_sync: failed to check existing tasks; skipping to avoid duplicates")
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
						SessionID:   sessionID,
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

						// Atomic fact processing (SimpleMem-style) - async, non-blocking
						if atomicCfg.APIKey != "" {
							go func(taskID, title, desc, ws, key, endpoint, model string, ts tasks.Store) {
								processor, procErr := atomic.NewProcessorWithConfig(key, endpoint, model)
								if procErr != nil {
									return
								}
								rawText := title
								if desc != "" {
									rawText = title + ": " + desc
								}
								fact, _, factErr := processor.ProcessSingle(context.Background(), rawText, atomic.ProcessContext{
									Workspace: ws,
								})
								if factErr != nil {
									return
								}
								_ = ts.UpdateAtomic(context.Background(), taskID, fact.Atomic, fact.Entities, fact.Keywords)
							}(created.ID, created.Title, created.Description, input.Workspace, atomicCfg.APIKey, atomicCfg.Endpoint, atomicCfg.Model, taskStore)
						}
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
			logger.Warn().
				Err(err).
				Str("plan_file", plan.FilePath).
				Msg("plan_sync: marshal state failed")
		} else {
			stateName := fmt.Sprintf("plan-sync-%s", sanitizeFileName(plan.FileName))

			_, saveErr := memStore.SaveResult(ctx, memory.SaveOptions{
				Name:      stateName,
				Type:      "plan_sync_state",
				Workspace: input.Workspace,
				Summary:   fmt.Sprintf("Plan sync state: %s", plan.Title),
				Result:    stateJSON,
				SessionID: sessionID,
			})
			if saveErr != nil {
				logger.Warn().
					Err(saveErr).
					Str("plan_file", plan.FilePath).
					Str("state_name", stateName).
					Msg("plan_sync: save state failed")
			}
		}
	}

	return result
}

// loadSyncStates retrieves previous sync states from memory store for change detection.
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

// sanitizeFileName cleans filenames for memory store naming with special character handling.
func sanitizeFileName(name string) string {
	// Remove extension and replace special chars
	name = strings.TrimSuffix(name, filepath.Ext(name))
	name = strings.ReplaceAll(name, " ", "-")
	name = strings.ToLower(name)
	return name
}
