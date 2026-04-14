package optimization_test

import (
	"testing"

	"github.com/joshka0/foxctl/internal/agent/optimization"
)

func TestPromptJudgeScoresTargetAlignedOutputHigherThanGeneric(t *testing.T) {
	t.Parallel()

	judge := optimization.DefaultPromptJudge()
	input := optimization.PromptJudgeInput{
		Question:       "I applied the changes, can you review",
		TargetResponse: "Ignore formatter churn and review only semantic changes.",
	}

	good := input
	good.Output = "Ignore formatter churn and review only semantic changes."
	bad := input
	bad.Output = "Please share the file or code snippet you'd like reviewed."

	if judge.Score(good) <= judge.Score(bad) {
		t.Fatalf("expected target-aligned output to outscore generic request")
	}
}

func TestPromptJudgeUsesQueryWhenTargetMissing(t *testing.T) {
	t.Parallel()

	judge := optimization.DefaultPromptJudge()
	good := optimization.PromptJudgeInput{
		Question: "Theres a hook error PreToolUse Bash hook error",
		Context:  "Strict mode is failing on Bash PreToolUse hooks.",
		Output:   "Read the installed hook file directly and diagnose the Bash strict-mode failure without invoking it again.",
	}
	bad := optimization.PromptJudgeInput{
		Question: good.Question,
		Context:  good.Context,
		Output:   "Please provide more details about the issue.",
	}

	if judge.Score(good) <= judge.Score(bad) {
		t.Fatalf("expected query-aligned answer to outscore generic fallback")
	}
}

func TestPromptJudgePenalizesExcessiveLength(t *testing.T) {
	t.Parallel()

	judge := optimization.DefaultPromptJudge()
	concise := optimization.PromptJudgeInput{
		Question:       "Lets do a small reindex first before the full one",
		TargetResponse: "Build first, then run a targeted reindex on a small package before the full workspace rebuild.",
		Output:         "Build first, then run a targeted reindex on a small package before the full workspace rebuild.",
	}
	verbose := concise
	verbose.Output = concise.Output + " Then explain every step in detail, add extra commentary, and repeat the plan with more background context and additional narrative about why this is safer."

	if judge.Score(concise) <= judge.Score(verbose) {
		t.Fatalf("expected concise answer to outscore overly long version")
	}
}
