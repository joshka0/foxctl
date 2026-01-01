// Package jsoncompat provides compatibility helpers for JSON v1 to v2 migration.
//
// These options can be used when v1 behavior is required during the transition period.
// See docs/designs/jsonv2-migration-plan.md for details.
package jsoncompat

// V1CompatOptions documents the marshal/unmarshal options available for v1 compatibility.
// These are not enforced globally but can be passed to individual Marshal/Unmarshal calls.
//
// Usage (when encoding/json/v2 is stable):
//
//	// Nil slices marshal to null (v1 behavior)
//	json.Marshal(value, json.NullForEmptySliceAndMap(true))
//
//	// Case-insensitive field matching (v1 behavior)
//	json.Unmarshal(data, &v, json.MatchCaseInsensitiveNames(true))
//
//	// Legacy omitempty definition (v1 behavior)
//	json.Marshal(value, json.OmitEmptyWithLegacyDefinition(true))
//
// Behavioral Differences v1 vs v2:
//
// 1. Nil Slice/Map Handling:
//   - v1: nil slices marshal to `null`
//   - v2: nil slices marshal to `[]`
//   - Most fields use `omitempty` which omits the field entirely
//
// 2. Case-Sensitive Matching:
//   - v1: Case-insensitive field matching
//   - v2: Case-sensitive field matching (use exact field names)
//
// 3. omitempty Definition:
//   - v1: Empty if zero value
//   - v2: Empty if zero value OR implements IsZero() returning true
//
// 4. format:units for Duration:
//   - v1: Duration marshals as nanoseconds (int64)
//   - v2: Duration with `format:units` marshals as "1h30m" strings
//
// Current codebase audit (see migration plan):
//   - 19 slice fields without omitempty (mostly internal types)
//   - External APIs (GitHub, LLMs) use consistent casing
//   - No known compatibility issues identified
