# Phase 7 PR2: Trajectory capture hooks

## Summary

- Added best-effort trajectory capture hooks at existing envelope emission
  points for CLI runs, job results, and cache hit/miss paths.
- Introduced correlation propagation via `correlation_id` and `cli_command` in
  skill inputs, and ensured emitted envelopes are annotated with
  `meta.correlation_id` and `meta.job_id` where available.
- Persisted `Trajectory`, `UserRequestCapture`, and `TrajectoryEvent` records to
  the trajectory store with secrets redaction applied prior to persistence.
- Added unit tests for the `internal/trajectorycapture` package.
