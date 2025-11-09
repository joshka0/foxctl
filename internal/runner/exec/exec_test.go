package execrunner

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/jkatigb/agentctl/internal/skill"
)

func TestRunnerExecutesBinary(t *testing.T) {
	bin := buildHelper(t, `package main
import (
	"io"
	"os"
)
func main() {
	io.Copy(os.Stdout, os.Stdin)
}`)
	runner := Runner{
		Manifest: skill.Manifest{
			Distribution: skill.Distribution{Type: "exec"},
			Metadata:     skill.Metadata{Name: "test/helper", Version: "0.1.0"},
			Capabilities: skill.Capabilities{Network: "none"},
		},
		Binary: bin,
	}
	stdout, stderr, err := runner.Run(context.Background(), []byte("hello"))
	if err != nil {
		t.Fatalf("run: %v (stderr=%s)", err, stderr)
	}
	if string(stdout) != "hello" {
		t.Fatalf("expected echo, got %s", stdout)
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
