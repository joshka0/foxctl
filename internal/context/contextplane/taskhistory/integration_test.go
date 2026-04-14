package taskhistory

import (
	"testing"

	platformcfg "github.com/joshka0/foxctl/internal/platform/config"
)

func TestTranscriptSummaryWorkerConfig_PrefersOpenRouterWhenAvailable(t *testing.T) {
	cfg := platformcfg.Config{}
	cfg.LLM.OpenRouterAPIKey = "openrouter-key"
	cfg.LLM.OpenRouterModel = "google/gemini-3.1-flash-lite-preview"

	worker := TranscriptSummaryWorkerConfig(cfg, "", "")
	if worker == nil {
		t.Fatal("expected worker config")
		return
	}
	if worker.Provider != "openrouter" {
		t.Fatalf("provider=%q want openrouter", worker.Provider)
	}
	if worker.Model != "google/gemini-3.1-flash-lite-preview" {
		t.Fatalf("model=%q", worker.Model)
	}
}

func TestTranscriptSummaryWorkerConfig_FallsBackToLMStudio(t *testing.T) {
	cfg := platformcfg.Config{}
	cfg.LLM.Model = ""

	worker := TranscriptSummaryWorkerConfig(cfg, "", "")
	if worker == nil {
		t.Fatal("expected worker config")
		return
	}
	if worker.Provider != "lmstudio" {
		t.Fatalf("provider=%q want lmstudio", worker.Provider)
	}
}

func TestTranscriptSummaryWorkerConfig_UsesExplicitOverrides(t *testing.T) {
	t.Setenv("AGENTCTL_TRANSCRIPT_SUMMARY_PROVIDER", "openrouter")
	t.Setenv("AGENTCTL_TRANSCRIPT_SUMMARY_MODEL", "google/gemini-3.1-flash-lite-preview")

	cfg := platformcfg.Config{}
	cfg.LLM.OpenRouterAPIKey = "openrouter-key"

	worker := TranscriptSummaryWorkerConfig(cfg, "", "")
	if worker == nil {
		t.Fatal("expected worker config")
		return
	}
	if worker.Provider != "openrouter" {
		t.Fatalf("provider=%q want openrouter", worker.Provider)
	}
	if worker.Model != "google/gemini-3.1-flash-lite-preview" {
		t.Fatalf("model=%q", worker.Model)
	}
}

func TestTranscriptSummaryWorkerConfig_ExplicitArgsOverrideEnv(t *testing.T) {
	t.Setenv("AGENTCTL_TRANSCRIPT_SUMMARY_PROVIDER", "lmstudio")
	t.Setenv("AGENTCTL_TRANSCRIPT_SUMMARY_MODEL", "zai-org/glm-4.7-flash")

	cfg := platformcfg.Config{}
	cfg.LLM.OpenRouterAPIKey = "openrouter-key"

	worker := TranscriptSummaryWorkerConfig(cfg, "openrouter", "google/gemini-3.1-flash-lite-preview")
	if worker == nil {
		t.Fatal("expected worker config")
		return
	}
	if worker.Provider != "openrouter" {
		t.Fatalf("provider=%q want openrouter", worker.Provider)
	}
	if worker.Model != "google/gemini-3.1-flash-lite-preview" {
		t.Fatalf("model=%q", worker.Model)
	}
}
