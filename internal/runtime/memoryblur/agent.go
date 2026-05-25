package memoryblur

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/joshka0/foxctl/internal/context/contextplane"
)

const (
	BackendCommand = "command"
	BackendPi      = "pi"
	BackendClaude  = "claude"
	BackendHermes  = "hermes"
	BackendFoxctl  = "foxctl"
)

const (
	PromptModeStdin = "stdin"
	PromptModeArg   = "arg"
)

const (
	PiModeSDK = "sdk"
	PiModeCLI = "cli"
)

type AgentOptions struct {
	Backend string
	Timeout time.Duration

	Command CommandAgentOptions
	Pi      PiAgentOptions
	Claude  ClaudeAgentOptions
	Hermes  HermesAgentOptions
	Foxctl  FoxctlAgentOptions
}

type CommandAgentOptions struct {
	Name       string
	Bin        string
	Args       []string
	PromptMode string
	Timeout    time.Duration
}

type PiAgentOptions struct {
	PiBin        string
	Mode         string
	SDKBin       string
	SDKScript    string
	SDKCWD       string
	AgentDir     string
	Thinking     string
	Provider     string
	Model        string
	NoExtensions bool
	Timeout      time.Duration
}

type ClaudeAgentOptions struct {
	ClaudeBin string
	Model     string
	Timeout   time.Duration
}

type HermesAgentOptions struct {
	HermesBin        string
	Provider         string
	Model            string
	IgnoreRules      bool
	IgnoreUserConfig bool
	Timeout          time.Duration
}

type FoxctlAgentOptions struct {
	FoxctlBin      string
	AgentID        string
	Dispatcher     string
	ConversationID string
	Timeout        time.Duration
}

type CommandAgent struct {
	opts CommandAgentOptions
}

func NewAgent(opts AgentOptions) (contextplane.MemoryBlurAgent, error) {
	backend := strings.ToLower(strings.TrimSpace(opts.Backend))
	if backend == "" {
		backend = BackendPi
	}
	switch backend {
	case BackendCommand:
		if opts.Command.Timeout <= 0 {
			opts.Command.Timeout = opts.Timeout
		}
		return NewCommandAgent(opts.Command)
	case BackendPi:
		if opts.Pi.Timeout <= 0 {
			opts.Pi.Timeout = opts.Timeout
		}
		return NewPiAgent(opts.Pi), nil
	case BackendClaude:
		if opts.Claude.Timeout <= 0 {
			opts.Claude.Timeout = opts.Timeout
		}
		return NewClaudeAgent(opts.Claude), nil
	case BackendHermes:
		if opts.Hermes.Timeout <= 0 {
			opts.Hermes.Timeout = opts.Timeout
		}
		return NewHermesAgent(opts.Hermes), nil
	case BackendFoxctl:
		if opts.Foxctl.Timeout <= 0 {
			opts.Foxctl.Timeout = opts.Timeout
		}
		return NewFoxctlAgent(opts.Foxctl)
	default:
		return nil, fmt.Errorf("unsupported memory blur agent backend %q", opts.Backend)
	}
}

func NewCommandAgent(opts CommandAgentOptions) (CommandAgent, error) {
	opts.Name = strings.TrimSpace(opts.Name)
	opts.Bin = strings.TrimSpace(opts.Bin)
	opts.PromptMode = strings.ToLower(strings.TrimSpace(opts.PromptMode))
	if opts.PromptMode == "" {
		opts.PromptMode = PromptModeStdin
	}
	if opts.Timeout <= 0 {
		opts.Timeout = 2 * time.Minute
	}
	if opts.Name == "" {
		opts.Name = opts.Bin
	}
	if opts.Bin == "" {
		return CommandAgent{}, fmt.Errorf("command agent bin is required")
	}
	switch opts.PromptMode {
	case PromptModeStdin, PromptModeArg:
	default:
		return CommandAgent{}, fmt.Errorf("unsupported command prompt mode %q", opts.PromptMode)
	}
	return CommandAgent{opts: opts}, nil
}

func NewPiAgent(opts PiAgentOptions) CommandAgent {
	mode := strings.ToLower(strings.TrimSpace(opts.Mode))
	if mode == "" {
		mode = PiModeSDK
	}
	if mode == PiModeCLI {
		return NewPiCLIAgent(opts)
	}
	return NewPiSDKAgent(opts)
}

func NewPiSDKAgent(opts PiAgentOptions) CommandAgent {
	if strings.TrimSpace(opts.SDKBin) == "" {
		opts.SDKBin = "bun"
	}
	if strings.TrimSpace(opts.SDKScript) == "" {
		opts.SDKScript = defaultPiSDKBlurScript()
	}
	if opts.Timeout <= 0 {
		opts.Timeout = 2 * time.Minute
	}
	args := []string{"run", opts.SDKScript}
	if cwd := strings.TrimSpace(opts.SDKCWD); cwd != "" {
		args = append(args, "--cwd", cwd)
	}
	if agentDir := strings.TrimSpace(opts.AgentDir); agentDir != "" {
		args = append(args, "--agent-dir", agentDir)
	}
	if provider := strings.TrimSpace(opts.Provider); provider != "" {
		args = append(args, "--provider", provider)
	}
	if model := strings.TrimSpace(opts.Model); model != "" {
		args = append(args, "--model", model)
	}
	if thinking := strings.TrimSpace(opts.Thinking); thinking != "" {
		args = append(args, "--thinking-level", thinking)
	}
	agent, _ := NewCommandAgent(CommandAgentOptions{
		Name:       BackendPi + "-sdk",
		Bin:        opts.SDKBin,
		Args:       args,
		PromptMode: PromptModeStdin,
		Timeout:    opts.Timeout,
	})
	return agent
}

func NewPiCLIAgent(opts PiAgentOptions) CommandAgent {
	if strings.TrimSpace(opts.PiBin) == "" {
		opts.PiBin = "pi"
	}
	if opts.Timeout <= 0 {
		opts.Timeout = 2 * time.Minute
	}
	args := []string{"-p", "--no-tools", "--no-context-files", "--no-session", "--mode", "text"}
	if opts.NoExtensions {
		args = append(args, "--no-extensions")
	}
	if provider := strings.TrimSpace(opts.Provider); provider != "" {
		args = append(args, "--provider", provider)
	}
	if model := strings.TrimSpace(opts.Model); model != "" {
		args = append(args, "--model", model)
	}
	agent, _ := NewCommandAgent(CommandAgentOptions{
		Name:       BackendPi,
		Bin:        opts.PiBin,
		Args:       args,
		PromptMode: PromptModeStdin,
		Timeout:    opts.Timeout,
	})
	return agent
}

func defaultPiSDKBlurScript() string {
	if raw := strings.TrimSpace(os.Getenv("FOXCTL_PI_SDK_BLUR_SCRIPT")); raw != "" {
		return raw
	}
	candidates := []string{
		filepath.Join("integrations", "pi", "memory-blur-agent.ts"),
		filepath.Join(".", "integrations", "pi", "memory-blur-agent.ts"),
	}
	if _, file, _, ok := runtime.Caller(0); ok {
		repoRoot := filepath.Clean(filepath.Join(filepath.Dir(file), "..", "..", ".."))
		candidates = append(candidates, filepath.Join(repoRoot, "integrations", "pi", "memory-blur-agent.ts"))
	}
	for _, candidate := range candidates {
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}
	}
	return filepath.Join("integrations", "pi", "memory-blur-agent.ts")
}

func NewClaudeAgent(opts ClaudeAgentOptions) CommandAgent {
	if strings.TrimSpace(opts.ClaudeBin) == "" {
		opts.ClaudeBin = "claude"
	}
	if opts.Timeout <= 0 {
		opts.Timeout = 2 * time.Minute
	}
	args := []string{"-p", "--output-format", "text", "--no-session-persistence", "--tools", ""}
	if model := strings.TrimSpace(opts.Model); model != "" {
		args = append(args, "--model", model)
	}
	agent, _ := NewCommandAgent(CommandAgentOptions{
		Name:       BackendClaude,
		Bin:        opts.ClaudeBin,
		Args:       args,
		PromptMode: PromptModeStdin,
		Timeout:    opts.Timeout,
	})
	return agent
}

func NewHermesAgent(opts HermesAgentOptions) CommandAgent {
	if strings.TrimSpace(opts.HermesBin) == "" {
		opts.HermesBin = "hermes"
	}
	if opts.Timeout <= 0 {
		opts.Timeout = 2 * time.Minute
	}
	args := []string{}
	if provider := strings.TrimSpace(opts.Provider); provider != "" {
		args = append(args, "--provider", provider)
	}
	if model := strings.TrimSpace(opts.Model); model != "" {
		args = append(args, "--model", model)
	}
	if opts.IgnoreRules {
		args = append(args, "--ignore-rules")
	}
	if opts.IgnoreUserConfig {
		args = append(args, "--ignore-user-config")
	}
	args = append(args, "-z")
	agent, _ := NewCommandAgent(CommandAgentOptions{
		Name:       BackendHermes,
		Bin:        opts.HermesBin,
		Args:       args,
		PromptMode: PromptModeArg,
		Timeout:    opts.Timeout,
	})
	return agent
}

func (a CommandAgent) BlurMemory(ctx context.Context, input contextplane.MemoryBlurAgentPromptInput) (contextplane.MemoryBlurAgentOutput, string, error) {
	prompt, err := contextplane.BuildMemoryBlurAgentPrompt(input)
	if err != nil {
		return contextplane.MemoryBlurAgentOutput{}, "", err
	}
	raw, err := a.BlurPrompt(ctx, prompt)
	if err != nil {
		return contextplane.MemoryBlurAgentOutput{}, raw, err
	}
	output, err := contextplane.ParseMemoryBlurAgentOutput(raw)
	if err != nil {
		return contextplane.MemoryBlurAgentOutput{}, raw, err
	}
	return output, raw, nil
}

func (a CommandAgent) BlurPrompt(ctx context.Context, prompt string) (string, error) {
	timeout := a.opts.Timeout
	if timeout <= 0 {
		timeout = 2 * time.Minute
	}
	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	args := append([]string(nil), a.opts.Args...)
	if a.opts.PromptMode == PromptModeArg {
		args = append(args, prompt)
	}
	proc := exec.CommandContext(runCtx, a.opts.Bin, args...)
	if a.opts.PromptMode == PromptModeStdin {
		proc.Stdin = strings.NewReader(prompt)
	}
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	proc.Stdout = &stdout
	proc.Stderr = &stderr
	if err := proc.Run(); err != nil {
		detail := strings.TrimSpace(stderr.String())
		if detail == "" {
			detail = strings.TrimSpace(stdout.String())
		}
		if detail == "" {
			detail = err.Error()
		}
		name := strings.TrimSpace(a.opts.Name)
		if name == "" {
			name = a.opts.Bin
		}
		return stdout.String(), fmt.Errorf("run %s blur agent: %s", name, detail)
	}
	return stdout.String(), nil
}

type FoxctlAgent struct {
	opts FoxctlAgentOptions
}

func NewFoxctlAgent(opts FoxctlAgentOptions) (FoxctlAgent, error) {
	if strings.TrimSpace(opts.FoxctlBin) == "" {
		opts.FoxctlBin = "foxctl"
	}
	if strings.TrimSpace(opts.AgentID) == "" {
		return FoxctlAgent{}, fmt.Errorf("foxctl agent id is required")
	}
	if opts.Timeout <= 0 {
		opts.Timeout = 5 * time.Minute
	}
	return FoxctlAgent{opts: opts}, nil
}

func (a FoxctlAgent) BlurMemory(ctx context.Context, input contextplane.MemoryBlurAgentPromptInput) (contextplane.MemoryBlurAgentOutput, string, error) {
	prompt, err := contextplane.BuildMemoryBlurAgentPrompt(input)
	if err != nil {
		return contextplane.MemoryBlurAgentOutput{}, "", err
	}
	raw, err := a.BlurPrompt(ctx, prompt)
	if err != nil {
		return contextplane.MemoryBlurAgentOutput{}, raw, err
	}
	output, err := contextplane.ParseMemoryBlurAgentOutput(raw)
	if err != nil {
		return contextplane.MemoryBlurAgentOutput{}, raw, err
	}
	return output, raw, nil
}

func (a FoxctlAgent) BlurPrompt(ctx context.Context, prompt string) (string, error) {
	timeout := a.opts.Timeout
	if timeout <= 0 {
		timeout = 5 * time.Minute
	}
	args := []string{
		"agent", "ask", strings.TrimSpace(a.opts.AgentID),
		"--question", prompt,
		"--kind", "context",
		"--wait",
		"--timeout", timeout.String(),
	}
	if dispatcher := strings.TrimSpace(a.opts.Dispatcher); dispatcher != "" {
		args = append(args, "--dispatcher", dispatcher)
	}
	if conversationID := strings.TrimSpace(a.opts.ConversationID); conversationID != "" {
		args = append(args, "--conversation-id", conversationID)
	}
	command, err := NewCommandAgent(CommandAgentOptions{
		Name:       BackendFoxctl,
		Bin:        a.opts.FoxctlBin,
		Args:       args,
		PromptMode: PromptModeStdin,
		Timeout:    timeout,
	})
	if err != nil {
		return "", err
	}
	raw, err := command.BlurPrompt(ctx, "")
	if err != nil {
		return raw, err
	}
	return extractFoxctlAgentResponse(raw)
}

func extractFoxctlAgentResponse(raw string) (string, error) {
	var lastResponse string
	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || !strings.HasPrefix(line, "{") {
			continue
		}
		var env struct {
			Command string         `json:"command"`
			Data    map[string]any `json:"data"`
		}
		if err := json.Unmarshal([]byte(line), &env); err != nil {
			continue
		}
		answer, _ := env.Data["answer"].(map[string]any)
		if answer == nil {
			continue
		}
		if response, ok := answer["response"].(string); ok && strings.TrimSpace(response) != "" {
			lastResponse = response
		}
	}
	if strings.TrimSpace(lastResponse) == "" {
		return raw, nil
	}
	return lastResponse, nil
}
