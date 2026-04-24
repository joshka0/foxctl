package smolvm

import (
	"errors"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
)

var (
	ErrInvalidGoBuildOutput = errors.New("smolvm: go build output path is required")
	ErrInvalidMachineName   = errors.New("smolvm: machine name is required")
)

const (
	defaultGoBinary        = "go"
	defaultFoxctlPackage   = "./cmd/foxctl"
	defaultFoxctlGOOS      = "linux"
	defaultFoxctlGOARCH    = "arm64"
	defaultGuestFoxctlPath = "/usr/local/bin/foxctl"
	defaultPackageImage    = "alpine:3.20"
	defaultPackageCPU      = 2
	defaultPackageMemory   = 512
	defaultPackageOverlay  = 2
)

// CommandStepPlan captures one ordered shell command in a larger smolvm flow.
type CommandStepPlan struct {
	Name     string      `json:"name"`
	Command  CommandPlan `json:"command"`
	Optional bool        `json:"optional,omitempty"`
}

// FoxctlPackageOptions configures FoxctlPackagePlan.
type FoxctlPackageOptions struct {
	SmolVMBinary   string `json:"smolvm_binary,omitempty"`
	GoBinary       string `json:"go_binary,omitempty"`
	SourcePackage  string `json:"source_package,omitempty"`
	BuildOutput    string `json:"build_output,omitempty"`
	GOOS           string `json:"goos,omitempty"`
	GOARCH         string `json:"goarch,omitempty"`
	CGOEnabled     bool   `json:"cgo_enabled,omitempty"`
	Image          string `json:"image,omitempty"`
	MachineName    string `json:"machine_name,omitempty"`
	GuestFoxctl    string `json:"guest_foxctl,omitempty"`
	OutputPath     string `json:"output_path"`
	NoSign         bool   `json:"no_sign,omitempty"`
	Platform       string `json:"platform,omitempty"`
	CPU            int    `json:"cpu,omitempty"`
	MemoryMiB      int    `json:"memory_mib,omitempty"`
	OverlayGiB     int    `json:"overlay_gib,omitempty"`
	CleanupMachine bool   `json:"cleanup_machine,omitempty"`
}

// FoxctlPackageSequencePlan captures the deterministic command sequence needed
// to stage a Linux foxctl binary into a smolvm VM and pack it.
type FoxctlPackageSequencePlan struct {
	Steps          []CommandStepPlan `json:"steps"`
	Artifacts      PackArtifacts     `json:"artifacts"`
	MachineName    string            `json:"machine_name"`
	BuildOutput    string            `json:"build_output"`
	GuestFoxctl    string            `json:"guest_foxctl"`
	PackedRunArgv  []string          `json:"packed_run_argv"`
	CleanupMachine bool              `json:"cleanup_machine,omitempty"`
}

// FoxctlPackagePlan returns a pure, deterministic plan. It does not build,
// mutate smolvm state, or touch the filesystem.
func FoxctlPackagePlan(opts FoxctlPackageOptions) (FoxctlPackageSequencePlan, error) {
	output := strings.TrimSpace(opts.OutputPath)
	if output == "" {
		return FoxctlPackageSequencePlan{}, ErrInvalidPackOutput
	}
	artifacts, err := ExpectedPackArtifacts(output)
	if err != nil {
		return FoxctlPackageSequencePlan{}, err
	}

	buildOutput := strings.TrimSpace(opts.BuildOutput)
	if buildOutput == "" {
		buildOutput = filepath.Join(filepath.Dir(artifacts.OutputPath), "staging", "foxctl")
	}
	buildOutput = filepath.Clean(buildOutput)
	if buildOutput == "." {
		return FoxctlPackageSequencePlan{}, ErrInvalidGoBuildOutput
	}

	machineName := strings.TrimSpace(opts.MachineName)
	if machineName == "" {
		baseMachineName := NormalizeReadableID(filepath.Base(artifacts.OutputPath))
		if baseMachineName == "" {
			return FoxctlPackageSequencePlan{}, ErrInvalidMachineName
		}
		machineName = baseMachineName + "-stage"
	} else {
		machineName = NormalizeReadableID(machineName)
	}
	if machineName == "" {
		return FoxctlPackageSequencePlan{}, ErrInvalidMachineName
	}

	smolvmBinary := strings.TrimSpace(opts.SmolVMBinary)
	if smolvmBinary == "" {
		smolvmBinary = defaultSmolVMBinary
	}
	goBinary := strings.TrimSpace(opts.GoBinary)
	if goBinary == "" {
		goBinary = defaultGoBinary
	}
	sourcePackage := strings.TrimSpace(opts.SourcePackage)
	if sourcePackage == "" {
		sourcePackage = defaultFoxctlPackage
	}
	goos := strings.TrimSpace(opts.GOOS)
	if goos == "" {
		goos = defaultFoxctlGOOS
	}
	goarch := strings.TrimSpace(opts.GOARCH)
	if goarch == "" {
		goarch = defaultFoxctlGOARCH
	}
	guestFoxctl := strings.TrimSpace(opts.GuestFoxctl)
	if guestFoxctl == "" {
		guestFoxctl = defaultGuestFoxctlPath
	}
	image := strings.TrimSpace(opts.Image)
	if image == "" {
		image = defaultPackageImage
	}
	cpu := opts.CPU
	if cpu == 0 {
		cpu = defaultPackageCPU
	}
	if cpu < 0 {
		return FoxctlPackageSequencePlan{}, ErrInvalidPackCPU
	}
	memory := opts.MemoryMiB
	if memory == 0 {
		memory = defaultPackageMemory
	}
	if memory < 0 {
		return FoxctlPackageSequencePlan{}, ErrInvalidPackMemory
	}
	overlay := opts.OverlayGiB
	if overlay == 0 {
		overlay = defaultPackageOverlay
	}
	if overlay < 0 {
		return FoxctlPackageSequencePlan{}, fmt.Errorf("smolvm: package overlay_gib must be >= 0")
	}

	goBuild := CommandPlan{
		Argv: []string{goBinary, "build", "-o", buildOutput, sourcePackage},
		Env: []EnvVarPlan{
			{Name: "GOOS", Value: goos},
			{Name: "GOARCH", Value: goarch},
			{Name: "CGO_ENABLED", Value: boolEnv(opts.CGOEnabled)},
		},
		Summary: CommandPlanSummary{Mode: "go_build_foxctl"},
	}

	createFlags, createNetwork, createLimitations := buildMachineCreateNetworkFlags()
	machineCreateArgv := []string{
		smolvmBinary,
		"machine",
		"create",
		"--image", image,
		"--cpus", strconv.Itoa(cpu),
		"--mem", strconv.Itoa(memory),
		"--overlay", strconv.Itoa(overlay),
	}
	machineCreateArgv = append(machineCreateArgv, createFlags...)
	machineCreateArgv = append(machineCreateArgv, machineName)
	machineCreate := CommandPlan{
		Argv: machineCreateArgv,
		Summary: CommandPlanSummary{
			Mode: "machine_create",
			Network: NetworkPlan{
				Requested:   NetworkPolicy{Enabled: true},
				Applied:     createNetwork,
				Limitations: createLimitations,
			},
			Limitations: createLimitations,
		},
	}

	packCreate, err := PackCreateCommand(PackCreateOptions{
		SmolVMBinary: smolvmBinary,
		FromVM:       machineName,
		OutputPath:   artifacts.OutputPath,
		NoSign:       opts.NoSign,
		Platform:     strings.TrimSpace(opts.Platform),
		CPU:          cpu,
		MemoryMiB:    memory,
	})
	if err != nil {
		return FoxctlPackageSequencePlan{}, err
	}

	steps := []CommandStepPlan{
		{Name: "host_prepare_dirs", Command: commandStep("host_prepare_dirs", "mkdir", "-p", filepath.Dir(buildOutput), filepath.Dir(artifacts.OutputPath))},
		{Name: "host_go_build", Command: goBuild},
		{Name: "machine_create", Command: machineCreate},
		{Name: "machine_start", Command: commandStep("machine_start", smolvmBinary, "machine", "start", "--name", machineName)},
		{Name: "machine_copy_foxctl", Command: commandStep("machine_copy", smolvmBinary, "machine", "cp", buildOutput, machineName+":"+guestFoxctl)},
		{Name: "machine_chmod_foxctl", Command: commandStep("machine_exec", smolvmBinary, "machine", "exec", "--name", machineName, "--", "chmod", "+x", guestFoxctl)},
		{Name: "machine_verify_foxctl", Command: commandStep("machine_exec", smolvmBinary, "machine", "exec", "--name", machineName, "--", guestFoxctl, "--help")},
		{Name: "machine_stop", Command: commandStep("machine_stop", smolvmBinary, "machine", "stop", "--name", machineName)},
		{Name: "pack_create", Command: packCreate},
		{Name: "packed_verify_foxctl", Command: commandStep("packed_verify", artifacts.StubPath, "run", "--", guestFoxctl, "--help")},
	}
	if opts.CleanupMachine {
		steps = append(steps, CommandStepPlan{
			Name:     "machine_delete",
			Command:  commandStep("machine_delete", smolvmBinary, "machine", "delete", "--force", machineName),
			Optional: true,
		})
	}

	return FoxctlPackageSequencePlan{
		Steps:          steps,
		Artifacts:      artifacts,
		MachineName:    machineName,
		BuildOutput:    buildOutput,
		GuestFoxctl:    guestFoxctl,
		PackedRunArgv:  []string{artifacts.StubPath, "run", "--", guestFoxctl, "--help"},
		CleanupMachine: opts.CleanupMachine,
	}, nil
}

func buildMachineCreateNetworkFlags() ([]string, NetworkPolicy, []string) {
	applied := NetworkPolicy{
		Enabled:      true,
		AllowedCIDRs: []string{"0.0.0.0/0"},
	}
	limitations := []string{
		"smolvm machine start may pull OCI layers; local v0.5.19 DNS worked with --allow-cidr 0.0.0.0/0 during package construction.",
	}
	return []string{"--allow-cidr", "0.0.0.0/0"}, applied, limitations
}

func commandStep(mode, binary string, args ...string) CommandPlan {
	argv := []string{binary}
	argv = append(argv, args...)
	return CommandPlan{
		Argv:    argv,
		Summary: CommandPlanSummary{Mode: mode},
	}
}

func boolEnv(enabled bool) string {
	if enabled {
		return "1"
	}
	return "0"
}
