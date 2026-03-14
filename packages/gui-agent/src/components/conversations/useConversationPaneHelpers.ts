import { useCallback } from "react";
import type React from "react";
import type {
  CompanionCompressionResult,
  CompanionMemoryStats,
  ConsoleMessage,
  ConsoleSession,
  PersonalityInfo,
} from "@/api/client";
import type { Agent } from "@/api/types";
import type { ContextInfo } from "@/components/conversations/types";

interface UseConversationPaneHelpersParams {
  eventSourceRef: React.MutableRefObject<EventSource | null>;
  setLinkedAgent: React.Dispatch<React.SetStateAction<Agent | null>>;
  setInflight: React.Dispatch<React.SetStateAction<boolean>>;
  setMessages: React.Dispatch<React.SetStateAction<ConsoleMessage[]>>;
  setSessionId: React.Dispatch<React.SetStateAction<string | null>>;
  setSession: React.Dispatch<React.SetStateAction<ConsoleSession | null>>;
  setContextInfo: React.Dispatch<React.SetStateAction<ContextInfo>>;
  setPersonalityInfo: React.Dispatch<
    React.SetStateAction<PersonalityInfo | null>
  >;
  setSelectedMessage: React.Dispatch<
    React.SetStateAction<ConsoleMessage | null>
  >;
  setShowContextPanel: React.Dispatch<React.SetStateAction<boolean>>;
  setIsCompressing: React.Dispatch<React.SetStateAction<boolean>>;
  setLastCompression: React.Dispatch<
    React.SetStateAction<CompanionCompressionResult | null>
  >;
  setMemoryStats: React.Dispatch<
    React.SetStateAction<CompanionMemoryStats | null>
  >;
  setMemoryContext: React.Dispatch<React.SetStateAction<string | null>>;
  setShowMemoryContext: React.Dispatch<React.SetStateAction<boolean>>;
  setIsLoadingMessages: React.Dispatch<React.SetStateAction<boolean>>;
}

export function useConversationPaneHelpers({
  eventSourceRef,
  setLinkedAgent,
  setInflight,
  setMessages,
  setSessionId,
  setSession,
  setContextInfo,
  setPersonalityInfo,
  setSelectedMessage,
  setShowContextPanel,
  setIsCompressing,
  setLastCompression,
  setMemoryStats,
  setMemoryContext,
  setShowMemoryContext,
  setIsLoadingMessages,
}: UseConversationPaneHelpersParams) {
  const closeEventStream = useCallback(() => {
    if (eventSourceRef.current) {
      eventSourceRef.current.close();
      eventSourceRef.current = null;
    }
  }, [eventSourceRef]);

  const resetConversationPane = useCallback(
    (options?: {
      preserveLinkedAgent?: boolean;
      preserveContextInfo?: boolean;
      preserveInflight?: boolean;
    }) => {
      if (!options?.preserveLinkedAgent) {
        setLinkedAgent(null);
      }
      if (!options?.preserveInflight) {
        setInflight(false);
      }
      setMessages([]);
      setSessionId(null);
      setSession(null);
      if (!options?.preserveContextInfo) {
        setContextInfo({});
      }
      setPersonalityInfo(null);
      setSelectedMessage(null);
      setShowContextPanel(false);
      setIsCompressing(false);
      setLastCompression(null);
      setMemoryStats(null);
      setMemoryContext(null);
      setShowMemoryContext(false);
    },
    [
      setContextInfo,
      setInflight,
      setIsCompressing,
      setLastCompression,
      setLinkedAgent,
      setMemoryContext,
      setMemoryStats,
      setMessages,
      setPersonalityInfo,
      setSelectedMessage,
      setSession,
      setSessionId,
      setShowContextPanel,
      setShowMemoryContext,
    ],
  );

  const beginConversationLoad = useCallback(
    (options?: {
      preserveLinkedAgent?: boolean;
      preserveContextInfo?: boolean;
      preserveInflight?: boolean;
    }) => {
      closeEventStream();
      setIsLoadingMessages(true);
      resetConversationPane(options);
    },
    [closeEventStream, resetConversationPane, setIsLoadingMessages],
  );

  const activateConsoleSession = useCallback(
    (
      sessionData: { session: ConsoleSession },
      nextContext?: Partial<ContextInfo>,
    ) => {
      setSessionId(sessionData.session.id);
      setSession(sessionData.session);
      setContextInfo({
        workspace: sessionData.session.workspace,
        profile: sessionData.session.profile,
        createdAt: sessionData.session.created,
        lastActivity: sessionData.session.last_activity,
        ...nextContext,
      });
    },
    [setContextInfo, setSession, setSessionId],
  );

  return {
    closeEventStream,
    resetConversationPane,
    beginConversationLoad,
    activateConsoleSession,
  };
}
