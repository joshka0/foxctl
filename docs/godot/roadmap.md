# Godot Skill Roadmap

This document tracks planned and implemented features for the `editor/godot`
skill.

---

## Summary of Implemented Actions (31 total)

| Action                | CLI Command                             | Description               |
| --------------------- | --------------------------------------- | ------------------------- |
| `ping`                | `godot ping`                            | Check plugin connectivity |
| `scene_tree`          | `godot tree`                            | Get scene tree structure  |
| `node_inspect`        | `godot inspect <path>`                  | Inspect a node            |
| `node_create`         | `godot create <parent> <type> <name>`   | Create a node             |
| `node_delete`         | `godot delete <path>`                   | Delete a node             |
| `node_rename`         | `godot rename <path> <new-name>`        | Rename a node             |
| `node_reparent`       | `godot reparent <path> <new-parent>`    | Reparent a node           |
| `node_set_prop`       | `godot set <path> <prop> <value>`       | Set a property            |
| `node_attach_script`  | `godot attach-script <path> <script>`   | Attach a script           |
| `signal_connect`      | `godot connect-signal ...`              | Connect a signal          |
| `class_info`          | `godot class-info <class>`              | Get class info            |
| `ensure_node`         | `godot ensure-node <path> <type>`       | Idempotent node creation  |
| `scene_save`          | `godot save`                            | Save current scene        |
| `scene_list`          | `godot scene-list`                      | List scenes               |
| `scene_open`          | `godot scene-open <path>`               | Open a scene              |
| `scene_instance`      | `godot scene-instance <scene> <parent>` | Instance a scene          |
| `search_nodes`        | `godot search-nodes`                    | Search nodes              |
| `focus_node`          | `godot focus <path>`                    | Focus a node              |
| `selection_state`     | `godot selection`                       | Get selection             |
| `camera_save`         | `godot camera-save <name>`              | Save camera position      |
| `camera_restore`      | `godot camera-restore <name>`           | Restore camera position   |
| `camera_list`         | `godot camera-list`                     | List camera bookmarks     |
| `script_create`       | `godot script-create <path>`            | Create a script           |
| `resource_list`       | `godot resources`                       | List resources            |
| `search_resources`    | `godot search-resources --type`         | Search by type            |
| `resource_references` | `godot resource-references <path>`      | Find references           |
| `run_game`            | `godot run`                             | Run main scene            |
| `run_scene`           | `godot run-scene <path>`                | Run specific scene        |
| `stop_game`           | `godot stop`                            | Stop running game         |
| `errors`              | `godot errors`                          | Get editor errors         |

---

## Features by Category

### 1. Idempotent "ensure" operations ✅

- `ensure_node` with `--if-exists` policy
- `script_create` for safe script scaffolding

### 2. Scene & prefab workflows ✅

- `scene_list`, `scene_open`, `scene_instance`, `scene_save`

### 3. Script scaffolding ✅

- `script_create` with exports, methods, signals

### 4. Editor UX helpers ✅

- `focus_node`, `selection_state`
- `camera_save`, `camera_restore`, `camera_list`

### 5. Test & play workflows ✅

- `run_game`, `run_scene`, `stop_game`, `errors`

### 6. Introspection & search ✅

- `search_nodes`, `class_info`, `resource_list`
- `search_resources`, `resource_references`

### 7. Change previews ✅

- `--dry-run` flag on mutating commands

---

## Pending Features

- **`run_tests`** - Trigger Godot unit tests
- **3D camera restore** - Currently only 2D is supported
- **`change_summary`** - Standardized diff structure
- **Overcharge-specific helpers** - Game-specific automation
