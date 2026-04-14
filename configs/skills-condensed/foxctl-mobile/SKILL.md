---
name: foxctl Mobile
description: Mobile simulator automation with foxctl - iOS Simulator and Android Emulator control. Use when asked to take screenshots, tap buttons, interact with mobile app, debug Expo, or test on device.
---

# Mobile Automation

Use this skill for iOS/Android simulator control, Expo development, and mobile testing.

**Trigger phrases**: "take a screenshot", "tap on", "swipe", "mobile app", "simulator", "emulator", "Expo", "test on device", "launch the app", "what's on screen"

Cross-platform mobile control via `foxctl run mobile`.

## Usage

```bash
# List all devices (both platforms)
foxctl run mobile --input '{"operation": "list_devices"}'

# Platform-specific
foxctl run mobile --input '{"platform": "ios", "operation": "screenshot"}'
foxctl run mobile --input '{"platform": "android", "operation": "tap", "x": 200, "y": 400}'
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

Platform-specific skills: `/foxctl-mobile-ios`, `/foxctl-mobile-android`

Full docs: `~/.foxctl/share/configs/skills/foxctl-mobile/Skill.md`
