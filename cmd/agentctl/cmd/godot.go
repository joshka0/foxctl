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
	dryRun    bool
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
		newGodotEnsureNodeCommand(),
		newGodotSaveCommand(),
		newGodotSceneListCommand(),
		newGodotSceneOpenCommand(),
		newGodotSceneInstanceCommand(),
		newGodotSearchNodesCommand(),
		newGodotFocusCommand(),
		newGodotSelectionCommand(),
		newGodotCameraSaveCommand(),
		newGodotCameraRestoreCommand(),
		newGodotCameraListCommand(),
		newGodotScriptCreateCommand(),
		newGodotResourcesCommand(),
		newGodotSearchResourcesCommand(),
		newGodotResourceReferencesCommand(),
		newGodotRunCommand(),
		newGodotRunSceneCommand(),
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

func addGodotMutatingFlags(cmd *cobra.Command, f *godotFlags) {
	addGodotFlags(cmd, f)
	cmd.Flags().BoolVar(&f.dryRun, "dry-run", false, "Preview changes without applying them")
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
				"dry_run":     f.dryRun,
			}
			return runGodotSkill(cmd, payload, f.skipCache, f.dataOnly)
		},
	}

	addGodotMutatingFlags(cmd, &f)
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

func newGodotEnsureNodeCommand() *cobra.Command {
	var f godotFlags
	var props []string
	var ifExists string

	cmd := &cobra.Command{
		Use:   "ensure-node <path> <type>",
		Short: "Idempotently ensure a node exists",
		Long: `Ensure a node exists at the given path with the given type.

If the node already exists and the type matches:
  - With --if-exists=update (default): update properties if --prop flags are provided
  - With --if-exists=ignore: return info without modifying
  - With --if-exists=error: fail with ENODE_EXISTS

If the node doesn't exist, create it with the specified type and properties.
All operations are registered with Godot's Undo/Redo system.`,
		Example: `  # Ensure a Node2D exists (create if missing)
  agentctl godot ensure-node /root/GameRoot/Player Node2D

  # Ensure with properties
  agentctl godot ensure-node /root/GameRoot/Player CharacterBody2D \
    --prop position="Vector2(100, 200)" \
    --prop visible=true

  # Fail if node already exists
  agentctl godot ensure-node /root/GameRoot/NewNode Sprite2D --if-exists error`,
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			// Parse --prop flags into a map
			propsMap := make(map[string]string)
			for _, p := range props {
				parts := strings.SplitN(p, "=", 2)
				if len(parts) == 2 {
					propsMap[parts[0]] = parts[1]
				} else {
					return fmt.Errorf("invalid --prop format %q, expected key=value", p)
				}
			}

			payload := map[string]any{
				"action":     "ensure_node",
				"host":       f.host,
				"port":       f.port,
				"timeout_ms": f.timeoutMS,
				"node_path":  args[0],
				"node_type":  args[1],
			}
			if len(propsMap) > 0 {
				payload["props"] = propsMap
			}
			if ifExists != "" {
				payload["if_exists"] = ifExists
			}
			return runGodotSkill(cmd, payload, f.skipCache, f.dataOnly)
		},
	}

	addGodotFlags(cmd, &f)
	cmd.Flags().StringArrayVar(&props, "prop", nil, "Property to set (format: key=value, repeatable)")
	cmd.Flags().StringVar(&ifExists, "if-exists", "update", "Behavior if node exists: update, ignore, or error")
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

func newGodotSceneListCommand() *cobra.Command {
	var f godotFlags
	var path string
	var maxResults int
	var recursive bool

	cmd := &cobra.Command{
		Use:   "scene-list",
		Short: "List scenes in the project",
		Long:  "List all .tscn scene files in the Godot project.",
		Example: `  # List all scenes
  agentctl godot scene-list

  # List scenes in a specific directory
  agentctl godot scene-list --path res://Scenes

  # Non-recursive listing
  agentctl godot scene-list --path res://Scenes --no-recursive`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			payload := map[string]any{
				"action":        "scene_list",
				"host":          f.host,
				"port":          f.port,
				"timeout_ms":    f.timeoutMS,
				"resource_path": path,
				"max_results":   maxResults,
				"recursive":     recursive,
			}
			return runGodotSkill(cmd, payload, f.skipCache, f.dataOnly)
		},
	}

	addGodotFlags(cmd, &f)
	cmd.Flags().StringVar(&path, "path", "res://", "Directory to search for scenes")
	cmd.Flags().IntVar(&maxResults, "max-results", 100, "Maximum scenes to return")
	cmd.Flags().BoolVar(&recursive, "recursive", true, "Search subdirectories recursively")
	return cmd
}

func newGodotSceneOpenCommand() *cobra.Command {
	var f godotFlags

	cmd := &cobra.Command{
		Use:   "scene-open <scene-path>",
		Short: "Open a scene in the editor",
		Long:  "Open the specified scene file in the Godot Editor.",
		Example: `  # Open main scene
  agentctl godot scene-open res://Scenes/Main.tscn

  # Open a specific scene
  agentctl godot scene-open res://Scenes/Maps/Level1.tscn`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			payload := map[string]any{
				"action":     "scene_open",
				"host":       f.host,
				"port":       f.port,
				"timeout_ms": f.timeoutMS,
				"scene_path": args[0],
			}
			return runGodotSkill(cmd, payload, f.skipCache, f.dataOnly)
		},
	}

	addGodotFlags(cmd, &f)
	return cmd
}

func newGodotSceneInstanceCommand() *cobra.Command {
	var f godotFlags
	var instanceName string

	cmd := &cobra.Command{
		Use:   "scene-instance <scene-path> <parent-path>",
		Short: "Instance a scene as a child node",
		Long: `Instance a PackedScene as a child of the specified parent node.

This is useful for adding prefabs/templates to your scene.
The operation is registered with Godot's Undo/Redo system.`,
		Example: `  # Instance an enemy prefab
  agentctl godot scene-instance res://Scenes/Entities/Enemy.tscn /root/GameRoot

  # Instance with a custom name
  agentctl godot scene-instance res://Scenes/Entities/Enemy.tscn /root/GameRoot --name Boss`,
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			payload := map[string]any{
				"action":      "scene_instance",
				"host":        f.host,
				"port":        f.port,
				"timeout_ms":  f.timeoutMS,
				"scene_path":  args[0],
				"parent_path": args[1],
			}
			if instanceName != "" {
				payload["instance_name"] = instanceName
			}
			return runGodotSkill(cmd, payload, f.skipCache, f.dataOnly)
		},
	}

	addGodotFlags(cmd, &f)
	cmd.Flags().StringVar(&instanceName, "name", "", "Name for the instanced node (default: scene root name)")
	return cmd
}

func newGodotSearchNodesCommand() *cobra.Command {
	var f godotFlags
	var name string
	var nodeType string
	var property string
	var value string
	var maxResults int

	cmd := &cobra.Command{
		Use:   "search-nodes",
		Short: "Search for nodes by name, type, or property",
		Long: `Search the current scene for nodes matching the given criteria.

All filters are optional and combined with AND logic.
Name supports * wildcard patterns.`,
		Example: `  # Find all nodes with "Player" in the name
  agentctl godot search-nodes --name "*Player*"

  # Find all Area2D nodes
  agentctl godot search-nodes --type Area2D

  # Find nodes with a specific property value
  agentctl godot search-nodes --property visible --value false

  # Combine filters
  agentctl godot search-nodes --type Sprite2D --property modulate --value "Color(1, 0, 0, 1)"`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			payload := map[string]any{
				"action":      "search_nodes",
				"host":        f.host,
				"port":        f.port,
				"timeout_ms":  f.timeoutMS,
				"search_name": name,
				"search_type": nodeType,
				"property":    property,
				"value":       value,
				"max_results": maxResults,
			}
			return runGodotSkill(cmd, payload, f.skipCache, f.dataOnly)
		},
	}

	addGodotFlags(cmd, &f)
	cmd.Flags().StringVar(&name, "name", "", "Node name pattern (supports * wildcard)")
	cmd.Flags().StringVar(&nodeType, "type", "", "Node type/class to filter by")
	cmd.Flags().StringVar(&property, "property", "", "Property name to check")
	cmd.Flags().StringVar(&value, "value", "", "Property value to match")
	cmd.Flags().IntVar(&maxResults, "max-results", 50, "Maximum results to return")
	return cmd
}

func newGodotFocusCommand() *cobra.Command {
	var f godotFlags
	var frame bool

	cmd := &cobra.Command{
		Use:   "focus <node-path>",
		Short: "Select and focus a node in the editor",
		Long:  "Select the specified node in the editor's scene tree, making it the active selection.",
		Example: `  # Focus on a specific node
  agentctl godot focus /root/GameRoot/Player

  # Focus without framing
  agentctl godot focus /root/GameRoot/Player --no-frame`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			payload := map[string]any{
				"action":     "focus_node",
				"host":       f.host,
				"port":       f.port,
				"timeout_ms": f.timeoutMS,
				"node_path":  args[0],
				"frame":      frame,
			}
			return runGodotSkill(cmd, payload, f.skipCache, f.dataOnly)
		},
	}

	addGodotFlags(cmd, &f)
	cmd.Flags().BoolVar(&frame, "frame", true, "Frame the node in the viewport")
	return cmd
}

func newGodotSelectionCommand() *cobra.Command {
	var f godotFlags

	cmd := &cobra.Command{
		Use:     "selection",
		Short:   "Get the current editor selection",
		Long:    "Report what nodes are currently selected in the Godot Editor's scene tree.",
		Example: `  agentctl godot selection`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			payload := map[string]any{
				"action":     "selection_state",
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

func newGodotCameraSaveCommand() *cobra.Command {
	var f godotFlags

	cmd := &cobra.Command{
		Use:   "camera-save <name>",
		Short: "Save the current editor camera position",
		Long:  "Save the current 2D/3D editor camera position as a named bookmark.",
		Example: `  # Save current camera position
  agentctl godot camera-save spawn_area

  # Save another position
  agentctl godot camera-save boss_arena`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			payload := map[string]any{
				"action":        "camera_save",
				"host":          f.host,
				"port":          f.port,
				"timeout_ms":    f.timeoutMS,
				"bookmark_name": args[0],
			}
			return runGodotSkill(cmd, payload, f.skipCache, f.dataOnly)
		},
	}

	addGodotFlags(cmd, &f)
	return cmd
}

func newGodotCameraRestoreCommand() *cobra.Command {
	var f godotFlags

	cmd := &cobra.Command{
		Use:   "camera-restore <name>",
		Short: "Restore a saved camera position",
		Long:  "Restore the editor camera to a previously saved bookmark position.",
		Example: `  # Restore a saved camera position
  agentctl godot camera-restore spawn_area`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			payload := map[string]any{
				"action":        "camera_restore",
				"host":          f.host,
				"port":          f.port,
				"timeout_ms":    f.timeoutMS,
				"bookmark_name": args[0],
			}
			return runGodotSkill(cmd, payload, f.skipCache, f.dataOnly)
		},
	}

	addGodotFlags(cmd, &f)
	return cmd
}

func newGodotCameraListCommand() *cobra.Command {
	var f godotFlags

	cmd := &cobra.Command{
		Use:     "camera-list",
		Short:   "List saved camera bookmarks",
		Long:    "List all saved camera position bookmarks.",
		Example: `  agentctl godot camera-list`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			payload := map[string]any{
				"action":     "camera_list",
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

func newGodotScriptCreateCommand() *cobra.Command {
	var f godotFlags
	var extendsClass string
	var exports []string
	var methods []string
	var signals []string
	var overwrite bool

	cmd := &cobra.Command{
		Use:   "script-create <path>",
		Short: "Create a new GDScript file",
		Long: `Create a new GDScript file with a safe template.

The script will include:
- extends declaration
- Optional exported variables
- Optional method stubs
- Optional signal declarations

The path should be relative to res:// (e.g., Scripts/Player.gd).`,
		Example: `  # Create a simple script
  agentctl godot script-create res://Scripts/Player.gd --extends CharacterBody2D

  # Create with exports and methods
  agentctl godot script-create res://Scripts/Enemy.gd \
    --extends CharacterBody2D \
    --export speed:float=100.0 \
    --export health:int=100 \
    --method _physics_process \
    --method take_damage

  # Create with signals
  agentctl godot script-create res://Scripts/GameManager.gd \
    --extends Node \
    --signal game_started \
    --signal game_over`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			// Parse exports into structured format
			var exportsList []any
			for _, e := range exports {
				parts := strings.SplitN(e, ":", 2)
				if len(parts) == 2 {
					typeParts := strings.SplitN(parts[1], "=", 2)
					export := map[string]string{
						"name": parts[0],
						"type": typeParts[0],
					}
					if len(typeParts) == 2 {
						export["default"] = typeParts[1]
					}
					exportsList = append(exportsList, export)
				} else {
					exportsList = append(exportsList, e)
				}
			}

			payload := map[string]any{
				"action":      "script_create",
				"host":        f.host,
				"port":        f.port,
				"timeout_ms":  f.timeoutMS,
				"script_path": args[0],
				"extends":     extendsClass,
				"overwrite":   overwrite,
			}
			if len(exportsList) > 0 {
				payload["exports"] = exportsList
			}
			if len(methods) > 0 {
				payload["methods"] = methods
			}
			if len(signals) > 0 {
				payload["signals"] = signals
			}
			return runGodotSkill(cmd, payload, f.skipCache, f.dataOnly)
		},
	}

	addGodotFlags(cmd, &f)
	cmd.Flags().StringVar(&extendsClass, "extends", "Node", "Base class to extend")
	cmd.Flags().StringArrayVar(&exports, "export", nil, "Exported variable (format: name:type=default)")
	cmd.Flags().StringArrayVar(&methods, "method", nil, "Method stub to create")
	cmd.Flags().StringArrayVar(&signals, "signal", nil, "Signal to declare")
	cmd.Flags().BoolVar(&overwrite, "overwrite", false, "Overwrite existing script")
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

func newGodotSearchResourcesCommand() *cobra.Command {
	var f godotFlags
	var resourceType string
	var path string
	var name string
	var maxResults int

	cmd := &cobra.Command{
		Use:   "search-resources",
		Short: "Search for resources by type",
		Long: `Search the project for resources of a specific type.

Supported type shortcuts:
  - scene, packedscene: .tscn/.scn files
  - script, gdscript: .gd files
  - texture, texture2d: .png/.jpg/.webp/.svg files
  - audio, audiostream: .mp3/.ogg/.wav files
  - shader: .gdshader/.shader files
  - material: .material/.tres files
  - resource, tres: .tres files`,
		Example: `  # Find all scenes
  agentctl godot search-resources --type scene

  # Find all scripts with "Player" in the name
  agentctl godot search-resources --type script --name "*Player*"

  # Find textures in a specific directory
  agentctl godot search-resources --type texture --path res://Assets/Sprites`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if resourceType == "" {
				return fmt.Errorf("--type is required")
			}
			payload := map[string]any{
				"action":        "search_resources",
				"host":          f.host,
				"port":          f.port,
				"timeout_ms":    f.timeoutMS,
				"resource_type": resourceType,
				"resource_path": path,
				"search_name":   name,
				"max_results":   maxResults,
			}
			return runGodotSkill(cmd, payload, f.skipCache, f.dataOnly)
		},
	}

	addGodotFlags(cmd, &f)
	cmd.Flags().StringVar(&resourceType, "type", "", "Resource type to search for (required)")
	cmd.Flags().StringVar(&path, "path", "res://", "Directory to search in")
	cmd.Flags().StringVar(&name, "name", "", "Name pattern to filter (supports wildcards)")
	cmd.Flags().IntVar(&maxResults, "max-results", 50, "Maximum results to return")
	return cmd
}

func newGodotResourceReferencesCommand() *cobra.Command {
	var f godotFlags
	var maxResults int

	cmd := &cobra.Command{
		Use:   "resource-references <resource-path>",
		Short: "Find scenes that reference a resource",
		Long: `Find all scenes and resources that reference the specified resource.

Useful for understanding dependencies before refactoring or deleting resources.`,
		Example: `  # Find what uses a script
  agentctl godot resource-references res://Scripts/Player.gd

  # Find what uses a texture
  agentctl godot resource-references res://Assets/player.png`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			payload := map[string]any{
				"action":        "resource_references",
				"host":          f.host,
				"port":          f.port,
				"timeout_ms":    f.timeoutMS,
				"resource_path": args[0],
				"max_results":   maxResults,
			}
			return runGodotSkill(cmd, payload, f.skipCache, f.dataOnly)
		},
	}

	addGodotFlags(cmd, &f)
	cmd.Flags().IntVar(&maxResults, "max-results", 50, "Maximum results to return")
	return cmd
}

func newGodotRunCommand() *cobra.Command {
	var f godotFlags

	cmd := &cobra.Command{
		Use:     "run",
		Short:   "Run the game",
		Long:    "Start the game from the main scene in the Godot Editor.",
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

func newGodotRunSceneCommand() *cobra.Command {
	var f godotFlags

	cmd := &cobra.Command{
		Use:   "run-scene <scene-path>",
		Short: "Run a specific scene",
		Long:  "Run a specific scene instead of the main scene. Useful for testing individual scenes.",
		Example: `  # Run a specific scene
  agentctl godot run-scene res://Scenes/TestLevel.tscn

  # Run the main menu
  agentctl godot run-scene res://Scenes/MainMenu.tscn`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			payload := map[string]any{
				"action":     "run_scene",
				"host":       f.host,
				"port":       f.port,
				"timeout_ms": f.timeoutMS,
				"scene_path": args[0],
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
