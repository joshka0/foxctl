// Package orchestration defines the v2 long-lived scheduler and reconcile runtime.
//
// Package-topology note:
// orchestration is one of the explicit internal/v2/runtime/* replacement seams
// for legacy agent/runtime control. It should stay scoped to the newer
// runtime/orchestration lane, not grow into a generic future namespace.
package orchestration
