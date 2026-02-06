# Agent Runtime Refactors

**Status:** Approved\
**Scope:** Two focused refactors to improve agent runtime security and
flexibility\
**Related:** `agent-daemon.md`, `core_profile_v1.md` (§6 Filesystem Skills)

---

## Overview

This spec describes two targeted refactors for the LLMChatEngine-based agent runtime:

1. **LLM Provider Switching** - Support multiple LLM providers (Gemini, OpenAI)
   with configurable defaults
2. **PathValidator Integration** - Route all fs/code tools through
   `policy.PathValidator` for security

---

## 1. LLM Provider Switching

### 1.1 Problem

Previously the agent runtime defaulted to a fixed provider:

```go
llm, err := llms.NewGeminiLLM(apiKey, core.ModelID(model))
```

The runtime should support multiple providers with a clear selection policy.

### 1.2 Solution

Add provider selection with a clear precedence chain.

#### Configuration Precedence (per field)

**Provider:**

1. `AgentConfig.LLMProvider` (agent-level)
2. `RuntimeConfig.LLMProvider` (runtime-level)
3. `AGENTCTL_LLM_PROVIDER` env var
4. Default: `"gemini"`

**Model:**

1. `AgentConfig.LLMModel` (agent-level)
2. `RuntimeConfig.LLMModel` (runtime-level)
3. `AGENTCTL_LLM_MODEL` env var
4. Provider-specific default (see below)

#### Supported Providers & Defaults

| Provider | Provider String | Default Model      | Notes                                       |
| -------- | --------------- | ------------------ | ------------------------------------------- |
| Gemini   | `gemini`        | `gemini-2.5-flash` | Best for agents - fast, agentic, 1M context |
| OpenAI   | `openai`        | `gpt-4.1-mini`     | Great perf, 83% cheaper than 4o             |

**Available Models:**

Gemini:

- `gemini-2.5-flash` (default) - Best price/performance for agents
- `gemini-2.5-pro` - Advanced reasoning
- `gemini-2.5-flash-lite` - Ultra-fast, cheapest
- `gemini-3-pro-preview` - Latest preview  // brand new model as of November 28, 2025

OpenAI:

- `gpt-4.1-mini` (default) - Best balance of perf/cost
- `gpt-4.1` - Best coding, instruction following
- `gpt-4.1-nano` - Fastest/cheapest

#### Implementation

In `runtime.go` `createAgent`:

```go
// Resolve provider (agent → runtime → env → default)
provider := cfg.LLMProvider
if provider == "" {
    provider = r.config.LLMProvider
}
if provider == "" {
    provider = os.Getenv("AGENTCTL_LLM_PROVIDER")
}
if provider == "" {
    provider = "gemini"
}

// Resolve model (agent → runtime → env → provider default)
model := cfg.LLMModel
if model == "" {
    model = r.config.LLMModel
}
if model == "" {
    model = os.Getenv("AGENTCTL_LLM_MODEL")
}
if model == "" {
    model = defaultModelForProvider(provider)
}

// Create LLM based on provider
var llm core.LLM
switch provider {
case "gemini", "":
    llm, err = llms.NewGeminiLLM(apiKey, core.ModelID(model))
case "openai":
    llm, err = llms.NewOpenAILLM(core.ModelID(model), llms.WithAPIKey(apiKey))
default:
    return nil, fmt.Errorf("unsupported LLM provider: %s (supported: gemini, openai)", provider)
}
```

Helper function:

```go
func defaultModelForProvider(provider string) string {
    switch provider {
    case "openai":
        return "gpt-4.1-mini"
    case "gemini", "":
        return "gemini-2.5-flash"
    default:
        return "gemini-2.5-flash"
    }
}
```

#### Error Semantics

- **Missing API key:**
  `"LLM API key not configured for provider %s (set AGENTCTL_LLM_API_KEY)"`
- **Unsupported provider:**
  `"unsupported LLM provider: %s (supported: gemini, openai)"`
- **LLM creation failure:** `"create %s LLM: %w"`

### 1.3 Tests

- Provider fallback chain works correctly
- Model fallback uses provider-specific defaults
- Unsupported provider returns clear error
- Missing API key error includes provider name

---

## 2. PathValidator Integration

### 2.1 Problem

`fs_tools.go` uses a simple `resolvePath` that only does basic `".."` rejection:

```go
func (r *Registry) resolvePath(path string) (string, error) {
    // ... basic string manipulation
}
```

This doesn't use `policy.PathValidator` which handles:

- Symlink traversal attacks
- Proper canonicalization
- Allowed roots configuration
- UTF-8 and null byte validation

Per `AGENTS.md` §6: "All `fs/*` and `text/grep` skills must route user paths
through `policy.PathValidator`."

### 2.2 Solution

Wire `PathValidator` into the tools Registry.

#### ToolsConfig Extension

```go
type ToolsConfig struct {
    WorkspaceRoot    string
    WorkspaceID      string
    ActorID          string
    MaxFileSize      int64
    MaxSearchResults int
    AllowedRoots     []string // NEW: optional additional allowed roots
}
```

#### Registry Extension

```go
type Registry struct {
    tools         *dstools.InMemoryToolRegistry
    recorder      TelemetryRecorder
    config        ToolsConfig
    pathValidator *policy.PathValidator // NEW
}
```

#### NewRegistry Changes

```go
func NewRegistry(cfg ToolsConfig, recorder TelemetryRecorder) (*Registry, error) {
    // ... existing setup ...

    // Initialize PathValidator
    pathValidator, err := policy.NewPathValidator(cfg.WorkspaceRoot, cfg.AllowedRoots)
    if err != nil {
        return nil, fmt.Errorf("init path validator: %w", err)
    }

    r := &Registry{
        tools:         dstools.NewInMemoryToolRegistry(),
        recorder:      recorder,
        config:        cfg,
        pathValidator: pathValidator,
    }
    // ...
}
```

#### resolvePath Implementation

```go
func (r *Registry) resolvePath(userPath string) (string, error) {
    if r.pathValidator == nil {
        return "", fmt.Errorf("path validator not configured")
    }
    
    abs, err := r.pathValidator.ValidatePath(userPath)
    if err != nil {
        // Map validator errors to user-friendly messages
        switch {
        case errors.Is(err, policy.ErrPathEscape):
            return "", fmt.Errorf("path %q escapes workspace boundary", userPath)
        case errors.Is(err, policy.ErrSymlinkEscape):
            return "", fmt.Errorf("path %q contains symlink pointing outside workspace", userPath)
        case errors.Is(err, policy.ErrNullByte):
            return "", fmt.Errorf("path %q contains invalid characters", userPath)
        default:
            return "", fmt.Errorf("invalid path %q: %w", userPath, err)
        }
    }
    return abs, nil
}
```

#### Affected Tools

All tools using `resolvePath` automatically get protection:

- `fs.read_file`
- `fs.list_dir`
- `code.search` (via search path)
- `edit.create_file`
- `edit.apply_patch`

### 2.3 Tests

New tests in `internal/agent/tools/`:

```go
func TestResolvePath_InsideWorkspace(t *testing.T)
func TestResolvePath_ParentEscape(t *testing.T)      // ../../../etc/passwd
func TestResolvePath_AbsoluteEscape(t *testing.T)   // /etc/passwd
func TestResolvePath_SymlinkEscape(t *testing.T)    // symlink → outside workspace
func TestResolvePath_AllowedRoots(t *testing.T)     // paths in allowed roots work
func TestResolvePath_NullByte(t *testing.T)         // null byte rejection
```

---

## 3. Implementation Order

1. **LLM Provider Switching** (smaller, self-contained)
   - Update `runtime.go` with provider switch
   - Add `defaultModelForProvider` helper
   - Add unit tests
   - Update `agent_hierarchy.md` with LLM configuration section

2. **PathValidator Integration** (security-critical)
   - Extend `ToolsConfig` and `Registry`
   - Wire `PathValidator` in `NewRegistry`
   - Replace `resolvePath` implementation
   - Add comprehensive tests
   - Verify all tools protected

---

## 4. Future Considerations

- **Per-agent provider policies:** Overseer could restrict which providers child
  agents can use
- **Per-role allowed roots:** Different roles might have different filesystem
  access
- **Cost tracking:** Track API costs per provider/model for budgeting

---

## Appendix: Environment Variables

| Variable                | Description                       | Default           |
| ----------------------- | --------------------------------- | ----------------- |
| `AGENTCTL_LLM_PROVIDER` | LLM provider (`gemini`, `openai`) | `gemini`          |
| `AGENTCTL_LLM_MODEL`    | Model name                        | Provider-specific |
| `AGENTCTL_LLM_API_KEY`  | API key for the selected provider | (required)        |
