---
name: agentctl Godot Editor
description: Control the Godot Editor from agentctl - inspect scene trees, create nodes, set properties, attach scripts, and connect signals.
---

# Godot Editor Integration

Interact with running Godot Editor via GodotAIBridge plugin (localhost:7777).

## Usage

```bash
agentctl run editor/godot --input '{"action": "<action>", ...}'
```

## Actions

| Action | Params | Description |
|--------|--------|-------------|
| `ping` | - | Check connection |
| `scene_tree` | `max_depth?`, `max_nodes?` | Get scene structure |
| `node_inspect` | `node_path` | Node details |
| `node_create` | `parent_path`, `node_type`, `node_name` | Create node |
| `node_set_prop` | `node_path`, `property`, `value` | Set property |
| `node_attach_script` | `node_path`, `script_path` | Attach GDScript |
| `signal_connect` | `node_path`, `signal_name`, `target_path`, `method_name` | Connect signal |
| `run_game` | - | Start game |
| `errors` | `error_limit?` | Get editor errors |

## Example

```bash
# Create node with script
agentctl run editor/godot --input '{"action": "node_create", "parent_path": "/root/Main", "node_type": "CharacterBody2D", "node_name": "Player"}'
agentctl run editor/godot --input '{"action": "node_attach_script", "node_path": "/root/Main/Player", "script_path": "res://scripts/player.gd"}'
```

Full docs: `~/.agentctl/share/configs/skills/agentctl-godot/Skill.md`
