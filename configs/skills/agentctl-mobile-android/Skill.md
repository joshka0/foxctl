---
name: agentctl Mobile Android
description: Android Emulator automation via ADB - device management, app lifecycle, UI interaction, accessibility inspection, debugging, and system analysis.
---

# Android Emulator Automation

Full Android Emulator control via ADB (Android Debug Bridge).

## Prerequisites

Install ADB:

```bash
# Via Homebrew
brew install android-platform-tools

# Or via Android SDK
# ADB is included in platform-tools
```

## Device Management

### List Emulators

```bash
agentctl run mobile/android --input '{"operation": "list_devices"}'
```

### Get Device Info

```bash
agentctl run mobile/android --input '{"operation": "device_info"}'
agentctl run mobile/android --input '{"operation": "device_info", "serial": "emulator-5554"}'
```

Returns device properties including model, Android version, SDK level, screen size, and CPU architecture.

## App Lifecycle

### Install APK

```bash
agentctl run mobile/android --input '{"operation": "install", "app": "/path/to/app.apk"}'
```

### Launch App

```bash
# Auto-detect main activity
agentctl run mobile/android --input '{"operation": "launch", "app": "com.example.myapp"}'

# Specify activity
agentctl run mobile/android --input '{
  "operation": "launch",
  "app": "com.example.myapp",
  "activity": ".MainActivity"
}'
```

### Terminate App

```bash
agentctl run mobile/android --input '{"operation": "terminate", "app": "com.example.myapp"}'
```

## Screenshots & Recording

### Take Screenshot

```bash
# Screenshot stored in CAS
agentctl run mobile/android --input '{"operation": "screenshot"}'

# Save to specific path
agentctl run mobile/android --input '{"operation": "screenshot", "output": "/tmp/screen.png"}'
```

### Video Recording

```bash
# Start recording (max 180 seconds)
agentctl run mobile/android --input '{"operation": "record_screen", "output": "/tmp/recording.mp4"}'

# Stop recording
agentctl run mobile/android --input '{"operation": "record_stop"}'
```

## UI Interaction

### Tap

```bash
agentctl run mobile/android --input '{"operation": "tap", "x": 540, "y": 960}'
```

### Swipe

```bash
agentctl run mobile/android --input '{
  "operation": "swipe",
  "x": 540, "y": 1500,
  "x2": 540, "y2": 500,
  "duration": 300
}'
```

### Type Text

```bash
agentctl run mobile/android --input '{"operation": "type_text", "text": "Hello World"}'
```

### Press Key

```bash
# Common keycodes
agentctl run mobile/android --input '{"operation": "press_key", "keycode": "HOME"}'
agentctl run mobile/android --input '{"operation": "press_key", "keycode": "BACK"}'
agentctl run mobile/android --input '{"operation": "press_key", "keycode": "MENU"}'
agentctl run mobile/android --input '{"operation": "press_key", "keycode": "ENTER"}'
agentctl run mobile/android --input '{"operation": "press_key", "keycode": "VOLUME_UP"}'
agentctl run mobile/android --input '{"operation": "press_key", "keycode": "VOLUME_DOWN"}'
```

## UI Inspection

### Get UI Hierarchy

```bash
agentctl run mobile/android --input '{"operation": "ui_tree"}'
```

Returns parsed UI elements with:
- `class`: View class (e.g., `android.widget.Button`)
- `text`: Displayed text
- `resource_id`: Resource identifier
- `content_desc`: Accessibility description
- `bounds`: Position `[x1,y1][x2,y2]`
- `clickable`, `enabled`, `focused`: States

## Debugging

### Get Logcat

```bash
# Get recent logs (last 500 entries)
agentctl run mobile/android --input '{"operation": "logs"}'
```

### Filter Logcat

```bash
# Filter by tag
agentctl run mobile/android --input '{"operation": "logcat_filter", "tag": "MyApp"}'

# Filter by tag and level
agentctl run mobile/android --input '{"operation": "logcat_filter", "tag": "MyApp", "level": "E"}'
```

Levels: `V` (Verbose), `D` (Debug), `I` (Info), `W` (Warn), `E` (Error), `F` (Fatal)

### Dumpsys

Get system service information:

```bash
# Activity manager
agentctl run mobile/android --input '{"operation": "dumpsys", "service": "activity"}'

# Window manager
agentctl run mobile/android --input '{"operation": "dumpsys", "service": "window"}'

# Battery
agentctl run mobile/android --input '{"operation": "dumpsys", "service": "battery"}'

# Memory info
agentctl run mobile/android --input '{"operation": "dumpsys", "service": "meminfo"}'

# Package info
agentctl run mobile/android --input '{"operation": "dumpsys", "service": "package"}'

# CPU info
agentctl run mobile/android --input '{"operation": "dumpsys", "service": "cpuinfo"}'
```

## Permissions

### Grant Runtime Permission

```bash
agentctl run mobile/android --input '{
  "operation": "grant_permission",
  "app": "com.example.myapp",
  "permission": "android.permission.CAMERA"
}'

# Short form (android.permission. prefix added automatically)
agentctl run mobile/android --input '{
  "operation": "grant_permission",
  "app": "com.example.myapp",
  "permission": "READ_EXTERNAL_STORAGE"
}'
```

## File Transfer

### Pull File from Device

```bash
agentctl run mobile/android --input '{
  "operation": "pull_file",
  "remote_path": "/sdcard/Download/file.txt",
  "local_path": "/tmp/file.txt"
}'
```

### Push File to Device

```bash
agentctl run mobile/android --input '{
  "operation": "push_file",
  "local_path": "/tmp/test.json",
  "remote_path": "/sdcard/Download/test.json"
}'
```

## Open URL

```bash
agentctl run mobile/android --input '{"operation": "open_url", "url": "https://example.com"}'
agentctl run mobile/android --input '{"operation": "open_url", "url": "myapp://deeplink/path"}'
```

## All Operations

| Operation | Description |
|-----------|-------------|
| `list_devices` | List Android emulators |
| `device_info` | Get device properties |
| `install` | Install APK |
| `launch` | Launch app by package |
| `terminate` | Stop app by package |
| `screenshot` | Capture screen |
| `tap` | Tap at coordinates |
| `swipe` | Swipe gesture |
| `type_text` | Input text |
| `press_key` | Press Android keycode |
| `ui_tree` | Get UI hierarchy |
| `logs` | Get logcat output |
| `logcat_filter` | Filtered logcat |
| `open_url` | Open URL/deep link |
| `grant_permission` | Grant runtime permission |
| `record_screen` | Start video recording |
| `record_stop` | Stop video recording |
| `dumpsys` | Get system service info |
| `pull_file` | Download file from device |
| `push_file` | Upload file to device |

## Common Keycodes

| Keycode | Description |
|---------|-------------|
| `HOME` | Home button |
| `BACK` | Back button |
| `MENU` | Menu button |
| `ENTER` | Enter key |
| `DEL` | Delete/backspace |
| `TAB` | Tab key |
| `ESCAPE` | Escape key |
| `VOLUME_UP` | Volume up |
| `VOLUME_DOWN` | Volume down |
| `POWER` | Power button |
| `CAMERA` | Camera button |

## Common Workflows

### Testing an App

1. List devices: `list_devices`
2. Install APK: `install`
3. Grant permissions: `grant_permission`
4. Launch app: `launch`
5. Take screenshots: `screenshot`
6. Inspect UI: `ui_tree`
7. Check logs: `logs` or `logcat_filter`

### Debugging a Crash

1. Filter error logs: `logcat_filter` with `level: "E"`
2. Get activity state: `dumpsys` with `service: "activity"`
3. Inspect UI: `ui_tree`
4. Pull crash files: `pull_file` from `/sdcard/`
