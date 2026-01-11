package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	dspycore "github.com/XiaoConstantine/dspy-go/pkg/core"
	"github.com/XiaoConstantine/dspy-go/pkg/llms"

	"github.com/jkatigb/agentctl/internal/adapters/skillslib/skillmain"
	"github.com/jkatigb/agentctl/internal/adapters/skillslib/skillout"
	"github.com/jkatigb/agentctl/internal/domain/envelope"
	"github.com/jkatigb/agentctl/internal/verification"
)

const command = "verification/cove_verify"

type llmConfig struct {
	Provider string `json:"provider,omitempty"`
	Model    string `json:"model,omitempty"`
}

type timeouts struct {
	BaselineMS     int `json:"baseline_ms,omitempty"`
	ExtractionMS   int `json:"extraction_ms,omitempty"`
	VerificationMS int `json:"verification_ms,omitempty"`
	RefinementMS   int `json:"refinement_ms,omitempty"`
}

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

type output struct {
	Summary summary `json:"summary"`

	Result   *result           `json:"result,omitempty"`
	Preview  *resultPreview    `json:"preview,omitempty"`
	Artifact string            `json:"artifact,omitempty"`
	CASHint  *envelope.CASHint `json:"cas_hint,omitempty"`
}

type resultPreview struct {
	FinalAnswer string            `json:"final_answer,omitempty"`
	Claims      []claimOut        `json:"claims,omitempty"`
	Results     []verifyResultOut `json:"results,omitempty"`
}

type result struct {
	Question         string          `json:"question"`
	BaselineResponse string          `json:"baseline_response"`
	Claims           []claimOut      `json:"claims"`
	Verification     batchOut        `json:"verification"`
	FinalAnswer      string          `json:"final_answer,omitempty"`
	Corrections      []correctionOut `json:"corrections,omitempty"`
	Metrics          metricsOut      `json:"metrics"`
}

type claimOut struct {
	ID       string `json:"id"`
	Text     string `json:"text"`
	Category string `json:"category,omitempty"`
}

type correctionOut struct {
	ClaimID   string `json:"claim_id,omitempty"`
	Original  string `json:"original"`
	Corrected string `json:"corrected"`
	Reason    string `json:"reason"`
}

type verifyResultOut struct {
	ClaimID    string `json:"claim_id"`
	Claim      string `json:"claim"`
	Verdict    string `json:"verdict"`
	Evidence   string `json:"evidence,omitempty"`
	RawOutput  string `json:"raw_output,omitempty"`
	DurationMS int64  `json:"duration_ms"`
	Error      string `json:"error,omitempty"`
}

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

type metricsOut struct {
	BaselineMS     int64 `json:"baseline_ms"`
	ExtractionMS   int64 `json:"extraction_ms"`
	VerificationMS int64 `json:"verification_ms"`
	RefinementMS   int64 `json:"refinement_ms"`
	TotalMS        int64 `json:"total_ms"`
}

func main() {
	skillmain.Main(command, run)
}

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
	if usedBaseline {
		resp, err = cove.RunFromBaseline(ctx, req, in.Baseline)
	} else {
		resp, err = cove.Run(ctx, req)
	}
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
		artifact, err := skillmain.PersistBuffer(ctx, rc, bytes.NewBuffer(payload), "application/json", "cove_verify")
		if err != nil {
			return err
		}
		out.Artifact = artifact.Digest
		hint := skillout.BuildCASHint(artifact, 200)
		out.CASHint = &hint
		preview := buildPreview(res, rc.MaxPreview)
		out.Preview = &preview
	} else {
		out.Result = &res
	}

	return skillout.Emit(rc, command, out)
}

func resolveLLM(cfg *llmConfig) (dspycore.LLM, string, string, error) {
	llms.EnsureFactory()

	provider := ""
	model := ""
	if cfg != nil {
		provider = strings.TrimSpace(cfg.Provider)
		model = strings.TrimSpace(cfg.Model)
	}

	if provider == "" {
		provider = strings.TrimSpace(os.Getenv("AGENTCTL_LLM_PROVIDER"))
	}
	if provider == "" {
		switch {
		case os.Getenv("GROQ_API_KEY") != "":
			provider = "groq"
		case os.Getenv("OPENROUTER_API_KEY") != "":
			provider = "openrouter"
		case os.Getenv("GEMINI_API_KEY") != "":
			provider = "gemini"
		case os.Getenv("OPENAI_API_KEY") != "":
			provider = "openai"
		case os.Getenv("ANTHROPIC_API_KEY") != "":
			provider = "anthropic"
		default:
			return nil, "", "", fmt.Errorf("no LLM provider configured (set AGENTCTL_LLM_PROVIDER+AGENTCTL_LLM_API_KEY or provider-specific *_API_KEY)")
		}
	}

	if model == "" {
		model = strings.TrimSpace(os.Getenv("AGENTCTL_LLM_MODEL"))
	}
	if model == "" {
		switch provider {
		case "gemini":
			model = "gemini-2.0-flash"
		case "groq":
			model = "llama-3.1-8b-instant"
		case "openrouter":
			model = "meta-llama/llama-3.1-8b-instruct:free"
		case "openai":
			model = "gpt-4o-mini"
		case "anthropic":
			model = "claude-3-5-haiku-20241022"
		default:
			model = "llama-3.1-8b-instant"
		}
	}

	apiKey := strings.TrimSpace(os.Getenv("AGENTCTL_LLM_API_KEY"))
	if apiKey == "" {
		switch provider {
		case "gemini":
			apiKey = os.Getenv("GEMINI_API_KEY")
		case "groq":
			apiKey = os.Getenv("GROQ_API_KEY")
		case "openrouter":
			apiKey = os.Getenv("OPENROUTER_API_KEY")
		case "openai":
			apiKey = os.Getenv("OPENAI_API_KEY")
		case "anthropic":
			apiKey = os.Getenv("ANTHROPIC_API_KEY")
		}
	}
	if apiKey == "" {
		return nil, provider, model, fmt.Errorf("LLM API key not configured for provider %q", provider)
	}

	var llm dspycore.LLM
	var err error
	switch provider {
	case "gemini":
		llm, err = llms.NewGeminiLLM(apiKey, dspycore.ModelID(model))
	case "openai":
		llm, err = llms.NewOpenAILLM(dspycore.ModelID(model), llms.WithAPIKey(apiKey))
	case "anthropic":
		llm, err = llms.NewAnthropicLLMFromConfig(context.Background(), dspycore.ProviderConfig{Name: "anthropic", APIKey: apiKey}, dspycore.ModelID(model))
	case "groq":
		llm, err = llms.NewOpenAICompatible("groq", dspycore.ModelID(model), "https://api.groq.com/openai/v1", llms.WithAPIKey(apiKey))
	case "openrouter":
		llm, err = llms.NewOpenAICompatible("openrouter", dspycore.ModelID(model), "https://openrouter.ai/api/v1", llms.WithAPIKey(apiKey))
	default:
		return nil, provider, model, fmt.Errorf("unsupported LLM provider: %q", provider)
	}
	if err != nil {
		return nil, provider, model, fmt.Errorf("create %s LLM: %w", provider, err)
	}

	return llm, provider, model, nil
}

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

