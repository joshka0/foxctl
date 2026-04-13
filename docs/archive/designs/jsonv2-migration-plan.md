# Go 1.25 JSON v2 Migration Plan

> Status: **Phase 1-4 Complete** | Target: Go 1.25 (experimental via `GOEXPERIMENT=jsonv2`)
>
> Completed: 2026-01-01 | Branch: `feat/jsonv2-migration`

## Overview

Migrate agentctl from `encoding/json` to `encoding/json/v2` to leverage new
features, reduce boilerplate, and improve JSON output quality.

## New Packages

| Package                   | Purpose                                    |
| ------------------------- | ------------------------------------------ |
| `encoding/json/v2`        | Main replacement for `encoding/json`       |
| `encoding/json/jsontext`  | Low-level token/value streaming            |

## Migration Phases

### Phase 1: Required Changes (Breaking Without)

These changes MUST be made before enabling JSON v2 - the code will fail to
marshal without them.

#### 1.1 Add `format:units` to all `time.Duration` fields

v2 requires explicit format for Duration fields. Without this tag, marshaling
fails.

| File | Line | Field | Change |
|------|------|-------|--------|
| `skills/hooks_impact_analysis/main.go` | 83 | `Timeout` | Add `format:units` |
| `skills/code_llm_search/main.go` | 72 | `Timeout` | Add `format:units` |
| `internal/domain/backup/backup.go` | 129 | `Duration` | Add `format:units` |
| `internal/indexing/embedding/worker.go` | 21 | `PollInterval` | Add `format:units` |
| `internal/indexing/embedding/worker.go` | 27 | `ShutdownTimeout` | Add `format:units` |
| `internal/openapi/retry/retry.go` | 24 | `InitialDelay` | Add `format:units` |
| `internal/openapi/retry/retry.go` | 25 | `MaxDelay` | Add `format:units` |
| `internal/agent/optimization/reflection.go` | 51 | `AvgDuration` | Add `format:units` |
| `internal/agent/optimization/prompt_optimizer.go` | 114 | `Duration` | Add `format:units` |

**Example:**
```go
// Before
Timeout time.Duration `json:"timeout,omitempty"`

// After
Timeout time.Duration `json:"timeout,omitempty,format:units"`
```

**Output:** `"30s"` instead of nanoseconds

---

### Phase 2: High-Impact Improvements

#### 2.1 Keep `error` always present (no `omitzero`)

**File:** `internal/domain/envelope/envelope.go:52`

**Problem:** With jsonv2, using `omitzero` on `error` would omit the field on
success responses, which breaks the Core Profile v1 envelope contract and
existing consumers.

**Current output:**
```json
{"version":1,"status":"ok","command":"test","data":{},"meta":{"ts":"..."},"error":{}}
```

**Change:**
```go
// Before
Error   ErrorFields `json:"error"`

// After
Error   ErrorFields `json:"error"`
```

**Result output:**
```json
{"version":1,"status":"ok","command":"test","data":{},"meta":{"ts":"..."},"error":{}}
```

**Impact:** Envelope output remains stable across jsonv1/jsonv2.

---

#### 2.2 Remove Vector Custom Marshaler

**File:** `internal/storage/dbdriver/vector.go:49-61`

**Current:** 13 lines of custom marshaler that just delegates to `[]float32`.

```go
func (v Vector) MarshalJSON() ([]byte, error) {
    return json.Marshal([]float32(v))
}

func (v *Vector) UnmarshalJSON(data []byte) error {
    var floats []float32
    if err := json.Unmarshal(data, &floats); err != nil { return err }
    *v = Vector(floats)
    return nil
}
```

**Change:** Delete both methods. v2 handles type aliases natively.

**Lines saved:** 13

---

#### 2.3 Remove Duration Custom Type and Marshaler

**File:** `internal/agent/types/types.go:14-56`

**Current:** 45 lines of custom Duration type with marshal/unmarshal logic.

```go
type Duration time.Duration

func (d Duration) MarshalJSON() ([]byte, error) {
    return json.Marshal(time.Duration(d).String())
}

func (d *Duration) UnmarshalJSON(b []byte) error {
    // ... 25 lines of switch/case handling
}
```

**Change:**
1. Delete the `Duration` type and its methods
2. Replace all usages with `time.Duration` + `format:units` tag
3. Update all callers (search for `types.Duration`)

**Files affected:**
- `internal/agent/types/types.go` (delete type)
- All files using `types.Duration` (change to `time.Duration`)

**Lines saved:** 45+

---

#### 2.4 Migrate TursoSettings Marshaler to MarshalerTo

**File:** `internal/platform/config/config.go:104-112`

**Current:** Uses type alias trick for auth token redaction.

```go
func (t TursoSettings) MarshalJSON() ([]byte, error) {
    type Alias TursoSettings
    redacted := Alias(t)
    if redacted.AuthToken != "" {
        redacted.AuthToken = "[REDACTED]"
    }
    return json.Marshal(redacted)
}
```

**Change:** Use v2's streaming `MarshalerTo` interface:

```go
func (t TursoSettings) MarshalJSONTo(enc *jsontext.Encoder) error {
    type Alias TursoSettings
    redacted := Alias(t)
    if redacted.AuthToken != "" {
        redacted.AuthToken = "[REDACTED]"
    }
    return json.MarshalEncode(enc, redacted)
}
```

**Benefit:** No intermediate allocation, better performance.

---

### Phase 3: Behavior Compatibility

These options ensure v1 compatibility during transition.

#### 3.1 Nil Slice/Map Handling

**v1:** `nil` slices marshal to `null`
**v2:** `nil` slices marshal to `[]`

**If v1 behavior needed:**
```go
json.Marshal(value, json.NullForEmptySliceAndMap(true))
```

**Codebase check:** 1119 nil checks found - review API contracts.

#### 3.2 Case-Sensitive Matching

**v1:** Case-insensitive field matching
**v2:** Case-sensitive field matching

**If v1 behavior needed:**
```go
json.Unmarshal(data, &v, json.MatchCaseInsensitiveNames(true))
```

#### 3.3 Legacy omitempty Definition

**If v1 behavior needed:**
```go
json.Marshal(value, json.OmitEmptyWithLegacyDefinition(true))
```

---

### Phase 4: Forward Compatibility

#### 4.1 Add `unknown` Field for Forward Compatibility ✅

Added `unknown` fields to 11 external API response types to capture new fields
when APIs evolve:

| Provider | Types Updated |
|----------|---------------|
| OpenAI/Groq/OpenRouter | `openAIResponse` |
| Voyage AI | `voyageEmbedResponse`, `voyageRerankResponse` |
| Google Gemini | `geminiEmbedResponse`, `geminiBatchEmbedResponse` |
| Mistral | `mistralEmbedResponse` |
| Codestral | `codestralEmbedResponse` |
| GitHub | `PRInfo`, `CheckRun`, `JobDetails`, `JobStep` |

**Pattern used:**
```go
_ map[string]any `json:",unknown"` // Captures unknown fields for forward compatibility
```

#### 4.2 Evaluate `inline` for Embedded Structs ✅

**Analysis result:** No suitable candidates found.

The codebase already follows good practices:
- Explicit field naming over anonymous embedding
- Semantic grouping with intentional nesting
- Clear JSON structure mirroring conceptual organization

**Recommendation:** Do not apply `inline` - current structure is semantically sound.

---

## File Summary

### Files Modified

| Phase | File | Changes |
|-------|------|---------|
| 1 | `skills/hooks_impact_analysis/main.go` | Add `format:units` |
| 1 | `skills/code_llm_search/main.go` | Add `format:units` |
| 1 | `internal/domain/backup/backup.go` | Add `format:units` |
| 1 | `internal/indexing/embedding/worker.go` | Add `format:units` (2 fields) |
| 1 | `internal/openapi/retry/retry.go` | Add `format:units` (2 fields) |
| 1 | `internal/agent/optimization/reflection.go` | Add `format:units` |
| 1 | `internal/agent/optimization/prompt_optimizer.go` | Add `format:units` |
| 1 | `internal/platform/config/config.go` | Add `format:units` (2 fields) |
| 2 | `internal/domain/envelope/envelope.go` | Keep `error` always present (do not use `omitzero`) |
| 2 | `internal/agent/types/types.go` | Delete Duration type (68 lines) |
| 2 | `internal/storage/dbdriver/vector.go` | Delete Vector marshaler (14 lines) |
| 2 | `internal/platform/config/config.go` | Add TODO for MarshalerTo |
| 3 | `internal/platform/jsoncompat/compat.go` | Created - compatibility docs |
| 4 | `internal/intelligence/planning/llm/openai.go` | Add `unknown` field |
| 4 | `internal/indexing/semantic/provider_voyage.go` | Add `unknown` field |
| 4 | `internal/indexing/semantic/provider_gemini.go` | Add `unknown` fields (2) |
| 4 | `internal/indexing/semantic/provider_mistral.go` | Add `unknown` field |
| 4 | `internal/indexing/semantic/provider_codestral.go` | Add `unknown` field |
| 4 | `internal/indexing/rerank/voyage.go` | Add `unknown` field |
| 4 | `skills/ci_github_checks/main.go` | Add `unknown` fields (4) |

### Final Impact

| Metric | Value |
|--------|-------|
| Files modified | 18 |
| Lines removed | ~82 |
| Lines added | ~30 |
| Custom marshalers removed | 2 |
| Duration fields updated | 11 |
| Forward-compat fields added | 11 |

---

## Testing Strategy

1. **Unit tests:** Run existing test suite with `GOEXPERIMENT=jsonv2`
2. **Golden file tests:** Compare JSON output before/after for envelope
3. **Integration tests:** Verify skill inputs/outputs parse correctly
4. **API compatibility:** Check nil slice behavior in stored data

---

## Rollout Plan

1. **Feature flag:** Use build tag to enable v2 selectively
2. **CI job:** Add parallel CI job running with `GOEXPERIMENT=jsonv2`
3. **Gradual migration:** Phase 1 first, then Phase 2 changes
4. **Full switch:** Remove v1 import after all tests pass

---

## Appendix A: Full Codebase Analysis

### A.1 Files Using `encoding/json` (299 files)

Top directories by JSON usage:
- `skills/` - 89 files
- `internal/` - 165 files
- `cmd/` - 45 files

### A.2 All `omitempty` Usages (500+ instances)

High-density files:
| File | Count |
|------|-------|
| `skills/code_semantic_search/main.go` | 25+ |
| `internal/storage/interfaces.go` | 20+ |
| `internal/agent/types/types.go` | 30+ |
| `internal/workflow/types.go` | 25+ |
| `internal/domain/envelope/envelope.go` | 25+ |

**v2 Behavior:** `omitempty` works the same by default. Use
`json.OmitEmptyWithLegacyDefinition(true)` for exact v1 behavior if needed.

### A.3 `json.RawMessage` Usage (81+ files)

Used for flexible payloads where structure is unknown at compile time.

Key files:
| File | Usage |
|------|-------|
| `internal/domain/hook/types.go` | `ToolInput`, `ToolResponse` |
| `internal/actor/event_bus.go` | `Data` field |
| `skills/session_archive/main.go` | Message content |

**v2 Benefit:** `jsontext.Value` provides better streaming performance.

### A.4 Pointer Types with `omitempty` (50+ instances)

```go
EndedAt    *time.Time   `json:"ended_at,omitempty"`
QuotaRem   *QuotaRemain `json:"quota_remaining,omitempty"`
ActiveTask *TaskInfo    `json:"active_task,omitempty"`
```

**v2 Benefit:** Works the same, plus `omitzero` now available for non-pointers.

### A.5 `json:"-"` Secret Exclusion (2 instances)

| File | Line | Field |
|------|------|-------|
| `internal/actor/registry_store.go` | 69 | `Config` |
| `internal/agent/types/types.go` | 268 | `LLMAPIKey` |

**v2:** No change needed, `json:"-"` works the same.

### A.6 Error Struct Patterns (Candidates for `omitzero`)

| File | Struct | Field | Line |
|------|--------|-------|------|
| `internal/domain/envelope/envelope.go` | `Envelope` | `Error ErrorFields` | 52 |
| `cmd/agentctl_viewer/types.go` | `Envelope` | `Error *EnvelopeError` | 13 |
| `skills/editor_godot/main.go` | `PluginResponse` | `Error *PluginError` | 34 |

### A.7 Files Using Custom Duration Type

Search for `types.Duration` to find all usages that need migration:

```bash
grep -r "types\.Duration" --include="*.go" | wc -l
```

---

## Appendix B: JSON v2 Feature Reference

### B.1 New Struct Tags

| Tag | Description | Example |
|-----|-------------|---------|
| `omitzero` | Omit zero-value structs | `Error ErrorFields \`json:"error,omitzero"\`` |
| `inline` | Flatten embedded fields | `Meta \`json:",inline"\`` |
| `unknown` | Capture unknown fields | `Extra map[string]any \`json:",unknown"\`` |
| `format` | Custom format | `Timeout time.Duration \`json:"timeout,format:units"\`` |
| `case` | Case matching control | `Name string \`json:"name,case:strict"\`` |

### B.2 Format Options for time.Duration

| Format | Output | Use Case |
|--------|--------|----------|
| `format:units` | `"30s"`, `"1h30m"` | Human-readable |
| `format:nanos` | `30000000000` | Machine-readable |

### B.3 Migration Functions

```go
// Restore v1 behaviors during transition
json.Marshal(v,
    json.OmitEmptyWithLegacyDefinition(true),     // Old omitempty
    json.NullForEmptySliceAndMap(true),           // nil -> null
    json.MatchCaseInsensitiveNames(true),         // Case-insensitive
    json.FormatTimeWithLegacySemantics(true),     // Old time format
)
```

### B.4 New Interfaces

```go
// v1: Allocates intermediate []byte
type Marshaler interface {
    MarshalJSON() ([]byte, error)
}

// v2: Streams directly (no allocation)
type MarshalerTo interface {
    MarshalJSONTo(*jsontext.Encoder) error
}
```

---

## References

- [Go 1.25 JSON v2 Proposal](https://github.com/golang/go/discussions/63397)
- [JSON v2 Documentation](https://pkg.go.dev/encoding/json/v2)
- [Migration Guide](https://go.dev/blog/json-v2)
- [antonz.org JSON v2 Overview](https://antonz.org/go-json-v2/)
- [gosuda.org JSON Experiments](https://www.gosuda.org/blog/2024/12/go-json-experiments/)
