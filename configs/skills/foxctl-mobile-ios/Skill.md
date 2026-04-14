---
name: foxctl Mobile iOS
description: iOS Simulator automation via Facebook IDB - device management, app lifecycle, UI interaction, accessibility inspection, debugging, and Expo development features.
---

# iOS Simulator Automation

Full iOS Simulator control via Facebook IDB (iOS Development Bridge).

## Prerequisites

Install IDB:

```bash
brew install idb-companion
```

## Device Management

### List Simulators

```bash
foxctl run mobile/ios --input '{"operation": "list_devices"}'
```

### Get Device Info

```bash
foxctl run mobile/ios --input '{"operation": "device_info"}'
foxctl run mobile/ios --input '{"operation": "device_info", "udid": "<device-udid>"}'
```

### Boot Simulator

```bash
foxctl run mobile/ios --input '{"operation": "boot", "udid": "<device-udid>"}'
```

### Focus Simulator Window

```bash
foxctl run mobile/ios --input '{"operation": "focus"}'
```

## App Lifecycle

### Install App

```bash
foxctl run mobile/ios --input '{"operation": "install", "app": "/path/to/MyApp.app"}'
foxctl run mobile/ios --input '{"operation": "install", "app": "/path/to/MyApp.ipa"}'
```

### Launch App

```bash
foxctl run mobile/ios --input '{"operation": "launch", "app": "com.example.myapp"}'
```

### Terminate App

```bash
foxctl run mobile/ios --input '{"operation": "terminate", "app": "com.example.myapp"}'
```

## Screenshots & Recording

### Take Screenshot

```bash
# Screenshot stored in CAS
foxctl run mobile/ios --input '{"operation": "screenshot"}'

# Save to specific path
foxctl run mobile/ios --input '{"operation": "screenshot", "output": "/tmp/screen.png"}'
```

### Video Recording

```bash
# Start recording
foxctl run mobile/ios --input '{"operation": "record_start", "output": "/tmp/recording.mp4"}'

# Stop recording
foxctl run mobile/ios --input '{"operation": "record_stop"}'
```

## UI Interaction

### Tap

```bash
foxctl run mobile/ios --input '{"operation": "tap", "x": 200, "y": 400}'
```

### Swipe

```bash
foxctl run mobile/ios --input '{"operation": "swipe", "x": 200, "y": 600, "x2": 200, "y2": 200}'
```

### Type Text

```bash
foxctl run mobile/ios --input '{"operation": "type_text", "text": "Hello World"}'
```

### Press Button

```bash
# Hardware buttons
foxctl run mobile/ios --input '{"operation": "button", "button": "HOME"}'
foxctl run mobile/ios --input '{"operation": "button", "button": "LOCK"}'
foxctl run mobile/ios --input '{"operation": "button", "button": "SIRI"}'
```

## UI Inspection

### Get Full UI Tree

```bash
foxctl run mobile/ios --input '{"operation": "ui_tree"}'
```

Returns accessibility elements with properties like label, type, bounds, and states.

### Describe Element at Point

```bash
foxctl run mobile/ios --input '{"operation": "describe_point", "x": 200, "y": 400}'
```

## Expo Development

### Shake Gesture

Opens Expo dev menu:

```bash
foxctl run mobile/ios --input '{"operation": "shake"}'
```

### Expo Deep Link

Open an Expo development URL:

```bash
foxctl run mobile/ios --input '{"operation": "expo_deep_link", "expo_url": "exp://192.168.1.100:8081"}'
```

### Reload Expo App

Trigger a reload via shake + 'r' key:

```bash
foxctl run mobile/ios --input '{"operation": "expo_reload"}'
```

## Debugging

### Get Logs

```bash
foxctl run mobile/ios --input '{"operation": "logs"}'
```

### Get Crash Logs

```bash
foxctl run mobile/ios --input '{"operation": "crash_logs"}'
```

## Permissions & Location

### Approve Permissions

```bash
foxctl run mobile/ios --input '{
  "operation": "approve_permissions",
  "app": "com.example.myapp",
  "permissions": ["photos", "camera", "contacts", "location"]
}'
```

### Set GPS Location

```bash
foxctl run mobile/ios --input '{
  "operation": "set_location",
  "lat": 37.7749,
  "long": -122.4194
}'
```

## Media & Keychain

### Add Media to Simulator

```bash
foxctl run mobile/ios --input '{"operation": "add_media", "media_path": "/path/to/photo.jpg"}'
```

### Clear Keychain

```bash
foxctl run mobile/ios --input '{"operation": "clear_keychain"}'
```

## Open URL

```bash
foxctl run mobile/ios --input '{"operation": "open_url", "url": "https://example.com"}'
foxctl run mobile/ios --input '{"operation": "open_url", "url": "myapp://deeplink"}'
```

## All Operations

| Operation | Description |
|-----------|-------------|
| `list_devices` | List iOS simulators |
| `device_info` | Get device properties |
| `boot` | Boot a simulator by UDID |
| `install` | Install .app or .ipa |
| `launch` | Launch app by bundle ID |
| `terminate` | Stop app by bundle ID |
| `screenshot` | Capture screen |
| `tap` | Tap at coordinates |
| `swipe` | Swipe gesture |
| `type_text` | Input text |
| `button` | Press hardware button |
| `ui_tree` | Get accessibility tree |
| `describe_point` | Describe element at x,y |
| `logs` | Get simulator logs |
| `open_url` | Open URL/deep link |
| `set_location` | Set GPS coordinates |
| `approve_permissions` | Grant permissions to app |
| `record_start` | Start video recording |
| `record_stop` | Stop video recording |
| `crash_logs` | Get crash reports |
| `add_media` | Add photos/videos |
| `clear_keychain` | Clear keychain |
| `focus` | Bring simulator to front |
| `shake` | Trigger shake gesture |
| `expo_deep_link` | Open exp:// URL |
| `expo_reload` | Reload Expo app |

## Common Workflows

### Testing an Expo App

1. List devices: `list_devices`
2. Boot simulator: `boot` (if needed)
3. Open Expo: `expo_deep_link` with your Metro URL
4. Take screenshots: `screenshot`
5. Inspect UI: `ui_tree`
6. Reload after changes: `expo_reload`

### Debugging a Crash

1. Get crash logs: `crash_logs`
2. Get runtime logs: `logs`
3. Inspect UI state: `ui_tree`
