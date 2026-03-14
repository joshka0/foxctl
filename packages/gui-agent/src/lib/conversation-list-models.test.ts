import { describe, expect, test } from "bun:test";
import type { PersistedSession } from "@/api/client";
import type { Agent } from "@/api/types";
import {
  buildFeedItems,
  buildGroupedConversations,
  filterConversationsBySearch,
  type Conversation,
} from "./conversation-list-models";

function makeAgent(overrides: Partial<Agent> = {}): Agent {
  return {
    id: "agent-1",
    ns: "/workspace",
    skills_allow: [],
    share_bb: "scoped",
    state: "running",
    created_at: "2026-03-13T09:00:00Z",
    ...overrides,
  };
}

function makeConversation(
  id: string,
  overrides: Partial<Conversation> = {},
): Conversation {
  return {
    id,
    created_at: "2026-03-13T09:00:00Z",
    updated_at: "2026-03-13T09:05:00Z",
    message_count: 1,
    ...overrides,
  };
}

function makePersistedSession(
  id: string,
  overrides: Partial<PersistedSession> = {},
): PersistedSession {
  return {
    id,
    agent_id: "",
    workspace_path: "/workspace",
    project_name: "Project",
    summary: "",
    status: "completed",
    started_at: "2026-03-13T09:00:00Z",
    ended_at: "2026-03-13T09:10:00Z",
    message_count: 3,
    user_turns: 1,
    tool_invocations: 0,
    total_tokens: 128,
    ...overrides,
  };
}

describe("conversation-list-models", () => {
  test("filterConversationsBySearch matches linked agent metadata", () => {
    const agents = [
      makeAgent({
        id: "agent-atlas",
        name: "Atlas",
        role: "coder",
        state: "running",
        conversation_id: "conv-atlas",
      }),
    ];
    const conversations = [
      makeConversation("conv-atlas", {
        title: "Design follow-up",
      }),
    ];

    expect(filterConversationsBySearch(conversations, "atlas", agents)).toEqual(
      conversations,
    );
    expect(
      filterConversationsBySearch(conversations, "running", agents),
    ).toEqual(conversations);
    expect(filterConversationsBySearch(conversations, "missing", agents)).toHaveLength(
      0,
    );
  });

  test("buildGroupedConversations sorts groups and conversations by recency", () => {
    const agents = [
      makeAgent({
        id: "agent-new",
        name: "Nova",
        conversation_id: "conv-new",
      }),
      makeAgent({
        id: "agent-old",
        name: "Orion",
        conversation_id: "conv-old",
      }),
      makeAgent({
        id: "agent-selected",
        name: "Selene",
      }),
    ];
    const filteredConversations = [
      makeConversation("conv-old", {
        updated_at: "2026-03-13T09:05:00Z",
      }),
      makeConversation("conv-new", {
        updated_at: "2026-03-13T11:05:00Z",
      }),
      makeConversation("conv-new-earlier", {
        agent_id: "agent-new",
        updated_at: "2026-03-13T10:00:00Z",
      }),
    ];
    const selectedConversation = makeConversation("conv-selected", {
      updated_at: "2026-03-13T08:30:00Z",
    });

    const result = buildGroupedConversations({
      filteredConversations,
      agents,
      selectedConversation,
      linkedAgent: agents[2],
    });

    expect(result.agentGroups.map((group) => group.agent.id)).toEqual([
      "agent-new",
      "agent-old",
      "agent-selected",
    ]);
    expect(
      result.agentGroups.find((group) => group.agent.id === "agent-new")
        ?.conversations.map((conversation) => conversation.id),
    ).toEqual(["conv-new", "conv-new-earlier"]);
    expect(
      result.agentGroups.find((group) => group.agent.id === "agent-selected")
        ?.conversations.map((conversation) => conversation.id),
    ).toEqual(["conv-selected"]);
  });

  test("buildFeedItems excludes empty and agent-owned history while preserving recency order", () => {
    const agents = [
      makeAgent({
        id: "agent-atlas",
        name: "Atlas",
        conversation_id: "conv-atlas",
      }),
    ];
    const conversations = [
      makeConversation("conv-atlas", {
        updated_at: "2026-03-13T11:00:00Z",
        message_count: 4,
      }),
      makeConversation("conv-empty", {
        updated_at: "2026-03-13T12:00:00Z",
        message_count: 0,
      }),
    ];
    const persistedSessions = [
      makePersistedSession("session-unlinked", {
        started_at: "2026-03-13T10:30:00Z",
      }),
      makePersistedSession("session-linked", {
        agent_id: "agent-atlas",
        started_at: "2026-03-13T12:30:00Z",
      }),
    ];

    const items = buildFeedItems({
      searchQuery: "",
      agents,
      conversations,
      persistedSessions,
    });

    expect(items.map((item) => item.kind)).toEqual(["companion", "session"]);
    expect(items[0]?.kind).toBe("companion");
    expect(items[0]?.kind === "companion" ? items[0].conversation.id : "").toBe(
      "conv-atlas",
    );
    expect(items[1]?.kind).toBe("session");
    expect(items[1]?.kind === "session" ? items[1].session.id : "").toBe(
      "session-unlinked",
    );
  });
});
