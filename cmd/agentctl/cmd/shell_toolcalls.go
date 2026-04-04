package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/jkatigb/agentctl/internal/protocol"
	"github.com/jkatigb/agentctl/internal/sessionkit/claudejsonl"
	"github.com/jkatigb/agentctl/internal/sessionkit/codexjsonl"
	"github.com/spf13/cobra"
)

type transcriptCount struct {
	Name   string `json:"name"`
	Total  int    `json:"total"`
	Claude int    `json:"claude"`
	Codex  int    `json:"codex"`
}

type transcriptSourceSummary struct {
	Files     int `json:"files"`
	Messages  int `json:"messages"`
	ToolCalls int `json:"tool_calls"`
	Commands  int `json:"commands"`
}

type transcriptAccumulator struct {
	toolTotals        map[string]*transcriptCount
	commandFamilies   map[string]*transcriptCount
	exactCommands     map[string]*transcriptCount
	commandToolTotals map[string]*transcriptCount
	claude            transcriptSourceSummary
	codex             transcriptSourceSummary
}

func newTranscriptAccumulator() *transcriptAccumulator {
	return &transcriptAccumulator{
		toolTotals:        make(map[string]*transcriptCount),
		commandFamilies:   make(map[string]*transcriptCount),
		exactCommands:     make(map[string]*transcriptCount),
		commandToolTotals: make(map[string]*transcriptCount),
	}
}

func newShellToolcallsCommand() *cobra.Command {
	var (
		source          string
		claudeDir       string
		codexHome       string
		transcriptLimit int
		limit           int
		commandLimit    int
		saveFile        string
	)

	cmd := &cobra.Command{
		Use:   "toolcalls",
		Short: "Mine Claude and Codex transcripts for tool-call and command-family usage",
		Long: `Shell toolcalls scans local Claude and Codex transcript JSONL files and
aggregates:
- top tool names
- top command-carrying tools
- top shell command families
- top exact shell commands

This is useful for deciding which shell-like workflows are worth optimizing.`,
		Example: `  agentctl shell toolcalls
  agentctl shell toolcalls --source claude --transcript-limit 200
  agentctl shell toolcalls --save-file .agentctl/reports/transcript-toolcalls.json`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runShellToolcallsCommand(cmd, source, claudeDir, codexHome, transcriptLimit, limit, commandLimit, saveFile)
		},
	}
	cmd.Flags().StringVar(&source, "source", "all", "Transcript source: all, claude, or codex")
	cmd.Flags().StringVar(&claudeDir, "claude-dir", "~/.claude/transcripts", "Claude transcript directory")
	cmd.Flags().StringVar(&codexHome, "codex-home", "~/.codex", "Codex home directory")
	cmd.Flags().IntVar(&transcriptLimit, "transcript-limit", 200, "Maximum transcript files to scan per source (0 = unlimited)")
	cmd.Flags().IntVar(&limit, "limit", 20, "Maximum rows in each top-N list")
	cmd.Flags().IntVar(&commandLimit, "command-limit", 20, "Maximum rows in command-family and exact-command lists")
	cmd.Flags().StringVar(&saveFile, "save-file", "", "Optional path to write the JSON report payload")
	return cmd
}

func runShellToolcallsCommand(cmd *cobra.Command, source, claudeDir, codexHome string, transcriptLimit, limit, commandLimit int, saveFile string) error {
	source = strings.ToLower(strings.TrimSpace(source))
	if source == "" {
		source = "all"
	}
	if source != "all" && source != "claude" && source != "codex" {
		return protocol.WriteError(cmd.OutOrStdout(), "agentctl.shell.toolcalls", protocol.ErrorCodeEARG, "source must be one of: all, claude, codex", nil, protocol.WithSource("cli"))
	}

	acc, err := collectTranscriptAccumulator(source, claudeDir, codexHome, transcriptLimit)
	if err != nil {
		return protocol.WriteError(cmd.OutOrStdout(), "agentctl.shell.toolcalls", protocol.ErrorCodeERuntime, err.Error(), nil, protocol.WithSource("cli"))
	}

	payload := map[string]any{
		"source":           source,
		"transcript_limit": transcriptLimit,
		"claude_dir":       expandHomePath(claudeDir),
		"codex_home":       expandHomePath(codexHome),
		"sources": map[string]any{
			"claude": acc.claude,
			"codex":  acc.codex,
		},
		"top_tools":            topCounts(acc.toolTotals, limit),
		"command_like_tools":   topCounts(acc.commandToolTotals, limit),
		"top_command_families": topCounts(acc.commandFamilies, commandLimit),
		"top_exact_commands":   topCounts(acc.exactCommands, commandLimit),
	}

	if strings.TrimSpace(saveFile) != "" {
		if err := saveShellReport(resolveShellWorkspace("."), saveFile, payload); err != nil {
			return protocol.WriteError(cmd.OutOrStdout(), "agentctl.shell.toolcalls", protocol.ErrorCodeERuntime, err.Error(), nil, protocol.WithSource("cli"))
		}
		payload["saved_to"] = saveFile
	}

	return protocol.Write(cmd.OutOrStdout(), protocol.OK("agentctl.shell.toolcalls", payload, protocol.WithSource("cli")))
}

func collectTranscriptAccumulator(source, claudeDir, codexHome string, transcriptLimit int) (*transcriptAccumulator, error) {
	source = strings.ToLower(strings.TrimSpace(source))
	if source == "" {
		source = "all"
	}

	acc := newTranscriptAccumulator()
	if source == "all" || source == "claude" {
		files, err := listClaudeTranscriptFiles(expandHomePath(claudeDir))
		if err != nil {
			return nil, err
		}
		if transcriptLimit > 0 && len(files) > transcriptLimit {
			files = files[:transcriptLimit]
		}
		if err := scanClaudeTranscriptFiles(files, acc); err != nil {
			return nil, err
		}
	}

	if source == "all" || source == "codex" {
		files, err := codexjsonl.ListSessionFiles(expandHomePath(codexHome))
		if err != nil {
			return nil, err
		}
		if transcriptLimit > 0 && len(files) > transcriptLimit {
			files = files[:transcriptLimit]
		}
		if err := scanCodexTranscriptFiles(files, acc); err != nil {
			return nil, err
		}
	}

	return acc, nil
}

func expandHomePath(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return path
	}
	if path == "~" || strings.HasPrefix(path, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			if path == "~" {
				return home
			}
			return filepath.Join(home, strings.TrimPrefix(path, "~/"))
		}
	}
	return path
}

func listClaudeTranscriptFiles(dir string) ([]string, error) {
	seen := make(map[string]struct{})
	var matches []string

	appendMatch := func(path string) {
		path = strings.TrimSpace(path)
		if path == "" {
			return
		}
		if _, ok := seen[path]; ok {
			return
		}
		seen[path] = struct{}{}
		matches = append(matches, path)
	}

	rootMatches, err := filepath.Glob(filepath.Join(dir, "*.jsonl"))
	if err != nil {
		return nil, err
	}
	for _, match := range rootMatches {
		appendMatch(match)
	}

	claudeRoot := filepath.Dir(dir)
	projectsDir := filepath.Join(claudeRoot, "projects")
	_ = filepath.WalkDir(projectsDir, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil || d == nil || d.IsDir() {
			return nil
		}
		if filepath.Ext(path) == ".jsonl" {
			appendMatch(path)
		}
		return nil
	})

	sort.Slice(matches, func(i, j int) bool {
		ai, aerr := os.Stat(matches[i])
		bi, berr := os.Stat(matches[j])
		if aerr != nil || berr != nil {
			return matches[i] < matches[j]
		}
		return ai.ModTime().After(bi.ModTime())
	})
	return matches, nil
}

func scanClaudeTranscriptFiles(paths []string, acc *transcriptAccumulator) error {
	for _, path := range paths {
		reader, err := claudejsonl.OpenReader(path)
		if err != nil {
			return fmt.Errorf("open claude transcript %s: %w", path, err)
		}
		acc.claude.Files++
		for {
			rm, err := reader.Next()
			if err != nil {
				_ = reader.Close()
				return fmt.Errorf("read claude transcript %s: %w", path, err)
			}
			if rm == nil || rm.Message == nil {
				break
			}
			acc.claude.Messages++
			msg := rm.Message
			tools := claudejsonl.ExtractTools(msg)
			if len(tools) > 0 {
				acc.claude.ToolCalls += len(tools)
			}
			for _, tool := range tools {
				incrementCount(acc.toolTotals, normalizeToolName(tool), "claude")
			}
			for _, anchor := range claudejsonl.ExtractCommands(msg) {
				command := strings.TrimSpace(anchor.Content)
				if command == "" {
					continue
				}
				acc.claude.Commands++
				incrementCount(acc.commandToolTotals, "bash", "claude")
				incrementCount(acc.exactCommands, command, "claude")
				incrementCount(acc.commandFamilies, deriveReportOperation(command), "claude")
			}
		}
		_ = reader.Close()
	}
	return nil
}

func scanCodexTranscriptFiles(files []codexjsonl.SessionFile, acc *transcriptAccumulator) error {
	for _, file := range files {
		reader, err := codexjsonl.OpenReader(file.Path)
		if err != nil {
			return fmt.Errorf("open codex transcript %s: %w", file.Path, err)
		}
		acc.codex.Files++
		for {
			rm, err := reader.Next()
			if err != nil {
				_ = reader.Close()
				return fmt.Errorf("read codex transcript %s: %w", file.Path, err)
			}
			if rm == nil || rm.Message == nil {
				break
			}
			acc.codex.Messages++
			msg := rm.Message
			if msg.Type != "response_item" || len(msg.Payload) == 0 {
				continue
			}
			var item codexjsonl.ResponseItem
			if err := json.Unmarshal(msg.Payload, &item); err != nil {
				continue
			}
			if item.Type != "function_call" && item.Type != "custom_tool_call" {
				continue
			}
			toolName := normalizeToolName(item.Name)
			if toolName == "" {
				continue
			}
			acc.codex.ToolCalls++
			incrementCount(acc.toolTotals, toolName, "codex")
			command := extractCodexCommand(item.Arguments)
			if command == "" {
				continue
			}
			acc.codex.Commands++
			incrementCount(acc.commandToolTotals, toolName, "codex")
			incrementCount(acc.exactCommands, command, "codex")
			incrementCount(acc.commandFamilies, deriveReportOperation(command), "codex")
		}
		_ = reader.Close()
	}
	return nil
}

func extractCodexCommand(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var payload any
	if err := json.Unmarshal(raw, &payload); err != nil {
		return ""
	}
	if text, ok := payload.(string); ok {
		text = strings.TrimSpace(text)
		if text == "" {
			return ""
		}
		var nested any
		if json.Unmarshal([]byte(text), &nested) == nil {
			return firstCommandInValue(nested)
		}
		return text
	}
	return firstCommandInValue(payload)
}

func firstCommandInValue(value any) string {
	switch typed := value.(type) {
	case map[string]any:
		for key, val := range typed {
			k := strings.ToLower(strings.TrimSpace(key))
			if strings.Contains(k, "command") || k == "cmd" || strings.Contains(k, "shell") {
				if s, ok := val.(string); ok && strings.TrimSpace(s) != "" {
					return strings.TrimSpace(s)
				}
			}
		}
		for _, val := range typed {
			if cmd := firstCommandInValue(val); cmd != "" {
				return cmd
			}
		}
	case []any:
		for _, item := range typed {
			if cmd := firstCommandInValue(item); cmd != "" {
				return cmd
			}
		}
	}
	return ""
}

func incrementCount(target map[string]*transcriptCount, key, source string) {
	key = strings.TrimSpace(key)
	if key == "" {
		return
	}
	row := target[key]
	if row == nil {
		row = &transcriptCount{Name: key}
		target[key] = row
	}
	row.Total++
	switch source {
	case "claude":
		row.Claude++
	case "codex":
		row.Codex++
	}
}

func topCounts(target map[string]*transcriptCount, limit int) []transcriptCount {
	rows := make([]transcriptCount, 0, len(target))
	for _, item := range target {
		rows = append(rows, *item)
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].Total != rows[j].Total {
			return rows[i].Total > rows[j].Total
		}
		return rows[i].Name < rows[j].Name
	})
	if limit > 0 && len(rows) > limit {
		rows = rows[:limit]
	}
	return rows
}

func normalizeToolName(name string) string {
	return strings.TrimSpace(strings.ToLower(name))
}
