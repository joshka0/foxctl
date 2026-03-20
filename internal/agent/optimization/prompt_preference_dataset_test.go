package optimization_test

import (
	"strings"
	"testing"

	"github.com/jkatigb/agentctl/internal/agent/optimization"
)

func TestParsePromptPreferenceDatasetJSONL(t *testing.T) {
	t.Parallel()

	body := strings.NewReader(`{"record_type":"prompt_preference","input":{"question":"Q"},"chosen":{"variant_id":"a","agent_role":"coder","mode":"gepa","prompt":"Chosen","mean_score":0.9,"worst_score":0.8,"pass_count":4},"rejected":{"variant_id":"b","agent_role":"coder","mode":"copro","prompt":"Rejected","mean_score":0.4,"worst_score":0.3,"pass_count":1},"metadata":{"run_id":"run-1","artifact_digest":"sha256:test","provider":"lmstudio"}}
`)

	examples, err := optimization.ParsePromptPreferenceDatasetJSONL(body)
	if err != nil {
		t.Fatalf("ParsePromptPreferenceDatasetJSONL: %v", err)
	}
	if len(examples) != 1 {
		t.Fatalf("len(examples)=%d want 1", len(examples))
	}
	if examples[0].Chosen.VariantID != "a" {
		t.Fatalf("chosen.variant_id=%q want a", examples[0].Chosen.VariantID)
	}
}

func TestParsePromptPreferenceDatasetJSONLLargeLine(t *testing.T) {
	t.Parallel()

	largePrompt := strings.Repeat("x", 70_000)
	body := strings.NewReader(`{"record_type":"prompt_preference","input":{"question":"Q"},"chosen":{"variant_id":"a","agent_role":"coder","mode":"gepa","prompt":"` + largePrompt + `","mean_score":0.9,"worst_score":0.8,"pass_count":4},"rejected":{"variant_id":"b","agent_role":"coder","mode":"copro","prompt":"Rejected","mean_score":0.4,"worst_score":0.3,"pass_count":1},"metadata":{"run_id":"run-1","artifact_digest":"sha256:test","provider":"lmstudio"}}
`)

	examples, err := optimization.ParsePromptPreferenceDatasetJSONL(body)
	if err != nil {
		t.Fatalf("ParsePromptPreferenceDatasetJSONL(large): %v", err)
	}
	if len(examples) != 1 {
		t.Fatalf("len(examples)=%d want 1", len(examples))
	}
	if examples[0].Chosen.Prompt != largePrompt {
		t.Fatalf("chosen.prompt length=%d want %d", len(examples[0].Chosen.Prompt), len(largePrompt))
	}
}
