// Package agentmanager provides the legacy agent lifecycle manager.
//
// Package-topology note:
// agentmanager remains a fallback runtime-management surface while spawn/list/
// run/kill semantics move into internal/v2/services/*. New package placement
// should follow docs/architecture/package-topology.md instead of treating this
// package or internal/v2 as the default home for unrelated work.
package agentmanager
