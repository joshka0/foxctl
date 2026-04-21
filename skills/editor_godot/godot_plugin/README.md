# GodotAIBridge Plugin

This Godot 4 EditorPlugin enables the `editor/godot` foxctl skill to interact
with the Godot Editor.

## Installation

1. Copy this folder to your Godot project:
   ```
   cp -r godot_plugin /path/to/your/godot/project/addons/godot_ai_bridge
   ```

2. Open your project in Godot Editor.

3. Go to **Project > Project Settings > Plugins**.

4. Enable **GodotAIBridge**.

5. You should see in the Output panel:
   ```
   [GodotAIBridge] Listening on 127.0.0.1:7777
   ```

## Usage

Once the plugin is running, you can use foxctl to interact with the editor:

Examples assume the skill is installed. JSON goes through `--input`; use
`--input-file -` when piping raw JSON, or `foxctl skills run` for direct
parameter flags.

```bash
# Check connection
foxctl run editor/godot --input '{"action": "ping"}'

# Get scene tree
foxctl run editor/godot --input '{"action": "scene_tree"}'

# Inspect a node
foxctl run editor/godot --input '{"action": "node_inspect", "node_path": "/root/Main/Player"}'

# Create a node
foxctl run editor/godot --input '{"action": "node_create", "parent_path": "/root/Main", "node_type": "Node2D", "node_name": "Enemy"}'

# Set a property
foxctl run editor/godot --input '{"action": "node_set_prop", "node_path": "/root/Main/Player", "property": "position", "value": "Vector2(100, 200)"}'
```

## Configuration

The plugin listens on `127.0.0.1:7777` by default. To change the port, edit
`bridge.gd`:

```gdscript
const PORT: int = 7777  # Change this
```

## Supported Actions

| Action               | Description                        |
| -------------------- | ---------------------------------- |
| `ping`               | Health check, returns project info |
| `scene_tree`         | Get current scene hierarchy        |
| `node_inspect`       | Get detailed node information      |
| `node_create`        | Create a new node                  |
| `node_set_prop`      | Set a node property                |
| `node_attach_script` | Attach a script to a node          |
| `signal_connect`     | Connect a signal                   |
| `run_game`           | Start the game                     |
| `errors`             | Get recent editor errors           |

## Undo/Redo Support

All mutating operations (create, set property, attach script) are registered
with Godot's Undo/Redo system. You can use **Ctrl+Z** to undo any changes made
by the AI.

## Security

- The plugin only listens on `127.0.0.1` (localhost).
- No remote connections are accepted.
- Workspace validation ensures the foxctl workspace matches the Godot project.

## Troubleshooting

### "Cannot connect to GodotAIBridge"

1. Ensure Godot Editor is running with your project open.
2. Check that the plugin is enabled in Project Settings > Plugins.
3. Look for the "Listening on 127.0.0.1:7777" message in the Output panel.

### "Workspace mismatch"

Run foxctl from your Godot project directory, or use `--workspace` to specify
the correct path.

### "No scene currently open"

Open a scene in the Godot editor before running scene-related commands.
