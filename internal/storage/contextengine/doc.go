// Package contextengine provides the durable store for the unified context engine.
//
// It defines an abstract Store interface and a SQLite-backed implementation
// supporting CRUD for all 9 entity types: context_events, evidence_packs,
// evidence_nodes, memory_claims, impact_edges, staleness_markers, projections,
// retrieval_episodes, and retrieval_feedback.
//
// Key design constraints:
//   - Append-only semantics for events, episodes, and feedback
//   - CAS integration for payloads >64KB
//   - Impact graph forward/reverse traversal
//   - Clock injection for deterministic tests
//   - No imports of domain packages (only internal/context/contextengine)
package contextengine
