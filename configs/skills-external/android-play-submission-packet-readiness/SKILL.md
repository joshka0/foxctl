---
name: android-play-submission-packet-readiness
description: "Prepare Play review notes, app access, demo accounts, declarations, release notes, and reviewer evidence."
---

# Android Play Submission Packet Readiness

This skill checks whether Google Play reviewers and release managers have everything needed to review, test, and approve the app.

A technically valid AAB is not enough. Google Play must be able to access and understand the app.

## Required Review Access

If any app functionality is gated by login, subscription, invite, QR code, external account, location, device pairing, seeded workspace, paid plan, organization membership, or external hardware, provide review access.

Reviewer access must be:

- valid at all times
- reusable
- location-independent
- not dependent on expiring OTP codes
- in English where instructions are needed
- able to bypass 2FA or include explicit bypass instructions
- able to access paid/subscription/paywalled features for free during review
- seeded with enough data to show the core experience

## App Access Checklist

| Item | Pass condition |
| --- | --- |
| Login not required | Reviewer can reach all reviewable app areas without an account |
| Login required | Demo credentials provided in Play Console App access |
| 2FA / OTP | Reusable reviewer path exists or bypass instructions are included |
| Social login | Test account and provider instructions included |
| QR/barcode required | Static URL or image provided in Play Console |
| Subscription/paywall | Reviewers can access paywalled functionality without payment |
| Invite-only app | Review invite or reviewer account is pre-provisioned |
| External hardware | Demo video, simulator mode, or review instructions are included |
| AI app | Seed prompts and expected safe/unsafe behavior notes included |
| Backend-dependent app | Backend is live and stable during review |

## Demo Video Guidance

Google Play does not require a demo video for every app. Treat a private review video as strongly advisable when review cannot reliably reproduce the app.

Attach or link a demo video when:

- app requires external hardware
- app requires a hard-to-replicate environment
- app requires a seeded account/workspace
- AI behavior depends on special prompts or backend configuration
- review must see moderation/reporting behavior
- app uses regulated features like health/finance/government/VPN
- app has paid entitlement flows reviewers must verify
- app uses QR code, device pairing, geofence, or location-dependent flows

Good demo video structure:

1. launch app
2. sign in with reviewer credentials
3. show core happy path
4. show gated/premium path if present
5. show AI or UGC safety/reporting path if present
6. show account deletion or data controls if relevant
7. show where app disclosures appear
8. keep it short and factual

Do not use marketing-style videos for review evidence. Use a sober walkthrough.

## App Content Forms

Check the Play Console App content section before every meaningful release.

Common required or conditional forms:

- Data Safety
- App access
- Ads
- Content rating questionnaire
- Target audience and content
- News declaration if applicable
- Health apps declaration if applicable
- Financial features declaration
- Government declaration if applicable
- Permissions declarations for restricted permissions/APIs
- Data deletion questions
- AI/UGC-related evidence if requested

If the app has no financial features, still verify whether the Financial features declaration is complete, because Google has made this declaration broadly relevant across developer accounts.

## Release Notes

Release notes should be concise, user-facing, and honest.

Good:

```text
- Improved onboarding reliability
- Added safer AI response reporting
- Fixed crashes affecting Android 15 devices
```

Bad:

```text
- Bug fixes and improvements
- Best AI app ever
- We fixed everything
```

For staged rollouts, release notes should not overpromise functionality that is server-gated or unavailable to some users.

## Track-Specific Packet

### Internal test

- Build installable by trusted testers
- Basic tester instructions included
- Login/test data ready
- Known issues documented

### Closed test

- Tester list/groups configured
- Opt-in link works
- Test plan exists
- Feedback mechanism exists
- Enough production-like data is available

### Open test

- Store listing is public-ready
- App does not reveal private staging content
- Feedback handling process exists

### Production

- All app content forms complete
- All review access provided
- Store listing complete
- Release notes complete
- Rollout plan chosen
- App is not obviously broken under current Android target behavior

## AI-Specific Submission Packet

For AI apps, include reviewer notes that answer:

- What AI features exist?
- What model/backend is used if relevant to policy or privacy?
- What user data is sent to AI systems?
- Where is this disclosed?
- How can users report or flag offensive AI output?
- What happens after a report?
- Are minors allowed?
- Does the AI generate images/video/voice of real people?
- Does the AI provide health, legal, financial, educational, or safety-critical advice?
- Are there prompt/output filters?

Keep this factual, not defensive.

## Blockers

| Blocker | Fix |
| --- | --- |
| No reviewer credentials for gated app | Add reusable credentials and instructions in Play Console App access |
| Review path requires OTP / expiring code | Create reviewer bypass or static test credential |
| Subscription content inaccessible | Add reviewer entitlement or instructions to access paid features freely |
| External hardware required with no evidence | Provide hardware, simulation mode, or demo video |
| AI app lacks reviewer explanation | Add AI feature notes and test prompts |
| App content forms incomplete | Complete Play Console App content before review |
| Backend not live | Deploy stable review backend before submitting |

## Output Format

### Submission packet verdict

`Ready` / `Not ready` / `Unknown`

### Review-access table

| Area | Status | Notes |
| --- | --- | --- |
| Login | pass | Demo account works |
| Subscription | block | No reviewer entitlement configured |
| AI safety | warn | Reporting exists but reviewer notes missing |

### Required fixes

| Fix | Where | Owner | Verification |
| --- | --- | --- | --- |
| Add seeded reviewer account | Play Console > App access | Product | Reviewer can complete core journey |
