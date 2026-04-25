---
name: foxctl-dev
description: "Developer loop for inspecting diffs, running the right tests, checking CI status, and keeping changes verifiable."
---

## What I do
- Provide a tight loop: inspect changes → run the right checks → summarize failures.

## When to use me
- You changed code and need confidence before shipping.
- You need CI status or PR feedback triage.

## Common commands

### Git status + diffs
```bash
foxctl run git/status --input '{}'
foxctl run code/diff --input '{"staged": true}'
foxctl run code/diff --input '{"staged": false}'
```

### Tests
```bash
foxctl run test/run --input '{"path": ".", "coverage": false, "race": false, "verbose": false}'
```

### CI (GitHub)
```bash
foxctl run ci/checks --input '{"owner": "<owner>", "repo": "<repo>", "ref": "<sha-or-branch>"}'
foxctl run ci/prcomments --input '{"owner": "<owner>", "repo": "<repo>", "pr": 123}'
```

### CoVe Verification (Chain-of-Verification)
```bash
# Verify a draft answer (extracts claims → parallel verification → refined answer)
foxctl run verification/cove_verify --input '{"question": "Is this fix correct?", "baseline": "<draft answer with claims>"}'

# Gate a TODO against Definition of Done (strict STATUS/BLOCKERS/EVIDENCE output)
foxctl run verification/cove_verify --input '{"question": "Is this TODO done?", "baseline": "<TODO + DoD + evidence + claims>", "mode": "gate", "max_verifiers": 6}'

# Verify claims only (no refinement)
foxctl run verification/cove_verify --input '{"question": "Are these claims correct?", "baseline": "<claims>", "skip_refine": true}'
```

## Operating rules
- Start narrow (closest tests) and broaden only if needed.
- If a check fails, either fix now (quick) or record a follow-up with the error text.
