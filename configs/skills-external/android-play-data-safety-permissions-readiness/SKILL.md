---
name: android-play-data-safety-permissions-readiness
description: "Review Play Data safety, privacy policy, account deletion, SDK collection, and Android permission disclosures."
---

# Android Play Data Safety & Permissions Readiness

This skill checks whether the app’s actual data behavior matches Google Play declarations, privacy policy, in-app disclosures, and Android permissions.

## Core Rule

The Data Safety form is not a marketing document. It must match the app, backend, SDKs, analytics, crash reporting, ads, AI providers, payments, and support tools.

## Required Data Inventory

Before answering Play Console forms, map:

| Source | What to inspect |
| --- | --- |
| App code | API calls, uploads, local storage, permissions, forms, account/profile data |
| Native config | AndroidManifest, Expo config plugins, permissions, services |
| SDKs | Analytics, crash reporting, ads, attribution, auth, payments, maps, AI, chat, support |
| Backend | Logs, prompts, uploads, IPs, user IDs, retention, admin tooling |
| AI systems | Prompts, generated outputs, files, images, voice, embeddings, moderation logs |
| Payments | purchase tokens, subscription state, entitlement identifiers |
| Support tools | tickets, chat transcripts, emails, diagnostics |

For every data type, record:

- collected or not
- shared or not
- purpose
- optional or required
- ephemeral or stored
- encrypted in transit
- user-deletable or not
- SDK/vendor involved
- whether it is personal/sensitive

## Privacy Policy Gate

All apps need a privacy policy in Play Console and in-app, even if they claim not to collect personal/sensitive data.

Privacy policy must be:

- publicly accessible
- active
- not geofenced
- not a PDF
- non-editable by users
- clearly labeled as privacy policy
- naming the developer or app consistently with the store listing
- including privacy contact or inquiry mechanism
- explaining access, collection, use, sharing, security, retention, and deletion

## Data Safety Gate

Check that the Data Safety form matches:

- privacy policy
- SDK behavior
- server behavior
- AI provider behavior
- analytics and crash tooling
- account deletion flow
- in-app disclosures and consent
- actual permissions requested in the build

Common problems:

| Problem | Why it blocks or risks enforcement |
| --- | --- |
| Privacy policy says no sharing but SDK sends analytics to third party | Data Safety mismatch |
| App sends prompts/files to AI backend but form omits this | User data mismatch |
| Crash logs include user IDs or prompt snippets | Hidden collection risk |
| App requests contacts but only needs one contact | Broad access risk |
| App asks for location before explaining why | Prominent disclosure/consent risk |
| App has account creation but no deletion route | Data deletion issue |
| SDK declaration copied blindly | Developer remains responsible for accuracy |

## Account and Data Deletion

If users can create accounts, verify:

- in-app account deletion path exists when required
- web deletion link exists where required
- deletion is discoverable
- deletion covers account and associated data, or exceptions are disclosed
- retention policy is explained
- paid subscription cancellation is not confused with account deletion
- support-only deletion routes are not unnecessarily burdensome

Good deletion copy:

```text
Delete account
This permanently deletes your profile, saved prompts, uploads, and history within 30 days. We may retain transaction records where legally required.
```

Bad deletion copy:

```text
Email support and maybe we’ll remove your account.
```

## Permissions Minimality

Every permission must support a current, user-facing core feature.

Review Android permissions in:

- `app.json` / `app.config.*` `android.permissions`
- Expo config plugins
- generated AndroidManifest
- library manifests merged into the final build
- Play Console permissions declarations

Remove unused permissions. Do not keep “future” permissions.

## Sensitive Permission Areas

### Contacts

For broad contacts access, verify that the app truly needs broad access. If the app only needs the user to select one or a few contacts, use Android Contact Picker or another narrower flow when possible.

Blockers:

- broad contacts permission for invite/referral/share-only flows
- uploading address books without clear user value
- contacts access not disclosed in privacy policy or Data Safety

### Location

Use the narrowest possible location access:

- approximate over precise when precise is unnecessary
- foreground over background when background is unnecessary
- one-time current location over continuous tracking when possible
- location button / user-initiated flows where appropriate
- Geofence API for geofencing instead of foreground-service misuse

Blockers:

- background location without core user-facing need
- precise location for vague personalization
- no in-app disclosure before collection
- location data used for ads/analytics without accurate declaration

### Photos / Videos / Files

Prefer system pickers and scoped access over broad storage permissions.

Blockers:

- broad photo/video/file permissions when picker access is sufficient
- reading media before user action
- uploading media to AI or cloud without disclosure

### Camera / Microphone

Blockers:

- requesting at startup instead of just-in-time
- unclear purpose
- background capture or recording-like behavior without strong justification
- voice/image AI features without disclosure of processing and retention

### Health Data

Use `android-play-sensitive-category-readiness` for full review.

Blockers:

- health permissions not essential to core functionality
- health data used for ads, employment/insurance eligibility, or social sharing without authorization
- no Health apps declaration where applicable

### Accessibility API

Use `android-play-sensitive-category-readiness` for full review.

Blockers:

- using Accessibility API for non-accessibility automation
- autonomously planning/executing actions in other apps
- changing settings or interacting with UI without user knowledge/consent

### Advertising ID

Verify:

- declaration is accurate
- ad SDKs are declared
- child-directed or Families constraints are honored
- user reset/limit behavior is respected

## Prominent Disclosure and Consent

Use in-app prominent disclosure before collecting sensitive data when required.

Good disclosure:

- appears before collection
- uses clear language
- names data collected
- explains feature purpose
- states sharing if any
- includes affirmative consent
- not hidden in privacy policy alone

Bad disclosure:

- buried in onboarding terms
- shown after permission prompt
- vague “we use data to improve experience”
- consent tied to unrelated functionality

## AI Data Safety Special Case

If user prompts, documents, images, voice, contacts, location, profile data, or chat history are sent to AI providers or model infrastructure, treat that as data collection/sharing/processing to assess.

Check:

- Is data sent to a third-party AI provider?
- Is data stored for abuse monitoring, model improvement, analytics, or debugging?
- Are outputs logged with user IDs?
- Are files stored after processing?
- Are embeddings created and retained?
- Can user delete prompts/history/uploads?
- Does privacy policy explain AI processing clearly?

## Output Format

### Data/privacy verdict

`Ready` / `Not ready` / `Unknown`

### Data map summary

| Data / Permission | Collected? | Shared? | Declared? | Risk |
| --- | --- | --- | --- | --- |
| Email | yes | no | yes | low |
| Prompts | yes | third-party AI | unknown | block |
| Contacts | permission requested | unknown | no | block |

### Required fixes

| Fix | Where | Verification | Owner |
| --- | --- | --- | --- |
| Remove broad contacts permission or justify core use | app config / native manifest | Final manifest no longer includes READ_CONTACTS | Engineering |
| Add AI prompt processing to privacy policy and Data Safety | Privacy policy + Play Console | Data Safety matches actual provider flow | Legal/Product |
