# Godot Editor Skill - Modular Refactor Plan

## Overview

Refactor the Godot editor skill (`editor/godot`) from a monolithic structure into a modular, category-based architecture. This makes the codebase maintainable and allows incremental implementation of 33 new actions.

## Current State

### Go Skill (Complete)
- `skills/editor_godot/main.go` - All 33 new actions implemented
- `skills/editor_godot/skill.yaml` - Updated to v0.2.0 with all parameters
- Build and lint pass

### GDScript Plugin (Needs Work)
- `skills/editor_godot/godot_plugin/bridge.gd` - Monolithic 1400+ line file
- Only handles ~30 original actions
- Missing handlers for all 33 new actions

## Target Architecture

```
skills/editor_godot/
├── main.go                    # Go skill (unchanged)
├── skill.yaml                 # Manifest (unchanged)
└── godot_plugin/
    ├── plugin.cfg             # Plugin manifest
    ├── bridge.gd              # Thin router - dispatches to handlers
    ├── handlers/
    │   ├── core.gd            # ping, errors, scene_tree, node_*, signal_connect, run_game
    │   ├── scripts.gd         # script_read, script_edit, script_create
    │   ├── groups.gd          # group_add, group_remove, group_list
    │   ├── console.gd         # console_output
    │   ├── settings.gd        # project_setting_get, project_setting_set
    │   ├── build.gd           # build (export)
    │   ├── animation.gd       # animation_list, animation_play, animation_stop
    │   ├── audio.gd           # audio_play, audio_stop
    │   ├── input.gd           # input_action_list, input_action_add, input_action_remove
    │   ├── autoload.gd        # autoload_list, autoload_add, autoload_remove
    │   ├── plugins.gd         # plugin_list, plugin_enable, plugin_disable
    │   ├── resources.gd       # resource_list, resource_get, scene_list, scene_open, camera_*
    │   ├── theme.gd           # theme_get, theme_set
    │   ├── shader.gd          # shader_create, shader_edit
    │   ├── tilemap.gd         # tilemap_get_cell, tilemap_set_cell
    │   ├── physics.gd         # physics_layer_get, physics_layer_set
    │   └── debug.gd           # debug_draw_enable, debug_draw_disable
    └── README.md              # Plugin documentation
```

## Implementation Phases

### Phase 1: Foundation (Refactor Existing)

1. **Create handler directory structure**
   - Create `handlers/` directory
   - Create base handler pattern

2. **Extract `handlers/core.gd`**
   - Move: `ping`, `errors`, `scene_tree`, `node_inspect`, `node_create`, `node_set_prop`, `node_attach_script`, `signal_connect`, `run_game`
   - These are the most-used actions

3. **Extract `handlers/resources.gd`**
   - Move: `resource_list`, `resource_get`, `scene_list`, `scene_open`, `camera_move`, `camera_set`
   - Existing functionality

4. **Extract `handlers/scripts.gd`**
   - Move: `script_create` (existing)
   - Add: `script_read`, `script_edit` (NEW)

5. **Refactor `bridge.gd` as router**
   - Load all handler modules
   - Dispatch actions to appropriate handler
   - Keep TCP server and JSON parsing logic

### Phase 2: HIGH Priority Actions

6. **Implement `handlers/groups.gd`** (NEW)
   - `group_add` - Add node to group
   - `group_remove` - Remove node from group
   - `group_list` - List all groups or nodes in group

7. **Implement `handlers/console.gd`** (NEW)
   - `console_output` - Get recent console/debug output

8. **Implement `handlers/settings.gd`** (NEW)
   - `project_setting_get` - Read project setting
   - `project_setting_set` - Write project setting

9. **Implement `handlers/build.gd`** (NEW)
   - `build` - Export game using EditorExportPlugin

### Phase 3: MEDIUM Priority Actions

10. **Implement `handlers/animation.gd`** (NEW)
    - `animation_list` - List animations on AnimationPlayer
    - `animation_play` - Play animation with options
    - `animation_stop` - Stop animation

11. **Implement `handlers/audio.gd`** (NEW)
    - `audio_play` - Play audio with volume/pitch/bus
    - `audio_stop` - Stop audio playback

12. **Implement `handlers/input.gd`** (NEW)
    - `input_action_list` - List input actions
    - `input_action_add` - Add input action mapping
    - `input_action_remove` - Remove input action

13. **Implement `handlers/autoload.gd`** (NEW)
    - `autoload_list` - List autoload singletons
    - `autoload_add` - Register autoload
    - `autoload_remove` - Unregister autoload

14. **Implement `handlers/plugins.gd`** (NEW)
    - `plugin_list` - List editor plugins
    - `plugin_enable` - Enable plugin
    - `plugin_disable` - Disable plugin

### Phase 4: LOW Priority Actions

15. **Implement `handlers/theme.gd`** (NEW)
    - `theme_get` - Get theme property
    - `theme_set` - Set theme property

16. **Implement `handlers/shader.gd`** (NEW)
    - `shader_create` - Create shader file
    - `shader_edit` - Edit shader code

17. **Implement `handlers/tilemap.gd`** (NEW)
    - `tilemap_get_cell` - Get tile at position
    - `tilemap_set_cell` - Set tile at position

18. **Implement `handlers/physics.gd`** (NEW)
    - `physics_layer_get` - Get physics layer info
    - `physics_layer_set` - Configure physics layer

19. **Implement `handlers/debug.gd`** (NEW)
    - `debug_draw_enable` - Enable debug visualization
    - `debug_draw_disable` - Disable debug visualization

## Handler Pattern

Each handler follows this pattern:

```gdscript
# handlers/example.gd
extends RefCounted
class_name ExampleHandler

var editor_interface: EditorInterface

func _init(ei: EditorInterface) -> void:
    editor_interface = ei

func handle(action: String, data: Dictionary) -> Dictionary:
    match action:
        "example_action":
            return _handle_example_action(data)
        _:
            return {"error": "Unknown action: " + action}

func _handle_example_action(data: Dictionary) -> Dictionary:
    # Implementation
    return {"status": "ok", "data": {...}}
```

## Router Pattern

```gdscript
# bridge.gd (simplified)
var handlers := {}

func _ready() -> void:
    var ei = EditorInterface.get_singleton()
    handlers["core"] = CoreHandler.new(ei)
    handlers["scripts"] = ScriptsHandler.new(ei)
    # ... etc

func _handle_request(action: String, data: Dictionary) -> Dictionary:
    var handler = _get_handler_for_action(action)
    if handler:
        return handler.handle(action, data)
    return {"error": "Unknown action: " + action}

func _get_handler_for_action(action: String) -> RefCounted:
    match action:
        "ping", "errors", "scene_tree", "node_inspect", "node_create", \
        "node_set_prop", "node_attach_script", "signal_connect", "run_game":
            return handlers["core"]
        "script_read", "script_edit", "script_create":
            return handlers["scripts"]
        # ... etc
```

## Testing Strategy

1. **After each handler extraction**: Test that existing functionality still works
2. **After each new handler**: Test the new actions via `agentctl run editor/godot`
3. **Integration test**: Run through all workflows in skill.yaml

## Success Criteria

- [ ] All 33 new actions working in GDScript plugin
- [ ] bridge.gd reduced from 1400+ lines to ~200 lines (router only)
- [ ] Each handler is self-contained and testable
- [ ] No regressions in existing functionality
- [ ] Documentation updated

## Dependencies

- Godot 4.x (EditorInterface API)
- GodotAIBridge plugin framework
- agentctl v0.2.0+ (for new skill.yaml)

## Risks

1. **Breaking existing users**: Mitigate by testing thoroughly after each extraction
2. **GDScript module loading**: May need to use `preload()` or `load()` patterns
3. **EditorInterface availability**: Some APIs only work in editor context

## Timeline

Work in progress - no time estimates per project guidelines.
