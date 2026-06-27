package llm

import "testing"

func TestLimitsForModelKnownLongContextModels(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		provider    string
		model       string
		wantContext int
		wantOutput  int
	}{
		{name: "glm 5.2 direct", provider: "zai", model: "glm-5.2", wantContext: Context1M, wantOutput: Context128K},
		{name: "glm 5.2 one million alias", provider: "zai", model: "glm-5.2[1m]", wantContext: Context1M, wantOutput: Context128K},
		{name: "glm 5.2 openrouter namespace", provider: "openrouter", model: "z-ai/glm-5.2", wantContext: Context1M, wantOutput: Context128K},
		{name: "glm 5.1", provider: "zai", model: "glm-5.1", wantContext: Context200K, wantOutput: Context128K},
		{name: "minimax m3", provider: "minimax", model: "minimax-m3", wantContext: Context1M},
		{name: "minimax m3 openrouter namespace", provider: "openrouter", model: "minimax/minimax-m3", wantContext: Context1M},
		{name: "minimax m1", provider: "minimax", model: "minimax-m1", wantContext: Context1M},
		{name: "kimi k2.7 code", provider: "kimi", model: "kimi-k2.7-code", wantContext: Context256K},
		{name: "kimi k2.6 openrouter namespace", provider: "openrouter", model: "moonshotai/kimi-k2.6", wantContext: Context256K},
		{name: "moonshot v1 128k", provider: "moonshot", model: "moonshot-v1-128k", wantContext: Context128K},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			limits, ok := LimitsForModel(tt.provider, tt.model)
			if !ok {
				t.Fatalf("LimitsForModel(%q, %q) not found", tt.provider, tt.model)
			}
			if limits.ContextTokens != tt.wantContext {
				t.Fatalf("context=%d want %d", limits.ContextTokens, tt.wantContext)
			}
			if limits.MaxOutputTokens != tt.wantOutput {
				t.Fatalf("max_output=%d want %d", limits.MaxOutputTokens, tt.wantOutput)
			}
		})
	}
}

func TestLimitsForModelUnknown(t *testing.T) {
	t.Parallel()

	if limits, ok := LimitsForModel("openrouter", "unknown/model"); ok {
		t.Fatalf("unexpected limits: %+v", limits)
	}
	if got := ContextTokensForModel("openrouter", "unknown/model"); got != 0 {
		t.Fatalf("ContextTokensForModel unknown=%d want 0", got)
	}
}
