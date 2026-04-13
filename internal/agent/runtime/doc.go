// Package runtime provides the legacy mailbox-driven agent runtime.
//
// Package-topology note:
// runtime remains part of the older agent/runtime lane while command and
// orchestration replacements continue moving into internal/v2/runtime/*
// and internal/v2/services/*. New non-runtime families should not be routed
// here or into internal/v2 by default; follow docs/architecture/package-topology.md.
package runtime
