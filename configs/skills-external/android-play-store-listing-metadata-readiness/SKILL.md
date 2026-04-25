---
name: android-play-store-listing-metadata-readiness
description: "Review Play Store title, descriptions, screenshots, assets, claims, links, tags, and listing consistency."
---

# Android Play Store Listing & Metadata Readiness

Use this skill to make sure the Google Play listing is clear, accurate, appealing, and policy-safe.

## Core Rule

The listing should sell the real product, not a fantasy version of it. Misleading metadata can block review or create enforcement risk after release.

## Required Store Listing Assets

Check:

- app name
- short description
- full description
- app icon
- feature graphic
- phone screenshots
- tablet screenshots if supporting tablets
- preview video if used
- category and tags
- contact email
- website if applicable
- privacy policy link
- localized listings if enabled
- release notes

## Metadata Policy Safety

Avoid:

- misleading claims
- exaggerated AI accuracy claims
- unsupported “#1”, “best”, “official”, “guaranteed”, “certified” claims
- competitor names unless legally and contextually appropriate
- fake urgency: “limited time”, “download now”, “new 2026 miracle”
- promotional text in app icon
- rating, ranking, price, or discount claims in icon or screenshots when not allowed
- impersonation of Google, Android, government agencies, financial institutions, health authorities, or other apps
- screenshots that show unavailable features
- screenshots that hide required disclaimers or paywalls
- privacy/security claims not backed by actual implementation

## AI App Listing Rules

For AI apps, be especially careful.

Bad claims:

```text
The only AI assistant you will ever need.
Always accurate medical advice.
Guaranteed undetectable AI writing.
Create realistic videos of anyone.
```

Better:

```text
Draft, summarize, and organize notes with AI assistance.
Responses may be inaccurate; verify important information.
Report unsafe or offensive responses directly in the app.
```

AI listing checklist:

| Area | Requirement |
| --- | --- |
| Capability claims | Match actual app behavior |
| Accuracy | No guarantees unless provable and scoped |
| Safety | Do not claim perfect moderation/safety |
| Data handling | Do not say “private” if prompts/files leave device |
| Professional advice | Avoid implying medical/legal/financial authority |
| Generated media | Avoid deepfake/scam-adjacent positioning |
| Minors | Rating and listing match access controls |

## Screenshot Quality

Screenshots should show:

- real UI from production build or production-equivalent state
- core value quickly
- important screens, not just splash/onboarding
- Android-native status/navigation treatment
- no debug banners, staging URLs, fake API keys, or internal emails
- no copyrighted content unless licensed
- no personal data from real users
- no misleading premium-free implication

For AI apps, include a screenshot of the safety/report affordance when helpful.

For subscriptions, do not make screenshots imply free access to paid features unless the flow is genuinely free.

## Icon and Feature Graphic

Icon:

- no text-heavy design
- no notification badges or fake system UI
- no price/ranking/promo claims
- works as adaptive icon
- foreground and background are production-ready
- recognizable at small sizes

Feature graphic:

- brand/benefit-focused
- not overcrowded
- no unsupported claims
- no fake platform badges
- localize where needed

## Description Structure

Good full description:

```text
[One clear sentence about what the app does.]

Use it to:
• [Core job 1]
• [Core job 2]
• [Core job 3]

Key features:
• [Feature with user benefit]
• [Feature with user benefit]
• [Feature with user benefit]

Important:
[AI / health / subscription / privacy / hardware / regional limitation disclosure if needed.]
```

Avoid keyword stuffing. ASO is useful only when it does not weaken trust.

## Localization

If localized listings are enabled:

- every localized title/description must be accurate
- screenshots should match language/region where possible
- privacy and subscription claims must remain consistent
- regulated claims must be reviewed in each locale
- AI disclaimers should not disappear in localization

Do not publish machine-translated legal or medical claims without review.

## Store Contact and Links

Verify:

- support email is monitored
- website works
- privacy policy URL works
- data deletion URL works if needed
- developer name matches legal/privacy policy expectations

## Blockers

| Blocker | Fix |
| --- | --- |
| Listing claims unsupported AI/health/finance capabilities | Rewrite to accurate scoped claims |
| Screenshots show unavailable features | Replace with production-equivalent screenshots |
| Privacy policy link broken | Publish accessible privacy URL and update Play Console |
| Icon contains promo text/rating/price | Replace icon with compliant asset |
| App title impersonates another brand | Rename and remove misleading references |
| Subscription screenshots hide pricing/renewal implications | Add accurate paywall screenshots or remove misleading assets |

## Output Format

### Listing verdict

`Ready` / `Not ready` / `Unknown`

### Metadata review

| Asset | Status | Notes |
| --- | --- | --- |
| Title | pass | Accurate and non-misleading |
| Short description | warn | AI claim needs softer wording |
| Screenshots | block | Shows staging workspace |

### Required edits

| Before | After | Reason |
| --- | --- | --- |
| “Always accurate AI health answers” | “AI-assisted wellness notes; not medical advice” | Removes misleading health/AI claim |
