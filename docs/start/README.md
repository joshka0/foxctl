# docs/ — Documentation Index

> **Start with `AGENTS.md`** for conventions, then use this index to find
> detailed documentation.

## Directory Tree

```
docs/
├── start/                    # Quick-start guides (this directory)
│   ├── README.md             # ← You are here
│   ├── testing_and_ci.md     # Tests, coverage, race, CI pipelines
│   ├── openapi_and_plugins.md # http/openapi skill, auth/pagination plugins
│   └── gotchas.md            # Gotchas Graveyard rules and "never again" list
├── spec/                     # Canonical specifications
│   ├── core_profile_v1.md    # Envelope, CAS, jobs, WASI/exec runners
│   ├── openapi_skill.md      # http/openapi input/output contract
│   ├── plugin_protocol.md    # Auth/pagination plugin SPI
│   └── dspy_go_agents.md     # dspy-go agent runtime & tools
├── impl_plan/                # Phased implementation plans
│   └── universal_swe_grep_and_agents*.md
├── changelogs/               # YYYY-MM-DD_<name>.md entries
├── ci/                       # CI skill docs (prcomments, github_checks)
├── examples/                 # Workflow examples
├── guides/                   # How-to guides (agent_loop, protocol_v1)
├── knowledge/                # Knowledge packs for Claude hooks
├── godot/                    # Godot editor integration (future)
├── agent_profile.md          # Multi-agent orchestration spec
├── architecture.md           # Layered architecture overview
├── roadmap.md                # v1.0 roadmap and priorities
├── error-handling.md         # Error codes & patterns
├── SECURITY.md               # Security policy
└── TROUBLESHOOTING.md        # Common issues
```

## Quick Reference

| Topic                    | Document                                    |
| ------------------------ | ------------------------------------------- |
| **Envelope contract**    | `spec/core_profile_v1.md` §2                |
| **CAS & large outputs**  | `spec/core_profile_v1.md` §4                |
| **Jobs & caching**       | `spec/core_profile_v1.md` §7–8              |
| **WASI vs exec runners** | `spec/core_profile_v1.md` §10               |
| **OpenAPI skill**        | `spec/openapi_skill.md`                     |
| **Plugin protocol**      | `spec/plugin_protocol.md`                   |
| **Agent orchestration**  | `agent_profile.md`                          |
| **dspy-go agents**       | `spec/dspy_go_agents.md`                    |
| **SWE Grep skill**       | `spec/code_symbol_index_and_swe_grep.md` §5 |
| **Symbol index**         | `start/symbol_index.md`                     |
| **Testing & CI**         | `start/testing_and_ci.md`                   |
| **OpenAPI & plugins**    | `start/openapi_and_plugins.md`              |
| **Gotchas & rules**      | `start/gotchas.md`                          |
| **Architecture**         | `architecture.md`                           |
| **Roadmap**              | `roadmap.md`                                |
| **Error codes**          | `error-handling.md`                         |

## Where to Look

- **Wire contract questions** → `spec/core_profile_v1.md`
- **How to test / CI failing** → `start/testing_and_ci.md`
- **OpenAPI skill behavior** → `spec/openapi_skill.md` +
  `start/openapi_and_plugins.md`
- **Multi-agent features** → `agent_profile.md`
- **Implementation status** → `impl_plan/universal_swe_grep_and_agents.md`
