// Package claudejsonl provides types and parsing for Claude Code JSONL sessions.
//
// Claude Code stores conversation history in JSONL files at:
// ~/.claude/projects/<workspace-hash>/<session-id>.jsonl
//
// Each line contains a JSON message with nested content structures.
package claudejsonl
