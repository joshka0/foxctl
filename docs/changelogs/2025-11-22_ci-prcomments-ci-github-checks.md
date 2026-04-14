# Changelog: CI PR comments and checks skills

## Summary

- Added new `ci/prcomments` exec skill and `foxctl ci prcomments` CLI wrapper to summarize merge conflicts, CI failures, and review comments for a GitHub PR, with an `errors_only` mode and optional markdown export to `docs/prcomments/`.
- Added new `ci/github_checks` exec skill and `foxctl ci checks` CLI wrapper to summarize GitHub check runs for a PR, supporting `errors_only` filtering and `summary`/`detailed` modes.
- Documented both skills under `docs/ci/prcomments.md` and `docs/ci/github_checks.md`.
- Added basic Cobra tests for the new `ci` command and its flag surfaces.
