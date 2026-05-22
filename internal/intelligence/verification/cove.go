package verification

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// CoVe implements the Chain of Verification pattern with parallel "Draft" verification.
// It orchestrates the full verification pipeline: Baseline -> Extract -> Verify -> Refine.
type CoVe struct {
	llm     LLMClient
	spawner *Spawner
	config  CoVeConfig
}

// CoVeConfig configures the Chain of Verification orchestrator.
type CoVeConfig struct {
	MaxVerifiers        int
	VerificationTimeout time.Duration
	BaselineTimeout     time.Duration
	RefinementTimeout   time.Duration
	ExtractionTimeout   time.Duration
}

// DefaultCoVeConfig returns a CoVeConfig with sensible defaults.
func DefaultCoVeConfig() CoVeConfig {
	return CoVeConfig{
		MaxVerifiers:        10,
		VerificationTimeout: 30 * time.Second,
		BaselineTimeout:     2 * time.Minute,
		RefinementTimeout:   2 * time.Minute,
		ExtractionTimeout:   30 * time.Second,
	}
}

// NewCoVe creates a new Chain of Verification orchestrator.
func NewCoVe(llm LLMClient, config CoVeConfig) *CoVe {
	spawnerConfig := SpawnerConfig{
		MaxWorkers:     config.MaxVerifiers,
		DefaultTimeout: config.VerificationTimeout,
	}

	return &CoVe{
		llm:     llm,
		spawner: NewSpawner(llm, spawnerConfig),
		config:  config,
	}
}

func (c *CoVe) spawnerForRequest(req CoVeRequest) *Spawner {
	maxWorkers := c.config.MaxVerifiers
	verifyTimeout := c.config.VerificationTimeout
	if req.MaxVerifiers > 0 {
		maxWorkers = req.MaxVerifiers
	}
	if req.VerificationTimeout > 0 {
		verifyTimeout = req.VerificationTimeout
	}

	if maxWorkers == c.config.MaxVerifiers && verifyTimeout == c.config.VerificationTimeout {
		return c.spawner
	}

	return NewSpawner(c.llm, SpawnerConfig{
		MaxWorkers:     maxWorkers,
		DefaultTimeout: verifyTimeout,
	})
}

// Run executes the complete Chain of Verification pipeline.
//
// Index:
//
//	Purpose: Run the full CoVe pipeline from baseline generation through verification
//	Keywords: cove_run, baseline_response, claims, verification, corrections, skip_refine
//	Related: CoVe.generateBaseline, CoVe.extractClaims, Spawner.SpawnVerifiers, CoVe.refine
//	Flow: generate baseline → extract claims → verify claims → refine (optional) → return response
//	Resources: LLM client, spawner
//	Events: cove-pipeline-complete
//	OutputFields: CoVeResponse
//
// [[protocol:chain-of-verification-run]]
// [[invariant:skip-refine-bypasses-refinement]]
func (c *CoVe) Run(ctx context.Context, req CoVeRequest) (*CoVeResponse, error) {
	startTime := time.Now()
	metrics := CoVeMetrics{}
	spawner := c.spawnerForRequest(req)

	baselineStart := time.Now()
	baseline, err := c.generateBaseline(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("baseline generation failed: %w", err)
	}
	metrics.BaselineDuration = time.Since(baselineStart)

	extractStart := time.Now()
	claims, err := c.extractClaims(ctx, baseline)
	if err != nil {
		return nil, fmt.Errorf("claim extraction failed: %w", err)
	}
	metrics.ExtractionDuration = time.Since(extractStart)

	verifyStart := time.Now()
	verification, err := spawner.SpawnVerifiers(ctx, req.Question, claims)
	if err != nil {
		return nil, fmt.Errorf("verification failed: %w", err)
	}
	metrics.VerificationDuration = time.Since(verifyStart)

	response := &CoVeResponse{
		Question:         req.Question,
		BaselineResponse: baseline,
		Claims:           claims,
		Verification:     *verification,
		Metrics:          metrics,
	}

	if !req.SkipRefine {
		refineStart := time.Now()
		finalAnswer, corrections, err := c.refine(ctx, req.Question, baseline, verification, req.Mode)
		if err != nil {
			return nil, fmt.Errorf("refinement failed: %w", err)
		}
		response.FinalAnswer = finalAnswer
		response.Corrections = corrections
		metrics.RefinementDuration = time.Since(refineStart)
	}

	metrics.TotalDuration = time.Since(startTime)
	response.Metrics = metrics

	return response, nil
}

// RunFromBaseline executes the verification pipeline starting from a provided baseline.
//
// Index:
//
//	Purpose: Run the CoVe pipeline when a baseline response is precomputed
//	Keywords: cove_run_from_baseline, baseline_response, claims, verification, corrections, skip_refine
//	Related: CoVe.extractClaims, Spawner.SpawnVerifiers, CoVe.refine
//	Flow: validate baseline → extract claims → verify claims → refine (optional) → return response
//	Resources: LLM client, spawner
//	Events: cove-from-baseline-complete
//	OutputFields: CoVeResponse
//
// [[protocol:chain-of-verification-from-baseline]]
// [[invariant:baseline-non-empty-required]]
func (c *CoVe) RunFromBaseline(ctx context.Context, req CoVeRequest, baseline string) (*CoVeResponse, error) {
	startTime := time.Now()
	metrics := CoVeMetrics{}

	if strings.TrimSpace(baseline) == "" {
		return nil, fmt.Errorf("baseline is required")
	}

	spawner := c.spawnerForRequest(req)

	extractStart := time.Now()
	claims, err := c.extractClaims(ctx, baseline)
	if err != nil {
		return nil, fmt.Errorf("claim extraction failed: %w", err)
	}
	metrics.ExtractionDuration = time.Since(extractStart)

	verifyStart := time.Now()
	verification, err := spawner.SpawnVerifiers(ctx, req.Question, claims)
	if err != nil {
		return nil, fmt.Errorf("verification failed: %w", err)
	}
	metrics.VerificationDuration = time.Since(verifyStart)

	response := &CoVeResponse{
		Question:         req.Question,
		BaselineResponse: baseline,
		Claims:           claims,
		Verification:     *verification,
		Metrics:          metrics,
	}

	if !req.SkipRefine {
		refineStart := time.Now()
		finalAnswer, corrections, err := c.refine(ctx, req.Question, baseline, verification, req.Mode)
		if err != nil {
			return nil, fmt.Errorf("refinement failed: %w", err)
		}
		response.FinalAnswer = finalAnswer
		response.Corrections = corrections
		metrics.RefinementDuration = time.Since(refineStart)
	}

	metrics.TotalDuration = time.Since(startTime)
	response.Metrics = metrics

	return response, nil
}

func (c *CoVe) generateBaseline(ctx context.Context, req CoVeRequest) (string, error) {
	baselineCtx, cancel := context.WithTimeout(ctx, c.config.BaselineTimeout)
	defer cancel()

	result, err := c.llm.Chat(baselineCtx, baselineSystemPrompt(), baselineUserPrompt(req.Question, req.Context), LLMCallOptions{
		Temperature: 0.2,
	})
	if err != nil {
		return "", err
	}
	return requireLLMOutput("baseline generation", result)
}

func (c *CoVe) extractClaims(ctx context.Context, baseline string) ([]Claim, error) {
	extractCtx, cancel := context.WithTimeout(ctx, c.config.ExtractionTimeout)
	defer cancel()

	result, err := c.llm.Chat(extractCtx, claimExtractorSystemPrompt(), claimExtractorUserPrompt(baseline), LLMCallOptions{
		Temperature: 0.0,
	})
	if err != nil {
		return nil, err
	}

	claimsJSON, err := requireLLMOutput("claim extraction", result)
	if err != nil {
		return nil, err
	}
	return parseClaimsJSON(claimsJSON)
}

func (c *CoVe) refine(ctx context.Context, question, baseline string, verification *BatchVerificationResult, mode CoVeMode) (string, []Correction, error) {
	refineCtx, cancel := context.WithTimeout(ctx, c.config.RefinementTimeout)
	defer cancel()

	verificationNotes := formatVerificationNotes(verification)
	result, err := c.llm.Chat(refineCtx, refinerSystemPrompt(mode), refinerUserPrompt(question, baseline, verificationNotes), LLMCallOptions{
		Temperature: 0.2,
	})
	if err != nil {
		return "", nil, err
	}
	result, err = requireLLMOutput("refinement", result)
	if err != nil {
		return "", nil, err
	}
	return parseRefinerOutput(result, mode)
}

func requireLLMOutput(stage, raw string) (string, error) {
	output := strings.TrimSpace(raw)
	if output == "" {
		return "", fmt.Errorf("%s returned empty LLM output", stage)
	}
	return output, nil
}

func formatVerificationNotes(verification *BatchVerificationResult) string {
	var sb strings.Builder

	sb.WriteString(fmt.Sprintf("Verification Summary: %d claims checked\n", verification.TotalClaims))
	sb.WriteString(fmt.Sprintf("- True: %d, False: %d, Uncertain: %d, Errors: %d\n\n",
		verification.TrueCount, verification.FalseCount, verification.UncertainCount, verification.ErrorCount))

	sb.WriteString("Individual Results:\n")
	for _, vr := range verification.Results {
		sb.WriteString(fmt.Sprintf("- Claim [%s]: \"%s\"\n", vr.ClaimID, truncate(vr.Claim, 80)))
		sb.WriteString(fmt.Sprintf("  Verdict: %s | Evidence: %s\n", vr.Verdict, truncate(vr.Evidence, 100)))
		if vr.Error != "" {
			sb.WriteString(fmt.Sprintf("  Error: %s\n", vr.Error))
		}
	}

	return sb.String()
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
}

func parseCorrections(correctionsStr string) []Correction {
	var corrections []Correction

	lines := strings.Split(correctionsStr, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		if strings.Contains(strings.ToLower(line), "no corrections") {
			continue
		}

		corrections = append(corrections, Correction{
			Original:  line,
			Corrected: line,
			Reason:    "extracted from refinement",
		})
	}

	return corrections
}

type refinerOutput struct {
	FinalAnswer    string          `json:"final_answer"`
	CorrectionsRaw json.RawMessage `json:"corrections_made"`
}

func parseRefinerOutput(raw string, mode CoVeMode) (string, []Correction, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", nil, fmt.Errorf("refiner output is empty")
	}

	var parsed refinerOutput
	if err := decodeJSONFragment(raw, &parsed); err != nil {
		return "", nil, fmt.Errorf("refiner output must be a JSON object with final_answer and corrections_made: %w", err)
	}

	finalAnswer := strings.TrimSpace(parsed.FinalAnswer)
	if finalAnswer == "" {
		return "", nil, fmt.Errorf("refiner output missing non-empty final_answer")
	}
	if len(parsed.CorrectionsRaw) == 0 {
		return "", nil, fmt.Errorf("refiner output missing corrections_made")
	}

	corrections, err := parseRefinerCorrections(parsed.CorrectionsRaw)
	if err != nil {
		return "", nil, err
	}
	if mode == CoVeModeGate {
		return finalAnswer, nil, nil
	}
	return finalAnswer, corrections, nil
}

func parseRefinerCorrections(raw json.RawMessage) ([]Correction, error) {
	var decoded any
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return nil, fmt.Errorf("refiner corrections_made is malformed: %w", err)
	}

	switch v := decoded.(type) {
	case nil:
		return nil, fmt.Errorf("refiner output corrections_made must not be null")
	case string:
		return parseCorrections(v), nil
	case []any:
		lines := make([]string, 0, len(v))
		for _, item := range v {
			lines = append(lines, fmt.Sprintf("%v", item))
		}
		return parseCorrections(strings.Join(lines, "\n")), nil
	default:
		return parseCorrections(fmt.Sprintf("%v", v)), nil
	}
}

func decodeJSONFragment(raw string, dst any) error {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return fmt.Errorf("empty input")
	}
	if err := json.Unmarshal([]byte(raw), dst); err == nil {
		return nil
	}
	start := strings.Index(raw, "{")
	end := strings.LastIndex(raw, "}")
	if start == -1 || end <= start {
		return fmt.Errorf("no json object found")
	}
	return json.Unmarshal([]byte(raw[start:end+1]), dst)
}

// RunQuick is a convenience method for quick verification with minimal config.
func (c *CoVe) RunQuick(ctx context.Context, question string) (*CoVeResponse, error) {
	return c.Run(ctx, CoVeRequest{
		Question: question,
	})
}

// VerifyOnly runs only the verification phase (no baseline/refine) on provided claims.
func (c *CoVe) VerifyOnly(ctx context.Context, question string, claims []Claim) (*BatchVerificationResult, error) {
	return c.spawner.SpawnVerifiers(ctx, question, claims)
}
