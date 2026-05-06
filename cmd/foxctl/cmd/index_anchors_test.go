package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/joshka0/foxctl/internal/domain/envelope"
	"github.com/joshka0/foxctl/internal/intelligence/indexing/repoindex"
	"github.com/joshka0/foxctl/internal/platform/config"
	"github.com/spf13/cobra"
)

func TestIndexAnchorsExplainHardenedOutput(t *testing.T) {
	ctx := context.Background()
	home := t.TempDir()
	t.Setenv("HOME", home)

	workspace := filepath.Join(home, "repo")
	writeIndexAnchorsTestFile(t, workspace, "go.mod", "module example.com/anchors\n\ngo 1.22\n")
	writeIndexAnchorsTestFile(t, workspace, "docs/guide.md", "# Different Heading\n")
	writeIndexAnchorsTestFile(t, workspace, "internal/demo/demo_test.go", `package demo

import "testing"

func TestOther(t *testing.T) {}
`)
	writeIndexAnchorsTestFile(t, workspace, "internal/demo/demo.go", `package demo

// [[doc:docs/missing.md#Missing]]
// [[doc:docs/guide.md#Missing Fragment]]
// [[test:internal/demo/demo_test.go#MissingTest]]
// [[doc:https://example.com/raw?token=ghp_abcdef123456]]
// [[invariant:api-key-leak]]
// [[beacon:agent-terminal-safety]]
func Build() {}

// [[risk:unbound-risk]]
`)

	cfg, err := config.Load(ctx)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	store, err := repoindex.Open(ctx, cfg.Storage.Root, workspace)
	if err != nil {
		t.Fatalf("open repoindex store: %v", err)
	}
	defer store.Close()
	repoKey := store.RepoKey()
	pkg := "go:example.com/anchors/internal/demo"
	now := time.Unix(123, 0).UTC()
	if err := store.ReplaceAll(ctx, []repoindex.Node{
		{
			ID:        repoindex.FileID(repoKey, pkg, "internal/demo/demo.go"),
			Kind:      repoindex.NodeFile,
			Pkg:       pkg,
			File:      "internal/demo/demo.go",
			Name:      "demo.go",
			SpanStart: 1,
			SpanEnd:   11,
			UpdatedAt: now,
		},
		{
			ID:        repoindex.SymbolID(repoKey, pkg, "Build"),
			Kind:      repoindex.NodeSymbol,
			Pkg:       pkg,
			File:      "internal/demo/demo.go",
			Name:      "Build",
			SpanStart: 9,
			SpanEnd:   9,
			UpdatedAt: now,
		},
	}, nil); err != nil {
		t.Fatalf("replace repoindex nodes: %v", err)
	}

	env, raw := executeIndexAnchorsTestCommand(t, cfg, newIndexAnchorsExplainCommand(),
		"--workspace", workspace,
		"--path", "internal/demo/demo.go",
	)
	if env.Command != "index.anchors.explain" {
		t.Fatalf("command=%q want index.anchors.explain", env.Command)
	}
	if bytes.Contains(raw.Bytes(), []byte("https://example.com")) || bytes.Contains(raw.Bytes(), []byte("ghp_")) || bytes.Contains(raw.Bytes(), []byte("api-key-leak")) {
		t.Fatalf("unsafe raw anchor text leaked:\n%s", raw.String())
	}
	for _, want := range []string{
		`"evidence_authority":"evidence_only"`,
		`"instruction_eligible":false`,
		`[redacted:unsafe_url]`,
		`[redacted:secret_like]`,
		`missing_target`,
		`unresolved_fragment`,
		`unbound_owner`,
		`beacon anchors are advisory recall hints and are not indexed as semantic graph edges`,
	} {
		if !strings.Contains(raw.String(), want) {
			t.Fatalf("output missing %q:\n%s", want, raw.String())
		}
	}
}

func TestIndexAnchorsExplainRejectsUnsafePath(t *testing.T) {
	ctx := context.Background()
	home := t.TempDir()
	t.Setenv("HOME", home)

	workspace := filepath.Join(home, "repo")
	writeIndexAnchorsTestFile(t, workspace, "go.mod", "module example.com/anchors\n\ngo 1.22\n")
	writeIndexAnchorsTestFile(t, workspace, "internal/demo/demo.go", "package demo\n")

	cfg, err := config.Load(ctx)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	store, err := repoindex.Open(ctx, cfg.Storage.Root, workspace)
	if err != nil {
		t.Fatalf("open repoindex store: %v", err)
	}
	defer store.Close()

	for _, badPath := range []string{
		"../outside.go",
		filepath.Join(home, "outside.go"),
		"docs/readme.md",
		`internal\demo\demo.go`,
	} {
		t.Run(badPath, func(t *testing.T) {
			var stdout bytes.Buffer
			cmd := newIndexAnchorsExplainCommand()
			cmd.SetContext(config.WithContext(context.Background(), cfg))
			cmd.SetOut(&stdout)
			cmd.SetArgs([]string{"--workspace", workspace, "--path", badPath})
			if err := cmd.Execute(); err == nil {
				t.Fatalf("expected unsafe path %q to fail; stdout=%s", badPath, stdout.String())
			}
		})
	}
}

func TestIndexAnchorsLintSummaryReportsOwnerIndexHealth(t *testing.T) {
	ctx := context.Background()
	home := t.TempDir()
	t.Setenv("HOME", home)

	workspace := filepath.Join(home, "repo")
	writeIndexAnchorsTestFile(t, workspace, "go.mod", "module example.com/anchors\n\ngo 1.22\n")
	writeIndexAnchorsTestFile(t, workspace, "internal/demo/demo.go", `package demo

// [[risk:orphan-owner]]
func Build() {}
`)

	cfg, err := config.Load(ctx)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	_, raw := executeIndexAnchorsTestCommand(t, cfg, newIndexAnchorsLintCommand(),
		"--workspace", workspace,
		"--summary",
	)

	var payload map[string]any
	if err := json.Unmarshal(raw.Bytes(), &payload); err != nil {
		t.Fatalf("decode raw payload: %v\nstdout:\n%s", err, raw.String())
	}
	data := payload["data"].(map[string]any)
	if got := data["summary_only"]; got != true {
		t.Fatalf("summary_only=%v want true", got)
	}
	if got := int(data["file_count"].(float64)); got != 0 {
		t.Fatalf("file_count=%d want 0 in summary mode", got)
	}
	if got := int(data["scanned_file_count"].(float64)); got != 1 {
		t.Fatalf("scanned_file_count=%d want 1", got)
	}
	ownerIndex := data["owner_index"].(map[string]any)
	if got := ownerIndex["status"]; got != "empty" {
		t.Fatalf("owner_index.status=%v want empty", got)
	}
	findingSummary := data["finding_summary"].(map[string]any)
	byReason := findingSummary["by_reason"].(map[string]any)
	if got := int(byReason["unbound_owner"].(float64)); got == 0 {
		t.Fatalf("unbound_owner count=%d want >0", got)
	}
	bindingSummary := data["owner_binding_summary"].(map[string]any)
	if got := int(bindingSummary["occurrence_count"].(float64)); got != 1 {
		t.Fatalf("occurrence_count=%d want 1", got)
	}
	if got := int(bindingSummary["unbound_occurrence_count"].(float64)); got != 1 {
		t.Fatalf("unbound_occurrence_count=%d want 1", got)
	}
}

func TestNormalizeAnchorSourcePath(t *testing.T) {
	tests := []struct {
		input   string
		want    string
		wantErr bool
	}{
		{input: "internal/demo/../demo/demo.go", want: "internal/demo/demo.go"},
		{input: "../demo.go", wantErr: true},
		{input: "/tmp/demo.go", wantErr: true},
		{input: `internal\demo.go`, wantErr: true},
		{input: "docs/readme.md", wantErr: true},
	}
	for _, tc := range tests {
		t.Run(fmt.Sprintf("%q", tc.input), func(t *testing.T) {
			got, err := normalizeAnchorSourcePath(tc.input)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("normalizeAnchorSourcePath(%q)=%q, want error", tc.input, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("normalizeAnchorSourcePath(%q): %v", tc.input, err)
			}
			if got != tc.want {
				t.Fatalf("normalizeAnchorSourcePath(%q)=%q want %q", tc.input, got, tc.want)
			}
		})
	}
}

func executeIndexAnchorsTestCommand(t *testing.T, cfg config.Config, cmd *cobra.Command, args ...string) (envelope.Envelope, bytes.Buffer) {
	t.Helper()

	var stdout bytes.Buffer
	cmd.SetContext(config.WithContext(context.Background(), cfg))
	cmd.SetOut(&stdout)
	cmd.SetArgs(args)
	if err := cmd.Execute(); err != nil {
		t.Fatalf("execute index anchors command: %v\nstdout:\n%s", err, stdout.String())
	}
	var env envelope.Envelope
	if err := json.Unmarshal(stdout.Bytes(), &env); err != nil {
		t.Fatalf("decode envelope: %v\nstdout:\n%s", err, stdout.String())
	}
	if env.Status != envelope.StatusOK {
		t.Fatalf("status=%q error=%+v stdout:\n%s", env.Status, env.Error, stdout.String())
	}
	return env, stdout
}

func writeIndexAnchorsTestFile(t *testing.T, root, rel, body string) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}
