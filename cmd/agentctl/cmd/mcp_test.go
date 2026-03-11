package cmd

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"

	"github.com/jkatigb/agentctl/internal/contextplane"
	"github.com/jkatigb/agentctl/internal/domain/skill"
	"github.com/jkatigb/agentctl/internal/platform/config"
	"github.com/jkatigb/agentctl/internal/storage/obsidianindex"
)

func TestGetArgs(t *testing.T) {
	tests := []struct {
		name    string
		args    any
		wantLen int
		wantKey string
		wantVal any
	}{
		{
			name:    "valid map",
			args:    map[string]any{"query": "test"},
			wantLen: 1,
			wantKey: "query",
			wantVal: "test",
		},
		{
			name:    "nil args",
			args:    nil,
			wantLen: 0,
		},
		{
			name:    "wrong type",
			args:    "not a map",
			wantLen: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := mcp.CallToolRequest{}
			req.Params.Arguments = tt.args

			got := getArgs(req)
			if len(got) != tt.wantLen {
				t.Errorf("getArgs() len = %d, want %d", len(got), tt.wantLen)
			}
			if tt.wantKey != "" {
				if v, ok := got[tt.wantKey]; !ok || v != tt.wantVal {
					t.Errorf("getArgs()[%s] = %v, want %v", tt.wantKey, v, tt.wantVal)
				}
			}
		})
	}
}

func TestExtractLibraryID(t *testing.T) {
	tests := []struct {
		name    string
		content []mcp.Content
		want    string
	}{
		{
			name: "path in text",
			content: []mcp.Content{
				mcp.TextContent{Text: "Library ID: /vercel/next.js"},
			},
			want: "/vercel/next.js",
		},
		{
			name: "just path",
			content: []mcp.Content{
				mcp.TextContent{Text: "/mongodb/docs"},
			},
			want: "/mongodb/docs",
		},
		{
			name:    "empty content",
			content: []mcp.Content{},
			want:    "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := &mcp.CallToolResult{Content: tt.content}
			got := extractLibraryID(result)
			if got != tt.want {
				t.Errorf("extractLibraryID() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestInitBackendConfigs(t *testing.T) {
	// Reset backends
	backends.configs = make(map[string]mcpServerConfig)

	// Pass SearchSettings directly instead of using env vars
	searchSettings := config.SearchSettings{
		TavilyAPIKey: "test-key",
	}
	initBackendConfigs(searchSettings)

	// Check tavily was configured
	if cfg, ok := backends.configs["tavily"]; !ok {
		t.Error("tavily config not set")
	} else {
		if cfg.Command != "npx" {
			t.Errorf("tavily command = %q, want npx", cfg.Command)
		}
		if cfg.Env["TAVILY_API_KEY"] != "test-key" {
			t.Errorf("tavily env key = %q, want test-key", cfg.Env["TAVILY_API_KEY"])
		}
	}

	// context7 should always be configured (no API key needed)
	if _, ok := backends.configs["context7"]; !ok {
		t.Error("context7 config not set")
	}
}

func TestRegisterSkillAsTool_ArrayItemsSchema(t *testing.T) {
	manifestPath := filepath.Join(repoRoot(t), "skills", "code_context_ripgrep", "skill.yaml")
	manifest, err := skill.LoadManifest(manifestPath)
	if err != nil {
		t.Fatalf("load manifest: %v", err)
	}

	srv := server.NewMCPServer("agentctl", "test")
	registerSkillAsTool(srv, manifest, "unused")

	tools := srv.ListTools()
	tool, ok := tools["code_context_ripgrep"]
	if !ok || tool == nil {
		t.Fatalf("expected code_context_ripgrep tool")
	}

	for _, name := range []string{"glob", "glob_not"} {
		prop, ok := tool.Tool.InputSchema.Properties[name].(map[string]any)
		if !ok {
			t.Fatalf("expected schema map for %s", name)
		}
		if prop["type"] != "array" {
			t.Fatalf("expected %s type array, got %v", name, prop["type"])
		}
		if items, ok := prop["items"]; !ok || items == nil {
			t.Fatalf("expected %s items schema", name)
		}
	}
}

func TestHandleContextShow(t *testing.T) {
	workspace := t.TempDir()
	store := contextplane.NewWorkspaceStore(workspace)
	if _, err := store.SaveTopOfMind(contextplane.TopOfMind{
		WorkspaceID: "ws-test",
		Objective:   "Test context show",
		Phase:       "design",
	}); err != nil {
		t.Fatalf("SaveTopOfMind: %v", err)
	}

	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{"workspace": workspace}
	result, err := handleContextShow(context.Background(), req)
	if err != nil {
		t.Fatalf("handleContextShow: %v", err)
	}
	text := firstTextContent(result)
	if text == "" || !strings.Contains(text, "Test context show") {
		t.Fatalf("unexpected result text: %s", text)
	}
}

func TestHandleContextObservations(t *testing.T) {
	workspace := t.TempDir()
	store := contextplane.NewWorkspaceStore(workspace)
	if _, err := store.AppendObservation(contextplane.Observation{
		Statement:    "Compact handoffs work better than swollen transcripts.",
		Confidence:   0.72,
		Count:        2,
		Project:      "agentctl",
		Area:         "runtime",
		EvidenceRefs: []string{"handoff:T-1038"},
	}); err != nil {
		t.Fatalf("AppendObservation: %v", err)
	}

	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{"workspace": workspace}
	result, err := handleContextObservations(context.Background(), req)
	if err != nil {
		t.Fatalf("handleContextObservations: %v", err)
	}
	text := firstTextContent(result)
	if text == "" || !strings.Contains(text, "Compact handoffs work better") {
		t.Fatalf("unexpected result text: %s", text)
	}
}

func TestHandleContextHandoffs(t *testing.T) {
	workspace := t.TempDir()
	store := contextplane.NewWorkspaceStore(workspace)
	path, err := store.SaveHandoff(contextplane.Handoff{
		TaskID:      "T-1042",
		Phase:       "formalize",
		Outcome:     "partial",
		Summary:     "Defined planes and promotion rules.",
		NextActions: []string{"Draft ADR-0001"},
	})
	if err != nil {
		t.Fatalf("SaveHandoff: %v", err)
	}

	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{"workspace": workspace, "path": path}
	result, err := handleContextHandoffs(context.Background(), req)
	if err != nil {
		t.Fatalf("handleContextHandoffs: %v", err)
	}
	text := firstTextContent(result)
	if text == "" || !strings.Contains(text, "Defined planes and promotion rules.") {
		t.Fatalf("unexpected result text: %s", text)
	}
}

func TestHandleContextReport(t *testing.T) {
	workspace := t.TempDir()
	store := contextplane.NewWorkspaceStore(workspace)
	if _, err := store.SaveTopOfMind(contextplane.TopOfMind{
		WorkspaceID: "ws-test",
		Objective:   "Build ACA report",
		Phase:       "design",
	}); err != nil {
		t.Fatalf("SaveTopOfMind: %v", err)
	}

	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{"workspace": workspace}
	result, err := handleContextReport(context.Background(), req)
	if err != nil {
		t.Fatalf("handleContextReport: %v", err)
	}
	text := firstTextContent(result)
	if text == "" || !strings.Contains(text, "Build ACA report") {
		t.Fatalf("unexpected result text: %s", text)
	}
}

func TestHandleContextTensions(t *testing.T) {
	workspace := t.TempDir()
	store := contextplane.NewWorkspaceStore(workspace)
	if _, err := store.AppendTension(contextplane.Tension{
		Kind:        "contradiction",
		Statement:   "Runtime writes are bypassing the promotion path.",
		Impact:      "medium",
		RelatedRefs: []string{"note:write-policy"},
	}); err != nil {
		t.Fatalf("AppendTension: %v", err)
	}

	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{"workspace": workspace}
	result, err := handleContextTensions(context.Background(), req)
	if err != nil {
		t.Fatalf("handleContextTensions: %v", err)
	}
	text := firstTextContent(result)
	if text == "" || !strings.Contains(text, "Runtime writes are bypassing the promotion path.") {
		t.Fatalf("unexpected result text: %s", text)
	}
}

func TestHandleContextRethinkAndPromote(t *testing.T) {
	workspace := t.TempDir()
	store := contextplane.NewWorkspaceStore(workspace)
	if _, err := store.AppendTension(contextplane.Tension{
		Kind:        "contradiction",
		Statement:   "Runtime writes are bypassing the promotion path.",
		Impact:      "high",
		Count:       2,
		RelatedRefs: []string{"note:write-policy"},
	}); err != nil {
		t.Fatalf("AppendTension: %v", err)
	}
	if _, err := store.AppendObservation(contextplane.Observation{
		ID:           "O-887",
		Statement:    "Compact handoffs work better than swollen transcripts.",
		Confidence:   0.72,
		Count:        2,
		Project:      "agentctl",
		Area:         "runtime",
		EvidenceRefs: []string{"handoff:T-1038"},
	}); err != nil {
		t.Fatalf("AppendObservation: %v", err)
	}

	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{"workspace": workspace}
	rethinkResult, err := handleContextRethink(context.Background(), req)
	if err != nil {
		t.Fatalf("handleContextRethink: %v", err)
	}
	if text := firstTextContent(rethinkResult); text == "" || !strings.Contains(text, "maintenance_tasks") {
		t.Fatalf("unexpected rethink result: %s", text)
	}

	req = mcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{
		"workspace": workspace,
		"source":    "observation",
		"id":        "O-887",
		"type":      "pattern",
		"title":     "Compact Handoff Pattern",
	}
	promoteResult, err := handleContextPromote(context.Background(), req)
	if err != nil {
		t.Fatalf("handleContextPromote: %v", err)
	}
	if text := firstTextContent(promoteResult); text == "" || !strings.Contains(text, "Compact Handoff Pattern") {
		t.Fatalf("unexpected promote result: %s", text)
	}
}

func TestHandleContextMergePromotion(t *testing.T) {
	workspace := t.TempDir()
	store := contextplane.NewWorkspaceStore(workspace)
	if _, err := store.AppendObservation(contextplane.Observation{
		ID:           "O-887",
		Statement:    "Compact handoffs work better than swollen transcripts.",
		Confidence:   0.72,
		Count:        2,
		Project:      "agentctl",
		Area:         "runtime",
		EvidenceRefs: []string{"handoff:T-1038"},
	}); err != nil {
		t.Fatalf("AppendObservation: %v", err)
	}
	draft, err := store.DraftPromotionFromObservation("O-887", "pattern", "Compact Handoff Pattern")
	if err != nil {
		t.Fatalf("DraftPromotionFromObservation: %v", err)
	}

	vaultRoot := filepath.Join(t.TempDir(), "vault")
	if err := os.MkdirAll(vaultRoot, 0o755); err != nil {
		t.Fatalf("mkdir vault: %v", err)
	}
	script := filepath.Join(t.TempDir(), "obsidian")
	content := `#!/bin/sh
cmd="$1"; shift
path=""
payload=""
for arg in "$@"; do
  case "$arg" in
    path=*) path="${arg#path=}" ;;
    content=*) payload="${arg#content=}" ;;
  esac
done
root="` + vaultRoot + `"
full="$root/$path"
case "$cmd" in
  create)
    mkdir -p "$(dirname "$full")"
    printf "%s" "$payload" > "$full"
    ;;
  read)
    if [ ! -f "$full" ]; then
      echo "File not found." 1>&2
      exit 1
    fi
    cat "$full"
    ;;
  vaults)
    printf "TestVault\t%s\n" "$root"
    ;;
  *) exit 1 ;;
esac
`
	if err := os.WriteFile(script, []byte(content), 0o755); err != nil {
		t.Fatalf("write fake cli: %v", err)
	}
	oldPath := os.Getenv("PATH")
	t.Setenv("PATH", filepath.Dir(script)+string(os.PathListSeparator)+oldPath)

	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{
		"workspace":   workspace,
		"vault_name":  "TestVault",
		"vault_path":  vaultRoot,
		"draft_path":  draft.DraftPath,
		"target_path": "notes/patterns/compact-handoff-pattern.md",
	}
	result, err := handleContextMergePromotion(context.Background(), req)
	if err != nil {
		t.Fatalf("handleContextMergePromotion: %v", err)
	}
	text := firstTextContent(result)
	if text == "" || !strings.Contains(text, "reviewed_merged") {
		t.Fatalf("unexpected merge result: %s", text)
	}
}

func TestHandleContextRetrieve(t *testing.T) {
	workspace := t.TempDir()
	storageRoot := t.TempDir()
	store := contextplane.NewWorkspaceStore(workspace)
	if _, err := store.SaveTopOfMind(contextplane.TopOfMind{
		WorkspaceID:  "ws-test",
		Objective:    "Compact handoff work",
		Phase:        "design",
		RelevantRefs: []string{"path:notes/patterns/compact-handoff-pattern.md"},
		UpdatedAt:    time.Date(2026, 3, 10, 0, 0, 0, 0, time.UTC),
	}); err != nil {
		t.Fatalf("SaveTopOfMind: %v", err)
	}
	vaultRoot := filepath.Clean(filepath.Join(repoRoot(t), "internal", "tools", "obsidian", "testdata", "vaults", "basic"))
	index, err := obsidianindex.Open(context.Background(), storageRoot, vaultRoot)
	if err != nil {
		t.Fatalf("Open index: %v", err)
	}
	defer index.Close()
	if _, err := index.Rebuild(context.Background(), vaultRoot); err != nil {
		t.Fatalf("Rebuild: %v", err)
	}

	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{
		"workspace":  workspace,
		"vault_path": vaultRoot,
		"query":      "Compact Handoff Pattern",
		"limit":      5,
	}
	ctx := config.WithContext(context.Background(), config.Config{
		Storage: config.StorageSettings{Root: storageRoot},
	})
	result, err := handleContextRetrieve(ctx, req)
	if err != nil {
		t.Fatalf("handleContextRetrieve: %v", err)
	}
	text := firstTextContent(result)
	if text == "" || !strings.Contains(text, "Compact Handoff Pattern") {
		t.Fatalf("unexpected retrieve result: %s", text)
	}
}

func TestHandleContextContradictions(t *testing.T) {
	workspace := t.TempDir()
	storageRoot := t.TempDir()
	store := contextplane.NewWorkspaceStore(workspace)
	if _, err := store.SaveTopOfMind(contextplane.TopOfMind{
		WorkspaceID: "ws-test",
		Objective:   "Review contradictions",
		Phase:       "review",
		UpdatedAt:   time.Date(2026, 3, 10, 0, 0, 0, 0, time.UTC),
	}); err != nil {
		t.Fatalf("SaveTopOfMind: %v", err)
	}
	if _, err := store.AppendTension(contextplane.Tension{
		Kind:        "contradiction",
		Statement:   "Pattern notes conflict with the current runtime write policy.",
		Impact:      "high",
		Status:      "open",
		Count:       2,
		RelatedRefs: []string{"note:write-policy"},
	}); err != nil {
		t.Fatalf("AppendTension: %v", err)
	}
	vaultRoot := filepath.Clean(filepath.Join(repoRoot(t), "internal", "tools", "obsidian", "testdata", "vaults", "basic"))
	index, err := obsidianindex.Open(context.Background(), storageRoot, vaultRoot)
	if err != nil {
		t.Fatalf("Open index: %v", err)
	}
	defer index.Close()
	if _, err := index.Rebuild(context.Background(), vaultRoot); err != nil {
		t.Fatalf("Rebuild: %v", err)
	}

	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{
		"workspace":  workspace,
		"vault_path": vaultRoot,
		"limit":      5,
	}
	ctx := config.WithContext(context.Background(), config.Config{
		Storage: config.StorageSettings{Root: storageRoot},
	})
	result, err := handleContextContradictions(ctx, req)
	if err != nil {
		t.Fatalf("handleContextContradictions: %v", err)
	}
	text := firstTextContent(result)
	if text == "" || !strings.Contains(text, "blocked_promotion") {
		t.Fatalf("unexpected contradictions result: %s", text)
	}
}

func firstTextContent(result *mcp.CallToolResult) string {
	if result == nil {
		return ""
	}
	for _, content := range result.Content {
		if text, ok := content.(mcp.TextContent); ok {
			return text.Text
		}
	}
	return ""
}
