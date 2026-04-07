package hot

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	refscope "github.com/jkatigb/agentctl/internal/refactor/scope"
	refsnapshot "github.com/jkatigb/agentctl/internal/refactor/snapshot"
	refstatus "github.com/jkatigb/agentctl/internal/refactor/status"
)

func TestBuildSymbolHotspotsMatchesChangedFunctionByLineRange(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	root := t.TempDir()
	storageRoot := filepath.Join(root, "storage")
	repo := filepath.Join(root, "repo")

	mustWriteHotFile(t, filepath.Join(repo, "internal", "a.go"), `package internal

func Alpha() int {
	return 1
}

func Beta() int {
	return 2
}
`)
	runHotGit(t, ctx, repo, "init")
	runHotGit(t, ctx, repo, "add", ".")
	runHotGit(t, ctx, repo, "-c", "user.email=test@example.com", "-c", "user.name=Test", "commit", "-m", "init")

	mustWriteHotFile(t, filepath.Join(repo, "internal", "a.go"), `package internal

func Alpha() int {
	value := 1
	return value + 1
}

func Beta() int {
	return 2
}
`)
	runHotGit(t, ctx, repo, "add", "internal/a.go")
	runHotGit(t, ctx, repo, "-c", "user.email=test@example.com", "-c", "user.name=Test", "commit", "-m", "touch alpha")

	scope := refscope.Scope{
		Workspace: repo,
		RepoRoot:  repo,
		Path:      "internal",
		Absolute:  filepath.Join(repo, "internal"),
		Mode:      "explicit",
		Language:  "go",
		Detected:  []string{"go"},
		IsDir:     true,
	}
	now := time.Now().UTC()
	hotResult, err := Build(ctx, storageRoot, Options{
		Scope:      scope,
		Since:      "HEAD~1",
		MaxResults: 10,
		Now:        now,
	})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	fileIndex := make(map[string]FileHotspot, len(hotResult.Files))
	for _, file := range hotResult.Files {
		fileIndex[file.Path] = file
	}

	snapshotPayload, err := refsnapshot.Builder{}.Build(ctx, refsnapshot.Input{
		SnapshotID: "refsnap-test",
		CreatedAt:  now,
		Scope:      scope,
		Status: refstatus.Status{
			Mode:  refstatus.ModeParserOnly,
			Scope: scope,
		},
	})
	if err != nil {
		t.Fatalf("snapshot build: %v", err)
	}

	hotspots, err := BuildSymbolHotspots(ctx, scope, "HEAD~1", snapshotPayload, fileIndex, now)
	if err != nil {
		t.Fatalf("BuildSymbolHotspots: %v", err)
	}
	if len(hotspots) == 0 {
		t.Fatal("expected symbol hotspots")
	}
	if hotspots[0].Name != "Alpha" {
		t.Fatalf("top symbol=%q want Alpha (all=%#v)", hotspots[0].Name, hotspots)
	}
	for _, item := range hotspots {
		if item.Name == "Beta" {
			t.Fatalf("unchanged symbol Beta unexpectedly marked hot: %#v", hotspots)
		}
	}
}
