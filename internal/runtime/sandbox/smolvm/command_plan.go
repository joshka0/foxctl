package smolvm

import (
	"errors"
	"fmt"
	"path"
	"path/filepath"
	"strconv"
	"strings"
)

const (
	defaultSmolVMBinary       = "smolvm"
	defaultFoxctlBinary       = "foxctl"
	defaultGuestRepoDir       = "/mnt/repo"
	defaultGuestOutDir        = "/mnt/out"
	defaultGuestWorkDir       = "/mnt/repo"
	defaultGuestFoxctlHome    = "/mnt/.foxctl"
	defaultGuestObservability = "/mnt/out/observability"
	defaultAPIKeyPlaceholder  = "${FOXCTL_LLM_API_KEY}"
)

var (
	ErrInvalidSidecarPath = errors.New("smolvm: sidecar path is required")
	ErrInvalidRepoPath    = errors.New("smolvm: repo host path is required")
	ErrInvalidOutPath     = errors.New("smolvm: out host path is required")
	ErrInvalidRole        = errors.New("smolvm: role is required")
	ErrInvalidPrompt      = errors.New("smolvm: prompt is required")
	ErrInvalidProbeImage  = errors.New("smolvm: probe image is required")
	ErrInvalidProbeURL    = errors.New("smolvm: probe base url is required")
	ErrInvalidPackSource  = errors.New("smolvm: exactly one pack source is required")
	ErrInvalidPackOutput  = errors.New("smolvm: pack output path is required")
	ErrInvalidPackCPU     = errors.New("smolvm: pack cpu must be >= 0")
	ErrInvalidPackMemory  = errors.New("smolvm: pack memory_mib must be >= 0")
)

// NetworkPolicy captures requested network behavior for smolvm runs.
type NetworkPolicy struct {
	Enabled               bool     `json:"enabled"`
	OutboundLocalhostOnly bool     `json:"outbound_localhost_only,omitempty"`
	AllowedHosts          []string `json:"allowed_hosts,omitempty"`
	AllowedCIDRs          []string `json:"allowed_cidrs,omitempty"`
}

// EnvVarPlan captures one deterministic environment assignment.
type EnvVarPlan struct {
	Name      string `json:"name"`
	Value     string `json:"value"`
	Sensitive bool   `json:"sensitive,omitempty"`
}

// VolumeMountPlan captures one deterministic mount planning decision.
type VolumeMountPlan struct {
	HostPath  string `json:"host_path"`
	GuestPath string `json:"guest_path"`
	ReadOnly  bool   `json:"read_only,omitempty"`
}

// NetworkPlan captures requested and applied network flags for one command.
type NetworkPlan struct {
	Requested   NetworkPolicy `json:"requested"`
	Applied     NetworkPolicy `json:"applied"`
	Limitations []string      `json:"limitations,omitempty"`
}

// CommandPlanSummary captures structured command planning metadata.
type CommandPlanSummary struct {
	Mode          string            `json:"mode"`
	Mounts        []VolumeMountPlan `json:"mounts,omitempty"`
	Network       NetworkPlan       `json:"network"`
	OutputLayout  *OutputLayoutPlan `json:"output_layout,omitempty"`
	PackArtifacts *PackArtifacts    `json:"pack_artifacts,omitempty"`
	Limitations   []string          `json:"limitations,omitempty"`
}

// CommandPlan captures a pure deterministic command plan.
type CommandPlan struct {
	Argv    []string           `json:"argv"`
	Env     []EnvVarPlan       `json:"env,omitempty"`
	Summary CommandPlanSummary `json:"summary"`
}

// LLMEnvPlan captures deterministic LLM env planning decisions.
type LLMEnvPlan struct {
	Provider          string `json:"provider,omitempty"`
	BaseURL           string `json:"base_url,omitempty"`
	Model             string `json:"model,omitempty"`
	APIKey            string `json:"api_key,omitempty"`
	APIKeyPlaceholder string `json:"api_key_placeholder,omitempty"`
}

// PackCreateOptions configures PackCreateCommand.
type PackCreateOptions struct {
	SmolVMBinary string `json:"smolvm_binary,omitempty"`
	Image        string `json:"image,omitempty"`
	FromVM       string `json:"from_vm,omitempty"`
	OutputPath   string `json:"output_path"`
	NoSign       bool   `json:"no_sign,omitempty"`
	SingleFile   bool   `json:"single_file,omitempty"`

	Platform   string `json:"platform,omitempty"`
	CPU        int    `json:"cpu,omitempty"`
	MemoryMiB  int    `json:"memory_mib,omitempty"`
	Smolfile   string `json:"smolfile,omitempty"`
	Entrypoint string `json:"entrypoint,omitempty"`
}

// PackRunAgentOptions configures PackRunAgentCommand.
type PackRunAgentOptions struct {
	SmolVMBinary       string `json:"smolvm_binary,omitempty"`
	FoxctlBinary       string `json:"foxctl_binary,omitempty"`
	SidecarPath        string `json:"sidecar_path"`
	RepoHostPath       string `json:"repo_host_path"`
	RepoWritable       bool   `json:"repo_writable,omitempty"`
	OutHostPath        string `json:"out_host_path"`
	StorageHostPath    string `json:"storage_host_path,omitempty"`
	FoxctlHomeHostPath string `json:"foxctl_home_host_path,omitempty"`
	GuestWorkingDir    string `json:"guest_working_dir,omitempty"`

	Role        string   `json:"role"`
	Prompt      string   `json:"prompt"`
	SkillsAllow []string `json:"skills_allow,omitempty"`
	RunID       string   `json:"run_id,omitempty"`
	AgentIDs    []string `json:"agent_ids,omitempty"`
	AgentSlug   string   `json:"agent_slug,omitempty"`
	AgentDryRun bool     `json:"agent_dry_run,omitempty"`
	ExecMode    string   `json:"exec_mode,omitempty"`

	MaxAutoTurns       int    `json:"max_auto_turns,omitempty"`
	MaxIterations      int    `json:"max_iterations,omitempty"`
	Timeout            string `json:"timeout,omitempty"`
	AskQuestion        string `json:"ask_question,omitempty"`
	AskTimeout         string `json:"ask_timeout,omitempty"`
	RepoIndexWorkspace string `json:"repo_index_workspace,omitempty"`

	Network NetworkPolicy `json:"network,omitempty"`
	LLM     LLMEnvPlan    `json:"llm,omitempty"`
}

// PackCreateCommand plans a deterministic smolvm pack create command.
func PackCreateCommand(opts PackCreateOptions) (CommandPlan, error) {
	image := strings.TrimSpace(opts.Image)
	fromVM := strings.TrimSpace(opts.FromVM)
	if (image == "") == (fromVM == "") {
		return CommandPlan{}, ErrInvalidPackSource
	}
	if opts.CPU < 0 {
		return CommandPlan{}, ErrInvalidPackCPU
	}
	if opts.MemoryMiB < 0 {
		return CommandPlan{}, ErrInvalidPackMemory
	}

	artifacts, err := ExpectedPackArtifactsForMode(opts.OutputPath, opts.SingleFile)
	if err != nil {
		return CommandPlan{}, err
	}

	binary := strings.TrimSpace(opts.SmolVMBinary)
	if binary == "" {
		binary = defaultSmolVMBinary
	}

	argv := []string{
		binary,
		"pack",
		"create",
		"--output", artifacts.OutputPath,
	}
	if image != "" {
		argv = append(argv, "--image", image)
	} else {
		argv = append(argv, "--from-vm", fromVM)
	}
	if platform := strings.TrimSpace(opts.Platform); platform != "" {
		argv = append(argv, "--oci-platform", platform)
	}
	if opts.CPU > 0 {
		argv = append(argv, "--cpus", strconv.Itoa(opts.CPU))
	}
	if opts.MemoryMiB > 0 {
		argv = append(argv, "--mem", strconv.Itoa(opts.MemoryMiB))
	}
	if smolfile := strings.TrimSpace(opts.Smolfile); smolfile != "" {
		argv = append(argv, "--smolfile", smolfile)
	}
	if entrypoint := strings.TrimSpace(opts.Entrypoint); entrypoint != "" {
		argv = append(argv, "--entrypoint", entrypoint)
	}
	if opts.NoSign {
		argv = append(argv, "--no-sign")
	}
	if opts.SingleFile {
		argv = append(argv, "--single-file")
	}

	return CommandPlan{
		Argv: argv,
		Summary: CommandPlanSummary{
			Mode:          "pack_create",
			PackArtifacts: &artifacts,
		},
	}, nil
}

// ProbeLMStudioOptions configures ProbeLMStudioCommand.
type ProbeLMStudioOptions struct {
	SmolVMBinary string        `json:"smolvm_binary,omitempty"`
	Image        string        `json:"image"`
	BaseURL      string        `json:"base_url"`
	Network      NetworkPolicy `json:"network,omitempty"`
}

// ProbeLMStudioCommand plans a deterministic machine-level probe for /models.
func ProbeLMStudioCommand(opts ProbeLMStudioOptions) (CommandPlan, error) {
	image := strings.TrimSpace(opts.Image)
	if image == "" {
		return CommandPlan{}, ErrInvalidProbeImage
	}
	baseURL := strings.TrimSpace(opts.BaseURL)
	if baseURL == "" {
		return CommandPlan{}, ErrInvalidProbeURL
	}

	binary := strings.TrimSpace(opts.SmolVMBinary)
	if binary == "" {
		binary = defaultSmolVMBinary
	}

	networkRequested := normalizeNetworkPolicy(opts.Network)
	networkFlags, appliedPolicy, networkLimitations := buildMachineNetworkFlags(networkRequested)

	argv := []string{binary, "machine", "run", "--image", image}
	argv = append(argv, networkFlags...)
	argv = append(argv, "--", "wget", "-qO-", joinURLPath(baseURL, "models"))

	return CommandPlan{
		Argv: argv,
		Summary: CommandPlanSummary{
			Mode: "probe_lmstudio",
			Network: NetworkPlan{
				Requested:   networkRequested,
				Applied:     appliedPolicy,
				Limitations: append([]string{}, networkLimitations...),
			},
			Limitations: append([]string{}, networkLimitations...),
		},
	}, nil
}

// PackRunAgentCommand plans a deterministic smolvm pack run command for a
// sandboxed foxctl agent spawn.
func PackRunAgentCommand(opts PackRunAgentOptions) (CommandPlan, error) {
	sidecar := strings.TrimSpace(opts.SidecarPath)
	if sidecar == "" {
		return CommandPlan{}, ErrInvalidSidecarPath
	}
	repoHost := strings.TrimSpace(opts.RepoHostPath)
	if repoHost == "" {
		return CommandPlan{}, ErrInvalidRepoPath
	}
	outHost := strings.TrimSpace(opts.OutHostPath)
	if outHost == "" {
		return CommandPlan{}, ErrInvalidOutPath
	}
	role := strings.TrimSpace(opts.Role)
	if role == "" {
		return CommandPlan{}, ErrInvalidRole
	}
	prompt := strings.TrimSpace(opts.Prompt)
	if prompt == "" {
		return CommandPlan{}, ErrInvalidPrompt
	}
	runID := strings.TrimSpace(opts.RunID)
	if NormalizeReadableID(runID) == "" {
		return CommandPlan{}, ErrInvalidRunID
	}

	binary := strings.TrimSpace(opts.SmolVMBinary)
	if binary == "" {
		binary = defaultSmolVMBinary
	}
	foxctlBinary := strings.TrimSpace(opts.FoxctlBinary)
	if foxctlBinary == "" {
		foxctlBinary = defaultFoxctlBinary
	}
	guestWorkDir := strings.TrimSpace(opts.GuestWorkingDir)
	if guestWorkDir == "" {
		guestWorkDir = defaultGuestWorkDir
	}
	repoReadOnly := !opts.RepoWritable

	var (
		outputLayout *OutputLayoutPlan
		obsDir       = defaultGuestObservability
	)
	agentIDs := normalizeAgentIDs(opts.AgentIDs, role)
	layout, err := PlanOutputLayout(defaultGuestOutDir, runID, agentIDs)
	if err != nil {
		return CommandPlan{}, fmt.Errorf("smolvm: plan output layout: %w", err)
	}
	outputLayout = &layout
	obsDir = path.Join(layout.Run.Dir, "observability")

	mounts := []VolumeMountPlan{
		{
			HostPath:  filepath.Clean(repoHost),
			GuestPath: defaultGuestRepoDir,
			ReadOnly:  repoReadOnly,
		},
		{
			HostPath:  filepath.Clean(outHost),
			GuestPath: defaultGuestOutDir,
		},
	}
	if foxctlHomeHost := strings.TrimSpace(opts.FoxctlHomeHostPath); foxctlHomeHost != "" {
		mounts = append(mounts, VolumeMountPlan{
			HostPath:  filepath.Clean(foxctlHomeHost),
			GuestPath: defaultGuestFoxctlHome,
		})
	} else if storageHost := strings.TrimSpace(opts.StorageHostPath); storageHost != "" {
		mounts = append(mounts, VolumeMountPlan{
			HostPath:  filepath.Clean(storageHost),
			GuestPath: path.Join(defaultGuestFoxctlHome, "storage"),
		})
	}

	networkRequested := normalizeNetworkPolicy(opts.Network)
	networkFlags, appliedPolicy, networkLimitations := buildPackRunNetworkFlags(networkRequested)

	env, envLimitations := buildPackRunEnv(opts.LLM, obsDir)
	limitations := append([]string{}, networkLimitations...)
	limitations = append(limitations, envLimitations...)

	argv := []string{binary, "pack", "run", "--sidecar", sidecar}
	argv = append(argv, networkFlags...)
	for _, mount := range mounts {
		argv = append(argv, "-v", renderVolumeMount(mount))
	}
	argv = append(argv, "-w", guestWorkDir)
	for _, item := range env {
		argv = append(argv, "-e", item.Name+"="+item.Value)
	}
	spawnArgv := buildGuestAgentSpawnArgv(foxctlBinary, role, prompt, guestWorkDir, opts)
	askQuestion := strings.TrimSpace(opts.AskQuestion)
	if askQuestion == "" {
		argv = append(argv, "--")
		argv = append(argv, spawnArgv...)
	} else {
		slug := plannedAgentSlug(opts, runID, role)
		script := buildGuestRunAndAskScript(foxctlBinary, spawnArgv, layout.Run.Dir, slug, askQuestion, opts.AskTimeout, opts.RepoIndexWorkspace)
		argv = append(argv, "--", "/bin/sh", "-lc", script)
	}

	return CommandPlan{
		Argv: argv,
		Env:  env,
		Summary: CommandPlanSummary{
			Mode:         "pack_run_agent",
			Mounts:       mounts,
			OutputLayout: outputLayout,
			Network: NetworkPlan{
				Requested:   networkRequested,
				Applied:     appliedPolicy,
				Limitations: append([]string{}, networkLimitations...),
			},
			Limitations: limitations,
		},
	}, nil
}

func buildGuestAgentSpawnArgv(foxctlBinary, role, prompt, guestWorkDir string, opts PackRunAgentOptions) []string {
	runID := strings.TrimSpace(opts.RunID)
	argv := []string{
		foxctlBinary,
		"agent",
		"spawn",
		"--role",
		role,
		"--prompt",
		prompt,
		"--workspace",
		guestWorkDir,
	}
	if slug := plannedAgentSlug(opts, runID, role); slug != "" {
		argv = append(argv, "--slug", slug)
	}
	if opts.AgentDryRun {
		argv = append(argv, "--dry-run")
	}
	skillsAllow := normalizeStringSlice(opts.SkillsAllow)
	if len(skillsAllow) > 0 {
		argv = append(argv, "--skills-allow", strings.Join(skillsAllow, ","))
	}
	if execMode := strings.TrimSpace(opts.ExecMode); execMode != "" {
		argv = append(argv, "--exec-mode", execMode)
	}
	if opts.MaxAutoTurns > 0 {
		argv = append(argv, "--max-auto-turns", strconv.Itoa(opts.MaxAutoTurns))
	}
	if opts.MaxIterations > 0 {
		argv = append(argv, "--max-iterations", strconv.Itoa(opts.MaxIterations))
	}
	if timeout := strings.TrimSpace(opts.Timeout); timeout != "" {
		argv = append(argv, "--timeout", timeout)
	}
	if provider := strings.TrimSpace(opts.LLM.Provider); provider != "" {
		argv = append(argv, "--llm-provider", provider)
	}
	if baseURL := strings.TrimSpace(opts.LLM.BaseURL); baseURL != "" {
		argv = append(argv, "--llm-base-url", baseURL)
	}
	if model := strings.TrimSpace(opts.LLM.Model); model != "" {
		argv = append(argv, "--llm-model", model)
	}
	if apiKeyPlaceholder := plannedAPIKeyPlaceholder(opts.LLM); apiKeyPlaceholder != "" {
		argv = append(argv, "--llm-api-key", apiKeyPlaceholder)
	}
	return argv
}

func plannedAgentSlug(opts PackRunAgentOptions, runID, role string) string {
	if slug := NormalizeReadableID(opts.AgentSlug); slug != "" {
		return slug
	}
	if len(opts.AgentIDs) > 0 {
		if slug := NormalizeReadableID(strings.ReplaceAll(opts.AgentIDs[0], "/", "-")); slug != "" {
			return slug
		}
	}
	return NormalizeReadableID(runID + "-" + role)
}

func buildGuestRunAndAskScript(foxctlBinary string, spawnArgv []string, runDir, slug, question, timeout, repoIndexWorkspace string) string {
	askTimeout := strings.TrimSpace(timeout)
	if askTimeout == "" {
		askTimeout = "90s"
	}
	runLog := path.Join(runDir, "agent-run.log")
	spawnJSON := path.Join(runDir, "spawn.json")
	parts := []string{
		"set -eu",
		"mkdir -p " + shellQuote(runDir),
		shellJoin(spawnArgv) + " > " + shellQuote(spawnJSON),
		shellJoin(buildGuestAgentRunArgv(foxctlBinary, slug, repoIndexWorkspace)) + " > " + shellQuote(runLog) + " 2>&1 &",
		"agent_pid=$!",
		"trap 'kill \"$agent_pid\" 2>/dev/null || true' EXIT",
		"sleep 1",
		shellQuote(foxctlBinary) + " agent ask " + shellQuote(slug) + " --question " + shellQuote(question) + " --wait --timeout " + shellQuote(askTimeout),
	}
	return strings.Join(parts, "\n")
}

func buildGuestAgentRunArgv(foxctlBinary, slug, repoIndexWorkspace string) []string {
	argv := []string{foxctlBinary, "agent", "run", slug}
	if workspace := strings.TrimSpace(repoIndexWorkspace); workspace != "" {
		argv = append(argv, "--repo-index-workspace", workspace)
	}
	return argv
}

func plannedAPIKeyPlaceholder(llm LLMEnvPlan) string {
	apiKey := strings.TrimSpace(llm.APIKey)
	apiKeyPlaceholder := strings.TrimSpace(llm.APIKeyPlaceholder)
	if apiKey == "" && apiKeyPlaceholder == "" {
		return ""
	}
	if apiKeyPlaceholder == "" {
		return defaultAPIKeyPlaceholder
	}
	return apiKeyPlaceholder
}

func shellJoin(argv []string) string {
	quoted := make([]string, 0, len(argv))
	for _, arg := range argv {
		quoted = append(quoted, shellQuote(arg))
	}
	return strings.Join(quoted, " ")
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
}

func buildPackRunEnv(llm LLMEnvPlan, obsDir string) ([]EnvVarPlan, []string) {
	env := []EnvVarPlan{
		{Name: "FOXCTL_HOME", Value: defaultGuestFoxctlHome},
		{Name: "FOXCTL_STORAGE_ROOT", Value: path.Join(defaultGuestFoxctlHome, "storage")},
		{Name: "FOXCTL_OBS_DIR", Value: obsDir},
	}
	limitations := make([]string, 0, 1)

	if provider := strings.TrimSpace(llm.Provider); provider != "" {
		env = append(env, EnvVarPlan{Name: "FOXCTL_LLM_PROVIDER", Value: provider})
	}
	if baseURL := strings.TrimSpace(llm.BaseURL); baseURL != "" {
		env = append(env, EnvVarPlan{Name: "FOXCTL_LLM_BASE_URL", Value: baseURL})
	}

	apiKey := strings.TrimSpace(llm.APIKey)
	apiKeyPlaceholder := strings.TrimSpace(llm.APIKeyPlaceholder)
	if apiKey != "" || apiKeyPlaceholder != "" {
		if apiKeyPlaceholder == "" {
			apiKeyPlaceholder = defaultAPIKeyPlaceholder
		}
		env = append(env, EnvVarPlan{
			Name:      "FOXCTL_LLM_API_KEY",
			Value:     apiKeyPlaceholder,
			Sensitive: true,
		})
		if apiKey != "" {
			limitations = append(
				limitations,
				"FOXCTL_LLM_API_KEY is redacted in plan output; replace placeholder before execution.",
			)
		}
	}
	if model := strings.TrimSpace(llm.Model); model != "" {
		env = append(env, EnvVarPlan{Name: "FOXCTL_LLM_MODEL", Value: model})
	}
	return env, limitations
}

func buildMachineNetworkFlags(policy NetworkPolicy) ([]string, NetworkPolicy, []string) {
	applied := normalizeNetworkPolicy(policy)
	flags := make([]string, 0, 1+len(applied.AllowedHosts)*2+len(applied.AllowedCIDRs)*2)
	limitations := make([]string, 0, 2)

	if applied.OutboundLocalhostOnly {
		applied.Enabled = true
		applied.AllowedCIDRs = prependIfMissing(applied.AllowedCIDRs, "127.0.0.0/8")
		applied.OutboundLocalhostOnly = false
		limitations = append(limitations, "smolvm machine run --outbound-localhost-only failed locally in v0.5.19; using --allow-cidr 127.0.0.0/8 for IPv4 localhost egress.")
	}
	if applied.Enabled && len(applied.AllowedHosts) == 0 && len(applied.AllowedCIDRs) == 0 {
		applied.AllowedCIDRs = []string{"0.0.0.0/0"}
		limitations = append(limitations, "smolvm machine run --net wrote an unusable localhost resolver locally; using --allow-cidr 0.0.0.0/0 as equivalent broad IPv4 egress.")
	}
	if requiresNetwork(applied) {
		applied.Enabled = true
	}
	if applied.Enabled && len(applied.AllowedHosts) == 0 && len(applied.AllowedCIDRs) == 0 {
		flags = append(flags, "--net")
	}
	for _, host := range applied.AllowedHosts {
		flags = append(flags, "--allow-host", host)
	}
	for _, cidr := range applied.AllowedCIDRs {
		flags = append(flags, "--allow-cidr", cidr)
	}
	return flags, applied, limitations
}

func buildPackRunNetworkFlags(policy NetworkPolicy) ([]string, NetworkPolicy, []string) {
	requested := normalizeNetworkPolicy(policy)
	applied := NetworkPolicy{}
	limitations := make([]string, 0, 1)
	flags := make([]string, 0, 1)

	if requiresNetwork(requested) {
		applied.Enabled = true
		flags = append(flags, "--net")
	}
	if requested.OutboundLocalhostOnly || len(requested.AllowedHosts) > 0 || len(requested.AllowedCIDRs) > 0 {
		limitations = append(
			limitations,
			"smolvm pack run does not support --outbound-localhost-only/--allow-host/--allow-cidr in v0.5.19; use machine run or a constrained proxy path.",
		)
	}
	return flags, applied, limitations
}

func normalizeNetworkPolicy(in NetworkPolicy) NetworkPolicy {
	out := NetworkPolicy{
		Enabled:               in.Enabled,
		OutboundLocalhostOnly: in.OutboundLocalhostOnly,
		AllowedHosts:          normalizeStringSlice(in.AllowedHosts),
		AllowedCIDRs:          normalizeStringSlice(in.AllowedCIDRs),
	}
	return out
}

func normalizeAgentIDs(agentIDs []string, fallback string) []string {
	out := normalizeStringSlice(agentIDs)
	if len(out) == 0 && strings.TrimSpace(fallback) != "" {
		out = append(out, fallback)
	}
	return out
}

func normalizeStringSlice(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	out := make([]string, 0, len(in))
	seen := make(map[string]struct{}, len(in))
	for _, raw := range in {
		item := strings.TrimSpace(raw)
		if item == "" {
			continue
		}
		if _, exists := seen[item]; exists {
			continue
		}
		seen[item] = struct{}{}
		out = append(out, item)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func prependIfMissing(items []string, value string) []string {
	for _, item := range items {
		if item == value {
			return items
		}
	}
	return append([]string{value}, items...)
}

func requiresNetwork(policy NetworkPolicy) bool {
	return policy.Enabled ||
		policy.OutboundLocalhostOnly ||
		len(policy.AllowedHosts) > 0 ||
		len(policy.AllowedCIDRs) > 0
}

func renderVolumeMount(mount VolumeMountPlan) string {
	spec := mount.HostPath + ":" + mount.GuestPath
	if mount.ReadOnly {
		spec += ":ro"
	}
	return spec
}

func joinURLPath(baseURL, suffix string) string {
	trimmedBase := strings.TrimRight(strings.TrimSpace(baseURL), "/")
	trimmedSuffix := strings.Trim(strings.TrimSpace(suffix), "/")
	if trimmedSuffix == "" {
		return trimmedBase
	}
	return trimmedBase + "/" + trimmedSuffix
}
