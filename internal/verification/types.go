// Package verification implements the Draft-Accelerated Chain of Verification (CoVe) pattern.
//
// This package provides a hybrid verification architecture where:
//   - A Main Agent generates an initial baseline response
//   - Multiple Verification Agents check claims in parallel ("Draft" style - minimalist output)
//   - A Refiner Agent integrates verification results to produce a corrected final answer
//
// The "Draft" approach keeps verification agents lightweight by enforcing minimalist output
// format, enabling high-throughput parallel verification using Go's concurrency primitives.
//
// See: docs/designs/cove_verification.md for the full design.
package verification

import (
	"time"
)

// Verdict represents the verification outcome for a single claim.
type Verdict string

const (
	// VerdictTrue indicates the claim was verified as accurate.
	VerdictTrue Verdict = "True"
	// VerdictFalse indicates the claim was verified as inaccurate.
	VerdictFalse Verdict = "False"
	// VerdictUncertain indicates the claim could not be definitively verified.
	VerdictUncertain Verdict = "Uncertain"
)

// Claim represents a single verifiable statement extracted from a baseline response.
type Claim struct {
	// ID is a unique identifier for this claim within the verification session.
	ID string `json:"id"`

	// Text is the actual claim statement to verify.
	Text string `json:"text"`

	// SourceSpan optionally references the position in the baseline response.
	SourceSpan *TextSpan `json:"source_span,omitempty"`

	// Category optionally categorizes the claim (e.g., "factual", "numerical", "temporal").
	Category string `json:"category,omitempty"`
}

// TextSpan references a portion of text in the source document.
type TextSpan struct {
	Start int `json:"start"`
	End   int `json:"end"`
}

// VerificationResult holds the outcome of verifying a single claim.
type VerificationResult struct {
	// ClaimID references the claim that was verified.
	ClaimID string `json:"claim_id"`

	// Claim is the original claim text.
	Claim string `json:"claim"`

	// Verdict is the verification outcome.
	Verdict Verdict `json:"verdict"`

	// Evidence is the supporting data/source for the verdict.
	// Format: concise reference to the data source used.
	Evidence string `json:"evidence"`

	// RawOutput is the full "Draft" output from the verifier.
	// Format: "Source: [evidence] -> Verdict: [True/False/Uncertain]"
	RawOutput string `json:"raw_output"`

	// Duration is how long the verification took.
	Duration time.Duration `json:"duration"`

	// Error holds any error that occurred during verification.
	Error string `json:"error,omitempty"`
}

// IsValid returns true if the verification completed without error.
func (r VerificationResult) IsValid() bool {
	return r.Error == ""
}

// BatchVerificationResult holds the results of verifying multiple claims in parallel.
type BatchVerificationResult struct {
	// Results contains individual verification outcomes, indexed by claim ID.
	Results map[string]VerificationResult `json:"results"`

	// TotalClaims is the number of claims submitted for verification.
	TotalClaims int `json:"total_claims"`

	// VerifiedCount is the number of claims successfully verified.
	VerifiedCount int `json:"verified_count"`

	// TrueCount is the number of claims verified as True.
	TrueCount int `json:"true_count"`

	// FalseCount is the number of claims verified as False.
	FalseCount int `json:"false_count"`

	// UncertainCount is the number of claims with uncertain verdicts.
	UncertainCount int `json:"uncertain_count"`

	// ErrorCount is the number of claims that failed verification.
	ErrorCount int `json:"error_count"`

	// TotalDuration is the wall-clock time for the entire batch.
	TotalDuration time.Duration `json:"total_duration"`

	// Parallelism is the number of concurrent verifiers used.
	Parallelism int `json:"parallelism"`
}

// Summary returns a human-readable summary of the batch results.
func (b BatchVerificationResult) Summary() string {
	return ""
}

type CoVeMode string

const (
	CoVeModeDefault CoVeMode = ""
	CoVeModeGate    CoVeMode = "gate"
)

// CoVeRequest is the input for a complete Chain of Verification run.
type CoVeRequest struct {
	// Question is the original user query.
	Question string `json:"question"`

	// Context provides additional context for answering the question.
	Context map[string]any `json:"context,omitempty"`

	// MaxVerifiers limits the number of parallel verification agents.
	// Default: 10
	MaxVerifiers int `json:"max_verifiers,omitempty"`

	// VerificationTimeout is the timeout for each individual verification.
	// Default: 30s
	VerificationTimeout time.Duration `json:"verification_timeout,omitempty"`

	// SkipRefine if true, returns results after verification without refinement.
	SkipRefine bool `json:"skip_refine,omitempty"`

	Mode CoVeMode `json:"mode,omitempty"`
}

// CoVeResponse is the output of a complete Chain of Verification run.
type CoVeResponse struct {
	// Question is the original question.
	Question string `json:"question"`

	// BaselineResponse is the initial response from the Main Agent.
	BaselineResponse string `json:"baseline_response"`

	// Claims are the claims extracted from the baseline.
	Claims []Claim `json:"claims"`

	// Verification holds the batch verification results.
	Verification BatchVerificationResult `json:"verification"`

	// FinalAnswer is the refined answer incorporating verification results.
	// Empty if SkipRefine was true.
	FinalAnswer string `json:"final_answer,omitempty"`

	// Corrections lists specific corrections made during refinement.
	Corrections []Correction `json:"corrections,omitempty"`

	// Metrics contains timing and resource usage information.
	Metrics CoVeMetrics `json:"metrics"`
}

// Correction represents a change made during the refinement phase.
type Correction struct {
	// ClaimID references the claim that was corrected.
	ClaimID string `json:"claim_id"`

	// Original is the original (incorrect) claim text.
	Original string `json:"original"`

	// Corrected is the corrected claim text.
	Corrected string `json:"corrected"`

	// Reason explains why the correction was made.
	Reason string `json:"reason"`
}

// CoVeMetrics captures performance metrics for the verification run.
type CoVeMetrics struct {
	// BaselineDuration is how long the baseline generation took.
	BaselineDuration time.Duration `json:"baseline_duration"`

	// ExtractionDuration is how long claim extraction took.
	ExtractionDuration time.Duration `json:"extraction_duration"`

	// VerificationDuration is the wall-clock time for all verifications.
	VerificationDuration time.Duration `json:"verification_duration"`

	// RefinementDuration is how long the refinement took.
	RefinementDuration time.Duration `json:"refinement_duration"`

	// TotalDuration is the end-to-end time.
	TotalDuration time.Duration `json:"total_duration"`

	// TokensUsed is an estimate of total tokens consumed (if available).
	TokensUsed int `json:"tokens_used,omitempty"`
}

// VerifierConfig configures a single verification agent.
type VerifierConfig struct {
	// ID is the unique identifier for this verifier instance.
	ID string

	// Timeout is the maximum time for this verifier to complete.
	Timeout time.Duration

	// RetryCount is the number of retries on transient failures.
	RetryCount int
}

// SpawnerConfig configures the parallel verification spawner.
type SpawnerConfig struct {
	// MaxWorkers is the maximum number of concurrent verification agents.
	MaxWorkers int

	// DefaultTimeout is the default timeout per verification.
	DefaultTimeout time.Duration

	// QueueSize is the buffer size for the claims channel.
	// Default: 2 * MaxWorkers
	QueueSize int
}

// DefaultSpawnerConfig returns a SpawnerConfig with sensible defaults.
func DefaultSpawnerConfig() SpawnerConfig {
	return SpawnerConfig{
		MaxWorkers:     10,
		DefaultTimeout: 30 * time.Second,
		QueueSize:      20,
	}
}
