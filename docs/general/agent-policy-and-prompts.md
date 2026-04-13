# Agent Policy and Prompts

Machine-friendly reference for capability profiles and role instructions.

## Core Packages

| Package | Responsibility |
|--------|----------------|
| `internal/agentpolicy` | Authorize/deny agent shell commands based on profile skill allowlists |
| `internal/runtime/agentprompt` | Build role-specific system instructions and runtime tool-name aliases |

## Profile Model (`internal/agentpolicy`)

| Profile | Intent | Restriction model |
|--------|--------|-------------------|
| `explorer` | Read-only investigation | Only allowlisted `agentctl run <skill>` commands |
| `reviewer` | Analysis/review | Explorer + review-oriented skills |
| `implementer` | Targeted code changes | Reviewer + write/test skills |
| `unrestricted` | Full capability | No skill-level command restriction |

## Authorization Semantics

| Rule | Behavior |
|------|----------|
| Non-restricted profile | Command allowed |
| Restricted profile + allowlisted `agentctl run <skill>` | Allowed |
| Restricted profile + non-allowlisted skill | Blocked |
| Restricted profile + non-`agentctl run` bash command | Blocked |

## Prompt Construction (`internal/runtime/agentprompt`)

| Function | Purpose |
|---------|---------|
| `Instruction(role)` | Returns role-specific system instruction text |
| `InstructionRuntime(role)` | Returns role instruction with runtime tool-name aliases applied |

## Integration Points

| Location | Usage |
|---------|-------|
| `internal/agent/runtime` | Applies role prompts to session/system prompts |
| `internal/actor` | Uses role prompts when running agent actors |

## Related Docs

- `docs/general/agent-daemon.md`
- `docs/spec/agent_hierarchy.md`
- `docs/spec/overseer_profile.md`
