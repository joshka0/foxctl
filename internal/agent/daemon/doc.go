// Package daemon runs the classic long-lived agent daemon loop.
//
// Package-topology note:
// this package is transitional runtime infrastructure. New typed runtime
// orchestration belongs in internal/v2/runtime/* and internal/v2/services/*,
// while non-runtime families should follow the broader internal/* family model
// in docs/architecture/package-topology.md.
package daemon
