package runner

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/jkatigb/agentctl/internal/domain/skill"
	"github.com/jkatigb/agentctl/internal/platform/workspace"
)

func TestRunWithOptions_UsesEnvWorkspaceWhenContextMissing(t *testing.T) {
	bin := buildHelper(t, `package main
import (
	"fmt"
	"os"
)
func main() {
	wd, _ := os.Getwd()
	fmt.Print(wd)
}`)

	ws := t.TempDir()
	t.Setenv("AGENTCTL_WORKSPACE", ws)

	stdout, stderr, err := RunWithOptions(context.Background(), RunOptions{
		Manifest: skill.Manifest{
			Distribution: skill.Distribution{Type: "exec"},
			Metadata:     skill.Metadata{Name: "test/workspace-env", Version: "0.1.0"},
			Capabilities: skill.Capabilities{Network: "none"},
		},
		ArtifactPath: bin,
		Input:        []byte("{}"),
	})
	if err != nil {
		t.Fatalf("RunWithOptions() error = %v (stderr=%s)", err, stderr)
	}
	got := workspace.Normalize(string(stdout))
	want := workspace.Normalize(ws)
	if gotResolved, err := filepath.EvalSymlinks(got); err == nil {
		got = gotResolved
	}
	if wantResolved, err := filepath.EvalSymlinks(want); err == nil {
		want = wantResolved
	}
	if got != want {
		t.Fatalf("workdir = %q, want %q", got, want)
	}
}

func TestRunWithOptions_ContextWorkspaceOverridesEnv(t *testing.T) {
	bin := buildHelper(t, `package main
import (
	"fmt"
	"os"
)
func main() {
	wd, _ := os.Getwd()
	fmt.Print(wd)
}`)

	envWS := t.TempDir()
	ctxWS := t.TempDir()
	t.Setenv("AGENTCTL_WORKSPACE", envWS)

	ctx := workspace.WithContext(context.Background(), ctxWS)

	stdout, stderr, err := RunWithOptions(ctx, RunOptions{
		Manifest: skill.Manifest{
			Distribution: skill.Distribution{Type: "exec"},
			Metadata:     skill.Metadata{Name: "test/workspace-ctx", Version: "0.1.0"},
			Capabilities: skill.Capabilities{Network: "none"},
		},
		ArtifactPath: bin,
		Input:        []byte("{}"),
	})
	if err != nil {
		t.Fatalf("RunWithOptions() error = %v (stderr=%s)", err, stderr)
	}
	got := workspace.Normalize(string(stdout))
	want := workspace.Normalize(ctxWS)
	if gotResolved, err := filepath.EvalSymlinks(got); err == nil {
		got = gotResolved
	}
	if wantResolved, err := filepath.EvalSymlinks(want); err == nil {
		want = wantResolved
	}
	if got != want {
		t.Fatalf("workdir = %q, want %q", got, want)
	}
}

func buildHelper(t *testing.T, src string) string {
	t.Helper()

	dir := t.TempDir()
	srcPath := filepath.Join(dir, "main.go")
	if err := os.WriteFile(srcPath, []byte(src), 0o644); err != nil {
		t.Fatalf("write helper: %v", err)
	}
	binPath := filepath.Join(dir, "helper")
	cmd := exec.Command("go", "build", "-o", binPath, srcPath)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build helper: %v (%s)", err, out)
	}
	return binPath
}
