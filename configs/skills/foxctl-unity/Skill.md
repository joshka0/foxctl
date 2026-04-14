---
name: foxctl Unity
description: Manage Unity projects - build/test/export, package dependencies, scene build settings, and Input System action maps.
---

# Unity Project Management

Four skills for comprehensive Unity project control without requiring a running editor.

## Skills Overview

| Skill | Purpose |
|-------|---------|
| `build/unity` | Build, test, export, and clean Unity projects via CLI |
| `unity/packages` | Manage UPM dependencies in `Packages/manifest.json` |
| `unity/scenes` | Manage scene build settings in `EditorBuildSettings.asset` |
| `unity/input` | Manage Input System action maps in `.inputactions` files |

## Prerequisites

1. Unity project directory with an `Assets/` folder
2. For `build/unity`: Unity Editor installed (auto-detected via Unity Hub on macOS)

## build/unity

Build, test, and export Unity projects using the Unity Editor CLI (`-batchmode`).

### List Build Targets

```bash
foxctl run build/unity --input '{"action": "list_targets"}'
```

### Build Project

```bash
foxctl run build/unity --input '{
  "action": "build",
  "build_target": "StandaloneOSX"
}'
```

### Run Tests

```bash
foxctl run build/unity --input '{
  "action": "test",
  "test_platform": "EditMode"
}'
```

### Export Player

```bash
foxctl run build/unity --input '{
  "action": "export",
  "build_target": "StandaloneWindows64",
  "output_path": "builds/game.exe"
}'
```

### Clean Artifacts

```bash
foxctl run build/unity --input '{"action": "clean"}'
```

## unity/packages

Manage UPM dependencies in `Packages/manifest.json`.

### List Packages

```bash
foxctl run unity/packages --input '{"operation": "list"}'
```

### Add Package

```bash
foxctl run unity/packages --input '{
  "operation": "add",
  "package_name": "com.unity.inputsystem",
  "version": "1.7.0"
}'
```

### Remove Package

```bash
foxctl run unity/packages --input '{
  "operation": "remove",
  "package_name": "com.unity.inputsystem"
}'
```

### Get Package Version

```bash
foxctl run unity/packages --input '{
  "operation": "get",
  "package_name": "com.unity.inputsystem"
}'
```

## unity/scenes

Manage scene build settings in `ProjectSettings/EditorBuildSettings.asset`.

### List Scenes

```bash
foxctl run unity/scenes --input '{"operation": "list"}'
```

### Add Scene

```bash
foxctl run unity/scenes --input '{
  "operation": "add",
  "scene_path": "Assets/Scenes/MainMenu.unity"
}'
```

### Enable/Disable Scene

```bash
foxctl run unity/scenes --input '{
  "operation": "disable",
  "scene_path": "Assets/Scenes/Debug.unity"
}'
```

### Reorder Scene

```bash
foxctl run unity/scenes --input '{
  "operation": "reorder",
  "scene_path": "Assets/Scenes/MainMenu.unity",
  "index": 0
}'
```

### Find Scene Files

```bash
foxctl run unity/scenes --input '{"operation": "find"}'
```

## unity/input

Manage Unity Input System `.inputactions` files.

### List Action Maps

```bash
foxctl run unity/input --input '{
  "operation": "list_maps",
  "input_file": "Assets/Input/PlayerControls.inputactions"
}'
```

### List Actions in a Map

```bash
foxctl run unity/input --input '{
  "operation": "list_actions",
  "input_file": "Assets/Input/PlayerControls.inputactions",
  "map_name": "Player"
}'
```

### Add Action Map

```bash
foxctl run unity/input --input '{
  "operation": "add_map",
  "input_file": "Assets/Input/PlayerControls.inputactions",
  "map_name": "UI"
}'
```

### Add Action

```bash
foxctl run unity/input --input '{
  "operation": "add_action",
  "input_file": "Assets/Input/PlayerControls.inputactions",
  "map_name": "Player",
  "action_name": "Move",
  "action_type": "Value"
}'
```

Action types: `Button`, `Value`, `PassThrough`

### Add Binding

```bash
foxctl run unity/input --input '{
  "operation": "add_binding",
  "input_file": "Assets/Input/PlayerControls.inputactions",
  "map_name": "Player",
  "action_name": "Move",
  "binding_path": "<Keyboard>/w"
}'
```

### Remove Action Map

```bash
foxctl run unity/input --input '{
  "operation": "remove_map",
  "input_file": "Assets/Input/PlayerControls.inputactions",
  "map_name": "UI"
}'
```

### Remove Action

Removes the action and all its bindings:

```bash
foxctl run unity/input --input '{
  "operation": "remove_action",
  "input_file": "Assets/Input/PlayerControls.inputactions",
  "map_name": "Player",
  "action_name": "Move"
}'
```

## Common Workflows

### New Project Setup

1. Add required packages (`unity/packages add`)
2. Find or create scenes (`unity/scenes find`, `unity/scenes add`)
3. Set up input actions (`unity/input add_map`, `add_action`, `add_binding`)
4. Build for target platform (`build/unity build`)

### CI/CD Pipeline

1. List and verify packages (`unity/packages list`)
2. Check scene build order (`unity/scenes list`)
3. Run tests (`build/unity test`)
4. Export player (`build/unity export`)

### Input System Configuration

1. Create action maps for each context (`unity/input add_map` — Player, UI, Vehicle)
2. Add actions per map (`unity/input add_action` — Move, Look, Fire)
3. Bind inputs to actions (`unity/input add_binding` — keyboard, gamepad)
