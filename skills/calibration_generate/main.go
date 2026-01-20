// Package main implements the calibration/generate skill for user profile generation.
// It analyzes sessions at the context window level to extract communication style,
// tone, working patterns, and deeper user understanding.
package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/jkatigb/agentctl/internal/adapters/skillslib/executil"
	"github.com/jkatigb/agentctl/internal/adapters/skillslib/skillerr"
	"github.com/jkatigb/agentctl/internal/adapters/skillslib/skillmain"
	"github.com/jkatigb/agentctl/internal/adapters/skillslib/skillout"
	"github.com/jkatigb/agentctl/internal/adapters/skillslib/workspaceutil"
	"github.com/jkatigb/agentctl/internal/calibration"
	llmproviders "github.com/jkatigb/agentctl/internal/providers/llm"
	"github.com/jkatigb/agentctl/internal/sessionkit"
	"github.com/jkatigb/agentctl/internal/storage/sessions"
)

const command = "calibration/generate"

// Input represents the skill input parameters.
type Input struct {
	Workspace  string   `json:"workspace,omitempty"`
	SessionIDs []string `json:"session_ids,omitempty"` // Specific sessions to analyze
	Scope      string   `json:"scope,omitempty"`       // recent|all|incremental (default: incremental)
	MaxWindows int      `json:"max_windows,omitempty"` // Max windows per run (default: 50)
	Force      bool     `json:"force,omitempty"`       // Re-analyze even if already done
	DryRun     bool     `json:"dry_run,omitempty"`     // Skip LLM and persistence
}

// Output represents the skill output.
type Output struct {
	ProfileID        string   `json:"profile_id"`
	Workspace        string   `json:"workspace"`
	WindowsAnalyzed  int      `json:"windows_analyzed"`
	WindowsSkipped   int      `json:"windows_skipped"`
	SignalsExtracted int      `json:"signals_extracted"`
	ProfileChanged   bool     `json:"profile_changed"`
	Changes          []string `json:"changes,omitempty"`
	Status           string   `json:"status"`
	Message          string   `json:"message,omitempty"`
	Provider         string   `json:"provider,omitempty"`
}

// LLMSignals is the expected JSON structure from the LLM.
type LLMSignals struct {
	// Surface signals
	Communication []LLMSignal `json:"communication"`
	Tone          []LLMSignal `json:"tone"`
	WorkingStyle  []LLMSignal `json:"working_style"`

	// Deeper inference
	Cognition []LLMSignal    `json:"cognition"`
	Expertise []LLMExpertise `json:"expertise"`
	Trust     []LLMSignal    `json:"trust"`
}

// LLMSignal represents a single signal extracted by the LLM.
type LLMSignal struct {
	Dimension  string  `json:"dimension"`  // e.g., "communication.verbosity"
	Direction  string  `json:"direction"`  // increase|decrease|confirm or specific value
	Quote      string  `json:"quote"`      // User's exact words as evidence
	Confidence float32 `json:"confidence"` // 0.0-1.0
}

// LLMExpertise represents an expertise signal.
type LLMExpertise struct {
	Domain string `json:"domain"` // e.g., "Go concurrency"
	Level  string `json:"level"`  // expert|proficient|familiar|learning|novice
	Quote  string `json:"quote"`  // Evidence
}

const (
	defaultMaxWindows = 50
	maxContentTokens  = 6000
)

func main() {
	skillmain.Main(command, run)
}

func run(ctx context.Context, rc *skillmain.RunContext, in Input) error {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Minute)
	defer cancel()

	// Resolve workspace
	workspace := workspaceutil.Resolve(in.Workspace, "", rc.Workspace)
	if workspace == "" {
		return skillerr.Arg("workspace is required", skillerr.WithHint("Provide workspace path or run from a project directory"))
	}

	// Default scope
	scope := in.Scope
	if scope == "" {
		scope = "incremental"
	}
	if scope != "recent" && scope != "all" && scope != "incremental" {
		return skillerr.Arg(fmt.Sprintf("invalid scope %q", scope), skillerr.WithHint("Use one of: recent, all, incremental"))
	}

	// Default max windows
	maxWindows := in.MaxWindows
	if maxWindows <= 0 {
		maxWindows = defaultMaxWindows
	}

	// Open stores
	sessionStore, sessionCleanup, err := sessionkit.OpenSessions(ctx, rc.Config)
	if err != nil {
		return skillerr.IO("open sessions store", skillerr.WithCause(err))
	}
	defer sessionCleanup()

	memStore, memCleanup, err := sessionkit.OpenMemory(ctx, rc.Config)
	if err != nil {
		return skillerr.IO("open memory store", skillerr.WithCause(err))
	}
	defer memCleanup()

	// Load or create profile
	profile, err := calibration.LoadProfile(ctx, memStore, workspace)
	if err != nil {
		return skillerr.IO("load profile", skillerr.WithCause(err))
	}
	if profile == nil {
		profile = calibration.NewProfile(workspace)
	}

	// Dry run check
	if in.DryRun {
		output := Output{
			ProfileID: profile.ProfileID,
			Workspace: workspace,
			Status:    "dry_run",
			Message:   "dry run: would analyze sessions for calibration signals",
		}
		return skillout.Emit(rc, command, output)
	}

	// Get LLM providers
	providers := llmproviders.ExtractionProviders()
	if len(providers) == 0 {
		return skillerr.Arg("no LLM provider configured", skillerr.WithHint("Set ANTHROPIC_API_KEY or OPENAI_API_KEY"))
	}

	// Resolve sessions to analyze
	sessionList, err := resolveSessions(ctx, sessionStore, workspace, in.SessionIDs, scope)
	if err != nil {
		return skillerr.IO("resolve sessions", skillerr.WithCause(err))
	}

	if len(sessionList) == 0 {
		output := Output{
			ProfileID: profile.ProfileID,
			Workspace: workspace,
			Status:    "no_sessions",
			Message:   "no sessions found for workspace",
		}
		return skillout.Emit(rc, command, output)
	}

	// Process windows
	totalAnalyzed := 0
	totalSkipped := 0
	totalSignals := 0
	var allSignals []calibration.Signal
	var allDomains []calibration.Domain
	var usedProvider string

	for _, session := range sessionList {
		if totalAnalyzed >= maxWindows {
			break
		}

		// Get context windows for this session
		windows, err := sessionStore.GetContextWindows(ctx, session.ID)
		if err != nil {
			log.Printf("[WARN] calibration/generate: failed to get windows for session %s: %v", session.ID, err)
			continue
		}

		for _, window := range windows {
			if totalAnalyzed >= maxWindows {
				break
			}

			// Build window content
			content, err := buildWindowContent(ctx, sessionStore, session.ID, window)
			if err != nil {
				log.Printf("[WARN] calibration/generate: failed to build content for window %d: %v", window.WindowIndex, err)
				continue
			}

			if content == "" {
				continue
			}

			// Compute content hash for idempotency
			contentHash := hashContent(content)

			// Check if already analyzed
			if !in.Force && profile.IsWindowAnalyzed(session.ID, window.WindowIndex, contentHash) {
				totalSkipped++
				continue
			}

			// Extract signals via LLM
			llmSignals, provider, err := extractSignals(ctx, providers, session.ID, window.WindowIndex, content)
			if err != nil {
				log.Printf("[WARN] calibration/generate: LLM extraction failed for window %d: %v", window.WindowIndex, err)
				continue
			}

			usedProvider = provider
			totalAnalyzed++

			// Convert LLM signals to calibration signals
			signals, domains := convertSignals(llmSignals, session.ID, window.WindowIndex)
			signalCount := len(signals)
			totalSignals += signalCount

			allSignals = append(allSignals, signals...)
			allDomains = append(allDomains, domains...)

			// Mark window as analyzed
			profile.MarkWindowAnalyzed(session.ID, window.WindowIndex, contentHash, signalCount)
		}
	}

	// Aggregate signals into profile
	changed := calibration.AggregateSignals(profile, allSignals)

	// Update expertise
	if len(allDomains) > 0 {
		calibration.UpdateExpertise(profile, allDomains)
	}

	// Create timeline snapshot if changed
	var changes []string
	if changed {
		profile.Version++
		profile.UpdatedAt = time.Now().UTC()

		// Capture changes for output
		changes = detectChanges(profile)

		// Add snapshot
		var sessionID string
		if len(sessionList) > 0 {
			sessionID = sessionList[0].ID
		}
		profile.AddSnapshot("calibration_generate", sessionID, nil)
	}

	// Save profile
	if err := calibration.SaveProfile(ctx, memStore, profile); err != nil {
		return skillerr.IO("save profile", skillerr.WithCause(err))
	}

	output := Output{
		ProfileID:        profile.ProfileID,
		Workspace:        workspace,
		WindowsAnalyzed:  totalAnalyzed,
		WindowsSkipped:   totalSkipped,
		SignalsExtracted: totalSignals,
		ProfileChanged:   changed,
		Changes:          changes,
		Status:           "ok",
		Provider:         usedProvider,
	}

	if totalAnalyzed == 0 && totalSkipped > 0 {
		output.Message = fmt.Sprintf("all %d windows already analyzed (use force=true to re-analyze)", totalSkipped)
	} else {
		output.Message = fmt.Sprintf("analyzed %d windows, extracted %d signals", totalAnalyzed, totalSignals)
	}

	return skillout.Emit(rc, command, output)
}

// resolveSessions gets the list of sessions to analyze based on scope.
func resolveSessions(ctx context.Context, store *sessions.Store, workspace string, sessionIDs []string, scope string) ([]sessions.Session, error) {
	// If specific sessions provided, use those
	if len(sessionIDs) > 0 {
		var result []sessions.Session
		for _, id := range sessionIDs {
			session, err := store.Get(ctx, id)
			if err != nil {
				continue
			}
			result = append(result, session)
		}
		return result, nil
	}

	// Otherwise, list by workspace
	opts := sessions.ListOptions{
		WorkspacePath: workspace,
	}

	switch scope {
	case "recent":
		opts.Limit = 5
	case "all":
		opts.Limit = 100
	case "incremental":
		opts.Limit = 20
	}

	return store.List(ctx, opts)
}

// buildWindowContent builds the transcript content for a context window.
func buildWindowContent(ctx context.Context, store *sessions.Store, sessionID string, window sessions.ContextWindow) (string, error) {
	var contentParts []string
	var totalChars int
	maxChars := maxContentTokens * 4 // Rough token estimate

	for chunkIdx := window.ChunkStart; chunkIdx <= window.ChunkEnd; chunkIdx++ {
		chunk, err := store.GetChunk(ctx, sessionID, chunkIdx)
		if err != nil {
			continue
		}

		preview := strings.TrimSpace(chunk.ContentPreview)
		if preview == "" {
			continue
		}

		// Check budget
		if totalChars+len(preview) > maxChars {
			// Truncate to fit
			remaining := maxChars - totalChars
			if remaining > 100 {
				preview = preview[:remaining]
			} else {
				break
			}
		}

		contentParts = append(contentParts, fmt.Sprintf("[%s] %s", chunk.ChunkType, preview))
		totalChars += len(preview)
	}

	if len(contentParts) == 0 {
		return "", nil
	}

	return strings.Join(contentParts, "\n\n"), nil
}

// hashContent computes a SHA256 hash of the content.
func hashContent(content string) string {
	h := sha256.Sum256([]byte(content))
	return hex.EncodeToString(h[:8]) // First 8 bytes = 16 hex chars
}

// extractSignals uses LLM to extract calibration signals from window content.
func extractSignals(ctx context.Context, providers []llmproviders.Provider, sessionID string, windowIndex int, content string) (*LLMSignals, string, error) {
	prompt := buildExtractionPrompt(sessionID, windowIndex, content)

	var lastErr error
	for _, p := range providers {
		var signals *LLMSignals
		var err error

		if p.IsCLI {
			signals, err = extractWithCLI(ctx, p, prompt)
		} else {
			signals, err = extractWithAPI(ctx, p, prompt)
		}

		if err == nil {
			return signals, p.Name, nil
		}
		lastErr = err
	}

	return nil, "", lastErr
}

func extractWithAPI(ctx context.Context, provider llmproviders.Provider, prompt string) (*LLMSignals, error) {
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

func extractWithCLI(ctx context.Context, provider llmproviders.Provider, prompt string) (*LLMSignals, error) {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()

	result := executil.Run(ctx, "", "gemini", "-m", provider.Model, "-p", prompt)
	if !result.Success() {
		return nil, fmt.Errorf("gemini failed: %w", result.Err)
	}

	return parseResponse(result.String())
}

func buildExtractionPrompt(sessionID string, windowIndex int, content string) string {
	return fmt.Sprintf(`You are analyzing a context window from a coding session to extract USER CALIBRATION SIGNALS.
These signals help understand the user's communication preferences, working style, and deeper characteristics.

<window session="%s" index="%d">
%s
</window>

Extract signals in these categories. For each signal, provide the user's exact quote as evidence.

SURFACE SIGNALS (observable preferences):

1. **Communication** - How they prefer to receive information:
   - communication.verbosity: Do they want concise or detailed responses? (increase/decrease/confirm)
   - communication.technical_depth: Do they want high-level or deep-dive explanations? (increase/decrease/confirm)
   - communication.code_preference: Do they prefer full code, snippets, or pseudocode? (full-code/snippets/pseudocode)
   - communication.explanation_style: Do they prefer examples first or theory first? (examples-first/theory-first/mixed)

2. **Tone** - Interaction style preferences:
   - tone.formality: Formal, casual, or adaptive? (increase=more formal, decrease=more casual)
   - tone.patience: Are they patient or impatient? (increase/decrease/confirm)
   - tone.assertiveness: Directive, collaborative, or deferential? (directive/collaborative/deferential)

3. **Working Style** - How they approach problems:
   - working_style.problem_approach: Iterative, big-picture, test-driven, or exploratory?
   - working_style.feedback_style: Direct, diplomatic, or detailed-critique?
   - working_style.collaboration_mode: Pair-programming, review-based, or autonomous?

DEEPER SIGNALS (inferred characteristics):

4. **Cognition** - How they think and process information:
   - cognition.mental_model: Visual, sequential, hierarchical, or associative?
   - cognition.learning_style: Examples-first, theory-first, hands-on, or reading?
   - cognition.motivations: What drives them? (speed/correctness/elegance/learning/pragmatism)

5. **Expertise** - Domain knowledge indicators:
   - Look for technical language that reveals expertise level
   - Note domains where they seem strong vs. learning

6. **Trust** - Autonomy and verification preferences:
   - trust.autonomy_level: Do they want to delegate or stay in control? (increase/decrease)
   - trust.verification_need: Do they verify AI suggestions? (always/sometimes/rarely)
   - trust.pushback_pattern: How often do they push back? (frequent/selective/rare)
   - trust.corrections_style: How do they correct mistakes? (direct/diplomatic/questioning)

Return JSON:
{
  "communication": [
    {"dimension": "communication.verbosity", "direction": "increase", "quote": "can you give me more detail?", "confidence": 0.8}
  ],
  "tone": [
    {"dimension": "tone.formality", "direction": "decrease", "quote": "just give me the gist", "confidence": 0.7}
  ],
  "working_style": [
    {"dimension": "working_style.problem_approach", "direction": "iterative", "quote": "let's start simple and build up", "confidence": 0.9}
  ],
  "cognition": [
    {"dimension": "cognition.mental_model", "direction": "visual", "quote": "can you draw a diagram?", "confidence": 0.8}
  ],
  "expertise": [
    {"domain": "Go concurrency", "level": "expert", "quote": "we need a mutex here for the race condition"}
  ],
  "trust": [
    {"dimension": "trust.autonomy_level", "direction": "increase", "quote": "just do it, I trust you", "confidence": 0.8}
  ]
}

Guidelines:
- Only extract signals where you have clear evidence (user quotes)
- Confidence should reflect how clear the signal is (0.5-1.0)
- For direction, use increase/decrease for scaled dimensions, or the specific value
- Expertise level: expert|proficient|familiar|learning|novice
- It's OK to return empty arrays if no signals are found for a category
- Look for patterns in HOW the user communicates, not just WHAT they say

Return ONLY valid JSON, no markdown fences.`, sessionID, windowIndex, content)
}

func parseResponse(content string) (*LLMSignals, error) {
	// Clean up response
	content = strings.TrimSpace(content)
	content = strings.TrimPrefix(content, "```json")
	content = strings.TrimPrefix(content, "```")
	content = strings.TrimSuffix(content, "```")
	content = strings.TrimSpace(content)

	var signals LLMSignals
	if err := json.Unmarshal([]byte(content), &signals); err != nil {
		return nil, fmt.Errorf("parse error: %w", err)
	}

	return &signals, nil
}

// convertSignals converts LLM signals to calibration.Signal and calibration.Domain.
func convertSignals(llm *LLMSignals, sessionID string, windowIndex int) ([]calibration.Signal, []calibration.Domain) {
	now := time.Now().UTC()
	var signals []calibration.Signal
	var domains []calibration.Domain

	// Convert each category
	for _, s := range llm.Communication {
		signals = append(signals, calibration.Signal{
			SessionID:   sessionID,
			WindowIndex: windowIndex,
			At:          now,
			Quote:       s.Quote,
			Dimension:   s.Dimension,
			Direction:   s.Direction,
			Confidence:  s.Confidence,
		})
	}

	for _, s := range llm.Tone {
		signals = append(signals, calibration.Signal{
			SessionID:   sessionID,
			WindowIndex: windowIndex,
			At:          now,
			Quote:       s.Quote,
			Dimension:   s.Dimension,
			Direction:   s.Direction,
			Confidence:  s.Confidence,
		})
	}

	for _, s := range llm.WorkingStyle {
		signals = append(signals, calibration.Signal{
			SessionID:   sessionID,
			WindowIndex: windowIndex,
			At:          now,
			Quote:       s.Quote,
			Dimension:   s.Dimension,
			Direction:   s.Direction,
			Confidence:  s.Confidence,
		})
	}

	for _, s := range llm.Cognition {
		signals = append(signals, calibration.Signal{
			SessionID:   sessionID,
			WindowIndex: windowIndex,
			At:          now,
			Quote:       s.Quote,
			Dimension:   s.Dimension,
			Direction:   s.Direction,
			Confidence:  s.Confidence,
		})
	}

	for _, s := range llm.Trust {
		signals = append(signals, calibration.Signal{
			SessionID:   sessionID,
			WindowIndex: windowIndex,
			At:          now,
			Quote:       s.Quote,
			Dimension:   s.Dimension,
			Direction:   s.Direction,
			Confidence:  s.Confidence,
		})
	}

	// Convert expertise
	for _, e := range llm.Expertise {
		domains = append(domains, calibration.Domain{
			Name:        e.Domain,
			Level:       e.Level,
			LastSeen:    now,
			SignalCount: 1,
		})
	}

	return signals, domains
}

// detectChanges returns a list of human-readable changes in the profile.
func detectChanges(profile *calibration.Profile) []string {
	var changes []string

	// Report current values (in a real impl, we'd compare to previous snapshot)
	changes = append(changes, fmt.Sprintf("communication: %s verbosity, %s depth",
		profile.Communication.Verbosity, profile.Communication.TechnicalDepth))
	changes = append(changes, fmt.Sprintf("tone: %s formality, %s patience",
		profile.Tone.Formality, profile.Tone.Patience))
	changes = append(changes, fmt.Sprintf("working_style: %s approach, %s collaboration",
		profile.WorkingStyle.ProblemApproach, profile.WorkingStyle.CollaborationMode))
	changes = append(changes, fmt.Sprintf("trust: %s autonomy, %s verification",
		profile.Trust.AutonomyLevel, profile.Trust.VerificationNeed))

	return changes
}
