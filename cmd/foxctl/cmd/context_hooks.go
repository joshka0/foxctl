package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/joshka0/foxctl/internal/domain/envelope"
	"github.com/spf13/cobra"
)

type claudeHookCommand struct {
	Type    string `json:"type"`
	Command string `json:"command"`
	Timeout int    `json:"timeout,omitempty"`
}

type claudeHookMatcher struct {
	Matcher string              `json:"matcher"`
	Hooks   []claudeHookCommand `json:"hooks"`
}

type claudeSettings struct {
	Env         map[string]string              `json:"env,omitempty"`
	Permissions map[string]any                 `json:"permissions,omitempty"`
	Hooks       map[string][]claudeHookMatcher `json:"hooks,omitempty"`
}

func newContextHooksCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "hooks",
		Short: "Manage ContextWiki hook wiring for Claude settings",
	}
	cmd.AddCommand(newContextHooksInstallCommand())
	return cmd
}

func newContextHooksInstallCommand() *cobra.Command {
	var workspacePath string
	var dryRun bool

	cmd := &cobra.Command{
		Use:   "install",
		Short: "Install ContextWiki lifecycle hooks into workspace .claude/settings.json",
		RunE: func(cmd *cobra.Command, _ []string) error {
			target := resolveContextWorkspace(workspacePath)
			settingsPath := filepath.Join(target, ".claude", "settings.json")

			settings, created, err := loadClaudeSettings(settingsPath)
			if err != nil {
				return fmt.Errorf("load claude settings: %w", err)
			}
			installed := mergeContextWikiHooks(&settings)

			out := map[string]any{
				"workspace_path": target,
				"settings_path":  settingsPath,
				"created":        created,
				"installed":      installed,
				"dry_run":        dryRun,
			}

			if !dryRun {
				if err := writeClaudeSettings(settingsPath, settings); err != nil {
					return fmt.Errorf("write claude settings: %w", err)
				}
			}

			env := envelope.OK("context/hooks_install", out, envelope.WithMeta(envelope.Meta{Source: "cli"}))
			return envelope.Write(cmd.OutOrStdout(), env)
		},
	}

	cmd.Flags().StringVar(&workspacePath, "workspace", "", "Workspace path (default: auto-detect from cwd)")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Preview changes without writing settings.json")
	return cmd
}

func loadClaudeSettings(path string) (claudeSettings, bool, error) {
	body, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return claudeSettings{}, true, nil
		}
		return claudeSettings{}, false, err
	}
	var settings claudeSettings
	if err := json.Unmarshal(body, &settings); err != nil {
		return claudeSettings{}, false, err
	}
	return settings, false, nil
}

func writeClaudeSettings(path string, settings claudeSettings) error {
	if settings.Hooks == nil {
		settings.Hooks = map[string][]claudeHookMatcher{}
	}
	body, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return err
	}
	body = append(body, '\n')
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, body, 0o644)
}

func mergeContextWikiHooks(settings *claudeSettings) map[string]bool {
	if settings.Hooks == nil {
		settings.Hooks = map[string][]claudeHookMatcher{}
	}
	installed := map[string]bool{
		"SessionStart": false,
		"Stop":         false,
		"SubagentStop": false,
	}
	specs := map[string]claudeHookMatcher{
		"SessionStart": {
			Matcher: "startup|compact|resume",
			Hooks: []claudeHookCommand{
				{Type: "command", Command: "$CLAUDE_PROJECT_DIR/configs/hooks/session-init.sh", Timeout: 10},
			},
		},
		"Stop": {
			Matcher: "",
			Hooks: []claudeHookCommand{
				{Type: "command", Command: "$CLAUDE_PROJECT_DIR/configs/hooks/session-end.sh", Timeout: 15},
			},
		},
		"SubagentStop": {
			Matcher: "",
			Hooks: []claudeHookCommand{
				{Type: "command", Command: "$CLAUDE_PROJECT_DIR/configs/hooks/subagent-stop.sh", Timeout: 10},
			},
		},
	}

	for event, matcher := range specs {
		list := settings.Hooks[event]
		command := matcher.Hooks[0].Command
		if hasHookCommand(list, command) {
			installed[event] = false
			continue
		}
		settings.Hooks[event] = append(list, matcher)
		installed[event] = true
	}
	return installed
}

func hasHookCommand(matchers []claudeHookMatcher, want string) bool {
	want = strings.TrimSpace(want)
	for _, matcher := range matchers {
		for _, hook := range matcher.Hooks {
			if strings.TrimSpace(hook.Command) == want {
				return true
			}
		}
	}
	return false
}
