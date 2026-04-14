package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/joshka0/foxctl/cmd/foxctl/cmd/memorycmd"
	envpkg "github.com/joshka0/foxctl/internal/domain/envelope"
	"github.com/joshka0/foxctl/internal/domain/skill"
	"github.com/joshka0/foxctl/internal/platform/config"
	"github.com/joshka0/foxctl/internal/protocol"
	"github.com/joshka0/foxctl/internal/tooling/shellreduce"
	"github.com/spf13/cobra"
)

var errShellEnvelopeWritten = errors.New("shell envelope already written")

func init() {
	rootCmd.AddCommand(newShellCommand())
}

func newShellCommand() *cobra.Command {
	var (
		commandString string
		measure       bool
		tokenModel    string
		workspace     string
	)

	cmd := &cobra.Command{
		Use:     "shell [-- command...]",
		Aliases: []string{"bash"},
		Short:   "Route common shell commands through structured reducers",
		Long: `Shell routes common shell-style commands onto existing structured skills.

This keeps the default path deterministic and compact for agent use:
  ls/tree        -> fs skills
  find           -> fs/find
  cat/read       -> fs/read
  grep/rg        -> text/grep
  git status     -> git/status
  git diff       -> git/status diff
  git log        -> git/status log
  go/cargo test  -> test/run
  pytest         -> test/run
  npm/pnpm/yarn  -> test/run
  ruff check     -> native reducer
  docker ps      -> native reducer
  pytest         -> test/run
  npm test       -> test/run

Unsupported commands return a structured error instead of silently falling back
to raw shell output.`,
		Example: `  foxctl shell -- ls -la src/
  foxctl shell -- grep -rn "pub fn" src/
  foxctl shell -- git log --stat -10
  foxctl shell -- find internal -name "*.go"
  foxctl shell -- pytest -k unit tests/
  foxctl shell -- npm test --prefix packages/gui-agent
  foxctl shell --measure -- git diff --name-only
  foxctl bash -- go test -race ./...`,
		Args: cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runShellCommand(cmd, workspace, commandString, measure, tokenModel, args)
		},
	}

	cmd.Flags().StringVar(&commandString, "command", "", "Shell command string to route (alternative to passing argv after --)")
	cmd.Flags().BoolVar(&measure, "measure", false, "Measure raw command output size and token estimates against the reduced summary")
	cmd.Flags().StringVar(&tokenModel, "token-model", "cl100k_base", "Tokenizer model or encoding for measurement (for example cl100k_base or gpt-4o-mini)")
	cmd.Flags().StringVar(&workspace, "workspace", ".", "Workspace root override")
	cmd.AddCommand(newShellReportCommand(), newShellToolcallsCommand())
	return cmd
}

func runShellCommand(cmd *cobra.Command, workspaceOverride, commandString string, measure bool, tokenModel string, args []string) error {
	argv, err := resolveShellArgv(commandString, args)
	if err != nil {
		return protocol.WriteError(cmd.OutOrStdout(), "foxctl.shell", protocol.ErrorCodeEARG, err.Error(), map[string]any{
			"hint": "Pass a supported command after -- or with --command. Supported: " + strings.Join(shellreduce.SupportedFamilies(), ", "),
		}, protocol.WithSource("cli"), protocol.WithWorkspace(resolveShellWorkspace(workspaceOverride)))
	}

	route, err := shellreduce.RouteArgv(argv)
	if err != nil {
		code := protocol.ErrorCodeEARG
		data := map[string]any{
			"input": map[string]any{
				"argv":      argv,
				"command":   shellreduce.JoinCommand(argv),
				"workspace": resolveShellWorkspace(workspaceOverride),
			},
			"supported": shellreduce.SupportedFamilies(),
		}
		var unsupported shellreduce.ErrUnsupported
		if errors.As(err, &unsupported) {
			data["hint"] = unsupported.Reason
		}
		return protocol.WriteError(cmd.OutOrStdout(), "foxctl.shell", code, err.Error(), data, protocol.WithSource("cli"), protocol.WithWorkspace(resolveShellWorkspace(workspaceOverride)))
	}

	resultData, routeErr := executeStructuredShellRoute(cmd, workspaceOverride, route)
	if errors.Is(routeErr, errShellEnvelopeWritten) {
		return nil
	}
	if routeErr != nil {
		return protocol.WriteError(cmd.OutOrStdout(), "foxctl.shell", protocol.ErrorCodeERuntime, routeErr.Error(), map[string]any{
			"input": map[string]any{
				"argv":      argv,
				"command":   shellreduce.JoinCommand(argv),
				"workspace": resolveShellWorkspace(workspaceOverride),
			},
			"route": map[string]any{
				"intent": route.Intent,
				"skill":  route.Skill,
				"notes":  route.Notes,
			},
		}, protocol.WithSource("cli"), protocol.WithWorkspace(resolveShellWorkspace(workspaceOverride)))
	}
	if resultData == nil {
		return routeErr
	}

	payload := map[string]any{
		"input": map[string]any{
			"argv":      argv,
			"command":   shellreduce.JoinCommand(argv),
			"workspace": resolveShellWorkspace(workspaceOverride),
		},
		"route": map[string]any{
			"intent": route.Intent,
			"skill":  route.Skill,
			"native": route.Native,
			"notes":  route.Notes,
		},
		"summary": shellreduce.Summarize(route, resultData),
		"result":  resultData,
	}
	if measure {
		payload["measure"] = shellreduce.Measure(cmd.Context(), resolveShellWorkspace(workspaceOverride), argv, stringValueFromAny(payload["summary"]), shellreduce.MeasureOptions{
			TokenModel: tokenModel,
		})
	}

	if artifact := extractNestedArtifact(resultData); artifact != "" {
		payload["result_artifact"] = artifact
	}

	env := protocol.OK("foxctl.shell", payload, protocol.WithSource("cli"), protocol.WithWorkspace(resolveShellWorkspace(workspaceOverride)))
	return protocol.Write(cmd.OutOrStdout(), env)
}

func resolveShellArgv(commandString string, args []string) ([]string, error) {
	if strings.TrimSpace(commandString) != "" && len(args) > 0 {
		return nil, fmt.Errorf("use either --command or argv after --, not both")
	}
	if strings.TrimSpace(commandString) != "" {
		return shellreduce.SplitCommand(commandString)
	}
	if len(args) == 0 {
		return nil, fmt.Errorf("command is required")
	}
	return args, nil
}

func executeStructuredShellRoute(cmd *cobra.Command, workspaceOverride string, route shellreduce.Route) (map[string]any, error) {
	workspaceRoot := resolveShellWorkspace(workspaceOverride)
	if strings.TrimSpace(route.Native) != "" {
		return shellreduce.ExecuteNative(cmd.Context(), workspaceRoot, route)
	}

	inputBytes, err := json.Marshal(route.Input)
	if err != nil {
		return nil, fmt.Errorf("marshal shell route input: %w", err)
	}

	cfg := config.MustFromContext(cmd.Context())
	resolver := skill.NewResolver(skill.WithSearchPaths(
		filepath.Join(workspaceRoot, "dist", "skills"),
		filepath.Join(workspaceRoot, "skills"),
	))
	handle, err := resolver.Resolve(route.Skill)
	if err != nil {
		handle, err = createSkillResolver(cfg).Resolve(route.Skill)
	}
	if err != nil {
		if writeErr := protocol.WriteError(cmd.OutOrStdout(), "foxctl.shell", protocol.ErrorCodeENotFound, err.Error(), map[string]any{
			"hint":  "build the routed skills with make skills-build or install the missing skill",
			"skill": route.Skill,
		}, protocol.WithSource("cli"), protocol.WithWorkspace(resolveShellWorkspace(workspaceOverride))); writeErr != nil {
			return nil, writeErr
		}
		return nil, errShellEnvelopeWritten
	}

	cwd, err := os.Getwd()
	if err != nil {
		return nil, fmt.Errorf("get working directory: %w", err)
	}
	manifest, artifactPath, err := loadRefactorManifestAndArtifact(handle.ManifestPath, cwd)
	if err != nil {
		return nil, fmt.Errorf("resolve skill artifact: %w", err)
	}

	runCtx := resolveWorkspaceContext(cmd.Context(), workspaceOverride)
	stdout, stderr, err := executeSkill(runCtx, manifest, artifactPath, inputBytes)
	if len(stderr) > 0 {
		if _, werr := cmd.ErrOrStderr().Write(append(stderr, '\n')); werr != nil {
			return nil, werr
		}
	}
	if err != nil {
		return nil, err
	}

	env, err := protocol.DecodeEnvelope(stdout)
	if err != nil {
		return nil, fmt.Errorf("decode routed skill envelope: %w", err)
	}
	if env.Status == envpkg.StatusError {
		if writeErr := memorycmd.WriteEnvelope(cmd.OutOrStdout(), stdout); writeErr != nil {
			return nil, writeErr
		}
		return nil, errShellEnvelopeWritten
	}

	data, ok := env.Data.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("routed skill %s returned non-object data", route.Skill)
	}
	return data, nil
}

func resolveShellWorkspace(workspaceOverride string) string {
	if strings.TrimSpace(workspaceOverride) == "" {
		if cwd, err := os.Getwd(); err == nil {
			return cwd
		}
		return "."
	}
	if abs, err := filepath.Abs(workspaceOverride); err == nil {
		return abs
	}
	return workspaceOverride
}

func extractNestedArtifact(data map[string]any) string {
	if data == nil {
		return ""
	}
	if artifact, ok := data["artifact"].(string); ok {
		return strings.TrimSpace(artifact)
	}
	return ""
}

func stringValueFromAny(value any) string {
	if s, ok := value.(string); ok {
		return s
	}
	return ""
}

func newShellReportCommand() *cobra.Command {
	var (
		commands         []string
		commandsFile     string
		preset           string
		saveFile         string
		transcriptSource string
		transcriptLimit  int
		claudeDir        string
		codexHome        string
		tokenModel       string
		workspace        string
	)

	cmd := &cobra.Command{
		Use:   "report",
		Short: "Measure many supported shell reductions and aggregate the savings",
		Long: `Shell report runs the reduced shell path in measurement mode for many commands
and aggregates raw-vs-reduced bytes and token estimates.

Commands can be provided with repeated --command flags or a plain-text file
containing one command per line. Blank lines and # comments are ignored.`,
		Example: `  foxctl shell report \
    --command 'git log --stat -5' \
    --command 'find internal -name "*.go"'

  foxctl shell report --commands-file .foxctl/shell-bench.txt

  foxctl shell report --preset typical-bash

  foxctl shell report --preset transcript-derived

  foxctl shell report --preset typical-bash --save-file .foxctl/reports/shell-typical.json`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runShellReportCommand(cmd, workspace, commands, commandsFile, preset, saveFile, transcriptSource, transcriptLimit, claudeDir, codexHome, tokenModel)
		},
	}
	cmd.Flags().StringArrayVar(&commands, "command", nil, "Supported shell-style command to measure (repeatable)")
	cmd.Flags().StringVar(&commandsFile, "commands-file", "", "Plain-text file containing one command per line")
	cmd.Flags().StringVar(&preset, "preset", "", "Built-in weighted benchmark preset (for example: typical-bash or transcript-derived)")
	cmd.Flags().StringVar(&saveFile, "save-file", "", "Optional path to write the JSON report payload")
	cmd.Flags().StringVar(&transcriptSource, "transcript-source", "all", "Transcript source for transcript-derived preset: all, claude, or codex")
	cmd.Flags().IntVar(&transcriptLimit, "transcript-limit", 200, "Maximum transcript files to scan per source for transcript-derived preset")
	cmd.Flags().StringVar(&claudeDir, "claude-dir", "~/.claude/transcripts", "Claude transcript directory for transcript-derived preset")
	cmd.Flags().StringVar(&codexHome, "codex-home", "~/.codex", "Codex home directory for transcript-derived preset")
	cmd.Flags().StringVar(&tokenModel, "token-model", "cl100k_base", "Tokenizer model or encoding for measurement")
	cmd.Flags().StringVar(&workspace, "workspace", ".", "Workspace root override")
	return cmd
}

type shellReportSpec struct {
	Command    string
	Operation  string
	Weight     int
	Source     string
	Optional   bool
	SkipReason string
}

func runShellReportCommand(cmd *cobra.Command, workspaceOverride string, commands []string, commandsFile, preset, saveFile, transcriptSource string, transcriptLimit int, claudeDir, codexHome, tokenModel string) error {
	items, err := loadShellReportCommands(resolveShellWorkspace(workspaceOverride), commands, commandsFile, preset, transcriptSource, transcriptLimit, claudeDir, codexHome)
	if err != nil {
		return protocol.WriteError(cmd.OutOrStdout(), "foxctl.shell.report", protocol.ErrorCodeEARG, err.Error(), map[string]any{
			"hint": "Use repeated --command flags, --commands-file, or --preset typical-bash / transcript-derived",
		}, protocol.WithSource("cli"), protocol.WithWorkspace(resolveShellWorkspace(workspaceOverride)))
	}

	rows := make([]shellreduce.ReportCase, 0, len(items))
	for _, item := range items {
		if strings.TrimSpace(item.SkipReason) != "" {
			rows = append(rows, shellreduce.SkippedCase(item.Operation, item.Weight, item.SkipReason))
			continue
		}
		data, invokeErr := invokeMeasuredShell(cmd.Context(), resolveShellWorkspace(workspaceOverride), tokenModel, item.Command)
		if invokeErr != nil {
			var row shellreduce.ReportCase
			if item.Optional {
				row = shellreduce.OptionalUnavailableCase(item.Command, item.Operation, item.Weight, invokeErr.Error())
			} else {
				row = shellreduce.ErrorReportCase(item.Command, invokeErr.Error())
				row.Operation = item.Operation
				row.Weight = item.Weight
			}
			rows = append(rows, row)
			continue
		}
		rows = append(rows, shellreduce.BuildReportCase(item.Command, item.Operation, item.Weight, data))
	}

	payload := map[string]any{
		"workspace":         resolveShellWorkspace(workspaceOverride),
		"token_model":       tokenModel,
		"case_count":        len(rows),
		"cases":             rows,
		"summary":           shellreduce.SummarizeReport(rows),
		"supported":         shellreduce.SupportedFamilies(),
		"preset":            preset,
		"transcript_source": transcriptSource,
		"transcript_limit":  transcriptLimit,
	}
	if strings.TrimSpace(saveFile) != "" {
		if err := saveShellReport(resolveShellWorkspace(workspaceOverride), saveFile, payload); err != nil {
			return protocol.WriteError(cmd.OutOrStdout(), "foxctl.shell.report", protocol.ErrorCodeERuntime, err.Error(), nil, protocol.WithSource("cli"), protocol.WithWorkspace(resolveShellWorkspace(workspaceOverride)))
		}
		payload["saved_to"] = saveFile
	}
	return protocol.Write(cmd.OutOrStdout(), protocol.OK("foxctl.shell.report", payload, protocol.WithSource("cli"), protocol.WithWorkspace(resolveShellWorkspace(workspaceOverride))))
}

func loadShellReportCommands(workspace string, commands []string, commandsFile, preset, transcriptSource string, transcriptLimit int, claudeDir, codexHome string) ([]shellReportSpec, error) {
	items := make([]shellReportSpec, 0, len(commands))
	for _, command := range commands {
		command = strings.TrimSpace(command)
		if command != "" {
			items = append(items, shellReportSpec{
				Command:   command,
				Operation: deriveReportOperation(command),
				Weight:    1,
				Source:    "flag",
			})
		}
	}
	if strings.TrimSpace(commandsFile) != "" {
		body, err := os.ReadFile(commandsFile)
		if err != nil {
			return nil, fmt.Errorf("read commands file: %w", err)
		}
		for _, line := range strings.Split(string(body), "\n") {
			line = strings.TrimSpace(line)
			if line == "" || strings.HasPrefix(line, "#") {
				continue
			}
			items = append(items, shellReportSpec{
				Command:   line,
				Operation: deriveReportOperation(line),
				Weight:    1,
				Source:    "file",
			})
		}
	}
	if strings.TrimSpace(preset) != "" {
		presetItems, err := shellReportPresetSpecs(strings.TrimSpace(preset), workspace, transcriptSource, transcriptLimit, claudeDir, codexHome)
		if err != nil {
			return nil, err
		}
		items = append(items, presetItems...)
	}
	if len(items) == 0 {
		return nil, fmt.Errorf("at least one command is required")
	}
	return items, nil
}

func deriveReportOperation(command string) string {
	command = trimToTailCommand(command)
	argv, err := shellreduce.SplitCommand(command)
	if err != nil || len(argv) == 0 {
		return command
	}
	argv = stripLeadingEnvAssignments(argv)
	if len(argv) == 0 {
		return command
	}
	if shellInvocation := unwrapShellInvocation(argv); len(shellInvocation) > 0 {
		return deriveReportOperation(shellreduce.JoinCommand(shellInvocation))
	}
	switch argv[0] {
	case "git":
		if len(argv) > 3 && argv[1] == "-C" {
			return "git " + argv[3]
		}
		if len(argv) > 1 {
			return "git " + argv[1]
		}
	case "pnpm":
		if len(argv) > 3 && argv[1] == "--dir" {
			return "pnpm " + argv[3]
		}
		if len(argv) > 2 && strings.HasPrefix(argv[1], "--dir=") {
			return "pnpm " + argv[2]
		}
		if len(argv) > 1 {
			return "pnpm " + argv[1]
		}
	case "yarn":
		if len(argv) > 3 && argv[1] == "--cwd" {
			return "yarn " + argv[3]
		}
		if len(argv) > 2 && strings.HasPrefix(argv[1], "--cwd=") {
			return "yarn " + argv[2]
		}
		if len(argv) > 1 {
			return "yarn " + argv[1]
		}
	case "kubectl":
		if len(argv) > 2 && argv[1] == "rollout" {
			return "kubectl rollout " + argv[2]
		}
		if len(argv) > 1 {
			return "kubectl " + argv[1]
		}
	case "mix":
		if len(argv) > 1 {
			return "mix " + argv[1]
		}
	case "tmux":
		if len(argv) > 1 {
			return "tmux " + argv[1]
		}
	case "go", "cargo", "npm", "ruff", "docker":
		if len(argv) > 1 {
			return argv[0] + " " + argv[1]
		}
	case "python":
		if len(argv) > 2 && argv[1] == "-m" {
			return argv[0] + " " + argv[1] + " " + argv[2]
		}
	case "sed", "rg", "nl", "ls", "find", "cat":
		return argv[0]
	}
	return argv[0]
}

func trimToTailCommand(command string) string {
	command = strings.TrimSpace(command)
	for _, sep := range []string{"&&", ";"} {
		if idx := strings.LastIndex(command, sep); idx >= 0 {
			command = strings.TrimSpace(command[idx+len(sep):])
		}
	}
	return command
}

func stripLeadingEnvAssignments(argv []string) []string {
	index := 0
	for index < len(argv) && looksLikeEnvAssignment(argv[index]) {
		index++
	}
	return argv[index:]
}

func looksLikeEnvAssignment(token string) bool {
	if !strings.Contains(token, "=") || strings.HasPrefix(token, "=") {
		return false
	}
	if strings.Contains(token, "/") {
		return false
	}
	key := token[:strings.Index(token, "=")]
	if key == "" {
		return false
	}
	for i, r := range key {
		switch {
		case i == 0 && ((r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z') || r == '_'):
		case i > 0 && ((r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '_'):
		default:
			return false
		}
	}
	return true
}

func unwrapShellInvocation(argv []string) []string {
	if len(argv) >= 3 && (argv[0] == "bash" || argv[0] == "zsh" || argv[0] == "sh") && argv[1] == "-lc" {
		inner, err := shellreduce.SplitCommand(argv[2])
		if err == nil {
			return inner
		}
	}
	return nil
}

type shellWorkspaceProfile struct {
	HasGo     bool
	HasNode   bool
	HasPython bool
	HasRuff   bool
	HasRust   bool
	HasElixir bool
}

func shellReportPresetSpecs(preset, workspace, transcriptSource string, transcriptLimit int, claudeDir, codexHome string) ([]shellReportSpec, error) {
	switch preset {
	case "typical-bash":
		profile := detectShellWorkspaceProfile(workspace)
		specs := []shellReportSpec{
			{Operation: "ls / tree", Command: "ls -la internal", Weight: 10, Source: "preset:typical-bash"},
			{Operation: "cat / read", Command: "cat go.mod", Weight: 20, Source: "preset:typical-bash", Optional: !profile.HasGo},
			{Operation: "grep / rg", Command: "grep -rn 'func ' internal", Weight: 8, Source: "preset:typical-bash"},
			{Operation: "sed / nl", Command: "sed -n '1,120p' cmd/foxctl/cmd/shell.go", Weight: 8, Source: "preset:typical-bash"},
			{Operation: "git status", Command: "git status --short", Weight: 10, Source: "preset:typical-bash"},
			{Operation: "git diff", Command: "git diff --stat", Weight: 5, Source: "preset:typical-bash"},
			{Operation: "git log", Command: "git log --stat -5", Weight: 5, Source: "preset:typical-bash"},
			{Operation: "git add/commit/push", Weight: 8, Source: "preset:typical-bash", SkipReason: "mutating command family intentionally excluded from reducer benchmark"},
		}
		if profile.HasNode {
			specs = append(specs, shellReportSpec{Operation: "cargo test / npm test", Command: "npm test --prefix packages/gui-agent", Weight: 5, Source: "preset:typical-bash"})
		} else if profile.HasRust {
			specs = append(specs, shellReportSpec{Operation: "cargo test / npm test", Command: "cargo test", Weight: 5, Source: "preset:typical-bash"})
		}
		if profile.HasRuff {
			specs = append(specs, shellReportSpec{Operation: "ruff check", Command: "ruff check .", Weight: 3, Source: "preset:typical-bash"})
		}
		if profile.HasPython {
			specs = append(specs, shellReportSpec{Operation: "pytest", Command: "python -m pytest .", Weight: 4, Source: "preset:typical-bash"})
		}
		if profile.HasGo {
			specs = append(specs, shellReportSpec{Operation: "go test", Command: "go test ./internal/tooling/shellreduce", Weight: 3, Source: "preset:typical-bash"})
		}
		specs = append(specs, shellReportSpec{Operation: "docker ps", Command: "docker ps", Weight: 3, Source: "preset:typical-bash", Optional: true})
		return specs, nil
	case "transcript-derived":
		acc, err := collectTranscriptAccumulator(transcriptSource, claudeDir, codexHome, transcriptLimit)
		if err != nil {
			return nil, err
		}
		return transcriptDerivedReportSpecs(workspace, acc), nil
	default:
		return nil, fmt.Errorf("unknown shell report preset %q", preset)
	}
}

func detectShellWorkspaceProfile(workspace string) shellWorkspaceProfile {
	workspace = resolveShellWorkspace(workspace)
	return shellWorkspaceProfile{
		HasGo:     shellMarkerExists(workspace, "go.mod"),
		HasNode:   shellMarkerExists(workspace, "package.json"),
		HasPython: shellMarkerExists(workspace, "pyproject.toml") || shellMarkerExists(workspace, "pytest.ini") || shellMarkerExists(workspace, "setup.py"),
		HasRuff:   shellMarkerExists(workspace, "ruff.toml") || shellMarkerExists(workspace, ".ruff.toml") || shellMarkerExists(workspace, "pyproject.toml"),
		HasRust:   shellMarkerExists(workspace, "Cargo.toml"),
		HasElixir: shellMarkerExists(workspace, "mix.exs"),
	}
}

func shellMarkerExists(workspace, name string) bool {
	_, err := os.Stat(filepath.Join(workspace, name))
	return err == nil
}

func saveShellReport(workspace, target string, payload map[string]any) error {
	if strings.TrimSpace(target) == "" {
		return nil
	}
	if !filepath.IsAbs(target) {
		target = filepath.Join(workspace, target)
	}
	target = filepath.Clean(target)
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return fmt.Errorf("mkdir report dir: %w", err)
	}
	body, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal report: %w", err)
	}
	if err := os.WriteFile(target, append(body, '\n'), 0o644); err != nil {
		return fmt.Errorf("write report: %w", err)
	}
	return nil
}

func transcriptDerivedReportSpecs(workspace string, acc *transcriptAccumulator) []shellReportSpec {
	profile := detectShellWorkspaceProfile(workspace)
	weights := aggregateTranscriptFamilyWeights(acc.commandFamilies)
	specs := make([]shellReportSpec, 0, len(weights))

	choose := func(operation string, fallback string) string {
		if exact := bestTranscriptCommandForOperation(acc.exactCommands, operation); exact != "" {
			if candidate := benchmarkableTranscriptCommand(workspace, exact, fallback); candidate != "" {
				return candidate
			}
		}
		return fallback
	}

	add := func(operation, command string, weight int, optional bool, skipReason string) {
		if weight <= 0 {
			return
		}
		specs = append(specs, shellReportSpec{
			Operation:  operation,
			Command:    command,
			Weight:     weight,
			Source:     "preset:transcript-derived",
			Optional:   optional,
			SkipReason: skipReason,
		})
	}

	add("grep / rg", choose("grep / rg", "grep -rn 'func ' internal"), weights["grep / rg"], false, "")
	add("git status", choose("git status", "git status --short"), weights["git status"], false, "")
	add("git diff", choose("git diff", "git diff --stat"), weights["git diff"], false, "")
	add("git log", choose("git log", "git log --stat -5"), weights["git log"], false, "")
	add("find", choose("find", "find internal -name '*.go'"), weights["find"], false, "")
	add("ls / tree", choose("ls / tree", "ls -la internal"), weights["ls / tree"], false, "")
	add("cat / read", choose("cat / read", "cat go.mod"), weights["cat / read"], !profile.HasGo, "")
	add("head", choose("head", "head -n 20 go.mod"), weights["head"], !profile.HasGo, "")
	add("tail", choose("tail", "tail -n 20 go.mod"), weights["tail"], !profile.HasGo, "")
	add("wc", choose("wc", "wc -l go.mod"), weights["wc"], !profile.HasGo, "")
	add("go test", choose("go test", "go test ./internal/tooling/shellreduce"), weights["go test"], !profile.HasGo, "")
	if profile.HasNode {
		add("node test", choose("node test", "npm test --prefix packages/gui-agent"), weights["node test"], false, "")
	} else {
		add("node test", "", weights["node test"], false, "node workspace not detected in current repo")
	}
	if profile.HasRust {
		add("cargo test", choose("cargo test", "cargo test"), weights["cargo test"], false, "")
	} else {
		add("cargo test", "", weights["cargo test"], false, "rust workspace not detected in current repo")
	}
	if profile.HasElixir {
		add("mix compile", choose("mix compile", "mix compile"), weights["mix compile"], false, "")
		add("mix test", choose("mix test", "mix test"), weights["mix test"], false, "")
		add("mix format", choose("mix format", "mix format"), weights["mix format"], false, "")
		add("mix deps.get", choose("mix deps.get", "mix deps.get"), weights["mix deps.get"], false, "")
	} else {
		add("mix compile", "", weights["mix compile"], false, "elixir workspace not detected in current repo")
		add("mix test", "", weights["mix test"], false, "elixir workspace not detected in current repo")
		add("mix format", "", weights["mix format"], false, "elixir workspace not detected in current repo")
		add("mix deps.get", "", weights["mix deps.get"], false, "elixir workspace not detected in current repo")
	}
	if profile.HasPython {
		add("pytest", choose("pytest", "python -m pytest ."), weights["pytest"], false, "")
	} else {
		add("pytest", "", weights["pytest"], true, "python test workspace not detected in current repo")
	}
	if profile.HasRuff {
		add("ruff check", choose("ruff check", "ruff check ."), weights["ruff check"], false, "")
	} else {
		add("ruff check", "", weights["ruff check"], true, "ruff project markers not detected in current repo")
	}
	add("docker ps", choose("docker ps", "docker ps"), weights["docker ps"], true, "")
	add("kubectl get", choose("kubectl get", "kubectl get pods -A"), weights["kubectl get"], true, "")
	add("kubectl describe", choose("kubectl describe", "kubectl describe pods -A"), weights["kubectl describe"], true, "")
	add("kubectl logs", choose("kubectl logs", "kubectl logs deployment/example --tail=50"), weights["kubectl logs"], true, "")
	add("kubectl rollout status", choose("kubectl rollout status", "kubectl rollout status deployment/example --timeout=180s"), weights["kubectl rollout status"], true, "")
	add("tmux capture-pane", choose("tmux capture-pane", bestTMUXCaptureFallback()), weights["tmux capture-pane"], true, "")
	add("sed / nl", choose("sed / nl", "sed -n '1,120p' cmd/foxctl/cmd/shell.go"), weights["sed / nl"], false, "")
	add("kubectl exec", "", weights["kubectl exec"], false, "kubectl exec is intentionally not reduced")
	add("kubectl port-forward", "", weights["kubectl port-forward"], false, "kubectl port-forward is intentionally not reduced")
	add("mix run", "", weights["mix run"], false, "mix run reducer not implemented")
	add("mix ecto", "", weights["mix ecto"], false, "mix ecto reducer not implemented")
	add("git add/commit/push", "", weights["git add/commit/push"], false, "mutating command family intentionally excluded from reducer benchmark")
	add("cd / shell wrapper", "", weights["cd / shell wrapper"], false, "navigation wrapper family not benchmarked directly")

	sort.Slice(specs, func(i, j int) bool {
		if specs[i].Weight != specs[j].Weight {
			return specs[i].Weight > specs[j].Weight
		}
		return specs[i].Operation < specs[j].Operation
	})
	return specs
}

func bestTranscriptCommandForOperation(exact map[string]*transcriptCount, operation string) string {
	bestCount := 0
	bestCommand := ""
	for command, item := range exact {
		if deriveReportOperation(command) != operation {
			continue
		}
		if item.Total > bestCount || (item.Total == bestCount && command < bestCommand) {
			bestCount = item.Total
			bestCommand = command
		}
	}
	return bestCommand
}

func benchmarkableTranscriptCommand(workspace, command, fallback string) string {
	command = strings.TrimSpace(command)
	if command == "" {
		return fallback
	}
	argv, err := shellreduce.SplitCommand(command)
	if err != nil {
		return fallback
	}
	route, err := shellreduce.RouteArgv(argv)
	if err != nil {
		return fallback
	}
	switch route.Intent {
	case "file_head", "file_tail", "file_wc", "file_wc_slice", "read", "line_range":
		path := routeFilePath(route.Input)
		if path == "" {
			return fallback
		}
		full := path
		if !filepath.IsAbs(full) {
			full = filepath.Join(workspace, full)
		}
		if _, err := os.Stat(full); err != nil {
			return fallback
		}
	}
	return command
}

func bestTMUXCaptureFallback() string {
	cmd := exec.Command("tmux", "list-panes", "-a", "-F", "#{pane_id}")
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line != "" {
			return "tmux capture-pane -t " + line + " -p -S -50 | tail -n 30"
		}
	}
	return ""
}

func routeFilePath(input map[string]any) string {
	if input == nil {
		return ""
	}
	if value, ok := input["path"].(string); ok && strings.TrimSpace(value) != "" {
		return strings.TrimSpace(value)
	}
	if value, ok := input["file_path"].(string); ok && strings.TrimSpace(value) != "" {
		return strings.TrimSpace(value)
	}
	return ""
}

func aggregateTranscriptFamilyWeights(counts map[string]*transcriptCount) map[string]int {
	out := map[string]int{}
	for key, item := range counts {
		name := strings.TrimSpace(key)
		switch name {
		case "rg", "grep":
			out["grep / rg"] += item.Total
		case "git status":
			out["git status"] += item.Total
		case "git diff":
			out["git diff"] += item.Total
		case "git log":
			out["git log"] += item.Total
		case "find":
			out["find"] += item.Total
		case "ls", "tree":
			out["ls / tree"] += item.Total
		case "cat", "read":
			out["cat / read"] += item.Total
		case "go test":
			out["go test"] += item.Total
		case "cargo test":
			out["cargo test"] += item.Total
		case "npm test", "pnpm test", "yarn test", "pnpm run", "npm run", "yarn run":
			out["node test"] += item.Total
		case "python -m pytest", "pytest":
			out["pytest"] += item.Total
		case "ruff check", "ruff":
			out["ruff check"] += item.Total
		case "docker ps":
			out["docker ps"] += item.Total
		case "sed", "nl":
			out["sed / nl"] += item.Total
		case "head":
			out["head"] += item.Total
		case "tail":
			out["tail"] += item.Total
		case "wc":
			out["wc"] += item.Total
		case "kubectl get":
			out["kubectl get"] += item.Total
		case "kubectl describe":
			out["kubectl describe"] += item.Total
		case "kubectl logs":
			out["kubectl logs"] += item.Total
		case "kubectl rollout status":
			out["kubectl rollout status"] += item.Total
		case "kubectl exec":
			out["kubectl exec"] += item.Total
		case "kubectl port-forward":
			out["kubectl port-forward"] += item.Total
		case "mix compile":
			out["mix compile"] += item.Total
		case "mix test":
			out["mix test"] += item.Total
		case "mix format":
			out["mix format"] += item.Total
		case "mix deps.get":
			out["mix deps.get"] += item.Total
		case "mix run":
			out["mix run"] += item.Total
		case "mix ecto.create", "mix ecto.migrate", "mix ecto.drop":
			out["mix ecto"] += item.Total
		case "tmux capture-pane":
			out["tmux capture-pane"] += item.Total
		case "git add", "git commit", "git push":
			out["git add/commit/push"] += item.Total
		case "cd":
			out["cd / shell wrapper"] += item.Total
		}
	}
	return out
}

func invokeMeasuredShell(ctx context.Context, workspace, tokenModel, command string) (map[string]any, error) {
	bin := "foxctl"
	if exe, err := os.Executable(); err == nil && strings.TrimSpace(exe) != "" {
		bin = exe
	}
	args := []string{"shell", "--measure", "--token-model", tokenModel, "--workspace", workspace, "--command", command}
	execCmd := exec.CommandContext(ctx, bin, args...)
	execCmd.Dir = workspace

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	execCmd.Stdout = &stdout
	execCmd.Stderr = &stderr
	err := execCmd.Run()

	output := stdout.Bytes()
	if len(output) == 0 && err != nil {
		return nil, fmt.Errorf("run measured shell: %w (%s)", err, strings.TrimSpace(stderr.String()))
	}
	env, decodeErr := protocol.DecodeEnvelope(output)
	if decodeErr != nil {
		return nil, fmt.Errorf("decode shell report envelope: %w", decodeErr)
	}
	if env.Status == envpkg.StatusError {
		return nil, protocol.EnvelopeStatusErrorFromEnvelope(env)
	}
	data, ok := env.Data.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("shell result data is not an object")
	}
	return data, nil
}
