---
name: android-play-iap-subscriptions-readiness
description: "Review Google Play Billing, subscriptions, entitlements, pricing, trials, cancellation, and digital goods policy readiness."
---

# Android Play IAP & Subscriptions Readiness

Use this skill when the app sells anything, gates functionality, uses subscriptions, or manages paid entitlements.

## Core Rule

If a Play-distributed app sells digital goods or digital services that users can consume in the app, assume Google Play Billing is required unless a specific policy exception or regional alternative billing program applies.

Do not rely on web checkout, Stripe, crypto, external links, or manually granted paid access for in-app digital goods unless legal/policy has confirmed the allowed path.

## Product Classification

Classify every paid item:

| Product | Billing default |
| --- | --- |
| Premium app feature | Google Play Billing |
| AI credits consumed in app | Google Play Billing |
| Token pack for in-app generation | Google Play Billing |
| Subscription unlocking in-app features | Google Play Billing |
| Digital content consumed in app | Google Play Billing |
| Physical goods | External payment usually allowed |
| 1:1 live service meeting criteria | May not require Play Billing |
| Cloud platform management app with no in-app cloud consumption | May not require Play Billing |
| Donation/tip with no digital benefit | May not require Play Billing |

When unsure, treat as Play Billing required and verify policy.

## Billing Readiness Checklist

| Area | Pass condition |
| --- | --- |
| Products configured | Products/subscriptions exist in Play Console |
| Product IDs | App product IDs match Play Console exactly |
| Base plans/offers | Correct regions, prices, eligibility, trials |
| Paywall copy | Price, period, trial, renewal, cancellation clear |
| Entitlement backend | Server validates purchases and grants access |
| Restore/recover | User can recover entitlement across reinstall/device |
| Account handling | Subscription tied to correct app account where needed |
| Grace/hold/cancel | Lifecycle states handled correctly |
| Testing | License testers and Play test cards used |
| Review access | Reviewers can access paid features without real payment |
| Data safety | Purchase data declared if collected/shared |

## Subscription Disclosure

Before purchase, disclose material terms:

- product name
- price
- billing period
- trial length if any
- when trial converts to paid subscription
- auto-renewal behavior
- cancellation path via Google Play
- what features are included
- limits for AI credits/usage if applicable

Bad paywall:

```text
Go Pro — $9.99
```

Better:

```text
Pro Monthly — $9.99/month
Includes unlimited saved projects and 1,000 AI generations/month. Renews automatically unless canceled in Google Play before renewal.
```

## AI Credits and Usage-Based Features

AI apps often fail billing review because credits are vague.

Define:

- what one credit does
- whether credits expire
- whether generated content remains accessible after subscription cancellation
- what happens when model/provider fails
- whether credits are consumable or subscription allowance
- refund/support policy
- abuse limits

Avoid dark patterns:

- hiding recurring price
- implying “lifetime” when server limits exist
- making cancellation difficult
- showing model usage claims that can change without notice
- consuming credits for failed generations without clear policy

## Entitlement Verification

Use server-side verification for meaningful entitlements.

Verify:

- purchase token sent securely to backend
- backend validates with Google Play Developer API or trusted provider
- entitlement state is not based only on local client storage
- user ID and purchase account mapping are correct
- refund/revoke/cancel states are honored
- subscription renewal state is refreshed
- offline grace behavior is intentional

## RevenueCat / Third-Party Billing SDKs

If using RevenueCat or similar:

- products match Play Console
- offerings match app copy
- entitlements are tested on Android specifically
- Android package name and service credentials configured
- webhook/backend receives lifecycle events if used
- Data Safety includes SDK/vendor data handling

Do not assume iOS subscription behavior maps perfectly to Google Play.

## Review and Test Setup

Before production:

- add license testers in Play Console
- test purchase, cancel, restore, expiration, grace/hold if possible
- verify paywall in Play-delivered build, not just local dev
- verify reviewer account can access paid content
- include App access notes for paywalled areas

## Blockers

| Blocker | Fix |
| --- | --- |
| Digital goods sold through Stripe/web checkout in app | Move to Play Billing or verify valid exception |
| Paywall lacks recurring price/period | Add clear price, term, renewal, cancellation copy |
| Entitlement only stored locally | Add server-side validation or trusted entitlement service |
| Reviewers cannot access premium content | Provide reviewer entitlement or access instructions |
| Product IDs mismatch | Align app code, Play Console, and backend/RevenueCat |
| AI credits undefined | Define credit usage, expiration, failure/refund behavior |
| Subscription lifecycle not handled | Implement cancel, grace, hold, expiration, restore flows |

## Output Format

### Billing verdict

`Ready` / `Not ready` / `Unknown` / `Not applicable`

### Product map

| Product | Type | Billing path | Status |
| --- | --- | --- | --- |
| Pro Monthly | subscription | Play Billing | pass |
| 100 AI credits | consumable | Play Billing | warn: credit rules unclear |

### Required fixes

| Fix | Where | Verification | Owner |
| --- | --- | --- | --- |
| Add renewal/cancellation copy | Paywall | Screenshot shows price/period/auto-renewal | Product/Design |
