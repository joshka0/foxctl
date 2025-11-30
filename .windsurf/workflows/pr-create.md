---
description: PR Creation Workflow
---

## PR CREATION WORKFLOW — EXACT STEPS, NO SHORTCUTS EVER

Every single PR must be created **exactly** like this. If you deviate, the PR
is auto-blocked by the AUTO-REJECT rules.

### The 7 Sacred Steps (run in this order, every time)

1. **Work is covered by an Approved spec (or is truly tiny)**

   - There must be a spec at `docs/spec/<YYYY-MM-DD_name>.md` with status
     **Approved**.
   - For tiny, obviously-safe changes (e.g., typo fix) you may skip the spec,
     but still follow the rest of this workflow.

2. **Create a dedicated branch from `main`**

   ```bash
   git checkout main && git pull --ff-only
   git checkout -b <type>/<feature>/<phase>   # e.g. feat/review-gate/1 or fix/login-race
   ```

3. **Do the work + checkpoint commits**

   - Make small, reviewable commits as you work.
   - Every commit title **must** follow:

     ```text
     checkpoint(<feature>): <what changed>
     ```

     Example:

     ```text
     checkpoint(review-gate/1): wire todo review_request and review_status
     ```

4. **Run full local validation — everything must be green**

   These are the canonical local checks (see `docs/start/testing_and_ci.md`):

   // turbo
   ```bash
   make check      # fmt + lint + vet + test + coverage + build
   make test-race  # race tests (excludes internal/storage/vector by default)
   ```

   - Fix **all** lint/type/test failures before proceeding.

5. **Prepare to fill the PR template EXACTLY**

   - Open `.github/pull_request_template.md` locally as your source of truth.
   - You will paste/fill this content in the GitHub UI **immediately after**
     running `gh pr create`.

6. **Create the PR via `gh pr create` (body in UI, not CLI)**

   Use a phase-prefixed title that matches your implementation plan:

   ```bash
   gh pr create \
     --base main \
     --head <type>/<feature>/<phase> \
     --title "feat(<feature>/<phase>): <short summary>"
   ```

   Examples:

   - `feat(review-gate/1): todo review gate + CAS artifacts`
   - `fix(openapi/2): handle 429 rate limit envelopes`

   After the PR is created, GitHub will open it in the browser. In the PR
   description:

   - Ensure the body matches `.github/pull_request_template.md`.
   - Fill in:
     - `## Spec` → correct `docs/spec/<YYYY-MM-DD_name>.md`.
     - `## Phase` → e.g. `Phase 1 of 3 — review gate operations`.
   - Check every box in the Gold Standard checklist only when it is truly met.
   - Complete the completeness matrix, rollback plan, and (for bugfixes) the
     root cause section.

7. **Immediately request human review (never self-merge)**

   - Assign at least one human reviewer.
   - Do not self-merge; wait for CI + review to be green and approved.

---

### Notes

- This workflow is Go/agentctl-specific and assumes the `Makefile` targets
  defined in the repo.
- If you need to adjust the required checks or branching scheme, update this
  file in the same PR where you change the rules, and call that out explicitly
  in the PR description.
