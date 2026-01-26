// Package main implements the session/extract_learnings skill for deep extraction of actionable knowledge.
// This runs in parallel with session/summarize during PreCompact to capture learnings separately.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/jkatigb/agentctl/internal/adapters/skillslib/executil"
	"github.com/jkatigb/agentctl/internal/adapters/skillslib/hashutil"
	"github.com/jkatigb/agentctl/internal/adapters/skillslib/obs"
	"github.com/jkatigb/agentctl/internal/adapters/skillslib/skillerr"
	"github.com/jkatigb/agentctl/internal/adapters/skillslib/skillmain"
	"github.com/jkatigb/agentctl/internal/adapters/skillslib/skillout"
	"github.com/jkatigb/agentctl/internal/adapters/skillslib/workspaceutil"
	"github.com/jkatigb/agentctl/internal/indexing/atomic"
	"github.com/jkatigb/agentctl/internal/indexing/semantic"
	llmproviders "github.com/jkatigb/agentctl/internal/providers/llm"
	"github.com/jkatigb/agentctl/internal/sessionkit"
	"github.com/jkatigb/agentctl/internal/storage/memory"
	"github.com/jkatigb/agentctl/internal/storage/sessions"
)

const commandName = "session/extract_learnings"

// logger is the package-level observability logger.
var logger *obs.Logger

// Input defines the skill input parameters.
type Input struct {
	SessionID string `json:"session_id"`
	Force     bool   `json:"force,omitempty"`      // Re-extract even if already done
	MaxTokens int    `json:"max_tokens,omitempty"` // Limit for transcript
	EpicID    string `json:"epic_id,omitempty"`    // Link learnings to epic
	DryRun    bool   `json:"dry_run,omitempty"`    // Skip LLM and persistence

	// Mode selects extraction scope:
	// - "session" (default): extract from full session transcript
	// - "windows": extract from each context window individually
	Mode string `json:"mode,omitempty"`

	// BatchSize limits windows processed per batch (mode=windows). Default: 5.
	BatchSize int `json:"batch_size,omitempty"`

	// ProcessAll loops until all windows are done (mode=windows).
	ProcessAll bool `json:"process_all,omitempty"`
}

// Output defines the skill output.
type Output struct {
	SessionID       string        `json:"session_id"`
	EpicID          string        `json:"epic_id,omitempty"`
	Gotchas         []Gotcha      `json:"gotchas"`
	Decisions       []Decision    `json:"decisions"`
	UserPreferences []Preference  `json:"user_preferences"`
	AntiPatterns    []AntiPattern `json:"anti_patterns"`
	Learnings       []Learning    `json:"learnings"`
	PersistedCount  int           `json:"persisted_count"`
	Provider        string        `json:"provider,omitempty"`
	Status          string        `json:"status"`
	Message         string        `json:"message,omitempty"`

	// Windows mode stats
	WindowsProcessed int `json:"windows_processed,omitempty"`
	WindowsSkipped   int `json:"windows_skipped,omitempty"`
	WindowsRemaining int `json:"windows_remaining,omitempty"`
}

// Gotcha represents a non-obvious issue encountered.
type Gotcha struct {
	Summary  string   `json:"summary"`  // Brief description
	Context  string   `json:"context"`  // What triggered it
	Fix      string   `json:"fix"`      // How it was resolved
	Tags     []string `json:"tags"`     // Categorization tags
	Severity string   `json:"severity"` // low, medium, high
	Files    []string `json:"files"`    // Related file paths
}

// Decision represents a technical choice made.
type Decision struct {
	Summary      string   `json:"summary"`      // What was decided
	Reasoning    string   `json:"reasoning"`    // Why this choice
	Alternatives []string `json:"alternatives"` // What else was considered
	Tags         []string `json:"tags"`
	Files        []string `json:"files"` // Related file paths
}

// Preference represents a user preference or constraint.
type Preference struct {
	Summary  string   `json:"summary"`  // The preference
	Context  string   `json:"context"`  // When it applies
	Strength string   `json:"strength"` // strong, moderate, weak
	Tags     []string `json:"tags"`
	Files    []string `json:"files"` // Related file paths
}

// AntiPattern represents something that didn't work.
type AntiPattern struct {
	Summary     string   `json:"summary"`     // What didn't work
	WhyBad      string   `json:"why_bad"`     // Why it failed
	Alternative string   `json:"alternative"` // What to do instead
	Tags        []string `json:"tags"`
	Files       []string `json:"files"` // Related file paths
}

// Learning represents a reusable pattern or solution.
type Learning struct {
	Summary  string   `json:"summary"`  // The learning
	Example  string   `json:"example"`  // Concrete example
	Reusable bool     `json:"reusable"` // Applicable elsewhere
	Tags     []string `json:"tags"`
	Files    []string `json:"files"` // Related file paths
}

// LLMResponse is the expected JSON structure from the LLM.
type LLMResponse struct {
	Gotchas         []Gotcha      `json:"gotchas"`
	Decisions       []Decision    `json:"decisions"`
	UserPreferences []Preference  `json:"user_preferences"`
	AntiPatterns    []AntiPattern `json:"anti_patterns"`
	Learnings       []Learning    `json:"learnings"`
}

// LLMProvider represents a configured LLM provider.
type LLMProvider = llmproviders.Provider

const (
	defaultMaxTokens = 8000

	learningsWindowContentMin     = 1200
	learningsWindowContentMax     = 6000
	learningsWindowContentReserve = 1200
	learningsWindowHeadChunks     = 2
	learningsWindowTailChunks     = 2
)

func main() {
	skillmain.Main(commandName, run)
}

func run(ctx context.Context, rc *skillmain.RunContext, in Input) error {
	// Initialize package logger
	logger = obs.NewLogger(obs.WithLogCommand(commandName))

	ctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()

	if in.SessionID == "" {
		return skillerr.Arg("session_id is required")
	}

	if in.MaxTokens <= 0 {
		in.MaxTokens = defaultMaxTokens
	}

	// Parse mode
	mode := strings.ToLower(strings.TrimSpace(in.Mode))
	if mode == "" {
		mode = "session"
	}
	if mode != "session" && mode != "windows" {
		return skillerr.Arg(fmt.Sprintf("invalid mode %q (expected session or windows)", in.Mode))
	}

	// Open sessions store
	sessionStore, cleanup, err := sessionkit.OpenSessions(ctx, rc.Config)
	if err != nil {
		return skillerr.IO("open sessions store", skillerr.WithCause(err))
	}
	defer cleanup()

	// Get session
	session, err := sessionStore.Get(ctx, in.SessionID)
	if err != nil {
		return skillerr.Arg("session not found", skillerr.WithCause(err))
	}

	if in.DryRun {
		output := Output{
			SessionID: session.ID,
			EpicID:    in.EpicID,
			Status:    "dry_run",
			Message:   "dry run: skipped LLM extraction and persistence",
		}
		return skillout.Emit(rc, commandName, output)
	}

	providers := llmproviders.ExtractionProviders()
	if len(providers) == 0 {
		return skillerr.Arg("no LLM provider configured")
	}

	// Handle windows mode
	if mode == "windows" {
		output := extractLearningsWindows(ctx, rc, sessionStore, session, providers, in)
		return skillout.Emit(rc, commandName, output)
	}

	// Session mode: extract from full transcript
	transcriptPath := session.RawJSONLPath
	if transcriptPath == "" {
		return skillerr.Arg("session has no transcript (RawJSONLPath is empty)")
	}
	resolvedTranscriptPath, err := skillmain.ValidatePath(rc, transcriptPath, skillmain.WithPathMessage("session transcript path invalid"))
	if err != nil {
		return err
	}

	// Extract learnings with LLM
	llmResp, provider, err := extractWithFallback(ctx, providers, resolvedTranscriptPath, in.MaxTokens)
	if err != nil {
		return skillerr.Runtimef("extraction failed: %v", err)
	}

	// Persist learnings to memory store
	persistedCount, err := persistLearnings(ctx, rc, session.ID, session.WorkspacePath, in.EpicID, llmResp)
	if err != nil {
		logger.Warn("failed to persist some learnings", obs.Err(err))
	}

	output := Output{
		SessionID:       in.SessionID,
		EpicID:          in.EpicID,
		Gotchas:         llmResp.Gotchas,
		Decisions:       llmResp.Decisions,
		UserPreferences: llmResp.UserPreferences,
		AntiPatterns:    llmResp.AntiPatterns,
		Learnings:       llmResp.Learnings,
		PersistedCount:  persistedCount,
		Provider:        provider,
		Status:          "ok",
	}

	return skillout.Emit(rc, commandName, output)
}

func extractWithFallback(ctx context.Context, providers []LLMProvider, transcriptPath string, maxTokens int) (*LLMResponse, string, error) {
	var lastErr error
	for _, p := range providers {
		tokens := p.MaxTokens
		if maxTokens > 0 && maxTokens < tokens {
			tokens = maxTokens
		}

		// Read and filter transcript
		transcript, err := readTranscript(transcriptPath, tokens)
		if err != nil {
			lastErr = err
			continue
		}

		var resp *LLMResponse
		if p.IsCLI {
			resp, err = extractWithCLI(ctx, p, transcript)
		} else {
			resp, err = extractWithAPI(ctx, p, transcript)
		}

		if err == nil {
			return resp, p.Name, nil
		}
		lastErr = err
	}
	return nil, "", lastErr
}

// extractFromContent extracts learnings from transcript content directly (not from file).
func extractFromContent(ctx context.Context, providers []LLMProvider, content string, maxTokens int) (*LLMResponse, string, error) {
	// Simple truncation by estimated tokens (4 chars per token)
	maxChars := maxTokens * 4
	if len(content) > maxChars {
		content = content[len(content)-maxChars:] // Keep most recent
	}

	var lastErr error
	for _, p := range providers {
		var resp *LLMResponse
		var err error
		if p.IsCLI {
			resp, err = extractWithCLI(ctx, p, content)
		} else {
			resp, err = extractWithAPI(ctx, p, content)
		}

		if err == nil {
			return resp, p.Name, nil
		}
		lastErr = err
	}
	return nil, "", lastErr
}

func readTranscript(path string, maxTokens int) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}

	// Simple truncation by estimated tokens (4 chars per token)
	maxChars := maxTokens * 4
	if len(data) > maxChars {
		data = data[len(data)-maxChars:] // Keep most recent
	}

	return string(data), nil
}

func extractWithAPI(ctx context.Context, provider LLMProvider, transcript string) (*LLMResponse, error) {
	prompt := buildExtractionPrompt(transcript)

	reqBody := map[string]any{
		"model":      provider.Model,
		"max_tokens": 4096,
		"messages": []map[string]string{
			{"role": "user", "content": prompt},
		},
	}

	body, err := json.Marshal(reqBody)
	if err != nil {
		return nil, err
	}

	ctx, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, "POST", provider.Endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+provider.APIKey)

	if strings.HasPrefix(provider.Name, "openrouter") {
		req.Header.Set("HTTP-Referer", "https://github.com/jkatigb/agentctl")
	}

	client := &http.Client{Timeout: 90 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("API error %d: %s", resp.StatusCode, string(respBody))
	}

	var result struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}

	if len(result.Choices) == 0 {
		return nil, fmt.Errorf("empty response")
	}

	return parseResponse(result.Choices[0].Message.Content)
}

func extractWithCLI(ctx context.Context, provider LLMProvider, transcript string) (*LLMResponse, error) {
	prompt := buildExtractionPrompt(transcript)

	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()

	result := executil.Run(ctx, "", "gemini", "-m", provider.Model, "-p", prompt)
	if !result.Success() {
		return nil, fmt.Errorf("gemini failed: %w", result.Err)
	}

	return parseResponse(result.String())
}

func buildExtractionPrompt(transcript string) string {
	return fmt.Sprintf(`You are analyzing a coding session to extract ACTIONABLE LEARNINGS for future sessions.
Focus on knowledge that will help in similar situations. Be specific and detailed.

Return JSON with these categories:

{
  "gotchas": [
    {
      "summary": "Brief description of the non-obvious issue",
      "context": "What was being done when this happened",
      "fix": "How it was resolved",
      "tags": ["relevant", "tags"],
      "severity": "low|medium|high",
      "files": ["path/to/relevant/file.go"]
    }
  ],
  "decisions": [
    {
      "summary": "What technical decision was made",
      "reasoning": "WHY this choice was made (not just what)",
      "alternatives": ["other options considered"],
      "tags": ["relevant", "tags"],
      "files": ["path/to/relevant/file.go"]
    }
  ],
  "user_preferences": [
    {
      "summary": "What the user prefers or requires",
      "context": "When this preference applies",
      "strength": "strong|moderate|weak",
      "tags": ["relevant", "tags"],
      "files": ["path/to/relevant/file.go"]
    }
  ],
  "anti_patterns": [
    {
      "summary": "What approach didn't work",
      "why_bad": "Why it failed or caused problems",
      "alternative": "What to do instead",
      "tags": ["relevant", "tags"],
      "files": ["path/to/relevant/file.go"]
    }
  ],
  "learnings": [
    {
      "summary": "A reusable pattern or insight",
      "example": "Concrete example from the session",
      "reusable": true,
      "tags": ["relevant", "tags"],
      "files": ["path/to/relevant/file.go"]
    }
  ]
}

Guidelines:
- GOTCHAS: Non-obvious problems. Include the fix! Future sessions need to know what worked.
- DECISIONS: Focus on WHY, not just WHAT. Include alternatives considered.
- USER_PREFERENCES: Explicit statements from the user about how they want things done.
- ANTI_PATTERNS: Things that wasted time or didn't work. What to avoid.
- LEARNINGS: Reusable solutions or patterns that could help elsewhere.
- FILES: ALWAYS include relevant file paths. This enables file-scoped memory recall when editing those files later.
- The transcript may include a [Window Metadata] section (tools/files/errors). Use those identifiers explicitly.

Be specific. "Fixed the bug" is useless. "agentctl-mode-enforce.sh was missing from settings.json PreToolUse matcher" is useful.

Session transcript:
%s

Return ONLY valid JSON, no markdown fences.`, transcript)
}

func parseResponse(content string) (*LLMResponse, error) {
	// Clean up response
	content = strings.TrimSpace(content)
	content = strings.TrimPrefix(content, "```json")
	content = strings.TrimPrefix(content, "```")
	content = strings.TrimSuffix(content, "```")
	content = strings.TrimSpace(content)

	var resp LLMResponse
	if err := json.Unmarshal([]byte(content), &resp); err != nil {
		return nil, fmt.Errorf("parse error (response length: %d): %w", len(content), err)
	}

	return &resp, nil
}

func persistLearnings(ctx context.Context, rc *skillmain.RunContext, sessionID, workspace, epicID string, resp *LLMResponse) (int, error) {
	if workspace == "" {
		return 0, nil
	}

	store, cleanup, err := sessionkit.OpenMemory(ctx, rc.Config)
	if err != nil {
		return 0, err
	}

	// WaitGroup for async atomic processing goroutines
	var atomicWg sync.WaitGroup
	defer func() {
		atomicWg.Wait() // Wait for all atomic goroutines before cleanup
		cleanup()
	}()

	embedder, err := semantic.NewEmbedderFromConfig(semantic.ScopeMemory, rc.Config)
	if err != nil {
		embedder = nil
	}

	count := 0
	atomicCfg := struct{ APIKey, Endpoint, Model string }{
		rc.Config.LLM.AtomicAPIKey,
		rc.Config.LLM.AtomicEndpoint,
		rc.Config.LLM.AtomicModel,
	}

	// Persist gotchas
	for _, g := range resp.Gotchas {
		if err := persistOne(ctx, store, embedder, sessionID, epicID, workspace, "gotcha", g.Summary, atomicCfg, &atomicWg, map[string]any{
			"context":  g.Context,
			"fix":      g.Fix,
			"tags":     g.Tags,
			"severity": g.Severity,
			"files":    g.Files,
		}); err == nil {
			count++
		}
	}

	// Persist decisions
	for _, d := range resp.Decisions {
		if err := persistOne(ctx, store, embedder, sessionID, epicID, workspace, "decision", d.Summary, atomicCfg, &atomicWg, map[string]any{
			"reasoning":    d.Reasoning,
			"alternatives": d.Alternatives,
			"tags":         d.Tags,
			"files":        d.Files,
		}); err == nil {
			count++
		}
	}

	// Persist user preferences
	for _, p := range resp.UserPreferences {
		if err := persistOne(ctx, store, embedder, sessionID, epicID, workspace, "user_pref", p.Summary, atomicCfg, &atomicWg, map[string]any{
			"context":  p.Context,
			"strength": p.Strength,
			"tags":     p.Tags,
			"files":    p.Files,
		}); err == nil {
			count++
		}
	}

	// Persist anti-patterns
	for _, a := range resp.AntiPatterns {
		if err := persistOne(ctx, store, embedder, sessionID, epicID, workspace, "anti_pattern", a.Summary, atomicCfg, &atomicWg, map[string]any{
			"why_bad":     a.WhyBad,
			"alternative": a.Alternative,
			"tags":        a.Tags,
			"files":       a.Files,
		}); err == nil {
			count++
		}
	}

	// Persist learnings
	for _, l := range resp.Learnings {
		if err := persistOne(ctx, store, embedder, sessionID, epicID, workspace, "learning", l.Summary, atomicCfg, &atomicWg, map[string]any{
			"example":  l.Example,
			"reusable": l.Reusable,
			"tags":     l.Tags,
			"files":    l.Files,
		}); err == nil {
			count++
		}
	}

	return count, nil
}

func persistOne(ctx context.Context, store *memory.Store, embedder *semantic.Embedder, sessionID, epicID, workspace, typ, summary string, atomicCfg struct{ APIKey, Endpoint, Model string }, atomicWg *sync.WaitGroup, extra map[string]any) error {
	if summary == "" {
		return nil
	}

	digest := hashutil.ShortHash(summary)
	name := fmt.Sprintf("learning:%s:%s:%s", sessionID, typ, digest)
	if epicID != "" {
		name = fmt.Sprintf("%s:epic:%s", name, epicID)
	}

	// Idempotency check
	if existing, err := store.GetEmbedding(ctx, name, workspace); err == nil && len(existing) > 0 {
		return nil // Already exists
	}

	// Format date for semantic search (e.g., "Jan 14, 2026")
	dateStr := time.Now().Format("Jan 2, 2006")
	datedSummary := fmt.Sprintf("[%s] %s", dateStr, summary)

	payload := map[string]any{
		"session_id":   sessionID,
		"type":         typ,
		"summary":      summary,
		"extracted_at": dateStr,
	}
	if epicID != "" {
		payload["epic_id"] = epicID
	}
	for k, v := range extra {
		payload[k] = v
	}

	payloadBytes, _ := json.Marshal(payload)

	// Store with dated summary for better semantic search
	_, err := store.SaveResult(ctx, memory.SaveOptions{
		Name:      name,
		Type:      typ,
		Workspace: workspace,
		Summary:   datedSummary,
		Result:    payloadBytes,
		SessionID: sessionID,
	})
	if err != nil {
		return err
	}

	// Generate embedding with dated summary
	if embedder != nil {
		if embedding, err := embedder.Embed(ctx, datedSummary); err == nil && len(embedding.Vec) > 0 {
			_ = store.UpdateEmbedding(ctx, name, workspace, embedding.Vec)
		}
	}

	// Atomic fact processing (SimpleMem-style) - async with WaitGroup
	if atomicCfg.APIKey != "" && atomicWg != nil {
		atomicWg.Add(1)
		go func(entryName, entryWorkspace, entrySummary, key, endpoint, model string) {
			defer atomicWg.Done()
			processor, procErr := atomic.NewProcessorWithConfig(key, endpoint, model)
			if procErr != nil {
				return // Silently skip if processor unavailable
			}
			fact, _, factErr := processor.ProcessSingle(ctx, entrySummary, atomic.ProcessContext{
				Workspace: entryWorkspace,
			})
			if factErr != nil {
				return // Silently skip on processing error
			}
			_ = store.UpdateAtomic(ctx, entryName, entryWorkspace, fact.Atomic, fact.Entities, fact.Keywords)
		}(name, workspace, datedSummary, atomicCfg.APIKey, atomicCfg.Endpoint, atomicCfg.Model)
	}

	return nil
}

// extractLearningsWindows processes learnings extraction window by window.
func extractLearningsWindows(ctx context.Context, rc *skillmain.RunContext, sessionStore *sessions.Store, session sessions.Session, providers []LLMProvider, in Input) Output {
	output := Output{
		SessionID: session.ID,
		EpicID:    in.EpicID,
		Status:    "windows_extracted",
	}

	// Default batch size: 5 windows per batch
	batchSize := in.BatchSize
	if batchSize <= 0 {
		batchSize = 5
	}

	// Open memory store for persistence
	memStore, memCleanup, err := sessionkit.OpenMemory(ctx, rc.Config)
	if err != nil {
		output.Status = "error"
		output.Message = fmt.Sprintf("failed to open memory store: %v", err)
		return output
	}
	// Track async atomic processing goroutines to wait before cleanup
	var atomicWg sync.WaitGroup
	defer func() {
		atomicWg.Wait() // Wait for all atomic goroutines before closing store
		memCleanup()
	}()

	// Workspace detection
	workspace := workspaceutil.Resolve(session.WorkspacePath, "", rc.Workspace)

	embedder, err := semantic.NewEmbedderFromConfig(semantic.ScopeMemory, rc.Config)
	if err != nil {
		embedder = nil
	}

	// Aggregated results
	var allGotchas []Gotcha
	var allDecisions []Decision
	var allPrefs []Preference
	var allAnti []AntiPattern
	var allLearnings []Learning
	totalPersisted := 0
	totalProcessed := 0
	totalSkipped := 0
	batchCount := 0

	for {
		// Get fresh window list each iteration
		windows, err := sessionStore.GetContextWindows(ctx, session.ID)
		if err != nil {
			output.Status = "error"
			output.Message = fmt.Sprintf("failed to get windows: %v", err)
			return output
		}

		if len(windows) == 0 {
			if batchCount == 0 {
				output.Status = "no_windows"
				output.Message = "no context windows found for session"
			}
			break
		}

		// Filter windows that need extraction
		var needsExtraction []sessions.ContextWindow
		alreadyDone := 0
		for _, window := range windows {
			// Check if we already extracted learnings for this window
			markerName := fmt.Sprintf("learning:%s:window:%d:marker", session.ID, window.WindowIndex)
			if !in.Force {
				if _, err := memStore.Get(ctx, markerName, workspace); err == nil {
					alreadyDone++
					continue
				}
			}
			needsExtraction = append(needsExtraction, window)
		}

		totalSkipped += alreadyDone

		if len(needsExtraction) == 0 {
			// All done
			output.WindowsRemaining = 0
			break
		}

		// Apply batch limit
		batch := needsExtraction
		if len(batch) > batchSize {
			batch = batch[:batchSize]
		}

		// Process this batch
		atomicCfg := struct{ APIKey, Endpoint, Model string }{
			rc.Config.LLM.AtomicAPIKey,
			rc.Config.LLM.AtomicEndpoint,
			rc.Config.LLM.AtomicModel,
		}
		processed, persisted, gotchas, decisions, prefs, anti, learnings := processWindowBatch(
			ctx, sessionStore, memStore, embedder, session.ID, in.EpicID, workspace, atomicCfg, &atomicWg, batch, providers, in.MaxTokens,
		)

		totalProcessed += processed
		totalPersisted += persisted
		allGotchas = append(allGotchas, gotchas...)
		allDecisions = append(allDecisions, decisions...)
		allPrefs = append(allPrefs, prefs...)
		allAnti = append(allAnti, anti...)
		allLearnings = append(allLearnings, learnings...)
		batchCount++

		output.WindowsRemaining = len(needsExtraction) - len(batch)

		// If not ProcessAll, stop after one batch
		if !in.ProcessAll {
			break
		}

		// Check context deadline
		if ctx.Err() != nil {
			output.Message = fmt.Sprintf("stopped after %d batches due to context deadline", batchCount)
			break
		}
	}

	output.Gotchas = allGotchas
	output.Decisions = allDecisions
	output.UserPreferences = allPrefs
	output.AntiPatterns = allAnti
	output.Learnings = allLearnings
	output.PersistedCount = totalPersisted
	output.WindowsProcessed = totalProcessed
	output.WindowsSkipped = totalSkipped

	if output.Message == "" {
		output.Message = fmt.Sprintf("extracted learnings from %d windows (%d skipped, %d remaining)", totalProcessed, totalSkipped, output.WindowsRemaining)
	}

	return output
}

// processWindowBatch processes a batch of windows for learning extraction.
func processWindowBatch(
	ctx context.Context,
	sessionStore *sessions.Store,
	memStore *memory.Store,
	embedder *semantic.Embedder,
	sessionID, epicID, workspace string,
	atomicCfg struct{ APIKey, Endpoint, Model string },
	atomicWg *sync.WaitGroup,
	batch []sessions.ContextWindow,
	providers []LLMProvider,
	maxTokens int,
) (processed, persisted int, gotchas []Gotcha, decisions []Decision, prefs []Preference, anti []AntiPattern, learnings []Learning) {
	contentBudget := learningsContentBudget(maxTokens)
	for _, window := range batch {
		// Build content from chunks
		var contentParts []string
		var candidates []learningsChunkCandidate
		toolsSeen := make(map[string]struct{})
		filesSeen := make(map[string]struct{})
		var errorSnippets []string

		for chunkIdx := window.ChunkStart; chunkIdx <= window.ChunkEnd; chunkIdx++ {
			chunk, err := sessionStore.GetChunk(ctx, sessionID, chunkIdx)
			if err != nil {
				continue
			}
			for _, tool := range chunk.ToolsUsed {
				if tool != "" {
					toolsSeen[tool] = struct{}{}
				}
			}
			for _, file := range chunk.FilesTouched {
				if file != "" {
					filesSeen[file] = struct{}{}
				}
			}
			isError := chunk.HasError || chunk.ChunkType == "error"
			if isError {
				snippet := skillout.TruncateSingleLine(strings.TrimSpace(chunk.ContentPreview), 200)
				if snippet != "" {
					errorSnippets = append(errorSnippets, snippet)
				}
			}
			preview := strings.TrimSpace(chunk.ContentPreview)
			if preview == "" {
				continue
			}
			chunkTokens := len(preview) / 4
			if chunkTokens == 0 {
				chunkTokens = 1
			}
			candidates = append(candidates, learningsChunkCandidate{
				Chunk:    chunk,
				Preview:  preview,
				Tokens:   chunkTokens,
				HasTools: len(chunk.ToolsUsed) > 0,
				HasFiles: len(chunk.FilesTouched) > 0,
				IsError:  isError,
			})
		}

		selected := selectLearningsChunks(candidates, contentBudget)
		for _, candidate := range selected {
			contentParts = append(contentParts, fmt.Sprintf("[%s #%d] %s", candidate.Chunk.ChunkType, candidate.Chunk.ChunkIndex, candidate.Preview))
		}

		if len(contentParts) == 0 {
			continue
		}

		// Build transcript content for this window
		windowContent := strings.Join(contentParts, "\n")
		meta := formatWindowMetadata(window, sortedSet(toolsSeen, 8), sortedSet(filesSeen, 12), uniqueLimited(errorSnippets, 3))
		if meta != "" {
			windowContent = windowContent + "\n\n[Window Metadata]\n" + meta
		}

		// Extract learnings via LLM
		resp, _, err := extractFromContent(ctx, providers, windowContent, maxTokens)
		if err != nil {
			logger.Warn("LLM failed for window", obs.Int("window_index", window.WindowIndex), obs.Err(err))
			continue
		}

		processed++

		// Persist learnings from this window
		windowPersisted := persistWindowLearnings(ctx, memStore, embedder, sessionID, epicID, workspace, atomicCfg, atomicWg, window, resp)
		persisted += windowPersisted

		// Collect results
		gotchas = append(gotchas, resp.Gotchas...)
		decisions = append(decisions, resp.Decisions...)
		prefs = append(prefs, resp.UserPreferences...)
		anti = append(anti, resp.AntiPatterns...)
		learnings = append(learnings, resp.Learnings...)
	}

	return
}

type learningsChunkCandidate struct {
	Chunk    sessions.SessionChunk
	Preview  string
	Tokens   int
	HasTools bool
	HasFiles bool
	IsError  bool
}

func learningsContentBudget(maxTokens int) int {
	budget := maxTokens
	if budget <= 0 {
		budget = defaultMaxTokens
	}
	budget -= learningsWindowContentReserve
	if budget < learningsWindowContentMin {
		budget = learningsWindowContentMin
	}
	if budget > learningsWindowContentMax {
		budget = learningsWindowContentMax
	}
	return budget
}

func selectLearningsChunks(candidates []learningsChunkCandidate, maxTokens int) []learningsChunkCandidate {
	if len(candidates) == 0 || maxTokens <= 0 {
		return nil
	}
	selected := make(map[int]learningsChunkCandidate, len(candidates))
	tokensUsed := 0
	add := func(candidate learningsChunkCandidate) bool {
		if candidate.Tokens <= 0 || candidate.Preview == "" {
			return true
		}
		index := candidate.Chunk.ChunkIndex
		if _, ok := selected[index]; ok {
			return true
		}
		if tokensUsed+candidate.Tokens > maxTokens {
			return false
		}
		selected[index] = candidate
		tokensUsed += candidate.Tokens
		return true
	}

	for _, candidate := range candidates {
		if candidate.IsError {
			if !add(candidate) {
				break
			}
		}
	}

	for _, candidate := range candidates {
		if candidate.IsError || (!candidate.HasFiles && !candidate.HasTools) {
			continue
		}
		if !add(candidate) {
			break
		}
	}

	head := learningsWindowHeadChunks
	if head > len(candidates) {
		head = len(candidates)
	}
	for i := 0; i < head; i++ {
		if !add(candidates[i]) {
			break
		}
	}

	tail := learningsWindowTailChunks
	if tail > len(candidates) {
		tail = len(candidates)
	}
	for i := len(candidates) - tail; i < len(candidates); i++ {
		if i < 0 {
			continue
		}
		if !add(candidates[i]) {
			break
		}
	}

	i := 0
	j := len(candidates) - 1
	for tokensUsed < maxTokens && i <= j {
		if !add(candidates[i]) {
			break
		}
		i++
		if i > j {
			break
		}
		if !add(candidates[j]) {
			break
		}
		j--
	}

	out := make([]learningsChunkCandidate, 0, len(selected))
	for _, candidate := range candidates {
		if _, ok := selected[candidate.Chunk.ChunkIndex]; ok {
			out = append(out, candidate)
		}
	}
	return out
}

func formatWindowMetadata(window sessions.ContextWindow, tools, files, errors []string) string {
	var lines []string
	if window.Trigger != "" {
		lines = append(lines, fmt.Sprintf("Trigger: %s", window.Trigger))
	}
	if len(tools) > 0 {
		lines = append(lines, fmt.Sprintf("Tools: %s", strings.Join(tools, ", ")))
	}
	if len(files) > 0 {
		lines = append(lines, fmt.Sprintf("Files: %s", strings.Join(files, ", ")))
	}
	if len(errors) > 0 {
		lines = append(lines, fmt.Sprintf("Errors: %s", strings.Join(errors, " | ")))
	}
	if len(lines) == 0 {
		return ""
	}
	return strings.Join(lines, "\n")
}

func sortedSet(values map[string]struct{}, limit int) []string {
	if len(values) == 0 {
		return nil
	}
	out := make([]string, 0, len(values))
	for value := range values {
		out = append(out, value)
	}
	sort.Strings(out)
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out
}

func uniqueLimited(values []string, limit int) []string {
	seen := make(map[string]struct{})
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
		if limit > 0 && len(out) >= limit {
			break
		}
	}
	return out
}

// persistWindowLearnings saves learnings from a single window to memory store.
func persistWindowLearnings(
	ctx context.Context,
	store *memory.Store,
	embedder *semantic.Embedder,
	sessionID, epicID, workspace string,
	atomicCfg struct{ APIKey, Endpoint, Model string },
	atomicWg *sync.WaitGroup,
	window sessions.ContextWindow,
	resp *LLMResponse,
) int {
	count := 0

	// Format date for semantic search
	dateStr := time.Now().Format("Jan 2, 2006")

	// Helper to persist one learning with window context
	persistWithWindow := func(typ, summary string, extra map[string]any) {
		if summary == "" {
			return
		}
		digest := hashutil.ShortHash(summary)
		name := fmt.Sprintf("learning:%s:window:%d:%s:%s", sessionID, window.WindowIndex, typ, digest)
		if epicID != "" {
			name = fmt.Sprintf("%s:epic:%s", name, epicID)
		}

		// Idempotency check
		if existing, err := store.GetEmbedding(ctx, name, workspace); err == nil && len(existing) > 0 {
			return
		}

		// Create dated summary for semantic search
		datedSummary := fmt.Sprintf("[%s] %s", dateStr, summary)

		payload := map[string]any{
			"session_id":   sessionID,
			"window_index": window.WindowIndex,
			"chunk_start":  window.ChunkStart,
			"chunk_end":    window.ChunkEnd,
			"type":         typ,
			"summary":      summary,
			"extracted_at": dateStr,
		}
		if epicID != "" {
			payload["epic_id"] = epicID
		}
		for k, v := range extra {
			payload[k] = v
		}

		payloadBytes, _ := json.Marshal(payload)

		_, err := store.SaveResult(ctx, memory.SaveOptions{
			Name:      name,
			Type:      typ,
			Workspace: workspace,
			Summary:   datedSummary,
			Result:    payloadBytes,
			SessionID: sessionID,
		})
		if err != nil {
			return
		}
		count++

		// Generate embedding with dated summary
		if embedder != nil {
			if embedding, err := embedder.Embed(ctx, datedSummary); err == nil && len(embedding.Vec) > 0 {
				_ = store.UpdateEmbedding(ctx, name, workspace, embedding.Vec)
			}
		}

		// Atomic fact processing (SimpleMem-style) - async with WaitGroup
		if atomicCfg.APIKey != "" && atomicWg != nil {
			atomicWg.Add(1)
			go func(entryName, entryWorkspace, entrySummary, key, endpoint, model string) {
				defer atomicWg.Done()
				processor, procErr := atomic.NewProcessorWithConfig(key, endpoint, model)
				if procErr != nil {
					return
				}
				fact, _, factErr := processor.ProcessSingle(ctx, entrySummary, atomic.ProcessContext{
					Workspace: entryWorkspace,
				})
				if factErr != nil {
					return
				}
				_ = store.UpdateAtomic(ctx, entryName, entryWorkspace, fact.Atomic, fact.Entities, fact.Keywords)
			}(name, workspace, datedSummary, atomicCfg.APIKey, atomicCfg.Endpoint, atomicCfg.Model)
		}
	}

	// Persist all types
	for _, g := range resp.Gotchas {
		persistWithWindow("gotcha", g.Summary, map[string]any{"context": g.Context, "fix": g.Fix, "tags": g.Tags, "severity": g.Severity, "files": g.Files})
	}
	for _, d := range resp.Decisions {
		persistWithWindow("decision", d.Summary, map[string]any{"reasoning": d.Reasoning, "alternatives": d.Alternatives, "tags": d.Tags, "files": d.Files})
	}
	for _, p := range resp.UserPreferences {
		persistWithWindow("preference", p.Summary, map[string]any{"context": p.Context, "strength": p.Strength, "tags": p.Tags, "files": p.Files})
	}
	for _, a := range resp.AntiPatterns {
		persistWithWindow("anti_pattern", a.Summary, map[string]any{"why_bad": a.WhyBad, "alternative": a.Alternative, "tags": a.Tags, "files": a.Files})
	}
	for _, l := range resp.Learnings {
		persistWithWindow("learning", l.Summary, map[string]any{"example": l.Example, "reusable": l.Reusable, "tags": l.Tags, "files": l.Files})
	}

	// Save marker so we don't re-extract this window
	markerName := fmt.Sprintf("learning:%s:window:%d:marker", sessionID, window.WindowIndex)
	if _, err := store.SaveResult(ctx, memory.SaveOptions{
		Name:      markerName,
		Type:      "marker",
		Workspace: workspace,
		Summary:   fmt.Sprintf("learnings extracted from window %d", window.WindowIndex),
		SessionID: sessionID,
	}); err != nil {
		logger.Warn("failed to save marker", obs.Int("window_index", window.WindowIndex), obs.Err(err))
	}
	// Generate marker embedding for lookup
	if embedder != nil {
		if embedding, err := embedder.Embed(ctx, markerName); err == nil && len(embedding.Vec) > 0 {
			_ = store.UpdateEmbedding(ctx, markerName, workspace, embedding.Vec)
		}
	}

	return count
}
