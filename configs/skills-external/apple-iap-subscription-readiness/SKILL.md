---
name: apple-iap-subscription-readiness
description: "Review Apple in-app purchase and subscription setup, paywalls, StoreKit flows, RevenueCat, pricing, and restore behavior."
---

# Apple IAP and Subscription Readiness

Use this skill to check whether Apple can review purchases and whether users are given clear, compliant purchase information.

## Core Rule

Digital goods and premium digital features generally need Apple In-App Purchase. Products must be complete, visible to reviewers, functional in the submitted build, and accurately disclosed before purchase.

## Progressive Flow

### Level 0 — Purchase readiness verdict

Report:

- **Ready** — products configured, testable, and disclosed.
- **Blocked** — products missing/incomplete or app cannot be reviewed.
- **Needs manual App Store Connect action** — first-review attachment or product selection required.
- **Needs catalog reconciliation** — backend/RevenueCat/StoreKit mismatch.

### Level 1 — Product table

| Product | Type | Status | Risk | Next step |
| --- | --- | --- | --- | --- |
| pro_monthly | Subscription | Ready | Low | Test restore |
| credits_100 | Consumable | Missing screenshot | High | Upload review screenshot |

## Required Checks

### 1. Correct purchase mechanism

Confirm whether the app sells:

- digital content
- AI credits
- premium app features
- creator content
- subscriptions
- consumable usage limits
- non-consumable unlocks

If yes, Apple IAP is likely required unless a specific guideline exception applies.

Do not use external purchase links, Stripe, web checkout, or unlock codes for digital goods unless the app is in a permitted category/region/entitlement situation and the implementation has been reviewed carefully.

### 2. Product configuration

For every product:

- product ID exists
- type is correct
- display name and description are localized
- price is set
- availability is set
- review screenshot is present where required
- status is review-ready
- product appears in the app
- product is not hidden behind unreachable backend state

### 3. Subscriptions

For auto-renewable subscriptions:

- subscription group exists
- subscription levels are ordered correctly
- upgrade/downgrade behavior is coherent
- duration and price are clear before purchase
- trial/intro offer copy matches App Store Connect configuration
- subscription offers do not mislead
- restore purchases exists
- manage subscription path exists where appropriate
- privacy policy URL is populated

Paywall copy should clearly state:

- what the user receives
- price and billing period
- renewal behavior
- cancellation path
- trial duration, if any
- what remains free, if applicable

### 4. In-app purchase review artifacts

Check:

- review screenshots for each IAP/subscription where required
- promotional images where required
- product metadata suitable for public audience
- product names do not include misleading prices or claims
- products shown in screenshots/previews are actually available

### 5. First-review behavior

First submission can require special handling:

- first IAPs may need to be selected with the app version in App Store Connect
- subscriptions may need first-review attachment flows
- product status alone is not enough if not associated with the app version review

Use ASC skills, but treat first-review product inclusion as a manual-verification area when public API support is incomplete.

### 6. Sandbox and TestFlight testing

Test:

- fresh purchase
- canceled purchase
- interrupted purchase
- restore purchases
- expired subscription
- upgrade/downgrade
- family sharing if enabled
- unavailable product fallback
- offline purchase screen behavior
- backend receipt validation failure
- RevenueCat entitlement sync, if used

### 7. RevenueCat or backend catalog reconciliation

If using RevenueCat or another backend:

- ASC products exist and match IDs
- RevenueCat products map to correct entitlements
- offerings/packages reference live products
- paywall fetch failure has fallback
- entitlement state matches App Store receipt
- webhook/server notification path is live

Use `asc-revenuecat-catalog-sync` where available.

### 8. AI credits and usage plans

For AI apps:

- credits used for digital AI generation are digital goods
- subscriptions unlocking AI features should use IAP unless a clear exception applies
- paywall must not imply unlimited service if there are fair-use limits
- if outputs can fail or be moderated, explain credit/refund behavior clearly
- do not block account deletion or privacy controls behind subscription

## Output Format

```md
## Purchase verdict
Ready / Blocked / Needs manual ASC action / Needs catalog reconciliation

## Product readiness
| Product | Type | Status | Issue | Fix |
| --- | --- | --- | --- | --- |

## Paywall disclosure review
| Requirement | Present? | Notes |
| --- | --- | --- |

## Sandbox test plan
- [ ] Fresh purchase
- [ ] Restore purchase
- [ ] Expiration/renewal
- [ ] Upgrade/downgrade
- [ ] Backend validation failure

## ASC automation
Use `asc-submission-health`, `asc-release-flow`, `asc-subscription-localization`, and `asc-revenuecat-catalog-sync` as relevant.
```
