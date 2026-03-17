package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/jkatigb/agentctl/internal/contextplane"
	"github.com/jkatigb/agentctl/internal/indexing/semantic"
	"github.com/jkatigb/agentctl/internal/platform/config"
	memorystore "github.com/jkatigb/agentctl/internal/storage/memory"
	"github.com/rs/zerolog"
)

func TestCompanionCoChangeHandler(t *testing.T) {
	ctx := context.Background()
	workspace := t.TempDir()
	cfg := config.Config{
		Storage: config.StorageSettings{Root: t.TempDir()},
		Paths:   config.Paths{CAS: t.TempDir()},
	}
	memStore, err := memorystore.Open(ctx, cfg.Storage.Root, cfg.Paths.CAS)
	if err != nil {
		t.Fatalf("open memory: %v", err)
	}
	defer memStore.Close()

	initGitRepoForCoChangeAPI(t, workspace)
	if _, err := contextplane.BuildCoChangeArtifacts(ctx, workspace, memStore, semantic.NewNoOpProvider("test", 8), contextplane.DefaultCoChangeArtifactBuildOptions()); err != nil {
		t.Fatalf("BuildCoChangeArtifacts: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/companion/cochange?workspace="+workspace+"&query=dispatch", nil)
	rr := httptest.NewRecorder()
	CompanionCoChangeHandler(cfg, zerolog.Nop()).ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body["ok"] != true {
		t.Fatalf("ok=%v", body["ok"])
	}
	if hits, ok := body["cochange_hits"].([]any); !ok || len(hits) == 0 {
		t.Fatalf("expected cochange_hits, got %v", body["cochange_hits"])
	}
}

func TestCompanionCoChangeHandler_MethodNotAllowed(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/api/companion/cochange", nil)
	rr := httptest.NewRecorder()
	CompanionCoChangeHandler(config.Config{}, zerolog.Nop()).ServeHTTP(rr, req)
	if rr.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status=%d", rr.Code)
	}
}

func initGitRepoForCoChangeAPI(t *testing.T, workspace string) {
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
			t.Fatalf("mkdir: %v", err)
		}
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatalf("write %s: %v", rel, err)
		}
	}
	runGit("init", "-b", "main")
	writeFile("internal/contextplane/store.go", "package contextplane\n\nvar StoreSeed = 1\n")
	writeFile("internal/contextplane/dispatch.go", "package contextplane\n\nvar DispatchSeed = 1\n")
	runGit("add", ".")
	runGit("commit", "-m", "initial")
	writeFile("internal/contextplane/store.go", "package contextplane\n\nvar StoreSeed = 2\n")
	writeFile("internal/contextplane/dispatch.go", "package contextplane\n\nvar DispatchSeed = 2\n")
	runGit("add", "internal/contextplane/store.go", "internal/contextplane/dispatch.go")
	runGit("commit", "-m", "couple store dispatch")
}
