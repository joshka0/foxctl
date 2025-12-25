---
name: agentctl Mobile
description: Unified mobile simulator automation for iOS and Android - list devices, install apps, take screenshots, interact with UI, and debug Expo apps.
---

# Mobile Automation

Cross-platform mobile control via `agentctl run mobile`.

## Usage

```bash
# List all devices (both platforms)
agentctl run mobile --input '{"operation": "list_devices"}'

# Platform-specific
agentctl run mobile --input '{"platform": "ios", "operation": "screenshot"}'
agentctl run mobile --input '{"platform": "android", "operation": "tap", "x": 200, "y": 400}'
```

## Common Operations

| Op | iOS | Android | Description |
|----|-----|---------|-------------|
| `list_devices` | Yes | Yes | List simulators/emulators |
| `device_info` | Yes | Yes | Device properties |
| `install` | Yes | Yes | Install app |
| `launch` | Yes | Yes | Launch by bundle/package |
| `terminate` | Yes | Yes | Stop app |
| `screenshot` | Yes | Yes | Capture screen |
| `tap` | Yes | Yes | Tap x,y |
| `swipe` | Yes | Yes | Swipe gesture |
| `type_text` | Yes | Yes | Input text |
| `ui_tree` | Yes | Yes | UI hierarchy |
| `logs` | Yes | Yes | Device logs |
| `open_url` | Yes | Yes | Open URL |

Platform-specific skills: `/agentctl-mobile-ios`, `/agentctl-mobile-android`

Full docs: `~/repos/personal/agentctl/configs/skills/agentctl-mobile/Skill.md`
