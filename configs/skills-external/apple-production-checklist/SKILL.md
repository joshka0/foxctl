---
name: apple-production-checklist
description: "Run a staged App Store production-readiness checklist across build, metadata, review, privacy, IAP, TestFlight, and accessibility."
---

# Apple Production Checklist

Use this skill to answer one question first:

> Can this app be submitted to Apple now without an obvious preventable review or upload failure?

Do not start by dumping a giant checklist. Use progressive disclosure.

## Operating Principle

Separate readiness into three layers:

1. **Apple-blocking issues** — likely upload, validation, or App Review blockers.
2. **Launch-quality issues** — not always rejection-worthy, but harmful to users or conversion.
3. **Operational issues** — missing automation, missing notes, unverified accounts, or unclear owner.

Always report the highest-risk blockers first.

## Progressive Disclosure Flow

### Level 0 — Release verdict

Give a compact answer:

- **Ready to submit**
- **Not ready — blocking issues remain**
- **Probably ready, but needs manual verification**
- **Unknown — missing required evidence**

Then list only the top blockers.

Example:

```md
Verdict: Not ready — blocking issues remain.

Top blockers:
1. App Review cannot access paid AI features without a demo path.
2. Third-party AI data sharing is not disclosed before prompt submission.
3. The build was produced with an SDK that will miss the April 28, 2026 minimum requirement.
```

### Level 1 — Domain summary

Group readiness by domain:

| Domain | Status | Highest-risk item | Next step |
| --- | --- | --- | --- |
| Build & binary | Blocked | Build not validated | Run build/preflight checks |
| Review packet | Needs work | Missing demo account/video | Prepare reviewer notes |
| AI/privacy | Blocked | No explicit consent for third-party AI sharing | Add consent copy |
| Metadata | Needs work | Screenshots do not show app in use | Recapture screenshots |

Use only relevant domains.

### Level 2 — Checklist details

Open only the domain the user asks about, or the domain with the highest risk.

Use domain skills:

- `apple-build-binary-readiness`
- `apple-submission-packet-readiness`
- `apple-ai-privacy-safety-readiness`
- `apple-metadata-storefront-readiness`
- `apple-iap-subscription-readiness`
- `apple-testflight-release-readiness`
- `apple-accessibility-age-rating-readiness`
- `apple-asc-cli-automation-bridge`

### Level 3 — Remediation plan

For each blocker, provide:

| Blocker | Evidence | Fix | Owner | Verification |
| --- | --- | --- | --- | --- |

Do not provide vague advice. Every row must end with a verification step.

## Required Domains

### 1. Build and binary readiness

Use `apple-build-binary-readiness`.

Must cover:

- Xcode / SDK minimum requirement
- Release configuration
- signing and provisioning
- build number uniqueness
- crash-free launch
- permissions and purpose strings
- encryption/export compliance
- entitlements matching actual behavior
- no debug/test artifacts
- backend live during review

### 2. App Review submission packet

Use `apple-submission-packet-readiness`.

Must cover:

- demo account or full demo mode
- reviewer notes
- demo video when review may not reproduce behavior
- paid feature access
- special configuration
- hardware / external account / QR-code requirements
- support contact and privacy policy links
- non-obvious AI features, background behavior, or entitlement use

### 3. AI, privacy, and safety readiness

Use `apple-ai-privacy-safety-readiness`.

Must cover:

- third-party AI data sharing disclosure
- explicit consent before sending personal data to third-party AI
- App Privacy answers
- privacy policy accuracy
- user data deletion / account deletion
- minors, creator content, UGC, anonymous/random chat
- misleading AI claims
- regulated domains such as health, finance, legal, safety, or education

### 4. Metadata and storefront readiness

Use `apple-metadata-storefront-readiness`.

Must cover:

- app name/subtitle/keywords
- screenshots and app previews
- in-app purchase/subscription disclosure in metadata
- age-rating consistency
- no misleading claims
- localizations
- support URL and privacy URL
- “What’s New” specificity for meaningful updates

### 5. IAP and subscription readiness

Use `apple-iap-subscription-readiness`.

Must cover:

- IAP/subscription metadata
- review screenshots
- subscription group setup
- pricing and availability
- paywall disclosure
- restore purchases
- sandbox testing
- first-review attachment flow
- RevenueCat or backend catalog consistency, if used

### 6. TestFlight and release health

Use `apple-testflight-release-readiness`.

Must cover:

- recent TestFlight coverage
- crash and feedback triage
- performance diagnostics
- What to Test notes
- tester groups
- regression check
- external beta review state, if relevant

### 7. Accessibility and age-rating readiness

Use `apple-accessibility-age-rating-readiness`.

Must cover:

- accessibility labels and navigation
- Dynamic Type / Larger Text
- contrast and dark mode
- reduced motion
- VoiceOver-critical flows
- Accessibility Nutrition Labels, if provided
- updated age-rating questions
- creator content and underage access restrictions

### 8. ASC automation bridge

Use `apple-asc-cli-automation-bridge` when translating findings into `asc` commands or when using the `rorkai/app-store-connect-cli-skills` pack.

## Apple Review Risk Categories

### Hard blockers

Treat as likely rejection, upload failure, or review delay:

- App crashes or obvious broken functionality
- reviewer cannot access the full app
- missing demo account or demo mode for account-gated features
- hidden or undocumented features
- incomplete metadata, screenshots, URLs, or privacy policy
- missing purpose strings for protected data access
- undisclosed third-party AI data sharing
- missing explicit consent before sending personal data to third-party AI
- App Privacy answers inconsistent with actual SDKs or data flows
- UGC without reporting, blocking, moderation, and contact mechanisms
- random or anonymous chat used as a primary experience
- paid digital goods sold outside IAP where IAP is required
- incomplete IAP/subscription products
- unresolved encryption/export compliance
- build not processed or not valid
- unsupported SDK/toolchain after Apple’s enforcement date

### Manual verification required

Flag these as “needs human confirmation”:

- App Privacy publish state when not verifiable by public API
- legal/regulatory licensing
- medical clearance
- rights to third-party content
- specialized hardware workflows
- financial, health, legal, gambling, VPN, crypto, or cannabis claims
- app behavior that depends on external accounts or real-world context

### Launch-quality issues

Important, but usually not immediate rejection on their own:

- weak screenshots
- generic release notes for meaningful feature changes
- poor onboarding
- untested localization
- confusing paywall copy
- low-quality demo video
- unclear empty/error/offline states
- insufficient accessibility polish

## Output Format

Use this structure unless the user asks for something narrower:

```md
## Verdict
Ready / Not ready / Probably ready / Unknown

## Top blockers
1. ...
2. ...
3. ...

## Domain status
| Domain | Status | Evidence | Next step |
| --- | --- | --- | --- |

## Recommended next action
One concrete action or command, not a giant list.
```

When the user asks to go deeper, expand only the relevant domain.

## Review Discipline

- Verify current Apple requirements before final submission.
- Prefer official Apple sources over blog posts.
- Treat App Review outcomes as probabilistic, not guaranteed.
- Never claim an app “will pass review.” Say “no obvious blockers found.”
- Distinguish API-verifiable facts from manual review judgment.
- Do not run destructive or submitting commands without explicit confirmation.
- Use ASC automation skills for commands; use this skill for release judgment.
