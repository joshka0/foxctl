package analysisflow

import (
	"context"
	"strings"
	"testing"

	"github.com/jkatigb/agentctl/internal/runtime/hooks"
	"github.com/jkatigb/agentctl/internal/runtime/hooks/lifecycle"
)

func TestAnalyzeEditedFileSkipsNonCode(t *testing.T) {
	resp, err := AnalyzeEditedFile(context.Background(), Dependencies{}, Request{
		Workspace: "/tmp/workspace",
		Payload: Payload{ToolInput: struct {
			FilePath string `json:"file_path,omitempty"`
		}{FilePath: "README.md"}},
	})
	if err != nil {
		t.Fatalf("AnalyzeEditedFile: %v", err)
	}
	if resp.Context != "" {
		t.Fatalf("expected empty context, got %q", resp.Context)
	}
}

func TestAnalyzeEditedFileBuildsComplexityAndImpactContext(t *testing.T) {
	deps := Dependencies{
		RunSkill: func(ctx context.Context, skill string, input any, workspace string, out any) error {
			switch skill {
			case "code/complexity":
				target := out.(*complexityEnvelope)
				target.Data.Results = []struct {
					Function             string `json:"function"`
					Line                 int    `json:"line"`
					CyclomaticComplexity int    `json:"cyclomatic_complexity"`
					RiskLevel            string `json:"risk_level"`
				}{
					{Function: "BuildReport", Line: 42, CyclomaticComplexity: 18, RiskLevel: "high"},
				}
			case "hooks/impact_analysis":
				target := out.(*impactEnvelope)
				target.Data.HookOutput = hooks.Output{Context: "**Impact:** `report.go` - external dependencies found"}
			default:
				t.Fatalf("unexpected skill %s", skill)
			}
			return nil
		},
	}

	resp, err := AnalyzeEditedFile(context.Background(), deps, Request{
		Workspace: "/tmp/workspace",
		Payload: Payload{ToolInput: struct {
			FilePath string `json:"file_path,omitempty"`
		}{FilePath: "internal/report.go"}},
	})
	if err != nil {
		t.Fatalf("AnalyzeEditedFile: %v", err)
	}
	if !strings.Contains(resp.Context, "Complexity") {
		t.Fatalf("expected complexity context, got %q", resp.Context)
	}
	if !strings.Contains(resp.Context, "Impact") {
		t.Fatalf("expected impact context, got %q", resp.Context)
	}
}

func TestNewDependenciesCopiesLifecycleRunner(t *testing.T) {
	life := lifecycle.Dependencies{}
	deps := NewDependencies(life)
	if deps.RunSkill != nil {
		t.Fatalf("expected nil run skill copy, got %#v", deps.RunSkill)
	}
}
