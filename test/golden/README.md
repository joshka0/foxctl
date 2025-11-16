# Golden Fixtures (SPEC-018)

This directory contains canonical JSON envelopes used for SPEC-018, "Golden Test Fixtures".
They provide reference outputs for the core envelope format, built-in skills, and the
OpenAPI skill. Each fixture must remain stable to detect unintended wire-level changes.

## Layout

```
envelopes/  # Core envelope examples (success, errors, progress)
openapi/    # OpenAPI skill envelopes (inline, CAS, pagination, errors)
skills/     # Built-in skill envelopes (fs/ls, fs/read, text/grep, todo/manage)
```

## Updating fixtures

Fixtures should only be updated intentionally. After editing any fixture, run:

```
go test ./test/golden
```

The tests ensure every JSON envelope conforms to the Core Profile v1 invariants and that
progress streams remain valid NDJSON. When regenerating fixtures programmatically, prefer
to write through the protocol helpers so metadata (timestamps, CAS digests, etc.) stay
consistent with production envelopes.
