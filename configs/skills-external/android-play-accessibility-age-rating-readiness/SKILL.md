---
name: android-play-accessibility-age-rating-readiness
description: "Review Android accessibility, Play age rating, target audience, minors, ads, and sensitive access patterns before release."
---

# Android Accessibility, Age Rating & Audience Readiness

Use this skill to verify that the app is accessible enough, accurately rated, and configured for the right audience on Google Play.

## Core Rule

Play Console audience answers must match reality. Do not select an older audience to avoid child policy if the product, marketing, screenshots, characters, language, or acquisition channels clearly appeal to children.

## Content Rating

Before release:

- complete content rating questionnaire accurately
- update rating answers when features/content change
- account for UGC, AI outputs, chat, violence, sexual content, substances, gambling, health, finance, location sharing, and user interaction
- ensure screenshots and description match rating

If AI or UGC can produce mature content, the rating and safeguards must reflect that risk.

## Target Audience

Check:

| Question | Evidence |
| --- | --- |
| Who is the app designed for? | product positioning, copy, screenshots, acquisition |
| Can children access it? | age gate, account signup, content restrictions |
| Does it have UGC/social/chat? | reporting/blocking/moderation |
| Does it show ads? | SDKs and Families self-certified requirements if children included |
| Does it contain age-restricted features? | dating/matchmaking/gambling/real-money contests |
| Does AI generate potentially mature content? | filters, rating, gating, report flow |

## Children / Families Gate

If any target audience includes children:

- comply with Families requirements
- use only compliant/self-certified ad SDKs where required
- ensure content is child-appropriate
- avoid unsafe UGC/chat flows
- avoid behavioral advertising where prohibited
- handle data collection with stricter child privacy expectations
- avoid adult AI/UGC features for minors

If children are not the target audience, the listing and app should not appear child-directed.

## Age-Restricted Functionality

Apps with core dating/matchmaking or real-money gambling/games/contests must use Play Console tools to block minors where required.

If dating/matchmaking is incidental, maintain effective age-gating to prevent minors from accessing that feature.

High-risk features:

- dating/matchmaking
- anonymous/random chat
- sexual or romantic AI companion features
- real-money games/contests/gambling
- adult UGC
- prediction markets with real-money transactions

## Accessibility QA

Minimum Android accessibility checks:

| Area | Pass condition |
| --- | --- |
| Touch targets | Important controls are about 48dp or larger |
| Text scaling | UI survives increased font size |
| Contrast | Text/icons readable in light and dark themes |
| TalkBack labels | Icon buttons and custom controls have labels |
| Focus order | TalkBack focus follows logical reading/action order |
| State announcement | toggles, selected tabs, errors, loading states are announced |
| Error handling | form errors are near fields and screen-reader discoverable |
| Motion | strong/repetitive motion has reduced-motion alternative |
| Color dependency | color is not the only state signal |

React Native specifics:

- use `accessibilityRole`
- use `accessibilityLabel` for icon-only buttons
- use `accessibilityState` for selected/disabled/checked/busy states
- avoid inaccessible custom controls when native controls would work
- test with Android TalkBack, not only iOS VoiceOver

## Store Listing Consistency

Check that:

- screenshots do not imply a younger audience than selected
- description does not include child-directed language if children are excluded
- content rating matches AI/UGC/social realities
- privacy policy explains children/minor handling where relevant
- ads and tracking declarations align with audience

## Blockers

| Blocker | Fix |
| --- | --- |
| Target audience excludes children but app is clearly child-directed | Reassess audience or redesign listing/product |
| AI/UGC can show mature content to minors | Add age gate, filters, moderation, and rating update |
| Dating/matchmaking feature accessible to minors | Use required Play Console minor-blocking or effective feature-level age gate |
| Icon buttons lack labels | Add `accessibilityLabel` and role/state |
| Text breaks under large font scale | Redesign layout for dynamic type |
| Ads SDK not suitable for children when children included | Replace or restrict ad serving appropriately |

## Output Format

### Accessibility/audience verdict

`Ready` / `Not ready` / `Unknown`

### Audience/rating status

| Area | Status | Notes |
| --- | --- | --- |
| Content rating | unknown | Questionnaire answers not provided |
| Target audience | warn | Teen appeal but adult-only selected |
| Accessibility | block | Icon-only controls unlabeled |

### Required fixes

| Fix | Where | Verification | Owner |
| --- | --- | --- | --- |
| Add TalkBack labels to bottom nav icons | React Native components | TalkBack announces each destination | Engineering |
