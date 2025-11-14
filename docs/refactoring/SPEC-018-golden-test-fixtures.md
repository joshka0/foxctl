# SPEC-018: Golden Test Fixtures

## Status
**Not Started** | Priority: Medium | Complexity: Low

## Problem Statement

The `test/golden/` directory is a `.keep` placeholder. Need comprehensive golden fixtures for:
- Envelope formats (ok, error, all error codes)
- OpenAPI responses (inline, CAS-wrapped, pagination)
- Skill outputs (fs/ls, fs/read, text/grep, todo/manage)

## Proposed Solution

### Structure

```
test/golden/
├── envelopes/
│   ├── ok-simple.json
│   ├── ok-cas-wrapper.json
│   ├── error-EARG.json
│   ├── error-ERUNTIME.json
│   ├── progress-stream.ndjson
│   └── ...
├── openapi/
│   ├── response-inline.json
│   ├── response-cas.json
│   ├── response-paginated.json
│   ├── error-4xx.json
│   └── ...
└── skills/
    ├── fs-ls-output.json
    ├── fs-read-preview.json
    ├── text-grep-matches.json
    └── ...
```

### Testing Pattern

```go
func TestEnvelopeFormat(t *testing.T) {
    got := generateEnvelope()
    goldenFile := "testdata/golden/envelopes/ok-simple.json"

    if *update {
        os.WriteFile(goldenFile, got, 0644)
    }

    want, _ := os.ReadFile(goldenFile)
    assert.JSONEq(t, string(want), string(got))
}
```

## Implementation Plan

1. **Create directory structure** (30min)
2. **Generate envelope fixtures** (2h)
3. **Generate OpenAPI fixtures** (2h)
4. **Generate skill fixtures** (2h)
5. **Add golden tests** (2h)

## Effort Estimate
**Total: 8 hours**
