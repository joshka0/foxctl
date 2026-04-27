---
name: apple-submission-packet-readiness
description: "Prepare App Review notes, reviewer access, demo accounts, demo videos, attachments, and hardware instructions."
---

# Apple Submission Packet Readiness

Use this skill to prepare the material Apple reviewers need to evaluate the full app experience.

A clean submission packet reduces preventable 2.1 App Completeness issues: reviewer cannot sign in, cannot reach a feature, cannot reproduce a flow, lacks context, or sees a placeholder state.

## Core Rule

App Review must be able to reach every submitted feature without guessing.

If a reviewer needs special setup, credentials, role selection, seeded data, hardware, geography, subscription access, QR code, server state, or a particular prompt, provide it in the App Review Information notes and, when useful, an attachment or demo video.

## Progressive Flow

### Level 0 — Reviewer access verdict

Answer:

- Can Apple access the whole app?
- Can Apple verify paid features?
- Can Apple reproduce the main value path?
- Is a demo video advisable or necessary?

### Level 1 — Packet inventory

| Item | Needed? | Present? | Notes |
| --- | --- | --- | --- |
| Demo account | Yes/No | Yes/No | Role, credentials, 2FA |
| Demo mode | Yes/No | Yes/No | Full feature coverage |
| Demo video | Yes/No | Yes/No | Use when hard to reproduce |
| Review notes | Yes | Yes/No | Non-obvious features |
| Hardware/resources | Yes/No | Yes/No | Device, QR code, invite |
| Paid feature access | Yes/No | Yes/No | Sandbox, promo, entitlement |
| Support contact | Yes | Yes/No | Valid and monitored |
| Privacy policy URL | Yes | Yes/No | Works and matches app |

### Level 2 — Compose the packet

Write short, explicit review notes. Avoid marketing language.

## App Review Notes Template

```text
Hello App Review,

This version includes [one-sentence summary of the app and release].

Reviewer access:
- Demo account: [email]
- Password: [password]
- 2FA / verification code: [how to bypass or retrieve]
- Account role: [admin/member/creator/etc.]

Core flows to review:
1. [Launch -> sign in -> feature]
2. [Paid or gated flow]
3. [AI / hardware / external service flow]

Special configuration:
- [Backend environment is live]
- [Use this QR code / invite / sample file / seeded project]
- [Required device/hardware, if any]

AI and data sharing disclosure:
- [Where the app discloses third-party AI processing]
- [Where the user grants explicit consent]
- [How to test opt-out or deletion, if relevant]

Purchases:
- [IAP/subscription product names]
- [How to reach paywall]
- [How to restore purchases]

Attachments:
- [Demo video link or App Review attachment]
- [Regulatory, licensing, rights, or hardware documentation]

Support:
- Support URL: [URL]
- Privacy policy: [URL]
- Contact: [email/phone]
```

## Demo Video Guidance

A demo video is not a universal requirement, but it is strongly advisable when:

- the app requires hardware
- the app requires a hard-to-replicate environment
- the app depends on physical location or external services
- the app needs a specific account role or workflow
- the app uses AI behavior that is prompt-dependent
- the app has safety/moderation behavior Apple should see
- the app has an onboarding path reviewers may not naturally discover
- the app has a backend state that takes time to create

### Review-demo video script

Keep it private, functional, and short. Usually 2–5 minutes is enough.

```text
0:00 Launch and identify app/version
0:15 Sign in or demo mode
0:45 Show core user value path
1:30 Show paid/gated feature, if any
2:00 Show AI/data disclosure and consent, if relevant
2:45 Show safety/moderation/reporting, if relevant
3:15 Show account deletion/data deletion or support path, if relevant
3:45 Show hardware/external service flow, if relevant
4:30 End with note that backend and credentials are active
```

### Demo video rules

- Show the current submitted build, not a future mockup.
- Prefer a physical Apple device when hardware, sensors, pairing, HealthKit, Bluetooth, camera, AR, payments, or external device behavior matters.
- Use simulator video only for ordinary app flows where device behavior is irrelevant.
- Do not include real personal data, real customer accounts, or secrets.
- Avoid marketing edits that obscure the actual workflow.
- State if a feature requires purchase or subscription.
- Keep links and attachments accessible for the full review period.

## Demo Account Rules

- Use a stable account that will not expire during review.
- Disable forced password reset.
- Avoid 2FA; if unavoidable, explain the review path.
- Seed realistic data.
- Provide all relevant roles if the app is multi-role.
- Do not use production customer data.
- Ensure paid entitlements are active if needed.
- Ensure backend services are live and reachable from Apple networks.

## Built-in Demo Mode

Use a demo mode only when a demo account is impossible or unsafe.

Demo mode must:

- expose the app’s full features and functionality
- use realistic sample data
- clearly avoid real transactions unless sandboxed
- not hide paid, AI, or safety behavior from review
- be described in App Review Notes

## Non-obvious Feature Notes

Explain with specificity:

- background location, audio, Bluetooth, VPN, screen recording, notifications
- AI model use and data flow
- invisible moderation or safety systems
- creator/UGC moderation
- IAP/subscription behavior
- external account connection
- region-specific behavior
- regulated functionality

Generic notes such as “bug fixes” or “AI assistant added” are not enough for meaningful changes.

## Output Format

```md
## Reviewer access verdict
Ready / Needs work / Blocked

## Missing packet items
| Missing item | Risk | Fix |
| --- | --- | --- |

## Draft App Review Notes
[copy-ready notes]

## Demo video recommendation
Required / Strongly advisable / Optional / Not needed

## Verification
- [ ] Demo account login tested on a clean install
- [ ] Paid entitlement path tested
- [ ] Review notes match current build
- [ ] URLs open publicly
- [ ] Attachments are uploaded or linked
```
