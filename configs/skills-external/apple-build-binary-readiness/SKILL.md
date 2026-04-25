---
name: apple-build-binary-readiness
description: "Review Apple build, binary, signing, entitlement, SDK, permission, encryption, and upload readiness."
---

# Apple Build and Binary Readiness

Use this skill to verify that the submitted binary itself is production-ready.

## Core Rule

A submission cannot be rescued by good metadata if the binary crashes, uses the wrong SDK, lacks purpose strings, has mismatched entitlements, or prevents App Review from reaching the core experience.

## Progressive Flow

### Level 0 — Build verdict

Report one of:

- **Build ready** — no obvious binary blockers found.
- **Build blocked** — cannot upload or submit.
- **Build needs verification** — key evidence missing.
- **Build risky** — upload may work, but App Review risk remains.

### Level 1 — Binary status table

| Area | Status | Evidence | Next step |
| --- | --- | --- | --- |
| SDK/toolchain | Pass/Fail/Unknown | Xcode/SDK version | Upgrade or verify |
| Signing | Pass/Fail/Unknown | Profile/cert valid | Recreate assets |
| Entitlements | Pass/Fail/Unknown | Matches features | Remove or document |
| Permissions | Pass/Fail/Unknown | Purpose strings | Add clear strings |
| Build state | Pass/Fail/Unknown | Processed/VALID | Wait or fix |
| Encryption | Pass/Fail/Unknown | Exempt/declaration | Declare or rebuild |

## Required Checks

### 1. SDK and Xcode requirement

Verify current Apple minimum SDK requirements before upload.

As of April 28, 2026, new App Store Connect uploads must be built with the required current Apple SDK generation for the relevant platform.

Checklist:

- CI uses the required Xcode version or later.
- Archive uses the required platform SDK.
- Expo/EAS build image supports the required Xcode version.
- Native dependencies build cleanly under the required SDK.
- The app was tested on the latest OS and the minimum supported OS.

Do not confuse SDK requirement with deployment target. The app may still support older OS versions if the deployment target allows it.

### 2. Release configuration

Verify:

- Release build, not Debug.
- No development API base URL.
- No debug menus exposed to ordinary users.
- No test ads or placeholder banners.
- No simulator-only code paths.
- No console logs leaking secrets.
- No mock data unless it is an explicit demo mode.
- Feature flags are in the intended launch state.

### 3. Build number and version

Verify:

- `CFBundleShortVersionString` matches App Store version.
- `CFBundleVersion` is unique and higher than prior uploaded builds for that version/platform.
- Multiple targets/extensions share coherent versioning.
- Build number is not reused after a successful upload.

Use ASC skills when available:

- `asc-xcode-build`
- `asc-build-lifecycle`
- `asc-id-resolver`

### 4. Signing and provisioning

Verify:

- App Store distribution signing, not development/ad hoc.
- Provisioning profile matches bundle ID.
- All app extensions have matching profiles.
- Capabilities match App Store Connect and Developer Portal configuration.
- Automatic signing is deterministic enough for CI, or manual signing assets are documented.

Use `asc-signing-setup` for bundle IDs, certificates, capabilities, and profiles.

### 5. Entitlements

Entitlements should match real features and review notes.

Common review-sensitive entitlements:

- HealthKit
- HomeKit
- ClassKit
- FamilyControls / Screen Time
- VPN / Network Extension
- Push notifications
- iCloud / CloudKit
- Sign in with Apple
- App Groups
- Associated Domains
- Wallet / passes
- Bluetooth
- location background modes
- audio/background modes

Remove unused entitlements. Document non-obvious entitlement behavior in App Review Notes.

### 6. Permissions and purpose strings

Every protected API needs a clear purpose string.

Purpose strings should explain the user benefit, not merely restate the permission.

Bad:

```text
We need camera access.
```

Good:

```text
Camera access lets you scan receipts to add expenses automatically.
```

Check:

- Camera
- Microphone
- Photos
- Contacts
- Location When In Use / Always
- Bluetooth
- Local Network
- Calendars / Reminders
- Motion / Fitness
- Health
- Speech Recognition
- Face ID
- User Tracking / ATT

If the app can work without permission, provide an alternate path.

### 7. Encryption and export compliance

Verify:

- `ITSAppUsesNonExemptEncryption` is correct.
- Build processing state matches the export-compliance plan.
- If non-exempt encryption is used, required declarations/docs exist.
- If using only standard HTTPS/TLS and exempt cryptography, rebuild with the correct Info.plist value where appropriate.

Use `asc-submission-health` for exact commands.

### 8. Crash and launch health

Before submission:

- fresh install launch test
- upgrade from previous App Store version, if update
- cold start and warm start
- login and logout
- first-run onboarding
- offline behavior
- permissions denied path
- subscription/paywall path
- main AI or network flow
- app background/foreground
- memory pressure, if relevant

App Review often finds obvious crashes quickly. Do not submit with known first-run crashes.

### 9. Backend and external services

Verify during review window:

- production/review backend is live
- demo account exists
- AI provider keys and quotas are active
- external OAuth credentials work
- push/APNs production environment works where relevant
- CDN/assets are reachable
- subscriptions/IAP sandbox behavior is testable
- rate limits will not block reviewers

### 10. Expo / React Native specific checks

For Expo apps:

- EAS build image uses required Xcode/SDK.
- `app.json` / `app.config.ts` has correct bundle ID, version, build number.
- iOS permissions usage descriptions are present.
- associated domains, background modes, and entitlements are intentional.
- no Expo Go-only dependencies in production path.
- updates/runtime version policy is coherent if using EAS Update.
- native rebuild performed after config/plugin changes.

For React Native:

- release scheme tested on device.
- Hermes/JSC choice validated.
- native modules linked and permission strings included.
- crash reporting initialized safely.
- source maps/symbols uploaded to crash provider if used.

## Output Format

```md
## Build verdict
Ready / Blocked / Needs verification / Risky

## Binary checklist
| Area | Status | Evidence | Fix |
| --- | --- | --- | --- |

## Commands to run
Use asc skills where applicable. Provide read-only commands first.

## Manual device tests
- [ ] Fresh install
- [ ] Upgrade install
- [ ] Denied permissions
- [ ] Offline path
- [ ] Core paid/AI flow

## App Review notes to add
[only if entitlements, hardware, AI, or non-obvious behavior need explanation]
```
