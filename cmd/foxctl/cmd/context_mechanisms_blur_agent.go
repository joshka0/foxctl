package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/joshka0/foxctl/internal/context/contextengine"
	"github.com/joshka0/foxctl/internal/context/contextplane"
	"github.com/joshka0/foxctl/internal/domain/envelope"
	"github.com/joshka0/foxctl/internal/platform/config"
	"github.com/joshka0/foxctl/internal/runtime/memoryblur"
	"github.com/spf13/cobra"
)

type repoSymbolBlurAgentOptions struct {
	WorkspacePath          string
	WorkspaceID            string
	MaxSymbols             int
	PerNodeCap             int
	Query                  mechanismCandidateFilter
	Agent                  string
	AgentBin               string
	AgentProvider          string
	AgentModel             string
	AgentCommand           string
	AgentPrompt            string
	PiMode                 string
	PiSDKBin               string
	PiSDKScript            string
	PiSDKCWD               string
	PiAgentDir             string
	PiThinking             string
	PiNoExtensions         bool
	HermesIgnoreRules      bool
	HermesIgnoreUserConfig bool
	FoxctlAgentID          string
	FoxctlDispatcher       string
	FoxctlConversationID   string
	Timeout                time.Duration
	IncludePrompt          bool
	IncludeRaw             bool
	IncludeVectors         bool
}

type repoSymbolBlurAgentView struct {
	Query          mechanismCandidateView                  `json:"query"`
	PromptInput    contextplane.MemoryBlurAgentPromptInput `json:"prompt_input"`
	Agent          string                                  `json:"agent"`
	AgentOutput    contextplane.MemoryBlurAgentOutput      `json:"agent_output"`
	Validation     contextplane.MemoryBlurValidation       `json:"validation"`
	Blurred        mechanismCandidateView                  `json:"blurred"`
	Prompt         string                                  `json:"prompt,omitempty"`
	RawAgentOutput string                                  `json:"raw_agent_output,omitempty"`
	ReadOnly       bool                                    `json:"read_only"`
}

func newContextMechanismsRepoSymbolsBlurAgentCommand() *cobra.Command {
	opts := repoSymbolBlurAgentOptions{
		Agent:             "pi",
		PiMode:            memoryblur.PiModeSDK,
		PiSDKBin:          "bun",
		PiThinking:        "off",
		PiNoExtensions:    true,
		HermesIgnoreRules: true,
		Timeout:           2 * time.Minute,
		MaxSymbols:        defaultContextMechanismMaxSymbols,
		PerNodeCap:        200,
	}
	cmd := &cobra.Command{
		Use:   "blur-agent",
		Short: "Ask a real agent to blur one repo-symbol mechanism and validate leakage",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runRepoSymbolsBlurAgent(cmd.Context(), cmd, opts)
		},
	}
	addContextMechanismRepoSymbolFlags(cmd, &opts.WorkspacePath, &opts.WorkspaceID, &opts.MaxSymbols, &opts.PerNodeCap)
	cmd.Flags().StringVar(&opts.Query.SymbolID, "query-symbol-id", "", "Query symbol ID; accepts repoindex node ID, raw symbol key, or embedding symbol ID")
	cmd.Flags().StringVar(&opts.Query.Name, "query-name", "", "Exact query symbol name")
	cmd.Flags().StringVar(&opts.Query.File, "query-file", "", "Exact query symbol file")
	cmd.Flags().StringVar(&opts.Agent, "agent", "pi", "Real agent runner to use (pi|hermes|claude|foxctl|command)")
	cmd.Flags().StringVar(&opts.AgentBin, "agent-bin", "", "Executable override for hermes/claude/foxctl backends or Pi CLI mode")
	cmd.Flags().StringVar(&opts.AgentProvider, "agent-provider", "", "Optional provider override for Pi or Hermes")
	cmd.Flags().StringVar(&opts.AgentModel, "agent-model", "", "Optional model override for Pi, Hermes, or Claude")
	cmd.Flags().StringVar(&opts.AgentCommand, "agent-command", "", "JSON array command for --agent command, e.g. '[\"my-agent\",\"--oneshot\"]'")
	cmd.Flags().StringVar(&opts.AgentPrompt, "agent-prompt-mode", memoryblur.PromptModeStdin, "Prompt delivery for --agent command (stdin|arg)")
	cmd.Flags().StringVar(&opts.PiMode, "pi-mode", memoryblur.PiModeSDK, "Pi runner mode (sdk|cli)")
	cmd.Flags().StringVar(&opts.PiSDKBin, "pi-sdk-bin", "bun", "Executable for Pi SDK runner")
	cmd.Flags().StringVar(&opts.PiSDKScript, "pi-sdk-script", "", "Pi SDK memory blur runner script path")
	cmd.Flags().StringVar(&opts.PiSDKCWD, "pi-sdk-cwd", "", "Working directory passed to Pi SDK session")
	cmd.Flags().StringVar(&opts.PiAgentDir, "pi-agent-dir", "", "Pi agent directory for auth/models; defaults to Pi SDK default")
	cmd.Flags().StringVar(&opts.PiThinking, "pi-thinking", "off", "Pi SDK thinking level")
	cmd.Flags().BoolVar(&opts.PiNoExtensions, "pi-no-extensions", true, "Disable Pi extension discovery for deterministic non-interactive blurring")
	cmd.Flags().BoolVar(&opts.HermesIgnoreRules, "hermes-ignore-rules", true, "Pass --ignore-rules to Hermes one-shot runs")
	cmd.Flags().BoolVar(&opts.HermesIgnoreUserConfig, "hermes-ignore-user-config", false, "Pass --ignore-user-config to Hermes one-shot runs")
	cmd.Flags().StringVar(&opts.FoxctlAgentID, "foxctl-agent-id", "", "foxctl agent id/name/slug for --agent foxctl")
	cmd.Flags().StringVar(&opts.FoxctlDispatcher, "foxctl-dispatcher", "", "Optional foxctl agent ask dispatcher (mailbox|jido)")
	cmd.Flags().StringVar(&opts.FoxctlConversationID, "foxctl-conversation-id", "", "Optional foxctl agent conversation id")
	cmd.Flags().DurationVar(&opts.Timeout, "timeout", 2*time.Minute, "Agent call timeout")
	cmd.Flags().BoolVar(&opts.IncludePrompt, "include-prompt", false, "Include the generated agent prompt in output")
	cmd.Flags().BoolVar(&opts.IncludeRaw, "include-raw", false, "Include raw agent stdout in output")
	cmd.Flags().BoolVar(&opts.IncludeVectors, "include-vectors", false, "Include literal and structural vectors in output")
	return cmd
}

func runRepoSymbolsBlurAgent(ctx context.Context, cmd *cobra.Command, opts repoSymbolBlurAgentOptions) error {
	target := resolveContextWorkspace(opts.WorkspacePath)
	cfg, err := loadConfig(ctx, config.WithWorkspacePath(target))
	if err != nil {
		return err
	}
	result, resolvedWorkspaceID, err := buildRepoSymbolMechanismsForCommand(ctx, cfg, target, opts.WorkspaceID, opts.MaxSymbols, opts.PerNodeCap)
	if err != nil {
		return err
	}
	queryCandidate, ok := selectMechanismQueryCandidate(result.Candidates, opts.Query)
	if !ok {
		return fmt.Errorf("query symbol not found in mechanism candidate corpus; pass --query-name, --query-file, or --query-symbol-id")
	}
	forbiddenTerms := repoSymbolBlurForbiddenTerms(queryCandidate)
	promptInput := contextplane.MemoryBlurPromptInputFromProjection(queryCandidate.Projection, queryCandidate.StructuralShape, forbiddenTerms)
	prompt, err := contextplane.BuildMemoryBlurAgentPrompt(promptInput)
	if err != nil {
		return err
	}
	agent, err := memoryBlurAgentForOptions(opts)
	if err != nil {
		return err
	}
	agentOutput, rawOutput, err := agent.BlurMemory(ctx, promptInput)
	if err != nil {
		return err
	}
	blurredProjection, validation := contextplane.ApplyMemoryBlurAgentOutput(queryCandidate.Projection, promptInput, agentOutput)
	if !validation.Valid {
		if len(validation.LeakedTerms) > 0 {
			return fmt.Errorf("blur agent validation failed: %s (leaked_terms=%s)", strings.Join(validation.Errors, "; "), strings.Join(validation.LeakedTerms, ", "))
		}
		return fmt.Errorf("blur agent validation failed: %s", strings.Join(validation.Errors, "; "))
	}
	blurredCandidate := queryCandidate
	blurredCandidate.Projection = blurredProjection
	view := repoSymbolBlurAgentView{
		Query:       mechanismCandidateViewFor(queryCandidate, true, opts.IncludeVectors),
		PromptInput: promptInput,
		Agent:       strings.TrimSpace(opts.Agent),
		AgentOutput: agentOutput,
		Validation:  validation,
		Blurred:     mechanismCandidateViewFor(blurredCandidate, true, opts.IncludeVectors),
		ReadOnly:    true,
	}
	if opts.IncludePrompt {
		view.Prompt = prompt
	}
	if opts.IncludeRaw {
		view.RawAgentOutput = rawOutput
	}
	return envelope.Write(cmd.OutOrStdout(), envelope.OK("context/mechanisms_repo_symbols_blur_agent", map[string]any{
		"workspace_path":      target,
		"workspace_id":        resolvedWorkspaceID,
		"skipped_unembedded":  result.SkippedUnembedded,
		"skipped_invalid":     result.SkippedInvalid,
		"candidate_count":     len(result.Candidates),
		"blur":                view,
		"read_only":           true,
		"agent_boundary":      "memoryblur:" + strings.ToLower(strings.TrimSpace(opts.Agent)),
		"validation_boundary": "contextplane",
	}, envelope.WithMeta(envelope.Meta{Source: "cli"})))
}

func runPiMemoryBlurAgent(ctx context.Context, opts repoSymbolBlurAgentOptions, prompt string) (string, error) {
	agent := memoryblur.NewPiAgent(memoryblur.PiAgentOptions{
		PiBin:        firstNonEmpty(opts.AgentBin, "pi"),
		Mode:         opts.PiMode,
		SDKBin:       firstNonEmpty(opts.PiSDKBin, "bun"),
		SDKScript:    opts.PiSDKScript,
		SDKCWD:       opts.PiSDKCWD,
		AgentDir:     opts.PiAgentDir,
		Thinking:     opts.PiThinking,
		Provider:     opts.AgentProvider,
		Model:        opts.AgentModel,
		NoExtensions: opts.PiNoExtensions,
		Timeout:      opts.Timeout,
	})
	return agent.BlurPrompt(ctx, prompt)
}

func memoryBlurAgentForOptions(opts repoSymbolBlurAgentOptions) (contextplane.MemoryBlurAgent, error) {
	backend := strings.ToLower(strings.TrimSpace(opts.Agent))
	if backend == "" {
		backend = memoryblur.BackendPi
	}
	commandOpts, err := commandAgentOptionsFromFlags(opts)
	if err != nil {
		return nil, err
	}
	return memoryblur.NewAgent(memoryblur.AgentOptions{
		Backend: backend,
		Timeout: opts.Timeout,
		Command: commandOpts,
		Pi: memoryblur.PiAgentOptions{
			PiBin:        firstNonEmpty(opts.AgentBin, "pi"),
			Mode:         opts.PiMode,
			SDKBin:       firstNonEmpty(opts.PiSDKBin, "bun"),
			SDKScript:    opts.PiSDKScript,
			SDKCWD:       opts.PiSDKCWD,
			AgentDir:     opts.PiAgentDir,
			Thinking:     opts.PiThinking,
			Provider:     opts.AgentProvider,
			Model:        opts.AgentModel,
			NoExtensions: opts.PiNoExtensions,
			Timeout:      opts.Timeout,
		},
		Claude: memoryblur.ClaudeAgentOptions{
			ClaudeBin: firstNonEmpty(opts.AgentBin, "claude"),
			Model:     opts.AgentModel,
			Timeout:   opts.Timeout,
		},
		Hermes: memoryblur.HermesAgentOptions{
			HermesBin:        firstNonEmpty(opts.AgentBin, "hermes"),
			Provider:         opts.AgentProvider,
			Model:            opts.AgentModel,
			IgnoreRules:      opts.HermesIgnoreRules,
			IgnoreUserConfig: opts.HermesIgnoreUserConfig,
			Timeout:          opts.Timeout,
		},
		Foxctl: memoryblur.FoxctlAgentOptions{
			FoxctlBin:      firstNonEmpty(opts.AgentBin, "foxctl"),
			AgentID:        opts.FoxctlAgentID,
			Dispatcher:     opts.FoxctlDispatcher,
			ConversationID: opts.FoxctlConversationID,
			Timeout:        opts.Timeout,
		},
	})
}

func commandAgentOptionsFromFlags(opts repoSymbolBlurAgentOptions) (memoryblur.CommandAgentOptions, error) {
	raw := strings.TrimSpace(opts.AgentCommand)
	if raw == "" {
		return memoryblur.CommandAgentOptions{PromptMode: opts.AgentPrompt, Timeout: opts.Timeout}, nil
	}
	var argv []string
	if err := json.Unmarshal([]byte(raw), &argv); err != nil {
		return memoryblur.CommandAgentOptions{}, fmt.Errorf("--agent-command must be a JSON array of strings: %w", err)
	}
	if len(argv) == 0 || strings.TrimSpace(argv[0]) == "" {
		return memoryblur.CommandAgentOptions{}, fmt.Errorf("--agent-command must include an executable")
	}
	return memoryblur.CommandAgentOptions{
		Name:       memoryblur.BackendCommand,
		Bin:        argv[0],
		Args:       argv[1:],
		PromptMode: opts.AgentPrompt,
		Timeout:    opts.Timeout,
	}, nil
}

func runAgentPrompt(ctx context.Context, agent contextplane.MemoryBlurAgent, prompt string) (string, error) {
	if promptRunner, ok := agent.(interface {
		BlurPrompt(context.Context, string) (string, error)
	}); ok {
		return promptRunner.BlurPrompt(ctx, prompt)
	}
	return "", fmt.Errorf("selected blur agent does not expose direct prompt execution")
}

func repoSymbolBlurForbiddenTerms(candidate contextplane.RepoSymbolMechanismCandidate) []string {
	var terms []string
	add := func(value string) {
		value = strings.TrimSpace(filepath.ToSlash(value))
		if len(value) >= 3 {
			terms = append(terms, value)
		}
	}
	add(candidate.Projection.ID)
	add(candidate.Projection.OriginalDomain)
	add(candidate.SymbolID)
	add(candidate.Node.ID)
	add(candidate.Node.Name)
	add(candidate.Node.Pkg)
	add(candidate.Node.File)
	if candidate.Node.File != "" {
		base := filepath.Base(filepath.ToSlash(candidate.Node.File))
		add(base)
		add(strings.TrimSuffix(base, filepath.Ext(base)))
	}
	for _, ref := range candidate.Projection.SourceRefs {
		add(evidenceRefLiteral(ref))
		add(ref.Title)
	}
	return compactStringsInOrderForCommand(terms)
}

func evidenceRefLiteral(ref contextengine.EvidenceRef) string {
	return strings.TrimSpace(ref.Ref)
}

func compactStringsInOrderForCommand(values []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		key := strings.ToLower(value)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, value)
	}
	return out
}
