---
name: agentctl Mobile Android
description: Android Emulator automation via ADB - device management, app lifecycle, UI interaction, accessibility inspection, debugging, and system analysis.
---

# Android Emulator (ADB)

Control Android Emulators via `agentctl run mobile/android`.

## Usage

```bash
agentctl run mobile/android --input '{"operation": "<op>", ...}'
```

## Operations

| Op | Params | Description |
|----|--------|-------------|
| `list_devices` | - | List emulators |
| `device_info` | `serial?` | Device properties |
| `install` | `app` | Install APK |
| `launch` | `app`, `activity?` | Launch app |
| `terminate` | `app` | Stop app |
| `screenshot` | `output?` | Capture screen |
| `tap` | `x`, `y` | Tap coordinates |
| `swipe` | `x`, `y`, `x2`, `y2`, `duration?` | Swipe gesture |
| `type_text` | `text` | Input text |
| `press_key` | `keycode` | HOME/BACK/MENU/ENTER |
| `ui_tree` | - | UI hierarchy |
| `logs` | - | Logcat output |
| `logcat_filter` | `tag`, `level?` | Filtered logs (V/D/I/W/E/F) |
| `grant_permission` | `app`, `permission` | Grant permission |
| `dumpsys` | `service` | System info (activity/window/battery) |
| `pull_file` | `remote_path`, `local_path` | Download file |
| `push_file` | `local_path`, `remote_path` | Upload file |
| `open_url` | `url` | Open URL/deep link |

Full docs: `~/.agentctl/share/configs/skills/agentctl-mobile-android/Skill.md`
