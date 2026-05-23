// Package main implements the session/restore skill for restoring session state after compaction.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/joshka0/foxctl/internal/adapters/skillslib/executil"
	"github.com/joshka0/foxctl/internal/adapters/skillslib/skillmain"
	"github.com/joshka0/foxctl/internal/adapters/skillslib/skillout"
	"github.com/joshka0/foxctl/internal/adapters/skillslib/workspaceutil"
	"github.com/joshka0/foxctl/internal/context/calibration"
	"github.com/joshka0/foxctl/internal/context/memorycore"
	"github.com/joshka0/foxctl/internal/context/sessionkit"
	"github.com/joshka0/foxctl/internal/context/sessionkit/snapshot"
	"github.com/joshka0/foxctl/internal/intelligence/indexing/semantic"
	"github.com/joshka0/foxctl/internal/platform/config"
	"github.com/joshka0/foxctl/internal/runtime/observability"
	"github.com/joshka0/foxctl/internal/storage/memory"
	"github.com/joshka0/foxctl/internal/storage/sessions"
	"github.com/joshka0/foxctl/internal/storage/tasks"
)

// Input defines the skill input parameters for session restoration with workspace and trigger options.
type Input struct {
	Trigger             string `json:"trigger"`                        // "compact", "resume", "startup"
	Workspace           string `json:"workspace"`                      // Project path
	SessionID           string `json:"session_id,omitempty"`           // Optional: specific session to restore from
	ConversationSummary string `json:"conversation_summary,omitempty"` // Summary from current context window (passed by Claude Code)
	RunSemanticSearch   *bool  `json:"run_semantic_search,omitempty"`  // nil: default true
	MaxSearchResults    int    `json:"max_search_results"`             // Max results per query (default: 3)
	CheckPending        bool   `json:"check_pending,omitempty"`        // If true, only restore if pending_restore_at is set
}

type (
	SessionSnapshot = snapshot.Snapshot
	PlanInfo        = snapshot.PlanInfo
	TaskInfo        = snapshot.TaskInfo
)

// HookOutput is the Claude Code hook output format with decision and context injection.
type HookOutput struct {
	Decision string            `json:"decision"` // "approve", "block", "none"
	Reason   string            `json:"reason,omitempty"`
	Context  string            `json:"context,omitempty"` // Injected context
	Env      map[string]string `json:"env,omitempty"`     // Environment variables
}

// SemanticSearchResult represents results from a key question search with formatted output.
type SemanticSearchResult struct {
	Question string   `json:"question"`
	Results  []string `json:"results"` // Tree-formatted file paths with context
}

// SimilarContextWindow represents a similar past context window found via embedding search.
// Used for searching CURRENT session's context windows with similarity scoring.
type SimilarContextWindow struct {
	WindowID    string    `json:"window_id"`    // UUID for direct lookup
	SessionID   string    `json:"session_id"`   // Parent session
	WindowIndex int       `json:"window_index"` // 0-indexed within session
	Summary     string    `json:"summary"`
	Trigger     string    `json:"trigger"`
	Similarity  float64   `json:"similarity"`
	StartedAt   time.Time `json:"started_at"` // When this window started
}

// SimilarSession represents a similar past session found via embedding search.
// Used for searching OTHER sessions' summaries with accomplishment tracking.
type SimilarSession struct {
	SessionID    string    `json:"session_id"`
	Summary      string    `json:"summary"`
	Accomplished string    `json:"accomplished,omitempty"` // What was completed
	Similarity   float64   `json:"similarity"`
	StartedAt    time.Time `json:"started_at"`
	EndedAt      time.Time `json:"ended_at,omitempty"`
}

// AnchorInfo represents a session anchor (epic/goal) with prompt and learning tracking.
type AnchorInfo struct {
	AnchorID        string   `json:"anchor_id"`
	MainPrompt      string   `json:"main_prompt"`
	CompactionCount int      `json:"compaction_count"`
	Learnings       []string `json:"recent_learnings,omitempty"`
}

// Output defines the skill output with comprehensive restoration data and context injection.
type Output struct {
	HookOutput         HookOutput             `json:"hook_output"`
	SnapshotID         string                 `json:"snapshot_id,omitempty"`
	SnapshotAge        string                 `json:"snapshot_age,omitempty"`
	ItemsRestored      int                    `json:"items_restored"`
	KeyQuestions       []string               `json:"key_questions,omitempty"`
	SearchResults      []SemanticSearchResult `json:"search_results,omitempty"`
	RelevantRecords    []memorycore.Record    `json:"relevant_memory_records,omitempty"`
	Anchor             *AnchorInfo            `json:"anchor,omitempty"`
	CalibrationProfile bool                   `json:"calibration_profile,omitempty"` // Whether calibration profile was injected
}

const command = "session/restore"

// main is the skill entry point for session/restore with context reconstruction capabilities.
func main() {
	config.LoadDotEnv()
	skillmain.Main(command, skillmain.Chain(
		run,
		skillmain.WithRecover[Input](),
	))
}

// run orchestrates session restoration with semantic search, memory retrieval, and context injection.
//
// Index:
//
//	Purpose: Restore session state after compaction with semantic search, memory retrieval, and comprehensive context injection
//	Keywords: session/restore, context_restoration, semantic_search, memory_retrieval, session_continuity
//	Related: runSemanticSearches, searchRelevantMemoryRecords, formatContextWithSearch, searchSimilarSessions
//	Flow: validate input → open stores → find snapshot → search for context → format restoration → emit results
//	Resources: memory store, session store, task store, embedding provider
//	Events: session restore events
//	OutputFields: hook_output, snapshot_id, items_restored, key_questions, search_results, relevant_memory_records
//
// [[domain:session-context-restoration]]
// [[protocol:post-compaction-context-injection]]
//
//nolint:gocyclo // Legacy restoration orchestrator; this slice only changes the memory record contract.
func run(ctx context.Context, rc *skillmain.RunContext, input Input) error {
	start := time.Now()
	// Default workspace
	input.Workspace = workspaceutil.Resolve(input.Workspace, "", rc.Workspace)

	// Default trigger
	if input.Trigger == "" {
		input.Trigger = "compact"
	}

	// Default max search results
	if input.MaxSearchResults <= 0 {
		input.MaxSearchResults = 3
	}

	// Open memory store - use cache path (matches CLI)
	memStore, err := rc.Stores.MemoryInCache(ctx)
	if err != nil {
		// No snapshot available - that's ok, just return empty context
		return emitEmptyOutput(ctx, rc, input, start, "no memory store")
	}

	// Open sessions store for fallback data
	sessStore, err := rc.Stores.Sessions(ctx)
	if err != nil {
		sessStore = nil
	}

	// If check_pending is true, only proceed if there's a pending restore
	if input.CheckPending {
		if sessStore == nil {
			return emitEmptyOutput(ctx, rc, input, start, "no sessions store for pending check")
		}
		pendingSession, err := sessStore.GetPendingRestore(ctx, input.Workspace)
		if err != nil || pendingSession == nil {
			// No pending restore - exit silently
			return emitEmptyOutput(ctx, rc, input, start, "no pending restore")
		}
		// Use the pending session's ID if not provided
		if input.SessionID == "" {
			input.SessionID = pendingSession.ID
		}
	}

	// Open tasks store for pending todos
	taskStore, err := rc.Stores.Tasks(ctx)
	if err != nil {
		taskStore = nil
	}

	// Search for most recent session snapshot
	snapshots, err := memStore.Search(ctx, input.Workspace, "session-snapshot", 5)
	if err != nil || len(snapshots) == 0 {
		return emitEmptyOutput(ctx, rc, input, start, "no snapshots found")
	}

	// Get the most recent one
	latestEntry := snapshots[0].Entry

	// Parse the snapshot
	var snapshot SessionSnapshot
	if err := json.Unmarshal(latestEntry.Result, &snapshot); err != nil {
		return emitEmptyOutput(ctx, rc, input, start, "invalid snapshot format")
	}

	// Use conversation summary from input (current context window) if provided
	// This is the summary Claude Code generates during compact
	if input.ConversationSummary != "" {
		snapshot.Summary = input.ConversationSummary
	}

	// Get session ID for scoping
	sessionID := sessionkit.ResolveSessionID(input.Workspace, input.SessionID)
	if sessionID == "" {
		sessionID = snapshot.SessionID
	}

	// Build search queries from pending todos (current session context)
	var searchQueries []string
	var searchResults []SemanticSearchResult

	if taskStore != nil {
		// Get pending/in_progress tasks for this session
		pendingTasks, taskErr := taskStore.ListWithOptions(ctx, input.Workspace, tasks.ListOptions{
			SessionID: sessionID,
			Statuses:  []string{tasks.StatusPending, tasks.StatusInProgress},
		})
		if taskErr != nil || len(pendingTasks) == 0 {
			// Fallback to workspace-scoped
			pendingTasks, _ = taskStore.ListWithOptions(ctx, input.Workspace, tasks.ListOptions{
				Statuses: []string{tasks.StatusPending, tasks.StatusInProgress},
				Limit:    5,
			})
		}

		// Use task titles as search queries
		for _, t := range pendingTasks {
			if t.Title != "" {
				searchQueries = append(searchQueries, t.Title)
			}
			if len(searchQueries) >= 5 {
				break
			}
		}
	}

	// Run semantic searches for todo-based queries (default: true)
	runSearch := true
	if input.RunSemanticSearch != nil {
		runSearch = *input.RunSemanticSearch
	}
	if runSearch && len(searchQueries) > 0 {
		searchResults = runSemanticSearches(ctx, searchQueries, input.Workspace, input.MaxSearchResults)
	}

	// Get git status for files modified
	filesModified := getGitModifiedFiles(input.Workspace)

	// Search for related context using embedding
	// Use conversation summary if provided, otherwise auto-generate from snapshot
	contextSummary := input.ConversationSummary
	if contextSummary == "" {
		contextSummary = buildContextSummaryFromSnapshot(snapshot)
	}

	// Two-level search:
	// 1. SESSION summaries from OTHER sessions (high-level: "what did we work on before?")
	// 2. CONTEXT WINDOW summaries from CURRENT session (granular: "what was in recent context?")
	var similarSessions []SimilarSession
	var similarWindows []SimilarContextWindow
	if contextSummary != "" && sessStore != nil {
		// Search other sessions' summaries (excluding current session)
		similarSessions = searchSimilarSessions(ctx, sessStore, input.Workspace, contextSummary, sessionID, 3, rc.Config, skillmain.EmbeddingGuard(rc))
		// Search current session's context windows
		similarWindows = searchCurrentSessionWindows(ctx, sessStore, contextSummary, sessionID, 3, rc.Config, skillmain.EmbeddingGuard(rc))
	}

	// Search for relevant canonical memory records based on active task/plan.
	// Falls back to recent named memory records if no semantic search query is available.
	var relevantRecords []memorycore.Record
	contextQuery := buildContextQuery(snapshot)
	relevantRecords = searchRelevantMemoryRecords(ctx, contextQuery, input.Workspace, 5, memStore)

	// Fetch session anchor (epic/goal)
	anchor := fetchAnchor(ctx, input.Workspace, sessionID)

	// Load calibration profile for user preference injection
	var calibProfile *calibration.Profile
	if memStore != nil {
		calibProfile, _ = calibration.LoadProfile(ctx, memStore, input.Workspace)
	}

	// Format context for injection (with todo-based search results, memory records, files modified, and related context)
	contextStr := formatContextWithSearch(snapshot, input.Trigger, searchQueries, searchResults, relevantRecords, filesModified, similarSessions, similarWindows, anchor, calibProfile)
	snapshotAge := formatAge(snapshot.Timestamp)

	// Build output
	output := Output{
		HookOutput: HookOutput{
			Decision: "approve",
			Reason:   fmt.Sprintf("Restored session snapshot from %s ago", snapshotAge),
			Context:  contextStr,
			Env: map[string]string{
				"FOXCTL_SESSION_RESTORED": "true",
				"FOXCTL_SNAPSHOT_ID":      snapshot.SnapshotID,
			},
		},
		SnapshotID:         snapshot.SnapshotID,
		SnapshotAge:        snapshotAge,
		ItemsRestored:      countItems(snapshot),
		KeyQuestions:       searchQueries, // Now based on pending todos
		SearchResults:      searchResults,
		RelevantRecords:    relevantRecords,
		Anchor:             anchor,
		CalibrationProfile: calibProfile != nil,
	}

	// Clear pending restore flag after successful restore
	if sessStore != nil && sessionID != "" {
		_ = sessStore.ClearPendingRestore(ctx, sessionID)
	}

	emitSessionRestoreTelemetry(ctx, rc, input, output, "", time.Since(start))

	return skillout.Emit(rc, command, output)
}

func emitEmptyOutput(ctx context.Context, rc *skillmain.RunContext, input Input, start time.Time, reason string) error {
	output := Output{
		HookOutput: HookOutput{
			Decision: "approve",
			Reason:   reason,
		},
		ItemsRestored: 0,
	}
	emitSessionRestoreTelemetry(ctx, rc, input, output, reason, time.Since(start))
	return skillout.Emit(rc, command, output)
}

func emitSessionRestoreTelemetry(ctx context.Context, rc *skillmain.RunContext, input Input, output Output, emptyReason string, duration time.Duration) {
	sessionID := strings.TrimSpace(input.SessionID)
	agentID := ""
	if rc != nil {
		if sessionID == "" {
			sessionID = rc.SessionID
		}
		agentID = rc.AgentID
	}
	runSemanticSearch := true
	if input.RunSemanticSearch != nil {
		runSemanticSearch = *input.RunSemanticSearch
	}
	builder := observability.NewEvent(observability.OpMemorySessionRestore).
		WithComponent(observability.ComponentSkill).
		WithCommand(command).
		WithWorkspace(input.Workspace).
		WithSession(sessionID, agentID).
		EnrichFromContext(ctx).
		EnrichFromEnv().
		WithData("always_sample", true).
		WithData("trigger", input.Trigger).
		WithData("check_pending", input.CheckPending).
		WithData("run_semantic_search", runSemanticSearch).
		WithData("max_search_results", input.MaxSearchResults).
		WithData("items_restored", output.ItemsRestored).
		WithData("empty", emptyReason != "").
		WithData("empty_reason", emptyReason).
		WithData("snapshot_id_present", output.SnapshotID != "").
		WithData("key_questions", len(output.KeyQuestions)).
		WithData("semantic_result_groups", len(output.SearchResults)).
		WithData("semantic_result_items", semanticResultItemCount(output.SearchResults)).
		WithData("relevant_memory_records", len(output.RelevantRecords)).
		WithData("record_kind_counts", restoreRecordKindCounts(output.RelevantRecords)).
		WithData("record_lifecycle_counts", restoreRecordLifecycleCounts(output.RelevantRecords)).
		WithData("anchor_present", output.Anchor != nil).
		WithData("calibration_profile", output.CalibrationProfile)
	observability.Emit(ctx, builder.Success(duration))
}

func formatAge(t time.Time) string {
	d := time.Since(t)
	if d < time.Minute {
		return "just now"
	}
	if d < time.Hour {
		return fmt.Sprintf("%dm", int(d.Minutes()))
	}
	if d < 24*time.Hour {
		return fmt.Sprintf("%dh", int(d.Hours()))
	}
	return fmt.Sprintf("%dd", int(d.Hours()/24))
}

func countItems(snap SessionSnapshot) int {
	count := 0
	if snap.ActivePlan != nil {
		count++
	}
	if snap.ActiveTask != nil {
		count++
	}
	count += len(snap.PendingTodos)
	count += len(snap.Decisions)
	count += len(snap.Insights)
	return count
}

func semanticResultItemCount(results []SemanticSearchResult) int {
	count := 0
	for _, result := range results {
		count += len(result.Results)
	}
	return count
}

func restoreRecordKindCounts(records []memorycore.Record) map[string]int {
	if len(records) == 0 {
		return nil
	}
	counts := make(map[string]int)
	for _, record := range records {
		counts[string(record.Kind)]++
	}
	return counts
}

func restoreRecordLifecycleCounts(records []memorycore.Record) map[string]int {
	if len(records) == 0 {
		return nil
	}
	counts := make(map[string]int)
	for _, record := range records {
		counts[string(record.Lifecycle.State)]++
	}
	return counts
}

// runSemanticSearches executes semantic search for each key question using foxctl CLI.
func runSemanticSearches(ctx context.Context, keyQuestions []string, workspace string, maxResults int) []SemanticSearchResult {
	if len(keyQuestions) == 0 {
		return nil
	}

	var results []SemanticSearchResult

	for _, question := range keyQuestions {
		// Build input for code/semantic_search skill
		searchInput := map[string]any{
			"query":     question,
			"workspace": workspace,
			"limit":     maxResults,
			"scope":     []string{"symbols", "codemaps"}, // Focus on code structure
			"format":    "tree",
		}

		inputJSON, err := json.Marshal(searchInput)
		if err != nil {
			continue
		}

		// Execute foxctl run code/semantic_search
		searchCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
		var data struct {
			TreeText string `json:"tree_text"`
			Results  []struct {
				Name    string `json:"name"`
				Path    string `json:"path"`
				Line    int    `json:"line"`
				Snippet string `json:"snippet"`
			} `json:"results"`
		}
		_, err = executil.RunFoxctlSkillDecode(searchCtx, workspace, "code/semantic_search", inputJSON, &data)
		cancel()

		if err != nil {
			// Search failed, skip this question
			continue
		}

		// Build result
		semanticResult := SemanticSearchResult{
			Question: question,
		}

		// Prefer tree_text if available, otherwise format results
		if data.TreeText != "" {
			semanticResult.Results = []string{data.TreeText}
		} else if len(data.Results) > 0 {
			// Format results as tree
			for _, r := range data.Results {
				if r.Path != "" {
					line := ""
					if r.Line > 0 {
						line = fmt.Sprintf(":%d", r.Line)
					}
					entry := fmt.Sprintf("├── %s%s", r.Path, line)
					if r.Name != "" {
						entry += fmt.Sprintf(" (%s)", r.Name)
					}
					semanticResult.Results = append(semanticResult.Results, entry)
				}
			}
		}

		if len(semanticResult.Results) > 0 {
			results = append(results, semanticResult)
		}
	}

	return results
}

// buildContextQuery creates a search query from the snapshot to find relevant memory records.
func buildContextQuery(snap SessionSnapshot) string {
	var parts []string
	if snap.ActiveTask != nil && snap.ActiveTask.Title != "" {
		parts = append(parts, snap.ActiveTask.Title)
	}
	if snap.ActivePlan != nil && snap.ActivePlan.Title != "" {
		parts = append(parts, snap.ActivePlan.Title)
	}
	if len(parts) == 0 && snap.Summary != "" {
		summary := snap.Summary
		if len(summary) > 100 {
			summary = summary[:100]
		}
		parts = append(parts, summary)
	}
	if len(parts) == 0 {
		return ""
	}
	return strings.Join(parts, " ")
}

// searchRelevantMemoryRecords searches for relevant canonical memory records via semantic search
// with fallback to direct memory store query.
func searchRelevantMemoryRecords(ctx context.Context, query, workspace string, limit int, memStore *memory.Store) []memorycore.Record {
	var results []memorycore.Record
	seen := make(map[string]bool)

	// Try semantic search first if query is provided
	if query != "" {
		searchInput := map[string]any{
			"query":     query,
			"workspace": workspace,
			"limit":     limit,
			"scope":     []string{"memories"},
		}
		inputJSON, err := json.Marshal(searchInput)
		if err == nil {
			searchCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
			var data struct {
				Results []struct {
					Source  string `json:"source"`
					Name    string `json:"name"`
					Summary string `json:"summary"`
					Type    string `json:"type"`
				} `json:"results"`
			}
			_, err = executil.RunFoxctlSkillDecode(searchCtx, workspace, "code/semantic_search", inputJSON, &data)
			cancel()
			if err == nil {
				for _, r := range data.Results {
					if (r.Source != "memory" && r.Source != "memories") || seen[r.Summary] {
						continue
					}
					record := memoryRecordFromSemanticSearch(r.Name, r.Summary, r.Type)
					if !sessionRestoreMemoryKind(record.Kind) {
						continue
					}
					seen[r.Summary] = true
					results = append(results, record)
				}
			}
		}
	}

	// Fallback: query memory store directly for recent named memory records if semantic search returned nothing.
	if len(results) == 0 && memStore != nil {
		entries, err := memStore.List(ctx, workspace, limit*4)
		if err == nil {
			for _, entry := range entries {
				if seen[entry.Summary] {
					continue
				}
				record := memorycore.RecordFromNamedEntry(entry, memorycore.NamedEntryOptions{})
				if !sessionRestoreMemoryKind(record.Kind) {
					continue
				}
				seen[entry.Summary] = true
				results = append(results, record)
				if len(results) >= limit {
					break
				}
			}
		}
	}

	return results
}

func sessionRestoreMemoryKind(kind memorycore.Kind) bool {
	switch kind {
	case memorycore.KindSemanticFact, memorycore.KindDecision, memorycore.KindProceduralSkill, memorycore.KindPolicyRule:
		return true
	default:
		return false
	}
}

func memoryRecordFromSemanticSearch(name, summary, entryType string) memorycore.Record {
	record := memorycore.Record{
		ID:         strings.TrimSpace(name),
		Kind:       memorycore.KindForNamedType(entryType),
		SourceLane: memorycore.SourceLaneNamedMemory,
		SourceID:   strings.TrimSpace(name),
		Summary:    strings.TrimSpace(summary),
		Temporal: memorycore.TemporalEnvelope{
			TemporalScope: "unknown",
		},
		Provenance: memorycore.Provenance{
			SourceType: "tool_result",
			CreatedBy:  "foxctl.semantic_search",
		},
		Trust: memorycore.TrustEnvelope{
			SourceTrust: "agent_generated",
			Confidence:  0.5,
			Authority:   0.2,
			Tainted:     false,
		},
		Lifecycle: memorycore.LifecycleEnvelope{
			State:        memorycore.LifecycleStateActive,
			Pinned:       false,
			ReviewStatus: memorycore.ReviewStatusUnreviewed,
		},
		Usage: memorycore.UsageEnvelope{
			InstructionEligible: false,
			EvidenceOnly:        true,
			Reason:              "semantic memory search records are evidence unless promoted as validated policy or skill",
		},
	}
	if record.ID == "" {
		record.ID = record.Summary
	}
	if record.SourceID == "" {
		record.SourceID = record.ID
	}
	return record
}

// formatContextWithSearch formats the context including todo-based search results, files modified, and related context.
//
//nolint:gocyclo // Legacy prompt formatter; keep behavioral shape stable during the memory record migration.
func formatContextWithSearch(snap SessionSnapshot, trigger string, todoQueries []string, searchResults []SemanticSearchResult, memoryRecords []memorycore.Record, filesModified []string, similarSessions []SimilarSession, similarWindows []SimilarContextWindow, anchor *AnchorInfo, calibProfile *calibration.Profile) string {
	// Start with the base context, wrapped in clear delimiters
	var sb strings.Builder

	// Opening delimiter - makes it clear this is system-injected context after compaction
	sb.WriteString("<session-restore>\n")
	sb.WriteString("<!-- Auto-injected after context compaction. This is NOT part of the user's message. -->\n\n")

	// User calibration profile (inject first for agent calibration)
	if calibProfile != nil {
		sb.WriteString(calibration.FormatForInjection(calibProfile))
		sb.WriteString("\n\n")
	}

	sb.WriteString("## Session Continuity Context\n\n")
	sb.WriteString(fmt.Sprintf("*Restored after %s (snapshot from %s ago)*\n\n", trigger, formatAge(snap.Timestamp)))

	// Session Anchor (Epic/Goal) - shown first for prominence
	if anchor != nil && anchor.MainPrompt != "" {
		sb.WriteString("### 🎯 Session Anchor (Epic Goal)\n")
		sb.WriteString("**This is the high-level goal for this session. Continue working toward it.**\n\n")
		// Truncate very long anchors but keep reasonable amount
		prompt := anchor.MainPrompt
		if len(prompt) > 2000 {
			prompt = prompt[:2000] + "\n...(truncated)"
		}
		sb.WriteString("```\n")
		sb.WriteString(prompt)
		sb.WriteString("\n```\n")
		if anchor.CompactionCount > 0 {
			sb.WriteString(fmt.Sprintf("*Compaction #%d*\n", anchor.CompactionCount))
		}
		if len(anchor.Learnings) > 0 {
			sb.WriteString("\n**Recent learnings:**\n")
			for _, l := range anchor.Learnings {
				sb.WriteString(fmt.Sprintf("- %s\n", l))
			}
		}
		sb.WriteString("\n")
	}

	// Session Summary (current context window)
	if snap.Summary != "" {
		sb.WriteString("### Context Window Summary\n")
		sb.WriteString(snap.Summary)
		sb.WriteString("\n\n")
	}

	// Files modified (from git status)
	if len(filesModified) > 0 {
		sb.WriteString("### Files Modified\n")
		sb.WriteString("```\n")
		for _, f := range filesModified {
			sb.WriteString(f)
			sb.WriteString("\n")
		}
		sb.WriteString("```\n\n")
	}

	// Related past sessions (from OTHER sessions - high level)
	if len(similarSessions) > 0 {
		sb.WriteString("### Related Past Sessions\n")
		sb.WriteString("*Similar work from previous sessions:*\n\n")
		for _, s := range similarSessions {
			age := formatAge(s.StartedAt)
			shortSession := s.SessionID
			if len(shortSession) > 8 {
				shortSession = shortSession[:8]
			}
			// Show accomplished if available, otherwise summary
			content := s.Accomplished
			if content == "" {
				content = s.Summary
			}
			sb.WriteString(fmt.Sprintf("- **[%.0f%% match]** *%s* | `session:%s` | %s\n", s.Similarity*100, age, shortSession, skillout.TruncateSingleLine(content, 150)))
		}
		sb.WriteString("\n")
	}

	// Related context windows from CURRENT session (granular)
	if len(similarWindows) > 0 {
		sb.WriteString("### Earlier Context (this session)\n")
		sb.WriteString("*Relevant context windows from earlier in this session:*\n\n")
		for _, w := range similarWindows {
			age := formatAge(w.StartedAt)
			ref := fmt.Sprintf("`window #%d`", w.WindowIndex)
			sb.WriteString(fmt.Sprintf("- **[%.0f%% match]** *%s* | %s | %s\n", w.Similarity*100, age, ref, skillout.TruncateSingleLine(w.Summary, 150)))
		}
		sb.WriteString("\n")
	}

	// Add query hints if we have any related context
	if len(similarSessions) > 0 || len(similarWindows) > 0 {
		sb.WriteString("*To explore further:*\n")
		sb.WriteString("```bash\n")
		sb.WriteString("# Semantic search across sessions:\n")
		sb.WriteString("foxctl run session/recall --input '{\"query\": \"<topic>\"}'\n")
		if len(similarSessions) > 0 {
			sb.WriteString("\n# View specific session:\n")
			for _, s := range similarSessions {
				sb.WriteString(fmt.Sprintf("foxctl sessions get %s  # %s\n", s.SessionID, skillout.TruncateSingleLine(s.Summary, 40)))
				break // Just show first one as example
			}
		}
		sb.WriteString("```\n\n")
	}

	// Todo-based Search Results (codebase context related to pending work)
	if len(searchResults) > 0 {
		sb.WriteString("### Codebase Context (from pending todos)\n\n")
		for _, sr := range searchResults {
			sb.WriteString(fmt.Sprintf("**%s**\n", sr.Question))
			sb.WriteString("```\n")
			for _, r := range sr.Results {
				sb.WriteString(r)
				sb.WriteString("\n")
			}
			sb.WriteString("```\n\n")
		}
	} else if len(todoQueries) > 0 {
		// Show todos even if search failed
		sb.WriteString("### Pending Todos (for context)\n")
		for _, q := range todoQueries {
			sb.WriteString(fmt.Sprintf("- %s\n", q))
		}
		sb.WriteString("\n")
	}

	// Relevant canonical memory records from semantic search or named memory fallback.
	if len(memoryRecords) > 0 {
		sb.WriteString("### Relevant Memory Records\n")
		sb.WriteString("*Evidence only unless explicitly marked as active policy or validated skill.*\n")
		for _, record := range memoryRecords {
			sb.WriteString(fmt.Sprintf("- **[%s]** %s\n", record.Kind, record.Summary))
		}
		sb.WriteString("\n")
	}

	// Active plan (from ~/.claude/plans/)
	if snap.ActivePlan != nil {
		sb.WriteString("### Active Plan\n")
		sb.WriteString(fmt.Sprintf("**%s** (`%s`)\n", snap.ActivePlan.Title, snap.ActivePlan.FileName))
		if len(snap.ActivePlan.Sections) > 0 {
			sb.WriteString("Sections:\n")
			for _, sec := range snap.ActivePlan.Sections {
				sb.WriteString(fmt.Sprintf("  - %s\n", sec))
			}
		}
		if snap.ActivePlan.LinkedTasks > 0 {
			sb.WriteString(fmt.Sprintf("*%d tasks linked to this plan*\n", snap.ActivePlan.LinkedTasks))
		}
		sb.WriteString("\n")
	}

	// Active task
	if snap.ActiveTask != nil {
		sb.WriteString("### Active Task\n")
		sb.WriteString(fmt.Sprintf("**%s** (ID: %s)\n", snap.ActiveTask.Title, snap.ActiveTask.ID))
		if snap.ActiveTask.Description != "" {
			sb.WriteString(fmt.Sprintf("%s\n", snap.ActiveTask.Description))
		}
		sb.WriteString("\n")
	}

	// Pending todos
	if len(snap.PendingTodos) > 0 {
		sb.WriteString("### Pending Work\n")
		for _, todo := range snap.PendingTodos {
			status := "⏳"
			if todo.Status == "in_progress" {
				status = "🔄"
			}
			sb.WriteString(fmt.Sprintf("- %s %s\n", status, todo.Title))
		}
		sb.WriteString("\n")
	}

	// Collect and display gotchas from all tasks (deduplicated)
	seenGotchas := make(map[string]bool)
	var gotchas []string
	if snap.ActiveTask != nil && snap.ActiveTask.Gotchas != "" {
		entry := fmt.Sprintf("**%s**: %s", snap.ActiveTask.Title, snap.ActiveTask.Gotchas)
		gotchas = append(gotchas, entry)
		seenGotchas[snap.ActiveTask.ID] = true
	}
	for _, todo := range snap.PendingTodos {
		if todo.Gotchas != "" && !seenGotchas[todo.ID] {
			gotchas = append(gotchas, fmt.Sprintf("**%s**: %s", todo.Title, todo.Gotchas))
		}
	}
	if len(gotchas) > 0 {
		sb.WriteString("### Gotchas & Learnings\n")
		for _, g := range gotchas {
			sb.WriteString(fmt.Sprintf("- %s\n", g))
		}
		sb.WriteString("\n")
	}

	// Key decisions
	if len(snap.Decisions) > 0 {
		sb.WriteString("### Key Decisions Made\n")
		for _, d := range snap.Decisions {
			sb.WriteString(fmt.Sprintf("- %s\n", d))
		}
		sb.WriteString("\n")
	}

	// Insights
	if len(snap.Insights) > 0 {
		sb.WriteString("### Insights\n")
		for _, i := range snap.Insights {
			sb.WriteString(fmt.Sprintf("- %s\n", i))
		}
		sb.WriteString("\n")
	}

	// Note: Summary is shown at the top as "Context Window Summary"

	// Append foxctl skills reference
	sb.WriteString(getSkillsReference())

	sb.WriteString("---\n")
	sb.WriteString("*Continue where you left off.*\n\n")

	// Closing delimiter
	sb.WriteString("</session-restore>\n")

	return sb.String()
}

// getSkillsReference returns the foxctl skills quick reference.
func getSkillsReference() string {
	return `### foxctl Skills Reference

Run: ` + "`foxctl run <skill> --input '<json>'`" + ` | Help: ` + "`foxctl run <skill> --help`" + `

**Files & Search**
| Skill | Purpose |
|-------|---------|
| ` + "`code/context_ripgrep`" + ` | Search + expand to full functions |
| ` + "`code/snippet_extract`" + ` | Smart code retrieval |
| ` + "`code/symbols`" + ` | Extract functions/types/vars |
| ` + "`code/semantic_search`" + ` | Vector search (symbols/codemaps/memories) |

**Code Intelligence**
| Skill | Purpose |
|-------|---------|
| ` + "`code/complexity`" + ` | Cyclomatic complexity, hotspots |
| ` + "`code/smart_write`" + ` | Symbol-based editing with diff preview |
| ` + "`lsp/gopls`" + ` | Go: definitions, references, hover |

**Testing & CI**
| Skill | Purpose |
|-------|---------|
| ` + "`test/run`" + ` | Run tests with coverage |
| ` + "`ci/checks`" + ` | CI status, failed checks |
| ` + "`ci/prcomments`" + ` | PR review comments |

**Tasks & Memory**
| Skill | Purpose |
|-------|---------|
| ` + "`todo/manage`" + ` | Create/list/complete tasks |
| ` + "`memory/query`" + ` | Query canonical memory records |
| ` + "`session/recall`" + ` | Search past sessions |

**CLI Shortcuts**
` + "```bash" + `
foxctl todo list|add|complete    # Task management
foxctl ci status --pr 123        # CI + comments + merge
foxctl memory list|get|put       # Named memories
foxctl search "pattern"          # Quick ripgrep
` + "```" + `

`
}

// getGitModifiedFiles returns files modified in the workspace from git status.
func getGitModifiedFiles(workspace string) []string {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	result := executil.Run(ctx, workspace, "git", "status", "--porcelain")
	if result.Err != nil {
		return nil
	}

	var files []string
	for _, line := range strings.Split(string(result.Stdout), "\n") {
		if line == "" {
			continue
		}
		// Git status --porcelain format: "XY filename" where XY is 2 chars + 1 space
		// Examples: " M file.go", "?? file.go", "M  file.go"
		if len(line) >= 4 {
			status := line[:2]
			filename := line[3:] // Skip "XY " (2 status chars + 1 space)
			files = append(files, fmt.Sprintf("%s %s", status, filename))
		}
	}

	// Limit to 20 files
	if len(files) > 20 {
		files = files[:20]
		files = append(files, fmt.Sprintf("... and %d more files", len(files)-20))
	}

	return files
}

// buildContextSummaryFromSnapshot creates a search query from snapshot data.
// Used when no conversation_summary is provided in the input.
func buildContextSummaryFromSnapshot(snap SessionSnapshot) string {
	var parts []string

	// Add active task title and description
	if snap.ActiveTask != nil {
		if snap.ActiveTask.Title != "" {
			parts = append(parts, snap.ActiveTask.Title)
		}
		if snap.ActiveTask.Description != "" {
			parts = append(parts, snap.ActiveTask.Description)
		}
	}

	// Add active plan title
	if snap.ActivePlan != nil && snap.ActivePlan.Title != "" {
		parts = append(parts, snap.ActivePlan.Title)
	}

	// Add pending todo titles (up to 5)
	for i, todo := range snap.PendingTodos {
		if i >= 5 {
			break
		}
		if todo.Title != "" {
			parts = append(parts, todo.Title)
		}
	}

	// Add existing snapshot summary if nothing else
	if len(parts) == 0 && snap.Summary != "" {
		summary := snap.Summary
		if len(summary) > 200 {
			summary = summary[:200]
		}
		parts = append(parts, summary)
	}

	if len(parts) == 0 {
		return ""
	}

	return strings.Join(parts, ". ")
}

// searchSimilarSessions searches OTHER sessions' summaries (excluding current session).
// Returns high-level matches: "what similar work have we done before?"
func searchSimilarSessions(ctx context.Context, sessStore *sessions.Store, workspace, summary, currentSessionID string, limit int, cfg config.Config, embedOpts ...semantic.EmbedderOption) []SimilarSession {
	if summary == "" || sessStore == nil {
		return nil
	}

	// Generate embedding for the summary
	embedding, err := embedText(ctx, summary, cfg, embedOpts...)
	if err != nil || len(embedding) == 0 {
		return nil
	}

	// Search sessions using the store's built-in method
	// Request more than limit to account for filtering out current session
	scoredSessions, err := sessStore.SearchSimilar(ctx, workspace, embedding, limit+5)
	if err != nil || len(scoredSessions) == 0 {
		return nil
	}

	// Convert to output format, excluding current session
	var results []SimilarSession
	for _, ss := range scoredSessions {
		// Skip current session
		if ss.Session.ID == currentSessionID {
			continue
		}
		// Skip low similarity
		if ss.Similarity < 0.3 {
			continue
		}
		// Skip sessions without summary
		if ss.Session.Summary == "" && len(ss.Session.Accomplished) == 0 {
			continue
		}

		// Join accomplished items into a single string
		accomplished := strings.Join(ss.Session.Accomplished, "; ")

		results = append(results, SimilarSession{
			SessionID:    ss.Session.ID,
			Summary:      ss.Session.Summary,
			Accomplished: accomplished,
			Similarity:   ss.Similarity,
			StartedAt:    ss.Session.StartedAt,
			EndedAt:      ss.Session.EndedAt,
		})

		if len(results) >= limit {
			break
		}
	}

	return results
}

// searchCurrentSessionWindows searches context windows from the CURRENT session only.
// Returns granular matches: "what similar context was in previous windows of this session?"
func searchCurrentSessionWindows(ctx context.Context, sessStore *sessions.Store, summary, currentSessionID string, limit int, cfg config.Config, embedOpts ...semantic.EmbedderOption) []SimilarContextWindow {
	if summary == "" || sessStore == nil || currentSessionID == "" {
		return nil
	}

	// Generate embedding for the summary
	embedding, err := embedText(ctx, summary, cfg, embedOpts...)
	if err != nil || len(embedding) == 0 {
		return nil
	}

	// Search context windows using the sessions store's built-in method
	// Request more to account for filtering
	scoredWindows, err := sessStore.SearchContextWindows(ctx, embedding, limit*3)
	if err != nil || len(scoredWindows) == 0 {
		return nil
	}

	// Filter to only current session's windows
	var results []SimilarContextWindow
	for _, sw := range scoredWindows {
		// Only include windows from current session
		if sw.Window.SessionID != currentSessionID {
			continue
		}
		// Skip low similarity
		if sw.Similarity < 0.3 {
			continue
		}
		// Only include windows with summaries
		if sw.Window.Summary == "" {
			continue
		}

		results = append(results, SimilarContextWindow{
			WindowID:    sw.Window.ID,
			SessionID:   sw.Window.SessionID,
			WindowIndex: sw.Window.WindowIndex,
			Summary:     sw.Window.Summary,
			Trigger:     sw.Window.Trigger,
			Similarity:  sw.Similarity,
			StartedAt:   sw.Window.StartedAt,
		})

		if len(results) >= limit {
			break
		}
	}

	return results
}

// embedText generates an embedding for the given text using the Embedder.
func embedText(ctx context.Context, text string, cfg config.Config, embedOpts ...semantic.EmbedderOption) ([]float32, error) {
	if strings.TrimSpace(text) == "" {
		return nil, nil
	}

	embedder, err := semantic.NewEmbedderFromConfig(semantic.ScopeSessions, cfg, embedOpts...)
	if err != nil {
		// No provider available - not an error, just skip embedding
		return nil, nil
	}

	result, err := embedder.Embed(ctx, text)
	if err != nil {
		return nil, err
	}
	return result.Vec, nil
}

// fetchAnchor retrieves the session anchor for the workspace.
func fetchAnchor(ctx context.Context, workspace, sessionID string) *AnchorInfo {
	// Build input for session/anchor skill
	anchorInput := map[string]any{
		"operation":  "get",
		"workspace":  workspace,
		"session_id": sessionID,
	}

	inputJSON, err := json.Marshal(anchorInput)
	if err != nil {
		return nil
	}

	// Execute foxctl run session/anchor
	fetchCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	var data struct {
		Found  bool `json:"found"`
		Anchor struct {
			AnchorID        string   `json:"anchor_id"`
			MainPrompt      string   `json:"main_prompt"`
			CompactionCount int      `json:"compaction_count"`
			RecentLearnings []string `json:"recent_learnings"`
		} `json:"anchor"`
	}

	_, err = executil.RunFoxctlSkillDecode(fetchCtx, workspace, "session/anchor", inputJSON, &data)
	if err != nil || !data.Found {
		return nil
	}

	return &AnchorInfo{
		AnchorID:        data.Anchor.AnchorID,
		MainPrompt:      data.Anchor.MainPrompt,
		CompactionCount: data.Anchor.CompactionCount,
		Learnings:       data.Anchor.RecentLearnings,
	}
}

// truncateSummary truncates a summary to the specified length.
