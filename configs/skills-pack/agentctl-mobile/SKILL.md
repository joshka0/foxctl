---
name: agentctl-mobile
description: "Mobile simulator automation for iOS + Android via agentctl (tap, launch, screenshots, inspect UI)."
---

## What I do
- Drive iOS Simulator and Android Emulator for quick manual QA loops.

## When to use me
- You need to reproduce a UI bug quickly.
- You want screenshots or scripted navigation steps.

## iOS examples
```bash
agentctl run mobile/ios --input '{"operation":"list_devices"}'
agentctl run mobile/ios --input '{"operation":"launch","bundle_id":"com.example.app"}'
agentctl run mobile/ios --input '{"operation":"screenshot"}'
```

## Android examples
```bash
agentctl run mobile/android --input '{"operation":"devices"}'
agentctl run mobile/android --input '{"operation":"launch","package":"com.example.app"}'
agentctl run mobile/android --input '{"operation":"screenshot"}'
```
