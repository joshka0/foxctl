package runtime

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/joshka0/foxctl/internal/runtime/engine"
)

func TestEphemeralSkillToolsDraftAndRun(t *testing.T) {
	t.Parallel()

	tools := &EphemeralSkillTools{}
	draft, err := tools.Execute(t.Context(), EphemeralSkillDraftToolName, json.RawMessage(`{
		"source": "func Solve(input map[string]any) map[string]any { value, _ := input[\"value\"].(string); return map[string]any{\"ok\": true, \"answer\": \"solution = \" + value} }"
	}`))
	if err != nil {
		t.Fatalf("draft error = %v", err)
	}
	if !strings.Contains(draft, `"ok":true`) {
		t.Fatalf("draft=%s", draft)
	}
	run, err := tools.Execute(t.Context(), EphemeralSkillRunToolName, json.RawMessage(`{"input":{"value":"42"}}`))
	if err != nil {
		t.Fatalf("run error = %v", err)
	}
	if !strings.Contains(run, `"answer":"solution = 42"`) {
		t.Fatalf("run=%s", run)
	}
}

func TestEphemeralSkillToolsDraftRejectsInvalidSource(t *testing.T) {
	t.Parallel()

	tools := &EphemeralSkillTools{}
	draft, err := tools.Execute(t.Context(), EphemeralSkillDraftToolName, json.RawMessage(`{
		"source": "import \"os\"\nfunc Solve(input map[string]any) map[string]any { return map[string]any{\"ok\": true, \"x\": os.Args} }"
	}`))
	if err != nil {
		t.Fatalf("draft error = %v", err)
	}
	if !strings.Contains(draft, `"ok":false`) || !strings.Contains(draft, "disallowed import os") {
		t.Fatalf("draft=%s", draft)
	}
}

func TestEphemeralSkillToolsDraftAcceptsSourceObject(t *testing.T) {
	t.Parallel()

	tools := &EphemeralSkillTools{}
	draft, err := tools.Execute(t.Context(), EphemeralSkillDraftToolName, json.RawMessage(`{
		"source": {"code": "func Solve(input map[string]any) map[string]any { return map[string]any{\"ok\": true, \"answer\": \"solution = 42\"} }"}
	}`))
	if err != nil {
		t.Fatalf("draft error = %v", err)
	}
	if !strings.Contains(draft, `"ok":true`) {
		t.Fatalf("draft=%s", draft)
	}
}

func TestFinalAnswerFromEphemeralSkillRun(t *testing.T) {
	t.Parallel()

	answer, _, ok := finalAnswerFromEphemeralSkillRun([]engine.ToolResult{{
		Content: `{"ok":true,"output":{"ok":true,"answer":"solution = [[2,1,2],[0,0,2]]"}}`,
	}})
	if !ok {
		t.Fatal("ok=false")
	}
	if answer != "solution = [[2,1,2],[0,0,2]]" {
		t.Fatalf("answer=%q", answer)
	}
}

func TestFinalAnswerFromEphemeralSkillRunIgnoresNonSolution(t *testing.T) {
	t.Parallel()

	_, _, ok := finalAnswerFromEphemeralSkillRun([]engine.ToolResult{{
		Content: `{"ok":true,"output":{"ok":true,"answer":"not final"}}`,
	}})
	if ok {
		t.Fatal("ok=true want false")
	}
}
