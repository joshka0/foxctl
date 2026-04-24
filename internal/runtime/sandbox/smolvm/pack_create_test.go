package smolvm

import (
	"errors"
	"reflect"
	"strings"
	"testing"
)

func TestPackCreateCommandDeterministicPlan(t *testing.T) {
	t.Parallel()

	plan, err := PackCreateCommand(PackCreateOptions{
		SmolVMBinary: "/usr/local/bin/smolvm",
		Image:        "alpine:3.20",
		OutputPath:   "/tmp/foxctl-agent",
		NoSign:       true,
		SingleFile:   true,
		Platform:     "linux/arm64",
		CPU:          2,
		MemoryMiB:    2048,
		Smolfile:     "/tmp/Smolfile",
		Entrypoint:   "/usr/local/bin/foxctl-agent-entrypoint",
	})
	if err != nil {
		t.Fatalf("PackCreateCommand() error = %v", err)
	}

	wantArgv := []string{
		"/usr/local/bin/smolvm",
		"pack", "create",
		"--output", "/tmp/foxctl-agent",
		"--image", "alpine:3.20",
		"--oci-platform", "linux/arm64",
		"--cpus", "2",
		"--mem", "2048",
		"--smolfile", "/tmp/Smolfile",
		"--entrypoint", "/usr/local/bin/foxctl-agent-entrypoint",
		"--no-sign",
		"--single-file",
	}
	if !reflect.DeepEqual(plan.Argv, wantArgv) {
		t.Fatalf("argv=%v\nwant=%v", plan.Argv, wantArgv)
	}
	if plan.Summary.Mode != "pack_create" {
		t.Fatalf("mode=%q", plan.Summary.Mode)
	}
	if plan.Summary.PackArtifacts == nil {
		t.Fatalf("expected pack artifacts summary")
	}
	if plan.Summary.PackArtifacts.StubPath != "/tmp/foxctl-agent" {
		t.Fatalf("stub path=%q", plan.Summary.PackArtifacts.StubPath)
	}
	if plan.Summary.PackArtifacts.SidecarPath != "" {
		t.Fatalf("sidecar path=%q", plan.Summary.PackArtifacts.SidecarPath)
	}
	if !plan.Summary.PackArtifacts.SingleFile {
		t.Fatalf("expected single file artifact summary")
	}
}

func TestPackCreateCommandFromVM(t *testing.T) {
	t.Parallel()

	plan, err := PackCreateCommand(PackCreateOptions{
		FromVM:     "foxctl-agent-stage",
		OutputPath: "/tmp/foxctl-agent",
	})
	if err != nil {
		t.Fatalf("PackCreateCommand() error = %v", err)
	}
	wantArgv := []string{
		"smolvm",
		"pack", "create",
		"--output", "/tmp/foxctl-agent",
		"--from-vm", "foxctl-agent-stage",
	}
	if !containsStringsInOrder(plan.Argv, wantArgv) {
		t.Fatalf("argv=%v missing ordered tokens=%v", plan.Argv, wantArgv)
	}
}

func TestPackCreateCommandRejectsSidecarOutputPath(t *testing.T) {
	t.Parallel()

	_, err := PackCreateCommand(PackCreateOptions{
		Image:      "alpine:3.20",
		OutputPath: "/tmp/foxctl-agent.smolmachine",
	})
	if !errors.Is(err, ErrPackOutputIsSidecar) {
		t.Fatalf("expected ErrPackOutputIsSidecar, got %v", err)
	}
}

func TestPackCreateCommandValidation(t *testing.T) {
	t.Parallel()

	_, err := PackCreateCommand(PackCreateOptions{
		OutputPath: "/tmp/foxctl-agent",
	})
	if !errors.Is(err, ErrInvalidPackSource) {
		t.Fatalf("expected ErrInvalidPackSource, got %v", err)
	}

	_, err = PackCreateCommand(PackCreateOptions{
		Image:      "alpine:3.20",
		FromVM:     "foxctl-agent-stage",
		OutputPath: "/tmp/foxctl-agent",
	})
	if !errors.Is(err, ErrInvalidPackSource) {
		t.Fatalf("expected ErrInvalidPackSource, got %v", err)
	}

	_, err = PackCreateCommand(PackCreateOptions{
		Image: "alpine:3.20",
	})
	if !errors.Is(err, ErrInvalidPackOutput) {
		t.Fatalf("expected ErrInvalidPackOutput, got %v", err)
	}

	_, err = PackCreateCommand(PackCreateOptions{
		Image:      "alpine:3.20",
		OutputPath: "/tmp/foxctl-agent",
		CPU:        -1,
	})
	if !errors.Is(err, ErrInvalidPackCPU) {
		t.Fatalf("expected ErrInvalidPackCPU, got %v", err)
	}

	_, err = PackCreateCommand(PackCreateOptions{
		Image:      "alpine:3.20",
		OutputPath: "/tmp/foxctl-agent",
		MemoryMiB:  -1,
	})
	if !errors.Is(err, ErrInvalidPackMemory) {
		t.Fatalf("expected ErrInvalidPackMemory, got %v", err)
	}
}

func containsStringsInOrder(haystack, needle []string) bool {
	if len(needle) == 0 {
		return true
	}
	next := 0
	for _, item := range haystack {
		if item == needle[next] {
			next++
			if next == len(needle) {
				return true
			}
		}
	}
	return false
}

func TestPackCreateCommandNoSignOptional(t *testing.T) {
	t.Parallel()

	plan, err := PackCreateCommand(PackCreateOptions{
		Image:      "alpine:3.20",
		OutputPath: "/tmp/foxctl-agent",
	})
	if err != nil {
		t.Fatalf("PackCreateCommand() error = %v", err)
	}
	if strings.Contains(strings.Join(plan.Argv, " "), "--no-sign") {
		t.Fatalf("--no-sign should not be present when NoSign=false: %v", plan.Argv)
	}
}
