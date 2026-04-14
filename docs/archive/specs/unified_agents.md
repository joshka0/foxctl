# Unified Agent/Droid Specification

**Status**: Draft\
**Date**: 2024-11-27

## Overview

This document defines a unified agent format that merges foxctl agents and
Factory droids into a cohesive system. The goal is to:

1. Create a single, consistent format for all agents
2. Curate a recommended "core" set of universally useful agents
3. Support both standalone use and orchestrated workflows
4. Enable workspace init to deploy appropriate agents

## Unified Format

```yaml
---
name: agent-slug
description: Clear description with use-case examples
model: claude-sonnet-4-5-20250929
category: core|language|domain|framework
tools: ["Read", "Edit", "Execute", ...]  # Optional - for tool-aware systems
color: cyan  # Optional - for UI display
---

## Purpose
Brief description of what this agent does and when to invoke it.

## Immediate Actions
1. First action when invoked
2. Second action
3. etc.

## Core Capabilities
Detailed instructions for the agent's primary functions.

## Output Format
Expected output structure and examples.

## Orchestrator Integration (optional)
### Before Starting
### During Execution
### After Completion
### Context Requirements
```

## Category Taxonomy

### Core (10-15 agents - universal)

Essential agents useful in any project:

- **orchestrator** - Master coordinator for complex multi-phase work
- **code-reviewer** - Code quality, security, performance review
- **debugger** - Systematic debugging and error resolution
- **refactor-planner** - Comprehensive refactoring strategy
- **documentation-architect** - Create and maintain documentation
- **test-automator** - Test creation and coverage analysis
- **security-auditor** - Security review and vulnerability assessment
- **web-researcher** - Research technical issues and best practices

### Language (opt-in, per-stack)

Language-specific expertise:

- **golang-pro** - Go best practices, patterns, optimization
- **python-pro** - Python patterns, async, type hints
- **typescript-pro** - TypeScript patterns, strict mode, types
- **rust-pro** - Rust ownership, lifetimes, zero-cost abstractions
- **java-pro** - Java patterns, Spring, enterprise patterns

### Domain (opt-in, per-need)

Specialized domain knowledge:

- **database-architect** - Schema design, optimization, migrations
- **devops-specialist** - CI/CD, infrastructure, monitoring
- **kubernetes-architect** - K8s patterns, operators, scaling
- **backend-architect** - API design, microservices, system design
- **frontend-developer** - React/Next.js, component patterns

### Framework (opt-in, per-stack)

Framework-specific expertise:

- **nextjs-app-router** - Next.js 14+ App Router patterns
- **django-pro** - Django patterns, ORM, views
- **fastapi-pro** - FastAPI patterns, async, Pydantic

## Curated Core Set

The following agents form the "core" set that should be available by default:

### 1. orchestrator

**Source**: Factory (enhanced) **Purpose**: Master coordinator for complex,
multi-phase work **Key capabilities**:

- Project analysis and research
- Strategic planning with TodoWrite
- Direct implementation for simple tasks
- Delegation to specialists for complex domains

### 2. code-reviewer

**Source**: Factory (merged with foxctl's code-architecture-reviewer)
**Purpose**: Comprehensive code review for quality, security, performance **Key
capabilities**:

- OWASP security analysis
- Performance bottleneck detection
- Architecture consistency review
- Actionable feedback with severity levels

### 3. debugger

**Source**: Factory (merged with foxctl's auto-error-resolver) **Purpose**:
Systematic debugging and error resolution **Key capabilities**:

- Root cause analysis
- Error reproduction
- Targeted fix implementation
- Regression prevention

### 4. refactorer

**Source**: foxctl's code-refactor-master (enhanced) **Purpose**: Plan and
execute comprehensive refactoring **Key capabilities**:

- Dependency tracking
- Import path management
- Component extraction
- Pattern enforcement

### 5. documentation-architect

**Source**: foxctl (merged with Factory's docs-architect) **Purpose**: Create
and maintain comprehensive documentation **Key capabilities**:

- API documentation
- Architecture documentation
- Developer guides
- README maintenance

### 6. test-automator

**Source**: Factory **Purpose**: Test creation and coverage analysis **Key
capabilities**:

- Test case generation
- Coverage gap identification
- Test quality review
- Testing strategy recommendations

### 7. security-auditor

**Source**: Factory **Purpose**: Security review and vulnerability assessment
**Key capabilities**:

- OWASP Top 10 review
- Auth flow analysis
- Secrets management
- Dependency vulnerability scanning

### 8. web-researcher

**Source**: foxctl (merged with Factory's search-specialist) **Purpose**:
Research technical issues and solutions **Key capabilities**:

- Error investigation
- Best practices research
- Solution comparison
- Documentation lookup

## Workspace Init Integration

`foxctl workspace init` should support deploying agents:

```bash
# Initialize with core agents
foxctl workspace init --agents core

# Initialize with specific categories
foxctl workspace init --agents core,golang,devops

# List available agent sets
foxctl workspace init --list-agents
```

### File Layout After Init

```
.claude/
  agents/
    orchestrator.md
    code-reviewer.md
    debugger.md
    refactorer.md
    ...
  commands/
    plan.md
    review.md
    ...
```

## Commands Merge Strategy

### foxctl Commands (keep)

- `dev-docs.md` - Strategic planning command
- `dev-docs-update.md` - Update existing docs

### Factory Commands (adapt)

- `orchestrator.md` - Invoke orchestrator (adapt to foxctl format)

### Unified Command Format

```yaml
---
description: What this command does
argument-hint: Description of expected $ARGUMENTS
---

Instructions for the command using $ARGUMENTS placeholder.
```

## Implementation Plan

### Phase 1: Core Agent Curation

1. Merge `code-reviewer` (Factory) + `code-architecture-reviewer` (foxctl)
2. Merge `debugger` (Factory) + `auto-error-resolver` (foxctl)
3. Merge `documentation-specialist` (Factory) + `documentation-architect`
   (foxctl)
4. Keep `orchestrator`, `test-automator`, `security-auditor` from Factory
5. Keep `code-refactor-master` from foxctl (as `refactorer`)
6. Merge `search-specialist` (Factory) + `web-research-specialist` (foxctl)

### Phase 2: Builtin Embedding

1. Add curated core agents to `internal/context/knowledge/builtin/data/agents/`
2. Embed as knowledge items with `kind: agent`
3. Make available via `foxctl knowledge sync`

### Phase 3: Workspace Init

1. Implement `foxctl workspace init --agents` flag
2. Copy selected agents to `.claude/agents/`
3. Support category selection (core, language/X, domain/X)

### Phase 4: Command Merge

1. Unify command format
2. Add key commands to builtin
3. Support workspace init for commands

## Future Considerations

- **Agent versioning**: Track agent versions for updates
- **Custom agents**: Allow users to add custom agents to registry
- **Agent discovery**: Search agents by capability, not just name
- **Agent composition**: Combine agents for complex workflows
