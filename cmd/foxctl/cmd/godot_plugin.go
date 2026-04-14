package cmd

import (
	"embed"
	"fmt"
	"os"
	"path/filepath"

	"github.com/joshka0/foxctl/internal/protocol"
	"github.com/spf13/cobra"
)

//go:embed godot_plugin/*
var godotPluginFS embed.FS

func newGodotPluginInstallCommand() *cobra.Command {
	var force bool

	cmd := &cobra.Command{
		Use:   "plugin-install <project-path>",
		Short: "Install the GodotAIBridge plugin to a Godot project",
		Long: `Install the GodotAIBridge plugin to the specified Godot project directory.

The plugin will be installed to <project-path>/addons/godot_ai_bridge/.
After installation, enable the plugin in Godot via Project > Project Settings > Plugins.`,
		Example: `  # Install to current directory (if it's a Godot project)
  foxctl godot plugin-install .

  # Install to a specific project
  foxctl godot plugin-install ~/projects/my-game

  # Force reinstall (overwrite existing)
  foxctl godot plugin-install ~/projects/my-game --force`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			projectPath := args[0]

			// Resolve to absolute path
			absPath, err := filepath.Abs(projectPath)
			if err != nil {
				return fmt.Errorf("resolve path: %w", err)
			}

			// Check if it looks like a Godot project
			projectFile := filepath.Join(absPath, "project.godot")
			if _, err := os.Stat(projectFile); os.IsNotExist(err) {
				return writeGodotPluginError(cmd, "ENOTGODOT",
					fmt.Sprintf("No project.godot found at %s", absPath),
					"Ensure the path points to a Godot project directory containing a project.godot file.")
			}

			// Target directory
			addonDir := filepath.Join(absPath, "addons", "godot_ai_bridge")

			// Check if already exists
			if _, err := os.Stat(addonDir); err == nil && !force {
				return writeGodotPluginError(cmd, "EEXISTS",
					fmt.Sprintf("Plugin already installed at %s", addonDir),
					"Use --force to overwrite the existing installation.")
			}

			// Create addon directory
			if err := os.MkdirAll(addonDir, 0o755); err != nil {
				return fmt.Errorf("create addon directory: %w", err)
			}

			// Copy embedded files
			files := []string{"plugin.cfg", "bridge.gd", "README.md"}
			for _, name := range files {
				content, err := godotPluginFS.ReadFile("godot_plugin/" + name)
				if err != nil {
					return fmt.Errorf("read embedded %s: %w", name, err)
				}

				destPath := filepath.Join(addonDir, name)
				if err := os.WriteFile(destPath, content, 0o644); err != nil {
					return fmt.Errorf("write %s: %w", name, err)
				}
			}

			// Success response
			data := map[string]any{
				"installed_to": addonDir,
				"files":        files,
				"next_steps": []string{
					"Open the project in Godot Editor",
					"Go to Project > Project Settings > Plugins",
					"Enable 'GodotAIBridge'",
					"Verify '[GodotAIBridge] Listening on 127.0.0.1:7777' appears in Output",
				},
			}
			env := protocol.OK("godot/plugin-install", data, protocol.WithSource("cli"))
			return protocol.Write(cmd.OutOrStdout(), env)
		},
	}

	cmd.Flags().BoolVar(&force, "force", false, "Overwrite existing plugin installation")
	return cmd
}

func writeGodotPluginError(cmd *cobra.Command, code, message, hint string) error {
	data := protocol.ErrorData{
		Hint: hint,
	}
	env := protocol.ErrorWithData("godot/plugin-install", protocol.ErrorCode(code), message, data, protocol.WithSource("cli"))
	if err := protocol.Write(cmd.OutOrStdout(), env); err != nil {
		return fmt.Errorf("write error envelope: %w", err)
	}
	return fmt.Errorf("%s", message)
}
