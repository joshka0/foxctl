// Data fetching hooks for TUI
// Uses @agentctl/data client with simple React state management

import { useState, useEffect, useCallback } from "react";
import {
  getJobs,
  getJobDetail,
  getTasks,
  getStats,
  getInsights,
  getMailbox,
  getReservations,
  getBlackboard,
  getSQLiteDatabases,
  getSQLiteTables,
  getSQLiteData,
  search,
  listCASObjects,
  readCASObject,
  getMemoryEntries,
  getMemoryTypes,
  getMemoryEntry,
  saveMemory,
  pinMemory,
  deleteMemory,
  getSessions,
  getSession,
  getSessionMessages,
  getAgents,
  getAgent,
  spawnAgent,
  stopAgent,
  sendAgentMessage,
  sendMailboxMessage,
  acknowledgeMessage,
  getTrajectories,
  getTrajectoryEvents,
  getTrajectoryFeedback,
  submitTrajectoryFeedback,
  getScorerWeights,
  getUserRequests,
  getConsoles,
  getConsole,
  createConsole,
  sendConsoleMessage,
  cancelConsoleRequest,
  submitConsoleFeedback,
  subscribeToConsoleEvents,
  type AgentState,
  type ConsoleSession,
  type ConsoleSendRequest,
  type ConsoleCancelRequest,
  type ConsoleFeedbackRequest,
  type SSEEvent,
  type ScorerWeights,
  type WeightUpdate,
  type TrajectoryFeedback,
  type SpawnAgentParams,
  type SendMailboxMessageParams,
} from "@agentctl/data";

export interface UseQueryResult<T> {
  data: T | undefined;
  isLoading: boolean;
  error: Error | undefined;
  refetch: () => void;
}

// Generic hook for data fetching
function useQuery<T>(
  fetcher: () => Promise<T>,
  deps: unknown[] = []
): UseQueryResult<T> {
  const [data, setData] = useState<T | undefined>(undefined);
  const [isLoading, setIsLoading] = useState(true);
  const [error, setError] = useState<Error | undefined>(undefined);

  const refetch = useCallback(() => {
    setIsLoading(true);
    setError(undefined);
    fetcher()
      .then(setData)
      .catch(setError)
      .finally(() => setIsLoading(false));
  }, deps);

  useEffect(() => {
    refetch();
  }, [refetch]);

  return { data, isLoading, error, refetch };
}

// Jobs
export function useJobs(params?: { state?: string; limit?: number }) {
  return useQuery(
    async () => {
      const result = await getJobs(params);
      return result.jobs;
    },
    [params?.state, params?.limit]
  );
}

export function useJobDetail(id: string | undefined) {
  return useQuery(
    async () => {
      if (!id) return undefined;
      return getJobDetail(id);
    },
    [id]
  );
}

// Tasks
export function useTasks(params?: { limit?: number }) {
  return useQuery(
    async () => {
      const result = await getTasks(params);
      return { tasks: result.tasks, stats: result.stats };
    },
    [params?.limit]
  );
}

// SQLite
export function useSQLiteDatabases() {
  return useQuery(async () => {
    const result = await getSQLiteDatabases();
    return result.databases;
  }, []);
}

export function useSQLiteTables(db: string | undefined) {
  return useQuery(
    async () => {
      if (!db) return undefined;
      const result = await getSQLiteTables(db);
      return result.tables;
    },
    [db]
  );
}

export function useSQLiteData(
  db: string | undefined,
  table: string | undefined,
  limit = 100
) {
  return useQuery(
    async () => {
      if (!db || !table) return undefined;
      return getSQLiteData(db, table, limit);
    },
    [db, table, limit]
  );
}

// Search
export function useSearch(params: {
  q: string;
  limit?: number;
  rerank?: boolean;
  scope?: string;
}) {
  return useQuery(
    async () => {
      if (!params.q) return { results: [], stats: undefined };
      const result = await search(params);
      return { results: result.results, stats: result.stats };
    },
    [params.q, params.limit, params.rerank, params.scope]
  );
}

// Stats
export function useStats() {
  return useQuery(async () => {
    return getStats();
  }, []);
}

// Insights
export function useInsights() {
  return useQuery(async () => {
    return getInsights();
  }, []);
}

// Mailbox
export function useMailbox(params?: { actor?: string; limit?: number }) {
  return useQuery(
    async () => {
      const result = await getMailbox(params);
      return result.messages;
    },
    [params?.actor, params?.limit]
  );
}

// Reservations
export function useReservations() {
  return useQuery(async () => {
    const result = await getReservations();
    return result.reservations;
  }, []);
}

// Blackboard
export function useBlackboard(params?: {
  ns?: string;
  topic?: string;
  limit?: number;
}) {
  return useQuery(
    async () => {
      const result = await getBlackboard(params);
      return result.records;
    },
    [params?.ns, params?.topic, params?.limit]
  );
}

// CAS (Content Addressable Storage)
export function useCASObjects() {
  return useQuery(async () => {
    const result = await listCASObjects();
    return result.objects;
  }, []);
}

export function useCASContent(params: {
  digest: string | undefined;
  page?: number;
  pageSize?: number;
}) {
  return useQuery(
    async () => {
      if (!params.digest) return undefined;
      return readCASObject({
        digest: params.digest,
        page: params.page,
        pageSize: params.pageSize,
      });
    },
    [params.digest, params.page, params.pageSize]
  );
}

// Memory
export function useMemoryEntries(params?: { type?: string; limit?: number }) {
  return useQuery(
    async () => {
      const result = await getMemoryEntries(params);
      return { memories: result.memories, total: result.total };
    },
    [params?.type, params?.limit]
  );
}

export function useMemoryTypes() {
  return useQuery(async () => {
    const result = await getMemoryTypes();
    return result.types;
  }, []);
}

export function useMemoryEntry(id: string | undefined) {
  return useQuery(
    async () => {
      if (!id) return undefined;
      return getMemoryEntry(id);
    },
    [id]
  );
}

export function useSaveMemory() {
  const [isLoading, setIsLoading] = useState(false);
  const [error, setError] = useState<Error | null>(null);

  const save = useCallback(async (params: {
    name: string;
    type: string;
    summary?: string;
    data?: unknown;
  }) => {
    setIsLoading(true);
    setError(null);
    try {
      const result = await saveMemory(params);
      setIsLoading(false);
      return result;
    } catch (err) {
      setError(err instanceof Error ? err : new Error(String(err)));
      setIsLoading(false);
      throw err;
    }
  }, []);

  return { save, isLoading, error };
}

export function usePinMemory() {
  const [isLoading, setIsLoading] = useState(false);
  const [error, setError] = useState<Error | null>(null);

  const pin = useCallback(async (id: string) => {
    setIsLoading(true);
    setError(null);
    try {
      const result = await pinMemory(id);
      setIsLoading(false);
      return result;
    } catch (err) {
      setError(err instanceof Error ? err : new Error(String(err)));
      setIsLoading(false);
      throw err;
    }
  }, []);

  return { pin, isLoading, error };
}

export function useDeleteMemory() {
  const [isLoading, setIsLoading] = useState(false);
  const [error, setError] = useState<Error | null>(null);

  const remove = useCallback(async (id: string) => {
    setIsLoading(true);
    setError(null);
    try {
      const result = await deleteMemory(id);
      setIsLoading(false);
      return result;
    } catch (err) {
      setError(err instanceof Error ? err : new Error(String(err)));
      setIsLoading(false);
      throw err;
    }
  }, []);

  return { remove, isLoading, error };
}

// Sessions
export function useSessions(params?: { limit?: number; offset?: number }) {
  return useQuery(
    async () => {
      const result = await getSessions(params);
      return { sessions: result.sessions, total: result.total };
    },
    [params?.limit, params?.offset]
  );
}

export function useSession(id: string | undefined) {
  return useQuery(
    async () => {
      if (!id) return undefined;
      const result = await getSession(id);
      return result.session;
    },
    [id]
  );
}

// Agents
export function useAgents(params?: { state?: AgentState; limit?: number }) {
  return useQuery(
    async () => {
      const result = await getAgents(params);
      return { agents: result.agents, total: result.total };
    },
    [params?.state, params?.limit]
  );
}

export function useAgent(id: string | undefined) {
  return useQuery(
    async () => {
      if (!id) return undefined;
      const result = await getAgent(id);
      return result.agent;
    },
    [id]
  );
}

// Agent mutations (Phase 5: Multi-Agent Orchestration)
export function useSpawnAgent() {
  const [isLoading, setIsLoading] = useState(false);
  const [error, setError] = useState<Error | null>(null);

  const spawn = useCallback(async (params: SpawnAgentParams) => {
    setIsLoading(true);
    setError(null);
    try {
      const result = await spawnAgent(params);
      setIsLoading(false);
      return result.agent;
    } catch (err) {
      setError(err instanceof Error ? err : new Error(String(err)));
      setIsLoading(false);
      throw err;
    }
  }, []);

  return { spawn, isLoading, error };
}

export function useStopAgent() {
  const [isLoading, setIsLoading] = useState(false);
  const [error, setError] = useState<Error | null>(null);

  const stop = useCallback(async (agentId: string) => {
    setIsLoading(true);
    setError(null);
    try {
      const result = await stopAgent(agentId);
      setIsLoading(false);
      return result;
    } catch (err) {
      setError(err instanceof Error ? err : new Error(String(err)));
      setIsLoading(false);
      throw err;
    }
  }, []);

  return { stop, isLoading, error };
}

export function useSendAgentMessage() {
  const [isLoading, setIsLoading] = useState(false);
  const [error, setError] = useState<Error | null>(null);

  const send = useCallback(async (agentId: string, params: {
    subject: string;
    body?: string;
    kind?: string;
    priority?: number;
    sender?: string;
  }) => {
    setIsLoading(true);
    setError(null);
    try {
      const result = await sendAgentMessage(agentId, params);
      setIsLoading(false);
      return result;
    } catch (err) {
      setError(err instanceof Error ? err : new Error(String(err)));
      setIsLoading(false);
      throw err;
    }
  }, []);

  return { send, isLoading, error };
}

// Mailbox mutations
export function useSendMailboxMessage() {
  const [isLoading, setIsLoading] = useState(false);
  const [error, setError] = useState<Error | null>(null);

  const send = useCallback(async (params: SendMailboxMessageParams) => {
    setIsLoading(true);
    setError(null);
    try {
      const result = await sendMailboxMessage(params);
      setIsLoading(false);
      return result;
    } catch (err) {
      setError(err instanceof Error ? err : new Error(String(err)));
      setIsLoading(false);
      throw err;
    }
  }, []);

  return { send, isLoading, error };
}

export function useAcknowledgeMessage() {
  const [isLoading, setIsLoading] = useState(false);
  const [error, setError] = useState<Error | null>(null);

  const acknowledge = useCallback(async (messageId: string, actorId?: string) => {
    setIsLoading(true);
    setError(null);
    try {
      const result = await acknowledgeMessage(messageId, actorId);
      setIsLoading(false);
      return result;
    } catch (err) {
      setError(err instanceof Error ? err : new Error(String(err)));
      setIsLoading(false);
      throw err;
    }
  }, []);

  return { acknowledge, isLoading, error };
}

// Trajectories
export function useTrajectories(params?: {
  status?: string;
  agent_role?: string;
  limit?: number;
}) {
  return useQuery(
    async () => {
      const result = await getTrajectories(params);
      return { trajectories: result.trajectories, total: result.total };
    },
    [params?.status, params?.agent_role, params?.limit]
  );
}

export function useTrajectoryEvents(
  trajectoryId: string | undefined,
  params?: { kind?: string; limit?: number }
) {
  return useQuery(
    async () => {
      if (!trajectoryId) return undefined;
      const result = await getTrajectoryEvents(trajectoryId, params);
      return result.events;
    },
    [trajectoryId, params?.kind, params?.limit]
  );
}

// Trajectory feedback (DSPy training)
export function useTrajectoryFeedback(trajectoryId: string | undefined) {
  return useQuery(
    async () => {
      if (!trajectoryId) return undefined;
      const result = await getTrajectoryFeedback(trajectoryId);
      return result.feedback;
    },
    [trajectoryId]
  );
}

export function useSubmitTrajectoryFeedback() {
  const [isLoading, setIsLoading] = useState(false);
  const [error, setError] = useState<Error | null>(null);

  const submit = useCallback(
    async (trajectoryId: string, rating: number, comment?: string) => {
      setIsLoading(true);
      setError(null);
      try {
        const result = await submitTrajectoryFeedback(trajectoryId, {
          rating,
          comment,
        });
        setIsLoading(false);
        return result;
      } catch (err) {
        setError(err instanceof Error ? err : new Error(String(err)));
        setIsLoading(false);
        throw err;
      }
    },
    []
  );

  return { submit, isLoading, error };
}

// Scorer weights (learnable scoring)
export function useScorerWeights() {
  return useQuery(
    async () => {
      const result = await getScorerWeights();
      return { weights: result.weights, history: result.history };
    },
    []
  );
}

export function useUserRequests(params?: { limit?: number }) {
  return useQuery(
    async () => {
      const result = await getUserRequests(params);
      return result.requests;
    },
    [params?.limit]
  );
}

// Session Messages (for turn viewer)
export function useSessionMessages(
  sessionId: string | undefined,
  params?: { limit?: number; offset?: number }
) {
  return useQuery(
    async () => {
      if (!sessionId) return undefined;
      const result = await getSessionMessages(sessionId, params);
      return {
        messages: result.messages,
        total: result.total,
        limit: result.limit,
        offset: result.offset,
      };
    },
    [sessionId, params?.limit, params?.offset]
  );
}

// Console Sessions
export function useConsoles(params?: { limit?: number }) {
  return useQuery(
    async () => {
      const result = await getConsoles(params);
      return { consoles: result.consoles, total: result.total };
    },
    [params?.limit]
  );
}

export function useConsole(id: string | undefined) {
  return useQuery(
    async () => {
      if (!id) return undefined;
      return getConsole(id);
    },
    [id]
  );
}

// Console Event Types for history
export interface ConsoleHistoryEvent {
  id: string;
  type: "user" | "agent" | "event";
  kind?: string; // thought, tool_call, tool_result, progress
  content: string;
  timestamp: Date;
  seq?: number;
  iteration?: number;
  toolName?: string;
  status?: string;
  askId?: string;
}

// Console SSE subscription hook
export function useConsoleEvents(
  consoleId: string | undefined,
  onEvent?: (event: SSEEvent) => void
) {
  const [connected, setConnected] = useState(false);
  const [error, setError] = useState<Error | undefined>(undefined);
  const [history, setHistory] = useState<ConsoleHistoryEvent[]>([]);

  useEffect(() => {
    if (!consoleId) {
      setConnected(false);
      return;
    }

    const unsubscribe = subscribeToConsoleEvents(
      consoleId,
      (event: SSEEvent) => {
        // Process console.event and console.reply
        if (event.type === "console.event") {
          const data = event.data as {
            ask_id?: string;
            kind?: string;
            content?: string;
            seq?: number;
            iteration?: number;
            tool_name?: string;
          };
          const historyEvent: ConsoleHistoryEvent = {
            id: `${data.ask_id}-${data.seq || Date.now()}`,
            type: "event",
            kind: data.kind,
            content: data.content || "",
            timestamp: new Date(event.ts),
            seq: data.seq,
            iteration: data.iteration,
            toolName: data.tool_name,
            askId: data.ask_id,
          };
          setHistory((prev) => [...prev, historyEvent]);
        } else if (event.type === "console.reply") {
          const data = event.data as {
            ask_id?: string;
            response?: string;
            status?: string;
          };
          const historyEvent: ConsoleHistoryEvent = {
            id: `reply-${data.ask_id || Date.now()}`,
            type: "agent",
            content: data.response || "",
            timestamp: new Date(event.ts),
            status: data.status,
            askId: data.ask_id,
          };
          setHistory((prev) => [...prev, historyEvent]);
        }

        // Call custom handler if provided
        onEvent?.(event);
      },
      (err) => {
        setError(new Error("SSE connection error"));
        setConnected(false);
      },
      () => {
        setConnected(true);
        setError(undefined);
      }
    );

    return () => {
      unsubscribe();
      setConnected(false);
    };
  }, [consoleId]);

  const addUserMessage = useCallback((content: string, askId: string) => {
    const event: ConsoleHistoryEvent = {
      id: `user-${askId}`,
      type: "user",
      content,
      timestamp: new Date(),
      askId,
    };
    setHistory((prev) => [...prev, event]);
  }, []);

  const clearHistory = useCallback(() => {
    setHistory([]);
  }, []);

  return { connected, error, history, addUserMessage, clearHistory };
}

// Console mutation helpers
export interface UseConsoleMutations {
  createSession: (actorId: string, sessionId?: string, meta?: Record<string, unknown>) => Promise<ConsoleSession>;
  sendMessage: (consoleId: string, prompt: string, context?: Record<string, unknown>) => Promise<{ messageId: string; askId: string }>;
  cancel: (consoleId: string, askId?: string) => Promise<{ cmdId: string }>;
  submitFeedback: (consoleId: string, rating: number, trajectoryId?: string, askId?: string, comment?: string) => Promise<{ success: boolean }>;
}

export function useConsoleMutations(): UseConsoleMutations {
  const createSession = useCallback(
    async (actorId: string, sessionId?: string, meta?: Record<string, unknown>) => {
      return createConsole({ actor_id: actorId, session_id: sessionId, meta });
    },
    []
  );

  const sendMessage = useCallback(
    async (consoleId: string, prompt: string, context?: Record<string, unknown>) => {
      const result = await sendConsoleMessage(consoleId, { prompt, context });
      return { messageId: result.message_id, askId: result.ask_id };
    },
    []
  );

  const cancel = useCallback(async (consoleId: string, askId?: string) => {
    const result = await cancelConsoleRequest(consoleId, askId ? { ask_id: askId } : undefined);
    return { cmdId: result.cmd_id };
  }, []);

  const submitFeedback = useCallback(
    async (
      consoleId: string,
      rating: number,
      trajectoryId?: string,
      askId?: string,
      comment?: string
    ) => {
      const result = await submitConsoleFeedback(consoleId, {
        rating,
        trajectory_id: trajectoryId,
        ask_id: askId,
        comment,
      });
      return { success: result.success };
    },
    []
  );

  return { createSession, sendMessage, cancel, submitFeedback };
}
