// Package codexjsonl provides types and parsing for Codex CLI JSONL session files.
//
// Codex stores conversation history in JSONL files located at:
// ~/.codex/sessions/YYYY/MM/DD/rollout-<timestamp>-<session-id>.jsonl
//
// Each line contains a JSON object with a type field and nested payloads.
package codexjsonl
