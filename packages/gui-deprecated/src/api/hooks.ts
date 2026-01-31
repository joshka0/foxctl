import { useQuery, useQueryClient, useMutation } from "@tanstack/react-query";
import { useEffect, useRef, useState, useCallback } from "react";
import * as api from "./client";

// Query keys
export const queryKeys = {
  jobs: (params?: { state?: string; limit?: number }) => ["jobs", params] as const,
  jobDetail: (id: string) => ["jobs", id] as const,
  tasks: (params?: { limit?: number; workspace?: string }) => ["tasks", params] as const,
  agents: (params?: { state?: string; limit?: number }) => ["agents", params] as const,
  stats: () => ["stats"] as const,
  insights: (params?: { workspace?: string }) => ["insights", params] as const,
  mailbox: (params?: { actor?: string; all?: boolean; limit?: number; workspace?: string }) => ["mailbox", params] as const,
  reservations: (params?: { workspace?: string }) => ["reservations", params] as const,
  blackboard: (params?: { ns?: string; topic?: string; all?: boolean; limit?: number }) => ["blackboard", params] as const,
  sqlite: {
    databases: () => ["sqlite", "databases"] as const,
    tables: (db: string) => ["sqlite", "tables", db] as const,
    data: (db: string, table: string, offset: number) => ["sqlite", "data", db, table, offset] as const,
    schema: (db: string, table: string) => ["sqlite", "schema", db, table] as const,
    indexes: (db: string, table?: string) => ["sqlite", "indexes", db, table] as const,
  },
  search: (params: { q: string; limit?: number; rerank?: boolean; scope?: string; workspace?: string }) =>
    ["search", params] as const,
  workspaces: () => ["workspaces"] as const,
  sessions: (params?: { limit?: number; offset?: number; workspace?: string }) => ["sessions", params] as const,
  session: (id: string) => ["sessions", id] as const,
  sessionMessages: (id: string, params?: { limit?: number; offset?: number }) => ["sessions", id, "messages", params] as const,
  sessionSearch: (params: { pattern: string; limit?: number }) => ["sessions", "search", params] as const,
  codemaps: (params?: { workspace?: string; limit?: number }) => ["codemaps", params] as const,
  codemap: (id: string, workspace?: string) => ["codemaps", id, workspace] as const,
  codemapSearch: (params: { query: string; limit?: number; workspace?: string }) =>
    ["codemaps", "search", params] as const,
};

// Jobs
export function useJobs(params?: { state?: string; limit?: number }) {
  return useQuery({
    queryKey: queryKeys.jobs(params),
    queryFn: () => api.getJobs(params),
  });
}

export function useJobDetail(id: string) {
  return useQuery({
    queryKey: queryKeys.jobDetail(id),
    queryFn: () => api.getJobDetail(id),
    enabled: !!id,
  });
}

// Tasks
export function useTasks(params?: { limit?: number; workspace?: string }) {
  return useQuery({
    queryKey: queryKeys.tasks(params),
    queryFn: () => api.getTasks(params),
  });
}

export function useAgents(params?: { state?: string; limit?: number }) {
  return useQuery({
    queryKey: queryKeys.agents(params),
    queryFn: () => api.getAgents(params),
  });
}

// Stats
export function useStats() {
  return useQuery({
    queryKey: queryKeys.stats(),
    queryFn: api.getStats,
  });
}

// Insights
export function useInsights(params?: { workspace?: string }) {
  return useQuery({
    queryKey: queryKeys.insights(params),
    queryFn: () => api.getInsights(params),
  });
}

// Mailbox
export function useMailbox(params?: { actor?: string; all?: boolean; limit?: number; workspace?: string }) {
  return useQuery({
    queryKey: queryKeys.mailbox(params),
    queryFn: () => api.getMailbox(params),
    enabled: !!params?.workspace,
  });
}

// Reservations
export function useReservations(params?: { workspace?: string }) {
  return useQuery({
    queryKey: queryKeys.reservations(params),
    queryFn: () => api.getReservations(params),
    enabled: !!params?.workspace,
  });
}

// Blackboard
export function useBlackboard(params?: { ns?: string; topic?: string; all?: boolean; limit?: number }) {
  return useQuery({
    queryKey: queryKeys.blackboard(params),
    queryFn: () => api.getBlackboard(params),
  });
}

// SQLite
export function useSQLiteDatabases() {
  return useQuery({
    queryKey: queryKeys.sqlite.databases(),
    queryFn: api.getSQLiteDatabases,
  });
}

export function useSQLiteTables(db: string) {
  return useQuery({
    queryKey: queryKeys.sqlite.tables(db),
    queryFn: () => api.getSQLiteTables(db),
    enabled: !!db,
  });
}

export function useSQLiteData(db: string, table: string, limit = 100, offset = 0) {
  return useQuery({
    queryKey: queryKeys.sqlite.data(db, table, offset),
    queryFn: () => api.getSQLiteData(db, table, limit, offset),
    enabled: !!db && !!table,
  });
}

export function useSQLiteSchema(db: string, table: string) {
  return useQuery({
    queryKey: queryKeys.sqlite.schema(db, table),
    queryFn: () => api.getSQLiteSchema(db, table),
    enabled: !!db && !!table,
  });
}

export function useSQLiteIndexes(db: string, table?: string) {
  return useQuery({
    queryKey: queryKeys.sqlite.indexes(db, table),
    queryFn: () => api.getSQLiteIndexes(db, table),
    enabled: !!db,
  });
}

// Search
export function useSearch(params: {
  q: string;
  limit?: number;
  rerank?: boolean;
  scope?: string;
  workspace?: string;
}) {
  return useQuery({
    queryKey: queryKeys.search(params),
    queryFn: () => api.search(params),
    enabled: !!params.q,
  });
}

// Workspaces
export function useWorkspaces() {
  return useQuery({
    queryKey: queryKeys.workspaces(),
    queryFn: api.getWorkspaces,
  });
}

// Sessions
export function useSessions(params?: { limit?: number; offset?: number; workspace?: string }) {
  return useQuery({
    queryKey: queryKeys.sessions(params),
    queryFn: () => api.getSessions(params),
  });
}

export function useSession(id: string) {
  return useQuery({
    queryKey: queryKeys.session(id),
    queryFn: () => api.getSession(id),
    enabled: !!id,
  });
}

export function useSessionMessages(id: string, params?: { limit?: number; offset?: number }) {
  return useQuery({
    queryKey: queryKeys.sessionMessages(id, params),
    queryFn: () => api.getSessionMessages(id, params),
    enabled: !!id,
  });
}

export function useSessionSearch(params: { pattern: string; limit?: number }) {
  return useQuery({
    queryKey: queryKeys.sessionSearch(params),
    queryFn: () => api.searchSessions(params),
    enabled: !!params.pattern,
  });
}

// Codemaps
export function useCodemaps(params?: { workspace?: string; limit?: number }) {
  return useQuery({
    queryKey: queryKeys.codemaps(params),
    queryFn: () => api.getCodemaps(params),
  });
}

export function useCodemap(id: string, workspace?: string) {
  return useQuery({
    queryKey: queryKeys.codemap(id, workspace),
    queryFn: () => api.getCodemap(id, workspace),
    enabled: !!id,
  });
}

export function useCodemapSearch(params: { query: string; limit?: number; workspace?: string }) {
  return useQuery({
    queryKey: queryKeys.codemapSearch(params),
    queryFn: () => api.searchCodemaps(params),
    enabled: !!params.query,
  });
}

// SSE hook for real-time updates
export function useSSE() {
  const queryClient = useQueryClient();

  useEffect(() => {
    const unsubscribe = api.subscribeToEvents(
      (event) => {
        // Invalidate relevant queries based on event type
        switch (event.type) {
          case "job":
            queryClient.invalidateQueries({ queryKey: ["jobs"] });
            queryClient.invalidateQueries({ queryKey: ["stats"] });
            break;
          case "task":
            queryClient.invalidateQueries({ queryKey: ["tasks"] });
            queryClient.invalidateQueries({ queryKey: ["insights"] });
            break;
          case "mailbox":
            queryClient.invalidateQueries({ queryKey: ["mailbox"] });
            break;
          case "blackboard":
            queryClient.invalidateQueries({ queryKey: ["blackboard"] });
            break;
          default:
            // Unknown event, invalidate everything
            queryClient.invalidateQueries();
        }
      },
      (error) => {
        console.error("SSE error:", error);
      }
    );

    return unsubscribe;
  }, [queryClient]);
}

// ============================================================================
// Console WebSocket Hook
// ============================================================================

export interface ConsoleMessage {
  id: string;
  role: "user" | "assistant" | "system";
  content: string;
  timestamp: number;
  correlationId?: string;
  metadata?: Record<string, unknown>;
  isStreaming?: boolean;
}

export interface ConsoleWebSocketState {
  connected: boolean;
  connecting: boolean;
  error: string | null;
  messages: ConsoleMessage[];
  inflight: string | null; // correlation ID of pending request
}

type PayloadType = "ask" | "cmd" | "event" | "reply";

interface WsPayload {
  type: PayloadType;
  console_id?: string;
  correlation_id?: string;
  content?: string;
  metadata?: Record<string, unknown>;
  cmd?: { name: string; correlation_id?: string };
}

export function useConsoleWebSocket(sessionId: string | null, workspace?: string) {
  const [state, setState] = useState<ConsoleWebSocketState>({
    connected: false,
    connecting: false,
    error: null,
    messages: [],
    inflight: null,
  });

  const wsRef = useRef<WebSocket | null>(null);
  const reconnectTimeoutRef = useRef<ReturnType<typeof setTimeout> | null>(null);
  const correlationIdRef = useRef<number>(0);

  // Generate correlation ID
  const generateCorrelationId = useCallback(() => {
    correlationIdRef.current += 1;
    return `msg-${Date.now()}-${correlationIdRef.current}`;
  }, []);

  // Connect to WebSocket
  const connect = useCallback(() => {
    if (!sessionId) return;

    // Clear any pending reconnect
    if (reconnectTimeoutRef.current) {
      clearTimeout(reconnectTimeoutRef.current);
      reconnectTimeoutRef.current = null;
    }

    // Close existing connection
    if (wsRef.current) {
      wsRef.current.close();
    }

    setState((s) => ({ ...s, connecting: true, error: null }));

    const protocol = window.location.protocol === "https:" ? "wss:" : "ws:";
    const host = window.location.host;
    const wsUrl = `${protocol}//${host}/ws/console/${sessionId}${workspace ? `?workspace=${encodeURIComponent(workspace)}` : ""}`;

    const ws = new WebSocket(wsUrl);
    wsRef.current = ws;

    ws.onopen = () => {
      setState((s) => ({ ...s, connected: true, connecting: false, error: null }));
    };

    ws.onclose = (event) => {
      setState((s) => ({ ...s, connected: false, connecting: false }));

      // Auto-reconnect on abnormal close
      if (event.code !== 1000 && event.code !== 1001) {
        reconnectTimeoutRef.current = setTimeout(connect, 3000);
      }
    };

    ws.onerror = () => {
      setState((s) => ({ ...s, error: "WebSocket connection error" }));
    };

    ws.onmessage = (event) => {
      try {
        const payload: WsPayload = JSON.parse(event.data);
        handlePayload(payload);
      } catch (err) {
        console.error("Failed to parse WebSocket message:", err);
      }
    };
  }, [sessionId, workspace]);

  // Handle incoming payloads
  const handlePayload = useCallback((payload: WsPayload) => {
    const correlationId = payload.correlation_id || "";

    switch (payload.type) {
      case "event":
        // Streaming event - update or append to messages
        setState((s: ConsoleWebSocketState) => {
          const existing = s.messages.find(
            (m: ConsoleMessage) => m.correlationId === correlationId && m.role === "assistant" && m.isStreaming
          );

          if (existing) {
            // Append to existing streaming message
            return {
              ...s,
              messages: s.messages.map((m: ConsoleMessage) =>
                m.id === existing.id ? { ...m, content: m.content + (payload.content || "") } : m
              ),
            };
          } else {
            // New streaming message
            const newMsg: ConsoleMessage = {
              id: `${correlationId}-stream`,
              role: "assistant",
              content: payload.content || "",
              timestamp: Date.now(),
              correlationId,
              metadata: payload.metadata,
              isStreaming: true,
            };
            return {
              ...s,
              messages: [...s.messages, newMsg],
            };
          }
        });
        break;

      case "reply":
        // Final reply - complete the streaming message
        setState((s: ConsoleWebSocketState) => {
          const streamingMsg = s.messages.find(
            (m: ConsoleMessage) => m.correlationId === correlationId && m.isStreaming
          );

          if (streamingMsg) {
            // Mark as complete
            return {
              ...s,
              inflight: null,
              messages: s.messages.map((m: ConsoleMessage) =>
                m.id === streamingMsg.id
                  ? { ...m, content: payload.content || m.content, isStreaming: false }
                  : m
              ),
            };
          } else {
            // Add as new complete message
            const newMsg: ConsoleMessage = {
              id: `${correlationId}-reply`,
              role: "assistant",
              content: payload.content || "",
              timestamp: Date.now(),
              correlationId,
              isStreaming: false,
            };
            return {
              ...s,
              inflight: null,
              messages: [...s.messages, newMsg],
            };
          }
        });
        break;
    }
  }, []);

  // Send a message
  const sendMessage = useCallback(
    (content: string) => {
      if (!wsRef.current || wsRef.current.readyState !== WebSocket.OPEN) {
        setState((s: ConsoleWebSocketState) => ({ ...s, error: "Not connected" }));
        return null;
      }

      const correlationId = generateCorrelationId();

      // Add user message to state
      const userMsg: ConsoleMessage = {
        id: correlationId,
        role: "user",
        content,
        timestamp: Date.now(),
        correlationId,
      };

      setState((s: ConsoleWebSocketState) => ({
        ...s,
        messages: [...s.messages, userMsg],
        inflight: correlationId,
      }));

      // Send to server
      const payload: WsPayload = {
        type: "ask",
        correlation_id: correlationId,
        content,
      };

      wsRef.current.send(JSON.stringify(payload));
      return correlationId;
    },
    [generateCorrelationId]
  );

  // Cancel current request
  const cancel = useCallback(() => {
    if (!wsRef.current || wsRef.current.readyState !== WebSocket.OPEN) {
      return;
    }

    const payload: WsPayload = {
      type: "cmd",
      cmd: { name: "cancel" },
    };

    wsRef.current.send(JSON.stringify(payload));
    setState((s: ConsoleWebSocketState) => ({ ...s, inflight: null }));
  }, []);

  // Clear messages
  const clearMessages = useCallback(() => {
    setState((s: ConsoleWebSocketState) => ({ ...s, messages: [] }));
  }, []);

  // Disconnect
  const disconnect = useCallback(() => {
    if (reconnectTimeoutRef.current) {
      clearTimeout(reconnectTimeoutRef.current);
      reconnectTimeoutRef.current = null;
    }
    if (wsRef.current) {
      wsRef.current.close(1000);
      wsRef.current = null;
    }
    setState((s: ConsoleWebSocketState) => ({ ...s, connected: false, connecting: false }));
  }, []);

  // Connect on mount, disconnect on unmount
  useEffect(() => {
    if (sessionId) {
      connect();
    }

    return () => {
      disconnect();
    };
  }, [sessionId, connect, disconnect]);

  return {
    ...state,
    sendMessage,
    cancel,
    clearMessages,
    reconnect: connect,
    disconnect,
  };
}

// Console Sessions API hooks
export function useConsoles(params?: { limit?: number }) {
  return useQuery({
    queryKey: ["consoles", params],
    queryFn: () => api.getConsoles(params),
  });
}

export function useCreateConsole() {
  const queryClient = useQueryClient();

  return useMutation({
    mutationFn: api.createConsole,
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ["consoles"] });
    },
  });
}

export function useDeleteConsole() {
  const queryClient = useQueryClient();

  return useMutation({
    mutationFn: api.deleteConsole,
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ["consoles"] });
    },
  });
}

// ============================================================================
// Companion API Hooks
// ============================================================================

export const companionQueryKeys = {
  context: (conversationId: string) => ["companion", "context", conversationId] as const,
  memoryStats: (conversationId: string) => ["companion", "memory", conversationId, "stats"] as const,
  memoryContext: (conversationId: string) => ["companion", "memory", conversationId, "context"] as const,
  conversations: (params?: { limit?: number }) => ["companion", "conversations", params] as const,
};

// Companion chat mutation
export function useCompanionChat() {
  const queryClient = useQueryClient();

  return useMutation({
    mutationFn: api.companionChat,
    onSuccess: (_, variables) => {
      // Invalidate context after chat (may have changed)
      queryClient.invalidateQueries({
        queryKey: companionQueryKeys.context(variables.conversation_id),
      });
      queryClient.invalidateQueries({
        queryKey: companionQueryKeys.memoryStats(variables.conversation_id),
      });
      queryClient.invalidateQueries({
        queryKey: companionQueryKeys.memoryContext(variables.conversation_id),
      });
      // Also invalidate conversations list (message count, last_message updated)
      queryClient.invalidateQueries({
        queryKey: ["companion", "conversations"],
      });
    },
  });
}

// Get context variables for a conversation
export function useCompanionContext(conversationId: string | null) {
  return useQuery({
    queryKey: companionQueryKeys.context(conversationId || ""),
    queryFn: () => api.getCompanionContext(conversationId!),
    enabled: !!conversationId,
  });
}

// Set context variable mutation
export function useSetCompanionContext() {
  const queryClient = useQueryClient();

  return useMutation({
    mutationFn: api.setCompanionContext,
    onSuccess: (_, variables) => {
      queryClient.invalidateQueries({
        queryKey: companionQueryKeys.context(variables.conversation_id),
      });
    },
  });
}

// Delete context variable mutation
export function useDeleteCompanionContext() {
  const queryClient = useQueryClient();

  return useMutation({
    mutationFn: ({ conversationId, key, scope }: { conversationId: string; key: string; scope?: string }) =>
      api.deleteCompanionContext(conversationId, key, scope),
    onSuccess: (_, variables) => {
      queryClient.invalidateQueries({
        queryKey: companionQueryKeys.context(variables.conversationId),
      });
    },
  });
}

// Clear all context mutation
export function useClearCompanionContext() {
  const queryClient = useQueryClient();

  return useMutation({
    mutationFn: api.clearCompanionContext,
    onSuccess: (_, conversationId) => {
      queryClient.invalidateQueries({
        queryKey: companionQueryKeys.context(conversationId),
      });
    },
  });
}

// Get memory stats
export function useCompanionMemoryStats(conversationId: string | null) {
  return useQuery({
    queryKey: companionQueryKeys.memoryStats(conversationId || ""),
    queryFn: () => api.getCompanionMemoryStats(conversationId!),
    enabled: !!conversationId,
  });
}

// Get formatted memory context
export function useCompanionMemoryContext(conversationId: string | null) {
  return useQuery({
    queryKey: companionQueryKeys.memoryContext(conversationId || ""),
    queryFn: () => api.getCompanionMemoryContext(conversationId!),
    enabled: !!conversationId,
  });
}

// Clear memory mutation
export function useClearCompanionMemory() {
  const queryClient = useQueryClient();

  return useMutation({
    mutationFn: api.clearCompanionMemory,
    onSuccess: (_, conversationId) => {
      queryClient.invalidateQueries({
        queryKey: companionQueryKeys.memoryStats(conversationId),
      });
      queryClient.invalidateQueries({
        queryKey: companionQueryKeys.memoryContext(conversationId),
      });
    },
  });
}

// List conversations
export function useCompanionConversations(params?: { limit?: number }) {
  return useQuery({
    queryKey: companionQueryKeys.conversations(params),
    queryFn: () => api.getCompanionConversations(params),
  });
}

// Export types for components
export type {
  CompanionChatRequest,
  CompanionChatResponse,
  CompanionContextVariable,
  CompanionContextResponse,
  CompanionMemoryStats,
  CompanionConversation
} from "./client";
