---
name: semantic-commenting
description: "Write and review semantic code comments for retrieval: repoindex Index blocks and typed semantic anchors. Use when adding semantic anchors, semantic comments, Index: comment metadata, code-to-doc/test links, retrieval hints in comments, or deciding whether a comment should become an Index soft edge or a [[...]] semantic anchor."
---

# Semantic Commenting

Use this skill when a code change needs comments that future agents can retrieve
and trust at the right level. The goal is high-signal navigation, not comment
volume.

## Core Contract

There are two comment lanes:

1. `Index:` blocks create broad repoindex soft edges for discoverability.
2. `[[...]]` semantic anchors create typed, evidence-only semantic edges.

Do not collapse those lanes. Use `Index:` for keywords, related symbols, flow,
resources, events, and output fields. Use semantic anchors only for durable,
high-value contracts such as invariants, risks, protocols, decisions, domains,
and code-to-doc/test proof.

Semantic anchors are evidence. They may improve retrieval ranking and review
signals, but they must not be treated as instruction, policy, or durable
authority by themselves.

## When To Add Anchors

Add semantic anchors when all of these are true:

1. The owner code is a meaningful behavior boundary.
2. The anchor describes what the code actually enforces, protects, implements,
   participates in, or is verified/described by.
3. The target is stable enough that future agents should search for it.
4. The anchor can be placed near the strongest owner, not scattered everywhere.
5. The comment improves future retrieval more than plain code names already do.

Good owners include orchestrators, protocol boundaries, safety gates, authority
checks, storage transitions, concurrency guards, lifecycle components, public
API contracts, and tests that prove a behavior.

Skip anchors for leaf helpers, obvious wrappers, restated function names,
temporary TODOs, broad themes repeated across many files, generated/vendor code,
or claims not proven by the owner.

## Syntax

Use inline wikilink syntax in source-code comments:

Default repo-local examples:

```go
// [[invariant:no-send-without-read]]
// [[risk:agent-terminal-desync]]
// [[protocol:read-guard]]
// [[domain:agent-terminal-safety]]
// [[decision:evidence-only-anchors]]
// [[test-contract:read-before-write]]
// [[doc:docs/terminal-safety.md#Terminal Safety]]
// [[test:internal/terminal/guard_test.go#TestGuardReadBeforeWrite]]
```

Configured-scope example, only after confirming the repo/indexer defines
`acme` as a valid scope:

```go
// [[acme:protocol/read-guard]]
```

Rules:

- Use lowercase slugs: `a-z`, `0-9`, `.`, `_`, `-`, and `/`.
- Use repo-local concept anchors by default: `[[type:slug]]`.
- Use explicit scoped concept anchors, `[[scope:type/slug]]`, only when the
  target repo or indexer defines that scope and cross-repo disambiguation is
  needed. Discover the repo's scope from local tooling or configuration; do not
  hardcode one repository name, including `foxctl`, into portable semantic
  comments.
- Scoped anchors still use the same type/slug grammar: `[[scope:type/slug]]`.
  Example: `[[acme:protocol/read-guard]]`.
- `doc:` and `test:` anchors are repo-local path anchors and must be unscoped:
  use `[[doc:docs/foo.md#Heading]]`, not `[[project:doc/docs/foo.md]]`.
- Paths must be repo-relative. Do not use URLs, absolute paths, `..`, env vars,
  shell expansions, control characters, emails, tokens, or session-like IDs.
- Use `#Heading` for Markdown docs and `#TestName` for tests when a specific
  target exists.
- Avoid `beacon` anchors in ordinary code. They are reserved for broad retrieval
  experiments and should not be used as proof.

## Type Selection

Pick the narrowest true type:

| Type | Use For |
| --- | --- |
| `invariant` | A rule the owner must always enforce |
| `risk` | A concrete failure mode the owner protects against |
| `protocol` | A file or component implementing a named protocol |
| `domain` | A file or component participating in a stable domain concept |
| `decision` | Code embodying a durable design decision |
| `test-contract` | A named behavioral contract verified by tests |
| `doc` | A repo doc that describes the owner |
| `test` | A concrete test symbol or test file proving the owner |

Prefer one or two precise anchors over a cluster of vague ones. If a comment
needs many anchors, split the concept or choose the strongest two.

## Placement

Place anchors in source-code comments near the owner they describe:

- For symbol-owned anchors, put them in the doc comment immediately above the
  function, method, type, or constant.
- For file-scope anchors, put them in the package/file comment or a top-level
  comment that clearly belongs to the file/component.
- Keep anchors out of string literals, examples, arbitrary Markdown prose, and
  generated code.
- Keep anchors adjacent to the owner with no unrelated comment block between
  the anchors and the owner.

Preferred shape:

```go
// Guard enforces read-before-write terminal safety.
//
// Index:
//   Purpose: Protects terminal writes from stale unread output.
//   Keywords: terminal safety, read before write
//   Related: GuardHelper
//
// [[invariant:no-send-without-read]]
// [[risk:agent-terminal-desync]]
// [[doc:docs/terminal-safety.md#Terminal Safety]]
// [[test:internal/terminal/guard_test.go#TestGuardReadBeforeWrite]]
func Guard() {}
```

## Index Blocks

Use `Index:` when the goal is search and graph navigation rather than typed
evidence.

Structured form:

```go
// Index:
//   Purpose: Builds semantic anchor graph edges for indexed source comments.
//   Keywords: semantic anchors, repoindex, evidence edges
//   Related: applySemanticAnchorEdges, NewSemanticAnchorEdge
//   Flow: ExtractAnchorsFromSource, ResolveAnchorOccurrence
//   Resources: repoindex
//   Events: anchor-finding
//   OutputFields: anchors, warnings
```

Use single-line `Index: term1, term2` only for keywords. Use structured
`Index:` blocks for hub symbols, entrypoints, and symbols that need related or
flow edges.

Do not add `Index:` blocks to every exported symbol. Add them where searchers
will naturally ask "where does this happen?" or "what is related to this?"

## Decision Workflow

Before editing comments:

1. Read the owner code and nearby docs/tests.
2. Decide whether the need is discoverability (`Index:`) or typed proof
   (`[[...]]`).
3. Choose the strongest owner and avoid duplicate anchors elsewhere.
4. Write stable lowercase slugs using domain language, not implementation noise.
5. Link an existing doc/test target when possible; otherwise add only anchors
   that are true without that external target.
6. Keep the prose short enough that the doc comment remains readable.

After editing anchors, run the project-native parser, repoindex, or E2E
validation when practical. In foxctl specifically, use:

```bash
GOWORK=off go test -count=1 ./internal/intelligence/indexing/semanticanchors ./internal/intelligence/indexing/repoindex
GOWORK=off go test -count=1 ./cmd/foxctl/cmd -run TestIndexRepoSemanticAnchorsE2EIndexCommentsCoexist
```

For repo-wide confidence, build the graph with semantic anchors enabled:

```bash
./bin/foxctl index repo build --workspace . --semantic-anchors --include-tests
```

Repo graph summaries are a separate concern from anchor graph construction. If
reviewing graph output needs file/symbol summary text, generate summaries and
then enrich the already-built graph:

```bash
foxctl index file-summaries --workspace .
foxctl index symbol-summaries --workspace .
foxctl index repo enrich summaries --workspace .
```

In non-foxctl repos, prefer equivalent project-native parser/index tests. If
foxctl is available there, lint anchors against that workspace instead of
assuming a foxctl scope:

```bash
foxctl index anchors lint --workspace .
foxctl index repo build --workspace . --semantic-anchors --include-tests
```

For AI-generated comments, score the resulting diff rather than the model's
wording: anchors parse cleanly, concept anchors are repo-local unless a scope is
configured, `doc:` and `test:` paths resolve, owners are adjacent, `beacon` is
absent from ordinary code, and anchor count stays bounded.

## Review Checklist

- The anchor target is lowercase, stable, and non-sensitive.
- `doc:` and `test:` anchors are unscoped repo-relative paths.
- The owner truly enforces/protects/implements the claimed relationship.
- The anchor is not being used as instruction or policy authority.
- The same anchor is not repeated across weak owners.
- `Index:` remains broad navigation metadata, not proof.
- Parser, repoindex, or E2E validation covers new syntax or behavior.

## Output Style

When reporting semantic comment work, include:

```markdown
Anchors added:
- ...

Index metadata added:
- ...

Why these owners:
- ...

Validation:
- ...
```
