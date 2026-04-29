// Package adapters provides bidirectional conversions from existing domain types
// to canonical contextengine types. Each source subsystem gets its own adapter file.
//
// Invariants:
//   - No import cycles: adapters import both contextengine and source packages,
//     but contextengine never imports adapters.
//   - No keyword heuristics: classification uses typed fields and explicit mappings,
//     never strings.Contains for routing or signal detection.
//   - All conversion outputs pass Validate().
//   - Round-trip fidelity: converting source→canonical→checking preserves all significant fields.
package adapters
