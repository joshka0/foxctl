# RLM Context System

> Status: Fully implemented with contextvar store and RLM tools

## Overview

The RLM (Recursive Language Model) context system enables stateless per-turn LLM operation by storing context externally and querying it on-demand via tools. Instead of accumulating all context in the LLM's context window, the model actively navigates and retrieves relevant information each turn.

This approach offers several advantages:
- **Unlimited context**: No context window limits; context scales with storage
- **Active retrieval**: Model queries for what it needs, reducing noise
- **Semantic search**: Find relevant context by meaning, not just keywords
- **Evolving personality**: Learn and adapt communication style over time

## Architecture

```
┌────────────────────────────────────────────────────────────────────────┐
│                         RLM Context System                              │
├────────────────────────────────────────────────────────────────────────┤
│                                                                        │
│  ┌──────────────┐    ┌──────────────────┐    ┌───────────────────┐    │
│  │ RLM Tools    │───▶│ ContextVar Store │───▶│ SQLite + Vectors  │    │
│  │ (in engine)  │    │ (internal/       │    │ (context_vars)    │    │
│  │              │    │  storage/        │    │                   │    │
│  │ - put        │    │  contextvar)     │    └───────────────────┘    │
│  │ - query      │◀───│                  │                              │
│  │ - list       │    │                  │    ┌───────────────────┐    │
│  │ - personality│    └──────────────────┘    │ Memory Store      │    │
│  └──────────────┘             │              │ (semantic search) │    │
│                               │              └───────────────────┘    │
│                               ▼                        ▲              │
│                    ┌──────────────────┐                │              │
│                    │ Evolving         │────────────────┘              │
│                    │ Personality      │                               │
│                    │ (companion pkg)  │                               │
│                    └──────────────────┘                               │
│                                                                        │
└────────────────────────────────────────────────────────────────────────┘
```

## Components

### 1. Context Variable Store (`internal/storage/contextvar`)

A SQLite-backed key-value store with three persistence scopes:

| Scope | Description | Use Cases |
|-------|-------------|-----------|
| `global` | Persists across all conversations | User profile, preferences, learned traits |
| `conversation` | Persists within one conversation | Current topics, session-specific facts |
| `turn` | Ephemeral, current turn only | Working memory, temporary calculations |

**Features:**
- TTL (time-to-live) support for auto-expiring variables
- Glob pattern matching for key queries
- Access counting for usage tracking
- Semantic embedding support for vector search
- CAS integration for large values (>64KB)

### 2. RLM Tool Executor (`internal/engine/rlm_tools.go`)

Provides four tools that the LLM can call:

#### `rlm_context_put`
Store a context variable for later retrieval.

```json
{
  "key": "user_name",
  "value": "Sarah",
  "scope": "global",
  "ttl_seconds": 86400
}
```

#### `rlm_context_query`
Retrieve context by key, pattern, or semantic search.

```json
{
  "key": "user_name"
}
// or
{
  "key_pattern": "preferences/*"
}
// or
{
  "semantic_query": "topics we discussed yesterday"
}
```

#### `rlm_context_list`
List available context keys.

```json
{
  "scope": "conversation"
}
```

#### `rlm_personality_adjust`
Adjust communication style based on feedback.

```json
{
  "dimension": "verbosity",
  "direction": "decrease",
  "amount": 0.2,
  "reason": "user asked for shorter responses"
}
```

### 3. Evolving Personality (`internal/companion/evolving_personality.go`)

Dynamic personality adaptation with six adjustable dimensions:

| Dimension | Min Label | Max Label | Default |
|-----------|-----------|-----------|---------|
| `formality` | formal | casual | 0.5 |
| `verbosity` | brief | detailed | 0.5 |
| `enthusiasm` | calm | energetic | 0.6 |
| `humor` | serious | playful | 0.3 |
| `empathy` | task-focused | supportive | 0.7 |
| `proactivity` | responsive | suggests | 0.5 |

**Features:**
- Persistent profile storage in global scope
- Learned traits tracking (up to 10)
- User interests and dislikes
- Feedback log for analysis
- Dynamic system prompt generation

## Integration with Companion Memory

RLM context complements the [Companion Memory System](./companion-memory.md):

| System | Purpose | Storage |
|--------|---------|---------|
| Companion Memory | Turn-by-turn conversation history | L0/L1/L2 in companion.db |
| RLM Context | Structured facts and preferences | context_vars in contextvar.db |

**When to use each:**
- **Companion Memory**: "What did we talk about yesterday?"
- **RLM Context**: "What's the user's preferred coding style?"

## Data Flow

### Storing Context

```
User: "My name is Sarah"
    │
    ▼
LLM detects user info
    │
    ▼
LLM calls: rlm_context_put(key="user_name", value="Sarah", scope="global")
    │
    ▼
ContextVar Store persists to SQLite
    │
    ▼
Response: {"success": true, "key": "user_name", "scope": "global"}
```

### Querying Context

```
User: "What's my name again?"
    │
    ▼
LLM calls: rlm_context_query(key="user_name")
    │
    ▼
ContextVar Store retrieves from SQLite
    │
    ▼
Response: {"variables": [{"key": "user_name", "value": "Sarah", ...}], "found": true}
    │
    ▼
LLM responds: "Your name is Sarah!"
```

### Semantic Search Flow

```
LLM calls: rlm_context_query(semantic_query="topics we discussed")
    │
    ├─▶ If embedding provider configured:
    │      Generate query embedding
    │      Vector similarity search in Memory Store
    │
    └─▶ Fallback:
           Text search in Memory Store
    │
    ▼
Response: {"memories": [...], "found": true, "count": 5}
```

## Storage Schema

### context_vars table

```sql
CREATE TABLE context_vars (
    id TEXT PRIMARY KEY,
    conversation_id TEXT NOT NULL,
    scope TEXT NOT NULL,           -- 'global', 'conversation', 'turn'
    key TEXT NOT NULL,
    value_json TEXT,               -- Inline JSON for small values
    value_cas TEXT,                -- CAS digest for large values
    content_type TEXT DEFAULT 'json',
    sequence_num INTEGER,
    source TEXT,                   -- Producer (tool, skill, user)
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    expires_at DATETIME,
    access_count INTEGER DEFAULT 0,
    last_access DATETIME,
    embedding BLOB,                -- Optional semantic embedding
    embedding_model TEXT,
    UNIQUE(conversation_id, scope, key)
);

CREATE INDEX idx_context_vars_conv ON context_vars(conversation_id);
CREATE INDEX idx_context_vars_scope ON context_vars(scope);
CREATE INDEX idx_context_vars_expires ON context_vars(expires_at);
```

**Database location:** `~/.agentctl/storage/contextvar.db`

## Usage

### Enabling RLM Tools for an Agent

```go
import (
    "github.com/jkatigb/agentctl/internal/engine"
    "github.com/jkatigb/agentctl/internal/storage/contextvar"
)

// Create contextvar store
store, _ := contextvar.NewSQLiteStore(ctx, "/path/to/contextvar.db")

// Create RLM tool executor
rlmExecutor := engine.NewRLMToolExecutor(store, conversationID)

// Optional: Enable semantic search
rlmExecutor.SetMemoryStore(memoryStore, workspace)
rlmExecutor.SetEmbedProvider(embedProvider)

// Combine with other tools
tools := engine.NewCompositeToolExecutor(rlmExecutor, otherExecutor)
```

### Using Evolving Personality

```go
import "github.com/jkatigb/agentctl/internal/companion"

ep := companion.NewEvolvingPersonality(contextvarStore, conversationID)

// Get current profile
profile, _ := ep.GetProfile(ctx)

// Apply feedback
ep.ApplyFeedback(ctx, companion.PersonalityFeedback{
    Dimension: "verbosity",
    Direction: "decrease",
    Amount:    0.2,
    Reason:    "user prefers concise answers",
})

// Build dynamic system prompt
systemPrompt, _ := ep.BuildSystemPrompt(ctx, basePrompt)
```

## Dependencies

| Component | Dependency | Purpose |
|-----------|------------|---------|
| ContextVar Store | SQLite | Persistence |
| RLM Tools | `internal/storage/contextvar` | Variable storage |
| Semantic Query | `internal/storage` (MemoryStore) | Memory search |
| Semantic Query | `internal/intelligence/indexing/semantic` (EmbedProvider) | Embeddings |
| Personality | `internal/storage/contextvar` | Profile storage |

## Environment Variables

| Variable | Default | Purpose |
|----------|---------|---------|
| `AGENTCTL_HOME` | `~/.agentctl` | Storage root |
| `VOYAGE_API_KEY` | - | Semantic embeddings |

## Related

- [Companion Memory](./companion-memory.md) - Conversation history storage
- [Architecture](./architecture.md) - Overall system design
- [Storage](./storage.md) - Database overview
