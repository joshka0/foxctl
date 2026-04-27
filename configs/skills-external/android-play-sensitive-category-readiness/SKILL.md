---
name: android-play-sensitive-category-readiness
description: "Review Google Play declarations and policy risks for regulated Android categories, sensitive permissions, and restricted APIs."
---

# Android Play Sensitive Category Readiness

Use this skill when the app touches a category that Google Play reviews with extra scrutiny.

## Core Rule

Sensitive-category apps need more than correct code. They need accurate declarations, lawful positioning, restricted permissions, clear disclosures, and often proof of eligibility.

## Sensitive Categories

Open this skill when the app includes any of these:

- health, wellness, medical, symptoms, diagnosis, treatment, mental health, therapy, fitness data, Health Connect
- finance, loans, credit, investing, crypto, taxes, insurance, payments
- government information or affiliation
- news or magazine content
- VPN or device/network proxying
- Accessibility API
- background location or foreground services
- SMS/call logs
- exact alarms
- contacts, photos/videos, broad file access
- dating/matchmaking, random chat, anonymous chat
- real-money gambling/games/contests, prediction markets
- children/family audience
- AI used in any high-stakes domain

## Health / Medical

Health apps must complete relevant Play declarations and avoid misleading/harmful health functionality.

Checklist:

- Health apps declaration completed if in scope
- privacy policy and in-app disclosure explain health data handling
- health permissions are essential to core functionality
- no unused health permissions
- medical-device status assessed
- regulatory proof available if app is a medical device
- non-medical-device disclaimer included where applicable
- users reminded to consult healthcare professionals where appropriate
- no misleading claims that contradict medical consensus or cause harm
- no sale/promotion of prohibited pharmaceuticals/supplements
- Health Connect data use follows allowed purposes and deletion expectations

AI health apps require extra caution:

- do not claim diagnosis/treatment unless lawfully supported
- constrain model outputs
- include escalation/referral disclaimers
- test harmful self-harm/medical prompts

## Financial / Loans / Insurance / Investments

Checklist:

- financial features declaration completed for every app on the account where required
- regulated services have proof/registration if required
- loan/credit terms are transparent
- no deceptive earning or investment claims
- no misleading AI financial advisor claims
- data collection is minimal and declared
- sensitive permissions are justified
- payments/in-app purchases comply with Play Billing and financial service rules

Hard blockers:

- short-term/personal loan features in unsupported regions
- misleading APR/fees/repayment terms
- claiming guaranteed returns
- collecting unnecessary contacts/SMS/call logs for credit decisions

## Government / Public Services

Checklist:

- government affiliation is truthful and provable
- if not official, listing clearly says so
- source of government information is disclosed
- app does not mimic official identity deceptively
- credentials or proof of authorization ready if required

## News and Magazines

Checklist:

- news declaration completed when in scope
- publisher identity and contact details clear
- sources and editorial responsibility clear
- AI-generated news content is labeled/controlled where relevant
- misinformation/deceptive content risks reviewed

## VPN / Device & Network

Checklist:

- VpnService use is core to app function
- traffic handling disclosed
- no ad fraud, data harvesting, or hidden proxying
- privacy policy accurately explains routing/logging
- no deceptive system-like behavior

## Accessibility API

Accessibility API must be for accessibility or a permitted user-benefiting use case, not covert automation.

Blockers:

- autonomously initiating, planning, or executing actions through Accessibility API
- changing settings without clear user consent
- interacting with other apps deceptively
- using Accessibility API for ad fraud, surveillance, scraping, or bypassing privacy controls
- no clear disclosure of what the service does

## Foreground Services / Background Location / Geofencing

Checklist:

- foreground service type matches actual use
- notification is clear and user-visible
- use case is approved and current
- geofencing uses Geofence API where appropriate
- background location is essential and disclosed
- location is not collected continuously when user-initiated access would suffice

## Dating / Matchmaking / Real-Money Gambling / Prediction Markets

Checklist:

- age-restricted functionality handled with Play Console tools where required
- minors blocked from required categories
- regional eligibility checked
- gambling/real-money/prediction market program enrollment verified where required
- UGC/chat/report/block safeguards exist

## Crypto / Blockchain

Checklist:

- country/region rules checked
- exchange/wallet claims accurate
- high-risk financial claims avoided
- custody, risk, fees, and provider identity clear
- prohibited tokenized assets/gambling patterns avoided

## Output Format

### Sensitive category verdict

`Not applicable` / `Ready` / `Not ready` / `Unknown`

### Category map

| Category | Applies? | Status | Notes |
| --- | --- | --- | --- |
| Health | yes | block | Health declaration missing |
| Accessibility API | no | pass | No service declared |
| Finance | maybe | unknown | Subscription only, no financial advice |

### Required fixes

| Fix | Category | Verification | Owner |
| --- | --- | --- | --- |
| Add non-medical-device disclaimer | Health | Store description and in-app onboarding show disclaimer | Legal/Product |
