package execrunner

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jkatigb/agentctl/internal/domain/skill"
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

func TestRunnerTimeout(t *testing.T) {
	bin := buildHelper(t, `package main
import "time"
func main() {
	time.Sleep(10 * time.Second)
}`)
	runner := Runner{
		Manifest: skill.Manifest{
			Distribution: skill.Distribution{Type: "exec"},
			Metadata:     skill.Metadata{Name: "test/timeout", Version: "0.1.0"},
			Capabilities: skill.Capabilities{Network: "none"},
		},
		Binary: bin,
		Options: Options{
			Timeout: 100 * time.Millisecond, // Very short timeout
		},
	}
	_, _, err := runner.Run(context.Background(), []byte{})
	if err == nil {
		t.Fatal("expected timeout error, got nil")
	}
	// Should get a context deadline exceeded error
	if !strings.Contains(err.Error(), "deadline") && !strings.Contains(err.Error(), "killed") {
		t.Fatalf("expected timeout/deadline error, got: %v", err)
	}
}

func TestRunnerNoTimeout(t *testing.T) {
	// Test that when no timeout is specified, runner runs without timeout
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
			Metadata:     skill.Metadata{Name: "test/no-timeout", Version: "0.1.0"},
			Capabilities: skill.Capabilities{Network: "none"},
		},
		Binary: bin,
		// No timeout specified - should run without timeout
	}
	stdout, _, err := runner.Run(context.Background(), []byte("test"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(stdout) != "test" {
		t.Fatalf("expected 'test', got %s", stdout)
	}
}

func TestRunnerWorkdirIsolation(t *testing.T) {
	bin := buildHelper(t, `package main
import (
	"fmt"
	"os"
)
func main() {
	wd, _ := os.Getwd()
	fmt.Print(wd)
}`)
	runner := Runner{
		Manifest: skill.Manifest{
			Distribution: skill.Distribution{Type: "exec"},
			Metadata:     skill.Metadata{Name: "test/workdir", Version: "0.1.0"},
			Capabilities: skill.Capabilities{Network: "none"},
		},
		Binary: bin,
	}
	stdout, _, err := runner.Run(context.Background(), []byte{})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	workdir := string(stdout)
	// Verify it's in a temp directory
	if !strings.HasPrefix(workdir, os.TempDir()) && !strings.Contains(workdir, "agentctl-skill-") {
		t.Fatalf("expected workdir in temp, got: %s", workdir)
	}
}

func TestRunnerEnvironmentVariables(t *testing.T) {
	bin := buildHelper(t, `package main
import (
	"fmt"
	"os"
)
func main() {
	fmt.Printf("NAME=%s VERSION=%s", os.Getenv("SKILL_NAME"), os.Getenv("SKILL_VERSION"))
}`)
	runner := Runner{
		Manifest: skill.Manifest{
			Distribution: skill.Distribution{Type: "exec"},
			Metadata:     skill.Metadata{Name: "test/envvars", Version: "1.2.3"},
			Capabilities: skill.Capabilities{Network: "none"},
		},
		Binary: bin,
	}
	stdout, _, err := runner.Run(context.Background(), []byte{})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	output := string(stdout)
	if output != "NAME=test/envvars VERSION=1.2.3" {
		t.Fatalf("expected env vars, got: %s", output)
	}
}

func TestRunnerNetworkPolicyRejection(t *testing.T) {
	bin := buildHelper(t, `package main
func main() {}`)

	runner := Runner{
		Manifest: skill.Manifest{
			Distribution: skill.Distribution{Type: "exec"},
			Metadata:     skill.Metadata{Name: "test/network", Version: "0.1.0"},
			Capabilities: skill.Capabilities{Network: "egress"}, // Not supported by exec runner
		},
		Binary: bin,
	}
	_, _, err := runner.Run(context.Background(), []byte{})
	if err == nil {
		t.Fatal("expected network capability error, got nil")
	}
	if !strings.Contains(err.Error(), "network capability") {
		t.Fatalf("expected network capability error, got: %v", err)
	}
}
