---
name: android-play-production-checklist
description: "Run a staged Google Play production-readiness checklist for Expo or React Native Android releases and launch blockers."
---

# Android Play Production Checklist

Use this as the top-level readiness orchestrator for releasing an Expo / React Native app to Google Play.

The purpose is not to recite every possible Google Play rule. The purpose is to produce a useful release verdict, identify the highest-risk blockers, and progressively disclose only the checklist areas that matter for this app.

## Core Principle

A Google Play release is ready only when five things are true:

1. the binary is technically valid and signed correctly
2. the Play Console submission packet is complete
3. policy declarations match actual app behavior
4. reviewers and testers can access the full app
5. production rollout risk is understood and controlled

Do not let successful `eas build` or successful upload be mistaken for production readiness.

## Progressive Disclosure Model

### Level 0 — Verdict

Start with one of these verdicts:

| Verdict | Meaning |
| --- | --- |
| `Ready to submit` | No known blockers; remaining items are routine checks. |
| `Probably ready, verify these items` | No obvious blockers, but important evidence is missing. |
| `Not ready` | One or more blockers would likely prevent review, publishing, or safe rollout. |
| `Unknown` | Not enough information to judge; gather specific facts rather than dumping a full checklist. |

### Level 1 — Domain Status

Show a compact domain table.

| Domain | Status | Why it matters |
| --- | --- | --- |
| Build / binary | pass / warn / block / unknown | AAB, signing, versionCode, target SDK, package name |
| Play Console packet | pass / warn / block / unknown | App access, app content, declarations, review notes |
| Data safety / permissions | pass / warn / block / unknown | Privacy policy, SDKs, sensitive permissions, data deletion |
| AI / UGC safety | pass / warn / block / unknown | AI reporting, prohibited outputs, moderation, minors |
| Store listing | pass / warn / block / unknown | Metadata policy, screenshots, icon, claims, localization |
| Testing / release tracks | pass / warn / block / unknown | Internal/closed/open/production, 12 testers if applicable |
| Billing / subscriptions | pass / warn / block / unknown | Play Billing, entitlements, pricing, cancellation |
| Accessibility / age rating | pass / warn / block / unknown | Target audience, rating, Families, Android accessibility |
| Sensitive categories | pass / warn / block / unknown | Health, finance, government, VPN, prediction markets, etc. |
| Quality / rollout | pass / warn / block / unknown | Pre-launch, crashes, ANRs, staged rollout plan |

### Level 2 — Open only relevant domain skills

Use the specialized skill only when that domain is relevant or blocked:

- `android-expo-eas-build-binary-readiness`
- `android-play-submission-packet-readiness`
- `android-play-data-safety-permissions-readiness`
- `android-play-ai-ugc-safety-readiness`
- `android-play-store-listing-metadata-readiness`
- `android-play-testing-track-readiness`
- `android-play-iap-subscriptions-readiness`
- `android-play-accessibility-age-rating-readiness`
- `android-play-sensitive-category-readiness`
- `android-play-quality-prelaunch-readiness`
- `android-play-console-eas-automation-bridge`

### Level 3 — Remediation plan

For every blocker, provide:

| Field | Required content |
| --- | --- |
| Blocker | What will stop submission/review/release |
| Evidence | File, setting, policy form, screenshot, build log, or missing information |
| Fix | Concrete change |
| Verification | How to prove it is fixed |
| Automation | EAS / Play Console / API command only if appropriate |
| Owner | Engineering / product / legal / design / support |

## Inputs to Gather

Ask only for the missing facts needed to produce a verdict. When possible, infer from project files.

Important facts:

- Is this a new app or an update?
- Is the Play developer account personal or organization?
- Is the app already granted production access?
- Does the app use Expo managed/prebuild/bare workflow?
- What Expo SDK and React Native version are used?
- Is the release built with EAS Build or local Gradle?
- Does the app use EAS Update / OTA updates?
- Does the app use AI-generated content?
- Does the app include UGC, chat, social, comments, profiles, or public sharing?
- Does the app require login, subscription, QR code, invite, external hardware, or seeded data?
- Does it collect personal/sensitive data?
- Does it request contacts, location, camera, microphone, photos/videos, health, SMS/call logs, accessibility, VPN, background location, exact alarm, notification, or foreground service permissions?
- Does it sell digital goods/subscriptions?
- Is it health, finance, government, VPN, news, dating/matchmaking, gambling/contest, prediction market, children/family, crypto, or regulated?

## Hard Blockers

Treat these as likely blockers until proven otherwise:

- No unique `android.package` / application ID.
- Duplicate or non-incremented `android.versionCode` for a new upload.
- Wrong artifact for Play production: APK where AAB is expected for ordinary Play distribution.
- Target SDK below current Google Play requirement for new apps/updates.
- App not signed with valid upload key / Play App Signing not configured.
- Reviewers cannot access locked features, subscription content, seeded AI features, or account-only flows.
- Data Safety section missing, inaccurate, or inconsistent with app behavior or privacy policy.
- Privacy policy missing, PDF-only, geofenced, inactive, not clearly labeled, or not in-app when required.
- App collects sensitive data without prominent disclosure/consent where required.
- Broad contacts/location/photo/video/file permissions when system picker or narrower API is sufficient.
- Generative AI app lacks in-app offensive-content reporting or flagging.
- UGC app lacks terms, moderation, reporting, blocking, or child-safety safeguards.
- Digital goods/subscriptions sold outside Google Play Billing without a valid regional/program exception.
- Content rating or target audience is inaccurate.
- New personal developer account has not completed required closed testing before production access.
- App crashes, hangs, or cannot complete its core user journey.
- Metadata makes misleading AI, health, finance, privacy, security, or earning claims.

## High-Risk Warnings

Treat these as warnings that may require specialized review:

- AI companion, AI therapy, AI medical advice, AI legal/financial advice, AI image/video generation, deepfake-like features, or real-person voice/video synthesis.
- Children or teens can access AI/UGC/social features.
- App has anonymous/random chat or matching features.
- App uses health data, Health Connect, wearable data, mental health content, or symptom analysis.
- App uses Accessibility API for automation, screen reading, interaction with other apps, or agentic workflows.
- App uses foreground services, background location, exact alarm, SMS/call logs, contacts, or broad storage access.
- App offers subscriptions, free trials, token packs, credits, or usage-based AI consumables.
- App uses Firebase/Google Sign-In/OAuth and certificate fingerprints may differ between debug, upload, and Play app-signing keys.
- App relies on external backend services that may not be live during review.

## Current Google Play Policy Watch Items

Always verify current docs, but treat these as important current themes:

- New apps and app updates currently need a recent target API level; check the official target API level page before submission.
- New personal developer accounts may need a closed test with at least 12 opted-in testers for 14 continuous days before applying for production access.
- Google Play AI-generated content policy requires in-app reporting/flagging for apps that generate AI content.
- Developers are responsible for AI outputs that violate restricted content, deceptive behavior, child safety, or harassment policies.
- Data Safety declarations must include data handled by third-party SDKs/libraries.
- All apps need a privacy policy, even when no personal/sensitive data is collected.
- April 2026 updates introduced a Contacts Permissions policy and recommended narrower contacts/location access patterns.
- October 2025 updates added or clarified requirements around age-restricted features, health, accessibility API, subscriptions, malware terminology, and financial features declarations.

## Output Format

Use this format by default.

### Verdict

`Not ready` / `Probably ready` / `Ready to submit` / `Unknown`

One sentence explaining the verdict.

### Domain status

| Domain | Status | Notes |
| --- | --- | --- |
| Build / binary | block | `versionCode` unknown and target SDK not verified |
| Play Console packet | warn | Review access likely needs seeded demo account |
| AI / UGC safety | block | AI app has no in-app content reporting |

### Blockers

| Blocker | Fix | Verification | Owner |
| --- | --- | --- | --- |
| AI-generated content lacks in-app report flow | Add report/flag action in AI output UI and moderation queue | Reviewer can report an output without leaving app | Product + Engineering |

### Next actions

1. Do the smallest set of actions that removes blockers.
2. Then run the relevant EAS/Play Console automation bridge.
3. Do not submit to production until blockers are cleared.

## Rules for Automation

- Never mutate Play Console or submit a production release without explicit user approval.
- Prefer read-only inspection first.
- Prefer internal testing before closed/open/production if the account and app state are unknown.
- Use staged rollout for meaningful production updates unless the change is tiny and low-risk.
- Do not use automation to bypass unresolved policy issues.

## Final Gate

Before recommending production rollout, confirm:

- AAB exists and is signed correctly.
- Target SDK is compliant.
- `versionCode` is unique and incremented.
- Play App Signing / upload key / API fingerprints are understood.
- Store listing is complete.
- App content declarations are complete.
- Data Safety and privacy policy match actual behavior.
- App access instructions are reusable and valid.
- AI/UGC/reporting/moderation requirements are satisfied if applicable.
- Billing and subscriptions are correct if applicable.
- Content rating and target audience are accurate.
- Pre-launch report and critical QA are clean enough.
- Rollout plan is staged or otherwise justified.
