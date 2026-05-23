import type { Agent } from '@foxctl/data/types';

export interface ConversationLinkRecord {
  id: string;
  agent_id?: string;
  updated_at?: string;
  created_at?: string;
}

function conversationSortTimestamp(conversation: ConversationLinkRecord): number {
  return (
    Date.parse(conversation.updated_at || "") ||
    Date.parse(conversation.created_at || "") ||
    0
  );
}

export function resolveConversationIDForAgent(
  agent: Pick<Agent, "id" | "conversation_id">,
  conversations: ConversationLinkRecord[] = [],
): string {
  const linked = (agent.conversation_id || "").trim();
  if (linked) return linked;

  const conversationSideLink = [...conversations]
    .filter((conversation) => conversation.agent_id === agent.id)
    .sort((a, b) => conversationSortTimestamp(b) - conversationSortTimestamp(a))[0];

  if (conversationSideLink?.id) return conversationSideLink.id;
  return agent.id;
}

export function matchAgentToConversation(
  conversation: ConversationLinkRecord | null | undefined,
  agents: Agent[],
  fallbackAgent?: Agent | null,
): Agent | null {
  if (!conversation) return fallbackAgent ?? null;

  if (conversation.agent_id) {
    const byConversationAgentID = agents.find(
      (agent) => agent.id === conversation.agent_id,
    );
    if (byConversationAgentID) return byConversationAgentID;
  }

  const byAgentConversationID = agents.find(
    (agent) =>
      agent.conversation_id === conversation.id || agent.id === conversation.id,
  );

  return byAgentConversationID || fallbackAgent || null;
}

export function findAgentForConversationID(
  conversationID: string,
  conversations: ConversationLinkRecord[],
  agents: Agent[],
): Agent | null {
  const conversation = conversations.find(
    (candidate) => candidate.id === conversationID,
  );
  return matchAgentToConversation(conversation, agents);
}
