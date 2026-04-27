---
name: apple-testflight-release-readiness
description: "Review TestFlight build health, crashes, feedback, tester coverage, What to Test notes, and release confidence."
---

# Apple TestFlight and Release Health Readiness

Use this skill to decide whether the current build has enough real-world evidence to submit.

## Core Rule

Do not submit a build simply because it uploaded. Submit the build that survived the flows reviewers and early users will actually exercise.

## Progressive Flow

### Level 0 — Release health verdict

Report:

- **Healthy enough to submit**
- **Not healthy enough**
- **Insufficient beta evidence**
- **Needs crash/feedback triage**

### Level 1 — Evidence table

| Evidence | Status | Notes | Next step |
| --- | --- | --- | --- |
| Internal TestFlight | Done/Missing | Devices tested | Expand coverage |
| External TestFlight | Done/Missing | Review state | Add external beta |
| Crash reports | Clean/Issues/Unknown | Top signature | Triage |
| Feedback | Clean/Issues/Unknown | Top complaint | Fix or document |
| Performance | Clean/Issues/Unknown | Hangs/launch | Investigate |

## Required Checks

### 1. Build selection

Verify the build is:

- latest intended release candidate
- processed and valid
- attached to correct version
- not superseded by a newer untested build
- built from the intended branch/tag/commit
- using production/review backend configuration

### 2. Minimum beta coverage

For small apps, at minimum test:

- fresh install
- update from previous App Store version
- onboarding
- login/logout
- purchase/restore if applicable
- AI/core network flow if applicable
- denied permissions
- offline or degraded network behavior
- support/privacy/account deletion path
- push notification path if relevant
- background/foreground behavior

For complex apps, include role-based and device-matrix testing.

### 3. Crash triage

Before submission:

- fetch recent TestFlight crash reports
- group by signature
- identify affected builds and devices
- check whether top crash hits launch, onboarding, purchase, or core feature
- decide fix/accept/defer explicitly

Any crash in launch, onboarding, login, paywall, purchase, or core app flow is a submission blocker unless proven unrelated to the current build.

Use `asc-crash-triage`.

### 4. Feedback triage

Review beta feedback for:

- reviewer-access blockers
- confusing onboarding
- broken paywall
- inaccurate AI output claims
- privacy/consent confusion
- device-specific layout bugs
- screenshot evidence of UI breakage
- repeated complaints about a single core feature

Do not ignore “minor” feedback when it maps to App Review guidelines.

### 5. Performance diagnostics

Check where available:

- hangs
- excessive disk writes
- launch diagnostics
- memory pressure symptoms
- repeated ANR-like user reports

Long hangs in first-run, sign-in, purchase, model loading, or import flows are high risk.

### 6. What to Test notes

Good TestFlight notes should tell testers what changed and what to verify.

Good:

```text
Please test:
- New AI document import flow from Files.
- Subscription restore on Settings → Plan.
- Offline handling when opening a saved project.
Known issue: first import may take up to 20 seconds for large PDFs.
```

Bad:

```text
Bug fixes.
```

### 7. External beta review

If using external testers:

- beta app review info is complete
- demo account available for beta review
- export compliance answered
- What to Test notes present
- build includes enough functionality for public-distribution intent

TestFlight is not for compensating testers or distributing permanent beta/demo apps.

## Output Format

```md
## Release health verdict
Healthy / Not healthy / Insufficient evidence / Needs triage

## Evidence summary
| Area | Status | Evidence | Risk |
| --- | --- | --- | --- |

## Top crash/feedback issues
| Issue | Affected build | Severity | Decision |
| --- | --- | --- | --- |

## Required tests before submission
- [ ] ...

## ASC automation
Use `asc-testflight-orchestration`, `asc-crash-triage`, `asc-build-lifecycle`, and `asc-submission-health`.
```
