# foxctl V2 Workflow Primitives Specification

This document defines the durable primitive model for workflow steps in `foxctl` and their mapping to Eino concepts. These primitives establish the boundary contract for current and future Eino-backed engine integration.

## 1. Model Primitive

**Definition:** The core reasoning component responsible for processing conversation history and generating responses or tool calls.

*   **foxctl Seam:** `runner.Model` interface (V2) and `engine.AgentEngine` interface (Classic).
*   **Eino Mapping:** `components/model.ChatModel` and `components/model.ToolCallingChatModel`.
*   **Boundary Contract:** 
    *   Accepts conversation history and system instructions.
    *   Outputs assistant text, tool calls, and usage statistics.
    *   Must remain stateless; history and state are managed by the Memory primitive.

## 2. Tool Primitive

**Definition:** A discrete, executable action available to an agent.

*   **foxctl Seam:** `engine.ToolDef`, `engine.ToolCall`, and `engine.ToolResult`.
*   **Eino Mapping:** `components/tool.InvokableTool`.
*   **Boundary Contract:**
    *   `ToolDef` provides metadata (name, description, JSON schema) used for Eino `ToolInfo`.
    *   `ToolExecutor` handles the actual execution, isolated from the engine reasoning loop.
    *   Bridged via `einoToolBridge` adapter which handles type conversion between Eino and foxctl internal schemas.

## 3. Memory & Context Primitive

**Definition:** The substrate for persisting conversation history and retrieving relevant external facts.

*   **foxctl Seam:** `internal/storage/conversation_memory.go`, `internal/storage/contextvar`, and `internal/v2/runtime/contextbuilder`.
*   **Eino Mapping:** `adk.ConversationBuffer` (for short-term memory) and custom graph nodes for `contextbuilder` (for long-term or external context).
*   **Boundary Contract:**
    *   Manages the "Pyramid of Memory" (Temporal Views: L0 Vivid, L1 Recent, L2 Landmark).
    *   Ensures storage canonicality; engine-backed paths (like Eino) must not bypass the existing storage ownership model.
    *   Context is assembled into prompt fragments before being passed to the Model primitive.

## 4. Handoff & Subcall Primitive

**Definition:** The mechanism for multi-agent coordination and task delegation.

*   **foxctl Seam:** `SpawnService.SpawnChild`, `RuntimeSpawner`, and `internal/storage/coordination`.
*   **Eino Mapping:** Agent-as-a-Tool (wrapping an `adk.Agent` as a `tool.InvokableTool`).
*   **Boundary Contract:**
    *   Maintains parent/child relationships and the Go-owned worker registry.
    *   Handoffs are modeled as tool calls where the result is the outcome of the child agent's turn.
    *   Ensures policy enforcement (spawn depth, quota management) remains in the Go-native orchestration layer.

## Summary of Mappings

| Primitive | foxctl Type | Eino Mapping |
| :--- | :--- | :--- |
| **Model** | `runner.Model` / `engine.AgentEngine` | `model.ChatModel` / `adk.ChatModelAgent` |
| **Tool** | `engine.ToolDef` / `engine.ToolExecutor` | `tool.InvokableTool` |
| **Memory** | `contextbuilder.Builder` / `contextvar.Store` | `adk.ConversationBuffer` / Graph Nodes |
| **Handoff** | `spawn.Request` / `RuntimeSpawner` | `tool.InvokableTool` (Agent-as-a-Tool) |
