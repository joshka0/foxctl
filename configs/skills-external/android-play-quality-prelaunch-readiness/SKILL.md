---
name: android-play-quality-prelaunch-readiness
description: "Review Android quality, crashes, ANRs, performance, device coverage, and staged rollout readiness for Play release."
---

# Android Quality & Pre-launch Readiness

Use this skill to decide whether the build is stable enough to expose to testers or production users.

## Core Rule

Passing upload validation is not enough. The app must survive the first real session on real Android devices.

## Pre-launch Report

For builds uploaded to Play testing/production tracks, review Google Play pre-launch reports where available.

Check:

- crashes
- ANRs
- startup failures
- permission dialog problems
- login dead ends
- accessibility warnings
- performance issues
- screenshots from crawled devices
- unsupported device issues
- blocked content due to app access problems

If the crawler cannot log in, provide review/test credentials and test manually.

## Critical Manual QA Matrix

At minimum, test:

| Area | Test |
| --- | --- |
| Fresh install | launch, permissions, onboarding, login |
| Upgrade | existing data survives update |
| Auth | login, logout, token expiry, social auth |
| Network | offline, slow network, backend errors |
| Permissions | denied, allowed, later revoked |
| Push | notification opt-in, tap behavior |
| Deep links | open from link, fallback path |
| Purchases | buy, restore, cancel, entitlement refresh |
| AI | safe prompts, blocked prompts, report flow |
| UGC | report/block/moderation path |
| Account | deletion, export, support contact |
| Android back | back button and predictive back if enabled |
| Rotation | orientation if supported |
| Large font | no clipped critical controls |
| Dark mode | contrast and system bars |
| Low-end device | startup and memory behavior |

## Android Target Behavior

When target SDK changes, run a focused regression.

Check:

- permissions prompts and restricted permissions
- foreground service behavior
- notification permission behavior
- photo/video picker behavior
- background location behavior
- package visibility / intents
- predictive back gesture if enabled
- edge-to-edge/system bar layout if applicable
- storage/media access
- exact alarms
- WebView/OAuth redirects

## Expo-Specific QA

For Expo apps:

- test the Play-delivered AAB-derived install, not only Expo Go or dev client
- verify native modules work in production mode
- verify EAS Update channel/branch is correct
- verify `runtimeVersion` prevents incompatible OTA updates
- verify splash screen and app icon are production assets
- verify Proguard/R8/minification behavior if enabled
- verify push notifications with production credentials
- verify Google/Firebase OAuth fingerprints from Play App Signing

## Crash / ANR Gate

Block production if:

- app crashes on launch on any common supported device class
- core flow has known crash
- auth crash prevents review
- purchase flow crash exists
- AI/reporting flow crash exists if AI is central
- ANR occurs during startup or common interaction
- native crash appears after minification/production build only

Warnings:

- crash in obscure optional flow
- third-party SDK crash with workaround
- low-memory issue on rare devices

## Rollout Monitoring Plan

Before production:

- define rollout percentage
- define monitoring window
- identify dashboards: Play vitals, crash reporting, backend errors, billing events, AI moderation/report metrics
- define stop/pause criteria
- define rollback or hotfix path
- assign owner for first 24–72 hours

Example stop criteria:

```text
Pause rollout if crash-free sessions drop below 99.5%, auth error rate doubles, purchase validation errors exceed 1%, or AI reports spike unexpectedly.
```

## Low-Quality / Minimum Functionality Risk

Google Play may reject or remove apps with poor functionality or disrespectful UX.

Risky:

- app is mostly a WebView with no meaningful mobile functionality
- app is just text/PDF/wallpaper without app-specific value
- app crashes or does not load
- app is empty without login and reviewers cannot access it
- AI app is just an API wrapper with misleading claims and no safety/reporting flow
- app is spammy, duplicated, or low-effort

## Blockers

| Blocker | Fix |
| --- | --- |
| Crash on launch | Fix before any wider track |
| Core login/paywall/AI flow untestable | Provide credentials and fix flow |
| Play-delivered build fails Google/Firebase auth | Register app signing SHA fingerprints |
| Pre-launch report shows critical crash | Fix and upload new build |
| Target SDK upgrade untested | Run regression matrix on current Android versions |
| No rollout owner or stop criteria | Define monitoring and pause plan |

## Output Format

### Quality verdict

`Ready for testing` / `Ready for staged rollout` / `Not ready` / `Unknown`

### Quality status

| Area | Status | Evidence |
| --- | --- | --- |
| Launch | pass | Pixel 8 / Samsung A series tested |
| Purchases | warn | restore not tested |
| Pre-launch | block | crash on Android 15 crawler |

### Required fixes

| Fix | Verification | Owner |
| --- | --- | --- |
| Register Play app-signing SHA with Firebase | Play-delivered install can sign in with Google | Engineering |
