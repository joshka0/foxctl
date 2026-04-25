# Android Play Production Checklist Skills

A progressive-disclosure skill pack for shipping Expo / React Native Android apps to Google Play.

These skills are designed to sit above Expo EAS, Google Play Console, Google Play Developer API, fastlane/supply, or any project-specific release automation. They decide whether a release is actually ready; automation should only execute after the relevant readiness skill says the path is clear.

## Skill map

| Skill | Purpose |
| --- | --- |
| `android-play-production-checklist` | Main progressive-disclosure orchestrator and release verdict. |
| `android-play-submission-packet-readiness` | Play Console review access, app content forms, release notes, demo accounts, review instructions. |
| `android-expo-eas-build-binary-readiness` | Expo/EAS Android package, AAB, signing, target SDK, versionCode, runtimeVersion, build profile. |
| `android-play-data-safety-permissions-readiness` | Data safety, privacy policy, account deletion, permissions, SDK data collection, April 2026 contacts/location updates. |
| `android-play-ai-ugc-safety-readiness` | AI-generated content, in-app reporting, UGC moderation, minors, deceptive/high-risk outputs. |
| `android-play-store-listing-metadata-readiness` | Store listing, screenshots, icon, descriptions, metadata policy, ASO without policy risk. |
| `android-play-testing-track-readiness` | Internal/closed/open/production tracks, 12-tester rule for new personal accounts, staged rollout. |
| `android-play-iap-subscriptions-readiness` | Play Billing, subscriptions, entitlement verification, disclosure and cancellation flows. |
| `android-play-accessibility-age-rating-readiness` | Accessibility, content rating, target audience, Families, age-restricted features. |
| `android-play-sensitive-category-readiness` | Health, financial, government, VPN, accessibility API, prediction markets, news, regulated categories. |
| `android-play-quality-prelaunch-readiness` | Pre-launch report, crashes, ANRs, performance, Android 15 behavior, device coverage. |
| `android-play-console-eas-automation-bridge` | Maps checklist results to safe EAS/Play Console automation commands. |

## Operating principle

Do not dump a giant release checklist at the user. Start with a verdict, then progressively reveal the blocking area.

1. **Verdict first** — ready / probably ready / not ready / unknown.
2. **Domains second** — build, policy, metadata, testing, billing, AI/UGC, quality.
3. **Blockers third** — each blocker has owner, fix, verification, and command if applicable.
4. **Automation last** — read-only or dry-run before mutation; production release only after explicit approval.

## Source freshness

Policies move. Before giving a final answer about Google Play policy, target SDK, submission requirements, or Expo/EAS behavior, verify against current official documentation.

Start with `references/official-sources.md`.
