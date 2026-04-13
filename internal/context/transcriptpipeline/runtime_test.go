package transcriptpipeline

import (
	"testing"
	"time"
)

func TestNewLocalModelRuntime_AppliesDefaults(t *testing.T) {
	got := NewLocalModelRuntime("auto", "", "http://localhost:1234/v1", 45*time.Second)
	if got.Provider != DefaultWorkerProvider {
		t.Fatalf("provider=%q want %q", got.Provider, DefaultWorkerProvider)
	}
	if got.Model != DefaultWorkerModel {
		t.Fatalf("model=%q want %q", got.Model, DefaultWorkerModel)
	}
	if got.DoctrineBridgeModel != DefaultDoctrineBridgeModel {
		t.Fatalf("doctrine_bridge_model=%q want %q", got.DoctrineBridgeModel, DefaultDoctrineBridgeModel)
	}
	if got.DoctrineDistillModel != DefaultDoctrineDistillModel {
		t.Fatalf("doctrine_distill_model=%q want %q", got.DoctrineDistillModel, DefaultDoctrineDistillModel)
	}
	if got.ReferencePromptVersion != DefaultReferencePromptVersion {
		t.Fatalf("reference_prompt=%q want %q", got.ReferencePromptVersion, DefaultReferencePromptVersion)
	}
	if got.ObjectivePromptVersion != DefaultObjectivePromptVersion {
		t.Fatalf("objective_prompt=%q want %q", got.ObjectivePromptVersion, DefaultObjectivePromptVersion)
	}
	if got.DoctrineDistillPromptVersion != DefaultDoctrineDistillPromptVersion {
		t.Fatalf("doctrine_prompt=%q want %q", got.DoctrineDistillPromptVersion, DefaultDoctrineDistillPromptVersion)
	}
	if got.ObjectiveAlignmentPromptVersion != DefaultObjectiveAlignmentPromptVersion {
		t.Fatalf("objective_alignment_prompt=%q want %q", got.ObjectiveAlignmentPromptVersion, DefaultObjectiveAlignmentPromptVersion)
	}
	if got.FrameSynopsisPromptVersion != DefaultFrameSynopsisPromptVersion {
		t.Fatalf("frame_synopsis_prompt=%q want %q", got.FrameSynopsisPromptVersion, DefaultFrameSynopsisPromptVersion)
	}
	if got.GroupClaimsPromptVersion != DefaultGroupClaimsPromptVersion {
		t.Fatalf("group_claims_prompt=%q want %q", got.GroupClaimsPromptVersion, DefaultGroupClaimsPromptVersion)
	}
	if got.SynopsisWindowSize != DefaultSynopsisWindowSize {
		t.Fatalf("synopsis_window=%d want %d", got.SynopsisWindowSize, DefaultSynopsisWindowSize)
	}
}

func TestLocalModelRuntime_PreprocessOptionsAndWorkerConfig(t *testing.T) {
	runtime := LocalModelRuntime{
		Mode:                            "deterministic",
		Provider:                        "lmstudio",
		Model:                           "custom-model",
		DoctrineBridgeModel:             "bridge-model",
		DoctrineDistillModel:            "distill-model",
		BaseURL:                         "http://localhost:1234/v1",
		Timeout:                         30 * time.Second,
		MaxContextTokens:                64000,
		ToolOutputSummaryMinTokens:      900,
		SynopsisWindowSize:              4,
		ReferencePromptVersion:          "ref-v2",
		ToolOutputPromptVersion:         "tool-v2",
		ObjectivePromptVersion:          "objective-v2",
		DoctrineDistillPromptVersion:    "doctrine-v2",
		ObjectiveAlignmentPromptVersion: "objective-align-v2",
		FrameSynopsisPromptVersion:      "syn-v2",
	}

	pre := runtime.PreprocessOptions()
	if pre.Mode != "deterministic" || pre.Model != "custom-model" {
		t.Fatalf("preprocess opts=%+v want deterministic/custom-model", pre)
	}
	if pre.ReferencePromptVersion != "ref-v2" || pre.ToolOutputPromptVersion != "tool-v2" {
		t.Fatalf("preprocess prompt versions=%+v", pre)
	}
	if pre.MaxContextTokens != 64000 || pre.ToolOutputSummaryMinTokens != 900 {
		t.Fatalf("preprocess limits=%+v", pre)
	}

	worker := runtime.WorkerConfig()
	if worker.Provider != "lmstudio" || worker.Model != "custom-model" {
		t.Fatalf("worker=%+v want lmstudio/custom-model", worker)
	}
	bridgeWorker := runtime.WorkerConfigForStage(StageBridge)
	if bridgeWorker.Model != "bridge-model" {
		t.Fatalf("bridge worker=%+v want bridge-model", bridgeWorker)
	}
	distillWorker := runtime.WorkerConfigForStage(StageDistill)
	if distillWorker.Model != "distill-model" {
		t.Fatalf("distill worker=%+v want distill-model", distillWorker)
	}
	if worker.MaxContextTokens != 64000 {
		t.Fatalf("worker max_context=%d want 64000", worker.MaxContextTokens)
	}
	if runtime.TimeoutSeconds() != 30 {
		t.Fatalf("timeout_seconds=%d want 30", runtime.TimeoutSeconds())
	}
}
