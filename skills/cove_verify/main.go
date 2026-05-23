package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/joshka0/foxctl/internal/adapters/skillslib/skillmain"
	"github.com/joshka0/foxctl/internal/adapters/skillslib/skillout"
	"github.com/joshka0/foxctl/internal/domain/envelope"
	"github.com/joshka0/foxctl/internal/intelligence/verification"
	"github.com/joshka0/foxctl/internal/platform/env"
	llmproviders "github.com/joshka0/foxctl/internal/providers/llm"
)

const command = "verification/cove_verify"

// llmConfig configures the LLM provider for verification operations.
type llmConfig struct {
	Provider string `json:"provider,omitempty"`
	Model    string `json:"model,omitempty"`
	BaseURL  string `json:"base_url,omitempty"`
	AuthMode string `json:"auth_mode,omitempty"`
}

// timeouts defines operation timeouts for different verification phases.
type timeouts struct {
	BaselineMS     int `json:"baseline_ms,omitempty"`
	ExtractionMS   int `json:"extraction_ms,omitempty"`
	VerificationMS int `json:"verification_ms,omitempty"`
	RefinementMS   int `json:"refinement_ms,omitempty"`
}

// input is the expected JSON input for verification/cove_verify operations.
type input struct {
	Question     string         `json:"question"`
	Baseline     string         `json:"baseline,omitempty"`
	Context      map[string]any `json:"context,omitempty"`
	MaxVerifiers int            `json:"max_verifiers,omitempty"`
	SkipRefine   bool           `json:"skip_refine,omitempty"`
	Mode         string         `json:"mode,omitempty"`
	Timeouts     *timeouts      `json:"timeouts,omitempty"`
	LLM          *llmConfig     `json:"llm,omitempty"`
}

// summary provides high-level metrics for the verification process.
type summary struct {
	Provider     string `json:"provider"`
	Model        string `json:"model"`
	UsedBaseline bool   `json:"used_baseline"`

	Claims     int   `json:"claims"`
	Verified   int   `json:"verified"`
	TrueCount  int   `json:"true"`
	FalseCount int   `json:"false"`
	Uncertain  int   `json:"uncertain"`
	Errors     int   `json:"errors"`
	DurationMS int64 `json:"duration_ms"`
}

// output contains the verification results with optional artifact storage.
type output struct {
	Summary summary `json:"summary"`

	Result   *result           `json:"result,omitempty"`
	Preview  *resultPreview    `json:"preview,omitempty"`
	Artifact string            `json:"artifact,omitempty"`
	CASHint  *envelope.CASHint `json:"cas_hint,omitempty"`
}

// resultPreview provides a truncated view of verification results.
type resultPreview struct {
	FinalAnswer string            `json:"final_answer,omitempty"`
	Claims      []claimOut        `json:"claims,omitempty"`
	Results     []verifyResultOut `json:"results,omitempty"`
}

// result contains the complete verification results.
type result struct {
	Question         string          `json:"question"`
	BaselineResponse string          `json:"baseline_response"`
	Claims           []claimOut      `json:"claims"`
	Verification     batchOut        `json:"verification"`
	FinalAnswer      string          `json:"final_answer,omitempty"`
	Corrections      []correctionOut `json:"corrections,omitempty"`
	Metrics          metricsOut      `json:"metrics"`
}

// claimOut represents a claim extracted from the baseline response.
type claimOut struct {
	ID       string `json:"id"`
	Text     string `json:"text"`
	Category string `json:"category,omitempty"`
}

// correctionOut represents a correction made during refinement.
type correctionOut struct {
	ClaimID   string `json:"claim_id,omitempty"`
	Original  string `json:"original"`
	Corrected string `json:"corrected"`
	Reason    string `json:"reason"`
}

// verifyResultOut contains the result of verifying a single claim.
type verifyResultOut struct {
	ClaimID    string `json:"claim_id"`
	Claim      string `json:"claim"`
	Verdict    string `json:"verdict"`
	Evidence   string `json:"evidence,omitempty"`
	RawOutput  string `json:"raw_output,omitempty"`
	DurationMS int64  `json:"duration_ms"`
	Error      string `json:"error,omitempty"`
}

// batchOut contains aggregated results from batch verification.
type batchOut struct {
	Results         map[string]verifyResultOut `json:"results"`
	TotalClaims     int                        `json:"total_claims"`
	VerifiedCount   int                        `json:"verified_count"`
	TrueCount       int                        `json:"true_count"`
	FalseCount      int                        `json:"false_count"`
	UncertainCount  int                        `json:"uncertain_count"`
	ErrorCount      int                        `json:"error_count"`
	TotalDurationMS int64                      `json:"total_duration_ms"`
	Parallelism     int                        `json:"parallelism"`
}

// metricsOut provides timing metrics for each verification phase.
type metricsOut struct {
	BaselineMS     int64 `json:"baseline_ms"`
	ExtractionMS   int64 `json:"extraction_ms"`
	VerificationMS int64 `json:"verification_ms"`
	RefinementMS   int64 `json:"refinement_ms"`
	TotalMS        int64 `json:"total_ms"`
}

// main is the skill entry point for verification/cove_verify.
func main() {
	skillmain.Main(command, skillmain.Chain(
		run,
		skillmain.WithRecover[input](),
	))
}

// run orchestrates claim verification using the CoVE (Chain of Verification) method.
//
// Index:
//
//	Purpose: Verify factual claims in responses using multi-step verification with parallel claim checking
//	Flow: validate input → resolve LLM → configure CoVE → extract claims → verify in parallel → optionally refine → emit results
//	SideEffects: LLM API calls; parallel verification; artifact storage for large results; timing measurements
//	FailureModes: invalid input, LLM errors, verification failures, timeout errors, storage errors
//	Observability: emits verification metrics, claim results, corrections, and timing breakdown
//	Related: resolveLLM, convertResult, buildPreview
//	Keywords: verification/cove_verify, CoVE, claim_verification, fact_checking, LLM
//
// [[domain:llm-claim-verification]]
// [[protocol:chain-of-verification]]
func run(ctx context.Context, rc *skillmain.RunContext, in input) error {
	// Validate input
	if strings.TrimSpace(in.Question) == "" {
		return fmt.Errorf("question is required")
	}
	if in.MaxVerifiers <= 0 {
		in.MaxVerifiers = 6
	}
	in.Mode = strings.ToLower(strings.TrimSpace(in.Mode))
	if in.Mode != "" && in.Mode != string(verification.CoVeModeGate) {
		return fmt.Errorf("invalid mode %q (supported: %q)", in.Mode, string(verification.CoVeModeGate))
	}
	llm, provider, model, err := resolveLLM(in.LLM)
	if err != nil {
		return err
	}

	cfg := verification.DefaultCoVeConfig()
	cfg.MaxVerifiers = in.MaxVerifiers
	if in.Timeouts != nil {
		if in.Timeouts.BaselineMS > 0 {
			cfg.BaselineTimeout = time.Duration(in.Timeouts.BaselineMS) * time.Millisecond
		}
		if in.Timeouts.ExtractionMS > 0 {
			cfg.ExtractionTimeout = time.Duration(in.Timeouts.ExtractionMS) * time.Millisecond
		}
		if in.Timeouts.VerificationMS > 0 {
			cfg.VerificationTimeout = time.Duration(in.Timeouts.VerificationMS) * time.Millisecond
		}
		if in.Timeouts.RefinementMS > 0 {
			cfg.RefinementTimeout = time.Duration(in.Timeouts.RefinementMS) * time.Millisecond
		}
	}

	cove := verification.NewCoVe(llm, cfg)
	req := verification.CoVeRequest{
		Question:            in.Question,
		Context:             in.Context,
		MaxVerifiers:        in.MaxVerifiers,
		VerificationTimeout: cfg.VerificationTimeout,
		SkipRefine:          in.SkipRefine,
		Mode:                verification.CoVeMode(strings.TrimSpace(in.Mode)),
	}

	var resp *verification.CoVeResponse
	usedBaseline := strings.TrimSpace(in.Baseline) != ""
	err = skillmain.GuardCall(rc, skillmain.BreakerLLMProvider, ctx, func(ctx context.Context) error {
		var e error
		if usedBaseline {
			resp, e = cove.RunFromBaseline(ctx, req, in.Baseline)
		} else {
			resp, e = cove.Run(ctx, req)
		}
		return e
	})
	if err != nil {
		return err
	}

	res := convertResult(resp)
	out := output{
		Summary: summary{
			Provider:     provider,
			Model:        model,
			UsedBaseline: usedBaseline,
			Claims:       len(res.Claims),
			Verified:     res.Verification.VerifiedCount,
			TrueCount:    res.Verification.TrueCount,
			FalseCount:   res.Verification.FalseCount,
			Uncertain:    res.Verification.UncertainCount,
			Errors:       res.Verification.ErrorCount,
			DurationMS:   res.Metrics.TotalMS,
		},
	}

	payload, err := json.Marshal(res)
	if err != nil {
		return fmt.Errorf("marshal result: %w", err)
	}

	if rc.ShouldTruncate(len(payload)) {
		artifact, hint, err := skillout.PersistBufferWithHint(ctx, rc, bytes.NewBuffer(payload), "application/json", "cove_verify", 200)
		if err != nil {
			return err
		}
		out.Artifact = artifact.Digest
		out.CASHint = hint
		preview := buildPreview(res, rc.MaxPreview)
		out.Preview = &preview
	} else {
		out.Result = &res
	}

	return skillout.Emit(rc, command, out)
}

// resolveLLM creates and configures the LLM client based on provider settings.
func resolveLLM(cfg *llmConfig) (verification.LLMClient, string, string, error) {
	provider := ""
	model := ""
	baseURL := ""
	authMode := ""
	if cfg != nil {
		provider = strings.ToLower(strings.TrimSpace(cfg.Provider))
		model = strings.TrimSpace(cfg.Model)
		baseURL = strings.TrimSpace(cfg.BaseURL)
		authMode = strings.TrimSpace(cfg.AuthMode)
	}

	if provider == "" {
		provider = strings.ToLower(strings.TrimSpace(env.GetString("FOXCTL_LLM_PROVIDER")))
	}
	if provider == "" {
		switch {
		case env.GetString("LMSTUDIO_BASE_URL") != "" || env.GetString("LMSTUDIO_MODEL") != "":
			provider = "lmstudio"
		case env.GetString("CEREBRAS_API_KEY") != "":
			provider = "cerebras"
		case env.GetString("GROQ_API_KEY") != "":
			provider = "groq"
		case env.GetString("OPENROUTER_API_KEY") != "":
			provider = "openrouter"
		case env.GetString("OPENAI_API_KEY") != "":
			provider = "openai"
		default:
			return nil, "", "", fmt.Errorf("no LLM provider configured (set FOXCTL_LLM_PROVIDER+FOXCTL_LLM_API_KEY or provider-specific *_API_KEY)")
		}
	}

	if model == "" {
		model = env.GetString("FOXCTL_LLM_MODEL")
	}
	if model == "" && provider == "lmstudio" {
		model = env.GetString("LMSTUDIO_MODEL")
	}
	if model == "" {
		model = llmproviders.DefaultModelForProvider(provider)
	}
	if baseURL == "" {
		baseURL = env.GetString("FOXCTL_LLM_BASE_URL")
	}
	if baseURL == "" && provider == "lmstudio" {
		baseURL = env.GetString("LMSTUDIO_BASE_URL")
	}
	if authMode == "" {
		authMode = env.GetString("FOXCTL_LLM_AUTH_MODE")
	}

	apiKey := env.GetString("FOXCTL_LLM_API_KEY")
	if apiKey == "" {
		switch provider {
		case "cerebras":
			apiKey = env.GetString("CEREBRAS_API_KEY")
		case "groq":
			apiKey = env.GetString("GROQ_API_KEY")
		case "openrouter":
			apiKey = env.GetString("OPENROUTER_API_KEY")
		case "openai":
			apiKey = env.GetString("OPENAI_API_KEY")
		case "lmstudio":
			apiKey = env.GetString("LMSTUDIO_API_KEY")
		}
	}
	if apiKey == "" && !allowsNoAPIKey(provider, authMode) {
		return nil, provider, model, fmt.Errorf("LLM API key not configured for provider %q", provider)
	}

	client, err := verification.NewOpenAIClient(verification.OpenAIConfig{
		Provider: provider,
		BaseURL:  baseURL,
		APIKey:   apiKey,
		AuthMode: authMode,
		Model:    model,
	})
	if err != nil {
		return nil, provider, model, fmt.Errorf("create %s LLM client: %w", provider, err)
	}

	return client, provider, model, nil
}

func allowsNoAPIKey(provider, authMode string) bool {
	mode := strings.ToLower(strings.TrimSpace(authMode))
	if mode != "" && mode != "auto" {
		return mode == "none"
	}
	switch strings.ToLower(strings.TrimSpace(provider)) {
	case "lmstudio", "openai_compat", "openai-compatible":
		return true
	default:
		return false
	}
}

// convertResult converts internal verification response to output format.
func convertResult(resp *verification.CoVeResponse) result {
	claims := make([]claimOut, 0, len(resp.Claims))
	for _, c := range resp.Claims {
		claims = append(claims, claimOut{ID: c.ID, Text: c.Text, Category: c.Category})
	}

	results := make(map[string]verifyResultOut, len(resp.Verification.Results))
	for id, vr := range resp.Verification.Results {
		results[id] = verifyResultOut{
			ClaimID:    vr.ClaimID,
			Claim:      vr.Claim,
			Verdict:    string(vr.Verdict),
			Evidence:   vr.Evidence,
			RawOutput:  vr.RawOutput,
			DurationMS: vr.Duration.Milliseconds(),
			Error:      vr.Error,
		}
	}

	corrections := make([]correctionOut, 0, len(resp.Corrections))
	for _, c := range resp.Corrections {
		corrections = append(corrections, correctionOut{
			ClaimID:   c.ClaimID,
			Original:  c.Original,
			Corrected: c.Corrected,
			Reason:    c.Reason,
		})
	}

	return result{
		Question:         resp.Question,
		BaselineResponse: resp.BaselineResponse,
		Claims:           claims,
		Verification: batchOut{
			Results:         results,
			TotalClaims:     resp.Verification.TotalClaims,
			VerifiedCount:   resp.Verification.VerifiedCount,
			TrueCount:       resp.Verification.TrueCount,
			FalseCount:      resp.Verification.FalseCount,
			UncertainCount:  resp.Verification.UncertainCount,
			ErrorCount:      resp.Verification.ErrorCount,
			TotalDurationMS: resp.Verification.TotalDuration.Milliseconds(),
			Parallelism:     resp.Verification.Parallelism,
		},
		FinalAnswer: resp.FinalAnswer,
		Corrections: corrections,
		Metrics: metricsOut{
			BaselineMS:     resp.Metrics.BaselineDuration.Milliseconds(),
			ExtractionMS:   resp.Metrics.ExtractionDuration.Milliseconds(),
			VerificationMS: resp.Metrics.VerificationDuration.Milliseconds(),
			RefinementMS:   resp.Metrics.RefinementDuration.Milliseconds(),
			TotalMS:        resp.Metrics.TotalDuration.Milliseconds(),
		},
	}
}

// buildPreview creates a truncated preview of verification results.
func buildPreview(res result, maxItems int) resultPreview {
	claimsPreview := res.Claims
	if maxItems > 0 && len(claimsPreview) > maxItems {
		claimsPreview = claimsPreview[:maxItems]
	}

	var resultsPreview []verifyResultOut
	for _, c := range claimsPreview {
		if vr, ok := res.Verification.Results[c.ID]; ok {
			resultsPreview = append(resultsPreview, vr)
		}
	}

	return resultPreview{
		FinalAnswer: res.FinalAnswer,
		Claims:      claimsPreview,
		Results:     resultsPreview,
	}
}
