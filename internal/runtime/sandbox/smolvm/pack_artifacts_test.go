package smolvm

import (
	"errors"
	"strings"
	"testing"
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
