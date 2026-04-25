---
name: apple-asc-cli-automation-bridge
description: "Map Apple release blockers into concrete ASC CLI, signing, metadata, TestFlight, submission, and RevenueCat actions."
---

# Apple ASC CLI Automation Bridge

Use this skill to translate production-readiness findings into App Store Connect automation using the `asc` CLI and the `rorkai/app-store-connect-cli-skills` pack.

This skill is a bridge. It should not duplicate every command from the ASC skill pack. It chooses the right skill and preserves release safety.

## Core Rule

Policy and readiness judgment come first. Automation comes second. Do not submit, mutate pricing, delete builds, or change public metadata without explicit user confirmation.

## Skill Map

| Need | Use ASC skill |
| --- | --- |
| Find correct command/flags | `asc-cli-usage` |
| Resolve app/build/version/tester IDs | `asc-id-resolver` |
| Configure signing/capabilities/profiles | `asc-signing-setup` |
| Build/archive/export/upload | `asc-xcode-build` |
| Build processing/latest build/cleanup | `asc-build-lifecycle` |
| Metadata and localizations | `asc-metadata-sync` |
| Translate metadata | `asc-localize-metadata` |
| ASO audit | `asc-aso-audit` |
| Release notes | `asc-whats-new-writer` |
| Screenshots/framing/upload | `asc-shots-pipeline` |
| TestFlight groups/testers/notes | `asc-testflight-orchestration` |
| Crash/feedback/performance diagnostics | `asc-crash-triage` |
| Preflight/submission monitoring | `asc-submission-health` |
| End-to-end release readiness/staging | `asc-release-flow` |
| Subscriptions and IAP validation | `asc-submission-health`, `asc-release-flow`, `asc-subscription-localization` |
| RevenueCat catalog reconciliation | `asc-revenuecat-catalog-sync` |
| Mac notarization | `asc-notarization` |

## Progressive Automation Flow

### Level 0 — Read-only evidence first

Prefer non-mutating commands first:

- list
- view
- info
- validate
- preflight
- dry-run
- plan

Examples:

```bash
asc submit preflight --app "APP_ID" --version "1.2.3" --platform IOS
asc validate --app "APP_ID" --version "1.2.3" --platform IOS --output table
asc builds info --app "APP_ID" --latest --platform IOS
```

### Level 1 — Stage with explicit checkpoint

Use staging/dry-run flows before submission:

```bash
asc release run \
  --app "APP_ID" \
  --version "1.2.3" \
  --build "BUILD_ID" \
  --metadata-dir "./metadata/version/1.2.3" \
  --dry-run \
  --output table
```

Only proceed to mutation after the user approves the plan.

### Level 2 — Mutating actions require confirmation

Examples requiring explicit confirmation:

- upload build
- create/edit app availability
- push metadata
- upload screenshots
- change pricing
- create/delete testers or groups at scale
- attach build
- create review submission
- submit for review
- cancel submission
- expire builds
- change IAP/subscription catalog
- publish App Privacy via web-session flows

Use `--confirm` only after user approval.

### Level 3 — Submission

Only submit when:

- production checklist verdict is ready or accepted-risk
- ASC preflight is clean or warnings are understood
- build is valid and attached
- metadata/screenshots/localizations are complete
- App Review packet is complete
- AI/privacy consent and App Privacy are aligned
- IAP/subscriptions are review-ready
- manual verifications have owners

## Safety Rules

### Experimental web-session commands

Some ASC flows use web-session automation because public APIs cannot fully perform the task.

Rules:

- Present web-session flows as optional escape hatches.
- Use a user-owned Apple Account session.
- Explain what will be changed.
- Provide manual App Store Connect fallback.
- Never hide that a web-session command is experimental.

### App Privacy

Public API may not fully verify App Privacy publish state.

If ASC surfaces an App Privacy advisory:

- pull/plan/apply/publish only if user accepts web-session flow
- otherwise provide manual App Store Connect path
- mark as manual verification required

### First-time releases

First-time releases often have manual or semi-manual blockers:

- app availability bootstrap
- first-review subscription attachment
- first IAP selection with app version
- App Privacy publish state
- Game Center components added to same review submission

Use `asc-release-flow` to bucket blockers into:

- API-fixable
- web-session-fixable
- manual fallback

## Output Format

```md
## Automation plan
| Step | Command/Skill | Mutates? | Why |
| --- | --- | --- | --- |

## Read-only commands to run first
```bash
...
```

## Approval required before mutation
- [ ] Push metadata
- [ ] Upload screenshots
- [ ] Attach build
- [ ] Submit for review

## Manual verification
| Item | Reason | Link/path |
| --- | --- | --- |
```

## Agent Behavior

- Always check current `asc --help` or subcommand `--help` when flags may have changed.
- Prefer JSON output for machine parsing and table/markdown for human summaries.
- Use explicit long flags.
- Use `--paginate` when complete results matter.
- Use IDs for deterministic operations once resolved.
- Never claim ASC validation guarantees App Review approval.
- Never run destructive or submitting commands without explicit user confirmation.
