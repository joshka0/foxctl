package execrunner

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"testing/quick"
	"time"

	"github.com/joshka0/foxctl/internal/domain/skill"
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

func TestRunnerRejectsNegativeTimeout(t *testing.T) {
	runner := Runner{
		Manifest: skill.Manifest{
			Distribution: skill.Distribution{Type: "exec"},
			Metadata:     skill.Metadata{Name: "test/negative-timeout", Version: "0.1.0"},
			Capabilities: skill.Capabilities{Network: "none"},
		},
		Binary: "unused",
		Options: Options{
			Timeout: -time.Second,
		},
	}
	_, _, err := runner.Run(context.Background(), []byte{})
	if err == nil {
		t.Fatal("expected negative timeout error, got nil")
	}
	if !strings.Contains(err.Error(), "timeout must be non-negative") {
		t.Fatalf("expected negative timeout error, got: %v", err)
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
	if !strings.HasPrefix(workdir, os.TempDir()) && !strings.Contains(workdir, "foxctl-skill-") {
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

func TestRunnerPreservesExplicitFoxctlEnvironment(t *testing.T) {
	bin := buildHelper(t, `package main
import (
	"fmt"
	"os"
)
func main() {
	fmt.Printf("HOME=%s\nFOXCTL_HOME=%s\nFOXCTL_BIN=%s\nCAS=%s",
		os.Getenv("HOME"),
		os.Getenv("FOXCTL_HOME"),
		os.Getenv("FOXCTL_BIN"),
		os.Getenv("FOXCTL_CAS_AUTO_MIGRATE"),
	)
}`)
	runner := Runner{
		Manifest: skill.Manifest{
			Distribution: skill.Distribution{Type: "exec"},
			Metadata:     skill.Metadata{Name: "test/env-contract", Version: "0.1.0"},
			Capabilities: skill.Capabilities{Network: "none"},
		},
		Binary: bin,
		Options: Options{
			Env: []string{
				"HOME=/custom/home",
				"FOXCTL_HOME=/custom/foxctl-home",
				"FOXCTL_BIN=/custom/bin/foxctl",
				"FOXCTL_CAS_AUTO_MIGRATE=1",
			},
		},
	}

	stdout, stderr, err := runner.Run(context.Background(), []byte{})
	if err != nil {
		t.Fatalf("run: %v (stderr=%s)", err, stderr)
	}
	want := strings.Join([]string{
		"HOME=/custom/home",
		"FOXCTL_HOME=/custom/foxctl-home",
		"FOXCTL_BIN=/custom/bin/foxctl",
		"CAS=1",
	}, "\n")
	if string(stdout) != want {
		t.Fatalf("environment output:\n%s\nwant:\n%s", stdout, want)
	}
}

func TestEnsureEnvVarPropertyPreservesExistingNonEmptyValues(t *testing.T) {
	property := func(rawKey, rawExisting, rawReplacement string) bool {
		key := generatedEnvName(rawKey)
		existing := generatedEnvValue(rawExisting)
		replacement := generatedEnvValue(rawReplacement)
		if existing == replacement {
			replacement += "-replacement"
		}

		got := ensureEnvVar([]string{key + "=" + existing}, key, replacement)
		if value := getEnvVar(got, key); value != existing {
			t.Logf("getEnvVar(%v, %q)=%q want existing %q", got, key, value, existing)
			return false
		}
		return len(got) == 1
	}

	if err := quick.Check(property, &quick.Config{MaxCount: 250}); err != nil {
		t.Fatalf("ensure env preserve property failed: %v", err)
	}
}

func TestEnsureEnvVarPropertyFillsMissingOrEmptyValues(t *testing.T) {
	property := func(rawKey, rawValue string, startsEmpty bool) bool {
		key := generatedEnvName(rawKey)
		value := generatedEnvValue(rawValue)
		env := []string{"OTHER=1"}
		if startsEmpty {
			env = append(env, key+"=")
		}

		got := ensureEnvVar(env, key, value)
		if gotValue := getEnvVar(got, key); gotValue != value {
			t.Logf("getEnvVar(%v, %q)=%q want %q", got, key, gotValue, value)
			return false
		}
		return countEnvKeys(got, key) == 1
	}

	if err := quick.Check(property, &quick.Config{MaxCount: 250}); err != nil {
		t.Fatalf("ensure env fill property failed: %v", err)
	}
}

func TestRunnerNetworkPolicyRejectionUnknownCapability(t *testing.T) {
	bin := buildHelper(t, `package main
func main() {}`)

	runner := Runner{
		Manifest: skill.Manifest{
			Distribution: skill.Distribution{Type: "exec"},
			Metadata:     skill.Metadata{Name: "test/network", Version: "0.1.0"},
			Capabilities: skill.Capabilities{Network: "ingress"}, // Unknown capability should be rejected
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

func TestRunnerAllowsEgressNetworkCapability(t *testing.T) {
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
			Metadata:     skill.Metadata{Name: "test/network-egress", Version: "0.1.0"},
			Capabilities: skill.Capabilities{Network: "egress"},
		},
		Binary: bin,
	}
	stdout, stderr, err := runner.Run(context.Background(), []byte("hello"))
	if err != nil {
		t.Fatalf("run with egress capability failed: %v (stderr=%s)", err, stderr)
	}
	if string(stdout) != "hello" {
		t.Fatalf("expected echo with egress capability, got %s", stdout)
	}
}

func generatedEnvName(raw string) string {
	name := make([]rune, 0, len(raw))
	for _, r := range raw {
		switch {
		case r >= 'A' && r <= 'Z':
			name = append(name, r)
		case r >= 'a' && r <= 'z':
			name = append(name, r-'a'+'A')
		case r >= '0' && r <= '9':
			name = append(name, r)
		case r == '_':
			name = append(name, r)
		}
	}
	if len(name) == 0 || (name[0] >= '0' && name[0] <= '9') {
		name = append([]rune{'K'}, name...)
	}
	return string(name)
}

func generatedEnvValue(raw string) string {
	value := strings.Map(func(r rune) rune {
		switch r {
		case 0, '\n', '\r', '=':
			return '-'
		default:
			return r
		}
	}, raw)
	if value == "" {
		return "value"
	}
	return fmt.Sprintf("v-%s", value)
}

func countEnvKeys(env []string, key string) int {
	count := 0
	prefix := key + "="
	for _, kv := range env {
		if strings.HasPrefix(kv, prefix) {
			count++
		}
	}
	return count
}
