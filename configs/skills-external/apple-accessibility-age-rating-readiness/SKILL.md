---
name: apple-accessibility-age-rating-readiness
description: "Review Apple accessibility behavior, Accessibility Nutrition Labels, age rating, minors, and gating requirements."
---

# Apple Accessibility and Age-Rating Readiness

Use this skill to check whether the app is usable by people with accessibility needs and whether age-related metadata and controls match the app’s real content.

## Core Rule

Accessibility and age rating are not metadata chores. They must match actual in-app behavior.

## Progressive Flow

### Level 0 — Accessibility/age verdict

Report:

- **Ready** — evidence supports claims and rating.
- **Blocked** — age rating or access control conflicts with actual app behavior.
- **Needs testing** — insufficient accessibility evidence.
- **Needs metadata update** — App Store answers are stale.

### Level 1 — Coverage table

| Area | Status | Evidence | Next step |
| --- | --- | --- | --- |
| VoiceOver | Pass/Fail/Unknown | Critical flows tested | Fix labels/order |
| Larger Text | Pass/Fail/Unknown | No clipped CTAs | Adjust layout |
| Contrast | Pass/Fail/Unknown | Light/dark checked | Fix tokens |
| Reduced Motion | Pass/Fail/Unknown | Strong motion reduced | Add fallback |
| Age rating | Pass/Fail/Unknown | Questionnaire current | Update answers |
| Creator content | Pass/Fail/N/A | Age controls exist | Add reporting/gating |

## Required Accessibility Checks

### 1. VoiceOver

Test critical flows:

- onboarding
- login
- primary action
- form validation
- paywall and purchase
- restore purchases
- AI prompt/send flow
- consent/disclosure screens
- account deletion
- error states

Check:

- controls have useful labels
- labels do not repeat useless text
- decorative icons are hidden from accessibility where appropriate
- focus order matches visual order
- modal/sheet focus is contained
- selected state is announced
- loading and success/failure states are announced

### 2. Larger Text / Dynamic Type

Check:

- text does not clip
- buttons remain tappable
- bottom bars do not hide content
- form fields remain usable
- paywall disclosures remain visible
- key consent copy remains readable
- layout adapts rather than shrinking everything

For React Native/Expo:

- ensure custom text components do not accidentally disable font scaling unless justified
- test with large accessibility text sizes on device

### 3. Contrast and dark mode

Check:

- foreground/background contrast in light and dark mode
- disabled states still readable enough
- placeholder text not too faint
- focus/error states visible without color alone
- screenshots do not rely on low-contrast overlays
- charts or status indicators have text/glyph fallbacks

### 4. Reduced Motion

Check:

- large transitions have reduced alternatives
- looping animations can stop or are minimal
- loading states are not dizzying
- confetti/parallax/spinning is disabled or reduced
- AI response streaming does not create excessive motion

### 5. Differentiate without color alone

Do not rely only on red/green or color changes.

Use:

- icons
- labels
- borders
- patterns
- status text
- selected markers

### 6. Captions and audio descriptions

If the app includes video/audio:

- captions are available where needed
- important audio-only information has a visual equivalent
- important visual-only information has accessible text or description where needed

## Age Rating Checks

### 1. Updated age-rating questions

Verify App Store Connect age-rating answers reflect current app behavior.

Consider:

- AI-generated text/images/audio/video
- user-generated content
- creator content
- chat or anonymous interaction
- medical or wellness topics
- violence or mature themes
- gambling/contests
- unrestricted web access
- in-app controls
- location sharing

### 2. Metadata consistency

Ensure:

- screenshots/previews are appropriate for the metadata audience
- app name/subtitle/description do not imply Kids Category unless actually in Kids Category
- mature content is not shown in public storefront assets
- app age rating matches in-app content controls

### 3. Creator content age controls

If users can create, publish, share, or monetize content/experiences:

- provide a way to identify/report content that exceeds the app’s age rating
- use a verified or declared age mechanism to limit underage access
- apply moderation to UGC
- make content requiring purchases clear
- ensure creator content does not change the native app’s core functionality into hidden apps

### 4. Random or anonymous chat

If the app includes random or anonymous chat:

- treat it as high App Review risk
- ensure UGC moderation requirements are implemented
- avoid making random/anonymous chat the primary use case
- provide report/block/contact mechanisms
- document moderation in App Review notes

## Accessibility Nutrition Labels

If providing Accessibility Nutrition Labels:

- claims must be supported by actual testing
- do not overclaim VoiceOver, Larger Text, Reduced Motion, or captions support
- keep answers updated as UI changes
- test the exact submitted build

## Output Format

```md
## Accessibility/age verdict
Ready / Blocked / Needs testing / Needs metadata update

## Findings
| Area | Finding | Risk | Fix | Verification |
| --- | --- | --- | --- | --- |

## Required device tests
- [ ] VoiceOver critical flow
- [ ] Larger Text critical screens
- [ ] Dark mode
- [ ] Reduced Motion
- [ ] Paywall/accessibility path

## Age-rating notes
- Current rating evidence:
- Required App Store Connect updates:
- Creator/UGC age controls:
```
