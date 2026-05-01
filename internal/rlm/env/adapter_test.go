package env

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/joshka0/foxctl/internal/context/contextengine"
	"github.com/joshka0/foxctl/internal/context/contextplane"
	"github.com/joshka0/foxctl/internal/domain/skill"
	"github.com/joshka0/foxctl/internal/intelligence/indexing/repoindex"
	"github.com/joshka0/foxctl/internal/platform/config"
	ws "github.com/joshka0/foxctl/internal/platform/workspace"
	"github.com/joshka0/foxctl/internal/rlm"
	"github.com/joshka0/foxctl/internal/runtime/observability"
	"github.com/joshka0/foxctl/internal/storage/cas"
	ctxengstore "github.com/joshka0/foxctl/internal/storage/contextengine"
	"github.com/joshka0/foxctl/internal/storage/obsidianindex"
	"github.com/joshka0/foxctl/internal/storage/sessions"
	taskstore "github.com/joshka0/foxctl/internal/storage/tasks"
	"github.com/joshka0/foxctl/internal/storage/trajectory"
)

func writeFakeExecSkill(t *testing.T, workspaceRoot, skillName, artifactBody string) string {
	t.Helper()

	dir := filepath.Join(workspaceRoot, "skills", skill.NormalizeSkillName(skillName))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	manifest := `apiVersion: foxctl/v1
kind: Skill
metadata:
  name: ` + skillName + `
  version: 0.1.0
  description: fake skill
distribution:
  type: exec
  exec:
    entry: ` + skill.NormalizeSkillName(skillName) + `
io:
  format: JSON
signature:
  command: ` + skillName + `
  parameters: []
  returns: []
`
	if err := os.WriteFile(filepath.Join(dir, "skill.yaml"), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
	artifact := filepath.Join(dir, skill.NormalizeSkillName(skillName))
	if err := os.WriteFile(artifact, []byte(artifactBody), 0o755); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestReadOnlyAdapterBasicTools(t *testing.T) {
	ctx := context.Background()
	workspace := t.TempDir()
	storageRoot := t.TempDir()

	if err := os.WriteFile(filepath.Join(workspace, "main.tf"), []byte("resource \"aws_s3_bucket\" \"app\" {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	repoStore, err := repoindex.Open(ctx, storageRoot, workspace)
	if err != nil {
		t.Fatal(err)
	}
	defer repoStore.Close()
	repoKey := repoStore.RepoKey()
	if err := repoStore.ReplaceAll(ctx, []repoindex.Node{
		{
			ID:      repoindex.NamespacedID(repoKey, "res:tf:root:resource:aws_s3_bucket.app"),
			Kind:    repoindex.NodeConcept,
			Pkg:     "tf:root",
			File:    "main.tf",
			Name:    "resource aws_s3_bucket.app",
			Summary: "Terraform resource aws_s3_bucket app.",
		},
		{
			ID:      repoindex.FileID(repoKey, "tf:root", "main.tf"),
			Kind:    repoindex.NodeFile,
			Pkg:     "tf:root",
			File:    "main.tf",
			Name:    "main.tf",
			Summary: "Terraform file main.tf.",
		},
	}, nil); err != nil {
		t.Fatal(err)
	}

	var cfg config.Config
	cfg.Storage.Root = storageRoot
	adapter := NewReadOnlyAdapter(cfg, workspace, "", nil, rlm.Environment{
		TopOfMind: map[string]any{
			"objective": "trace terraform",
		},
	})

	top, err := adapter.ExecuteInternal(ctx, "get_top_of_mind", nil)
	if err != nil {
		t.Fatalf("get_top_of_mind: %v", err)
	}
	if top["top_of_mind"] == nil {
		t.Fatalf("top_of_mind=%v", top)
	}

	repoResult, err := adapter.ExecuteInternal(ctx, "search_repo", mustJSON(map[string]any{
		"query": "terraform bucket",
		"limit": 3,
	}))
	if err != nil {
		t.Fatalf("search_repo: %v", err)
	}
	rawResults, ok := repoResult["results"].([]map[string]any)
	if ok {
		_ = rawResults
	}
	raw, _ := json.Marshal(repoResult["results"])
	var results []map[string]any
	if err := json.Unmarshal(raw, &results); err != nil {
		t.Fatalf("decode repo results: %v", err)
	}
	if len(results) == 0 || results[0]["path"] != "main.tf" {
		t.Fatalf("repo results=%v", results)
	}

	fileResult, err := adapter.ExecuteInternal(ctx, "load_file", mustJSON(map[string]any{
		"path": "main.tf",
	}))
	if err != nil {
		t.Fatalf("load_file: %v", err)
	}
	if fileResult["content"] == "" {
		t.Fatalf("file result=%v", fileResult)
	}
}

func TestReadOnlyAdapterLoadEvidenceRefLoadsTaskAndBoundsPath(t *testing.T) {
	ctx := context.Background()
	workspace := t.TempDir()
	storageRoot := t.TempDir()
	workspaceID := ws.ID(workspace)

	if err := os.WriteFile(filepath.Join(workspace, "long.txt"), []byte(strings.Repeat("abcdef", 40)), 0o644); err != nil {
		t.Fatal(err)
	}
	store, err := taskstore.Open(ctx, storageRoot)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	_, err = store.Add(ctx, taskstore.Task{
		ID:          "task-1",
		WorkspaceID: workspaceID,
		Title:       "Wire evidence loading",
		Status:      taskstore.StatusInProgress,
		ScopePath:   "internal/rlm/env",
		CreatedAt:   time.Date(2026, 4, 30, 12, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatal(err)
	}

	adapter := NewReadOnlyAdapter(config.Config{}, workspace, "", nil, rlm.Environment{})
	adapter.SetTaskStore(store)

	out, err := adapter.ExecuteInternal(ctx, "load_evidence_ref", mustJSON(map[string]any{
		"ref":        "path:long.txt",
		"max_tokens": 5,
	}))
	if err != nil {
		t.Fatalf("load path: %v", err)
	}
	content, _ := out["content"].(string)
	if len(content) > 20 {
		t.Fatalf("content length=%d want bounded; out=%v", len(content), out)
	}
	if out["truncated"] != true {
		t.Fatalf("expected truncated=true: %v", out)
	}

	out, err = adapter.ExecuteInternal(ctx, "load_evidence_ref", mustJSON(map[string]any{
		"ref": "task:task-1",
	}))
	if err != nil {
		t.Fatalf("load task: %v", err)
	}
	if out["loaded"] != true {
		t.Fatalf("loaded=%v out=%v", out["loaded"], out)
	}
	if out["task_context"] == nil {
		t.Fatalf("missing task_context: %v", out)
	}
}

func TestReadOnlyAdapterLoadEvidenceRefLoadsSymbolAndContextEvent(t *testing.T) {
	ctx := context.Background()
	workspace := t.TempDir()
	storageRoot := t.TempDir()
	workspaceID := ws.ID(workspace)

	repoFile := filepath.Join(workspace, "internal", "rlm", "env", "tool_exec.go")
	if err := os.MkdirAll(filepath.Dir(repoFile), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(repoFile, []byte("package env\n\nfunc gatherContext() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	repoStore, err := repoindex.Open(ctx, storageRoot, workspace)
	if err != nil {
		t.Fatal(err)
	}
	repoKey := repoStore.RepoKey()
	err = repoStore.ReplaceAll(ctx, []repoindex.Node{{
		ID:        repoindex.SymbolID(repoKey, "go:internal/rlm/env", "func:gatherContext"),
		Kind:      repoindex.NodeSymbol,
		Pkg:       "go:internal/rlm/env",
		File:      "internal/rlm/env/tool_exec.go",
		Name:      "gatherContext",
		Summary:   "loads gather_context arguments",
		SpanStart: 3,
	}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := repoStore.Close(); err != nil {
		t.Fatal(err)
	}

	ceStore, err := ctxengstore.Open(ctx, storageRoot)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ceStore.Close() })
	_, err = ceStore.AppendEvent(ctx, contextengine.ContextEvent{
		ID:          "evt-1",
		WorkspaceID: workspaceID,
		Kind:        contextengine.EventKindRetrievalExecuted,
		Source:      "test",
		Data:        map[string]any{"query": "gather_context"},
		CreatedAt:   time.Date(2026, 4, 30, 12, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatal(err)
	}

	cfg := config.Config{}
	cfg.Storage.Root = storageRoot
	adapter := NewReadOnlyAdapter(cfg, workspace, "", nil, rlm.Environment{})
	adapter.SetContextEngineStore(ceStore)

	out, err := adapter.ExecuteInternal(ctx, "load_evidence_ref", mustJSON(map[string]any{
		"ref": "symbol:gatherContext",
	}))
	if err != nil {
		t.Fatalf("load symbol: %v", err)
	}
	if out["loaded"] != true {
		t.Fatalf("symbol loaded=%v out=%v", out["loaded"], out)
	}
	rawAnchors, _ := json.Marshal(out["anchors"])
	var anchors []map[string]any
	if err := json.Unmarshal(rawAnchors, &anchors); err != nil {
		t.Fatalf("decode anchors: %v", err)
	}
	if len(anchors) == 0 || anchors[0]["path"] != "internal/rlm/env/tool_exec.go" {
		t.Fatalf("anchors=%v", anchors)
	}

	out, err = adapter.ExecuteInternal(ctx, "load_evidence_ref", mustJSON(map[string]any{
		"ref": "event:evt-1",
	}))
	if err != nil {
		t.Fatalf("load event: %v", err)
	}
	if out["loaded"] != true {
		t.Fatalf("event loaded=%v out=%v", out["loaded"], out)
	}
}

func TestReadOnlyAdapterGatherContextIncludesSessionRecall(t *testing.T) {
	ctx := context.Background()
	workspace := t.TempDir()
	storageRoot := t.TempDir()
	workspaceID := ws.ID(workspace)
	sessionStore, err := sessions.Open(ctx, storageRoot)
	if err != nil {
		t.Fatal(err)
	}
	_, err = sessionStore.Save(ctx, sessions.Session{
		ID:            "sess-rlm-1",
		WorkspaceID:   workspaceID,
		WorkspacePath: workspace,
		ProjectName:   "foxctl",
		Summary:       "Implemented gather_context certification for RLM.",
		Decisions:     []string{"Use ContextBundle as the bounded answering surface."},
		KeyFiles:      []string{"internal/context/contextengine/context_gather.go"},
		StartedAt:     time.Date(2026, 4, 30, 9, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := sessionStore.Close(); err != nil {
		t.Fatal(err)
	}

	cfg := config.Config{}
	cfg.Storage.Root = storageRoot
	adapter := NewReadOnlyAdapter(cfg, workspace, "", nil, rlm.Environment{
		Tools: []rlm.Tool{{Name: "gather_context", ReadOnly: true}},
	})
	out, err := adapter.Execute(ctx, "gather_context", mustJSON(map[string]any{
		"query": "gather_context certification",
		"lanes": []string{"context"},
		"limit": 3,
	}))
	if err != nil {
		t.Fatalf("gather_context: %v", err)
	}
	body, err := json.Marshal(out)
	if err != nil {
		t.Fatalf("marshal output: %v", err)
	}
	var bundle contextengine.ContextBundle
	if err := json.Unmarshal(body, &bundle); err != nil {
		t.Fatalf("decode bundle: %v", err)
	}
	found := false
	for _, node := range bundle.Evidence {
		if node.Ref.Type == contextengine.RefTypeSession && node.Ref.Ref == "sess-rlm-1" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("session recall evidence missing: %#v", bundle.Evidence)
	}
}

func TestReadOnlyAdapterExecuteDeniesToolsOutsideAllowlist(t *testing.T) {
	t.Parallel()

	adapter := NewReadOnlyAdapter(config.Config{}, t.TempDir(), "", nil, rlm.Environment{
		Tools: []rlm.Tool{
			{Name: "load_file", ReadOnly: true},
		},
	})

	_, err := adapter.Execute(context.Background(), "get_top_of_mind", nil)
	if err == nil {
		t.Fatal("Execute() error = nil, want allowlist denial")
	}
	if !strings.Contains(err.Error(), "not allowed") {
		t.Fatalf("Execute() error = %v", err)
	}
}

func TestReadOnlyAdapterExecuteInternalBypassesAllowlist(t *testing.T) {
	t.Parallel()

	adapter := NewReadOnlyAdapter(config.Config{}, t.TempDir(), "", nil, rlm.Environment{
		TopOfMind: map[string]any{"objective": "verify bypass"},
		Tools: []rlm.Tool{
			{Name: "load_file", ReadOnly: true},
		},
	})

	out, err := adapter.ExecuteInternal(context.Background(), "get_top_of_mind", nil)
	if err != nil {
		t.Fatalf("ExecuteInternal() error = %v", err)
	}
	if out["top_of_mind"] == nil {
		t.Fatalf("ExecuteInternal() output=%v", out)
	}
}

func TestDirectContextPackFromObsidianHits(t *testing.T) {
	t.Parallel()

	pack := directContextPackFromObsidianHits("ws-test", "context bundle docs", []obsidianindex.SearchHit{{
		Path:    "docs/architecture/rlm-gather-context.md",
		Title:   "RLM Gather Context",
		Type:    "architecture",
		Project: "foxctl",
		Status:  "current",
		Trust:   "verified",
		Score:   900,
		Snippet: "ContextBundle certification bridge",
	}})
	if pack.Lane != contextengine.LaneContext || len(pack.Nodes) != 1 {
		t.Fatalf("pack=%+v", pack)
	}
	node := pack.Nodes[0]
	if node.Ref.Type != contextengine.RefTypePath || node.Ref.Ref != "docs/architecture/rlm-gather-context.md" {
		t.Fatalf("ref=%+v", node.Ref)
	}
	if node.Statement != "ContextBundle certification bridge" {
		t.Fatalf("statement=%q", node.Statement)
	}
}

func TestReadOnlyAdapterGatherContext(t *testing.T) {
	t.Parallel()

	adapter := NewReadOnlyAdapter(config.Config{}, t.TempDir(), "", nil, rlm.Environment{
		TopOfMind: map[string]any{"objective": "certify context", "phase": "implementation"},
		Tools: []rlm.Tool{
			{Name: "gather_context", ReadOnly: true},
		},
	})

	out, err := adapter.ExecuteInternal(context.Background(), "gather_context", mustJSON(map[string]any{
		"query": "certify context",
		"lanes": []string{"context"},
		"limit": 3,
	}))
	if err != nil {
		t.Fatalf("gather_context: %v", err)
	}
	if out["certificate"] == nil {
		t.Fatalf("output missing certificate: %v", out)
	}
	if out["answerable"] != true {
		t.Fatalf("answerable=%v output=%v", out["answerable"], out)
	}

	if _, err := adapter.Execute(context.Background(), "gather_context", mustJSON(map[string]any{"query": "certify"})); err != nil {
		t.Fatalf("allowlisted gather_context Execute: %v", err)
	}
}

func TestReadOnlyAdapterGatherContextAnswerSurfaceOmitsRawEvidence(t *testing.T) {
	t.Parallel()

	adapter := NewReadOnlyAdapter(config.Config{}, t.TempDir(), "", nil, rlm.Environment{
		TopOfMind: map[string]any{
			"objective": "certify context",
			"relevant_refs": []map[string]any{{
				"type": "path",
				"ref":  "internal/context/contextengine/context_bundle.go",
			}},
		},
		Tools: []rlm.Tool{{Name: "gather_context", ReadOnly: true}},
	})

	out, err := adapter.ExecuteInternal(context.Background(), "gather_context", mustJSON(map[string]any{
		"query":           "certify context",
		"task_type":       "subsystem_map",
		"source_profiles": []string{"repo_code", "codemaps", "cochange_history"},
		"lanes":           []string{"context"},
		"limit":           3,
		"response_mode":   "answer_surface",
	}))
	if err != nil {
		t.Fatalf("gather_context answer_surface: %v", err)
	}
	if _, ok := out["evidence"]; ok {
		t.Fatalf("answer_surface should omit raw evidence: %v", out)
	}
	if out["selected_paths"] == nil {
		t.Fatalf("answer_surface missing selected_paths: %v", out)
	}
	if out["answer_candidates"] == nil {
		t.Fatalf("answer_surface missing answer_candidates: %v", out)
	}
	if out["schema_version"] != "context_answer_surface/v2" {
		t.Fatalf("schema_version=%v output=%v", out["schema_version"], out)
	}
	seed, ok := out["answer_seed"].(map[string]any)
	if !ok || seed["paths"] == nil {
		t.Fatalf("answer_surface missing answer_seed.paths: %v", out)
	}
	pathSet, ok := out["path_set"].(map[string]any)
	if !ok || pathSet["must"] == nil {
		t.Fatalf("answer_surface missing path_set.must: %v", out)
	}
	loadQueue, ok := out["load_queue"].([]map[string]any)
	if !ok || len(loadQueue) == 0 || loadQueue[0]["ref"] == "" {
		t.Fatalf("answer_surface missing load_queue refs: %T %v", out["load_queue"], out["load_queue"])
	}
	categories, ok := out["categories"].([]contextengine.ContextCategory)
	if !ok || len(categories) == 0 {
		t.Fatalf("answer_surface missing subsystem categories: %T %v", out["categories"], out["categories"])
	}
	contract, ok := out["answer_contract"].(map[string]any)
	if !ok || contract["mode"] != "repo_subsystem_map" {
		t.Fatalf("answer_contract=%v", out["answer_contract"])
	}
}

func TestReadOnlyAdapterGatherContextIncludesACAContext(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	workspace := t.TempDir()
	storageRoot := t.TempDir()
	vault := t.TempDir()
	notePath := filepath.Join(vault, "notes", "repo", "foxctl", "contextbundle-certification.md")
	if err := os.MkdirAll(filepath.Dir(notePath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(notePath, []byte(`# ContextBundle Certification

The certified bundle gate requires runtime certification before an answerer trusts gathered context.
`), 0o644); err != nil {
		t.Fatal(err)
	}
	index, err := obsidianindex.Open(ctx, storageRoot, vault)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := index.Rebuild(ctx, vault); err != nil {
		t.Fatal(err)
	}
	if err := index.Close(); err != nil {
		t.Fatal(err)
	}

	workspaceID := ws.ID(workspace)
	acaStore := contextplane.NewWorkspaceStore(workspace)
	if _, err := acaStore.SaveTopOfMind(contextplane.TopOfMind{
		WorkspaceID: workspaceID,
		Objective:   "Wire ACA context into gather_context",
		Phase:       "implementation",
		UpdatedAt:   time.Date(2026, 4, 30, 12, 0, 0, 0, time.UTC),
	}); err != nil {
		t.Fatal(err)
	}

	var cfg config.Config
	cfg.Storage.Root = storageRoot
	adapter := NewReadOnlyAdapter(cfg, workspace, vault, nil, rlm.Environment{
		TopOfMind: map[string]any{
			"workspace_id": workspaceID,
			"objective":    "Wire ACA context into gather_context",
			"phase":        "implementation",
		},
		LatestHandoff: map[string]any{
			"summary": "Runtime certification belongs to ContextCertificate.",
			"ref":     "note:handoff:contextbundle-certification",
			"file_refs": []any{
				map[string]any{"type": "path", "ref": "internal/context/contextengine/context_certify.go"},
			},
		},
		Tools: []rlm.Tool{{Name: "gather_context", ReadOnly: true}},
	})

	out, err := adapter.ExecuteInternal(ctx, "gather_context", mustJSON(map[string]any{
		"query": "ContextBundle certified bundle gate runtime certification",
		"lanes": []string{"context"},
		"limit": 8,
	}))
	if err != nil {
		t.Fatalf("gather_context: %v", err)
	}
	var bundle contextengine.ContextBundle
	body, _ := json.Marshal(out)
	if err := json.Unmarshal(body, &bundle); err != nil {
		t.Fatal(err)
	}
	var facts []string
	for _, fact := range bundle.Facts {
		facts = append(facts, fact.Fact)
	}
	joined := strings.Join(facts, "\n")
	if !strings.Contains(joined, "Runtime certification belongs to ContextCertificate") {
		t.Fatalf("missing handoff fact: %s", joined)
	}
	if !strings.Contains(joined, "certified bundle gate requires runtime certification") {
		t.Fatalf("missing ACA vault fact: %s", joined)
	}
	if bundle.SourceCoverage["context"] == 0 {
		t.Fatalf("source coverage missing context lane: %#v", bundle.SourceCoverage)
	}
}

func TestReadOnlyAdapterLoadsTrajectoryAndCASArtifacts(t *testing.T) {
	ctx := context.Background()
	workspace := t.TempDir()
	storageRoot := t.TempDir()
	casRoot := filepath.Join(storageRoot, "cas")
	if err := os.MkdirAll(casRoot, 0o755); err != nil {
		t.Fatal(err)
	}

	var cfg config.Config
	cfg.Storage.Root = storageRoot
	cfg.Paths.CAS = casRoot

	casStore, err := cas.NewStore(casRoot)
	if err != nil {
		t.Fatal(err)
	}
	defer casStore.Close()
	obj, err := casStore.Put(ctx, strings.NewReader("artifact body"), "text/plain", nil)
	if err != nil {
		t.Fatal(err)
	}

	trajStore, err := trajectory.Open(ctx, storageRoot)
	if err != nil {
		t.Fatal(err)
	}
	defer trajStore.Close()
	traj, err := trajStore.InsertTrajectory(ctx, trajectory.Trajectory{
		ID:             "traj-1",
		WorkspaceID:    ws.ID(workspace),
		Status:         trajectory.StatusOK,
		Summary:        "artifact summary",
		ArtifactDigest: obj.Digest,
		CreatedAt:      time.Now().UTC(),
		UpdatedAt:      time.Now().UTC(),
	})
	if err != nil {
		t.Fatal(err)
	}

	adapter := NewReadOnlyAdapter(cfg, workspace, "", nil, rlm.Environment{
		ArtifactHandles: []string{"trajectory:" + traj.ID, "artifact:" + obj.Digest},
	})

	search, err := adapter.ExecuteInternal(ctx, "search_artifacts", mustJSON(map[string]any{
		"query": "artifact",
		"limit": 5,
	}))
	if err != nil {
		t.Fatalf("search_artifacts: %v", err)
	}
	raw, _ := json.Marshal(search["results"])
	var handles []string
	if err := json.Unmarshal(raw, &handles); err != nil {
		t.Fatalf("decode handles: %v", err)
	}
	if len(handles) == 0 {
		t.Fatalf("expected artifact handles")
	}

	loaded, err := adapter.ExecuteInternal(ctx, "load_artifact", mustJSON(map[string]any{
		"handle": "artifact:" + obj.Digest,
	}))
	if err != nil {
		t.Fatalf("load_artifact: %v", err)
	}
	artifact, ok := loaded["artifact"].(map[string]any)
	if !ok || artifact["content"] != "artifact body" {
		t.Fatalf("artifact=%v", loaded)
	}
}

func TestReadOnlyAdapterSubcallCarriesRole(t *testing.T) {
	t.Parallel()

	adapter := NewReadOnlyAdapter(config.Config{}, t.TempDir(), "", nil, rlm.Environment{})
	var gotTask rlm.Task
	adapter.SetSubcall(func(_ context.Context, task rlm.Task, env rlm.Environment) (rlm.Result, error) {
		gotTask = task
		return rlm.Result{Answer: "ok"}, nil
	})

	_, err := adapter.ExecuteInternal(context.Background(), "subcall", mustJSON(map[string]any{
		"prompt": "Find the latest codename update.",
		"role":   ScoutRoleMemoryTimeline,
	}))
	if err != nil {
		t.Fatalf("subcall: %v", err)
	}
	if gotTask.Role != ScoutRoleMemoryTimeline {
		t.Fatalf("role=%q want %q", gotTask.Role, ScoutRoleMemoryTimeline)
	}
}

func TestReadOnlyAdapterMemoryEnsembleRetrieve(t *testing.T) {
	t.Parallel()

	adapter := NewReadOnlyAdapter(config.Config{}, t.TempDir(), "", nil, rlm.Environment{})
	var seenRoles []string
	adapter.SetSubcall(func(_ context.Context, task rlm.Task, env rlm.Environment) (rlm.Result, error) {
		seenRoles = append(seenRoles, task.Role)
		return rlm.Result{
			Answer:         "summary for " + task.Role,
			EvidenceRefs:   []string{"evidence:" + task.Role},
			RetrievedPaths: []string{"path:" + task.Role},
		}, nil
	})

	out, err := adapter.ExecuteInternal(context.Background(), "memory_ensemble_retrieve", mustJSON(map[string]any{
		"query": "What changed about the codename over time?",
		"lanes": []string{"timeline", "facts"},
	}))
	if err != nil {
		t.Fatalf("memory_ensemble_retrieve: %v", err)
	}
	if len(seenRoles) != 2 {
		t.Fatalf("roles=%v want 2 roles", seenRoles)
	}
	if out["recommended_answer_basis"] != "combined" {
		t.Fatalf("recommended_answer_basis=%v want combined", out["recommended_answer_basis"])
	}
	metadata, ok := out["metadata"].(map[string]any)
	if !ok {
		t.Fatalf("metadata=%T", out["metadata"])
	}
	if metadata["scouts_run"] == nil {
		t.Fatalf("metadata=%v missing scouts_run", metadata)
	}
}

func TestReadOnlyAdapterMemoryEnsembleRetrieveStructuredFindings(t *testing.T) {
	t.Parallel()

	adapter := NewReadOnlyAdapter(config.Config{}, t.TempDir(), "", nil, rlm.Environment{})
	adapter.SetSubcall(func(_ context.Context, task rlm.Task, env rlm.Environment) (rlm.Result, error) {
		switch task.Role {
		case ScoutRoleMemoryFact:
			return rlm.Result{
				Answer:       `{"summary":"Current codename is amber-river-19.","claims":[{"key":"codename","value":"amber-river-19","status":"current","source":"agent_memory_search"}]}`,
				EvidenceRefs: []string{"artifact:fact"},
			}, nil
		case ScoutRoleMemoryTimeline:
			return rlm.Result{
				Answer:       `{"summary":"The codename changed once.","current_best_view":"Current codename is amber-river-19.","timeline":[{"ts":"2026-03-20T10:00:00Z","kind":"update","value":"codename changed to amber-river-19","source":"session_timeline"}]}`,
				EvidenceRefs: []string{"artifact:timeline"},
			}, nil
		default:
			return rlm.Result{
				Answer:       `{"summary":"No durable ACA context beyond the active codename update.","context_blocks":[{"lane":"top_of_mind","summary":"codename rollout in progress","refs":["note:aca"]}]}`,
				EvidenceRefs: []string{"artifact:aca"},
			}, nil
		}
	})

	out, err := adapter.ExecuteInternal(context.Background(), "memory_ensemble_retrieve", mustJSON(map[string]any{
		"query":      "What is the current codename?",
		"max_scouts": 3,
	}))
	if err != nil {
		t.Fatalf("memory_ensemble_retrieve: %v", err)
	}
	if out["recommended_answer_basis"] != "timeline" {
		t.Fatalf("recommended_answer_basis=%v want timeline", out["recommended_answer_basis"])
	}
	if summary, _ := out["summary"].(string); !strings.Contains(summary, "amber-river-19") {
		t.Fatalf("summary=%q want codename", summary)
	}
	if claims, ok := out["claims"].([]memoryScoutClaim); !ok || len(claims) != 1 {
		t.Fatalf("claims=%T %v", out["claims"], out["claims"])
	}
	if timeline, ok := out["timeline"].([]memoryScoutTimelineItem); !ok || len(timeline) != 1 {
		t.Fatalf("timeline=%T %v", out["timeline"], out["timeline"])
	}
}

func TestReadOnlyAdapterResolvePreferredSkillArtifactPrefersWorkspaceLocalSkill(t *testing.T) {
	t.Setenv("FOXCTL_SKILLS_PATH", "")
	workspace := t.TempDir()
	skillDir := writeFakeExecSkill(t, workspace, "code/semantic_search", "#!/bin/sh\nprintf '{}'\n")

	adapter := NewReadOnlyAdapter(config.Config{}, workspace, "", nil, rlm.Environment{})
	manifestPath, artifactPath, err := adapter.resolvePreferredSkillArtifact("code/semantic_search")
	if err != nil {
		t.Fatalf("resolvePreferredSkillArtifact: %v", err)
	}
	if got, want := filepath.Clean(manifestPath), filepath.Join(skillDir, "skill.yaml"); got != want {
		t.Fatalf("manifest=%q want %q", got, want)
	}
	if got, want := filepath.Clean(artifactPath), filepath.Join(skillDir, "code_semantic_search"); got != want {
		t.Fatalf("artifact=%q want %q", got, want)
	}
}

func TestReadOnlyAdapterSemanticSearchCodeDecodesCandidateBundlesFromWorkspaceSkill(t *testing.T) {
	t.Setenv("FOXCTL_SKILLS_PATH", "")
	workspace := t.TempDir()
	writeFakeExecSkill(t, workspace, "code/semantic_search", `#!/bin/sh
cat <<'EOF'
{"version":1,"status":"ok","command":"code/semantic_search","data":{"query":"skill manifest","results":[{"path":"skills/example/main.go","name":"Example","source":"symbols","summary":"implementation","similarity":0.91}],"candidate_bundles":[{"key":"skills/example","primary_path":"skills/example/skill.yaml","related_paths":["skills/example/main.go"],"symbols":["Example"],"sources":["symbols"],"score":0.91,"match_reason":"manifest bundle","ambiguity":"single_file_with_companions"}]},"meta":{"ts":"2026-03-23T00:00:00Z"},"error":{}}
EOF
`)

	adapter := NewReadOnlyAdapter(config.Config{}, workspace, "", nil, rlm.Environment{})
	out, err := adapter.semanticSearchCode(context.Background(), mustJSON(map[string]any{
		"query":   "skill manifest",
		"profile": "code",
		"scope":   []string{"symbols"},
		"limit":   8,
	}))
	if err != nil {
		t.Fatalf("semanticSearchCode: %v", err)
	}
	raw, err := json.Marshal(out["candidate_bundles"])
	if err != nil {
		t.Fatalf("marshal candidate_bundles: %v", err)
	}
	var bundles []map[string]any
	if err := json.Unmarshal(raw, &bundles); err != nil {
		t.Fatalf("decode candidate_bundles: %v", err)
	}
	if len(bundles) != 1 {
		t.Fatalf("candidate_bundles=%v want 1 bundle", bundles)
	}
	if got, want := bundles[0]["primary_path"], "skills/example/skill.yaml"; got != want {
		t.Fatalf("primary_path=%v want %q", got, want)
	}
	relatedRaw, err := json.Marshal(bundles[0]["related_paths"])
	if err != nil {
		t.Fatalf("marshal related_paths: %v", err)
	}
	var related []string
	if err := json.Unmarshal(relatedRaw, &related); err != nil {
		t.Fatalf("decode related_paths: %v", err)
	}
	if len(related) != 1 || related[0] != "skills/example/main.go" {
		t.Fatalf("related_paths=%v", related)
	}
}

func TestReadOnlyAdapterResolvePreferredSkillArtifactPrefersWorkspaceOverConfiguredSkillsRoot(t *testing.T) {
	t.Setenv("FOXCTL_SKILLS_PATH", "")
	workspace := t.TempDir()
	configuredSkills := t.TempDir()

	workspaceSkillDir := writeFakeExecSkill(t, workspace, "code/semantic_search", "#!/bin/sh\nprintf '{}'\n")
	configuredSkillDir := filepath.Join(configuredSkills, skill.NormalizeSkillName("code/semantic_search"))
	if err := os.MkdirAll(configuredSkillDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(configuredSkillDir, "skill.yaml"), []byte(`apiVersion: foxctl/v1
kind: Skill
metadata:
  name: code/semantic_search
  version: 0.1.0
  description: configured fake skill
distribution:
  type: exec
  exec:
    entry: code_semantic_search
io:
  format: JSON
signature:
  command: code/semantic_search
  parameters: []
  returns: []
`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(configuredSkillDir, "code_semantic_search"), []byte("#!/bin/sh\nprintf '{}'\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	var cfg config.Config
	cfg.Paths.Skills = configuredSkills
	adapter := NewReadOnlyAdapter(cfg, workspace, "", nil, rlm.Environment{})

	manifestPath, _, err := adapter.resolvePreferredSkillArtifact("code/semantic_search")
	if err != nil {
		t.Fatalf("resolvePreferredSkillArtifact: %v", err)
	}
	if got, want := filepath.Clean(manifestPath), filepath.Join(workspaceSkillDir, "skill.yaml"); got != want {
		t.Fatalf("manifest=%q want workspace skill %q", got, want)
	}
}

func TestReadOnlyAdapterCodeSearchEnsembleFileLocate(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	workspace := t.TempDir()
	storageRoot := t.TempDir()

	writeTestFile(t, filepath.Join(workspace, "internal", "agent", "types", "types.go"), "package types\n\ntype AgentRole string\n")
	writeTestFile(t, filepath.Join(workspace, "internal", "agent", "runtime", "runtime.go"), "package runtime\n\nfunc BuildToolDefsForRole() {}\n")
	writeTestFile(t, filepath.Join(workspace, "internal", "rlm", "env", "scout_roles.go"), "package env\n\nconst ScoutRole = \"experimental\"\n")

	store, err := repoindex.Open(ctx, storageRoot, workspace)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	repoKey := store.RepoKey()
	nodes := []repoindex.Node{
		{
			ID:        repoindex.FileID(repoKey, "agent/types", "internal/agent/types/types.go"),
			Kind:      repoindex.NodeFile,
			Pkg:       "agent/types",
			File:      "internal/agent/types/types.go",
			Name:      "types.go",
			Summary:   "Legacy agent role definitions for the classic runtime.",
			UpdatedAt: time.Now().UTC(),
		},
		{
			ID:        repoindex.NamespacedID(repoKey, "sym:agent/runtime:BuildToolDefsForRole"),
			Kind:      repoindex.NodeSymbol,
			Pkg:       "agent/runtime",
			File:      "internal/agent/runtime/runtime.go",
			Name:      "BuildToolDefsForRole",
			Summary:   "Role-specific runtime tool wiring for the classic agent runtime.",
			SpanStart: 3,
			SpanEnd:   3,
			UpdatedAt: time.Now().UTC(),
		},
		{
			ID:        repoindex.FileID(repoKey, "agent/runtime", "internal/agent/runtime/runtime.go"),
			Kind:      repoindex.NodeFile,
			Pkg:       "agent/runtime",
			File:      "internal/agent/runtime/runtime.go",
			Name:      "runtime.go",
			Summary:   "Classic runtime role wiring.",
			UpdatedAt: time.Now().UTC(),
		},
		{
			ID:        repoindex.FileID(repoKey, "rlm/env", "internal/rlm/env/scout_roles.go"),
			Kind:      repoindex.NodeFile,
			Pkg:       "rlm/env",
			File:      "internal/rlm/env/scout_roles.go",
			Name:      "scout_roles.go",
			Summary:   "Experimental RLM scout role routing.",
			UpdatedAt: time.Now().UTC(),
		},
	}
	if err := store.ReplaceAll(ctx, nodes, nil); err != nil {
		t.Fatal(err)
	}

	var cfg config.Config
	cfg.Storage.Root = storageRoot
	adapter := NewReadOnlyAdapter(cfg, workspace, "", nil, rlm.Environment{})

	out, err := adapter.ExecuteInternal(ctx, "code_search_ensemble", mustJSON(map[string]any{
		"query":     "legacy scout role classic runtime",
		"task_type": "file_locate",
		"constraints": map[string]any{
			"exclude_paths":     []string{"internal/rlm/env/**"},
			"require_grounding": true,
		},
	}))
	if err != nil {
		t.Fatalf("code_search_ensemble: %v", err)
	}
	if out["task_type"] != codeSearchTaskFileLocate {
		t.Fatalf("task_type=%v", out["task_type"])
	}
	metadata, ok := out["metadata"].(map[string]any)
	if !ok || metadata["grounded"] != true {
		t.Fatalf("metadata=%v", out["metadata"])
	}
	rawFiles, _ := json.Marshal(out["files"])
	var files []codeSearchEvidenceFile
	if err := json.Unmarshal(rawFiles, &files); err != nil {
		t.Fatalf("decode files: %v", err)
	}
	if len(files) == 0 {
		t.Fatalf("files=%v", out["files"])
	}
	foundRuntime := false
	for _, file := range files {
		if strings.HasPrefix(file.Path, "internal/rlm/env/") {
			t.Fatalf("excluded file leaked into output: %+v", files)
		}
		if file.Path == "internal/agent/runtime/runtime.go" {
			foundRuntime = true
		}
	}
	if !foundRuntime {
		t.Fatalf("expected runtime.go in files: %+v", files)
	}
}

func TestReadOnlyAdapterCodeSearchEnsembleExecutionTrace(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	workspace := t.TempDir()
	storageRoot := t.TempDir()

	writeTestFile(t, filepath.Join(workspace, "internal", "agent", "daemon", "handlers.go"), "package daemon\n\nfunc HandleAsk() {\n\trunEngine()\n}\n")
	writeTestFile(t, filepath.Join(workspace, "internal", "agent", "runtime", "runtime.go"), "package runtime\n\nfunc runEngine() {}\n")

	store, err := repoindex.Open(ctx, storageRoot, workspace)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	repoKey := store.RepoKey()

	handlerID := repoindex.NamespacedID(repoKey, "sym:agent/daemon:HandleAsk")
	runnerID := repoindex.NamespacedID(repoKey, "sym:agent/runtime:runEngine")
	nodes := []repoindex.Node{
		{
			ID:        handlerID,
			Kind:      repoindex.NodeSymbol,
			Pkg:       "agent/daemon",
			File:      "internal/agent/daemon/handlers.go",
			Name:      "HandleAsk",
			Summary:   "Mailbox ask handler entrypoint.",
			SpanStart: 3,
			SpanEnd:   5,
			UpdatedAt: time.Now().UTC(),
		},
		{
			ID:        runnerID,
			Kind:      repoindex.NodeSymbol,
			Pkg:       "agent/runtime",
			File:      "internal/agent/runtime/runtime.go",
			Name:      "runEngine",
			Summary:   "Runs the classic runtime engine.",
			SpanStart: 3,
			SpanEnd:   3,
			UpdatedAt: time.Now().UTC(),
		},
	}
	edges := []repoindex.Edge{
		{
			Src:    handlerID,
			Dst:    runnerID,
			Type:   repoindex.EdgeCalls,
			Weight: 1,
		},
	}
	if err := store.ReplaceAll(ctx, nodes, edges); err != nil {
		t.Fatal(err)
	}

	var cfg config.Config
	cfg.Storage.Root = storageRoot
	adapter := NewReadOnlyAdapter(cfg, workspace, "", nil, rlm.Environment{})

	out, err := adapter.ExecuteInternal(ctx, "code_search_ensemble", mustJSON(map[string]any{
		"query":     "HandleAsk execution path",
		"task_type": "execution_trace",
		"budget": map[string]any{
			"max_candidates": 4,
			"max_files":      2,
		},
	}))
	if err != nil {
		t.Fatalf("code_search_ensemble: %v", err)
	}
	if out["task_type"] != codeSearchTaskExecutionTrace {
		t.Fatalf("task_type=%v", out["task_type"])
	}
	metadata, ok := out["metadata"].(map[string]any)
	if !ok {
		t.Fatalf("metadata=%T", out["metadata"])
	}
	lanesRaw, _ := json.Marshal(metadata["lanes_used"])
	var lanes []string
	if err := json.Unmarshal(lanesRaw, &lanes); err != nil {
		t.Fatalf("decode lanes: %v", err)
	}
	foundGraph := false
	for _, lane := range lanes {
		if lane == "repo_graph" {
			foundGraph = true
			break
		}
	}
	if !foundGraph {
		t.Fatalf("expected repo_graph lane: %v", lanes)
	}
	rawCallPaths, _ := json.Marshal(out["call_paths"])
	var callPaths []map[string]any
	if err := json.Unmarshal(rawCallPaths, &callPaths); err != nil {
		t.Fatalf("decode call_paths: %v", err)
	}
	if len(callPaths) == 0 {
		t.Fatalf("call_paths=%v", out["call_paths"])
	}
}

func TestReadOnlyAdapterCodeSearchEnsembleUsesExactCodeProbe(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	workspace := t.TempDir()
	storageRoot := t.TempDir()

	writeTestFile(t, filepath.Join(workspace, "internal", "rlm", "env", "memory_ensemble.go"), "package env\n\nfunc describe() string { return \"memory_ensemble_retrieve\" }\n")

	store, err := repoindex.Open(ctx, storageRoot, workspace)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := store.ReplaceAll(ctx, nil, nil); err != nil {
		t.Fatal(err)
	}

	var cfg config.Config
	cfg.Storage.Root = storageRoot
	adapter := NewReadOnlyAdapter(cfg, workspace, "", nil, rlm.Environment{})

	out, err := adapter.ExecuteInternal(ctx, "code_search_ensemble", mustJSON(map[string]any{
		"query":     "Where does memory_ensemble_retrieve live?",
		"task_type": "file_locate",
		"constraints": map[string]any{
			"require_grounding": true,
		},
	}))
	if err != nil {
		t.Fatalf("code_search_ensemble: %v", err)
	}
	rawFiles, _ := json.Marshal(out["files"])
	var files []codeSearchEvidenceFile
	if err := json.Unmarshal(rawFiles, &files); err != nil {
		t.Fatalf("decode files: %v", err)
	}
	if len(files) == 0 {
		t.Fatalf("files=%v", out["files"])
	}
	if files[0].Path != "internal/rlm/env/memory_ensemble.go" {
		t.Fatalf("files=%+v", files)
	}
	metadata, ok := out["metadata"].(map[string]any)
	if !ok {
		t.Fatalf("metadata=%T", out["metadata"])
	}
	rawLanes, _ := json.Marshal(metadata["lanes_used"])
	var lanes []string
	if err := json.Unmarshal(rawLanes, &lanes); err != nil {
		t.Fatalf("decode lanes: %v", err)
	}
	found := false
	for _, lane := range lanes {
		if lane == "exact_probe" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected exact_probe lane in %v", lanes)
	}
}

func TestReadOnlyAdapterCodeSearchEnsembleEmitsTelemetry(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	obsDir := t.TempDir()
	observability.SetObsDirForTesting(obsDir)
	defer observability.SetObsDirForTesting("")

	workspace := t.TempDir()
	storageRoot := t.TempDir()

	writeTestFile(t, filepath.Join(workspace, "internal", "rlm", "env", "memory_ensemble.go"), "package env\n\nfunc describe() string { return \"memory_ensemble_retrieve\" }\n")
	store, err := repoindex.Open(ctx, storageRoot, workspace)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := store.ReplaceAll(ctx, nil, nil); err != nil {
		t.Fatal(err)
	}

	var cfg config.Config
	cfg.Storage.Root = storageRoot
	adapter := NewReadOnlyAdapter(cfg, workspace, "", nil, rlm.Environment{})

	out, err := adapter.ExecuteInternal(ctx, "code_search_ensemble", mustJSON(map[string]any{
		"query":     "Where does memory_ensemble_retrieve live?",
		"task_type": "file_locate",
		"constraints": map[string]any{
			"require_grounding": true,
		},
	}))
	if err != nil {
		t.Fatalf("code_search_ensemble: %v", err)
	}
	metadata, ok := out["metadata"].(map[string]any)
	if !ok {
		t.Fatalf("metadata=%T", out["metadata"])
	}
	telemetry, ok := metadata["telemetry"].(map[string]any)
	if !ok {
		t.Fatalf("telemetry=%T", metadata["telemetry"])
	}
	if intValue(telemetry["total_tool_calls"]) == 0 {
		t.Fatalf("telemetry=%v", telemetry)
	}
	entries, err := observability.QueryEventRecords(ctx, observability.EventQueryOptions{
		ObsDir:          obsDir,
		OperationPrefix: "context.code_search_ensemble",
		Limit:           10,
	})
	if err != nil {
		t.Fatalf("QueryEventRecords() error = %v", err)
	}
	if len(entries) == 0 {
		t.Fatal("expected code_search_ensemble wide event")
	}
	if entries[0].Data["total_tool_calls"] == nil {
		t.Fatalf("event data=%v", entries[0].Data)
	}
}

func TestExtractSkillTokenUsage(t *testing.T) {
	t.Parallel()

	usage := extractSkillTokenUsage(map[string]any{
		"summary": map[string]any{
			"model":       "test-model",
			"tokens_used": 123,
		},
	})
	if usage == nil {
		t.Fatal("expected usage")
		return
	}
	if usage.Model != "test-model" || usage.TotalTokens != 123 {
		t.Fatalf("usage=%+v", usage)
	}
}

func TestReadOnlyAdapterCodeSearchEnsembleRegistrationTrace(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	workspace := t.TempDir()
	storageRoot := t.TempDir()

	writeTestFile(t, filepath.Join(workspace, "cmd", "foxctl", "cmd", "eval.go"), "package cmd\n\nfunc newEvalCommand() *cobra.Command {\n\tcmd := &cobra.Command{}\n\tcmd.AddCommand(newEvalCodeSearchEnsembleCommand())\n\treturn cmd\n}\n")
	writeTestFile(t, filepath.Join(workspace, "cmd", "foxctl", "cmd", "eval_code_search_ensemble.go"), "package cmd\n\nfunc newEvalCodeSearchEnsembleCommand() *cobra.Command {\n\treturn &cobra.Command{}\n}\n")

	store, err := repoindex.Open(ctx, storageRoot, workspace)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := store.ReplaceAll(ctx, nil, nil); err != nil {
		t.Fatal(err)
	}

	var cfg config.Config
	cfg.Storage.Root = storageRoot
	adapter := NewReadOnlyAdapter(cfg, workspace, "", nil, rlm.Environment{})

	out, err := adapter.ExecuteInternal(ctx, "code_search_ensemble", mustJSON(map[string]any{
		"query":     "Where does the eval command register code-search-ensemble?",
		"task_type": "registration_trace",
		"constraints": map[string]any{
			"require_grounding": true,
		},
	}))
	if err != nil {
		t.Fatalf("code_search_ensemble: %v", err)
	}
	rawFiles, _ := json.Marshal(out["files"])
	var files []codeSearchEvidenceFile
	if err := json.Unmarshal(rawFiles, &files); err != nil {
		t.Fatalf("decode files: %v", err)
	}
	if len(files) == 0 {
		t.Fatalf("files=%v", out["files"])
	}
	if files[0].Path != "cmd/foxctl/cmd/eval.go" {
		t.Fatalf("files=%+v", files)
	}
}

func TestReadOnlyAdapterCodeSearchEnsembleChangeImpact(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	workspace := t.TempDir()
	storageRoot := t.TempDir()

	writeTestFile(t, filepath.Join(workspace, "internal", "rlm", "env", "code_search_ensemble.go"), "package env\n\nfunc codeSearchEnsemble() {}\nfunc helper() {}\n")
	writeTestFile(t, filepath.Join(workspace, "internal", "rlm", "env", "adapter.go"), "package env\n\nfunc execute() { codeSearchEnsemble() }\n")
	writeTestFile(t, filepath.Join(workspace, "cmd", "foxctl", "cmd", "eval_code_search_ensemble.go"), "package cmd\n\nfunc runSingleCodeSearchEnsembleEval() {}\n")

	store, err := repoindex.Open(ctx, storageRoot, workspace)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	repoKey := store.RepoKey()
	targetID := repoindex.NamespacedID(repoKey, "sym:env:codeSearchEnsemble")
	adapterID := repoindex.NamespacedID(repoKey, "sym:env:execute")
	evalID := repoindex.NamespacedID(repoKey, "sym:cmd:runSingleCodeSearchEnsembleEval")
	nodes := []repoindex.Node{
		{
			ID:        targetID,
			Kind:      repoindex.NodeSymbol,
			Pkg:       "env",
			File:      "internal/rlm/env/code_search_ensemble.go",
			Name:      "codeSearchEnsemble",
			Summary:   "Main ensemble implementation.",
			SpanStart: 3,
			SpanEnd:   3,
			UpdatedAt: time.Now().UTC(),
		},
		{
			ID:        adapterID,
			Kind:      repoindex.NodeSymbol,
			Pkg:       "env",
			File:      "internal/rlm/env/adapter.go",
			Name:      "execute",
			Summary:   "Adapter entrypoint calling codeSearchEnsemble.",
			SpanStart: 3,
			SpanEnd:   3,
			UpdatedAt: time.Now().UTC(),
		},
		{
			ID:        evalID,
			Kind:      repoindex.NodeSymbol,
			Pkg:       "cmd",
			File:      "cmd/foxctl/cmd/eval_code_search_ensemble.go",
			Name:      "runSingleCodeSearchEnsembleEval",
			Summary:   "CLI eval path for code_search_ensemble.",
			SpanStart: 3,
			SpanEnd:   3,
			UpdatedAt: time.Now().UTC(),
		},
	}
	edges := []repoindex.Edge{
		{Src: adapterID, Dst: targetID, Type: repoindex.EdgeCalls, Weight: 1},
		{Src: evalID, Dst: adapterID, Type: repoindex.EdgeCalls, Weight: 1},
	}
	if err := store.ReplaceAll(ctx, nodes, edges); err != nil {
		t.Fatal(err)
	}

	var cfg config.Config
	cfg.Storage.Root = storageRoot
	adapter := NewReadOnlyAdapter(cfg, workspace, "", nil, rlm.Environment{})

	out, err := adapter.ExecuteInternal(ctx, "code_search_ensemble", mustJSON(map[string]any{
		"query":     "If you change codeSearchEnsemble, which files are directly impacted?",
		"task_type": "change_impact",
		"constraints": map[string]any{
			"require_grounding": true,
		},
	}))
	if err != nil {
		t.Fatalf("code_search_ensemble: %v", err)
	}
	rawFiles, _ := json.Marshal(out["files"])
	var files []codeSearchEvidenceFile
	if err := json.Unmarshal(rawFiles, &files); err != nil {
		t.Fatalf("decode files: %v", err)
	}
	if len(files) < 2 {
		t.Fatalf("files=%+v", files)
	}
	paths := map[string]bool{}
	for _, file := range files {
		paths[file.Path] = true
	}
	if !paths["internal/rlm/env/code_search_ensemble.go"] || !paths["internal/rlm/env/adapter.go"] {
		t.Fatalf("files=%+v", files)
	}
	metadata, ok := out["metadata"].(map[string]any)
	if !ok {
		t.Fatalf("metadata=%T", out["metadata"])
	}
	rawLanes, _ := json.Marshal(metadata["lanes_used"])
	var lanes []string
	if err := json.Unmarshal(rawLanes, &lanes); err != nil {
		t.Fatalf("decode lanes: %v", err)
	}
	foundImpact := false
	for _, lane := range lanes {
		if lane == "impact_graph" {
			foundImpact = true
			break
		}
	}
	if !foundImpact {
		t.Fatalf("expected impact_graph lane in %v", lanes)
	}
}

func TestReadOnlyAdapterCodeSearchEnsembleExecutionTracePromotesBridgeFile(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	workspace := t.TempDir()
	storageRoot := t.TempDir()

	writeTestFile(t, filepath.Join(workspace, "internal", "rlm", "env", "code_search_ensemble.go"), "package env\n\nfunc codeSearchEnsemble() {}\n")
	writeTestFile(t, filepath.Join(workspace, "internal", "rlm", "env", "adapter.go"), "package env\n\ntype ReadOnlyAdapter struct{}\n\nfunc NewReadOnlyAdapter() *ReadOnlyAdapter { return &ReadOnlyAdapter{} }\n\nfunc (a *ReadOnlyAdapter) Execute(name string) { if name == \"code_search_ensemble\" { codeSearchEnsemble() } }\n")
	writeTestFile(t, filepath.Join(workspace, "cmd", "foxctl", "cmd", "eval_code_search_ensemble.go"), "package cmd\n\nfunc runSingleCodeSearchEnsembleEval() { adapter := NewReadOnlyAdapter(); adapter.Execute(\"code_search_ensemble\") }\n")

	store, err := repoindex.Open(ctx, storageRoot, workspace)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	repoKey := store.RepoKey()
	targetID := repoindex.NamespacedID(repoKey, "sym:env:codeSearchEnsemble")
	adapterID := repoindex.NamespacedID(repoKey, "sym:env:execute")
	evalID := repoindex.NamespacedID(repoKey, "sym:cmd:runSingleCodeSearchEnsembleEval")
	nodes := []repoindex.Node{
		{
			ID:        targetID,
			Kind:      repoindex.NodeSymbol,
			Pkg:       "env",
			File:      "internal/rlm/env/code_search_ensemble.go",
			Name:      "codeSearchEnsemble",
			Summary:   "Main ensemble implementation.",
			SpanStart: 3,
			SpanEnd:   3,
			UpdatedAt: time.Now().UTC(),
		},
		{
			ID:        adapterID,
			Kind:      repoindex.NodeSymbol,
			Pkg:       "env",
			File:      "internal/rlm/env/adapter.go",
			Name:      "execute",
			Summary:   "Adapter entrypoint calling codeSearchEnsemble.",
			SpanStart: 3,
			SpanEnd:   3,
			UpdatedAt: time.Now().UTC(),
		},
		{
			ID:        evalID,
			Kind:      repoindex.NodeSymbol,
			Pkg:       "cmd",
			File:      "cmd/foxctl/cmd/eval_code_search_ensemble.go",
			Name:      "runSingleCodeSearchEnsembleEval",
			Summary:   "CLI eval path for code_search_ensemble.",
			SpanStart: 3,
			SpanEnd:   3,
			UpdatedAt: time.Now().UTC(),
		},
	}
	edges := []repoindex.Edge{
		{Src: evalID, Dst: adapterID, Type: repoindex.EdgeCalls, Weight: 1},
		{Src: adapterID, Dst: targetID, Type: repoindex.EdgeCalls, Weight: 1},
	}
	if err := store.ReplaceAll(ctx, nodes, edges); err != nil {
		t.Fatal(err)
	}

	var cfg config.Config
	cfg.Storage.Root = storageRoot
	adapter := NewReadOnlyAdapter(cfg, workspace, "", nil, rlm.Environment{})

	out, err := adapter.ExecuteInternal(ctx, "code_search_ensemble", mustJSON(map[string]any{
		"query":     "Which files connect runSingleCodeSearchEnsembleEval to code_search_ensemble execution?",
		"task_type": "execution_trace",
		"constraints": map[string]any{
			"require_grounding": true,
		},
		"budget": map[string]any{
			"max_candidates": 8,
			"max_files":      4,
		},
	}))
	if err != nil {
		t.Fatalf("code_search_ensemble: %v", err)
	}
	rawFiles, _ := json.Marshal(out["files"])
	var files []codeSearchEvidenceFile
	if err := json.Unmarshal(rawFiles, &files); err != nil {
		t.Fatalf("decode files: %v", err)
	}
	paths := map[string]bool{}
	for _, file := range files {
		paths[file.Path] = true
	}
	if !paths["cmd/foxctl/cmd/eval_code_search_ensemble.go"] || !paths["internal/rlm/env/adapter.go"] || !paths["internal/rlm/env/code_search_ensemble.go"] {
		t.Fatalf("files=%+v", files)
	}
	metadata, ok := out["metadata"].(map[string]any)
	if !ok {
		t.Fatalf("metadata=%T", out["metadata"])
	}
	rawLanes, _ := json.Marshal(metadata["lanes_used"])
	var lanes []string
	if err := json.Unmarshal(rawLanes, &lanes); err != nil {
		t.Fatalf("decode lanes: %v", err)
	}
	foundExecutionGraph := false
	for _, lane := range lanes {
		if lane == "execution_graph" {
			foundExecutionGraph = true
			break
		}
	}
	if !foundExecutionGraph {
		t.Fatalf("expected execution_graph lane: %v", lanes)
	}
	rawTrace, _ := json.Marshal(metadata["candidate_trace"])
	var candidateTrace []codeSearchCandidateTrace
	if err := json.Unmarshal(rawTrace, &candidateTrace); err != nil {
		t.Fatalf("decode candidate_trace: %v", err)
	}
	if len(candidateTrace) == 0 {
		t.Fatalf("candidate_trace=%v", metadata["candidate_trace"])
	}
	rawBridgeQueries, _ := json.Marshal(metadata["bridge_queries"])
	var bridgeQueries []string
	if err := json.Unmarshal(rawBridgeQueries, &bridgeQueries); err != nil {
		t.Fatalf("decode bridge_queries: %v", err)
	}
	if len(bridgeQueries) == 0 {
		t.Fatalf("bridge_queries=%v", metadata["bridge_queries"])
	}
}

func TestReadOnlyAdapterCodeSearchEnsembleSymbolInspectUsesGoDefinitions(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	workspace := t.TempDir()
	storageRoot := t.TempDir()

	writeTestFile(t, filepath.Join(workspace, "internal", "rlm", "env", "code_search_ensemble.go"), "package env\n\ntype codeSearchEnsembleInput struct {\n\tQuery string\n}\n\ntype codeSearchEvidenceFile struct{}\n")
	writeTestFile(t, filepath.Join(workspace, "internal", "rlm", "env", "tool_exec.go"), "package env\n\nfunc useEvidence() { _ = \"codeSearchEnsembleInput\" }\n")

	var cfg config.Config
	cfg.Storage.Root = storageRoot
	adapter := NewReadOnlyAdapter(cfg, workspace, "", nil, rlm.Environment{})

	out, err := adapter.ExecuteInternal(ctx, "code_search_ensemble", mustJSON(map[string]any{
		"query":     "Which file defines codeSearchEnsembleInput?",
		"task_type": "symbol_inspect",
		"constraints": map[string]any{
			"require_grounding": true,
		},
		"budget": map[string]any{
			"max_candidates": 8,
			"max_files":      3,
		},
	}))
	if err != nil {
		t.Fatalf("code_search_ensemble: %v", err)
	}
	rawFiles, _ := json.Marshal(out["files"])
	var files []codeSearchEvidenceFile
	if err := json.Unmarshal(rawFiles, &files); err != nil {
		t.Fatalf("decode files: %v", err)
	}
	if len(files) == 0 || files[0].Path != "internal/rlm/env/code_search_ensemble.go" {
		t.Fatalf("files=%+v", files)
	}
	if !stringSliceHas(files[0].ConfirmedBy, "symbol_definition") {
		t.Fatalf("confirmed_by=%v", files[0].ConfirmedBy)
	}
	rawSymbols, _ := json.Marshal(out["symbols"])
	var symbols []codeSearchEvidenceSymbol
	if err := json.Unmarshal(rawSymbols, &symbols); err != nil {
		t.Fatalf("decode symbols: %v", err)
	}
	found := false
	for _, symbol := range symbols {
		if symbol.Path == "internal/rlm/env/code_search_ensemble.go" && symbol.Symbol == "codeSearchEnsembleInput" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("symbols=%+v", symbols)
	}
}

func TestReadOnlyAdapterLocalCodeProbeSearchSymbolInspectUsesDefinition(t *testing.T) {
	t.Parallel()

	workspace := t.TempDir()
	writeTestFile(t, filepath.Join(workspace, "internal", "rlm", "env", "adapter.go"), "package env\n\ntype ReadOnlyAdapter struct{}\n")
	writeTestFile(t, filepath.Join(workspace, "internal", "rlm", "env", "tool_exec.go"), "package env\n\nfunc useAdapter(value ReadOnlyAdapter) {}\n")

	adapter := NewReadOnlyAdapter(config.Config{}, workspace, "", nil, rlm.Environment{})
	hits, err := adapter.localCodeProbeSearch("Which file defines ReadOnlyAdapter?", codeSearchTaskSymbolInspect, 4)
	if err != nil {
		t.Fatalf("localCodeProbeSearch: %v", err)
	}
	if len(hits) == 0 {
		t.Fatalf("hits=%v", hits)
	}
	if got := hits[0].Hit.Path; got != "internal/rlm/env/adapter.go" {
		t.Fatalf("top hit=%q hits=%+v", got, hits)
	}
	if hits[0].Hit.Symbol != "ReadOnlyAdapter" {
		t.Fatalf("symbol=%q", hits[0].Hit.Symbol)
	}
}

func TestReadOnlyAdapterSubsystemSiblingClosureFindsRequiredRole(t *testing.T) {
	t.Parallel()

	workspace := t.TempDir()
	writeTestFile(t, filepath.Join(workspace, "internal", "context", "contextengine", "context_gather.go"), "package contextengine\n\nfunc GatherContext() {}\n")
	writeTestFile(t, filepath.Join(workspace, "internal", "context", "contextengine", "context_reduce.go"), "package contextengine\n\nfunc ReduceEvidencePacksToBundle() {}\n")
	writeTestFile(t, filepath.Join(workspace, "internal", "context", "contextengine", "context_verify.go"), "package contextengine\n\ntype RuntimeCertificate struct{}\n\nfunc CertifyRuntimeBundle() {}\n")

	adapter := NewReadOnlyAdapter(config.Config{}, workspace, "", nil, rlm.Environment{})
	seeds := []contextengine.CodeSearchHit{
		{Path: "internal/context/contextengine/context_gather.go", Score: 0.9},
		{Path: "internal/context/contextengine/context_reduce.go", Score: 0.85},
	}
	hits := adapter.localSubsystemSiblingClosureHits(
		"map the context runtime subsystem",
		"subsystem_map",
		[]string{"runtime certification"},
		seeds,
		6,
	)
	if len(hits) == 0 {
		t.Fatalf("closure hits empty")
	}
	if got := hits[0].Hit.Path; got != "internal/context/contextengine/context_verify.go" {
		t.Fatalf("top closure hit=%q hits=%+v", got, hits)
	}
	if hits[0].Hit.Metadata["candidate_role"] != "structural_support" {
		t.Fatalf("metadata=%v", hits[0].Hit.Metadata)
	}
	matches, ok := hits[0].Hit.Metadata["matched_terms"].([]string)
	if !ok || !containsString(matches, "certify") {
		t.Fatalf("matched_terms=%T %v", hits[0].Hit.Metadata["matched_terms"], hits[0].Hit.Metadata["matched_terms"])
	}
}

func TestReadOnlyAdapterLiveOverlayFindsUntrackedCoverageFile(t *testing.T) {
	t.Parallel()

	workspace := t.TempDir()
	runGitForTest(t, workspace, "init")
	writeTestFile(t, filepath.Join(workspace, "internal", "context", "contextengine", "coverage.go"), "package contextengine\n\ntype CoverageRequirement struct{}\n\nfunc selectEvidenceCoverageAware() {}\n")

	adapter := NewReadOnlyAdapter(config.Config{}, workspace, "", nil, rlm.Environment{})
	hits, err := adapter.liveOverlayCodeSearchHits(context.Background(), "coverage-aware selector", []string{"CoverageRequirement"}, 8)
	if err != nil {
		t.Fatalf("liveOverlayCodeSearchHits: %v", err)
	}
	if len(hits) == 0 {
		t.Fatalf("hits empty")
	}
	if got := hits[0].Hit.Path; got != "internal/context/contextengine/coverage.go" {
		t.Fatalf("top hit=%q hits=%+v", got, hits)
	}
	if hits[0].Hit.Metadata["source"] != "live_overlay" {
		t.Fatalf("metadata=%v", hits[0].Hit.Metadata)
	}
	coverageIDs, ok := hits[0].Hit.Metadata["coverage_requirement_ids"].([]string)
	if !ok || len(coverageIDs) == 0 {
		t.Fatalf("coverage ids=%T %v", hits[0].Hit.Metadata["coverage_requirement_ids"], hits[0].Hit.Metadata["coverage_requirement_ids"])
	}
}

func runGitForTest(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, string(output))
	}
}

func writeTestFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
