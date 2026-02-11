// Package main implements the code/counsel skill.
//
// This skill provides multi-perspective code analysis using LLMs.
// It takes code evidence (from smart_read or direct files) and analyzes
// it from different perspectives: security, performance, readability, etc.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/rs/zerolog"

	"github.com/jkatigb/agentctl/internal/adapters/skillslib/secretutil"
	"github.com/jkatigb/agentctl/internal/adapters/skillslib/skillerr"
	"github.com/jkatigb/agentctl/internal/adapters/skillslib/skillmain"
	"github.com/jkatigb/agentctl/internal/adapters/skillslib/skillout"
	"github.com/jkatigb/agentctl/internal/codecontext"
	"github.com/jkatigb/agentctl/internal/codecontext/guard"
	"github.com/jkatigb/agentctl/internal/indexing/semantic"
	errs "github.com/jkatigb/agentctl/internal/platform/errors"
	"github.com/jkatigb/agentctl/internal/retrieval"
	"github.com/jkatigb/agentctl/internal/storage/memory"
)

const command = "code/counsel"

// Default limits.
const (
	DefaultMaxFiles        = 8
	DefaultMaxBytesPerFile = 200 * 1024 // 200KB
	DefaultMaxSnippets     = 50
	DefaultContextLines    = 5
	DefaultTimeout         = 60 * time.Second
)

// Perspective identifiers.
const (
	PerspectiveSecurity    = "security"
	PerspectivePerformance = "performance"
	PerspectiveReadability = "readability"
	PerspectiveCorrectness = "correctness"
	PerspectiveGeneral     = "general"
)

// Input is the expected JSON input for multi-perspective code analysis.
type Input struct {
	// Query is the natural-language question or analysis focus.
	Query string `json:"query" validate:"required"`

	// Files are explicit file paths to analyze (optional).
	// If empty and AutoFiles is true, files are auto-selected.
	Files []string `json:"files,omitempty"`

	// AutoFiles enables automatic file selection based on query.
	AutoFiles *bool `json:"auto_files,omitempty"`

	// Evidence is pre-computed code evidence (from smart_read).
	// If provided, Files/AutoFiles are ignored.
	Evidence *codecontext.Evidence `json:"evidence,omitempty"`

	// Perspectives specifies which analysis perspectives to use.
	// Options: "security", "performance", "readability", "correctness", "general"
	// If empty, defaults to ["general"].
	Perspectives []string `json:"perspectives,omitempty"`

	// MaxFiles limits the number of files to process (for auto-selection).
	MaxFiles int `json:"max_files,omitempty"`

	// Provider specifies the LLM provider to use.
	// Options: "anthropic", "openai", "cerebras"
	// If empty, auto-detects from environment.
	Provider string `json:"provider,omitempty"`
}

// Output is the skill output with multi-perspective analysis results and statistics.
type Output struct {
	// Analyses contains results from each perspective.
	Analyses []PerspectiveAnalysis `json:"analyses"`

	// Summary is an overall summary combining all perspectives.
	Summary string `json:"summary,omitempty"`

	// SecretWarnings contains any detected secrets (redacted).
	SecretWarnings []SecretWarning `json:"secret_warnings,omitempty"`

	// Stats provides metrics about the analysis process.
	Stats Stats `json:"stats"`
}

// SecretWarning represents a detected secret.
type SecretWarning = secretutil.Finding

// PerspectiveAnalysis contains the analysis from a single perspective with findings and scoring.
type PerspectiveAnalysis struct {
	// Perspective is the analysis type.
	Perspective string `json:"perspective"`

	// Findings are the key observations.
	Findings []Finding `json:"findings"`

	// Summary is a brief overview from this perspective.
	Summary string `json:"summary"`

	// Score is an overall assessment (0.0-1.0, higher is better).
	Score float64 `json:"score,omitempty"`

	// DurationMS is the analysis time.
	DurationMS int64 `json:"duration_ms"`
}

// Finding represents a single observation from the analysis with severity and suggestions.
type Finding struct {
	// Type categorizes the finding (e.g., "issue", "suggestion", "positive").
	Type string `json:"type"`

	// Severity indicates importance: "critical", "high", "medium", "low", "info".
	Severity string `json:"severity,omitempty"`

	// Title is a brief description.
	Title string `json:"title"`

	// Description provides detailed explanation.
	Description string `json:"description"`

	// Location references the relevant code (file:line or symbol).
	Location string `json:"location,omitempty"`

	// Suggestion offers a potential improvement.
	Suggestion string `json:"suggestion,omitempty"`
}

// Stats provides metrics about the analysis process with timing and provider information.
type Stats struct {
	LatencyMS       int      `json:"latency_ms"`
	FilesAnalyzed   int      `json:"files_analyzed"`
	SnippetsUsed    int      `json:"snippets_used"`
	PerspectivesRun []string `json:"perspectives_run"`
	Provider        string   `json:"provider"`
}

// main is the skill entry point for code/counsel.
func main() {
	skillmain.Main(command, skillmain.Chain(run,
		skillmain.WithTimeout[Input](DefaultTimeout),
		skillmain.WithRecover[Input](),
	))
}

// run orchestrates multi-perspective code analysis using LLMs with evidence gathering and secret detection.
//
// Index:
// - Purpose: Analyze code from multiple perspectives (security, performance, readability, correctness) using LLMs
// - Flow: apply defaults → gather evidence → scan for secrets → detect provider → run parallel perspectives → generate summary
// - SideEffects: makes HTTP API calls to LLM providers; reads file contents; scans for secrets
// - FailureModes: missing API keys, file access errors, LLM API failures, parsing errors, timeout
// - Observability: emits analysis results, secret warnings, timing metrics, and provider information
// - Related: gatherEvidence, runPerspectiveAnalysis, detectProvider, renderEvidenceForLLM
// - Keywords: code/counsel, code_analysis, multi_perspective, llm_analysis, security_review, performance_review
func run(ctx context.Context, rc *skillmain.RunContext, in Input) error {
	start := time.Now()
	logger := rc.Logger.With().Str("skill", command).Logger()

	// Apply defaults
	if in.AutoFiles == nil {
		defaultTrue := true
		in.AutoFiles = &defaultTrue
	}
	if in.MaxFiles <= 0 {
		in.MaxFiles = DefaultMaxFiles
	}
	if len(in.Perspectives) == 0 {
		in.Perspectives = []string{PerspectiveGeneral}
	}

	out := &Output{
		Analyses: []PerspectiveAnalysis{},
		Stats: Stats{
			PerspectivesRun: in.Perspectives,
		},
	}

	// Get evidence
	var evidence *codecontext.Evidence
	if in.Evidence != nil {
		// Use pre-computed evidence
		evidence = in.Evidence
	} else {
		// Gather evidence
		gathered, err := gatherEvidence(ctx, rc, in, logger)
		if err != nil {
			return skillerr.WrapRuntime("gather evidence", err)
		}
		evidence = gathered
	}

	out.Stats.FilesAnalyzed = evidence.Stats.FilesProcessed
	out.Stats.SnippetsUsed = evidence.Stats.SnippetsExtracted

	// Scan evidence for secrets
	secretWarnings, hasHighSeverity := secretutil.ScanEvidence(ctx, evidence, logger, guard.ModeWarn)
	out.SecretWarnings = secretWarnings

	// Warn but don't block on secrets - the analysis may be about security review
	if hasHighSeverity {
		logger.Warn().Int("count", len(secretWarnings)).Msg("high-severity secrets detected in evidence - redacted in warnings")
	}

	// Detect provider
	provider := in.Provider
	if provider == "" {
		provider = detectProvider()
	}
	out.Stats.Provider = provider

	// Render evidence as context for LLM
	contextStr := renderEvidenceForLLM(evidence)

	// Run perspectives in parallel
	var wg sync.WaitGroup
	var mu sync.Mutex

	for _, perspective := range in.Perspectives {
		wg.Add(1)
		go func(p string) {
			defer wg.Done()

			perspectiveStart := time.Now()
			analysis, err := runPerspectiveAnalysis(ctx, rc, provider, p, in.Query, contextStr, logger)
			if err != nil {
				logger.Warn().Err(err).Str("perspective", p).Msg("perspective analysis failed")
				analysis = &PerspectiveAnalysis{
					Perspective: p,
					Summary:     "Analysis failed: " + err.Error(),
					Findings:    []Finding{},
				}
			}
			analysis.DurationMS = time.Since(perspectiveStart).Milliseconds()

			mu.Lock()
			out.Analyses = append(out.Analyses, *analysis)
			mu.Unlock()
		}(perspective)
	}

	wg.Wait()

	// Generate overall summary if multiple perspectives
	if len(out.Analyses) > 1 {
		out.Summary = generateOverallSummary(out.Analyses)
	} else if len(out.Analyses) == 1 {
		out.Summary = out.Analyses[0].Summary
	}

	out.Stats.LatencyMS = int(time.Since(start).Milliseconds())

	return skillout.Emit(rc, command, out)
}

// gatherEvidence collects code evidence for analysis with auto-selection and explicit file support.
func gatherEvidence(ctx context.Context, rc *skillmain.RunContext, in Input, logger zerolog.Logger) (*codecontext.Evidence, error) {
	var candidates []codecontext.Candidate

	if len(in.Files) > 0 {
		// Explicit files provided
		for _, f := range in.Files {
			candidates = append(candidates, codecontext.Candidate{
				Path:     f,
				Priority: 1.0,
			})
		}
	} else if *in.AutoFiles {
		// Auto-select files
		selected, err := autoSelectFiles(ctx, rc, in.Query, in.MaxFiles, logger)
		if err != nil {
			return nil, skillerr.WrapRuntime("auto-select files", err)
		}
		candidates = selected
	}

	if len(candidates) == 0 {
		return &codecontext.Evidence{
			Snippets: []codecontext.Snippet{},
			Stats:    codecontext.EvidenceStats{},
		}, nil
	}

	// Collect evidence
	evidence, err := codecontext.Collect(ctx, codecontext.CollectOpts{
		Candidates:      candidates,
		Query:           in.Query,
		PathValidator:   rc.PathValidator,
		MaxFiles:        in.MaxFiles,
		MaxSnippets:     DefaultMaxSnippets,
		MaxBytesPerFile: DefaultMaxBytesPerFile,
		ContextLines:    DefaultContextLines,
		Mode:            codecontext.ModeSnippets,
	})
	if err != nil {
		return nil, skillerr.WrapRuntime("collect evidence", err)
	}

	return evidence, nil
}

// autoSelectFiles uses retrieval.Generator to find relevant files based on semantic search.
func autoSelectFiles(ctx context.Context, rc *skillmain.RunContext, query string, maxFiles int, logger zerolog.Logger) ([]codecontext.Candidate, error) {
	memStore, err := memory.OpenWithConfig(ctx, rc.Config)
	if err != nil {
		return nil, skillerr.WrapIO("open memory store", err)
	}
	defer func() { errs.Ignore(memStore.Close(), "close memory store") }()

	// Create embedding provider (optional)
	var embedProvider semantic.EmbeddingProvider
	embedder, err := semantic.NewEmbedderFromConfig(semantic.ScopeSymbols, rc.Config, skillmain.EmbeddingGuard(rc))
	if err == nil {
		embedProvider = &embedderAdapter{embedder: embedder}
	}

	// Create generator
	gen := retrieval.NewGenerator(memStore, embedProvider, rc.Workspace, logger)

	// Generate candidates
	result, err := gen.Generate(ctx, rc.Workspace, query, retrieval.Options{
		MaxTotalCandidates:    maxFiles,
		MaxSymbolCandidates:   maxFiles,
		MaxSemanticCandidates: maxFiles / 2,
		MaxRipgrepCandidates:  maxFiles,
		EnableSymbols:         true,
		EnableSemantic:        embedProvider != nil,
		EnableRipgrep:         true,
		MinTotalCandidates:    3,
	})
	if err != nil {
		return nil, skillerr.WrapRuntime("generate candidates", err)
	}

	candidates := make([]codecontext.Candidate, 0, len(result.Candidates))
	for _, c := range result.Candidates {
		candidates = append(candidates, codecontext.Candidate{
			Path:     c.Path,
			SymbolID: c.SymbolID,
			LineHint: c.Line,
			Priority: c.Score,
		})
	}

	return candidates, nil
}

// renderEvidenceForLLM converts evidence to a formatted string suitable for LLM context.
func renderEvidenceForLLM(evidence *codecontext.Evidence) string {
	var sb strings.Builder

	sb.WriteString("# Code Context\n\n")

	for _, snippet := range evidence.Snippets {
		sb.WriteString(fmt.Sprintf("## %s (lines %d-%d)\n", snippet.File, snippet.StartLine, snippet.EndLine))
		if snippet.SymbolID != "" {
			sb.WriteString(fmt.Sprintf("Symbol: %s\n", snippet.SymbolID))
		}
		sb.WriteString("```\n")
		sb.WriteString(snippet.Text)
		if !strings.HasSuffix(snippet.Text, "\n") {
			sb.WriteString("\n")
		}
		sb.WriteString("```\n\n")
	}

	return sb.String()
}

// detectProvider returns the best available LLM provider based on environment variables.
func detectProvider() string {
	if os.Getenv("CEREBRAS_API_KEY") != "" {
		return "cerebras"
	}
	if os.Getenv("ANTHROPIC_API_KEY") != "" {
		return "anthropic"
	}
	if os.Getenv("OPENAI_API_KEY") != "" {
		return "openai"
	}
	return "anthropic" // Default fallback
}

// runPerspectiveAnalysis runs analysis from a specific perspective using LLM with structured parsing.
func runPerspectiveAnalysis(ctx context.Context, rc *skillmain.RunContext, provider, perspective, query, codeContext string, logger zerolog.Logger) (*PerspectiveAnalysis, error) {
	prompt := buildAnalysisPrompt(perspective, query, codeContext)

	var response string
	err := skillmain.GuardCall(rc, skillmain.BreakerLLMProvider, ctx, func(ctx context.Context) error {
		var e error
		response, e = callLLM(ctx, provider, prompt)
		return e
	})
	if err != nil {
		return nil, skillerr.WrapRuntime("call LLM", err)
	}

	analysis, err := parseAnalysisResponse(perspective, response)
	if err != nil {
		logger.Warn().Err(err).Msg("failed to parse structured response, using raw")
		return &PerspectiveAnalysis{
			Perspective: perspective,
			Summary:     response,
			Findings:    []Finding{},
		}, nil
	}

	return analysis, nil
}

// buildAnalysisPrompt constructs the perspective-specific prompt for LLM analysis with JSON output format.
func buildAnalysisPrompt(perspective, query, codeContext string) string {
	var systemContext string
	switch perspective {
	case PerspectiveSecurity:
		systemContext = "You are a security analyst reviewing code for vulnerabilities. Focus on: input validation, authentication, authorization, injection attacks, data exposure, and security best practices."
	case PerspectivePerformance:
		systemContext = "You are a performance engineer reviewing code for optimization opportunities. Focus on: algorithmic complexity, memory usage, I/O patterns, caching opportunities, and bottlenecks."
	case PerspectiveReadability:
		systemContext = "You are a code reviewer focusing on maintainability. Focus on: naming conventions, code organization, documentation, complexity, and adherence to best practices."
	case PerspectiveCorrectness:
		systemContext = "You are a quality engineer reviewing code for bugs and edge cases. Focus on: error handling, null checks, boundary conditions, race conditions, and logical errors."
	default:
		systemContext = "You are a senior software engineer reviewing code. Provide a balanced analysis covering correctness, readability, and potential improvements."
	}

	return fmt.Sprintf(`%s

Analyze the following code in relation to this query: %s

%s

Respond in JSON format:
{
  "summary": "Brief overview of findings",
  "score": 0.0-1.0,
  "findings": [
    {
      "type": "issue|suggestion|positive",
      "severity": "critical|high|medium|low|info",
      "title": "Brief title",
      "description": "Detailed explanation",
      "location": "file:line or symbol name",
      "suggestion": "How to improve (if applicable)"
    }
  ]
}`, systemContext, query, codeContext)
}

// callLLM makes an API call to the specified LLM provider with error handling and fallbacks.
func callLLM(ctx context.Context, provider, prompt string) (string, error) {
	switch provider {
	case "cerebras":
		return callCerebras(ctx, prompt)
	case "anthropic":
		return callAnthropic(ctx, prompt)
	case "openai":
		return callOpenAI(ctx, prompt)
	default:
		return callAnthropic(ctx, prompt)
	}
}

// callCerebras makes an API call to Cerebras LLM with authentication and error handling.
func callCerebras(ctx context.Context, prompt string) (string, error) {
	apiKey := os.Getenv("CEREBRAS_API_KEY")
	if apiKey == "" {
		return "", skillerr.Auth("CEREBRAS_API_KEY not set", skillerr.WithHint("Set CEREBRAS_API_KEY to use the Cerebras provider."))
	}

	model := os.Getenv("CEREBRAS_MODEL")
	if model == "" {
		model = "llama-3.3-70b"
	}

	reqBody := map[string]any{
		"model":      model,
		"max_tokens": 4096,
		"messages": []map[string]string{
			{"role": "user", "content": prompt},
		},
	}

	body, err := json.Marshal(reqBody)
	if err != nil {
		return "", skillerr.WrapRuntime("marshal request", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", "https://api.cerebras.ai/v1/chat/completions", bytes.NewReader(body))
	if err != nil {
		return "", skillerr.WrapRuntime("create request", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+apiKey)

	client := &http.Client{Timeout: 60 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", skillerr.WrapRuntime("send request", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return "", skillerr.Runtimef("API error %d: %s", resp.StatusCode, string(respBody))
	}

	var result struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", skillerr.WrapParse("decode response", err)
	}

	if len(result.Choices) == 0 {
		return "", skillerr.Runtime("empty response")
	}

	return result.Choices[0].Message.Content, nil
}

// callAnthropic makes an API call to Anthropic Claude with authentication and error handling.
func callAnthropic(ctx context.Context, prompt string) (string, error) {
	apiKey := os.Getenv("ANTHROPIC_API_KEY")
	if apiKey == "" {
		return "", skillerr.Auth("ANTHROPIC_API_KEY not set", skillerr.WithHint("Set ANTHROPIC_API_KEY to use the Anthropic provider."))
	}

	reqBody := map[string]any{
		"model":      "claude-3-5-haiku-20241022",
		"max_tokens": 4096,
		"messages": []map[string]string{
			{"role": "user", "content": prompt},
		},
	}

	body, err := json.Marshal(reqBody)
	if err != nil {
		return "", skillerr.WrapRuntime("marshal request", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", "https://api.anthropic.com/v1/messages", bytes.NewReader(body))
	if err != nil {
		return "", skillerr.WrapRuntime("create request", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", apiKey)
	req.Header.Set("anthropic-version", "2023-06-01")

	client := &http.Client{Timeout: 60 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", skillerr.WrapRuntime("send request", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return "", skillerr.Runtimef("API error %d: %s", resp.StatusCode, string(respBody))
	}

	var result struct {
		Content []struct {
			Text string `json:"text"`
		} `json:"content"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", skillerr.WrapParse("decode response", err)
	}

	if len(result.Content) == 0 {
		return "", skillerr.Runtime("empty response")
	}

	return result.Content[0].Text, nil
}

// callOpenAI makes an API call to OpenAI GPT with authentication and error handling.
func callOpenAI(ctx context.Context, prompt string) (string, error) {
	apiKey := os.Getenv("OPENAI_API_KEY")
	if apiKey == "" {
		return "", skillerr.Auth("OPENAI_API_KEY not set", skillerr.WithHint("Set OPENAI_API_KEY to use the OpenAI provider."))
	}

	reqBody := map[string]any{
		"model":      "gpt-4o-mini",
		"max_tokens": 4096,
		"messages": []map[string]string{
			{"role": "user", "content": prompt},
		},
	}

	body, err := json.Marshal(reqBody)
	if err != nil {
		return "", skillerr.WrapRuntime("marshal request", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", "https://api.openai.com/v1/chat/completions", bytes.NewReader(body))
	if err != nil {
		return "", skillerr.WrapRuntime("create request", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+apiKey)

	client := &http.Client{Timeout: 60 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", skillerr.WrapRuntime("send request", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return "", skillerr.Runtimef("API error %d: %s", resp.StatusCode, string(respBody))
	}

	var result struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", skillerr.WrapParse("decode response", err)
	}

	if len(result.Choices) == 0 {
		return "", skillerr.Runtime("empty response")
	}

	return result.Choices[0].Message.Content, nil
}

// parseAnalysisResponse parses the LLM's JSON response with markdown code block handling.
func parseAnalysisResponse(perspective, response string) (*PerspectiveAnalysis, error) {
	// Try to extract JSON from the response
	response = strings.TrimSpace(response)

	// Handle markdown code blocks
	if strings.HasPrefix(response, "```json") {
		response = strings.TrimPrefix(response, "```json")
		if idx := strings.Index(response, "```"); idx != -1 {
			response = response[:idx]
		}
	} else if strings.HasPrefix(response, "```") {
		response = strings.TrimPrefix(response, "```")
		if idx := strings.Index(response, "```"); idx != -1 {
			response = response[:idx]
		}
	}

	response = strings.TrimSpace(response)

	var parsed struct {
		Summary  string    `json:"summary"`
		Score    float64   `json:"score"`
		Findings []Finding `json:"findings"`
	}

	if err := json.Unmarshal([]byte(response), &parsed); err != nil {
		return nil, skillerr.WrapParse("parse JSON", err)
	}

	return &PerspectiveAnalysis{
		Perspective: perspective,
		Summary:     parsed.Summary,
		Score:       parsed.Score,
		Findings:    parsed.Findings,
	}, nil
}

// titleCase capitalizes the first letter of each word (replacement for deprecated strings.Title).
func titleCase(s string) string {
	words := strings.Fields(s)
	for i, w := range words {
		if len(w) > 0 {
			words[i] = strings.ToUpper(w[:1]) + w[1:]
		}
	}
	return strings.Join(words, " ")
}

// generateOverallSummary combines analyses from multiple perspectives with scoring aggregation.
func generateOverallSummary(analyses []PerspectiveAnalysis) string {
	var parts []string
	var totalScore float64
	var scoreCount int

	for _, a := range analyses {
		if a.Summary != "" {
			parts = append(parts, fmt.Sprintf("**%s**: %s", titleCase(a.Perspective), a.Summary))
		}
		if a.Score > 0 {
			totalScore += a.Score
			scoreCount++
		}
	}

	summary := strings.Join(parts, "\n\n")

	if scoreCount > 0 {
		avgScore := totalScore / float64(scoreCount)
		summary += fmt.Sprintf("\n\n**Overall Score**: %.2f/1.0", avgScore)
	}

	return summary
}

// embedderAdapter adapts *Embedder to EmbeddingProvider interface for semantic search integration.
type embedderAdapter struct {
	embedder *semantic.Embedder
}

func (a *embedderAdapter) Embed(ctx context.Context, text string) ([]float32, error) {
	result, err := a.embedder.Embed(ctx, text)
	if err != nil {
		return nil, err
	}
	return result.Vec, nil
}

func (a *embedderAdapter) EmbedBatch(ctx context.Context, texts []string) ([][]float32, error) {
	results, err := a.embedder.EmbedBatch(ctx, texts)
	if err != nil {
		return nil, err
	}
	vecs := make([][]float32, len(results))
	for i, r := range results {
		vecs[i] = r.Vec
	}
	return vecs, nil
}

func (a *embedderAdapter) Model() string {
	return a.embedder.Model()
}

func (a *embedderAdapter) Dimensions() int {
	return a.embedder.Dimensions()
}
