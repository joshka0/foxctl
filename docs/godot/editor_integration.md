# Godot Editor Integration Skill — Design Spec

**Version:** 0.1.0  
**Status:** Draft  
**Last Updated:** 2025-11-26

---

## 1. Overview

### 1.1 Purpose

The `editor/godot` skill enables AI agents to interact with a running Godot Editor instance. It provides:

- **Scene inspection**: Query the current scene tree and node properties.
- **Scene manipulation**: Create nodes, set properties, attach scripts, connect signals.
- **Debugging**: Retrieve editor errors and run the game.

### 1.2 Architecture

```
┌─────────────────┐     JSON/HTTP      ┌──────────────────────┐
│  agentctl CLI   │◄──────────────────►│  Godot Editor        │
│  editor/godot   │   localhost:7777   │  GodotAIBridge       │
│  (exec skill)   │                    │  (EditorPlugin)      │
└─────────────────┘                    └──────────────────────┘
        │                                        │
        ▼                                        ▼
   JSON Envelopes                         EditorInterface
   CAS for large outputs                  EditorUndoRedoManager
```

### 1.3 Design Goals

- **Token efficiency**: Large scene trees go to CAS with summaries.
- **Hallucination resistance**: Clear errors with valid alternatives when paths/properties don't exist.
- **Safety**: All mutations use Undo/Redo; workspace validation prevents cross-project accidents.
- **Core Profile compliance**: JSON envelopes, exec runner with `network: "egress"`.

---

## 2. Constraints

### 2.1 Skill Type

- **Distribution**: `exec` (native Go binary).
- **Network**: `egress` (required to connect to localhost plugin).
- **Filesystem**: `workdir` (for optional backup operations).
- **Purity**: `false` (mutations affect editor state).

### 2.2 WASI Exclusion

Per Core Profile v1, WASI skills must have `network: "none"`. Since this skill requires network access to the Godot plugin, it **must** be an exec skill.

---

## 3. Plugin Protocol

### 3.1 Transport

HTTP POST to `http://127.0.0.1:7777/` with JSON body. Single request → single JSON response.

### 3.2 Request Schema

```json
{
  "workspace_root": "/path/to/agentctl/workspace",
  "action": "scene_tree",
  "params": { ... }
}
```

### 3.3 Response Schema

**Success:**
```json
{
  "status": "success",
  "data": { ... },
  "error": null
}
```

**Error:**
```json
{
  "status": "error",
  "data": null,
  "error": {
    "code": "ENODE_NOT_FOUND",
    "message": "Node '/root/Main/PlayerX' not found",
    "hint": "Valid children under '/root/Main': [\"Player\", \"Ground\"]"
  }
}
```

### 3.4 Actions

| Action | Description | Required Params |
|--------|-------------|-----------------|
| `ping` | Health check and project info | — |
| `scene_tree` | Get current scene hierarchy | `max_depth?`, `max_nodes?` |
| `node_inspect` | Get node details | `node_path` |
| `node_create` | Create a new node | `parent_path`, `type`, `name` |
| `node_set_prop` | Set a node property | `node_path`, `property`, `value` |
| `node_attach_script` | Attach script to node | `node_path`, `script_path` |
| `signal_connect` | Connect a signal | `source_path`, `signal_name`, `target_path`, `method_name` |
| `run_game` | Launch the game | — |
| `errors` | Get recent editor errors | `limit?` |

---

## 4. Error Codes

| Code | Meaning |
|------|---------|
| `EBRIDGE_UNAVAILABLE` | Cannot connect to Godot plugin |
| `EWORKSPACE_MISMATCH` | Workspace doesn't match Godot project |
| `EEDITOR_STATE` | No scene open or editor not ready |
| `ENODE_NOT_FOUND` | Node path doesn't exist |
| `EPROP_NOT_FOUND` | Property doesn't exist on node |
| `ETYPE_INVALID` | Invalid Godot class name |
| `ETYPE_CONVERSION` | Cannot convert value to target type |
| `ESCRIPT_NOT_FOUND` | Script file doesn't exist |
| `ESIGNAL_NOT_FOUND` | Signal doesn't exist on node |
| `EMETHOD_NOT_FOUND` | Method doesn't exist on target |

---

## 5. Workspace Validation

On every request, the plugin validates:

1. Extract `workspace_root` from request.
2. Compute `project_root = ProjectSettings.globalize_path("res://")`.
3. If paths don't match (allowing for subdirectory relationships), return `EWORKSPACE_MISMATCH`.

This prevents accidentally controlling the wrong Godot project.

---

## 6. CAS Integration

When response data exceeds `inline_output_kb` (default 32KB):

1. Store full response in CAS.
2. Generate summary:
   - `scene_tree`: node count, max depth, first N node paths.
   - `errors`: error count, first/last messages.
3. Return envelope with `data.summary`, `data.artifact`, `meta.cas_digest`.

---

## 7. Undo/Redo Safety

All mutating operations (`node_create`, `node_set_prop`, `node_attach_script`, `signal_connect`) use `EditorUndoRedoManager`:

```gdscript
undo_redo.create_action("AI: Create Node Enemy")
undo_redo.add_do_method(parent, "add_child", new_node)
undo_redo.add_do_method(new_node, "set_owner", root)
undo_redo.add_do_reference(new_node)
undo_redo.add_undo_method(parent, "remove_child", new_node)
undo_redo.commit_action()
```

This allows humans to Ctrl+Z any AI-initiated changes.

---

## 8. Type Conversion

The plugin converts string values to Godot types:

| Target Type | Accepted Formats |
|-------------|------------------|
| `Vector2` | `"Vector2(10, 20)"`, `"(10, 20)"` |
| `Vector3` | `"Vector3(1, 2, 3)"`, `"(1, 2, 3)"` |
| `Color` | `"Color(1, 0, 0)"`, `"#ff0000"`, `"red"` |
| `int` | `"42"`, `42` |
| `float` | `"3.14"`, `3.14` |
| `bool` | `"true"`, `"false"`, `true`, `false` |

---

## 9. Security Considerations

- **Localhost only**: Plugin binds to `127.0.0.1`, not `0.0.0.0`.
- **No remote access**: No authentication needed since local-only.
- **Workspace isolation**: Validated on every request.
- **No arbitrary code execution**: Only predefined actions supported.

---

## 10. Future Extensions

- `class_info`: Query Godot ClassDB for available methods/properties.
- `scene_save`: Save current scene to disk.
- `resource_load`: Load and inspect resources.
- `breakpoint_set`: Set debugger breakpoints.

---

## 11. References

- [Core Profile v1 Spec](../spec/core_profile_v1.md)
- [Godot EditorInterface Docs](https://docs.godotengine.org/en/stable/classes/class_editorinterface.html)
- [Godot EditorPlugin Docs](https://docs.godotengine.org/en/stable/classes/class_editorplugin.html)
