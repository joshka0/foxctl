package env

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/joshka0/foxctl/internal/domain/skill"
	"github.com/joshka0/foxctl/internal/intelligence/indexing/repoindex"
	"github.com/joshka0/foxctl/internal/platform/config"
	ws "github.com/joshka0/foxctl/internal/platform/workspace"
	"github.com/joshka0/foxctl/internal/rlm"
	"github.com/joshka0/foxctl/internal/runtime/observability"
	"github.com/joshka0/foxctl/internal/storage/cas"
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

func writeTestFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
