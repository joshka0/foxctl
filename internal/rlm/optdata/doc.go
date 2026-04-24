// Package optdata defines optimizer-ready RLM trajectory records.
//
// The package is intentionally storage-light: it writes append-only JSONL rows
// that can later be consumed by GEPA, DSPy, or foxctl's native prompt optimizer.
package optdata
