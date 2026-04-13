// Package daemon provides the agentctl hosting daemon and client surfaces.
//
// Package-topology note:
// this package may host mixed legacy and newer wiring, but command semantics
// for the newer runtime should prefer internal/v2/services/*. Keep this package
// scoped to daemon hosting concerns rather than expanding it into a generic
// home for unrelated internal/* work.
package daemon
