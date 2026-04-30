package env

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/joshka0/foxctl/internal/context/contextengine"
	"github.com/joshka0/foxctl/internal/intelligence/indexing/repoindex"
	"github.com/joshka0/foxctl/internal/platform/config"
	ws "github.com/joshka0/foxctl/internal/platform/workspace"
	"github.com/joshka0/foxctl/internal/rlm"
	ctxengstore "github.com/joshka0/foxctl/internal/storage/contextengine"
	"github.com/joshka0/foxctl/internal/storage/tasks"
)

func TestReadOnlyAdapterGatherContextRealWorldStores(t *testing.T) {
	ctx := context.Background()
	storageRoot := t.TempDir()
	workspaceRoot := t.TempDir()
	workspaceID := ws.ID(workspaceRoot)
	now := time.Date(2026, 4, 30, 12, 0, 0, 0, time.UTC)

	cfg := config.Config{}
	cfg.Storage.Root = storageRoot

	ceStore, err := ctxengstore.Open(ctx, storageRoot)
	if err != nil {
		t.Fatalf("open contextengine store: %v", err)
	}
	t.Cleanup(func() { _ = ceStore.Close() })

	taskStore, err := tasks.Open(ctx, storageRoot)
	if err != nil {
		t.Fatalf("open task store: %v", err)
	}
	t.Cleanup(func() { _ = taskStore.Close() })

	_, err = ceStore.UpsertClaim(ctx, contextengine.MemoryClaim{
		ID:          "claim-contextbundle-certification",
		WorkspaceID: workspaceID,
		ClaimType:   "design_decision",
		Status:      contextengine.ClaimStatusCurrent,
		Summary:     "ContextBundle certification must be runtime-owned and every ContextFact must cite evidence.",
		Confidence:  0.97,
		SourceRefs: []contextengine.EvidenceRef{{
			Type:        contextengine.RefTypeTask,
			Ref:         "task-contextbundle-certification",
			WorkspaceID: workspaceID,
		}},
		CreatedAt: now,
		UpdatedAt: now,
	})
	if err != nil {
		t.Fatalf("upsert memory claim: %v", err)
	}

	_, err = ceStore.UpsertStaleness(ctx, contextengine.StalenessMarker{
		ID:          "stale-contextbundle-certification",
		WorkspaceID: workspaceID,
		TargetRef: contextengine.EvidenceRef{
			Type:        contextengine.RefTypeMemoryClaim,
			Ref:         "claim-contextbundle-certification",
			WorkspaceID: workspaceID,
		},
		Status:         contextengine.StalenessStatusNeedsRevalidation,
		CausedByEvents: []string{"event-docs-edited"},
		CreatedAt:      now,
		UpdatedAt:      now,
	})
	if err != nil {
		t.Fatalf("upsert staleness marker: %v", err)
	}

	_, err = taskStore.Add(ctx, tasks.Task{
		ID:          "task-contextbundle-certification",
		WorkspaceID: workspaceID,
		Title:       "Implement ContextBundle certification",
		Description: "Real gather_context test should exercise memory, task, and top-of-mind sources.",
		ScopePath:   "internal/context/contextengine",
		Status:      tasks.StatusInProgress,
		CreatedAt:   now,
		Notes:       "The returned bundle must include evidence-backed facts and a runtime certificate.",
	})
	if err != nil {
		t.Fatalf("add task: %v", err)
	}

	adapter := NewReadOnlyAdapter(cfg, workspaceRoot, "", nil, rlm.Environment{
		TopOfMind: map[string]any{
			"objective": "ContextBundle certification integration test",
			"phase":     "implementation",
		},
		Tools: []rlm.Tool{{Name: "gather_context", ReadOnly: true}},
	})
	adapter.SetContextEngineStore(ceStore)
	adapter.SetTaskStore(taskStore)

	out, err := adapter.Execute(ctx, "gather_context", mustJSON(map[string]any{
		"query": "ContextBundle certification",
		"goal":  "recover design context for implementation",
		"lanes": []string{"memory", "task", "context"},
		"limit": 10,
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
	if err := bundle.Validate(); err != nil {
		t.Fatalf("bundle validate: %v\n%s", err, string(body))
	}
	if bundle.Status != contextengine.ContextBundleStatusPartial {
		t.Fatalf("bundle status=%s want partial; certificate=%#v", bundle.Status, bundle.Certificate)
	}
	if !bundle.Answerable {
		t.Fatalf("bundle answerable=false; certificate=%#v", bundle.Certificate)
	}
	if bundle.Certificate == nil || bundle.Certificate.Status != contextengine.ContextCertificateStatusPartial {
		t.Fatalf("certificate=%#v want partial", bundle.Certificate)
	}
	if len(bundle.Certificate.StaleEvidenceIDs) == 0 {
		t.Fatalf("certificate stale evidence ids empty; certificate=%#v", bundle.Certificate)
	}
	if got := bundle.SourceCoverage[string(contextengine.LaneMemory)]; got == 0 {
		t.Fatalf("memory source coverage=%d want >0; coverage=%v", got, bundle.SourceCoverage)
	}
	if got := bundle.SourceCoverage[string(contextengine.LaneTask)]; got == 0 {
		t.Fatalf("task source coverage=%d want >0; coverage=%v", got, bundle.SourceCoverage)
	}
	if got := bundle.SourceCoverage[string(contextengine.LaneContext)]; got == 0 {
		t.Fatalf("context source coverage=%d want >0; coverage=%v", got, bundle.SourceCoverage)
	}
	if len(bundle.Facts) < 3 {
		t.Fatalf("facts=%d want at least 3; bundle=%#v", len(bundle.Facts), bundle)
	}
	if len(bundle.SourcePackIDs) != 3 {
		t.Fatalf("source packs=%v want 3 explicit lane packs", bundle.SourcePackIDs)
	}
	for _, packID := range bundle.SourcePackIDs {
		if _, err := ceStore.GetEvidencePack(ctx, packID); err != nil {
			t.Fatalf("source pack %q was not persisted: %v", packID, err)
		}
	}
}

func TestReadOnlyAdapterGatherContextUsesRepoIndexBeforeSemanticSkill(t *testing.T) {
	ctx := context.Background()
	storageRoot := t.TempDir()
	workspaceRoot := t.TempDir()
	repoFile := filepath.Join(workspaceRoot, "internal", "context", "contextengine", "context_bundle.go")
	if err := os.MkdirAll(filepath.Dir(repoFile), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(repoFile, []byte(`package contextengine

// ContextBundle is the reduced certified context surface for answerers.
type ContextBundle struct {
	ID string
}
`), 0o644); err != nil {
		t.Fatal(err)
	}

	repoStore, err := repoindex.Open(ctx, storageRoot, workspaceRoot)
	if err != nil {
		t.Fatalf("open repoindex: %v", err)
	}
	repoKey := repoStore.RepoKey()
	if err := repoStore.ReplaceAll(ctx, []repoindex.Node{
		{
			ID:        repoindex.FileID(repoKey, "go:internal/context/contextengine", "internal/context/contextengine/context_bundle.go"),
			Kind:      repoindex.NodeFile,
			Pkg:       "go:internal/context/contextengine",
			File:      "internal/context/contextengine/context_bundle.go",
			Name:      "context_bundle.go",
			Summary:   "Defines ContextBundle for certified context gathering.",
			SpanStart: 1,
		},
		{
			ID:        repoindex.SymbolID(repoKey, "go:internal/context/contextengine", "type:ContextBundle"),
			Kind:      repoindex.NodeSymbol,
			Pkg:       "go:internal/context/contextengine",
			File:      "internal/context/contextengine/context_bundle.go",
			Name:      "ContextBundle",
			Summary:   "ContextBundle is the reduced certified context surface passed to answerers.",
			SpanStart: 4,
			SpanEnd:   6,
		},
	}, nil); err != nil {
		t.Fatalf("replace repoindex: %v", err)
	}
	if err := repoStore.Close(); err != nil {
		t.Fatalf("close repoindex: %v", err)
	}

	cfg := config.Config{}
	cfg.Storage.Root = storageRoot
	adapter := NewReadOnlyAdapter(cfg, workspaceRoot, "", nil, rlm.Environment{
		Tools: []rlm.Tool{{Name: "gather_context", ReadOnly: true}},
	})

	out, err := adapter.Execute(ctx, "gather_context", mustJSON(map[string]any{
		"query": "ContextBundle certified context",
		"lanes": []string{"code"},
		"limit": 5,
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
	if err := bundle.Validate(); err != nil {
		t.Fatalf("bundle validate: %v\n%s", err, string(body))
	}
	if bundle.Certificate == nil || bundle.Certificate.Status != contextengine.ContextCertificateStatusCertified {
		t.Fatalf("certificate=%#v want certified", bundle.Certificate)
	}
	if got := bundle.SourceCoverage[string(contextengine.LaneCode)]; got == 0 {
		t.Fatalf("code source coverage=%d want >0; coverage=%v", got, bundle.SourceCoverage)
	}
	if len(bundle.Facts) == 0 {
		t.Fatalf("no facts in bundle: %#v", bundle)
	}
	joinedFacts := ""
	for _, fact := range bundle.Facts {
		joinedFacts += "\n" + fact.Fact
	}
	if !strings.Contains(joinedFacts, "ContextBundle") || !strings.Contains(joinedFacts, "excerpt:") {
		t.Fatalf("facts did not include repo index evidence plus file excerpt: %s", joinedFacts)
	}
}

func TestReadOnlyAdapterGatherContextUsesExactCodeProbe(t *testing.T) {
	ctx := context.Background()
	workspaceRoot := t.TempDir()
	writeTestFile(t, filepath.Join(workspaceRoot, "internal", "rlm", "env", "memory_ensemble.go"), "package env\n\nfunc describe() string { return \"memory_ensemble_retrieve\" }\n")

	adapter := NewReadOnlyAdapter(config.Config{}, workspaceRoot, "", nil, rlm.Environment{
		Tools: []rlm.Tool{{Name: "gather_context", ReadOnly: true}},
	})

	out, err := adapter.Execute(ctx, "gather_context", mustJSON(map[string]any{
		"query":     "Where does memory_ensemble_retrieve live?",
		"task_type": "file_locate",
		"lanes":     []string{"code"},
		"limit":     5,
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
	if err := bundle.Validate(); err != nil {
		t.Fatalf("bundle validate: %v\n%s", err, string(body))
	}
	paths := gatherContextTestPaths(bundle)
	if len(paths) == 0 || paths[0] != "internal/rlm/env/memory_ensemble.go" {
		t.Fatalf("paths=%v want exact probe file first", paths)
	}
	if bundle.Telemetry.EmittedContextChars == 0 {
		t.Fatalf("expected emitted context chars from exact probe excerpt: %#v", bundle.Telemetry)
	}
}

func TestReadOnlyAdapterGatherContextCanIncludeCandidateMemory(t *testing.T) {
	ctx := context.Background()
	storageRoot := t.TempDir()
	workspaceRoot := t.TempDir()
	workspaceID := ws.ID(workspaceRoot)
	now := time.Date(2026, 4, 30, 12, 0, 0, 0, time.UTC)

	ceStore, err := ctxengstore.Open(ctx, storageRoot)
	if err != nil {
		t.Fatalf("open contextengine store: %v", err)
	}
	t.Cleanup(func() { _ = ceStore.Close() })
	_, err = ceStore.UpsertClaim(ctx, contextengine.MemoryClaim{
		ID:          "claim-candidate-memory",
		WorkspaceID: workspaceID,
		ClaimType:   "assumption",
		Status:      contextengine.ClaimStatusCandidate,
		Summary:     "Candidate memory should remain an untrusted ContextFact.",
		Confidence:  0.6,
		CreatedAt:   now,
		UpdatedAt:   now,
	})
	if err != nil {
		t.Fatalf("upsert candidate claim: %v", err)
	}

	cfg := config.Config{}
	cfg.Storage.Root = storageRoot
	adapter := NewReadOnlyAdapter(cfg, workspaceRoot, "", nil, rlm.Environment{
		Tools: []rlm.Tool{{Name: "gather_context", ReadOnly: true}},
	})
	adapter.SetContextEngineStore(ceStore)
	out, err := adapter.Execute(ctx, "gather_context", mustJSON(map[string]any{
		"query":           "Candidate memory",
		"lanes":           []string{"memory"},
		"memory_statuses": []string{"candidate"},
		"limit":           5,
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
	if len(bundle.Facts) != 1 {
		t.Fatalf("facts=%d want 1: %#v", len(bundle.Facts), bundle.Facts)
	}
	if bundle.Facts[0].Status != contextengine.ContextFactStatusCandidate {
		t.Fatalf("fact status=%s want candidate", bundle.Facts[0].Status)
	}
	if bundle.Certificate == nil || bundle.Certificate.Status != contextengine.ContextCertificateStatusPartial {
		t.Fatalf("certificate=%#v want partial for candidate memory", bundle.Certificate)
	}
}

func TestReadOnlyAdapterGatherContextUsesRegistrationTrace(t *testing.T) {
	ctx := context.Background()
	workspaceRoot := t.TempDir()
	writeTestFile(t, filepath.Join(workspaceRoot, "cmd", "foxctl", "cmd", "eval.go"), "package cmd\n\nfunc newEvalCommand() *cobra.Command {\n\tcmd := &cobra.Command{}\n\tcmd.AddCommand(newEvalCodeSearchEnsembleCommand())\n\treturn cmd\n}\n")
	writeTestFile(t, filepath.Join(workspaceRoot, "cmd", "foxctl", "cmd", "eval_code_search_ensemble.go"), "package cmd\n\nfunc newEvalCodeSearchEnsembleCommand() *cobra.Command {\n\treturn &cobra.Command{}\n}\n")

	adapter := NewReadOnlyAdapter(config.Config{}, workspaceRoot, "", nil, rlm.Environment{
		Tools: []rlm.Tool{{Name: "gather_context", ReadOnly: true}},
	})

	out, err := adapter.Execute(ctx, "gather_context", mustJSON(map[string]any{
		"query":     "Where does the eval command register code-search-ensemble?",
		"task_type": "registration_trace",
		"lanes":     []string{"code"},
		"limit":     5,
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
	if err := bundle.Validate(); err != nil {
		t.Fatalf("bundle validate: %v\n%s", err, string(body))
	}
	paths := gatherContextTestPaths(bundle)
	if len(paths) == 0 || paths[0] != "cmd/foxctl/cmd/eval.go" {
		t.Fatalf("paths=%v want registration site first", paths)
	}
}

func TestReadOnlyAdapterGatherContextExpandsReferencedDefinitions(t *testing.T) {
	ctx := context.Background()
	workspaceRoot := t.TempDir()
	writeTestFile(t, filepath.Join(workspaceRoot, "internal", "rlm", "env", "tool_exec.go"), `package env

func gather() {
	_ = "latest_handoff"
	_ = "aca_retrieval"
	_, _ = contextengine.RetrieveContext(ctx, cfg, queryFn, query)
}
`)
	writeTestFile(t, filepath.Join(workspaceRoot, "internal", "context", "contextengine", "retrieve_context.go"), `package contextengine

func RetrieveContext(ctx context.Context, cfg LaneConfig, queryFn ContextQueryFunc, query string) (EvidencePack, error) {
	return EvidencePack{}, nil
}
`)

	adapter := NewReadOnlyAdapter(config.Config{}, workspaceRoot, "", nil, rlm.Environment{
		Tools: []rlm.Tool{{Name: "gather_context", ReadOnly: true}},
	})

	out, err := adapter.Execute(ctx, "gather_context", mustJSON(map[string]any{
		"query":             "Which code path makes durable ACA context, handoff summaries, vault-backed contextplane hits, and session recall visible?",
		"required_evidence": []string{"latest_handoff", "aca_retrieval"},
		"lanes":             []string{"code"},
		"limit":             5,
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
	paths := gatherContextTestPaths(bundle)
	if !containsString(paths, "internal/rlm/env/tool_exec.go") || !containsString(paths, "internal/context/contextengine/retrieve_context.go") {
		t.Fatalf("paths=%v want tool_exec plus referenced RetrieveContext definition", paths)
	}
}

func TestReadOnlyAdapterGatherContextBoostsCommandBuildCompanions(t *testing.T) {
	ctx := context.Background()
	workspaceRoot := t.TempDir()
	writeTestFile(t, filepath.Join(workspaceRoot, "cmd", "foxctl", "cmd", "semantic_index.go"), `package cmd

func normalizeSemanticIndexProvider(provider string) string {
	if provider == "lmstudio" {
		return "openai_compat"
	}
	return provider
}
`)
	writeTestFile(t, filepath.Join(workspaceRoot, "Makefile"), "skills-build-cgo:\n\tgo build -tags=libsqlite3 ./skills/code_semantic_search\n")

	adapter := NewReadOnlyAdapter(config.Config{}, workspaceRoot, "", nil, rlm.Environment{
		Tools: []rlm.Tool{{Name: "gather_context", ReadOnly: true}},
	})

	out, err := adapter.Execute(ctx, "gather_context", mustJSON(map[string]any{
		"query":             "Which command implementation and build target let local embedding rebuilds use LM Studio through a CGO skill binary?",
		"required_evidence": []string{"openai_compat", "skills-build-cgo"},
		"lanes":             []string{"code"},
		"limit":             5,
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
	paths := gatherContextTestPaths(bundle)
	if !containsString(paths, "cmd/foxctl/cmd/semantic_index.go") || !containsString(paths, "Makefile") {
		t.Fatalf("paths=%v want semantic_index.go plus Makefile", paths)
	}
}

func gatherContextTestPaths(bundle contextengine.ContextBundle) []string {
	paths := make([]string, 0, len(bundle.Evidence))
	for _, node := range bundle.Evidence {
		if node.Ref.Type == contextengine.RefTypePath {
			paths = append(paths, filepath.ToSlash(strings.TrimSpace(node.Ref.Ref)))
		}
		if path, ok := node.Metadata["path"].(string); ok {
			paths = append(paths, filepath.ToSlash(strings.TrimSpace(path)))
		}
	}
	seen := map[string]struct{}{}
	out := make([]string, 0, len(paths))
	for _, path := range paths {
		if path == "" {
			continue
		}
		if _, ok := seen[path]; ok {
			continue
		}
		seen[path] = struct{}{}
		out = append(out, path)
	}
	return out
}

func containsString(items []string, want string) bool {
	for _, item := range items {
		if item == want {
			return true
		}
	}
	return false
}
