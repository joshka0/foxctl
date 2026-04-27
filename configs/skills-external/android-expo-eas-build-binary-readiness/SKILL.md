---
name: android-expo-eas-build-binary-readiness
description: "Check Expo/EAS Android build, package, signing, SDK, runtimeVersion, and upload readiness for Google Play."
---

# Android Expo/EAS Build & Binary Readiness

Use this skill to verify that an Expo / React Native Android binary is valid for Google Play release.

## Core Rule

For Play Store production and testing tracks, build an Android App Bundle (`.aab`) unless there is a very specific reason to build an APK. APK builds are for direct installation, local QA, emulators, or internal distribution outside Play.

## Required Files and Settings

Check:

- `app.json` or `app.config.*`
- `eas.json`
- `package.json`
- generated native `android/` project if present
- build logs
- EAS project credentials
- Play Console App integrity / signing page

## Expo Config Checklist

| Setting | Requirement |
| --- | --- |
| `android.package` | Stable, unique application ID in reverse-DNS style |
| `version` | User-facing version, intentionally set for release |
| `android.versionCode` or remote versioning | Positive integer, unique for each Play upload |
| `runtimeVersion` | Correct for EAS Update compatibility if OTA updates are used |
| `android.permissions` | Only permissions required by core functionality |
| `android.adaptiveIcon` | Production-ready adaptive icon assets |
| `android.googleServicesFile` | Present only if Firebase/FCM/Google services require it |
| `android.userInterfaceStyle` | Matches app design expectations when dark mode is supported |

Never change `android.package` after release unless you intend to publish a new app.

## Versioning Rules

Google Play requires each uploaded build to have a unique, increasing `versionCode`.

Recommended EAS setup:

```json
{
  "cli": {
    "appVersionSource": "remote"
  },
  "build": {
    "production": {
      "autoIncrement": true
    }
  }
}
```

If using remote versioning:

- `android.versionCode` in app config may be ignored during builds.
- EAS remote build version is the source of truth.
- Confirm the actual build artifact version before upload.

If using local versioning:

- increment `android.versionCode` manually for every Play upload.
- never reuse a versionCode from a failed or previous submission.

## Target SDK Gate

Before every release, verify current Google Play target API requirements.

As a working rule for this skill, new apps and updates should target the current Play-required Android API level or higher. If unsure, stop and verify the official Google Play target API page.

In Expo projects:

- target SDK is usually determined by Expo SDK / React Native / Android Gradle setup.
- verify the generated Gradle configuration or build output when policy compliance matters.
- upgrading Expo SDK may be required to satisfy new Play target requirements.

## Build Profile Checklist

Good production profile:

```json
{
  "cli": {
    "appVersionSource": "remote"
  },
  "build": {
    "production": {
      "android": {
        "buildType": "app-bundle"
      },
      "autoIncrement": true
    }
  },
  "submit": {
    "production": {
      "android": {
        "track": "internal"
      }
    }
  }
}
```

Use an internal track first unless this is an already mature release flow.

Commands:

```bash
eas build --platform android --profile production
```

Then submit only after readiness checks:

```bash
eas submit --platform android --profile production
```

## Signing and Credentials

For Google Play:

- App bundle must be signed with an upload key.
- Play App Signing should be configured for Play distribution.
- EAS Build can manage Android keystores or use provided credentials.
- Preserve upload key access and recovery procedure.
- Register the correct certificate fingerprints with APIs that require SHA-1/SHA-256.

Important certificate distinction:

| Certificate | Used for |
| --- | --- |
| Debug certificate | local/dev builds only |
| Upload certificate | signing the AAB uploaded to Play |
| App signing certificate | APKs delivered to users by Google Play |

If using Google Sign-In, Firebase Auth, Maps, OAuth, app links, or other API-gated services, verify whether they require the app signing certificate, upload certificate, or both.

## EAS Submit Prerequisites

For Android EAS Submit:

- Google Play Developer account exists.
- App exists in Play Console.
- `android.package` matches Play Console application ID.
- Google Service Account exists.
- Google Play Android Developer API is enabled.
- Service account is invited to Play Console with appropriate permissions.
- Service account key is uploaded to EAS or configured securely for CI.
- First-time Play submission may require a manual upload before API-based submissions work.

Do not commit service account JSON keys to the repository.

## OTA / EAS Update Gate

If the app uses `expo-updates` / EAS Update:

- `runtimeVersion` must prevent incompatible OTA bundles from reaching incompatible native binaries.
- production channel/branch is correct.
- update rollbacks are understood.
- critical native changes are not shipped as OTA-only updates.
- release notes and Play listing match the native binary behavior, not just an OTA branch.

Risky:

```json
{
  "runtimeVersion": "1.0.0"
}
```

Better when managed carefully:

```json
{
  "runtimeVersion": {
    "policy": "appVersion"
  }
}
```

Choose the policy that matches the project’s update strategy.

## Common Blockers

| Blocker | Fix |
| --- | --- |
| APK built for Play production | Build AAB with production profile |
| `android.package` missing | Add stable unique package name to app config |
| Duplicate `versionCode` | Increment or enable EAS remote auto-increment |
| Target SDK below Play requirement | Upgrade Expo SDK / native config and rebuild |
| Wrong signing key | Verify upload key and Play App Signing setup |
| Google/Firebase API failures in Play build | Register Play app-signing SHA fingerprints |
| Service account upload fails | Verify API enabled and Play Console permissions |
| Native folders stale in prebuild workflow | Regenerate or align native config intentionally |
| Runtime version unsafe for OTA | Set correct runtimeVersion policy before release |

## Verification Commands

Use project-appropriate commands:

```bash
eas build:configure
npx expo config --type public
eas credentials --platform android
eas build:list --platform android
```

Build:

```bash
eas build --platform android --profile production
```

Submit to non-production track first when appropriate:

```bash
eas submit --platform android --profile production
```

If inspecting native output locally:

```bash
./gradlew :app:bundleRelease
```

## Output Format

### Binary verdict

`Ready` / `Not ready` / `Unknown`

### Binary status

| Area | Status | Evidence |
| --- | --- | --- |
| Package ID | pass | `com.company.app` |
| Artifact | pass | AAB built by EAS production profile |
| versionCode | warn | remote auto-increment enabled; artifact not yet verified |
| Target SDK | unknown | Need build output or generated Gradle |

### Required fixes

| Fix | File / Console Area | Verification |
| --- | --- | --- |
| Enable remote auto-increment | `eas.json` | New EAS build shows incremented versionCode |
