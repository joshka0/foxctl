package smolvm

import (
	"errors"
	"reflect"
	"testing"
)

func TestFoxctlPackagePlanDeterministicSteps(t *testing.T) {
	t.Parallel()

	plan, err := FoxctlPackagePlan(FoxctlPackageOptions{
		SmolVMBinary:   "/usr/local/bin/smolvm",
		GoBinary:       "/usr/local/go/bin/go",
		SourcePackage:  "./cmd/foxctl",
		BuildOutput:    "/tmp/build/foxctl",
		Image:          "alpine:3.20",
		MachineName:    "foxctl-agent-stage",
		OutputPath:     "/tmp/out/foxctl-agent",
		NoSign:         true,
		Platform:       "linux/arm64",
		CPU:            2,
		MemoryMiB:      512,
		OverlayGiB:     2,
		CleanupMachine: true,
	})
	if err != nil {
		t.Fatalf("FoxctlPackagePlan() error = %v", err)
	}

	names := stepNames(plan.Steps)
	wantNames := []string{
		"host_prepare_dirs",
		"host_go_build",
		"machine_create",
		"machine_start",
		"machine_copy_foxctl",
		"machine_chmod_foxctl",
		"machine_verify_foxctl",
		"machine_stop",
		"pack_create",
		"packed_verify_foxctl",
		"machine_delete",
	}
	if !reflect.DeepEqual(names, wantNames) {
		t.Fatalf("step names=%v want=%v", names, wantNames)
	}

	prepareDirs := plan.Steps[0].Command
	wantPrepareDirs := []string{"mkdir", "-p", "/tmp/build", "/tmp/out"}
	if !reflect.DeepEqual(prepareDirs.Argv, wantPrepareDirs) {
		t.Fatalf("prepare dirs argv=%v want=%v", prepareDirs.Argv, wantPrepareDirs)
	}

	goBuild := plan.Steps[1].Command
	wantGoBuild := []string{"/usr/local/go/bin/go", "build", "-o", "/tmp/build/foxctl", "./cmd/foxctl"}
	if !reflect.DeepEqual(goBuild.Argv, wantGoBuild) {
		t.Fatalf("go build argv=%v want=%v", goBuild.Argv, wantGoBuild)
	}
	if !hasEnv(goBuild.Env, "GOOS", "linux") || !hasEnv(goBuild.Env, "GOARCH", "arm64") || !hasEnv(goBuild.Env, "CGO_ENABLED", "0") {
		t.Fatalf("go build env=%v", goBuild.Env)
	}

	machineCreate := plan.Steps[2].Command
	wantCreate := []string{
		"/usr/local/bin/smolvm",
		"machine", "create",
		"--image", "alpine:3.20",
		"--cpus", "2",
		"--mem", "512",
		"--overlay", "2",
		"--allow-cidr", "0.0.0.0/0",
		"foxctl-agent-stage",
	}
	if !reflect.DeepEqual(machineCreate.Argv, wantCreate) {
		t.Fatalf("machine create argv=%v want=%v", machineCreate.Argv, wantCreate)
	}
	if !containsSubstring(machineCreate.Summary.Limitations, "DNS worked with --allow-cidr 0.0.0.0/0") {
		t.Fatalf("machine create limitations=%v", machineCreate.Summary.Limitations)
	}

	packCreate := plan.Steps[8].Command
	wantPackCreate := []string{
		"/usr/local/bin/smolvm",
		"pack", "create",
		"--output", "/tmp/out/foxctl-agent",
		"--from-vm", "foxctl-agent-stage",
		"--oci-platform", "linux/arm64",
		"--cpus", "2",
		"--mem", "512",
		"--no-sign",
	}
	if !reflect.DeepEqual(packCreate.Argv, wantPackCreate) {
		t.Fatalf("pack create argv=%v want=%v", packCreate.Argv, wantPackCreate)
	}

	wantPackedRun := []string{"/tmp/out/foxctl-agent", "run", "--", "/usr/local/bin/foxctl", "--help"}
	if !reflect.DeepEqual(plan.PackedRunArgv, wantPackedRun) {
		t.Fatalf("packed run argv=%v want=%v", plan.PackedRunArgv, wantPackedRun)
	}
}

func TestFoxctlPackagePlanDefaults(t *testing.T) {
	t.Parallel()

	plan, err := FoxctlPackagePlan(FoxctlPackageOptions{
		OutputPath: "/tmp/out/foxctl-agent",
	})
	if err != nil {
		t.Fatalf("FoxctlPackagePlan() error = %v", err)
	}
	if plan.MachineName != "foxctl-agent-stage" {
		t.Fatalf("machine name=%q", plan.MachineName)
	}
	if plan.BuildOutput != "/tmp/out/staging/foxctl" {
		t.Fatalf("build output=%q", plan.BuildOutput)
	}
	if got := plan.Steps[2].Command.Argv; !reflect.DeepEqual(got[0:9], []string{
		"smolvm", "machine", "create",
		"--image", "alpine:3.20",
		"--cpus", "2",
		"--mem", "512",
	}) {
		t.Fatalf("machine create default prefix=%v", got)
	}
}

func TestFoxctlPackagePlanValidation(t *testing.T) {
	t.Parallel()

	_, err := FoxctlPackagePlan(FoxctlPackageOptions{})
	if !errors.Is(err, ErrInvalidPackOutput) {
		t.Fatalf("expected ErrInvalidPackOutput, got %v", err)
	}

	_, err = FoxctlPackagePlan(FoxctlPackageOptions{
		OutputPath: "/tmp/out/foxctl-agent.smolmachine",
	})
	if !errors.Is(err, ErrPackOutputIsSidecar) {
		t.Fatalf("expected ErrPackOutputIsSidecar, got %v", err)
	}
}

func stepNames(steps []CommandStepPlan) []string {
	names := make([]string, 0, len(steps))
	for _, step := range steps {
		names = append(names, step.Name)
	}
	return names
}

func hasEnv(env []EnvVarPlan, name, value string) bool {
	for _, item := range env {
		if item.Name == name && item.Value == value {
			return true
		}
	}
	return false
}
