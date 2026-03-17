package contextplane

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/jkatigb/agentctl/internal/indexing/repoindex"
	"github.com/jkatigb/agentctl/internal/indexing/rerank"
	"github.com/jkatigb/agentctl/internal/storage/obsidianindex"
)

func TestRetrieveBlendsACAStateAndVaultHits(t *testing.T) {
	ctx := context.Background()
	workspace := t.TempDir()
	storageRoot := t.TempDir()
	store := NewWorkspaceStore(workspace)
	if _, err := store.SaveTopOfMind(TopOfMind{
		WorkspaceID:   "ws-test",
		Objective:     "Compact handoff work",
		Phase:         "design",
		ActiveTaskIDs: []string{"T-1042"},
		RelevantRefs:  []string{"path:notes/patterns/compact-handoff-pattern.md"},
		UpdatedAt:     time.Date(2026, 3, 10, 0, 0, 0, 0, time.UTC),
	}); err != nil {
		t.Fatalf("SaveTopOfMind: %v", err)
	}
	index, err := obsidianindex.Open(ctx, storageRoot, retrievalFixtureVaultRoot(t))
	if err != nil {
		t.Fatalf("Open index: %v", err)
	}
	defer index.Close()
	if _, err := index.Rebuild(ctx, retrievalFixtureVaultRoot(t)); err != nil {
		t.Fatalf("Rebuild: %v", err)
	}
	result, err := store.Retrieve(ctx, index, nil, nil, "Compact Handoff Pattern", 5)
	if err != nil {
		t.Fatalf("Retrieve: %v", err)
	}
	if len(result.VaultHits) == 0 {
		t.Fatalf("expected vault hits")
	}
	if result.VaultHits[0].Title != "Compact Handoff Pattern" {
		t.Fatalf("top hit=%q", result.VaultHits[0].Title)
	}
	if result.Weights.Pattern == 0 || result.Weights.Canonical == 0 {
		t.Fatalf("expected explicit weights in retrieval result")
	}
}

func TestRetrieveBoostsCodeLinkedNotesFromRepoIndex(t *testing.T) {
	ctx := context.Background()
	workspace := t.TempDir()
	storageRoot := t.TempDir()
	store := NewWorkspaceStore(workspace)
	if _, err := store.SaveTopOfMind(TopOfMind{
		WorkspaceID: "ws-test",
		Objective:   "WorkspaceStore work",
		Phase:       "design",
		UpdatedAt:   time.Date(2026, 3, 10, 0, 0, 0, 0, time.UTC),
	}); err != nil {
		t.Fatalf("SaveTopOfMind: %v", err)
	}
	index, err := obsidianindex.Open(ctx, storageRoot, retrievalFixtureVaultRoot(t))
	if err != nil {
		t.Fatalf("Open index: %v", err)
	}
	defer index.Close()
	if _, err := index.Rebuild(ctx, retrievalFixtureVaultRoot(t)); err != nil {
		t.Fatalf("Rebuild vault: %v", err)
	}

	noRepo, err := store.Retrieve(ctx, index, nil, nil, "WorkspaceStore", 5)
	if err != nil {
		t.Fatalf("Retrieve without repo: %v", err)
	}

	repo, err := repoindex.Open(ctx, storageRoot, workspace)
	if err != nil {
		t.Fatalf("Open repo index: %v", err)
	}
	defer repo.Close()
	if err := repo.ReplaceAll(ctx, []repoindex.Node{
		{
			ID:        repoindex.FileID(repo.RepoKey(), "internal/contextplane", "store.go"),
			Kind:      repoindex.NodeFile,
			Pkg:       "internal/contextplane",
			File:      "internal/contextplane/store.go",
			Name:      "store.go",
			UpdatedAt: time.Now().UTC(),
		},
		{
			ID:        repoindex.SymbolID(repo.RepoKey(), "internal/contextplane", "WorkspaceStore"),
			Kind:      repoindex.NodeSymbol,
			Pkg:       "internal/contextplane",
			File:      "internal/contextplane/store.go",
			Name:      "WorkspaceStore",
			Signature: "type WorkspaceStore struct",
			UpdatedAt: time.Now().UTC(),
		},
	}, nil); err != nil {
		t.Fatalf("ReplaceAll repo index: %v", err)
	}

	withRepo, err := store.Retrieve(ctx, index, repo, nil, "WorkspaceStore", 5)
	if err != nil {
		t.Fatalf("Retrieve with repo: %v", err)
	}
	if len(noRepo.VaultHits) == 0 || len(withRepo.VaultHits) == 0 {
		t.Fatalf("expected vault hits in both retrieval runs")
	}
	if withRepo.VaultHits[0].Score <= noRepo.VaultHits[0].Score {
		t.Fatalf("expected repo-aware retrieval to boost score: without=%d with=%d", noRepo.VaultHits[0].Score, withRepo.VaultHits[0].Score)
	}
}

func TestRetrieveWithOptions_ControlOnlySkipsVaultHits(t *testing.T) {
	ctx := context.Background()
	workspace := t.TempDir()
	storageRoot := t.TempDir()
	store := NewWorkspaceStore(workspace)
	if _, err := store.SaveTopOfMind(TopOfMind{
		WorkspaceID:  "ws-test",
		Objective:    "Compact handoff work",
		Phase:        "design",
		RelevantRefs: []string{"path:notes/patterns/compact-handoff-pattern.md"},
		UpdatedAt:    time.Date(2026, 3, 10, 0, 0, 0, 0, time.UTC),
	}); err != nil {
		t.Fatalf("SaveTopOfMind: %v", err)
	}
	index, err := obsidianindex.Open(ctx, storageRoot, retrievalFixtureVaultRoot(t))
	if err != nil {
		t.Fatalf("Open index: %v", err)
	}
	defer index.Close()
	if _, err := index.Rebuild(ctx, retrievalFixtureVaultRoot(t)); err != nil {
		t.Fatalf("Rebuild: %v", err)
	}
	result, err := store.RetrieveWithOptions(ctx, index, nil, nil, "Compact Handoff Pattern", 5, RetrievalOptions{
		IncludeTopOfMindResult:  true,
		IncludeLatestHandoff:    true,
		IncludeVaultHits:        false,
		UseRelevantRefBoost:     false,
		UseHandoffRefBoost:      false,
		UseCodeHints:            false,
		UseSemanticVaultSearch:  false,
		IncludeControlPlaneRefs: true,
	})
	if err != nil {
		t.Fatalf("RetrieveWithOptions: %v", err)
	}
	if len(result.VaultHits) != 0 {
		t.Fatalf("expected no vault hits, got %d", len(result.VaultHits))
	}
}

func TestRetrieveWithOptions_CanonicalOnlyFiltersRawHits(t *testing.T) {
	ctx := context.Background()
	workspace := t.TempDir()
	storageRoot := t.TempDir()
	store := NewWorkspaceStore(workspace)
	if _, err := store.SaveTopOfMind(TopOfMind{
		WorkspaceID: "ws-test",
		Objective:   "Compact handoff work",
		Phase:       "design",
		UpdatedAt:   time.Date(2026, 3, 10, 0, 0, 0, 0, time.UTC),
	}); err != nil {
		t.Fatalf("SaveTopOfMind: %v", err)
	}
	index, err := obsidianindex.Open(ctx, storageRoot, retrievalFixtureVaultRoot(t))
	if err != nil {
		t.Fatalf("Open index: %v", err)
	}
	defer index.Close()
	if _, err := index.Rebuild(ctx, retrievalFixtureVaultRoot(t)); err != nil {
		t.Fatalf("Rebuild: %v", err)
	}
	result, err := store.RetrieveWithOptions(ctx, index, nil, nil, "Compact Handoff Pattern", 5, RetrievalOptions{
		IncludeTopOfMindResult: false,
		IncludeLatestHandoff:   false,
		IncludeVaultHits:       true,
		UseSemanticVaultSearch: true,
		AllowedTrusts:          []string{"canonical", "reviewed"},
	})
	if err != nil {
		t.Fatalf("RetrieveWithOptions: %v", err)
	}
	for _, hit := range result.VaultHits {
		if hit.Trust != "canonical" && hit.Trust != "reviewed" {
			t.Fatalf("unexpected trust %q in hit %s", hit.Trust, hit.Path)
		}
	}
}

func TestLoadRetrievalOptions_PackageFallbackFromPolicy(t *testing.T) {
	ctx := context.Background()
	workspace := t.TempDir()
	store := NewWorkspaceStore(workspace)
	layout, err := store.EnsureLayout()
	if err != nil {
		t.Fatalf("EnsureLayout: %v", err)
	}
	body := []byte("aca:\n  package_note_fallback: true\n  co_change_prior: true\n  co_change_commit_limit: 12\n  co_change_max_files_per_commit: 7\n  co_change_half_life_days: 45\n  continuity_bundles: false\n")
	if err := os.WriteFile(layout.RetrievalPolicyPath, body, 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	opts := store.loadRetrievalOptions()
	if !opts.UsePackageNoteFallback {
		t.Fatalf("expected package_note_fallback to be enabled from retrieval policy")
	}
	if !opts.UseCoChangePrior {
		t.Fatalf("expected co_change_prior to be enabled from retrieval policy")
	}
	if opts.CoChangeCommitLimit != 12 {
		t.Fatalf("expected co_change_commit_limit=12, got %d", opts.CoChangeCommitLimit)
	}
	if opts.CoChangeMaxFilesPerCommit != 7 {
		t.Fatalf("expected co_change_max_files_per_commit=7, got %d", opts.CoChangeMaxFilesPerCommit)
	}
	if opts.CoChangeHalfLifeDays != 45 {
		t.Fatalf("expected co_change_half_life_days=45, got %d", opts.CoChangeHalfLifeDays)
	}
	if opts.UseContinuityBundles {
		t.Fatalf("expected continuity_bundles=false from retrieval policy")
	}
	_ = ctx
}

func TestFilterNoisyPaths(t *testing.T) {
	got := filterNoisyPaths([]string{
		"internal/contextplane/store.go",
		"bun.lock",
		"vendor/github.com/x/y.go",
		"node_modules/pkg/index.js",
		"go.sum",
		"internal/contextplane/dispatch.go",
	})
	if len(got) != 2 {
		t.Fatalf("filtered paths=%v", got)
	}
	if got[0] != "internal/contextplane/store.go" || got[1] != "internal/contextplane/dispatch.go" {
		t.Fatalf("unexpected filtered paths=%v", got)
	}
}

func TestContinuityBundlePaths(t *testing.T) {
	top := &TopOfMind{
		RelevantRefs: []string{
			"path:internal/contextplane/store.go",
			"note:ignored",
		},
	}
	handoff := &HandoffRecord{
		Handoff: Handoff{
			FilesTouched: []string{"internal/contextplane/dispatch.go"},
			EvidenceRefs: []string{"path:internal/contextplane/retrieval.go"},
		},
	}
	opts := DefaultRetrievalOptions()
	opts.UseContinuityBundles = true
	paths := continuityBundlePaths(top, handoff, opts)
	if len(paths) != 3 {
		t.Fatalf("continuity paths=%v", paths)
	}
}

func TestRetrieveWithOptions_CoChangePriorBoostsRelatedRepoPaths(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}

	ctx := context.Background()
	workspace := t.TempDir()
	initCoChangeGitRepo(t, workspace)

	store := NewWorkspaceStore(workspace)
	if _, err := store.SaveTopOfMind(TopOfMind{
		WorkspaceID:  "ws-test",
		Objective:    "Refine ACA retrieval",
		Phase:        "design",
		RelevantRefs: []string{"path:internal/contextplane/store.go"},
		UpdatedAt:    time.Date(2026, 3, 10, 0, 0, 0, 0, time.UTC),
	}); err != nil {
		t.Fatalf("SaveTopOfMind: %v", err)
	}

	vaultRoot := createCoChangeVault(t)
	storageRoot := t.TempDir()
	index, err := obsidianindex.Open(ctx, storageRoot, vaultRoot)
	if err != nil {
		t.Fatalf("Open index: %v", err)
	}
	defer index.Close()
	if _, err := index.Rebuild(ctx, vaultRoot); err != nil {
		t.Fatalf("Rebuild vault: %v", err)
	}

	baseOpts := RetrievalOptions{
		IncludeTopOfMindResult: true,
		IncludeLatestHandoff:   true,
		IncludeVaultHits:       true,
		UseRelevantRefBoost:    false,
		UseHandoffRefBoost:     false,
		UseCodeHints:           true,
		UseSemanticVaultSearch: false,
		UseCoChangePrior:       false,
		UseQueryTypeBias:       false,
	}
	base, err := store.RetrieveWithOptions(ctx, index, nil, nil, "retrieval cluster", 5, baseOpts)
	if err != nil {
		t.Fatalf("RetrieveWithOptions base: %v", err)
	}

	boostedOpts := baseOpts
	boostedOpts.UseCoChangePrior = true
	boostedOpts.CoChangeCommitLimit = 20
	boosted, err := store.RetrieveWithOptions(ctx, index, nil, nil, "retrieval cluster", 5, boostedOpts)
	if err != nil {
		t.Fatalf("RetrieveWithOptions boosted: %v", err)
	}

	baseScores := map[string]int{}
	for _, hit := range base.VaultHits {
		baseScores[filepath.Base(hit.Path)] = hit.Score
	}
	boostedScores := map[string]int{}
	for _, hit := range boosted.VaultHits {
		boostedScores[filepath.Base(hit.Path)] = hit.Score
	}

	if boostedScores["dispatch-note.md"] <= baseScores["dispatch-note.md"] {
		t.Fatalf("expected dispatch note score to increase: base=%d boosted=%d", baseScores["dispatch-note.md"], boostedScores["dispatch-note.md"])
	}
	if boostedScores["dispatch-note.md"] <= boostedScores["other-note.md"] {
		t.Fatalf("expected co-changed note to outrank unrelated note: dispatch=%d other=%d", boostedScores["dispatch-note.md"], boostedScores["other-note.md"])
	}
}

func TestDetectContradictions(t *testing.T) {
	ctx := context.Background()
	workspace := t.TempDir()
	storageRoot := t.TempDir()
	store := NewWorkspaceStore(workspace)
	if _, err := store.SaveTopOfMind(TopOfMind{
		WorkspaceID: "ws-test",
		Objective:   "Review contradictions",
		Phase:       "review",
		UpdatedAt:   time.Date(2026, 3, 10, 0, 0, 0, 0, time.UTC),
	}); err != nil {
		t.Fatalf("SaveTopOfMind: %v", err)
	}
	if _, err := store.AppendTension(Tension{
		Kind:        "contradiction",
		Statement:   "Pattern notes conflict with the current runtime write policy.",
		Impact:      "high",
		Status:      "open",
		Count:       2,
		RelatedRefs: []string{"note:write-policy"},
	}); err != nil {
		t.Fatalf("AppendTension: %v", err)
	}
	index, err := obsidianindex.Open(ctx, storageRoot, retrievalFixtureVaultRoot(t))
	if err != nil {
		t.Fatalf("Open index: %v", err)
	}
	defer index.Close()
	if _, err := index.Rebuild(ctx, retrievalFixtureVaultRoot(t)); err != nil {
		t.Fatalf("Rebuild: %v", err)
	}
	findings, err := store.DetectContradictions(ctx, index, nil, nil, 5)
	if err != nil {
		t.Fatalf("DetectContradictions: %v", err)
	}
	if len(findings) == 0 {
		t.Fatalf("expected contradiction findings")
	}
	if !findings[0].BlockedPromotion {
		t.Fatalf("expected high-impact contradiction to block promotion")
	}
}

func TestRerankVaultHits(t *testing.T) {
	hits := []RetrievalHit{
		{Path: "a.md", Title: "Less Relevant", Score: 10},
		{Path: "b.md", Title: "More Relevant", Score: 8},
	}
	cfg := rerank.Config{
		Enabled: true,
		TopK:    2,
		FinalK:  2,
	}
	provider := fakeReranker{
		results: []rerank.RankedResult{
			{ID: "b.md", FinalScore: 1.0},
			{ID: "a.md", FinalScore: 0.1},
		},
	}
	out, used, model, err := rerankVaultHits(context.Background(), "query", hits, RetrievalWeights{SemanticMatch: 2}, provider, cfg)
	if err != nil {
		t.Fatalf("rerankVaultHits: %v", err)
	}
	if !used {
		t.Fatalf("expected semantic rerank to be used")
	}
	if model != "fake-reranker" {
		t.Fatalf("model=%q", model)
	}
	if out[0].Path != "b.md" {
		t.Fatalf("expected reranked hit first, got %s", out[0].Path)
	}
}

func retrievalFixtureVaultRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatalf("runtime caller unavailable")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(file), "..", "tools", "obsidian", "testdata", "vaults", "basic"))
}

func createCoChangeVault(t *testing.T) string {
	t.Helper()
	vault := t.TempDir()
	writeNote := func(rel, body string) {
		t.Helper()
		path := filepath.Join(vault, rel)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("mkdir note dir: %v", err)
		}
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatalf("write note %s: %v", rel, err)
		}
	}
	writeNote("notes/patterns/dispatch-note.md", `---
type: pattern
trust: canonical
paths:
  - internal/contextplane/dispatch.go
---
# Retrieval Cluster Dispatch

retrieval cluster
`)
	writeNote("notes/patterns/other-note.md", `---
type: pattern
trust: canonical
paths:
  - internal/other/other.go
---
# Retrieval Cluster Other

retrieval cluster
`)
	return vault
}

func initCoChangeGitRepo(t *testing.T, workspace string) {
	t.Helper()
	runGit := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", workspace}, args...)...)
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=Test User",
			"GIT_AUTHOR_EMAIL=test@example.com",
			"GIT_COMMITTER_NAME=Test User",
			"GIT_COMMITTER_EMAIL=test@example.com",
		)
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v (%s)", args, err, string(out))
		}
	}
	writeFile := func(rel, body string) {
		t.Helper()
		path := filepath.Join(workspace, rel)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("mkdir repo dir: %v", err)
		}
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatalf("write file %s: %v", rel, err)
		}
	}

	runGit("init", "-b", "main")

	writeFile("internal/contextplane/store.go", "package contextplane\n\nvar StoreSeed = 1\n")
	writeFile("internal/contextplane/dispatch.go", "package contextplane\n\nvar DispatchSeed = 1\n")
	writeFile("internal/other/other.go", "package other\n\nvar OtherSeed = 1\n")
	runGit("add", ".")
	runGit("commit", "-m", "initial graph")

	for i := 2; i <= 3; i++ {
		writeFile("internal/contextplane/store.go", fmt.Sprintf("package contextplane\n\nvar StoreSeed = %d\n", i))
		writeFile("internal/contextplane/dispatch.go", fmt.Sprintf("package contextplane\n\nvar DispatchSeed = %d\n", i))
		runGit("add", "internal/contextplane/store.go", "internal/contextplane/dispatch.go")
		runGit("commit", "-m", fmt.Sprintf("couple store and dispatch %d", i))
	}

	writeFile("internal/other/other.go", "package other\n\nvar OtherSeed = 2\n")
	runGit("add", "internal/other/other.go")
	runGit("commit", "-m", "touch unrelated file")
}

type fakeReranker struct {
	results []rerank.RankedResult
}

func (f fakeReranker) Rerank(_ context.Context, _ string, _ []rerank.Candidate, _ int) ([]rerank.RankedResult, error) {
	return f.results, nil
}

func (f fakeReranker) Model() string {
	return "fake-reranker"
}
