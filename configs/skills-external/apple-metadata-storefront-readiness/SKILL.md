---
name: apple-metadata-storefront-readiness
description: "Review App Store metadata, screenshots, previews, keywords, links, localization, and storefront claims."
---

# Apple Metadata and Storefront Readiness

Use this skill to ensure App Store metadata is accurate, complete, compliant, and conversion-ready.

## Core Rule

Metadata must accurately reflect the current submitted app. Do not promise functionality the build does not include, hide required purchases, or use screenshots/previews that fail to show the app in use.

## Progressive Flow

### Level 0 — Metadata verdict

Report:

- **Ready** — complete and consistent.
- **Blocked** — likely rejection/delay.
- **Needs polish** — not likely a hard blocker, but poor storefront quality.
- **Unknown** — metadata evidence missing.

### Level 1 — Storefront table

| Area | Status | Risk | Next step |
| --- | --- | --- | --- |
| Name/subtitle | Pass | Low | None |
| Screenshots | Needs work | Medium | Show app in use |
| App preview | Optional | Low | Add if useful |
| Privacy/support URLs | Blocked | High | Fix links |
| IAP disclosure | Needs work | High | Add disclosure |
| Localizations | Unknown | Medium | Validate fields |

## Required Checks

### 1. Name, subtitle, and keywords

Check:

- App name is unique and within limit.
- Subtitle describes the app, not generic hype.
- Keywords are relevant, not trademark-stuffed.
- No prices, unsupported claims, platform names, or competitor names where inappropriate.
- Claims are verifiable.

Bad:

```text
Best AI Lawyer, Doctor & Money Maker
```

Better:

```text
AI notes for client meetings
```

### 2. Description

Description should:

- explain actual functionality
- disclose subscriptions or paid features at a high level
- avoid unverifiable superlatives
- avoid claiming Apple endorsement
- describe AI limitations if the app is high-risk
- avoid keyword stuffing
- stay aligned with screenshots and app behavior

### 3. Screenshots

Screenshots should show the app in use, not only splash screens, login screens, title art, or abstract marketing art.

Checklist:

- Show core user value in the first screenshot.
- Use real-looking fictional data, never real personal data.
- Do not show features hidden behind unavailable flags.
- If a purchase is needed for shown functionality, disclose it visually or in copy.
- Use device sizes required for the current platform targets.
- Localize screenshots where the locale is meaningful.
- Avoid references that date quickly unless truly part of the release.
- Avoid competitor marks or protected third-party content without rights.

### 4. App previews

App previews are public marketing videos, not the same as private App Review demo videos.

Use app previews when motion helps users understand the app.

Rules:

- Show only in-app footage and allowed overlays.
- Do not imply functionality that does not exist.
- Disclose purchase/subscription/login requirements if shown.
- Avoid prices, as they vary by region.
- Keep text legible and long enough to read.
- Keep content suitable for all audiences.

### 5. What’s New

For meaningful updates, explain what changed.

Good:

```text
New in 2.4:
- Added offline project search.
- Improved PDF import reliability.
- Fixed a crash when restoring purchases.
```

Acceptable for small maintenance updates:

```text
Bug fixes and performance improvements.
```

Do not use generic notes for major AI, privacy, subscription, or data-flow changes.

### 6. Privacy policy and support URL

Verify:

- URLs load publicly without login.
- Privacy policy matches actual data collection and AI processors.
- Support URL includes a reachable contact path.
- Links are not placeholders or empty pages.
- Localized storefronts do not point to broken locale-specific links.

### 7. IAP/subscription disclosure

If screenshots, previews, or description show paid functionality, disclose that it requires purchase/subscription.

Metadata should not make paid features look free.

Check:

- subscription is named consistently
- paid feature screenshots are not misleading
- paywall copy inside app matches App Store product descriptions
- “free trial” claims match actual offer configuration
- no price in screenshots unless controlled and globally accurate, which is rarely advisable

### 8. AI metadata claims

For AI apps:

- Avoid “guaranteed accurate,” “human-level,” or “certified” unless documented.
- Avoid implying medical/legal/financial authority without qualification and regulatory support.
- Describe what the AI helps users do, not magical outcomes.
- Mention human review where required for high-stakes use.
- Do not use “Apple Intelligence” or Apple-branded terms unless integrating official Apple technologies correctly.

### 9. Localizations

For each locale:

- required fields complete
- no machine-translation artifacts
- keywords localized, not literal translated blindly
- screenshots match language/region where appropriate
- privacy/support links valid
- units, dates, currencies, and examples localized

Use `asc-metadata-sync`, `asc-localize-metadata`, and `asc-aso-audit` when available.

## Output Format

```md
## Metadata verdict
Ready / Blocked / Needs polish / Unknown

## Storefront issues
| Field/asset | Issue | Risk | Fix |
| --- | --- | --- | --- |

## Copy edits
| Field | Before | After | Reason |
| --- | --- | --- | --- |

## Screenshot/app preview notes
- Required recaptures:
- Optional improvements:

## ASC automation
Use `asc-metadata-sync`, `asc-localize-metadata`, `asc-aso-audit`, and `asc-shots-pipeline` as appropriate.
```
