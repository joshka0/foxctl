package frontmatter

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestResolveWorkflowPath(t *testing.T) {
	got, err := ResolveWorkflowPath("", "/tmp/work")
	if err != nil {
		t.Fatalf("ResolveWorkflowPath() error = %v", err)
	}
	want := filepath.Clean("/tmp/work/WORKFLOW.md")
	if got != want {
		t.Fatalf("path=%q want %q", got, want)
	}

	explicit, err := ResolveWorkflowPath("./WORKFLOW.md", "")
	if err != nil {
		t.Fatalf("ResolveWorkflowPath(explicit) error = %v", err)
	}
	if !strings.HasSuffix(explicit, "WORKFLOW.md") {
		t.Fatalf("explicit path=%q", explicit)
	}
}

func TestResolveWorkflowPath_ExplicitRelativeUsesProvidedCWD(t *testing.T) {
	got, err := ResolveWorkflowPath("configs/WORKFLOW.md", "/tmp/project")
	if err != nil {
		t.Fatalf("ResolveWorkflowPath() error = %v", err)
	}
	want := filepath.Clean("/tmp/project/configs/WORKFLOW.md")
	if got != want {
		t.Fatalf("path=%q want %q", got, want)
	}
}

func TestReloader_InitialLoadSuccess(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "WORKFLOW.md")
	markdown := `---
tracker:
  kind: linear
  api_key: lin-key
  project_slug: AG-1
workspace:
  root: ./workspace
---
You are the runner.`
	if err := os.WriteFile(path, []byte(markdown), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	r := NewReloader(path, DecodeOptions{}, nil)
	snap, err := r.Reload()
	if err != nil {
		t.Fatalf("Reload() error = %v", err)
	}
	if snap.Version != 1 {
		t.Fatalf("version=%d want 1", snap.Version)
	}
	if snap.Config.Tracker.ProjectSlug != "AG-1" {
		t.Fatalf("project_slug=%q", snap.Config.Tracker.ProjectSlug)
	}
	if !strings.HasSuffix(snap.Config.Workspace.Root, string(filepath.Separator)+"workspace") {
		t.Fatalf("workspace.root=%q", snap.Config.Workspace.Root)
	}
}

func TestReloader_RetainsLastKnownGoodOnInvalidReload(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "WORKFLOW.md")
	good := `---
tracker:
  kind: linear
  api_key: lin-key
  project_slug: AG-1
---
Prompt`
	if err := os.WriteFile(path, []byte(good), 0o644); err != nil {
		t.Fatalf("write good file: %v", err)
	}

	r := NewReloader(path, DecodeOptions{}, nil)
	first, err := r.Reload()
	if err != nil {
		t.Fatalf("initial reload error = %v", err)
	}

	bad := `---
tracker:
  kind: linear
  project_slug: AG-1
---
Prompt`
	if err := os.WriteFile(path, []byte(bad), 0o644); err != nil {
		t.Fatalf("write bad file: %v", err)
	}

	second, err := r.Reload()
	if err == nil {
		t.Fatal("expected retained error on invalid reload")
	}
	var retained *RetainedError
	if !errors.As(err, &retained) {
		t.Fatalf("error type=%T want *RetainedError", err)
	}
	if second.Version != first.Version {
		t.Fatalf("version changed on retained config: got=%d want=%d", second.Version, first.Version)
	}
	if second.Config.Tracker.APIKey != first.Config.Tracker.APIKey {
		t.Fatalf("api key changed after failed reload")
	}
}

func TestReloader_InitialLoadFailureHasNoSnapshot(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "WORKFLOW.md")
	bad := `---
tracker:
  kind: linear
---
Prompt`
	if err := os.WriteFile(path, []byte(bad), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	r := NewReloader(path, DecodeOptions{}, nil)
	if _, err := r.Reload(); err == nil {
		t.Fatal("expected reload error")
	}
	if _, ok := r.Current(); ok {
		t.Fatal("expected no current snapshot after initial failure")
	}
}

func TestReloader_DefaultBaseDirRelativeWorkspace(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "WORKFLOW.md")
	markdown := `---
tracker:
  kind: linear
  api_key: lin-key
  project_slug: AG-1
workspace:
  root: rel/workspace
---
Prompt`
	if err := os.WriteFile(path, []byte(markdown), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	r := NewReloader(path, DecodeOptions{}, nil)
	snap, err := r.Reload()
	if err != nil {
		t.Fatalf("Reload() error = %v", err)
	}
	want := filepath.Clean(filepath.Join(dir, "rel/workspace"))
	if snap.Config.Workspace.Root != want {
		t.Fatalf("workspace.root=%q want=%q", snap.Config.Workspace.Root, want)
	}
}

func TestReloader_CurrentReturnsImmutableSnapshotCopy(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "WORKFLOW.md")
	markdown := `---
tracker:
  kind: linear
  api_key: lin-key
  project_slug: AG-1
server:
  port: 8080
---
Prompt`
	if err := os.WriteFile(path, []byte(markdown), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	r := NewReloader(path, DecodeOptions{}, nil)
	if _, err := r.Reload(); err != nil {
		t.Fatalf("Reload() error = %v", err)
	}

	snap, ok := r.Current()
	if !ok {
		t.Fatal("expected current snapshot")
	}
	trackerRaw, ok := snap.Document.Config["tracker"].(map[string]any)
	if !ok {
		t.Fatal("tracker not found in snapshot config")
	}
	trackerRaw["kind"] = "hacked"
	if snap.Config.Server.Port == nil {
		t.Fatal("expected server.port")
	}
	*snap.Config.Server.Port = 9999

	after, ok := r.Current()
	if !ok {
		t.Fatal("expected current snapshot after mutation")
	}
	trackerAfter, ok := after.Document.Config["tracker"].(map[string]any)
	if !ok {
		t.Fatal("tracker missing after mutation")
	}
	if got := trackerAfter["kind"]; got != "linear" {
		t.Fatalf("internal tracker kind mutated: %v", got)
	}
	if after.Config.Server.Port == nil || *after.Config.Server.Port != 8080 {
		t.Fatalf("internal server.port mutated: %v", after.Config.Server.Port)
	}
}

func TestReloader_ReloadReturnIsImmutableCopy(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "WORKFLOW.md")
	markdown := `---
tracker:
  kind: linear
  api_key: lin-key
  project_slug: AG-1
server:
  port: 8080
---
Prompt`
	if err := os.WriteFile(path, []byte(markdown), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	r := NewReloader(path, DecodeOptions{}, nil)
	loaded, err := r.Reload()
	if err != nil {
		t.Fatalf("Reload() error = %v", err)
	}

	trackerRaw, ok := loaded.Document.Config["tracker"].(map[string]any)
	if !ok {
		t.Fatal("tracker missing in reload snapshot")
	}
	trackerRaw["kind"] = "hacked"
	if loaded.Config.Server.Port == nil {
		t.Fatal("expected server.port")
	}
	*loaded.Config.Server.Port = 9090

	current, ok := r.Current()
	if !ok {
		t.Fatal("expected current snapshot")
	}
	trackerCurrent, ok := current.Document.Config["tracker"].(map[string]any)
	if !ok {
		t.Fatal("tracker missing in current snapshot")
	}
	if got := trackerCurrent["kind"]; got != "linear" {
		t.Fatalf("current tracker kind mutated: %v", got)
	}
	if current.Config.Server.Port == nil || *current.Config.Server.Port != 8080 {
		t.Fatalf("current server.port mutated: %v", current.Config.Server.Port)
	}
}
