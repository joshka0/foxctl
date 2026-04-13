# Factory Knowledge Builtin Integration

**Date**: 2024-11-27\
**Status**: Implemented\
**Spec**: `docs/spec/knowledge_factory_bridge.md`

## Summary

Integrated FactoryAI DROID assets as builtin knowledge in agentctl. Factory
droids are now embedded in the agentctl binary and automatically seeded into the
knowledge registry on `agentctl knowledge sync`.

## New Files

### Embedded Assets

- `internal/context/knowledge/builtin/data/droids/orchestrator.md` - Master coordinator
  droid
- `internal/context/knowledge/builtin/data/droids/backend-architect.md` - API/database
  design droid
- `internal/context/knowledge/builtin/data/droids/frontend-developer.md` - Next.js/React
  droid

### Package

- `internal/context/knowledge/builtin/factory.go` - Embedding, parsing, and seeding
  logic
- `internal/context/knowledge/builtin/factory_test.go` - Unit tests

## Modified Files

- `cmd/agentctl/cmd/knowledge.go` - Added builtin seeding to sync command

## CLI Changes

### `agentctl knowledge sync`

New flags:

- `--skip-builtin` - Skip seeding builtin knowledge (Factory droids, etc.)
- `--builtin-only` - Seed only builtin knowledge, skip workspace scan

Default behavior: Seeds builtin knowledge + scans workspace filesystem.

## Knowledge Items Created

| Name                               | Kind  | Source                                           |
| ---------------------------------- | ----- | ------------------------------------------------ |
| `factory/droid/orchestrator`       | agent | `builtin://factory/droids/orchestrator.md`       |
| `factory/droid/backend-architect`  | agent | `builtin://factory/droids/backend-architect.md`  |
| `factory/droid/frontend-developer` | agent | `builtin://factory/droids/frontend-developer.md` |

## Triggers

Each droid has keyword triggers extracted from its description, plus standard
`factory` and `droid` triggers.

## Architecture

```
┌─────────────────────────────────────────────────────┐
│                   agentctl binary                    │
│  ┌─────────────────────────────────────────────┐    │
│  │  internal/context/knowledge/builtin/                │    │
│  │  ├── data/droids/*.md  (go:embed)          │    │
│  │  └── factory.go        (SeedFactoryKnowledge) │  │
│  └─────────────────────────────────────────────┘    │
└─────────────────────────────────────────────────────┘
                          │
                          ▼
┌─────────────────────────────────────────────────────┐
│           ~/.agentctl/storage/knowledge.db          │
│  ┌─────────────────────────────────────────────┐    │
│  │  knowledge_items (kind=agent)               │    │
│  │  knowledge_triggers (keywords)              │    │
│  │  knowledge_documents (full markdown body)   │    │
│  └─────────────────────────────────────────────┘    │
└─────────────────────────────────────────────────────┘
```

## Future Work

- Add more Factory droids (security-auditor, devops-specialist, etc.)
- Add Factory orchestrator configuration as knowledge pack
- Integrate with `hooks/knowledge_router` for automatic advisory injection
