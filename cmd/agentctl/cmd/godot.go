package cmd

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/jkatigb/agentctl/internal/domain/envelope"
	"github.com/jkatigb/agentctl/internal/platform/config"
	"github.com/jkatigb/agentctl/internal/protocol"
	"github.com/spf13/cobra"
)

const godotSkillName = "editor/godot"

// Common flags for all godot commands.
type godotFlags struct {
	host      string
	port      int
	timeoutMS int
	dataOnly  bool
	skipCache bool
}

func newGodotCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "godot",
		Short: "Interact with the Godot Editor via GodotAIBridge plugin",
		Long: `Control a running Godot Editor instance through the GodotAIBridge plugin.

Prerequisites:
  1. Godot Editor must be running with your project open
  2. GodotAIBridge plugin must be installed and enabled
  3. Plugin listens on localhost:7777 by default

Common workflows:
  agentctl godot ping                           # Check connection
  agentctl godot scene-tree                     # Get scene hierarchy
  agentctl godot inspect /root/Main/Player      # Inspect a node
  agentctl godot create /root/Main Node2D Enemy # Create a node
  agentctl godot set /root/Main/Player position "Vector2(100, 200)"

See docs/godot/editor_integration.md for detailed documentation.`,
	}

	cmd.AddCommand(
		newGodotPluginInstallCommand(),
		newGodotPingCommand(),
		newGodotSceneTreeCommand(),
		newGodotInspectCommand(),
		newGodotCreateCommand(),
		newGodotDeleteCommand(),
		newGodotRenameCommand(),
		newGodotReparentCommand(),
		newGodotSetCommand(),
		newGodotAttachScriptCommand(),
		newGodotConnectSignalCommand(),
		newGodotClassInfoCommand(),
		newGodotSaveCommand(),
		newGodotResourcesCommand(),
		newGodotRunCommand(),
		newGodotStopCommand(),
		newGodotErrorsCommand(),
	)

	return cmd
}

func addGodotFlags(cmd *cobra.Command, f *godotFlags) {
	cmd.Flags().StringVar(&f.host, "host", "127.0.0.1", "GodotAIBridge host")
	cmd.Flags().IntVar(&f.port, "port", 7777, "GodotAIBridge port")
	cmd.Flags().IntVar(&f.timeoutMS, "timeout", 10000, "Request timeout in milliseconds")
	cmd.Flags().BoolVar(&f.dataOnly, "data-only", false, "Print only {status,data} from envelope")
	cmd.Flags().BoolVar(&f.skipCache, "skip-cache", false, "Bypass result cache")
}

func newGodotPingCommand() *cobra.Command {
	var f godotFlags

	cmd := &cobra.Command{
		Use:   "ping",
		Short: "Check connection to Godot Editor",
		Long:  "Verify that the GodotAIBridge plugin is running and responding.",
		Example: `  # Basic ping
  agentctl godot ping

  # Custom port
  agentctl godot ping --port 8888`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			payload := map[string]any{
				"action":     "ping",
				"host":       f.host,
				"port":       f.port,
				"timeout_ms": f.timeoutMS,
			}
			return runGodotSkill(cmd, payload, f.skipCache, f.dataOnly)
		},
	}

	addGodotFlags(cmd, &f)
	return cmd
}

func newGodotSceneTreeCommand() *cobra.Command {
	var f godotFlags
	var maxDepth int
	var maxNodes int

	cmd := &cobra.Command{
		Use:   "scene-tree",
		Short: "Get the current scene hierarchy",
		Long:  "Retrieve the scene tree structure from the currently open scene in Godot.",
		Example: `  # Get full scene tree
  agentctl godot scene-tree

  # Limit depth and nodes
  agentctl godot scene-tree --max-depth 3 --max-nodes 50`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			payload := map[string]any{
				"action":     "scene_tree",
				"host":       f.host,
				"port":       f.port,
				"timeout_ms": f.timeoutMS,
				"max_depth":  maxDepth,
				"max_nodes":  maxNodes,
			}
			return runGodotSkill(cmd, payload, f.skipCache, f.dataOnly)
		},
	}

	addGodotFlags(cmd, &f)
	cmd.Flags().IntVar(&maxDepth, "max-depth", 10, "Maximum depth to traverse")
	cmd.Flags().IntVar(&maxNodes, "max-nodes", 500, "Maximum nodes to return")
	return cmd
}

func newGodotInspectCommand() *cobra.Command {
	var f godotFlags

	cmd := &cobra.Command{
		Use:   "inspect <node-path>",
		Short: "Get detailed information about a node",
		Long:  "Retrieve properties, signals, groups, and script info for a specific node.",
		Example: `  # Inspect the player node
  agentctl godot inspect /root/Main/Player

  # Inspect root node
  agentctl godot inspect /root`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			payload := map[string]any{
				"action":     "node_inspect",
				"host":       f.host,
				"port":       f.port,
				"timeout_ms": f.timeoutMS,
				"node_path":  args[0],
			}
			return runGodotSkill(cmd, payload, f.skipCache, f.dataOnly)
		},
	}

	addGodotFlags(cmd, &f)
	return cmd
}

func newGodotCreateCommand() *cobra.Command {
	var f godotFlags

	cmd := &cobra.Command{
		Use:   "create <parent-path> <type> <name>",
		Short: "Create a new node in the scene",
		Long: `Create a new node as a child of the specified parent.

The node type must be a valid Godot class name (e.g., Node2D, Sprite2D, CharacterBody2D).
The operation is registered with Godot's Undo/Redo system.`,
		Example: `  # Create a Node2D named "Enemy" under /root/Main
  agentctl godot create /root/Main Node2D Enemy

  # Create a CharacterBody2D
  agentctl godot create /root/Main CharacterBody2D Player`,
		Args: cobra.ExactArgs(3),
		RunE: func(cmd *cobra.Command, args []string) error {
			payload := map[string]any{
				"action":      "node_create",
				"host":        f.host,
				"port":        f.port,
				"timeout_ms":  f.timeoutMS,
				"parent_path": args[0],
				"node_type":   args[1],
				"node_name":   args[2],
			}
			return runGodotSkill(cmd, payload, f.skipCache, f.dataOnly)
		},
	}

	addGodotFlags(cmd, &f)
	return cmd
}

func newGodotSetCommand() *cobra.Command {
	var f godotFlags

	cmd := &cobra.Command{
		Use:   "set <node-path> <property> <value>",
		Short: "Set a property on a node",
		Long: `Set a property value on the specified node.

Values are automatically converted to the appropriate Godot type:
  - Vector2: "Vector2(10, 20)" or "(10, 20)"
  - Vector3: "Vector3(1, 2, 3)" or "(1, 2, 3)"
  - Color: "Color(1, 0, 0)", "#ff0000", or "red"
  - Numbers: "42", "3.14"
  - Booleans: "true", "false"

The operation is registered with Godot's Undo/Redo system.`,
		Example: `  # Set position
  agentctl godot set /root/Main/Player position "Vector2(100, 200)"

  # Set visibility
  agentctl godot set /root/Main/Player visible false

  # Set modulate color
  agentctl godot set /root/Main/Player modulate "#ff0000"`,
		Args: cobra.ExactArgs(3),
		RunE: func(cmd *cobra.Command, args []string) error {
			payload := map[string]any{
				"action":     "node_set_prop",
				"host":       f.host,
				"port":       f.port,
				"timeout_ms": f.timeoutMS,
				"node_path":  args[0],
				"property":   args[1],
				"value":      args[2],
			}
			return runGodotSkill(cmd, payload, f.skipCache, f.dataOnly)
		},
	}

	addGodotFlags(cmd, &f)
	return cmd
}

func newGodotAttachScriptCommand() *cobra.Command {
	var f godotFlags

	cmd := &cobra.Command{
		Use:   "attach-script <node-path> <script-path>",
		Short: "Attach a script to a node",
		Long: `Attach a GDScript file to the specified node.

The script path should be a res:// path (e.g., res://scripts/player.gd).
The operation is registered with Godot's Undo/Redo system.`,
		Example: `  # Attach player script
  agentctl godot attach-script /root/Main/Player res://scripts/player.gd`,
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			payload := map[string]any{
				"action":      "node_attach_script",
				"host":        f.host,
				"port":        f.port,
				"timeout_ms":  f.timeoutMS,
				"node_path":   args[0],
				"script_path": args[1],
			}
			return runGodotSkill(cmd, payload, f.skipCache, f.dataOnly)
		},
	}

	addGodotFlags(cmd, &f)
	return cmd
}

func newGodotConnectSignalCommand() *cobra.Command {
	var f godotFlags

	cmd := &cobra.Command{
		Use:   "connect-signal <source-path> <signal> <target-path> <method>",
		Short: "Connect a signal between nodes",
		Long:  "Connect a signal from the source node to a method on the target node.",
		Example: `  # Connect button pressed signal
  agentctl godot connect-signal /root/Main/Button pressed /root/Main/Player _on_button_pressed

  # Connect area entered signal
  agentctl godot connect-signal /root/Main/Area2D body_entered /root/Main/Player _on_area_entered`,
		Args: cobra.ExactArgs(4),
		RunE: func(cmd *cobra.Command, args []string) error {
			payload := map[string]any{
				"action":      "signal_connect",
				"host":        f.host,
				"port":        f.port,
				"timeout_ms":  f.timeoutMS,
				"node_path":   args[0],
				"signal_name": args[1],
				"target_path": args[2],
				"method_name": args[3],
			}
			return runGodotSkill(cmd, payload, f.skipCache, f.dataOnly)
		},
	}

	addGodotFlags(cmd, &f)
	return cmd
}

func newGodotDeleteCommand() *cobra.Command {
	var f godotFlags

	cmd := &cobra.Command{
		Use:   "delete <node-path>",
		Short: "Delete a node from the scene",
		Long:  "Delete the specified node. The operation is registered with Godot's Undo/Redo system.",
		Example: `  # Delete a node
  agentctl godot delete /root/GameRoot/TestNode`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			payload := map[string]any{
				"action":     "node_delete",
				"host":       f.host,
				"port":       f.port,
				"timeout_ms": f.timeoutMS,
				"node_path":  args[0],
			}
			return runGodotSkill(cmd, payload, f.skipCache, f.dataOnly)
		},
	}

	addGodotFlags(cmd, &f)
	return cmd
}

func newGodotRenameCommand() *cobra.Command {
	var f godotFlags

	cmd := &cobra.Command{
		Use:   "rename <node-path> <new-name>",
		Short: "Rename a node",
		Long:  "Rename the specified node. The operation is registered with Godot's Undo/Redo system.",
		Example: `  # Rename a node
  agentctl godot rename /root/GameRoot/OldName NewName`,
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			payload := map[string]any{
				"action":     "node_rename",
				"host":       f.host,
				"port":       f.port,
				"timeout_ms": f.timeoutMS,
				"node_path":  args[0],
				"new_name":   args[1],
			}
			return runGodotSkill(cmd, payload, f.skipCache, f.dataOnly)
		},
	}

	addGodotFlags(cmd, &f)
	return cmd
}

func newGodotReparentCommand() *cobra.Command {
	var f godotFlags

	cmd := &cobra.Command{
		Use:   "reparent <node-path> <new-parent-path>",
		Short: "Move a node to a different parent",
		Long:  "Reparent the specified node to a new parent. The operation is registered with Godot's Undo/Redo system.",
		Example: `  # Move a node to a different parent
  agentctl godot reparent /root/GameRoot/Player /root/GameRoot/Entities`,
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			payload := map[string]any{
				"action":          "node_reparent",
				"host":            f.host,
				"port":            f.port,
				"timeout_ms":      f.timeoutMS,
				"node_path":       args[0],
				"new_parent_path": args[1],
			}
			return runGodotSkill(cmd, payload, f.skipCache, f.dataOnly)
		},
	}

	addGodotFlags(cmd, &f)
	return cmd
}

func newGodotClassInfoCommand() *cobra.Command {
	var f godotFlags

	cmd := &cobra.Command{
		Use:   "class-info <class-name>",
		Short: "Get information about a Godot class",
		Long:  "Query the ClassDB for methods, properties, and signals of a Godot class.",
		Example: `  # Get info about CharacterBody2D
  agentctl godot class-info CharacterBody2D

  # Get info about Node2D
  agentctl godot class-info Node2D`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			payload := map[string]any{
				"action":     "class_info",
				"host":       f.host,
				"port":       f.port,
				"timeout_ms": f.timeoutMS,
				"class_name": args[0],
			}
			return runGodotSkill(cmd, payload, f.skipCache, f.dataOnly)
		},
	}

	addGodotFlags(cmd, &f)
	return cmd
}

func newGodotSaveCommand() *cobra.Command {
	var f godotFlags

	cmd := &cobra.Command{
		Use:     "save",
		Short:   "Save the current scene",
		Long:    "Save the currently open scene to disk.",
		Example: `  agentctl godot save`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			payload := map[string]any{
				"action":     "scene_save",
				"host":       f.host,
				"port":       f.port,
				"timeout_ms": f.timeoutMS,
			}
			return runGodotSkill(cmd, payload, f.skipCache, f.dataOnly)
		},
	}

	addGodotFlags(cmd, &f)
	return cmd
}

func newGodotResourcesCommand() *cobra.Command {
	var f godotFlags
	var path string
	var pattern string
	var maxResults int

	cmd := &cobra.Command{
		Use:   "resources",
		Short: "List files in the project",
		Long:  "List files and directories in the Godot project's res:// filesystem.",
		Example: `  # List root directory
  agentctl godot resources

  # List Scripts directory
  agentctl godot resources --path res://Scripts

  # List only .gd files
  agentctl godot resources --pattern "*.gd"`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			payload := map[string]any{
				"action":        "resource_list",
				"host":          f.host,
				"port":          f.port,
				"timeout_ms":    f.timeoutMS,
				"resource_path": path,
				"pattern":       pattern,
				"max_results":   maxResults,
			}
			return runGodotSkill(cmd, payload, f.skipCache, f.dataOnly)
		},
	}

	addGodotFlags(cmd, &f)
	cmd.Flags().StringVar(&path, "path", "res://", "Directory path to list")
	cmd.Flags().StringVar(&pattern, "pattern", "", "Glob pattern to filter files (e.g., *.gd)")
	cmd.Flags().IntVar(&maxResults, "max-results", 100, "Maximum results to return")
	return cmd
}

func newGodotRunCommand() *cobra.Command {
	var f godotFlags

	cmd := &cobra.Command{
		Use:     "run",
		Short:   "Start the game",
		Long:    "Launch the game from the Godot Editor (equivalent to pressing F5).",
		Example: `  agentctl godot run`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			payload := map[string]any{
				"action":     "run_game",
				"host":       f.host,
				"port":       f.port,
				"timeout_ms": f.timeoutMS,
			}
			return runGodotSkill(cmd, payload, f.skipCache, f.dataOnly)
		},
	}

	addGodotFlags(cmd, &f)
	return cmd
}

func newGodotStopCommand() *cobra.Command {
	var f godotFlags

	cmd := &cobra.Command{
		Use:     "stop",
		Short:   "Stop the running game",
		Long:    "Stop the currently running game in the Godot Editor.",
		Example: `  agentctl godot stop`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			payload := map[string]any{
				"action":     "stop_game",
				"host":       f.host,
				"port":       f.port,
				"timeout_ms": f.timeoutMS,
			}
			return runGodotSkill(cmd, payload, f.skipCache, f.dataOnly)
		},
	}

	addGodotFlags(cmd, &f)
	return cmd
}

func newGodotErrorsCommand() *cobra.Command {
	var f godotFlags
	var limit int

	cmd := &cobra.Command{
		Use:   "errors",
		Short: "Get recent editor errors",
		Long:  "Retrieve recent errors from the Godot Editor's error buffer.",
		Example: `  # Get last 50 errors (default)
  agentctl godot errors

  # Get last 10 errors
  agentctl godot errors --limit 10`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			payload := map[string]any{
				"action":      "errors",
				"host":        f.host,
				"port":        f.port,
				"timeout_ms":  f.timeoutMS,
				"error_limit": limit,
			}
			return runGodotSkill(cmd, payload, f.skipCache, f.dataOnly)
		},
	}

	addGodotFlags(cmd, &f)
	cmd.Flags().IntVar(&limit, "limit", 50, "Maximum errors to return")
	return cmd
}

func runGodotSkill(cmd *cobra.Command, payload map[string]any, skipCache, dataOnly bool) error {
	cfg, err := config.Load(cmd.Context())
	if err != nil {
		return err
	}

	if _, err := findSkill(cfg, godotSkillName); err != nil {
		return writeGodotSkillMissingError(cmd)
	}

	input, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	runCmd := newRunCommand()
	runCmd.SetContext(cmd.Context())

	var buf bytes.Buffer
	if dataOnly {
		runCmd.SetOut(&buf)
	} else {
		runCmd.SetOut(cmd.OutOrStdout())
	}
	runCmd.SetErr(cmd.ErrOrStderr())

	args := []string{"--input", string(input)}
	if skipCache {
		args = append(args, "--cache", "off")
	}
	args = append(args, godotSkillName)
	runCmd.SetArgs(args)

	if err := runCmd.Execute(); err != nil {
		return err
	}

	if !dataOnly {
		return nil
	}

	// Parse and re-emit as data-only
	var env envelope.Envelope
	dec := json.NewDecoder(bytes.NewReader(buf.Bytes()))
	if err := dec.Decode(&env); err != nil {
		return fmt.Errorf("decode envelope: %w", err)
	}

	out := map[string]any{
		"status": env.Status,
		"data":   env.Data,
	}
	if env.Status == "error" {
		out["error"] = env.Error
	}

	enc := json.NewEncoder(cmd.OutOrStdout())
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	return enc.Encode(out)
}

func writeGodotSkillMissingError(cmd *cobra.Command) error {
	msg := fmt.Sprintf("%s skill not found", godotSkillName)
	hint := strings.Join([]string{
		"Build embedded skills with 'make skills-build' or install the skill via:",
		"  agentctl skills install --manifest skills/editor_godot/skill.yaml --binary dist/skills/editor_godot/bin",
	}, "\n")

	data := protocol.ErrorData{
		Hint: hint,
		Context: map[string]any{
			"skill":   godotSkillName,
			"command": cmd.CommandPath(),
		},
	}
	env := protocol.ErrorWithData(godotSkillName, protocol.ErrorCodeESkillDown, msg, data, protocol.WithSource("cli"))
	if err := protocol.Write(cmd.OutOrStdout(), env); err != nil {
		return fmt.Errorf("write skill missing error envelope: %w", err)
	}
	return fmt.Errorf("%s", msg)
}

func init() {
	rootCmd.AddCommand(newGodotCommand())
}
