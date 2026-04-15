# Agent Loop Implementation Guide

**Version:** 1.0.0
**Status:** Implementation Guide
**Last Updated:** 2025-11-15

> **Purpose:** This document provides a complete implementation guide for building LLM agent loops using foxctl as the execution substrate. It includes JSON contracts, Go reference implementation, and concrete examples.

---

## Table of Contents

1. [Overview](#1-overview)
2. [Why Foxctl for Agents](#2-why-foxctl-for-agents)
3. [Agent Loop JSON Contract](#3-agent-loop-json-contract)
4. [Go Implementation](#4-go-implementation)
5. [Complete Walkthrough](#5-complete-walkthrough)
6. [Token Efficiency](#6-token-efficiency)
7. [Best Practices](#7-best-practices)
8. [Production Considerations](#8-production-considerations)

---

## 1. Overview

### 1.1 What is an Agent Loop?

An **agent loop** is a system that:
1. Takes a high-level goal from the user
2. Breaks it down into executable tool calls
3. Invokes tools (skills) via foxctl
4. Feeds results back to an LLM for decision-making
5. Repeats until the goal is achieved

### 1.2 Why Use Foxctl as Agent Substrate?

**Conventional approach** (problematic):
```
User → LLM → free-text tool description → LLM → free-text output → LLM → ...
```
Problems: huge token usage, inconsistent outputs, hard to debug

**Foxctl approach** (efficient):
```
User → LLM → tiny JSON → foxctl skill → structured envelope → LLM → ...
```
Benefits:
- **Token efficiency**: Small JSON schemas, no repeated tool descriptions
- **Consistency**: Every skill returns same envelope shape
- **Debuggability**: Full audit trail with CAS artifacts
- **Reusability**: Same substrate across all projects

### 1.3 Design Goals

- **Minimal LLM context**: Only show small tool hints and recent summaries
- **Structured I/O**: JSON envelopes eliminate prompt engineering for tool outputs
- **Separation of concerns**: LLM does planning, foxctl does execution
- **Transparency**: All side effects captured in envelopes and CAS

---

## 2. Why Foxctl for Agents

### 2.1 Comparison with Traditional Approaches

| Aspect | Traditional ("Prompt RPC") | Foxctl Substrate |
|--------|---------------------------|-------------------|
| **Tool schemas** | Full OpenAPI spec in every prompt (10KB+) | Tiny hints (< 1KB) |
| **Tool outputs** | Plain text logs, HTML, giant JSON | Structured envelopes + CAS |
| **Pagination** | LLM asks "fetch page 2, page 3..." | Automatic in http/openapi skill |
| **Retries** | LLM prompts "try again" | Built-in retry logic with backoff |
| **Large outputs** | Consume massive tokens | CAS digest + summary only |
| **Secrets** | Risk of leaking in prompts | Redacted in all outputs |
| **Debuggability** | Hard to replay | Full envelope history + artifacts |

### 2.2 Token Usage Reduction

Example: Calling GitHub API to list repos

**Traditional approach**:
- Tool schema: ~5KB (full OpenAPI spec fragment)
- Output: ~50KB (JSON array of repos)
- **Total**: ~55KB per call

**Foxctl approach**:
- Tool hint: ~200 bytes
- Output summary: ~500 bytes (digest + preview)
- **Total**: ~700 bytes per call

**Reduction**: **98.7%** for this example

### 2.3 Consistency Gains

All skills return the same envelope shape:
```json
{
  "version": 1,
  "status": "ok|error",
  "command": "skill/name",
  "data": { /* predictable structure */ },
  "meta": { /* execution metadata */ },
  "error": { /* standardized errors */ }
}
```

The LLM only needs to learn **one** output format, not dozens of ad-hoc shapes.

---

## 3. Agent Loop JSON Contract

### 3.1 Tool Descriptors (Shown to LLM)

Minimal, human + machine friendly tool hints:

```json
[
  {
    "name": "fs/ls",
    "description": "List files and directories in a workspace.",
    "input_hint": {
      "path": "string (dir to list, default '.')",
      "max_depth": "int (optional)"
    }
  },
  {
    "name": "fs/read",
    "description": "Read a file and return preview + CAS digest.",
    "input_hint": {
      "path": "string (file path)",
      "max_bytes": "int (optional)"
    }
  },
  {
    "name": "text/grep",
    "description": "Search for a pattern in one or more files.",
    "input_hint": {
      "path": "string or array of strings",
      "pattern": "string (regex)"
    }
  },
  {
    "name": "http/openapi",
    "description": "Call an OpenAPI 3.x operation.",
    "input_hint": {
      "spec": "string (memory:name, sha256:..., or path)",
      "operationId": "string",
      "params": {
        "path": "object",
        "query": "object",
        "header": "object",
        "body": "object or null"
      },
      "paging": "object (optional)",
      "dry_run": "boolean (optional)"
    }
  }
]
```

### 3.2 Agent State (Input to LLM)

Minimal state for each planning step:

```json
{
  "goal": "Scan the ./services/payments package and identify likely flaky tests, then propose stabilization steps.",
  "workspace": "/home/user/repo",
  "namespace": "org/app/main",
  "step": 3,
  "max_steps": 10,
  "history": [
    {
      "kind": "thought",
      "content": "I should inspect the README first."
    },
    {
      "kind": "tool_call",
      "tool": "fs/read",
      "params": { "path": "README.md" }
    },
    {
      "kind": "tool_result",
      "tool": "fs/read",
      "status": "ok",
      "summary": {
        "path": "README.md",
        "preview": "foxctl is a CLI for structured AI workflows..."
      }
    }
  ]
}
```

**Key points**:
- `history`: Truncated summaries, not full envelopes
- `step` / `max_steps`: Prevent infinite loops
- Small state (< 5KB typical)

### 3.3 Agent Decision (Output from LLM)

Two modes: **tool call** or **final answer**

#### Tool Call Decision
```json
{
  "type": "tool",
  "tool": "http/openapi",
  "params": {
    "spec": "memory:github",
    "operationId": "listReposForUser",
    "params": {
      "path": { "username": "octocat" },
      "query": { "per_page": 50 }
    },
    "paging": { "max_items": 100 }
  },
  "commentary": "I will list Octocat's repos via GitHub OpenAPI.",
  "expectation": "I expect a list of repo objects to inspect repo names."
}
```

#### Final Answer Decision
```json
{
  "type": "final",
  "answer": "Based on my analysis, the flaky tests in services/payments are:\n1. TestRefunds_ExternalGateway - depends on real payment gateway\n2. TestRefunds_LongRunning - uses time.Sleep and real DB\n\nStabilization steps:\n1. Mock the payment gateway interface\n2. Replace time.Sleep with fake clock\n3. Isolate DB fixtures per test",
  "used_tools": [
    { "tool": "fs/ls", "reason": "discover test files" },
    { "tool": "text/grep", "reason": "find flaky markers" },
    { "tool": "fs/read", "reason": "inspect test implementation" }
  ]
}
```

### 3.4 LLM Prompt Template

```text
You are an agent that uses tools via foxctl. You MUST respond with valid JSON only.

GOAL:
{{ .State.Goal }}

WORKSPACE:
{{ .State.Workspace }}

STEP: {{ .State.Step }} of {{ .State.MaxSteps }}

TOOLS (you may ONLY call these):
{{ .Tools | json }}

HISTORY (recent steps, truncated summaries):
{{ .State.History | json }}

SCHEMA FOR YOUR RESPONSE:

You must respond with EXACTLY ONE JSON object of one of these shapes:

1) Call a tool:
{
  "type": "tool",
  "tool": "<tool name from TOOLS.name>",
  "params": { ... tool-specific params ... },
  "commentary": "short natural language why you picked this tool",
  "expectation": "short description of what you expect as a result"
}

2) Finish the task:
{
  "type": "final",
  "answer": "your final answer to the user, in natural language",
  "used_tools": [
    { "tool": "<name>", "reason": "why it helped" }
  ]
}

DO NOT add extra fields. DO NOT write any text before or after the JSON.
Think carefully, then respond with ONLY the JSON.
```

---

## 4. Go Implementation

### 4.1 Core Types

```go
// internal/agentloop/types.go
package agentloop

import (
    "context"
    "encoding/json"
    "time"

    "github.com/joshka0/foxctl/internal/protocol"
)

// ToolDescriptor shown to the model
type ToolDescriptor struct {
    Name        string      `json:"name"`
    Description string      `json:"description"`
    InputHint   interface{} `json:"input_hint,omitempty"`
}

// HistoryEntry summarizes a thought/tool call/result
type HistoryEntry struct {
    Kind      string                 `json:"kind"` // "thought"|"tool_call"|"tool_result"
    Tool      string                 `json:"tool,omitempty"`
    Params    map[string]interface{} `json:"params,omitempty"`
    Status    string                 `json:"status,omitempty"`
    Summary   map[string]interface{} `json:"summary,omitempty"`
    Content   string                 `json:"content,omitempty"`
    ErrorCode string                 `json:"error_code,omitempty"`
}

// State fed to the model
type State struct {
    Goal      string         `json:"goal"`
    Workspace string         `json:"workspace,omitempty"`
    Namespace string         `json:"namespace,omitempty"`
    Step      int            `json:"step"`
    MaxSteps  int            `json:"max_steps"`
    History   []HistoryEntry `json:"history,omitempty"`
}

// DecisionType is the union discriminator
type DecisionType string

const (
    DecisionTool  DecisionType = "tool"
    DecisionFinal DecisionType = "final"
)

// ToolDecision represents a tool invocation
type ToolDecision struct {
    Type        DecisionType    `json:"type"` // "tool"
    Tool        string          `json:"tool"`
    Params      json.RawMessage `json:"params"`
    Commentary  string          `json:"commentary,omitempty"`
    Expectation string          `json:"expectation,omitempty"`
}

// FinalDecision represents task completion
type FinalDecision struct {
    Type      DecisionType              `json:"type"` // "final"
    Answer    string                    `json:"answer"`
    UsedTools []map[string]string       `json:"used_tools,omitempty"`
}

// Decision is the union type
type Decision struct {
    Tool  *ToolDecision
    Final *FinalDecision
}
```

### 4.2 Interfaces

```go
// internal/agentloop/interfaces.go
package agentloop

import (
    "context"
    "encoding/json"

    "github.com/joshka0/foxctl/internal/protocol"
)

// LLMClient abstracts the language model
type LLMClient interface {
    // Complete generates a response for the given prompt
    Complete(ctx context.Context, prompt string) (string, error)
}

// Invoker executes a skill and returns an envelope
type Invoker interface {
    Invoke(ctx context.Context, tool string, params json.RawMessage) (protocol.Envelope, error)
}
```

### 4.3 Prompt Builder

```go
// internal/agentloop/prompt.go
package agentloop

import (
    "bytes"
    "encoding/json"
    "text/template"
)

const promptTemplate = `You are an agent that uses tools via foxctl.
You MUST respond with valid JSON only.

GOAL:
{{.Goal}}

WORKSPACE:
{{.Workspace}}

STEP: {{.Step}} of {{.MaxSteps}}

TOOLS (you may ONLY call these):
{{.ToolsJSON}}

HISTORY (recent steps, truncated summaries):
{{.HistoryJSON}}

SCHEMA FOR YOUR RESPONSE:

You must respond with EXACTLY ONE JSON object of one of these shapes:

1) Call a tool:
{
  "type": "tool",
  "tool": "<tool name from TOOLS.name>",
  "params": { ... tool-specific params ... },
  "commentary": "short natural language why you picked this tool",
  "expectation": "short description of what you expect as a result"
}

2) Finish the task:
{
  "type": "final",
  "answer": "your final answer to the user, in natural language",
  "used_tools": [
    { "tool": "<name>", "reason": "why it helped" }
  ]
}

DO NOT add extra fields. DO NOT write any text before or after the JSON.
Think carefully, then respond with ONLY the JSON.
`

func BuildPrompt(state State, tools []ToolDescriptor) (string, error) {
    tmpl, err := template.New("prompt").Parse(promptTemplate)
    if err != nil {
        return "", err
    }

    toolsJSON, err := json.MarshalIndent(tools, "", "  ")
    if err != nil {
        return "", err
    }

    historyJSON, err := json.MarshalIndent(state.History, "", "  ")
    if err != nil {
        return "", err
    }

    data := map[string]interface{}{
        "Goal":        state.Goal,
        "Workspace":   state.Workspace,
        "Step":        state.Step,
        "MaxSteps":    state.MaxSteps,
        "ToolsJSON":   string(toolsJSON),
        "HistoryJSON": string(historyJSON),
    }

    var buf bytes.Buffer
    if err := tmpl.Execute(&buf, data); err != nil {
        return "", err
    }

    return buf.String(), nil
}
```

### 4.4 Decision Parser

```go
// internal/agentloop/parse.go
package agentloop

import (
    "encoding/json"
    "fmt"
)

func ParseDecision(raw string) (Decision, error) {
    var envelope map[string]json.RawMessage
    if err := json.Unmarshal([]byte(raw), &envelope); err != nil {
        return Decision{}, fmt.Errorf("parse decision: %w", err)
    }

    var typ string
    if tBytes, ok := envelope["type"]; ok {
        if err := json.Unmarshal(tBytes, &typ); err != nil {
            return Decision{}, fmt.Errorf("parse decision.type: %w", err)
        }
    } else {
        return Decision{}, fmt.Errorf("decision missing 'type'")
    }

    switch typ {
    case "tool":
        var td ToolDecision
        if err := json.Unmarshal([]byte(raw), &td); err != nil {
            return Decision{}, fmt.Errorf("parse tool decision: %w", err)
        }
        if td.Tool == "" {
            return Decision{}, fmt.Errorf("tool decision missing 'tool'")
        }
        return Decision{Tool: &td}, nil

    case "final":
        var fd FinalDecision
        if err := json.Unmarshal([]byte(raw), &fd); err != nil {
            return Decision{}, fmt.Errorf("parse final decision: %w", err)
        }
        if fd.Answer == "" {
            return Decision{}, fmt.Errorf("final decision missing 'answer'")
        }
        return Decision{Final: &fd}, nil

    default:
        return Decision{}, fmt.Errorf("unknown decision.type %q", typ)
    }
}
```

### 4.5 Main Loop

```go
// internal/agentloop/loop.go
package agentloop

import (
    "context"
    "fmt"

    "github.com/joshka0/foxctl/internal/protocol"
)

type LoopConfig struct {
    MaxSteps int
}

type Result struct {
    FinalAnswer string
    History     []HistoryEntry
}

func RunLoop(
    ctx context.Context,
    llm LLMClient,
    invoker Invoker,
    state State,
    tools []ToolDescriptor,
    cfg LoopConfig,
) (*Result, error) {
    if cfg.MaxSteps <= 0 {
        cfg.MaxSteps = 10
    }
    state.MaxSteps = cfg.MaxSteps

    for state.Step = 1; state.Step <= cfg.MaxSteps; state.Step++ {
        // 1. Build LLM prompt
        prompt, err := BuildPrompt(state, tools)
        if err != nil {
            return nil, fmt.Errorf("build prompt: %w", err)
        }

        // 2. Call LLM
        rawResp, err := llm.Complete(ctx, prompt)
        if err != nil {
            return nil, fmt.Errorf("llm complete: %w", err)
        }

        // 3. Parse decision
        decision, err := ParseDecision(rawResp)
        if err != nil {
            // Could implement repair logic here
            return nil, fmt.Errorf("invalid agent decision: %w", err)
        }

        // 4. Handle final answer
        if decision.Final != nil {
            state.History = append(state.History, HistoryEntry{
                Kind:    "thought",
                Content: "[final] " + decision.Final.Answer,
            })
            return &Result{
                FinalAnswer: decision.Final.Answer,
                History:     state.History,
            }, nil
        }

        // 5. Handle tool call
        td := decision.Tool
        state.History = append(state.History, HistoryEntry{
            Kind:    "tool_call",
            Tool:    td.Tool,
            Params:  rawParamsToMap(td.Params),
            Content: td.Commentary,
        })

        // 6. Invoke tool
        env, err := invoker.Invoke(ctx, td.Tool, td.Params)
        if err != nil {
            state.History = append(state.History, HistoryEntry{
                Kind:      "tool_result",
                Tool:      td.Tool,
                Status:    "error",
                Content:   err.Error(),
                ErrorCode: "ERUNTIME",
            })
            continue
        }

        // 7. Summarize envelope for history
        state.History = append(state.History, summarizeEnvelope(td.Tool, env))
    }

    // Max steps reached without final answer
    return &Result{
        FinalAnswer: "",
        History:     state.History,
    }, fmt.Errorf("max steps (%d) reached without final answer", cfg.MaxSteps)
}

// Helper: convert JSON params to map
func rawParamsToMap(raw json.RawMessage) map[string]interface{} {
    if len(raw) == 0 {
        return nil
    }
    var m map[string]interface{}
    _ = json.Unmarshal(raw, &m)
    return m
}

// Helper: summarize envelope for LLM history
func summarizeEnvelope(tool string, env protocol.Envelope) HistoryEntry {
    summary := map[string]interface{}{
        "command": env.Command,
    }

    // Extract key fields from data
    if m, ok := env.Data.(map[string]interface{}); ok {
        // For artifactized responses
        if s, ok := m["summary"].(map[string]interface{}); ok {
            if code, ok := s["status_code"]; ok {
                summary["status_code"] = code
            }
            if rc, ok := s["record_count"]; ok {
                summary["record_count"] = rc
            }
        }
        // Include artifact digest
        if art, ok := m["artifact"]; ok {
            summary["artifact"] = art
        }
    }

    entry := HistoryEntry{
        Kind:    "tool_result",
        Tool:    tool,
        Status:  env.Status,
        Summary: summary,
    }

    if env.Error != nil && env.Error.Code != "" {
        entry.ErrorCode = env.Error.Code
        entry.Content = env.Error.Message
    }

    return entry
}
```

---

## 5. Complete Walkthrough

### 5.1 Scenario

**Goal**: "Scan the `./services/payments` package and identify likely flaky tests, then propose stabilization steps."

**Tools Available**:
- `fs/ls` - List files
- `fs/read` - Read files
- `text/grep` - Search for patterns

### 5.2 Step-by-Step Execution

#### Step 1: LLM Decides to List Files

**LLM sees**:
```json
{
  "goal": "Scan ./services/payments and identify flaky tests...",
  "workspace": "/home/user/repo",
  "step": 1,
  "max_steps": 6,
  "history": []
}
```

**LLM returns**:
```json
{
  "type": "tool",
  "tool": "fs/ls",
  "params": {
    "path": "services/payments",
    "max_depth": 2
  },
  "commentary": "List contents of services/payments to discover test files."
}
```

**Invoker executes**: `foxctl run fs/ls --path services/payments --max_depth 2`

**Envelope returned**:
```json
{
  "version": 1,
  "status": "ok",
  "command": "fs/ls",
  "data": {
    "summary": { "path": "services/payments", "entries": 5 },
    "preview": [
      { "path": "services/payments/handlers_test.go", "kind": "file" },
      { "path": "services/payments/refunds_test.go", "kind": "file" }
    ]
  }
}
```

**History updated**:
```json
[
  {
    "kind": "tool_call",
    "tool": "fs/ls",
    "params": { "path": "services/payments", "max_depth": 2 }
  },
  {
    "kind": "tool_result",
    "tool": "fs/ls",
    "status": "ok",
    "summary": {
      "path": "services/payments",
      "entries": 5,
      "files_sample": ["handlers_test.go", "refunds_test.go"]
    }
  }
]
```

#### Step 2: LLM Searches for Flaky Markers

**LLM returns**:
```json
{
  "type": "tool",
  "tool": "text/grep",
  "params": {
    "path": ["services/payments/**/*.go"],
    "pattern": "t\\.Skip|flaky|Flaky"
  },
  "commentary": "Search for skip markers or 'flaky' comments."
}
```

**Envelope returned**:
```json
{
  "status": "ok",
  "data": {
    "summary": { "matches": 3, "files": 2 },
    "matches": [
      {
        "path": "services/payments/handlers_test.go",
        "line": 42,
        "snippet": "t.Skip(\"flaky: depends on external service\")"
      },
      {
        "path": "services/payments/refunds_test.go",
        "line": 88,
        "snippet": "// flaky: intermittent timeout in CI"
      }
    ]
  }
}
```

#### Step 3: LLM Reads Test File

**LLM returns**:
```json
{
  "type": "tool",
  "tool": "fs/read",
  "params": {
    "path": "services/payments/refunds_test.go",
    "max_bytes": 4096
  },
  "commentary": "Inspect refunds_test.go to understand why tests are flaky."
}
```

**Envelope returned**: (preview of test code showing external gateway dependency)

#### Step 4: LLM Returns Final Answer

**LLM returns**:
```json
{
  "type": "final",
  "answer": "I identified 3 likely flaky tests in services/payments:\n\n1. TestRefunds_ExternalGateway (handlers_test.go:42) - depends on real payment gateway\n2. TestRefunds_LongRunning (refunds_test.go:88) - uses time.Sleep and real DB\n\nStabilization steps:\n1. Mock the payment gateway interface\n2. Replace time.Sleep with injected fake clock\n3. Isolate DB fixtures per test with cleanup\n4. Split into fast unit tests and slow integration tests",
  "used_tools": [
    { "tool": "fs/ls", "reason": "discover test files" },
    { "tool": "text/grep", "reason": "locate flaky markers" },
    { "tool": "fs/read", "reason": "inspect implementation" }
  ]
}
```

**Loop exits** with final answer.

### 5.3 Token Usage Analysis

**Total tokens used** (approximate):
- Prompt (goal + tools + 3 history entries): ~2KB
- LLM responses (4 tool calls + 1 final): ~1KB
- **Total**: ~3KB

**Comparison**: Traditional approach with full outputs inline would be **30-50KB**.

---

## 6. Token Efficiency

### 6.1 Where Tokens Are Saved

1. **Tool schemas**: 200 bytes vs 5KB (96% reduction)
2. **Large outputs**: Digest + summary vs full content (98%+ reduction)
3. **Repeated system prompts**: One envelope schema vs per-tool instructions (90%+ reduction)
4. **Pagination**: Automatic vs LLM-driven (100% reduction for pagination logic)

### 6.2 Estimated Savings

For a typical agent task with 10 tool calls:

| Approach | Tokens |
|----------|--------|
| Traditional (full outputs, repeated schemas) | ~200,000 |
| Foxctl substrate (envelopes + CAS) | ~8,000 |
| **Reduction** | **96%** |

### 6.3 Cost Impact

At $0.01 per 1K tokens:
- Traditional: $2.00 per task
- Foxctl: $0.08 per task
- **Savings**: $1.92 per task (25x cheaper)

---

## 7. Best Practices

### 7.1 Tool Selection

**DO**:
- Provide 5-10 focused tools
- Use descriptive names (`fs/ls`, not `list`)
- Keep input_hint simple and actionable

**DON'T**:
- Expose 50+ tools (LLM gets confused)
- Use cryptic tool names
- Include implementation details in descriptions

### 7.2 History Management

**DO**:
- Summarize envelopes to < 500 bytes each
- Keep last 5-10 history entries
- Include enough context for LLM to make decisions

**DON'T**:
- Include full envelopes in history
- Let history grow unbounded
- Omit error information from failed calls

### 7.3 Error Handling

**DO**:
- Continue after tool errors (add to history)
- Give LLM chance to retry with different approach
- Set max_steps to prevent infinite loops

**DON'T**:
- Abort on first error
- Hide error details from LLM
- Let agent run indefinitely

### 7.4 Prompt Engineering

**DO**:
- Use explicit JSON schema in prompt
- Validate LLM output structure
- Provide clear examples of valid decisions

**DON'T**:
- Ask for free-form text responses
- Mix structured and unstructured output
- Allow LLM to invent new tool names

---

## 8. Production Considerations

### 8.1 Performance Optimization

**Caching**:
```go
// Enable caching for repeated skill calls
invoker := &CachedInvoker{
    inner: baseInvoker,
    cache: lru.New(100),
}
```

**Parallelization**:
```go
// For independent tool calls, execute in parallel
// (requires LLM to emit multiple tool decisions)
var wg sync.WaitGroup
for _, toolCall := range decision.Tools {
    wg.Add(1)
    go func(tc ToolCall) {
        defer wg.Done()
        // Invoke tool
    }(toolCall)
}
wg.Wait()
```

### 8.2 Observability

**Logging**:
```go
logger.Info("agent_step",
    "step", state.Step,
    "tool", decision.Tool.Tool,
    "duration_ms", env.Meta.DurationMS,
    "status", env.Status,
)
```

**Metrics**:
```go
metrics.Histogram("agent_loop.step_duration_ms", env.Meta.DurationMS)
metrics.Counter("agent_loop.tool_calls", 1, "tool", decision.Tool.Tool)
metrics.Counter("agent_loop.errors", 1, "code", env.Error.Code)
```

**Tracing**:
```go
ctx, span := tracer.Start(ctx, "agent_loop")
defer span.End()
span.SetAttribute("goal", state.Goal)
span.SetAttribute("total_steps", state.Step)
```

### 8.3 Security

**Workspace Confinement**:
```go
// Ensure all tool calls respect workspace boundaries
invoker := &WorkspaceInvoker{
    inner:     baseInvoker,
    workspace: "/home/user/repo",
    validator: pathvalidator.New(),
}
```

**Rate Limiting**:
```go
// Prevent abuse
rateLimiter := rate.NewLimiter(rate.Limit(10), 20) // 10 calls/sec, burst 20
if !rateLimiter.Allow() {
    return fmt.Errorf("rate limit exceeded")
}
```

### 8.4 Reliability

**Retry Logic**:
```go
// Retry LLM calls on timeout/rate limit
for attempt := 0; attempt < 3; attempt++ {
    resp, err := llm.Complete(ctx, prompt)
    if err == nil {
        return resp, nil
    }
    time.Sleep(time.Duration(1<<attempt) * time.Second)
}
```

**Circuit Breaker**:
```go
// Stop calling failing skills
if errorRate > 0.5 {
    return fmt.Errorf("skill %s circuit breaker open", skill)
}
```

---

## Appendix A: Full Example Code

See `internal/agentloop/` for complete reference implementation with:
- `types.go` - Core types
- `interfaces.go` - LLM and Invoker interfaces
- `prompt.go` - Prompt template
- `parse.go` - Decision parser
- `loop.go` - Main loop logic
- `invoker_cli.go` - CLI-based invoker implementation
- `invoker_direct.go` - Direct Go invoker implementation

## Appendix B: Integration Examples

### B.1 OpenAI Integration

```go
type OpenAIClient struct {
    client *openai.Client
    model  string
}

func (c *OpenAIClient) Complete(ctx context.Context, prompt string) (string, error) {
    resp, err := c.client.CreateChatCompletion(ctx, openai.ChatCompletionRequest{
        Model: c.model,
        Messages: []openai.ChatCompletionMessage{
            {
                Role:    "system",
                Content: "You are a helpful agent that uses tools via JSON.",
            },
            {
                Role:    "user",
                Content: prompt,
            },
        },
        Temperature: 0.7,
    })
    if err != nil {
        return "", err
    }
    return resp.Choices[0].Message.Content, nil
}
```

### B.2 Anthropic Integration

```go
type AnthropicClient struct {
    client *anthropic.Client
    model  string
}

func (c *AnthropicClient) Complete(ctx context.Context, prompt string) (string, error) {
    resp, err := c.client.Messages.Create(ctx, anthropic.MessageCreateParams{
        Model:     anthropic.String(c.model),
        MaxTokens: anthropic.Int(4096),
        Messages: []anthropic.MessageParam{
            anthropic.NewUserMessage(anthropic.NewTextBlock(prompt)),
        },
    })
    if err != nil {
        return "", err
    }
    return resp.Content[0].Text, nil
}
```

---

**Document Status**: Implementation Guide
**Related Specs**: Protocol v1, Core Profile v1, OpenAPI Skill
