# Mobile Simulator Skills

This document describes the mobile simulator automation skills for iOS and Android.

## Architecture

```
┌─────────────────────────────────────────────────────────────────────┐
│                              agentctl                                │
│  ┌──────────┐  ┌──────────┐  ┌──────────────┐  ┌────────────────┐   │
│  │  mobile  │  │   expo   │  │  mobile/ios  │  │ mobile/android │   │
│  │ (unified)│  │ (unified)│  │              │  │                │   │
│  └────┬─────┘  └────┬─────┘  └──────┬───────┘  └───────┬────────┘   │
│       │             │               │                  │             │
│       ▼             ▼               ▼                  ▼             │
│  ┌───────────────────────────────────────────────────────────────┐  │
│  │                 CLI Wrapper Layer (Go native)                  │  │
│  └───────────────────────────────────────────────────────────────┘  │
└─────────────────────────────────────────────────────────────────────┘
          │             │                 │                   │
          ▼             ▼                 ▼                   ▼
    ┌───────────┐ ┌───────────┐    ┌───────────┐       ┌───────────┐
    │ auto-pick │ │  eas-cli  │    │    IDB    │       │    ADB    │
    │ iOS/Android│ │   Metro   │   │(Facebook) │       │ (Android) │
    └───────────┘ └───────────┘    └───────────┘       └───────────┘
          │             │                 │                   │
          ▼             ▼                 ▼                   ▼
    ┌───────────┐ ┌───────────┐    ┌───────────┐       ┌───────────┐
    │ Both      │ │   Expo    │    │    iOS    │       │  Android  │
    │ Platforms │ │   Cloud   │    │ Simulator │       │  Emulator │
    └───────────┘ └───────────┘    └───────────┘       └───────────┘
```

## Prerequisites

### iOS (IDB)

```bash
brew install idb-companion
```

IDB (iOS Development Bridge) is Facebook's tool for iOS Simulator automation.

### Android (ADB)

```bash
# Via Homebrew
brew install android-platform-tools

# Or install Android SDK
# ADB is in platform-tools
```

### Expo (EAS CLI)

```bash
npm install -g eas-cli

# Authenticate with Expo
eas login
```

EAS CLI is required for build and update operations. Dev menu operations work without it.

## Skills Overview

| Skill | Description | Backend |
|-------|-------------|---------|
| `mobile` | Unified cross-platform interface | Auto-dispatches to iOS/Android |
| `expo` | Expo development operations | IDB + ADB + EAS CLI |
| `mobile/ios` | iOS Simulator automation | Facebook IDB |
| `mobile/android` | Android Emulator automation | Android ADB |

## Operations Matrix

### Core Operations (all skills)

| Operation | iOS | Android | Description |
|-----------|-----|---------|-------------|
| `list_devices` | ✅ | ✅ | List available simulators/emulators |
| `device_info` | ✅ | ✅ | Get device properties |
| `install` | ✅ | ✅ | Install app (.app/.ipa or .apk) |
| `launch` | ✅ | ✅ | Launch app by identifier |
| `terminate` | ✅ | ✅ | Stop running app |
| `screenshot` | ✅ | ✅ | Capture screen (stored in CAS) |
| `tap` | ✅ | ✅ | Tap at x,y coordinates |
| `swipe` | ✅ | ✅ | Swipe gesture |
| `type_text` | ✅ | ✅ | Input text |
| `ui_tree` | ✅ | ✅ | Get accessibility tree |
| `logs` | ✅ | ✅ | Get device logs |
| `open_url` | ✅ | ✅ | Open URL/deep link |

### iOS-Specific Operations

| Operation | Description |
|-----------|-------------|
| `boot` | Boot simulator by UDID |
| `button` | Press hardware button (HOME, LOCK, SIRI) |
| `describe_point` | Describe UI element at x,y |
| `set_location` | Set GPS coordinates |
| `approve_permissions` | Grant permissions to app |
| `record_start/stop` | Video recording |
| `crash_logs` | Get crash reports |
| `add_media` | Add photos/videos to simulator |
| `clear_keychain` | Clear keychain |
| `focus` | Bring simulator window to front |
| `shake` | Trigger shake gesture (Expo dev menu) |
| `expo_deep_link` | Open exp:// URL |
| `expo_reload` | Reload Expo app |

### Android-Specific Operations

| Operation | Description |
|-----------|-------------|
| `press_key` | Press Android keycode |
| `logcat_filter` | Filtered logcat by tag/level |
| `grant_permission` | Grant runtime permission |
| `record_screen/stop` | Video recording |
| `dumpsys` | Get system service info |
| `pull_file` | Download file from device |
| `push_file` | Upload file to device |

### Expo Skill Operations

| Operation | Description | Platform |
|-----------|-------------|----------|
| `shake` | Trigger shake gesture (opens dev menu) | iOS/Android |
| `reload` | Reload JS bundle | iOS/Android |
| `deep_link` | Open exp:// or custom URL | iOS/Android |
| `dev_menu` | Open Expo dev menu | iOS/Android |
| `toggle_inspector` | Toggle element inspector | iOS/Android |
| `toggle_performance` | Toggle performance monitor | iOS/Android |
| `toggle_remote_debug` | Toggle remote JS debugging | iOS/Android |
| `build` | Trigger EAS cloud build | N/A |
| `update` | Publish OTA update | N/A |
| `build_status` | Check EAS build status | N/A |
| `logs` | Get Metro bundler logs | N/A |

## Usage Examples

### Unified Skill

```bash
# List all devices (both platforms)
agentctl run mobile --input '{"operation": "list_devices"}'

# Platform-specific operation
agentctl run mobile --input '{"platform": "ios", "operation": "screenshot"}'
agentctl run mobile --input '{"platform": "android", "operation": "tap", "x": 200, "y": 400}'
```

### iOS Skill

```bash
# Device management
agentctl run mobile/ios --input '{"operation": "list_devices"}'
agentctl run mobile/ios --input '{"operation": "boot", "udid": "<device-udid>"}'

# App lifecycle
agentctl run mobile/ios --input '{"operation": "install", "app": "/path/to/app.app"}'
agentctl run mobile/ios --input '{"operation": "launch", "app": "com.example.myapp"}'

# UI interaction
agentctl run mobile/ios --input '{"operation": "tap", "x": 200, "y": 400}'
agentctl run mobile/ios --input '{"operation": "type_text", "text": "Hello"}'

# Expo development
agentctl run mobile/ios --input '{"operation": "shake"}'
agentctl run mobile/ios --input '{"operation": "expo_deep_link", "expo_url": "exp://192.168.1.100:8081"}'
```

### Android Skill

```bash
# Device management
agentctl run mobile/android --input '{"operation": "list_devices"}'
agentctl run mobile/android --input '{"operation": "device_info", "serial": "emulator-5554"}'

# App lifecycle
agentctl run mobile/android --input '{"operation": "install", "app": "/path/to/app.apk"}'
agentctl run mobile/android --input '{"operation": "launch", "app": "com.example.myapp"}'

# UI interaction
agentctl run mobile/android --input '{"operation": "tap", "x": 540, "y": 960}'
agentctl run mobile/android --input '{"operation": "press_key", "keycode": "BACK"}'

# Debugging
agentctl run mobile/android --input '{"operation": "logcat_filter", "tag": "MyApp", "level": "E"}'
agentctl run mobile/android --input '{"operation": "dumpsys", "service": "activity"}'
```

### Expo Skill

```bash
# Dev menu operations (works on both iOS and Android)
agentctl run expo --input '{"operation": "shake"}'
agentctl run expo --input '{"operation": "reload", "platform": "ios"}'
agentctl run expo --input '{"operation": "deep_link", "url": "exp://192.168.1.100:8081"}'

# Toggle dev tools
agentctl run expo --input '{"operation": "toggle_inspector"}'
agentctl run expo --input '{"operation": "toggle_performance"}'

# EAS cloud builds
agentctl run expo --input '{"operation": "build", "build_platform": "ios", "profile": "development"}'
agentctl run expo --input '{"operation": "build_status"}'

# OTA updates
agentctl run expo --input '{"operation": "update", "channel": "preview", "message": "Bug fixes"}'

# Metro logs
agentctl run expo --input '{"operation": "logs", "filter": "error", "count": 50}'
```

## Output Format

All skills output JSON envelopes with the operation result:

```json
{
  "version": 1,
  "status": "ok",
  "command": "mobile/ios",
  "data": {
    "operation": "list_devices",
    "devices": [...],
    "count": 3
  }
}
```

### Large Outputs (CAS Artifacts)

For large outputs (screenshots, UI trees, logs), the data is stored in CAS:

```json
{
  "data": {
    "operation": "screenshot",
    "screenshot": "sha256:abc123...",
    "path": "/tmp/ios_screenshot_123.png",
    "size_bytes": 145234,
    "success": true
  }
}
```

Retrieve with: `agentctl cas get sha256:abc123...`

## Claude Code Integration

### Available Skills

- `/agentctl-mobile` - Unified mobile automation
- `/agentctl-mobile-ios` - iOS Simulator automation
- `/agentctl-mobile-android` - Android Emulator automation
- Expo skill: `agentctl run expo --input '...'`

### Expo Development Workflow

1. List devices: `list_devices`
2. Boot simulator if needed: `boot` (iOS)
3. Open Expo app: `expo_deep_link`
4. Make changes in code
5. Reload: `expo_reload` or `shake` to open dev menu
6. Take screenshot: `screenshot`
7. Inspect UI: `ui_tree`

### Debugging Workflow

1. Get logs: `logs` or `logcat_filter`
2. Inspect UI state: `ui_tree`
3. Check crash logs: `crash_logs` (iOS) or filtered logcat (Android)
4. Take screenshot for visual verification

## Troubleshooting

### iOS Issues

1. **IDB not found**: Install with `brew install idb-companion`
2. **No simulators listed**: Open Xcode and create simulators
3. **Boot fails**: Simulator may already be booted or corrupted

### Android Issues

1. **ADB not found**: Install Android SDK or `brew install android-platform-tools`
2. **No emulators listed**: Start an emulator from Android Studio
3. **UI automator fails**: Ensure emulator has Google APIs

### Debug Mode

```bash
AGENTCTL_DEBUG=1 agentctl run mobile/ios --input '{"operation": "list_devices"}'
```

## Building from Source

```bash
# Build all skills including mobile
make skills-build

# Output in dist/skills/
ls dist/skills/mobile*
```
