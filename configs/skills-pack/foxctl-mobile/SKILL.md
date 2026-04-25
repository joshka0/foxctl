---
name: foxctl-mobile
description: "Drive iOS Simulator and Android Emulator via foxctl for taps, launches, screenshots, UI inspection, and quick QA loops."
---

## What I do
- Drive iOS Simulator and Android Emulator for quick manual QA loops.

## When to use me
- You need to reproduce a UI bug quickly.
- You want screenshots or scripted navigation steps.

## iOS examples
```bash
foxctl run mobile/ios --input '{"operation":"list_devices"}'
foxctl run mobile/ios --input '{"operation":"launch","bundle_id":"com.example.app"}'
foxctl run mobile/ios --input '{"operation":"screenshot"}'
```

## Android examples
```bash
foxctl run mobile/android --input '{"operation":"devices"}'
foxctl run mobile/android --input '{"operation":"launch","package":"com.example.app"}'
foxctl run mobile/android --input '{"operation":"screenshot"}'
```
