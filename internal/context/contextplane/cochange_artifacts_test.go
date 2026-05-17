package contextplane

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/joshka0/foxctl/internal/storage"
	"github.com/joshka0/foxctl/internal/storage/memory"
)

func TestBuildAndSearchCoChangeArtifacts(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}

	ctx := context.Background()
	workspace := t.TempDir()
	initArtifactGitRepo(t, workspace)

	memStore, err := memory.Open(ctx, t.TempDir(), "")
	if err != nil {
		t.Fatalf("open memory: %v", err)
	}
	defer memStore.Close()

	provider := testEmbedder{dims: 8}
	clusters, err := BuildCoChangeArtifacts(ctx, workspace, memStore, provider, CoChangeArtifactBuildOptions{
		CommitLimit:       20,
		MaxFilesPerCommit: 10,
		HalfLifeDays:      90,
		MaxClusters:       10,
		MaxNeighbors:      4,
	})
	if err != nil {
		t.Fatalf("BuildCoChangeArtifacts: %v", err)
	}
	if len(clusters) == 0 {
		t.Fatalf("expected clusters")
	}

	entries, total, err := memStore.ListFiltered(ctx, workspace, storage.MemoryListFilter{Types: []string{CoChangeClusterType}}, 20, 0)
	if err != nil {
		t.Fatalf("ListFiltered: %v", err)
	}
	if total == 0 || len(entries) == 0 {
		t.Fatalf("expected persisted cochange_cluster entries")
	}

	hits, err := SearchCoChangeArtifacts(ctx, workspace, "dispatch", 5, memStore, provider)
	if err != nil {
		t.Fatalf("SearchCoChangeArtifacts: %v", err)
	}
	if len(hits) == 0 {
		t.Fatalf("expected search hits")
	}
	if hits[0].AnchorPath == "" {
		t.Fatalf("expected anchor path in first hit")
	}
}

type testEmbedder struct {
	dims int
}

func (t testEmbedder) Embed(_ context.Context, text string) ([]float32, error) {
	if t.dims <= 0 {
		t.dims = 8
	}
	vec := make([]float32, t.dims)
	for i, r := range text {
		vec[i%t.dims] += float32((int(r)%17)+1) / 17.0
	}
	return vec, nil
}

func (t testEmbedder) EmbedBatch(ctx context.Context, texts []string) ([][]float32, error) {
	out := make([][]float32, 0, len(texts))
	for _, text := range texts {
		vec, err := t.Embed(ctx, text)
		if err != nil {
			return nil, err
		}
		out = append(out, vec)
	}
	return out, nil
}

func (t testEmbedder) Model() string { return "test-cochange" }
func (t testEmbedder) Dimensions() int {
	if t.dims <= 0 {
		return 8
	}
	return t.dims
}

func initArtifactGitRepo(t *testing.T, workspace string) {
	t.Helper()
	runGit := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", workspace}, args...)...)
		cmd.Env = append(
			os.Environ(),
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
	writeFile("internal/context/contextplane/store.go", "package contextplane\n\nvar StoreSeed = 1\n")
	writeFile("internal/context/contextplane/dispatch.go", "package contextplane\n\nvar DispatchSeed = 1\n")
	writeFile("internal/runtime/worker.go", "package runtime\n\nvar WorkerSeed = 1\n")
	runGit("add", ".")
	runGit("commit", "-m", "initial")

	for i := 2; i <= 4; i++ {
		writeFile("internal/context/contextplane/store.go", fmt.Sprintf("package contextplane\n\nvar StoreSeed = %d\n", i))
		writeFile("internal/context/contextplane/dispatch.go", fmt.Sprintf("package contextplane\n\nvar DispatchSeed = %d\n", i))
		runGit("add", "internal/context/contextplane/store.go", "internal/context/contextplane/dispatch.go")
		runGit("commit", "-m", fmt.Sprintf("couple store dispatch %d", i))
	}

	writeFile("bun.lock", "noise\n")
	writeFile("vendor/example/noisy.go", "package example\n")
	runGit("add", "bun.lock", "vendor/example/noisy.go")
	runGit("commit", "-m", "noise commit")
}
