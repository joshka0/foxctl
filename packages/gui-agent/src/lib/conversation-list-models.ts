import type { PersistedSession } from "@/api/client";
import type { Agent } from "@/api/types";
import {
  getAgentDisplayName,
  isWorkerAgent,
} from "@/lib/agent-utils";
import { matchAgentToConversation } from "@/lib/conversation-utils";

export interface Conversation {
  id: string;
  title?: string;
  name?: string;
  agent_id?: string;
  created_at: string;
  updated_at: string;
  message_count: number;
}

export type FeedItem =
  | {
      kind: "companion";
      conversation: Conversation;
      agent?: Agent;
      sortAt: number;
    }
  | {
      kind: "session";
      session: PersistedSession;
      sortAt: number;
    };

export interface AgentConversationGroupData {
  agent: Agent;
  conversations: Conversation[];
}

export interface AgentSections {
  active: Agent[];
  errored: Agent[];
}

export const shortAgentID = (id: string) => id.slice(0, 8);

export function agentOptionLabel(agent: Agent): string {
  return `${getAgentDisplayName(agent)} · #${shortAgentID(agent.id)} · ${
    agent.role || "agent"
  }`;
}

export function filterConversationsBySearch(
  conversations: Conversation[],
  searchQuery: string,
): Conversation[] {
  if (!searchQuery) return conversations;
  const lower = searchQuery.toLowerCase();
  return conversations.filter(
    (conversation) =>
      conversation.id.toLowerCase().includes(lower) ||
      conversation.title?.toLowerCase().includes(lower),
  );
}

export function filterAgentsBySearch(
  agents: Agent[],
  searchQuery: string,
): Agent[] {
  if (!searchQuery) return agents;
  const lower = searchQuery.toLowerCase();
  return agents.filter(
    (agent) =>
      (agent.name || "").toLowerCase().includes(lower) ||
      (agent.slug || "").toLowerCase().includes(lower) ||
      (agent.role || "").toLowerCase().includes(lower) ||
      agent.id.toLowerCase().includes(lower),
  );
}

export function buildGroupedConversations(params: {
  filteredConversations: Conversation[];
  agents: Agent[];
  selectedConversation: Conversation | null;
  linkedAgent: Agent | null;
}): { agentGroups: AgentConversationGroupData[] } {
  const { filteredConversations, agents, selectedConversation, linkedAgent } =
    params;
  const agentGroups: Map<string, AgentConversationGroupData> = new Map();
  const knownIds = new Set(
    filteredConversations.map((conversation) => conversation.id),
  );
  const allConversations = [...filteredConversations];

  if (selectedConversation && !knownIds.has(selectedConversation.id)) {
    allConversations.push(selectedConversation);
  }

  const localLinkedConvId =
    selectedConversation && linkedAgent ? selectedConversation.id : null;

  for (const conversation of allConversations) {
    const matchedAgent = matchAgentToConversation(
      conversation,
      agents,
      localLinkedConvId === conversation.id ? linkedAgent : null,
    );
    if (!matchedAgent) continue;

    if (!agentGroups.has(matchedAgent.id)) {
      agentGroups.set(matchedAgent.id, {
        agent: matchedAgent,
        conversations: [],
      });
    }
    agentGroups.get(matchedAgent.id)!.conversations.push(conversation);
  }

  return {
    agentGroups: Array.from(agentGroups.values()),
  };
}

export function buildAgentSections(agents: Agent[]): AgentSections {
  const sections: AgentSections = {
    active: [],
    errored: [],
  };

  for (const agent of agents) {
    if (isWorkerAgent(agent)) continue;
    const state = (agent.state || "").toLowerCase();
    if (state === "running" || state === "idle") {
      sections.active.push(agent);
      continue;
    }
    if (state === "error") {
      sections.errored.push(agent);
    }
  }

  return sections;
}

export function buildFeedItems(params: {
  searchQuery: string;
  agents: Agent[];
  conversations: Conversation[];
  persistedSessions: PersistedSession[];
}): FeedItem[] {
  const { searchQuery, agents, conversations, persistedSessions } = params;
  const lowerQuery = searchQuery.trim().toLowerCase();
  const matchesQuery = (...values: Array<string | undefined>): boolean => {
    if (!lowerQuery) return true;
    return values.some((value) =>
      (value || "").toLowerCase().includes(lowerQuery),
    );
  };

  const knownAgentIDs = new Set(agents.map((agent) => agent.id));
  const items: FeedItem[] = [];

  for (const conversation of conversations) {
    if (conversation.message_count <= 0) continue;
    const agent = matchAgentToConversation(conversation, agents) || undefined;
    if (
      !matchesQuery(
        conversation.id,
        conversation.name,
        conversation.title,
        agent ? getAgentDisplayName(agent) : undefined,
      )
    ) {
      continue;
    }
    items.push({
      kind: "companion",
      conversation,
      agent,
      sortAt: Date.parse(conversation.updated_at) || 0,
    });
  }

  for (const persisted of persistedSessions) {
    if (persisted.message_count <= 0) continue;
    if (persisted.agent_id && knownAgentIDs.has(persisted.agent_id)) continue;
    if (
      !matchesQuery(
        persisted.id,
        persisted.project_name,
        persisted.workspace_path,
        persisted.summary,
        persisted.status,
      )
    ) {
      continue;
    }
    items.push({
      kind: "session",
      session: persisted,
      sortAt: Date.parse(persisted.started_at) || 0,
    });
  }

  items.sort((a, b) => b.sortAt - a.sortAt);
  return items;
}
