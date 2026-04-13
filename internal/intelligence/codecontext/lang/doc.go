// Package lang provides language detection and related utilities for code context.
// It re-exports functionality from internal/platform/fsutil to provide a consistent
// interface within the codecontext package hierarchy.
//
// Skills should use this package (or platform/fsutil directly) rather than
// implementing their own language detection.
package lang
