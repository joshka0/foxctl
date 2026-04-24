// Package repl provides persistent Python/Yaegi-backed sandbox primitives for RLM.
//
// The current implementation intentionally focuses on process/session lifecycle,
// bounded IO capture, and timeout handling. It does not yet enforce OS-level
// network isolation or hard security sandboxing.
package repl
