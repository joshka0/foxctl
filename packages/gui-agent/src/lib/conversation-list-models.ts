import type { PersistedSession } from "../api/client";
import type { Agent } from '@foxctl/data/types';
import {
  getAgentDisplayName,
  isWorkerAgent,
} from "./agent-utils";
import { matchAgentToConversation } from "./conversation-utils";

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

function conversationSortTimestamp(conversation: Conversation): number {
  return (
    Date.parse(conversation.updated_at || "") ||
    Date.parse(conversation.created_at || "") ||
    0
  );
}

function matchesConversationSearch(
  conversation: Conversation,
  searchQuery: string,
  agents: Agent[],
): boolean {
  if (!searchQuery) return true;

  const lower = searchQuery.trim().toLowerCase();
  const matchedAgent = matchAgentToConversation(conversation, agents);
  const values = [
    conversation.id,
    conversation.title,
    conversation.name,
    matchedAgent ? getAgentDisplayName(matchedAgent) : undefined,
    matchedAgent?.slug,
    matchedAgent?.role,
    matchedAgent?.state,
  ];

  return values.some((value) => (value || "").toLowerCase().includes(lower));
}

export function agentOptionLabel(agent: Agent): string {
  return `${getAgentDisplayName(agent)} · #${shortAgentID(agent.id)} · ${
    agent.role || "agent"
  }`;
}

export function filterConversationsBySearch(
  conversations: Conversation[],
  searchQuery: string,
  agents: Agent[],
): Conversation[] {
  return conversations.filter((conversation) =>
    matchesConversationSearch(conversation, searchQuery, agents),
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

  const sortedGroups = Array.from(agentGroups.values())
    .map((group) => ({
      ...group,
      conversations: [...group.conversations].sort(
        (left, right) =>
          conversationSortTimestamp(right) - conversationSortTimestamp(left),
      ),
    }))
    .sort((left, right) => {
      const leftLatest = left.conversations[0];
      const rightLatest = right.conversations[0];
      const byRecent =
        conversationSortTimestamp(rightLatest) -
        conversationSortTimestamp(leftLatest);
      if (byRecent !== 0) return byRecent;
      return getAgentDisplayName(left.agent).localeCompare(
        getAgentDisplayName(right.agent),
      );
    });

  return {
    agentGroups: sortedGroups,
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
    if (!matchesConversationSearch(conversation, searchQuery, agents)) {
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
