package cmd

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/joshka0/foxctl/internal/agent/toolnames"
	domainEnvelope "github.com/joshka0/foxctl/internal/domain/envelope"
	"github.com/joshka0/foxctl/internal/protocol"
	"github.com/joshka0/foxctl/internal/runtime/sandbox/smolvm"
	"github.com/spf13/cobra"
)

func newSandboxSmolVMCommand() *cobra.Command {
	smolvmCmd := &cobra.Command{
		Use:   "smolvm",
		Short: "Plan smolvm sandbox commands",
		Long:  "Plan-only smolvm command generation for sandboxed foxctl workflows.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return writeErrorEnvelope(cmd, "sandbox/smolvm", string(protocol.ErrorCodeEARG), "smolvm subcommand is required")
		},
	}

	smolvmCmd.AddCommand(newSandboxSmolVMProbeLMStudioCommand())
	smolvmCmd.AddCommand(newSandboxSmolVMPackagePlanCommand())
	smolvmCmd.AddCommand(newSandboxSmolVMRunAgentPlanCommand())
	smolvmCmd.AddCommand(newSandboxSmolVMFoxctlPackagePlanCommand())
	smolvmCmd.AddCommand(newSandboxSmolVMFoxctlPackageCommand())
	return smolvmCmd
}

func newSandboxSmolVMPackagePlanCommand() *cobra.Command {
	var (
		smolvmBinary string
		image        string
		fromVM       string
		outputPath   string
		noSign       bool
		singleFile   bool
		platform     string
		cpus         int
		memoryMiB    int
		smolfile     string
		entrypoint   string
	)

	cmd := &cobra.Command{
		Use:     "package-plan",
		Aliases: []string{"pack-create-plan"},
		Short:   "Plan smolvm pack create argv for a foxctl sandbox image",
		RunE: func(cmd *cobra.Command, _ []string) error {
			plan, err := smolvm.PackCreateCommand(smolvm.PackCreateOptions{
				SmolVMBinary: strings.TrimSpace(smolvmBinary),
				Image:        strings.TrimSpace(image),
				FromVM:       strings.TrimSpace(fromVM),
				OutputPath:   strings.TrimSpace(outputPath),
				NoSign:       noSign,
				SingleFile:   singleFile,
				Platform:     strings.TrimSpace(platform),
				CPU:          cpus,
				MemoryMiB:    memoryMiB,
				Smolfile:     strings.TrimSpace(smolfile),
				Entrypoint:   strings.TrimSpace(entrypoint),
			})
			if err != nil {
				return writeErrorEnvelope(cmd, "sandbox/smolvm/package-plan", string(protocol.ErrorCodeEARG), err.Error())
			}

			return writeOK(cmd, "sandbox/smolvm/package-plan", buildSmolVMPlanEnvelope(plan, true), "plan", profilesCoreAgent)
		},
	}

	cmd.Flags().StringVar(&smolvmBinary, "smolvm-binary", "", "Override the smolvm binary name/path")
	cmd.Flags().StringVar(&image, "image", "", "OCI image to pack")
	cmd.Flags().StringVar(&fromVM, "from-vm", "", "Stopped smolvm VM snapshot to pack")
	cmd.Flags().StringVar(&outputPath, "output", "", "Output packed binary path; sidecar is output + .smolmachine unless --single-file")
	cmd.Flags().BoolVar(&noSign, "no-sign", false, "Skip macOS code signing")
	cmd.Flags().BoolVar(&singleFile, "single-file", false, "Pack as one executable with no .smolmachine sidecar")
	cmd.Flags().StringVar(&platform, "platform", "", "OCI platform passed as --oci-platform, e.g. linux/arm64")
	cmd.Flags().IntVar(&cpus, "cpus", 0, "Default vCPU count for the packed VM")
	cmd.Flags().IntVar(&memoryMiB, "mem", 0, "Default memory in MiB for the packed VM")
	cmd.Flags().StringVar(&smolfile, "smolfile", "", "Smolfile path")
	cmd.Flags().StringVar(&entrypoint, "entrypoint", "", "Override image entrypoint")
	_ = cmd.MarkFlagRequired("output")
	return cmd
}

func newSandboxSmolVMProbeLMStudioCommand() *cobra.Command {
	var (
		smolvmBinary        string
		image               string
		baseURL             string
		networkEnabled      bool
		localhostOnly       bool
		networkAllowedHosts []string
		networkAllowedCIDRs []string
	)

	cmd := &cobra.Command{
		Use:   "probe-lmstudio",
		Short: "Plan a smolvm machine probe for LMStudio /models",
		RunE: func(cmd *cobra.Command, _ []string) error {
			plan, err := smolvm.ProbeLMStudioCommand(smolvm.ProbeLMStudioOptions{
				SmolVMBinary: strings.TrimSpace(smolvmBinary),
				Image:        strings.TrimSpace(image),
				BaseURL:      strings.TrimSpace(baseURL),
				Network: smolvm.NetworkPolicy{
					Enabled:               networkEnabled,
					OutboundLocalhostOnly: localhostOnly,
					AllowedHosts:          networkAllowedHosts,
					AllowedCIDRs:          networkAllowedCIDRs,
				},
			})
			if err != nil {
				return writeErrorEnvelope(cmd, "sandbox/smolvm/probe-lmstudio", string(protocol.ErrorCodeEARG), err.Error())
			}

			return writeOK(cmd, "sandbox/smolvm/probe-lmstudio", buildSmolVMPlanEnvelope(plan, true), "plan", profilesCoreAgent)
		},
	}

	cmd.Flags().StringVar(&smolvmBinary, "smolvm-binary", "", "Override the smolvm binary name/path")
	cmd.Flags().StringVar(&image, "image", "", "Guest image for smolvm machine run")
	cmd.Flags().StringVar(&baseURL, "base-url", "", "Base URL to probe for /models")
	cmd.Flags().BoolVar(&networkEnabled, "net", false, "Enable guest networking for the probe plan")
	cmd.Flags().BoolVar(&localhostOnly, "outbound-localhost-only", false, "Restrict outbound egress to localhost (planner-only flag)")
	cmd.Flags().StringSliceVar(&networkAllowedHosts, "allow-host", nil, "Allow outbound host (repeatable)")
	cmd.Flags().StringSliceVar(&networkAllowedCIDRs, "allow-cidr", nil, "Allow outbound CIDR (repeatable)")
	_ = cmd.MarkFlagRequired("image")
	_ = cmd.MarkFlagRequired("base-url")
	return cmd
}

func newSandboxSmolVMRunAgentPlanCommand() *cobra.Command {
	return newSandboxSmolVMRunAgentPlanCommandWithRunner(smolvm.ExecCommandRunner{})
}

func newSandboxSmolVMRunAgentPlanCommandWithRunner(runner smolvm.CommandRunner) *cobra.Command {
	var (
		dryRun               = true
		smolvmBinary         string
		foxctlBinary         string
		sidecarPath          string
		repoPath             string
		repoMode             string
		outPath              string
		storagePath          string
		foxctlHomePath       string
		guestWorkingDir      string
		role                 string
		prompt               string
		skillsAllowRaw       string
		runID                string
		agentIDs             []string
		agentDryRun          bool
		execMode             string
		maxAutoTurns         int
		maxIterations        int
		agentTimeout         string
		askQuestion          string
		askTimeout           string
		agentSlug            string
		repoIndexWorkspace   string
		llmProvider          string
		llmBaseURL           string
		llmModel             string
		llmAPIKey            string
		llmAPIKeyPlaceholder string
		localLLMOnly         bool
		networkEnabled       bool
		localhostOnly        bool
		networkAllowedHosts  []string
		networkAllowedCIDRs  []string
	)

	cmd := &cobra.Command{
		Use:     "run-agent-plan",
		Aliases: []string{"run-agent"},
		Short:   "Plan smolvm pack run argv/env for foxctl agent spawn",
		RunE: func(cmd *cobra.Command, _ []string) error {
			normalizedSkillsAllow, err := parseSkillsAllowList(skillsAllowRaw)
			if err != nil {
				return writeErrorEnvelope(
					cmd,
					"sandbox/smolvm/run-agent-plan",
					string(protocol.ErrorCodeEARG),
					fmt.Sprintf("invalid --skills-allow: %v", err),
				)
			}

			repoWritable, err := parseRepoMode(repoMode)
			if err != nil {
				return writeErrorEnvelope(cmd, "sandbox/smolvm/run-agent-plan", string(protocol.ErrorCodeEARG), err.Error())
			}

			networkPolicy := smolvm.NetworkPolicy{
				Enabled:               networkEnabled,
				OutboundLocalhostOnly: localhostOnly,
				AllowedHosts:          networkAllowedHosts,
				AllowedCIDRs:          networkAllowedCIDRs,
			}
			if localLLMOnly {
				networkPolicy.Enabled = true
				networkPolicy.OutboundLocalhostOnly = true
				if strings.TrimSpace(llmProvider) == "" {
					llmProvider = "openai_compat"
				}
			}

			plan, err := smolvm.PackRunAgentCommand(smolvm.PackRunAgentOptions{
				SmolVMBinary:       strings.TrimSpace(smolvmBinary),
				FoxctlBinary:       strings.TrimSpace(foxctlBinary),
				SidecarPath:        strings.TrimSpace(sidecarPath),
				RepoHostPath:       strings.TrimSpace(repoPath),
				RepoWritable:       repoWritable,
				OutHostPath:        strings.TrimSpace(outPath),
				StorageHostPath:    strings.TrimSpace(storagePath),
				FoxctlHomeHostPath: strings.TrimSpace(foxctlHomePath),
				GuestWorkingDir:    strings.TrimSpace(guestWorkingDir),
				Role:               strings.TrimSpace(role),
				Prompt:             strings.TrimSpace(prompt),
				SkillsAllow:        normalizedSkillsAllow,
				RunID:              strings.TrimSpace(runID),
				AgentIDs:           agentIDs,
				AgentDryRun:        agentDryRun,
				AgentSlug:          strings.TrimSpace(agentSlug),
				ExecMode:           strings.TrimSpace(execMode),
				MaxAutoTurns:       maxAutoTurns,
				MaxIterations:      maxIterations,
				Timeout:            strings.TrimSpace(agentTimeout),
				AskQuestion:        strings.TrimSpace(askQuestion),
				AskTimeout:         strings.TrimSpace(askTimeout),
				RepoIndexWorkspace: strings.TrimSpace(repoIndexWorkspace),
				Network:            networkPolicy,
				LLM: smolvm.LLMEnvPlan{
					Provider:          strings.TrimSpace(llmProvider),
					BaseURL:           strings.TrimSpace(llmBaseURL),
					Model:             strings.TrimSpace(llmModel),
					APIKey:            strings.TrimSpace(llmAPIKey),
					APIKeyPlaceholder: strings.TrimSpace(llmAPIKeyPlaceholder),
				},
			})
			if err != nil {
				return writeErrorEnvelope(cmd, "sandbox/smolvm/run-agent-plan", string(protocol.ErrorCodeEARG), err.Error())
			}

			if dryRun {
				return writeOK(cmd, "sandbox/smolvm/run-agent-plan", buildSmolVMPlanEnvelope(plan, true), "plan", profilesCoreAgent)
			}

			result, err := runner.Run(cmd.Context(), plan)
			data := buildSmolVMCommandExecutionEnvelope(plan, result)
			if err != nil {
				return writeSmolVMErrorEnvelope(cmd, "sandbox/smolvm/run-agent-plan", string(protocol.ErrorCodeERuntime), err.Error(), data)
			}
			if result.ExitCode != 0 {
				return writeSmolVMErrorEnvelope(
					cmd,
					"sandbox/smolvm/run-agent-plan",
					string(protocol.ErrorCodeERuntime),
					fmt.Sprintf("smolvm run-agent exited with code %d", result.ExitCode),
					data,
				)
			}
			return writeOK(cmd, "sandbox/smolvm/run-agent-plan", data, "execution", profilesCoreAgent)
		},
	}

	cmd.Flags().BoolVar(&dryRun, "dry-run", true, "Plan only unless set to false")
	cmd.Flags().StringVar(&smolvmBinary, "smolvm-binary", "", "Override the smolvm binary name/path")
	cmd.Flags().StringVar(&foxctlBinary, "foxctl-binary", "", "Override foxctl binary path used inside the guest command")
	cmd.Flags().StringVar(&sidecarPath, "sidecar", "", "Path to .smolmachine sidecar")
	cmd.Flags().StringVar(&repoPath, "repo", "", "Host repository path to mount")
	cmd.Flags().StringVar(&repoMode, "repo-mode", "readonly", "Repo mount mode: readonly or writable")
	cmd.Flags().StringVar(&outPath, "out", "", "Host output path to mount")
	cmd.Flags().StringVar(&storagePath, "storage", "", "Optional host foxctl storage root to mount at /mnt/.foxctl/storage")
	cmd.Flags().StringVar(&foxctlHomePath, "foxctl-home", "", "Optional host foxctl home to mount at /mnt/.foxctl (development only)")
	cmd.Flags().StringVar(&guestWorkingDir, "workdir", "", "Guest working directory (default: /mnt/repo)")
	cmd.Flags().StringVar(&role, "role", "", "Agent role for foxctl agent spawn")
	cmd.Flags().StringVar(&prompt, "prompt", "", "Agent prompt for foxctl agent spawn")
	cmd.Flags().StringVar(&skillsAllowRaw, "skills-allow", "", "Allowed skills (comma-separated or JSON array)")
	cmd.Flags().StringVar(&runID, "run-id", "", "Run id used for deterministic output layout planning")
	cmd.Flags().StringSliceVar(&agentIDs, "agent-id", nil, "Agent id for output layout planning (repeatable)")
	cmd.Flags().BoolVar(&agentDryRun, "agent-dry-run", false, "Pass --dry-run to guest foxctl agent spawn")
	cmd.Flags().StringVar(&execMode, "exec-mode", "", "Guest agent execution mode")
	cmd.Flags().IntVar(&maxAutoTurns, "max-auto-turns", 0, "Guest agent max autonomous turns")
	cmd.Flags().IntVar(&maxIterations, "max-iterations", 0, "Guest agent max tool iterations")
	cmd.Flags().StringVar(&agentTimeout, "agent-timeout", "", "Guest agent timeout, e.g. 2m")
	cmd.Flags().StringVar(&agentSlug, "agent-slug", "", "Guest agent slug")
	cmd.Flags().StringVar(&askQuestion, "ask-question", "", "After spawning, run the guest agent daemon and ask this question")
	cmd.Flags().StringVar(&askTimeout, "ask-timeout", "", "Guest agent ask --wait timeout, e.g. 90s")
	cmd.Flags().StringVar(&repoIndexWorkspace, "repo-index-workspace", "", "Workspace root to use for guest repo-index lookup keys")
	cmd.Flags().StringVar(&llmProvider, "llm-provider", "", "LLM provider override")
	cmd.Flags().StringVar(&llmBaseURL, "llm-base-url", "", "LLM base URL override")
	cmd.Flags().StringVar(&llmModel, "llm-model", "", "LLM model override")
	cmd.Flags().StringVar(&llmAPIKey, "llm-api-key", "", "LLM API key (redacted in plan output)")
	cmd.Flags().StringVar(&llmAPIKeyPlaceholder, "llm-api-key-placeholder", "", "Placeholder value to emit for FOXCTL_LLM_API_KEY in argv/env")
	cmd.Flags().BoolVar(&localLLMOnly, "local-llm-only", false, "Request localhost-only egress intent for local LLM traffic")
	cmd.Flags().BoolVar(&networkEnabled, "net", false, "Enable guest networking for planned pack run")
	cmd.Flags().BoolVar(&localhostOnly, "outbound-localhost-only", false, "Request localhost-only outbound policy")
	cmd.Flags().StringSliceVar(&networkAllowedHosts, "allow-host", nil, "Allow outbound host (repeatable)")
	cmd.Flags().StringSliceVar(&networkAllowedCIDRs, "allow-cidr", nil, "Allow outbound CIDR (repeatable)")
	_ = cmd.MarkFlagRequired("sidecar")
	_ = cmd.MarkFlagRequired("repo")
	_ = cmd.MarkFlagRequired("out")
	_ = cmd.MarkFlagRequired("role")
	_ = cmd.MarkFlagRequired("prompt")
	_ = cmd.MarkFlagRequired("run-id")
	return cmd
}

func newSandboxSmolVMFoxctlPackagePlanCommand() *cobra.Command {
	var flags sandboxSmolVMFoxctlPackageFlags

	cmd := &cobra.Command{
		Use:     "foxctl-package-plan",
		Aliases: []string{"package-foxctl-plan"},
		Short:   "Plan the staged smolvm sequence that builds and packages foxctl",
		RunE: func(cmd *cobra.Command, _ []string) error {
			plan, err := smolvm.FoxctlPackagePlan(flags.options())
			if err != nil {
				return writeErrorEnvelope(cmd, "sandbox/smolvm/foxctl-package-plan", string(protocol.ErrorCodeEARG), err.Error())
			}

			return writeOK(cmd, "sandbox/smolvm/foxctl-package-plan", buildSmolVMFoxctlPackagePlanEnvelope(plan), "plan", profilesCoreAgent)
		},
	}

	addFoxctlPackageFlags(cmd, &flags)
	return cmd
}

func newSandboxSmolVMFoxctlPackageCommand() *cobra.Command {
	return newSandboxSmolVMFoxctlPackageCommandWithRunner(smolvm.ExecCommandRunner{})
}

func newSandboxSmolVMFoxctlPackageCommandWithRunner(runner smolvm.CommandRunner) *cobra.Command {
	var flags sandboxSmolVMFoxctlPackageFlags
	dryRun := true

	cmd := &cobra.Command{
		Use:   "foxctl-package",
		Short: "Build, stage, and package foxctl into a smolvm artifact",
		RunE: func(cmd *cobra.Command, _ []string) error {
			plan, err := smolvm.FoxctlPackagePlan(flags.options())
			if err != nil {
				return writeErrorEnvelope(cmd, "sandbox/smolvm/foxctl-package", string(protocol.ErrorCodeEARG), err.Error())
			}
			if dryRun {
				return writeOK(cmd, "sandbox/smolvm/foxctl-package", buildSmolVMFoxctlPackagePlanEnvelope(plan), "plan", profilesCoreAgent)
			}

			result, err := smolvm.RunCommandSequence(cmd.Context(), runner, plan.Steps)
			data := buildSmolVMFoxctlPackageExecutionEnvelope(plan, result)
			if err != nil {
				return writeSmolVMErrorEnvelope(
					cmd,
					"sandbox/smolvm/foxctl-package",
					string(protocol.ErrorCodeERuntime),
					err.Error(),
					data,
				)
			}
			return writeOK(cmd, "sandbox/smolvm/foxctl-package", data, "execution", profilesCoreAgent)
		},
	}

	addFoxctlPackageFlags(cmd, &flags)
	cmd.Flags().BoolVar(&dryRun, "dry-run", true, "Plan only unless set to false")
	return cmd
}

type sandboxSmolVMFoxctlPackageFlags struct {
	smolvmBinary   string
	goBinary       string
	sourcePackage  string
	buildOutput    string
	goos           string
	goarch         string
	cgoEnabled     bool
	image          string
	machineName    string
	guestFoxctl    string
	outputPath     string
	noSign         bool
	platform       string
	cpus           int
	memoryMiB      int
	overlayGiB     int
	cleanupMachine bool
}

func addFoxctlPackageFlags(cmd *cobra.Command, flags *sandboxSmolVMFoxctlPackageFlags) {
	cmd.Flags().StringVar(&flags.smolvmBinary, "smolvm-binary", "", "Override the smolvm binary name/path")
	cmd.Flags().StringVar(&flags.goBinary, "go-binary", "", "Override the go binary name/path")
	cmd.Flags().StringVar(&flags.sourcePackage, "source-package", "", "Go package to build (default: ./cmd/foxctl)")
	cmd.Flags().StringVar(&flags.buildOutput, "build-output", "", "Host path for the Linux foxctl binary")
	cmd.Flags().StringVar(&flags.goos, "goos", "", "GOOS for host build (default: linux)")
	cmd.Flags().StringVar(&flags.goarch, "goarch", "", "GOARCH for host build (default: arm64)")
	cmd.Flags().BoolVar(&flags.cgoEnabled, "cgo-enabled", false, "Set CGO_ENABLED=1 for the host build")
	cmd.Flags().StringVar(&flags.image, "image", "", "Base image for machine create (default: alpine:3.20)")
	cmd.Flags().StringVar(&flags.machineName, "machine-name", "", "Staging VM name")
	cmd.Flags().StringVar(&flags.guestFoxctl, "guest-foxctl", "", "Guest path for the copied foxctl binary")
	cmd.Flags().StringVar(&flags.outputPath, "output", "", "Output packed binary path; sidecar is output + .smolmachine")
	cmd.Flags().BoolVar(&flags.noSign, "no-sign", false, "Skip macOS code signing")
	cmd.Flags().StringVar(&flags.platform, "platform", "", "OCI platform passed to final pack create as --oci-platform")
	cmd.Flags().IntVar(&flags.cpus, "cpus", 0, "vCPU count for staging and packed VM (default: 2)")
	cmd.Flags().IntVar(&flags.memoryMiB, "mem", 0, "Memory in MiB for staging and packed VM (default: 512)")
	cmd.Flags().IntVar(&flags.overlayGiB, "overlay", 0, "Overlay disk size in GiB for staging VM (default: 2)")
	cmd.Flags().BoolVar(&flags.cleanupMachine, "cleanup-machine", false, "Include an optional machine delete step")
	_ = cmd.MarkFlagRequired("output")
}

func (flags sandboxSmolVMFoxctlPackageFlags) options() smolvm.FoxctlPackageOptions {
	return smolvm.FoxctlPackageOptions{
		SmolVMBinary:   strings.TrimSpace(flags.smolvmBinary),
		GoBinary:       strings.TrimSpace(flags.goBinary),
		SourcePackage:  strings.TrimSpace(flags.sourcePackage),
		BuildOutput:    strings.TrimSpace(flags.buildOutput),
		GOOS:           strings.TrimSpace(flags.goos),
		GOARCH:         strings.TrimSpace(flags.goarch),
		CGOEnabled:     flags.cgoEnabled,
		Image:          strings.TrimSpace(flags.image),
		MachineName:    strings.TrimSpace(flags.machineName),
		GuestFoxctl:    strings.TrimSpace(flags.guestFoxctl),
		OutputPath:     strings.TrimSpace(flags.outputPath),
		NoSign:         flags.noSign,
		Platform:       strings.TrimSpace(flags.platform),
		CPU:            flags.cpus,
		MemoryMiB:      flags.memoryMiB,
		OverlayGiB:     flags.overlayGiB,
		CleanupMachine: flags.cleanupMachine,
	}
}

func parseRepoMode(mode string) (bool, error) {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case "", "readonly", "read-only", "ro":
		return false, nil
	case "writable", "readwrite", "read-write", "rw":
		return true, nil
	default:
		return false, fmt.Errorf("invalid --repo-mode %q (allowed: readonly, writable)", mode)
	}
}

func parseSkillsAllowList(raw string) ([]string, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return nil, nil
	}
	var skillsAllow []string
	if strings.HasPrefix(trimmed, "[") {
		if err := json.Unmarshal([]byte(trimmed), &skillsAllow); err != nil {
			return nil, err
		}
	} else {
		skillsAllow = parseCommaSeparated(trimmed)
	}
	if len(skillsAllow) == 0 {
		return nil, nil
	}
	return toolnames.NormalizeAllowlist(toolnames.ToolModeRuntime, skillsAllow), nil
}

func buildSmolVMPlanEnvelope(plan smolvm.CommandPlan, dryRun bool) map[string]any {
	limitations := append([]string{}, plan.Summary.Limitations...)
	if len(plan.Summary.Network.Limitations) > 0 {
		limitations = append(limitations, plan.Summary.Network.Limitations...)
	}
	return map[string]any{
		"dry_run":     dryRun,
		"argv":        plan.Argv,
		"env":         plan.Env,
		"summary":     plan.Summary,
		"limitations": dedupeStrings(limitations),
		"execution": map[string]any{
			"wired":   false,
			"message": "execution not wired yet",
		},
	}
}

func buildSmolVMCommandExecutionEnvelope(plan smolvm.CommandPlan, result smolvm.CommandResult) map[string]any {
	data := buildSmolVMPlanEnvelope(plan, false)
	data["execution"] = map[string]any{
		"wired":   true,
		"success": result.ExitCode == 0,
		"result":  result,
	}
	return data
}

func buildSmolVMFoxctlPackagePlanEnvelope(plan smolvm.FoxctlPackageSequencePlan) map[string]any {
	limitations := make([]string, 0)
	for _, step := range plan.Steps {
		limitations = append(limitations, step.Command.Summary.Limitations...)
		if len(step.Command.Summary.Network.Limitations) > 0 {
			limitations = append(limitations, step.Command.Summary.Network.Limitations...)
		}
	}
	return map[string]any{
		"dry_run":         true,
		"steps":           plan.Steps,
		"artifacts":       plan.Artifacts,
		"machine_name":    plan.MachineName,
		"build_output":    plan.BuildOutput,
		"guest_foxctl":    plan.GuestFoxctl,
		"packed_run_argv": plan.PackedRunArgv,
		"limitations":     dedupeStrings(limitations),
		"execution": map[string]any{
			"wired":   false,
			"message": "execution not wired yet",
		},
	}
}

func buildSmolVMFoxctlPackageExecutionEnvelope(plan smolvm.FoxctlPackageSequencePlan, result smolvm.CommandSequenceResult) map[string]any {
	data := buildSmolVMFoxctlPackagePlanEnvelope(plan)
	data["dry_run"] = false
	data["execution"] = map[string]any{
		"wired":   true,
		"success": result.Success,
		"steps":   result.Steps,
	}
	return data
}

func writeSmolVMErrorEnvelope(cmd *cobra.Command, command, code, message string, data any) error {
	env := domainEnvelope.Error(command, code, message, data, domainEnvelope.WithMetaMutator(func(m *domainEnvelope.Meta) {
		m.Source = "execution"
		m.Profiles = profilesCoreAgent
	}))
	if err := domainEnvelope.Write(cmd.OutOrStdout(), env); err != nil {
		return fmt.Errorf("write error envelope: %w", err)
	}
	return fmt.Errorf("%s", message)
}

func dedupeStrings(items []string) []string {
	if len(items) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(items))
	result := make([]string, 0, len(items))
	for _, item := range items {
		value := strings.TrimSpace(item)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	if len(result) == 0 {
		return nil
	}
	return result
}
