package smolvm

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"testing/quick"
)

func TestExpectedPackArtifacts(t *testing.T) {
	t.Parallel()

	artifacts, err := ExpectedPackArtifacts("/tmp/output/foxctl-agent")
	if err != nil {
		t.Fatalf("ExpectedPackArtifacts() error = %v", err)
	}

	if artifacts.OutputPath != "/tmp/output/foxctl-agent" {
		t.Fatalf("output=%q", artifacts.OutputPath)
	}
	if artifacts.StubPath != "/tmp/output/foxctl-agent" {
		t.Fatalf("stub=%q", artifacts.StubPath)
	}
	if artifacts.SidecarPath != "/tmp/output/foxctl-agent.smolmachine" {
		t.Fatalf("sidecar=%q", artifacts.SidecarPath)
	}

	single, err := ExpectedPackArtifactsForMode("/tmp/output/foxctl-agent-single", true)
	if err != nil {
		t.Fatalf("ExpectedPackArtifactsForMode(single) error = %v", err)
	}
	if single.StubPath != "/tmp/output/foxctl-agent-single" || single.SidecarPath != "" || !single.SingleFile {
		t.Fatalf("single=%+v", single)
	}

	_, err = ExpectedPackArtifacts("/tmp/output/foxctl-agent.smolmachine")
	if !errors.Is(err, ErrPackOutputIsSidecar) {
		t.Fatalf("expected ErrPackOutputIsSidecar, got %v", err)
	}

	_, err = ExpectedPackArtifacts("/")
	if !errors.Is(err, ErrInvalidPackOutput) {
		t.Fatalf("expected ErrInvalidPackOutput for root output, got %v", err)
	}
}

func TestValidatePackArtifacts(t *testing.T) {
	t.Parallel()

	expected, err := ExpectedPackArtifacts("/tmp/output/foxctl-agent")
	if err != nil {
		t.Fatalf("ExpectedPackArtifacts() error = %v", err)
	}

	if err := ValidatePackArtifacts(expected, []string{
		"/tmp/output/foxctl-agent.smolmachine",
		"/tmp/output/foxctl-agent",
		"/tmp/output/extra.log",
	}); err != nil {
		t.Fatalf("ValidatePackArtifacts() unexpected error = %v", err)
	}

	err = ValidatePackArtifacts(expected, []string{"/tmp/output/foxctl-agent"})
	if !errors.Is(err, ErrMissingPackArtifacts) {
		t.Fatalf("expected ErrMissingPackArtifacts, got %v", err)
	}
	if !strings.Contains(err.Error(), "/tmp/output/foxctl-agent.smolmachine") {
		t.Fatalf("missing path not surfaced in error: %v", err)
	}

	single, err := ExpectedPackArtifactsForMode("/tmp/output/foxctl-agent-single", true)
	if err != nil {
		t.Fatalf("ExpectedPackArtifactsForMode(single) error = %v", err)
	}
	if err := ValidatePackArtifacts(single, []string{"/tmp/output/foxctl-agent-single"}); err != nil {
		t.Fatalf("ValidatePackArtifacts(single) unexpected error = %v", err)
	}
}

func TestExpectedPackArtifactsPropertyDerivesDeterministicPaths(t *testing.T) {
	t.Parallel()

	property := func(raw string, singleFile bool) bool {
		output := generatedOutputPath(raw)
		artifacts, err := ExpectedPackArtifactsForMode(output, singleFile)
		if err != nil {
			t.Logf("ExpectedPackArtifactsForMode(%q, %v) error = %v", output, singleFile, err)
			return false
		}

		cleanOutput := filepath.Clean(strings.TrimSpace(output))
		if artifacts.OutputPath != cleanOutput || artifacts.StubPath != cleanOutput {
			t.Logf("artifacts=%+v cleanOutput=%q", artifacts, cleanOutput)
			return false
		}
		if artifacts.SingleFile != singleFile {
			t.Logf("SingleFile=%v want %v", artifacts.SingleFile, singleFile)
			return false
		}
		if singleFile {
			return artifacts.SidecarPath == ""
		}
		return artifacts.SidecarPath == cleanOutput+".smolmachine"
	}

	if err := quick.Check(property, &quick.Config{MaxCount: 500}); err != nil {
		t.Fatalf("pack artifact derivation property failed: %v", err)
	}
}

func TestValidatePackArtifactsPropertyIgnoresOrderExtraAndWhitespace(t *testing.T) {
	t.Parallel()

	property := func(raw string, singleFile bool) bool {
		output := generatedOutputPath(raw)
		expected, err := ExpectedPackArtifactsForMode(output, singleFile)
		if err != nil {
			t.Logf("ExpectedPackArtifactsForMode(%q, %v) error = %v", output, singleFile, err)
			return false
		}

		produced := []string{
			"  /tmp/unrelated.log  ",
			" " + expected.StubPath + " ",
		}
		if !singleFile {
			produced = append([]string{"\t" + expected.SidecarPath + "\n"}, produced...)
		}

		if err := ValidatePackArtifacts(expected, produced); err != nil {
			t.Logf("ValidatePackArtifacts(%+v, %v) error = %v", expected, produced, err)
			return false
		}
		return true
	}

	if err := quick.Check(property, &quick.Config{MaxCount: 500}); err != nil {
		t.Fatalf("pack artifact validation property failed: %v", err)
	}
}

func generatedOutputPath(raw string) string {
	name := strings.NewReplacer(
		"/", "_",
		"\\", "_",
		"\x00", "_",
		".", "_",
		" ", "_",
		"\t", "_",
		"\n", "_",
		"\r", "_",
	).Replace(raw)
	name = strings.Trim(name, "_")
	if name == "" {
		name = "foxctl-agent"
	}
	if strings.HasSuffix(name, "smolmachine") {
		name += "-bin"
	}
	if len(name) > 48 {
		name = name[:48]
	}
	return filepath.Join("/tmp/output", name)
}
