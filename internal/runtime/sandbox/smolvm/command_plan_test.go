package smolvm

import (
	"errors"
	"reflect"
	"strings"
	"testing"
)

func TestPackRunAgentCommandDeterministicPlan(t *testing.T) {
	t.Parallel()

	plan, err := PackRunAgentCommand(PackRunAgentOptions{
		SidecarPath:     "/tmp/foxctl-agent.smolmachine",
		RepoHostPath:    "/host/repo",
		OutHostPath:     "/host/out",
		StorageHostPath: "/host/storage",
		Role:            "researcher",
		Prompt:          "Investigate RLM runtime shape",
		SkillsAllow:     []string{"fs_read", "code_symbols"},
		RunID:           "Run 2026-04-21 Main",
		AgentIDs:        []string{"Researcher/Child#1"},
		AgentDryRun:     true,
		ExecMode:        "autonomous",
		MaxAutoTurns:    1,
		MaxIterations:   2,
		Timeout:         "2m",
		LLM: LLMEnvPlan{
			Provider:          "openai_compat",
			BaseURL:           "http://127.0.0.1:1234/v1",
			Model:             "liquid/lfm2.5-1.2b",
			APIKey:            "sk-real-secret",
			APIKeyPlaceholder: "${INJECT_RUNTIME_API_KEY}",
		},
	})
	if err != nil {
		t.Fatalf("PackRunAgentCommand() error = %v", err)
	}

	wantArgv := []string{
		"smolvm", "pack", "run",
		"--sidecar", "/tmp/foxctl-agent.smolmachine",
		"-v", "/host/repo:/mnt/repo:ro",
		"-v", "/host/out:/mnt/out",
		"-v", "/host/storage:/mnt/.foxctl/storage",
		"-w", "/mnt/repo",
		"-e", "FOXCTL_HOME=/mnt/.foxctl",
		"-e", "FOXCTL_STORAGE_ROOT=/mnt/.foxctl/storage",
		"-e", "FOXCTL_OBS_DIR=/mnt/out/runs/run-2026-04-21-main/observability",
		"-e", "FOXCTL_LLM_PROVIDER=openai_compat",
		"-e", "FOXCTL_LLM_BASE_URL=http://127.0.0.1:1234/v1",
		"-e", "FOXCTL_LLM_API_KEY=${INJECT_RUNTIME_API_KEY}",
		"-e", "FOXCTL_LLM_MODEL=liquid/lfm2.5-1.2b",
		"--",
		"foxctl", "agent", "spawn",
		"--role", "researcher",
		"--prompt", "Investigate RLM runtime shape",
		"--workspace", "/mnt/repo",
		"--slug", "researcher-child-1",
		"--dry-run",
		"--skills-allow", "fs_read,code_symbols",
		"--exec-mode", "autonomous",
		"--max-auto-turns", "1",
		"--max-iterations", "2",
		"--timeout", "2m",
		"--llm-provider", "openai_compat",
		"--llm-base-url", "http://127.0.0.1:1234/v1",
		"--llm-model", "liquid/lfm2.5-1.2b",
		"--llm-api-key", "${INJECT_RUNTIME_API_KEY}",
	}
	if !reflect.DeepEqual(plan.Argv, wantArgv) {
		t.Fatalf("argv=%v\nwant=%v", plan.Argv, wantArgv)
	}

	wantMounts := []VolumeMountPlan{
		{HostPath: "/host/repo", GuestPath: "/mnt/repo", ReadOnly: true},
		{HostPath: "/host/out", GuestPath: "/mnt/out"},
		{HostPath: "/host/storage", GuestPath: "/mnt/.foxctl/storage"},
	}
	if !reflect.DeepEqual(plan.Summary.Mounts, wantMounts) {
		t.Fatalf("mounts=%v want=%v", plan.Summary.Mounts, wantMounts)
	}

	if plan.Summary.OutputLayout == nil {
		t.Fatalf("expected output layout to be planned")
	}
	if plan.Summary.OutputLayout.Run.ID != "run-2026-04-21-main" {
		t.Fatalf("run id=%q", plan.Summary.OutputLayout.Run.ID)
	}
	if len(plan.Summary.OutputLayout.Run.Agents) != 1 {
		t.Fatalf("agent count=%d", len(plan.Summary.OutputLayout.Run.Agents))
	}
	if plan.Summary.OutputLayout.Run.Agents[0].ID != "researcher/child-1" {
		t.Fatalf("agent id=%q", plan.Summary.OutputLayout.Run.Agents[0].ID)
	}

	apiEnv, ok := lookupEnv(plan.Env, "FOXCTL_LLM_API_KEY")
	if !ok {
		t.Fatalf("expected FOXCTL_LLM_API_KEY env")
	}
	if !apiEnv.Sensitive {
		t.Fatalf("FOXCTL_LLM_API_KEY should be marked sensitive")
	}
	serialized := strings.Join(plan.Argv, "\n") + "\n" + serializeEnv(plan.Env)
	if strings.Contains(serialized, "sk-real-secret") {
		t.Fatalf("planned command leaked raw API key: %q", serialized)
	}
}

func TestPackRunAgentCommandPackRunNetworkDegradation(t *testing.T) {
	t.Parallel()

	plan, err := PackRunAgentCommand(PackRunAgentOptions{
		SidecarPath:  "/tmp/foxctl-agent.smolmachine",
		RepoHostPath: "/host/repo",
		OutHostPath:  "/host/out",
		Role:         "researcher",
		Prompt:       "Investigate",
		RunID:        "run-main",
		Network: NetworkPolicy{
			OutboundLocalhostOnly: true,
			AllowedHosts:          []string{"api.openai.com"},
			AllowedCIDRs:          []string{"10.0.0.0/8"},
		},
	})
	if err != nil {
		t.Fatalf("PackRunAgentCommand() error = %v", err)
	}

	if !containsToken(plan.Argv, "--net") {
		t.Fatalf("expected --net in argv when restricted egress requested")
	}
	for _, forbidden := range []string{"--outbound-localhost-only", "--allow-host", "--allow-cidr"} {
		if containsToken(plan.Argv, forbidden) {
			t.Fatalf("pack run argv should not include %s: %v", forbidden, plan.Argv)
		}
	}
	if len(plan.Summary.Network.Limitations) == 0 {
		t.Fatalf("expected network limitation warning")
	}
	if !containsSubstring(plan.Summary.Network.Limitations, "does not support --outbound-localhost-only") {
		t.Fatalf("network limitations=%v", plan.Summary.Network.Limitations)
	}
	if !plan.Summary.Network.Applied.Enabled {
		t.Fatalf("applied network should reflect --net usage")
	}
	if plan.Summary.Network.Applied.OutboundLocalhostOnly {
		t.Fatalf("applied network must not claim localhost-only for pack run")
	}
}

func TestPackRunAgentCommandRunAndAskScript(t *testing.T) {
	t.Parallel()

	plan, err := PackRunAgentCommand(PackRunAgentOptions{
		FoxctlBinary:       "/usr/local/bin/foxctl",
		SidecarPath:        "/tmp/foxctl-agent.smolmachine",
		RepoHostPath:       "/host/repo",
		OutHostPath:        "/host/out",
		Role:               "researcher",
		Prompt:             "Agent prompt",
		RunID:              "run-main",
		AgentSlug:          "smoke-live",
		AskQuestion:        "Answer with 'OK'",
		AskTimeout:         "30s",
		RepoIndexWorkspace: "/host/repo",
		LLM:                LLMEnvPlan{Provider: "lmstudio", BaseURL: "http://127.0.0.1:1234/v1", Model: "model-a"},
		MaxIterations:      1,
	})
	if err != nil {
		t.Fatalf("PackRunAgentCommand() error = %v", err)
	}
	if !containsToken(plan.Argv, "/bin/sh") || !containsToken(plan.Argv, "-lc") {
		t.Fatalf("expected shell script argv: %v", plan.Argv)
	}
	script := plan.Argv[len(plan.Argv)-1]
	for _, want := range []string{
		"/usr/local/bin/foxctl' 'agent' 'spawn'",
		"'--slug' 'smoke-live'",
		"/usr/local/bin/foxctl' 'agent' 'run' 'smoke-live' '--repo-index-workspace' '/host/repo'",
		"/usr/local/bin/foxctl' agent ask 'smoke-live'",
		"--question 'Answer with '\\''OK'\\'''",
		"--timeout '30s'",
	} {
		if !strings.Contains(script, want) {
			t.Fatalf("script missing %q:\n%s", want, script)
		}
	}
}

func TestProbeLMStudioCommandMachineNetworkFlags(t *testing.T) {
	t.Parallel()

	plan, err := ProbeLMStudioCommand(ProbeLMStudioOptions{
		Image:   "alpine:3.20",
		BaseURL: "http://127.0.0.1:1234/v1/",
		Network: NetworkPolicy{
			OutboundLocalhostOnly: true,
			AllowedHosts:          []string{"lmstudio-proxy.local"},
			AllowedCIDRs:          []string{"10.10.0.0/16"},
		},
	})
	if err != nil {
		t.Fatalf("ProbeLMStudioCommand() error = %v", err)
	}

	wantArgv := []string{
		"smolvm", "machine", "run",
		"--image", "alpine:3.20",
		"--allow-host", "lmstudio-proxy.local",
		"--allow-cidr", "127.0.0.0/8",
		"--allow-cidr", "10.10.0.0/16",
		"--",
		"wget", "-qO-", "http://127.0.0.1:1234/v1/models",
	}
	if !reflect.DeepEqual(plan.Argv, wantArgv) {
		t.Fatalf("argv=%v\nwant=%v", plan.Argv, wantArgv)
	}
	if plan.Summary.Network.Applied.OutboundLocalhostOnly {
		t.Fatalf("expected localhost-only policy to be translated to CIDR workaround")
	}
	if !reflect.DeepEqual(plan.Summary.Network.Applied.AllowedCIDRs, []string{"127.0.0.0/8", "10.10.0.0/16"}) {
		t.Fatalf("applied cidrs=%v", plan.Summary.Network.Applied.AllowedCIDRs)
	}
	if !reflect.DeepEqual(plan.Summary.Network.Applied.AllowedHosts, []string{"lmstudio-proxy.local"}) {
		t.Fatalf("applied hosts=%v", plan.Summary.Network.Applied.AllowedHosts)
	}
	if !containsSubstring(plan.Summary.Network.Limitations, "--outbound-localhost-only failed locally") {
		t.Fatalf("network limitations=%v", plan.Summary.Network.Limitations)
	}
}

func TestProbeLMStudioCommandBroadNetUsesCIDRWorkaround(t *testing.T) {
	t.Parallel()

	plan, err := ProbeLMStudioCommand(ProbeLMStudioOptions{
		Image:   "alpine:3.20",
		BaseURL: "http://127.0.0.1:1234/v1/",
		Network: NetworkPolicy{
			Enabled: true,
		},
	})
	if err != nil {
		t.Fatalf("ProbeLMStudioCommand() error = %v", err)
	}

	if containsToken(plan.Argv, "--net") {
		t.Fatalf("expected broad network to use CIDR workaround instead of --net: %v", plan.Argv)
	}
	if !containsToken(plan.Argv, "--allow-cidr") || !containsToken(plan.Argv, "0.0.0.0/0") {
		t.Fatalf("expected --allow-cidr 0.0.0.0/0 in argv: %v", plan.Argv)
	}
	if !containsSubstring(plan.Summary.Network.Limitations, "--net wrote an unusable localhost resolver") {
		t.Fatalf("network limitations=%v", plan.Summary.Network.Limitations)
	}
}

func TestPackRunAgentCommandInvalidOutputLayout(t *testing.T) {
	t.Parallel()

	_, err := PackRunAgentCommand(PackRunAgentOptions{
		SidecarPath:  "/tmp/foxctl-agent.smolmachine",
		RepoHostPath: "/host/repo",
		OutHostPath:  "/host/out",
		Role:         "researcher",
		Prompt:       "Investigate",
		RunID:        "!!!",
	})
	if !errors.Is(err, ErrInvalidRunID) {
		t.Fatalf("expected ErrInvalidRunID, got %v", err)
	}
}

func TestPackRunAgentCommandRequiresRunID(t *testing.T) {
	t.Parallel()

	_, err := PackRunAgentCommand(PackRunAgentOptions{
		SidecarPath:  "/tmp/foxctl-agent.smolmachine",
		RepoHostPath: "/host/repo",
		OutHostPath:  "/host/out",
		Role:         "researcher",
		Prompt:       "Investigate",
	})
	if !errors.Is(err, ErrInvalidRunID) {
		t.Fatalf("expected ErrInvalidRunID, got %v", err)
	}
}

func lookupEnv(env []EnvVarPlan, key string) (EnvVarPlan, bool) {
	for _, item := range env {
		if item.Name == key {
			return item, true
		}
	}
	return EnvVarPlan{}, false
}

func serializeEnv(env []EnvVarPlan) string {
	lines := make([]string, 0, len(env))
	for _, item := range env {
		lines = append(lines, item.Name+"="+item.Value)
	}
	return strings.Join(lines, "\n")
}

func containsToken(tokens []string, token string) bool {
	for _, item := range tokens {
		if item == token {
			return true
		}
	}
	return false
}

func containsSubstring(items []string, needle string) bool {
	for _, item := range items {
		if strings.Contains(item, needle) {
			return true
		}
	}
	return false
}
