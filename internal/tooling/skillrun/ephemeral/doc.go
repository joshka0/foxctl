// Package ephemeral runs short-lived, attempt-scoped skill helpers.
//
// Ephemeral skills intentionally reuse the foxctl skill contract shape without
// installing a durable skill. They are meant for RLM/eval loops where a model
// synthesizes a small domain helper, the runtime validates it, runs it in a
// bounded interpreter, records the source/output, and discards it after the run.
package ephemeral
