---
name: android-play-testing-track-readiness
description: "Plan Google Play testing tracks, tester requirements, rollout strategy, production access, and promotion between tracks."
---

# Android Play Testing Track Readiness

Use this skill to choose and verify the correct Google Play track before production.

## Track Model

| Track | Purpose | Typical use |
| --- | --- | --- |
| Internal testing | Fast trusted testing | Smoke test, QA, first Play upload |
| Closed testing | Controlled broader testing | Production-access requirement, beta cohorts, policy validation |
| Open testing | Public test on Play | Broader feedback after listing is ready |
| Production | Public release | Staged or full rollout |

## New Personal Developer Account Gate

If the developer account is a newly created personal account, production access may require a closed test with at least 12 opted-in testers for 14 continuous days before applying for production access.

Check:

- account type: personal vs organization
- production access already granted or not
- closed test created
- at least 12 testers opted in
- 14 continuous days satisfied
- tester feedback collected
- production-access application answers prepared

Do not promise production release until production access is granted.

## Internal Testing Readiness

Use internal testing first when:

- app is new to Play
- signing or Play App Signing is unverified
- service account/EAS Submit is new
- app access instructions need validation
- QA needs Play-delivered build behavior

Checklist:

- AAB uploaded to internal track
- tester list/group configured
- opt-in link works
- app installs from Play
- reviewer/test account works
- Play-delivered SHA fingerprints registered if needed
- notifications/auth/deep links/payments work in Play-delivered build

## Closed Testing Readiness

Closed testing should produce evidence, not just satisfy a timer.

Checklist:

- testers match target device mix
- testers can opt in and install
- test plan exists
- feedback channel exists
- crash/ANR monitoring is enabled
- core flows are tested
- permissions prompts are reviewed
- AI/UGC/report flows are tested if relevant
- subscriptions or entitlements are tested if relevant

For new personal developer accounts, keep records of:

- tester count
- opt-in start date
- test plan
- issues found and fixed
- feedback summary
- production readiness improvements

## Open Testing Readiness

Only use open testing if:

- app and listing are public-ready
- support can handle public feedback
- no private/staging data is exposed
- app content declarations are accurate
- pricing/subscriptions are clearly communicated

## Production Track Readiness

Before production:

- all blockers cleared
- app content forms complete
- review access provided
- store listing complete
- release notes complete
- rollout strategy chosen
- support team ready
- monitoring dashboards ready
- rollback/rollout pause plan understood

## Staged Rollout Guidance

Use staged rollout for:

- meaningful feature releases
- AI/model changes
- auth changes
- billing changes
- onboarding changes
- permissions changes
- Android target SDK upgrades
- native module upgrades
- performance-sensitive updates

Full rollout is acceptable for:

- tiny fixes
- urgent low-risk corrections
- very small user base
- internal-only apps

Suggested rollout rhythm:

| Stage | Condition to continue |
| --- | --- |
| 5% | no crash/ANR spike, support quiet |
| 20% | core metrics stable |
| 50% | no policy, billing, auth, AI safety issues |
| 100% | release stable |

## Track and EAS Submit

EAS Submit can upload to a chosen Google Play track, but uploading to a track is not the same as completing review or production rollout.

Example:

```json
{
  "submit": {
    "production": {
      "android": {
        "track": "internal"
      }
    }
  }
}
```

Use explicit submit profiles for `internal`, `closed`, `open`, and `production` if the team releases often.

## Blockers

| Blocker | Fix |
| --- | --- |
| New personal account lacks 12 testers / 14-day closed test | Run qualifying closed test before production access |
| Opt-in link broken | Fix tester group/track setup |
| Production access not granted | Apply after closed-test criteria are met |
| No staged rollout plan for risky update | Define percentage rollout and monitoring gates |
| Internal build fails Play-delivered auth | Register app signing SHA fingerprints and retest |
| No feedback mechanism for closed test | Add tester instructions and feedback channel |

## Output Format

### Track verdict

`Ready for internal` / `Ready for closed` / `Ready for production` / `Not ready`

### Track status

| Track | Status | Notes |
| --- | --- | --- |
| Internal | pass | Install verified |
| Closed | block | Need 12 opted-in testers for 14 days |
| Production | blocked | Production access not granted |

### Next track action

| Action | Owner | Verification |
| --- | --- | --- |
| Create closed testing cohort and opt-in link | Release manager | 12 testers opted in continuously |
