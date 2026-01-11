package verification

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/XiaoConstantine/dspy-go/pkg/core"
	"github.com/XiaoConstantine/dspy-go/pkg/modules"
)

// CoVe implements the Chain of Verification pattern with parallel "Draft" verification.
// It orchestrates the full verification pipeline: Baseline -> Extract -> Verify -> Refine.
type CoVe struct {
	llm     core.LLM
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
func NewCoVe(llm core.LLM, config CoVeConfig) *CoVe {
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

	sig := BuildBaselineSignature()
	predict := modules.NewPredict(*sig).WithTextOutput()
	predict.SetLLM(c.llm)

	input := map[string]any{
		"question": req.Question,
	}

	if req.Context != nil {
		input["context"] = req.Context
	}

	result, err := predict.Process(baselineCtx, input)
	if err != nil {
		return "", err
	}

	return extractStringResult(result, "baseline_response"), nil
}

func (c *CoVe) extractClaims(ctx context.Context, baseline string) ([]Claim, error) {
	extractCtx, cancel := context.WithTimeout(ctx, c.config.ExtractionTimeout)
	defer cancel()

	// Use Predict module for pure reasoning without tool calls.
	// WithTextOutput() ensures raw text extraction works correctly.
	sig := BuildClaimExtractorSignature()
	predict := modules.NewPredict(*sig).WithTextOutput()
	predict.SetLLM(c.llm)

	input := map[string]any{
		"text": baseline,
	}

	result, err := predict.Process(extractCtx, input)
	if err != nil {
		return nil, err
	}

	claimsJSON := extractStringResult(result, "claims")
	return parseClaimsJSON(claimsJSON)
}

func (c *CoVe) refine(ctx context.Context, question, baseline string, verification *BatchVerificationResult, mode CoVeMode) (string, []Correction, error) {
	refineCtx, cancel := context.WithTimeout(ctx, c.config.RefinementTimeout)
	defer cancel()

	sig := BuildRefinerSignature(mode)
	predict := modules.NewPredict(*sig).WithTextOutput()
	predict.SetLLM(c.llm)

	verificationNotes := formatVerificationNotes(verification)

	input := map[string]any{
		"question":           question,
		"baseline":           baseline,
		"verification_notes": verificationNotes,
	}

	result, err := predict.Process(refineCtx, input)
	if err != nil {
		return "", nil, err
	}

	finalAnswer := extractStringResult(result, "final_answer")
	correctionsStr := extractStringResult(result, "corrections_made")
	corrections := parseCorrections(correctionsStr)

	return finalAnswer, corrections, nil
}

func extractStringResult(result map[string]any, primaryKey string) string {
	if result == nil {
		return ""
	}

	if v, ok := result[primaryKey].(string); ok && v != "" {
		return v
	}

	for _, key := range []string{"result", "output", "answer", "thought"} {
		if v, ok := result[key].(string); ok && v != "" {
			return v
		}
	}

	return fmt.Sprintf("%v", result)
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
