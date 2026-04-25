---
name: android-play-console-eas-automation-bridge
description: "Map Android release blockers into concrete EAS, Play Console, fastlane, and Google Play API actions."
---

# Android Play Console / EAS Automation Bridge

This skill converts readiness findings into safe release automation.

It does not decide whether the app is ready. The checklist skills decide readiness. This bridge only executes or recommends operations after the readiness state is known.

## Automation Safety Rules

1. Read-only inspection first.
2. Build before submit.
3. Submit to internal/closed track before production when state is uncertain.
4. Never production-submit or promote without explicit user approval.
5. Never use automation to bypass unresolved policy blockers.
6. Never commit service account JSON keys, keystores, or secrets.
7. Prefer staged rollout for non-trivial production releases.
8. Record the build ID, versionName, versionCode, track, rollout percentage, and changelog.

## Tooling Map

| Need | Preferred tool |
| --- | --- |
| Expo Android build | `eas build --platform android` |
| Expo Android submit | `eas submit --platform android` |
| EAS credentials | `eas credentials --platform android` |
| EAS build history | `eas build:list --platform android` |
| Expo config inspection | `npx expo config --type public` |
| Local native build | Gradle / Android Studio only when needed |
| Play listing metadata automation | Google Play Developer API / fastlane supply if configured |
| Tracks and rollout automation | Google Play Developer API / fastlane supply / Play Console |
| Policy forms | Usually Play Console manual workflow unless reliable internal automation exists |

## Read-Only First Commands

Use these before mutation:

```bash
npx expo config --type public
cat eas.json
cat package.json
eas build:list --platform android --limit 10
eas credentials --platform android
```

If Play API tooling exists:

```bash
fastlane supply init
fastlane supply --validate_only
```

Use project conventions over generic commands when they exist.

## Build Commands

Production AAB:

```bash
eas build --platform android --profile production
```

Preview APK for device QA:

```bash
eas build --platform android --profile preview
```

Build and submit only when readiness is high:

```bash
eas build --platform android --profile production --auto-submit
```

Avoid `--auto-submit` when app content/policy readiness is not yet verified.

## Submit Profile Pattern

Recommended `eas.json` structure:

```json
{
  "cli": {
    "appVersionSource": "remote"
  },
  "build": {
    "production": {
      "autoIncrement": true,
      "android": {
        "buildType": "app-bundle"
      }
    }
  },
  "submit": {
    "internal": {
      "android": {
        "track": "internal"
      }
    },
    "closed": {
      "android": {
        "track": "alpha"
      }
    },
    "open": {
      "android": {
        "track": "beta"
      }
    },
    "production": {
      "android": {
        "track": "production"
      }
    }
  }
}
```

Adapt track names to the team’s Play Console setup.

Submit to internal:

```bash
eas submit --platform android --profile internal
```

Submit to production only after approval:

```bash
eas submit --platform android --profile production
```

## Service Account Gate

Before EAS Submit:

- Google Play Android Developer API enabled
- service account created in Google Cloud
- service account invited to Play Console
- permissions scoped to needed app/actions
- JSON key uploaded to EAS credentials or stored securely in CI
- key not committed to repository
- first manual Play upload completed if required

## Blocker-to-Automation Map

| Readiness blocker | Automation response |
| --- | --- |
| Missing `android.package` | Edit app config; do not build yet |
| Duplicate `versionCode` | Enable remote autoIncrement or increment local config; rebuild |
| Wrong artifact type | Run production AAB build |
| Target SDK not verified | Inspect Expo SDK/native Gradle; upgrade before build if needed |
| Signing issue | Use `eas credentials --platform android`; verify Play App Signing |
| Service account submit failure | Fix Play API/service account permissions; rerun submit |
| Review access missing | Update Play Console manually; no build needed |
| Data Safety incomplete | Complete Play Console forms manually; no build needed unless code changes |
| AI reporting missing | Implement app change; build new AAB; submit after QA |
| Store listing screenshots missing | Upload assets via Play Console or fastlane; no build needed |
| Subscription products missing | Configure Play products; retest app; no production submit |
| Pre-launch crash | Fix code; rebuild; upload to internal/closed track |

## Fastlane / Google Play Developer API Use

Use fastlane/supply or direct Google Play Developer API only if the project already has it configured or the user explicitly wants it.

Safe uses:

- validate metadata
- upload screenshots/listing
- upload AAB to internal track
- promote staged releases after approval
- update rollout percentage after approval

Risky uses requiring explicit approval:

- production release creation
- rollout percentage increase
- release promotion
- metadata changes visible to users
- pricing/subscription changes

## CI/CD Release Gates

CI should block release if:

- tests fail
- production AAB not produced
- versionCode duplicate risk detected
- target SDK not compliant
- app config missing package/version
- app content readiness checklist is not approved
- Data Safety/privacy review not approved for data/SDK changes
- AI/UGC safety review not approved for AI/UGC changes
- billing review not approved for purchase changes

## Recommended Release Sequence

1. Inspect config and current release state.
2. Resolve policy/declaration blockers.
3. Build production AAB.
4. Submit to internal track.
5. Install from Play and run smoke QA.
6. Submit/promote to closed/open/production as appropriate.
7. Use staged rollout for production.
8. Monitor Play vitals, crash reports, backend metrics, billing, and AI/UGC reports.

## Output Format

### Automation recommendation

| Step | Command / Action | Mutates? | Approval needed? |
| --- | --- | --- | --- |
| Inspect config | `npx expo config --type public` | no | no |
| Build AAB | `eas build --platform android --profile production` | yes, creates build | usually no |
| Submit internal | `eas submit --platform android --profile internal` | yes | yes |
| Promote production | Play Console / API | yes | explicit approval required |

### Safety note

State whether the next command is read-only, build-only, track upload, or production-affecting.
