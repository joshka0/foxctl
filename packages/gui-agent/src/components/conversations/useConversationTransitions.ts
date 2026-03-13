import { useCallback, useEffect } from "react";
import type React from "react";
import {
  type CompanionMemoryStats,
  createConsoleSession,
  getCompanionConversationMessages,
  getCompanionMemoryStats,
  getCompanionPersonality,
  getSessionMessages,
  renameCompanionConversation,
  type ConsoleMessage,
  type PersistedSession,
  type PersonalityInfo,
} from "@/api/client";
import type { Agent } from "@/api/types";
import { getAgentDisplayName } from "@/lib/agent-utils";
import { matchAgentToConversation } from "@/lib/conversation-utils";
import type { Conversation } from "@/lib/conversation-list-models";
import {
  buildHistoricalFollowUpPrompt,
  getSessionMessageContent,
} from "@/lib/conversation-session-utils";
import type { ContextInfo } from "@/components/conversations/types";

interface UseConversationTransitionsParams {
  selectedConversationID: string | null;
  setSelectedConversationID: (value: string | null) => void;
  selectedConversation: Conversation | null;
  setSelectedConversation: React.Dispatch<
    React.SetStateAction<Conversation | null>
  >;
  selectedPersistedSession: PersistedSession | null;
  setSelectedPersistedSession: React.Dispatch<
    React.SetStateAction<PersistedSession | null>
  >;
  conversations: Conversation[];
  agents: Agent[];
  setLinkedAgent: React.Dispatch<React.SetStateAction<Agent | null>>;
  messages: ConsoleMessage[];
  setMessages: React.Dispatch<React.SetStateAction<ConsoleMessage[]>>;
  setContextInfo: React.Dispatch<React.SetStateAction<ContextInfo>>;
  setPersonalityInfo: React.Dispatch<
    React.SetStateAction<PersonalityInfo | null>
  >;
  setMemoryStats: React.Dispatch<
    React.SetStateAction<CompanionMemoryStats | null>
  >;
  setMemoryContext: React.Dispatch<React.SetStateAction<string | null>>;
  setShowMemoryContext: React.Dispatch<React.SetStateAction<boolean>>;
  setIsLoadingMessages: React.Dispatch<React.SetStateAction<boolean>>;
  setExpandedAgents: React.Dispatch<React.SetStateAction<Set<string>>>;
  setSelectedAgentForNew: React.Dispatch<React.SetStateAction<string>>;
  pendingLinkedAgentRef: React.MutableRefObject<Agent | null>;
  beginConversationLoad: (options?: {
    preserveLinkedAgent?: boolean;
    preserveContextInfo?: boolean;
    preserveInflight?: boolean;
  }) => void;
  activateConsoleSession: (
    sessionData: {
      session: {
        id: string;
        workspace: string;
        profile: string;
        created: string;
        last_activity?: string;
      };
    },
    nextContext?: Partial<ContextInfo>,
  ) => void;
  refetch: () => Promise<unknown>;
  toolModel: string;
  responseModel: string;
}

export function useConversationTransitions({
  selectedConversationID,
  setSelectedConversationID,
  selectedConversation,
  setSelectedConversation,
  selectedPersistedSession,
    setSelectedPersistedSession,
    conversations,
    agents,
    setLinkedAgent,
    messages,
  setMessages,
  setContextInfo,
  setPersonalityInfo,
  setMemoryStats,
  setMemoryContext,
  setShowMemoryContext,
  setIsLoadingMessages,
  setExpandedAgents,
  setSelectedAgentForNew,
  pendingLinkedAgentRef,
  beginConversationLoad,
  activateConsoleSession,
  refetch,
  toolModel,
  responseModel,
}: UseConversationTransitionsParams) {
  const handleSelectConversation = useCallback(
    async (conversation: Conversation) => {
      if (selectedConversation?.id === conversation.id) return;

      setSelectedConversationID(conversation.id);
      setSelectedPersistedSession(null);
      setSelectedConversation(conversation);
      beginConversationLoad();

      try {
        const messagesData = await getCompanionConversationMessages(
          conversation.id,
          200,
        );
        const consoleMessages: ConsoleMessage[] = messagesData.messages.map(
          (message) => ({
            id: message.id,
            role: message.role as "user" | "assistant",
            content: message.content,
            timestamp: message.created_at,
            tool_calls: message.tool_calls?.map((toolCall) => ({
              name: toolCall.name,
              input: toolCall.arguments as Record<string, unknown>,
              status: "completed" as const,
            })),
          }),
        );
        setMessages(consoleMessages);

        const matchedAgent = matchAgentToConversation(conversation, agents);
        if (matchedAgent) {
          setContextInfo({
            workspace: matchedAgent.ns || "/",
            profile: "agent",
            createdAt: conversation.created_at,
          });
        } else {
          const sessionData = await createConsoleSession({
            workspace: "/",
            profile: "companion",
            conversation_id: conversation.id,
          });
          activateConsoleSession(sessionData);
        }

        try {
          const personality = await getCompanionPersonality(conversation.id);
          setPersonalityInfo(personality);
          setContextInfo((prev) => ({
            ...prev,
            systemPrompt: personality.system_prompt,
          }));
        } catch (personalityErr) {
          console.warn("Failed to load personality info:", personalityErr);
        }

        try {
          const stats = await getCompanionMemoryStats(conversation.id);
          setMemoryStats(stats);
        } catch {
          setMemoryStats(null);
        }
        setMemoryContext(null);
        setShowMemoryContext(false);
      } catch (err) {
        console.error("Failed to load conversation:", err);
      } finally {
        setIsLoadingMessages(false);
      }
    },
    [
      activateConsoleSession,
      agents,
      beginConversationLoad,
      selectedConversation?.id,
      setContextInfo,
      setIsLoadingMessages,
      setMemoryContext,
      setMemoryStats,
      setMessages,
      setPersonalityInfo,
      setSelectedConversation,
      setSelectedConversationID,
      setSelectedPersistedSession,
      setShowMemoryContext,
    ],
  );

  useEffect(() => {
    if (
      !selectedConversationID ||
      conversations.length === 0 ||
      selectedPersistedSession
    ) {
      return;
    }
    if (selectedConversation?.id === selectedConversationID) {
      return;
    }
    const conversation = conversations.find(
      (candidate) => candidate.id === selectedConversationID,
    );
    if (conversation) {
      void handleSelectConversation(conversation);
    }
  }, [
    conversations,
    handleSelectConversation,
    selectedConversation?.id,
    selectedConversationID,
    selectedPersistedSession,
  ]);

  const handleSelectSession = useCallback(
    async (persisted: PersistedSession) => {
      if (selectedPersistedSession?.id === persisted.id) return;

      setSelectedConversationID(null);
      setSelectedPersistedSession(persisted);
      setSelectedConversation(null);
      beginConversationLoad();
      setContextInfo({
        workspace: persisted.workspace_path || "/",
        profile: "historical",
        createdAt: persisted.started_at,
        lastActivity: persisted.ended_at || persisted.started_at,
      });

      try {
        const messagesData = await getSessionMessages(persisted.id, {
          limit: 200,
        });
        const consoleMessages: ConsoleMessage[] = messagesData.messages
          .filter(
            (message) =>
              message.type === "user" ||
              message.type === "assistant" ||
              message.type === "human",
          )
          .map((message) => ({
            role:
              message.type === "human"
                ? "user"
                : (message.type as "user" | "assistant"),
            content: getSessionMessageContent(message),
            timestamp:
              message.timestamp ||
              persisted.started_at ||
              new Date().toISOString(),
          }));
        setMessages(consoleMessages);
      } catch (err) {
        console.error("Failed to load persisted session:", err);
        setMessages([
          {
            role: "assistant",
            content: `Error loading historical session: ${err instanceof Error ? err.message : String(err)}`,
            timestamp: new Date().toISOString(),
          },
        ]);
      } finally {
        setIsLoadingMessages(false);
      }
    },
    [
      beginConversationLoad,
      selectedPersistedSession?.id,
      setContextInfo,
      setIsLoadingMessages,
      setMessages,
      setSelectedConversation,
      setSelectedConversationID,
      setSelectedPersistedSession,
    ],
  );

  const handleNewConversation = useCallback(async () => {
    setSelectedConversationID(null);
    setSelectedConversation(null);
    setSelectedPersistedSession(null);
    beginConversationLoad();

    try {
      const sessionData = await createConsoleSession({
        workspace: "/",
        profile: "companion",
        tool_model: toolModel,
        response_model: responseModel,
      });
      activateConsoleSession(sessionData);

      const newConversation: Conversation = {
        id: sessionData.session.id,
        title: "New Conversation",
        created_at: new Date().toISOString(),
        updated_at: new Date().toISOString(),
        message_count: 0,
      };
      setSelectedConversation(newConversation);
      setSelectedConversationID(newConversation.id);
    } catch (err) {
      console.error("Failed to create new conversation:", err);
    } finally {
      setIsLoadingMessages(false);
    }
  }, [
    activateConsoleSession,
    beginConversationLoad,
    responseModel,
    setIsLoadingMessages,
    setSelectedConversation,
    setSelectedConversationID,
    setSelectedPersistedSession,
    toolModel,
  ]);

  const handleNewConversationWithAgent = useCallback(
    async (agent: Agent) => {
      setSelectedConversationID(null);
      setSelectedConversation(null);
      setSelectedPersistedSession(null);
      setLinkedAgent(agent);
      beginConversationLoad({ preserveLinkedAgent: true });
      pendingLinkedAgentRef.current = agent;

      try {
        const sessionData = await createConsoleSession({
          workspace: agent.ns || "/",
          profile: "companion",
          system_prompt: `You are chatting in the context of an agent session.

Agent Details:
- Name: ${getAgentDisplayName(agent)}
- Role: ${agent.role || "N/A"}
- ID: ${agent.id}
- Workspace: ${agent.ns || "/"}
- Model: ${agent.llm_model || "default"}
- State: ${agent.state}

Help the user understand and interact with this agent's work.`,
          tool_model: toolModel,
          response_model: responseModel,
        });
        activateConsoleSession(sessionData);

        const newConversation: Conversation = {
          id: sessionData.session.id,
          title: `Chat with ${getAgentDisplayName(agent)}`,
          created_at: new Date().toISOString(),
          updated_at: new Date().toISOString(),
          message_count: 0,
        };
        setSelectedConversation(newConversation);
        setSelectedConversationID(newConversation.id);

        try {
          await renameCompanionConversation(
            sessionData.session.id,
            `Chat with ${getAgentDisplayName(agent)}`,
            agent.id,
          );
          await refetch();
          pendingLinkedAgentRef.current = null;
        } catch (linkErr) {
          console.error("Failed to link conversation to agent:", linkErr);
        }

        setExpandedAgents((prev) => {
          const next = new Set(prev);
          next.add(agent.id);
          return next;
        });
        setSelectedAgentForNew("");
      } catch (err) {
        console.error("Failed to create new conversation with agent:", err);
      } finally {
        setIsLoadingMessages(false);
      }
    },
    [
      activateConsoleSession,
      beginConversationLoad,
      pendingLinkedAgentRef,
      refetch,
      responseModel,
      setExpandedAgents,
      setIsLoadingMessages,
      setLinkedAgent,
      setSelectedAgentForNew,
      setSelectedConversation,
      setSelectedConversationID,
      setSelectedPersistedSession,
      toolModel,
    ],
  );

  const handleStartHistoricalFollowUp = useCallback(async () => {
    if (!selectedPersistedSession) return;

    const historical = selectedPersistedSession;
    const systemPrompt = buildHistoricalFollowUpPrompt(historical, messages);

    setSelectedConversationID(null);
    setSelectedConversation(null);
    setSelectedPersistedSession(null);
    beginConversationLoad();

    try {
      const sessionData = await createConsoleSession({
        workspace: historical.workspace_path || "/",
        profile: "companion",
        system_prompt: systemPrompt,
        tool_model: toolModel,
        response_model: responseModel,
      });
      activateConsoleSession(sessionData, { systemPrompt });

      const titleBase =
        historical.project_name ||
        historical.workspace_path.split("/").pop() ||
        "Historical Session";
      const newConversation: Conversation = {
        id: sessionData.session.id,
        title: `Follow-up: ${titleBase}`,
        created_at: new Date().toISOString(),
        updated_at: new Date().toISOString(),
        message_count: 0,
      };
      setSelectedConversation(newConversation);
      setSelectedConversationID(newConversation.id);
    } catch (err) {
      console.error("Failed to start historical follow-up:", err);
    } finally {
      setIsLoadingMessages(false);
    }
  }, [
    activateConsoleSession,
    beginConversationLoad,
    messages,
    responseModel,
    selectedPersistedSession,
    setIsLoadingMessages,
    setSelectedConversation,
    setSelectedConversationID,
    setSelectedPersistedSession,
    toolModel,
  ]);

  return {
    handleSelectConversation,
    handleSelectSession,
    handleNewConversation,
    handleNewConversationWithAgent,
    handleStartHistoricalFollowUp,
  };
}
