---
name: agentctl Mobile
description: Unified mobile simulator automation for iOS and Android - list devices, install apps, take screenshots, interact with UI, and debug Expo apps.
---

# Mobile Simulator Automation

Cross-platform mobile automation for iOS Simulator (via IDB) and Android Emulator (via ADB).

## Prerequisites

- **iOS**: `brew install idb-companion`
- **Android**: `brew install android-platform-tools` or Android SDK

## Quick Start

### List All Devices

Get devices from both platforms:

```bash
agentctl run mobile --input '{"operation": "list_devices"}'
```

### Platform-Specific Operations

For operations other than `list_devices`, specify the platform:

```bash
# iOS
agentctl run mobile --input '{"platform": "ios", "operation": "screenshot"}'

# Android
agentctl run mobile --input '{"platform": "android", "operation": "screenshot"}'
```

## Common Operations

### Device Management

```bash
# List all simulators/emulators
agentctl run mobile --input '{"operation": "list_devices"}'

# Get device info (iOS)
agentctl run mobile --input '{"platform": "ios", "operation": "device_info", "device": "<udid>"}'

# Get device info (Android)
agentctl run mobile --input '{"platform": "android", "operation": "device_info", "device": "emulator-5554"}'
```

### App Lifecycle

```bash
# Install app
agentctl run mobile --input '{"platform": "ios", "operation": "install", "app": "/path/to/app.app"}'
agentctl run mobile --input '{"platform": "android", "operation": "install", "app": "/path/to/app.apk"}'

# Launch app
agentctl run mobile --input '{"platform": "ios", "operation": "launch", "app": "com.example.myapp"}'
agentctl run mobile --input '{"platform": "android", "operation": "launch", "app": "com.example.myapp"}'

# Terminate app
agentctl run mobile --input '{"platform": "ios", "operation": "terminate", "app": "com.example.myapp"}'
```

### Screenshots

```bash
# Capture screenshot (stored in CAS)
agentctl run mobile --input '{"platform": "ios", "operation": "screenshot"}'
agentctl run mobile --input '{"platform": "android", "operation": "screenshot"}'

# Save to specific path
agentctl run mobile --input '{"platform": "ios", "operation": "screenshot", "output": "/tmp/screen.png"}'
```

### UI Interaction

```bash
# Tap at coordinates
agentctl run mobile --input '{"platform": "ios", "operation": "tap", "x": 200, "y": 400}'

# Swipe gesture
agentctl run mobile --input '{"platform": "android", "operation": "swipe", "x": 200, "y": 600, "x2": 200, "y2": 200}'

# Type text
agentctl run mobile --input '{"platform": "ios", "operation": "type_text", "text": "Hello World"}'
```

### UI Inspection

```bash
# Get UI element tree
agentctl run mobile --input '{"platform": "ios", "operation": "ui_tree"}'
agentctl run mobile --input '{"platform": "android", "operation": "ui_tree"}'
```

### Debugging

```bash
# Get device logs
agentctl run mobile --input '{"platform": "ios", "operation": "logs"}'
agentctl run mobile --input '{"platform": "android", "operation": "logs"}'
```

### Open URLs

```bash
# Open URL in browser/app
agentctl run mobile --input '{"platform": "ios", "operation": "open_url", "url": "https://example.com"}'
```

## Expo Development

For Expo-specific features like shake gesture and dev menu, use the iOS-specific skill:

```bash
# Trigger shake gesture (opens Expo dev menu)
agentctl run mobile/ios --input '{"operation": "shake"}'

# Open Expo deep link
agentctl run mobile/ios --input '{"operation": "expo_deep_link", "expo_url": "exp://192.168.1.100:8081"}'

# Reload Expo app
agentctl run mobile/ios --input '{"operation": "expo_reload"}'
```

## Available Operations

| Operation | iOS | Android | Description |
|-----------|-----|---------|-------------|
| `list_devices` | Yes | Yes | List available simulators/emulators |
| `device_info` | Yes | Yes | Get device properties |
| `install` | Yes | Yes | Install app |
| `launch` | Yes | Yes | Launch app by bundle ID/package |
| `terminate` | Yes | Yes | Stop running app |
| `screenshot` | Yes | Yes | Capture screen (stored in CAS) |
| `tap` | Yes | Yes | Tap at x,y coordinates |
| `swipe` | Yes | Yes | Swipe from x1,y1 to x2,y2 |
| `type_text` | Yes | Yes | Input text |
| `ui_tree` | Yes | Yes | Get accessibility tree |
| `logs` | Yes | Yes | Get device logs |
| `open_url` | Yes | Yes | Open URL |

## Platform-Specific Skills

For advanced platform-specific operations, use:

- **`/agentctl-mobile-ios`** - iOS Simulator with Expo features
- **`/agentctl-mobile-android`** - Android Emulator with ADB features
