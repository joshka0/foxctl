package analysisflow

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/quick"

	"github.com/joshka0/foxctl/internal/runtime/hooks"
	"github.com/joshka0/foxctl/internal/runtime/hooks/lifecycle"
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

func TestAnalyzeSemanticAnchorsDefaultDisabledIsNoop(t *testing.T) {
	workspace := t.TempDir()
	writeFile(t, workspace, "internal/demo.go", `package demo

// [[test:internal/demo_test.go]]
func Build() {}
`)

	resp, err := AnalyzeSemanticAnchors(context.Background(), Request{
		Workspace: workspace,
		Payload: Payload{ToolInput: struct {
			FilePath string `json:"file_path,omitempty"`
		}{FilePath: "internal/demo.go"}},
	})
	if err != nil {
		t.Fatalf("AnalyzeSemanticAnchors: %v", err)
	}
	if resp.Decision != "approve" {
		t.Fatalf("decision = %q", resp.Decision)
	}
	if resp.Context != "" || len(resp.Warnings) != 0 {
		t.Fatalf("expected disabled no-op, got context=%q warnings=%v", resp.Context, resp.Warnings)
	}
}

func TestAnalyzeSemanticAnchorsTouchedFileProducesContext(t *testing.T) {
	t.Setenv("FOXCTL_SEMANTIC_ANCHORS_HOOK", "1")
	workspace := t.TempDir()
	writeFile(t, workspace, "internal/demo_test.go", `package demo

func TestBuild(t *testing.T) {}
`)
	writeFile(t, workspace, "internal/demo.go", `package demo

// [[test:internal/demo_test.go]]
func Build() {}
`)

	resp, err := AnalyzeSemanticAnchors(context.Background(), Request{
		Workspace: workspace,
		Payload: Payload{ToolInput: struct {
			FilePath string `json:"file_path,omitempty"`
		}{FilePath: "internal/demo.go"}},
	})
	if err != nil {
		t.Fatalf("AnalyzeSemanticAnchors: %v", err)
	}
	if resp.Decision != "approve" {
		t.Fatalf("decision = %q", resp.Decision)
	}
	if !strings.Contains(resp.Context, "Semantic anchors") {
		t.Fatalf("expected anchor context, got %q", resp.Context)
	}
	if !strings.Contains(resp.Context, "Linked test contracts") || !strings.Contains(resp.Context, "internal/demo_test.go") {
		t.Fatalf("expected linked test contract, got %q", resp.Context)
	}
	if len(resp.TestContracts) != 1 || resp.TestContracts[0] != "internal/demo_test.go" {
		t.Fatalf("test contracts=%v want internal/demo_test.go", resp.TestContracts)
	}
	if len(resp.GraphDiff) == 0 {
		t.Fatalf("expected graph diff entries")
	}
}

func TestAnalyzeSemanticAnchorsExplicitWorkspaceIgnoresWorkspaceEnv(t *testing.T) {
	t.Setenv("FOXCTL_SEMANTIC_ANCHORS_HOOK", "1")
	t.Setenv("FOXCTL_WORKSPACE", t.TempDir())
	workspace := t.TempDir()
	writeFile(t, workspace, "internal/demo_test.go", `package demo

func TestBuild(t *testing.T) {}
`)
	writeFile(t, workspace, "internal/demo.go", `package demo

// [[test:internal/demo_test.go]]
func Build() {}
`)

	resp, err := AnalyzeSemanticAnchors(context.Background(), Request{
		Workspace: workspace,
		Payload: Payload{ToolInput: struct {
			FilePath string `json:"file_path,omitempty"`
		}{FilePath: "internal/demo.go"}},
	})
	if err != nil {
		t.Fatalf("AnalyzeSemanticAnchors: %v", err)
	}
	if !strings.Contains(resp.Context, "Semantic anchors") || len(resp.GraphDiff) == 0 {
		t.Fatalf("expected explicit workspace semantic anchor context, got context=%q graph=%+v", resp.Context, resp.GraphDiff)
	}
}

func TestAnalyzeSemanticAnchorsWarnsTrustCriticalAnchorWithoutLinkedTests(t *testing.T) {
	t.Setenv("FOXCTL_SEMANTIC_ANCHORS_HOOK", "1")
	workspace := t.TempDir()
	writeFile(t, workspace, "internal/demo.go", `package demo

// [[invariant:no-write-before-read]]
func Build() {}
`)

	resp, err := AnalyzeSemanticAnchors(context.Background(), Request{
		Workspace: workspace,
		Payload: Payload{ToolInput: struct {
			FilePath string `json:"file_path,omitempty"`
		}{FilePath: "internal/demo.go"}},
	})
	if err != nil {
		t.Fatalf("AnalyzeSemanticAnchors: %v", err)
	}
	if !strings.Contains(strings.Join(resp.Warnings, "\n"), "trust-critical anchors changed without linked test contracts") {
		t.Fatalf("expected trust-critical warning, got %v", resp.Warnings)
	}
	if len(resp.GraphDiff) != 1 {
		t.Fatalf("graph diff entries=%d want 1: %+v", len(resp.GraphDiff), resp.GraphDiff)
	}
	if resp.GraphDiff[0].Relation != "ENFORCES" || !resp.GraphDiff[0].WouldEmit {
		t.Fatalf("unexpected graph diff entry: %+v", resp.GraphDiff[0])
	}
	if !strings.Contains(resp.Context, "Semantic anchor graph diff") {
		t.Fatalf("expected graph diff context, got %q", resp.Context)
	}
}

func TestAnalyzeSemanticAnchorsInvalidAnchorWarnsWithoutBlocking(t *testing.T) {
	t.Setenv("FOXCTL_SEMANTIC_ANCHORS_HOOK", "1")
	workspace := t.TempDir()
	writeFile(t, workspace, "internal/demo.go", `package demo

// [[test:../secret_test.go]]
// [[doc:https://example.com/raw?token=ghp_abcdef123456]]
func Build() {}
`)

	resp, err := AnalyzeSemanticAnchors(context.Background(), Request{
		Workspace: workspace,
		Payload: Payload{ToolInput: struct {
			FilePath string `json:"file_path,omitempty"`
		}{FilePath: "internal/demo.go"}},
	})
	if err != nil {
		t.Fatalf("AnalyzeSemanticAnchors: %v", err)
	}
	if resp.Decision != "approve" {
		t.Fatalf("invalid anchor should remain advisory, decision=%q", resp.Decision)
	}
	if len(resp.Warnings) == 0 {
		t.Fatalf("expected warnings, got none; context=%q", resp.Context)
	}
	if strings.Contains(resp.Context, "https://example.com") || strings.Contains(resp.Context, "ghp_") {
		t.Fatalf("unsafe raw anchor leaked into context: %q", resp.Context)
	}
	if !strings.Contains(resp.Context, "[[redacted:unsafe_url]]") {
		t.Fatalf("expected redacted unsafe URL, got %q", resp.Context)
	}
	if !strings.Contains(resp.Context, "Semantic anchor warnings") {
		t.Fatalf("expected warning context, got %q", resp.Context)
	}
}

func TestAnalyzeSemanticAnchorsRejectsEscapingTouchedPath(t *testing.T) {
	t.Setenv("FOXCTL_SEMANTIC_ANCHORS_HOOK", "1")
	parent := t.TempDir()
	workspace := filepath.Join(parent, "workspace")
	if err := os.MkdirAll(workspace, 0o755); err != nil {
		t.Fatalf("mkdir workspace: %v", err)
	}
	writeFile(t, parent, "outside.go", `package outside

// [[invariant:should-not-be-read]]
func Escape() {}
`)

	resp, err := AnalyzeSemanticAnchors(context.Background(), Request{
		Workspace: workspace,
		Payload: Payload{ToolInput: struct {
			FilePath string `json:"file_path,omitempty"`
		}{FilePath: filepath.Join("..", "outside.go")}},
	})
	if err != nil {
		t.Fatalf("AnalyzeSemanticAnchors: %v", err)
	}
	if resp.Context != "" || len(resp.Warnings) != 0 || len(resp.GraphDiff) != 0 {
		t.Fatalf("escaping touched path produced semantic anchor output: %+v", resp)
	}
}

func TestNormalizeHookRepoPathDistinguishesDotDotNamesFromTraversal(t *testing.T) {
	t.Parallel()

	workspace := filepath.Join(t.TempDir(), "workspace")
	inside := filepath.Join(workspace, "..cache", "demo.go")
	if got := normalizeHookRepoPath(workspace, inside); got != filepath.ToSlash(filepath.Join("..cache", "demo.go")) {
		t.Fatalf("normalizeHookRepoPath(dot-dot child) = %q", got)
	}
	if got := normalizeHookRepoPath(workspace, filepath.Join("..", "outside.go")); got != "" {
		t.Fatalf("normalizeHookRepoPath(parent relative escape) = %q, want empty", got)
	}
}

func TestNormalizeHookRepoPathPropertyRejectsGeneratedEscapes(t *testing.T) {
	t.Parallel()

	parent := t.TempDir()
	workspace := filepath.Join(parent, "workspace")
	property := func(raw uint8) bool {
		leaf := "outside-" + string(rune('a'+raw%26)) + ".go"
		if got := normalizeHookRepoPath(workspace, filepath.Join("..", leaf)); got != "" {
			t.Logf("relative escape normalized to %q", got)
			return false
		}
		outside := filepath.Join(parent, leaf)
		if got := normalizeHookRepoPath(workspace, outside); got != "" {
			t.Logf("absolute escape normalized to %q", got)
			return false
		}
		inside := filepath.Join(workspace, "..cache-"+string(rune('a'+raw%26)), "demo.go")
		return normalizeHookRepoPath(workspace, inside) == filepath.ToSlash(filepath.Join(filepath.Base(filepath.Dir(inside)), "demo.go"))
	}
	if err := quick.Check(property, &quick.Config{MaxCount: 100}); err != nil {
		t.Fatalf("hook path normalization property failed: %v", err)
	}
}

func writeFile(t *testing.T, root, rel, body string) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}
