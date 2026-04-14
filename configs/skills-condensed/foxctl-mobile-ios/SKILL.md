---
name: foxctl Mobile iOS
description: iOS Simulator automation via Facebook IDB - device management, app lifecycle, UI interaction, accessibility inspection, debugging, and Expo development features.
---

# iOS Simulator (IDB)

Control iOS Simulators via `foxctl run mobile/ios`. Requires: `brew install idb-companion`

## Usage

```bash
foxctl run mobile/ios --input '{"operation": "<op>", ...}'
```

## Operations

| Op | Params | Description |
|----|--------|-------------|
| `list_devices` | - | List simulators |
| `device_info` | `udid?` | Device properties |
| `boot` | `udid` | Boot simulator |
| `focus` | - | Bring to front |
| `install` | `app` | Install .app/.ipa |
| `launch` | `app` | Launch by bundle ID |
| `terminate` | `app` | Stop app |
| `screenshot` | `output?` | Capture screen |
| `tap` | `x`, `y` | Tap coordinates |
| `swipe` | `x`, `y`, `x2`, `y2` | Swipe gesture |
| `type_text` | `text` | Input text |
| `button` | `button` | HOME/LOCK/SIRI |
| `ui_tree` | - | Accessibility tree |
| `describe_point` | `x`, `y` | Element at point |
| `logs` | - | Simulator logs |
| `crash_logs` | - | Crash reports |
| `set_location` | `lat`, `long` | GPS coordinates |
| `approve_permissions` | `app`, `permissions[]` | Grant permissions |
| `shake` | - | Shake gesture (Expo dev menu) |
| `expo_deep_link` | `expo_url` | Open exp:// URL |
| `expo_reload` | - | Reload Expo app |
| `open_url` | `url` | Open URL/deep link |

Full docs: `~/.foxctl/share/configs/skills/foxctl-mobile-ios/Skill.md`
