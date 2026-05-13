package helperpipeline

import "testing"

func TestStablePipelineIDIsDeterministicAndCompact(t *testing.T) {
	t.Parallel()

	a := StablePipelineID("ephemeral_helper_solve", "preset", "input")
	b := StablePipelineID(" ephemeral_helper_solve ", "", "preset", "input")
	c := StablePipelineID("ephemeral_helper_solve", "preset", "other-input")

	if a != b {
		t.Fatalf("stable id changed after trimming/empty parts: %q != %q", a, b)
	}
	if a == c {
		t.Fatalf("stable id did not vary by input: %q", a)
	}
	if len(a) != len("helper-pipeline:0000000000000000") {
		t.Fatalf("id length=%d id=%q", len(a), a)
	}
}

func TestNewRunBuildsCanonicalScaffoldedTrace(t *testing.T) {
	t.Parallel()

	run := NewRun(RunInput{
		ToolName:    "ephemeral_helper_solve",
		PresetName:  "preset",
		TaskDigest:  "task",
		InputDigest: "input",
		SourceHash:  "source",
		OK:          true,
		Answer:      " solution = 42 ",
		InputKeys:   []string{"initial_state"},
		Steps: []StepRun{{
			StepID:     "solve",
			Capability: CapabilitySolve,
			Status:     "completed",
		}},
	})

	if run.PipelineID == "" {
		t.Fatal("PipelineID is empty")
	}
	if !run.Scaffolded || run.LeaderboardComparable {
		t.Fatalf("scaffold flags scaffolded=%v leaderboard=%v", run.Scaffolded, run.LeaderboardComparable)
	}
	if run.Status != "completed" || run.Answer != "solution = 42" {
		t.Fatalf("status=%q answer=%q", run.Status, run.Answer)
	}
	if run.Signature.VerifierID != "preset" || run.Signature.InputDigest != "input" || len(run.Signature.InputKeys) != 1 {
		t.Fatalf("signature=%+v", run.Signature)
	}
	if len(run.Steps) != 1 || run.Steps[0].Capability != CapabilitySolve {
		t.Fatalf("steps=%+v", run.Steps)
	}
}
