// Package files provides TOCTOU-safe file reading with path validation.
//
// The SafeReader eliminates time-of-check to time-of-use (TOCTOU) race conditions
// by opening files immediately after validation and reading from the open descriptor.
// This prevents attacks where a symlink is swapped between validation and read.
//
// Pattern:
//  1. Validate path against workspace/allowed roots
//  2. Open file immediately (no race window)
//  3. Stat from the open descriptor
//  4. Re-validate resolved symlink path
//  5. Read from the open descriptor
package files
