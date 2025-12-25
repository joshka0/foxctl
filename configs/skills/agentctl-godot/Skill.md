---
name: agentctl Godot Editor
description: Control the Godot Editor from agentctl - inspect scene trees, create nodes, set properties, attach scripts, and connect signals.
---

# Godot Editor Integration

Interact with a running Godot Editor via the GodotAIBridge plugin.

## Prerequisites

1. Godot Editor must be running with your project open
2. GodotAIBridge plugin must be installed and enabled
3. Plugin listens on `localhost:7777` by default

## Actions

### Check Connection

```bash
agentctl run editor/godot --input '{"action": "ping"}'
```

### Inspect Scene Tree

Get the current scene structure:

```bash
agentctl run editor/godot --input '{
  "action": "scene_tree",
  "max_depth": 10,
  "max_nodes": 500
}'
```

### Inspect Node

Get details about a specific node:

```bash
agentctl run editor/godot --input '{
  "action": "node_inspect",
  "node_path": "/root/Main/Player"
}'
```

### Create Node

Add a new node to the scene:

```bash
agentctl run editor/godot --input '{
  "action": "node_create",
  "parent_path": "/root/Main",
  "node_type": "CharacterBody2D",
  "node_name": "Enemy"
}'
```

### Set Property

Modify a node's property:

```bash
agentctl run editor/godot --input '{
  "action": "node_set_prop",
  "node_path": "/root/Main/Player",
  "property": "position",
  "value": "Vector2(100, 200)"
}'
```

### Attach Script

Attach a GDScript to a node:

```bash
agentctl run editor/godot --input '{
  "action": "node_attach_script",
  "node_path": "/root/Main/Enemy",
  "script_path": "res://scripts/enemy.gd"
}'
```

### Connect Signal

Connect a signal between nodes:

```bash
agentctl run editor/godot --input '{
  "action": "signal_connect",
  "node_path": "/root/Main/Button",
  "signal_name": "pressed",
  "target_path": "/root/Main/Player",
  "method_name": "_on_button_pressed"
}'
```

### Run Game

Start the game from editor:

```bash
agentctl run editor/godot --input '{"action": "run_game"}'
```

### Get Errors

Retrieve editor errors:

```bash
agentctl run editor/godot --input '{
  "action": "errors",
  "error_limit": 50
}'
```

## Connection Options

Override default connection settings:

```bash
agentctl run editor/godot --input '{
  "action": "ping",
  "host": "192.168.1.100",
  "port": 7777,
  "timeout_ms": 5000
}'
```

## Common Workflows

### Scene Setup

1. Create node structure
2. Set properties (position, scale, etc.)
3. Attach scripts
4. Connect signals

### Debugging

1. Check connection with `ping`
2. Get scene tree to understand structure
3. Inspect specific nodes for property values
4. Check `errors` for issues
