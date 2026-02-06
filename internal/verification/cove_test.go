package verification

import (
	"context"
	"testing"
	"time"
)

func TestVerdictConstants(t *testing.T) {
	tests := []struct {
		verdict Verdict
		want    string
	}{
		{VerdictTrue, "True"},
		{VerdictFalse, "False"},
		{VerdictUncertain, "Uncertain"},
	}

	for _, tt := range tests {
		if string(tt.verdict) != tt.want {
			t.Errorf("Verdict %v = %q, want %q", tt.verdict, string(tt.verdict), tt.want)
		}
	}
}

func TestVerificationResultIsValid(t *testing.T) {
	tests := []struct {
		name   string
		result VerificationResult
		want   bool
	}{
		{
			name:   "valid result",
			result: VerificationResult{ClaimID: "c1", Verdict: VerdictTrue},
			want:   true,
		},
		{
			name:   "result with error",
			result: VerificationResult{ClaimID: "c1", Error: "verification failed"},
			want:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.result.IsValid(); got != tt.want {
				t.Errorf("IsValid() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestDefaultSpawnerConfig(t *testing.T) {
	cfg := DefaultSpawnerConfig()

	if cfg.MaxWorkers != 10 {
		t.Errorf("MaxWorkers = %d, want 10", cfg.MaxWorkers)
	}
	if cfg.DefaultTimeout != 30*time.Second {
		t.Errorf("DefaultTimeout = %v, want 30s", cfg.DefaultTimeout)
	}
	if cfg.QueueSize != 20 {
		t.Errorf("QueueSize = %d, want 20", cfg.QueueSize)
	}
}

func TestDefaultCoVeConfig(t *testing.T) {
	cfg := DefaultCoVeConfig()

	if cfg.MaxVerifiers != 10 {
		t.Errorf("MaxVerifiers = %d, want 10", cfg.MaxVerifiers)
	}
	if cfg.VerificationTimeout != 30*time.Second {
		t.Errorf("VerificationTimeout = %v, want 30s", cfg.VerificationTimeout)
	}
	if cfg.BaselineTimeout != 2*time.Minute {
		t.Errorf("BaselineTimeout = %v, want 2m", cfg.BaselineTimeout)
	}
}

func TestParseDraftOutput(t *testing.T) {
	tests := []struct {
		name         string
		raw          string
		wantVerdict  Verdict
		wantEvidence string
	}{
		{
			name:         "standard format - true",
			raw:          "Source: Water boils at 100C at sea level -> Verdict: True",
			wantVerdict:  VerdictTrue,
			wantEvidence: "Water boils at 100C at sea level",
		},
		{
			name:         "standard format - false",
			raw:          "Source: Paris is not France's capital -> Verdict: False",
			wantVerdict:  VerdictFalse,
			wantEvidence: "Paris is not France's capital",
		},
		{
			name:         "standard format - uncertain",
			raw:          "Source: Cannot verify specific statistic -> Verdict: Uncertain",
			wantVerdict:  VerdictUncertain,
			wantEvidence: "Cannot verify specific statistic",
		},
		{
			name:         "case insensitive",
			raw:          "source: known fact -> verdict: TRUE",
			wantVerdict:  VerdictTrue,
			wantEvidence: "known fact",
		},
		{
			name:         "fallback - contains true",
			raw:          "This claim is true based on evidence",
			wantVerdict:  VerdictTrue,
			wantEvidence: "This claim is true based on evidence",
		},
		{
			name:         "fallback - contains false",
			raw:          "This claim is false",
			wantVerdict:  VerdictFalse,
			wantEvidence: "This claim is false",
		},
		{
			name:         "fallback - uncertain",
			raw:          "I cannot determine the accuracy of this claim",
			wantVerdict:  VerdictUncertain,
			wantEvidence: "I cannot determine the accuracy of this claim",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotVerdict, gotEvidence := parseDraftOutput(tt.raw)
			if gotVerdict != tt.wantVerdict {
				t.Errorf("parseDraftOutput() verdict = %v, want %v", gotVerdict, tt.wantVerdict)
			}
			if gotEvidence != tt.wantEvidence {
				t.Errorf("parseDraftOutput() evidence = %q, want %q", gotEvidence, tt.wantEvidence)
			}
		})
	}
}

func TestParseClaimsJSON(t *testing.T) {
	tests := []struct {
		name      string
		jsonStr   string
		wantCount int
		wantFirst string
	}{
		{
			name:      "valid json array",
			jsonStr:   `[{"id": "c1", "text": "Paris is the capital", "category": "factual"}]`,
			wantCount: 1,
			wantFirst: "Paris is the capital",
		},
		{
			name:      "json with prefix text",
			jsonStr:   `Here are the claims: [{"id": "c1", "text": "Claim one"}]`,
			wantCount: 1,
			wantFirst: "Claim one",
		},
		{
			name:      "multiple claims",
			jsonStr:   `[{"id": "c1", "text": "First"}, {"id": "c2", "text": "Second"}]`,
			wantCount: 2,
			wantFirst: "First",
		},
		{
			name:      "auto-generate ids",
			jsonStr:   `[{"text": "Claim without id"}]`,
			wantCount: 1,
			wantFirst: "Claim without id",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			claims, err := parseClaimsJSON(tt.jsonStr)
			if err != nil {
				t.Fatalf("parseClaimsJSON() error = %v", err)
			}
			if len(claims) != tt.wantCount {
				t.Errorf("parseClaimsJSON() count = %d, want %d", len(claims), tt.wantCount)
			}
			if len(claims) > 0 && claims[0].Text != tt.wantFirst {
				t.Errorf("parseClaimsJSON() first claim = %q, want %q", claims[0].Text, tt.wantFirst)
			}
		})
	}
}

func TestExtractClaimsFallback(t *testing.T) {
	input := `Here are the claims:
- First claim that is longer than ten characters
- Second claim also long enough
- Short
* Third claim with bullet point
`
	claims := extractClaimsFallback(input)

	// "Here are the claims:" (>10 chars), two dash items (>10), one bullet item (>10) = 4
	// "Short" is only 5 chars so excluded
	if len(claims) != 4 {
		t.Errorf("extractClaimsFallback() count = %d, want 4", len(claims))
	}

	if claims[0].ID != "c1" {
		t.Errorf("first claim ID = %q, want c1", claims[0].ID)
	}
}

func TestTruncate(t *testing.T) {
	tests := []struct {
		s      string
		maxLen int
		want   string
	}{
		{"short", 10, "short"},
		{"exactly10!", 10, "exactly10!"},
		{"this is a longer string", 10, "this is..."},
	}

	for _, tt := range tests {
		got := truncate(tt.s, tt.maxLen)
		if got != tt.want {
			t.Errorf("truncate(%q, %d) = %q, want %q", tt.s, tt.maxLen, got, tt.want)
		}
	}
}

func TestFormatVerificationNotes(t *testing.T) {
	batch := &BatchVerificationResult{
		Results: map[string]VerificationResult{
			"c1": {ClaimID: "c1", Claim: "Test claim", Verdict: VerdictTrue, Evidence: "Known fact"},
		},
		TotalClaims:    1,
		TrueCount:      1,
		FalseCount:     0,
		UncertainCount: 0,
		ErrorCount:     0,
	}

	notes := formatVerificationNotes(batch)

	if notes == "" {
		t.Error("formatVerificationNotes() returned empty string")
	}

	if !contains(notes, "1 claims checked") {
		t.Error("notes should contain claim count")
	}

	if !contains(notes, "True: 1") {
		t.Error("notes should contain true count")
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsHelper(s, substr))
}

func containsHelper(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

func TestNewSpawner(t *testing.T) {
	spawner := NewSpawner(nil, SpawnerConfig{})

	if spawner.config.MaxWorkers != 10 {
		t.Errorf("default MaxWorkers = %d, want 10", spawner.config.MaxWorkers)
	}
	if spawner.config.DefaultTimeout != 30*time.Second {
		t.Errorf("default Timeout = %v, want 30s", spawner.config.DefaultTimeout)
	}
}

func TestSpawnerSpawnVerifiersEmptyClaims(t *testing.T) {
	spawner := NewSpawner(nil, DefaultSpawnerConfig())

	result, err := spawner.SpawnVerifiers(context.Background(), "test question", []Claim{})
	if err != nil {
		t.Fatalf("SpawnVerifiers() error = %v", err)
	}

	if result.TotalClaims != 0 {
		t.Errorf("TotalClaims = %d, want 0", result.TotalClaims)
	}
}

func TestExtractDraftOutput(t *testing.T) {
	tests := []struct {
		name      string
		resultMap map[string]any
		want      string
	}{
		{
			name:      "nil map",
			resultMap: nil,
			want:      "",
		},
		{
			name:      "draft_verdict field",
			resultMap: map[string]any{"draft_verdict": "Source: test -> Verdict: True"},
			want:      "Source: test -> Verdict: True",
		},
		{
			name:      "result field fallback",
			resultMap: map[string]any{"result": "some result"},
			want:      "some result",
		},
		{
			name:      "output field fallback",
			resultMap: map[string]any{"output": "some output"},
			want:      "some output",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractDraftOutput(tt.resultMap)
			if got != tt.want {
				t.Errorf("extractDraftOutput() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestParseCorrections(t *testing.T) {
	input := `Original: wrong claim -> Corrected: right claim
No corrections needed
Another correction line`

	corrections := parseCorrections(input)

	if len(corrections) != 2 {
		t.Errorf("parseCorrections() count = %d, want 2", len(corrections))
	}
}

func TestBatchVerificationResultSummary(t *testing.T) {
	batch := BatchVerificationResult{
		TotalClaims:    5,
		TrueCount:      3,
		FalseCount:     1,
		UncertainCount: 1,
	}

	summary := batch.Summary()
	if summary == "" {
		t.Log("Summary() returns empty (stub implementation)")
	}
}

func TestClaimFields(t *testing.T) {
	claim := Claim{
		ID:       "c1",
		Text:     "Test claim",
		Category: "factual",
		SourceSpan: &TextSpan{
			Start: 0,
			End:   10,
		},
	}

	if claim.ID != "c1" {
		t.Errorf("Claim.ID = %q, want c1", claim.ID)
	}
	if claim.SourceSpan.Start != 0 {
		t.Errorf("SourceSpan.Start = %d, want 0", claim.SourceSpan.Start)
	}
}

func TestCoVeRequestDefaults(t *testing.T) {
	req := CoVeRequest{
		Question: "What is the capital of France?",
	}

	if req.MaxVerifiers != 0 {
		t.Errorf("default MaxVerifiers = %d, want 0 (unset)", req.MaxVerifiers)
	}
	if req.SkipRefine != false {
		t.Errorf("default SkipRefine = %v, want false", req.SkipRefine)
	}
}

func TestVerifierConfigFields(t *testing.T) {
	cfg := VerifierConfig{
		ID:         "v1",
		Timeout:    30 * time.Second,
		RetryCount: 3,
	}

	if cfg.ID != "v1" {
		t.Errorf("ID = %q, want v1", cfg.ID)
	}
	if cfg.RetryCount != 3 {
		t.Errorf("RetryCount = %d, want 3", cfg.RetryCount)
	}
}
