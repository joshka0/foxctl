// Data fetching hooks for TUI
// Uses @foxctl/data client with simple React state management

import { useState, useEffect, useCallback, useReducer, useRef } from "react";
import {
  getJobs,
  getJobDetail,
  getTasks,
  getStats,
  getInsights,
  getMailbox,
  getReservations,
  getBlackboard,
  getOrchestrationBoard,
  getOrchestrationBoardCardRuntime,
  applyOrchestrationCardAction,
  dispatchOrchestrationIssue,
  refreshOrchestration,
  getWorkspaces,
  switchWorkspace,
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
  getSessionContextWindows,
  getAgents,
  getAgent,
  getAgentMemoryStats,
  getAgentMemoryContext,
  searchAgentMemory,
  compressAgentMemory,
  getRoom,
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
  type OrchestrationCardAction,
  type OrchestrationLaneID,
  type OrchestrationCard,
} from "@foxctl/data";

export interface UseQueryResult<T> {
  data: T | undefined;
  isLoading: boolean;
  error: Error | undefined;
  refetch: () => void;
}

// Generic hook for data fetching
function useQuery<T>(
  fetcher: () => Promise<T>,
  deps: unknown[] = [],
): UseQueryResult<T> {
  const [data, setData] = useState<T | undefined>(undefined);
  const [isLoading, setIsLoading] = useState(true);
  const [error, setError] = useState<Error | undefined>(undefined);

  const fetcherRef = useRef(fetcher);
  fetcherRef.current = fetcher;

  const [refreshToken, bumpRefresh] = useReducer((value) => value + 1, 0);
  const refetch = useCallback(() => bumpRefresh(), []);

  useEffect(() => {
    let alive = true;
    void refreshToken;
    setIsLoading(true);
    setError(undefined);
    fetcherRef
      .current()
      .then((result) => {
        if (!alive) return;
        setData(result);
      })
      .catch((err) => {
        if (!alive) return;
        setError(err instanceof Error ? err : new Error(String(err)));
      })
      .finally(() => {
        if (!alive) return;
        setIsLoading(false);
      });

    return () => {
      alive = false;
    };
  }, [...deps, refreshToken]);

  return { data, isLoading, error, refetch };
}

// Jobs
export function useJobs(params?: { state?: string; limit?: number }) {
  return useQuery(async () => {
    const result = await getJobs(params);
    return result.jobs;
  }, [params?.state, params?.limit]);
}

export function useJobDetail(id: string | undefined) {
  return useQuery(async () => {
    if (!id) return undefined;
    return getJobDetail(id);
  }, [id]);
}

// Tasks
export function useTasks(params?: { limit?: number }) {
  return useQuery(async () => {
    const result = await getTasks(params);
    return { tasks: result.tasks, stats: result.stats };
  }, [params?.limit]);
}

// SQLite
export function useSQLiteDatabases() {
  return useQuery(async () => {
    const result = await getSQLiteDatabases();
    return result.databases;
  }, []);
}

export function useSQLiteTables(db: string | undefined) {
  return useQuery(async () => {
    if (!db) return undefined;
    const result = await getSQLiteTables(db);
    return result.tables;
  }, [db]);
}

export function useSQLiteData(
  db: string | undefined,
  table: string | undefined,
  limit = 100,
) {
  return useQuery(async () => {
    if (!db || !table) return undefined;
    return getSQLiteData(db, table, limit);
  }, [db, table, limit]);
}

// Search
export function useSearch(params: {
  q: string;
  limit?: number;
  rerank?: boolean;
  scope?: string;
}) {
  return useQuery(async () => {
    if (!params.q) return { results: [], stats: undefined };
    const result = await search(params);
    return { results: result.results, stats: result.stats };
  }, [params.q, params.limit, params.rerank, params.scope]);
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
export function useMailbox(params?: {
  actor?: string;
  limit?: number;
  workspace?: string;
}) {
  return useQuery(async () => {
    const workspace =
      params?.workspace ||
      (typeof process !== "undefined" && typeof process.cwd === "function"
        ? process.cwd()
        : undefined);
    if (!workspace) {
      return [];
    }
    const result = await getMailbox({ ...params, workspace });
    return result.messages;
  }, [params?.actor, params?.limit, params?.workspace]);
}

// Reservations
export function useReservations(params?: { workspace?: string }) {
  return useQuery(async () => {
    const workspace =
      params?.workspace ||
      (typeof process !== "undefined" && typeof process.cwd === "function"
        ? process.cwd()
        : undefined);
    if (!workspace) {
      return [];
    }
    const result = await getReservations({ workspace });
    return result.reservations;
  }, [params?.workspace]);
}

// Blackboard
export function useBlackboard(params?: {
  ns?: string;
  topic?: string;
  limit?: number;
}) {
  return useQuery(async () => {
    const result = await getBlackboard(params);
    return result.records;
  }, [params?.ns, params?.topic, params?.limit]);
}

export function useOrchestrationBoard(params?: {
  workspace?: string;
  limit?: number;
  lane?: OrchestrationLaneID;
}) {
  return useQuery(async () => {
    const workspace =
      params?.workspace ||
      (typeof process !== "undefined" && typeof process.cwd === "function"
        ? process.cwd()
        : undefined);
    const result = await getOrchestrationBoard({
      request_id: `tui-board-${Date.now()}`,
      workspace_id: workspace,
      limit: params?.limit,
      lane: params?.lane,
    });
    return result;
  }, [params?.workspace, params?.limit, params?.lane]);
}

export function useWorkspaces() {
  return useQuery(async () => getWorkspaces(), []);
}

export function useSwitchWorkspace() {
  const [isLoading, setIsLoading] = useState(false);
  const [error, setError] = useState<Error | null>(null);

  const switchTo = useCallback(async (workspace: string) => {
    setIsLoading(true);
    setError(null);
    try {
      await switchWorkspace(workspace);
      setIsLoading(false);
    } catch (err) {
      setError(err instanceof Error ? err : new Error(String(err)));
      setIsLoading(false);
      throw err;
    }
  }, []);

  return { switchTo, isLoading, error };
}

export function useOrchestrationCardRuntime(
  issueID: string | undefined,
  params?: { workspace?: string; depth?: number },
) {
  return useQuery(async () => {
    if (!issueID) return undefined;
    const workspace =
      params?.workspace ||
      (typeof process !== "undefined" && typeof process.cwd === "function"
        ? process.cwd()
        : undefined);
    return getOrchestrationBoardCardRuntime({
      request_id: `tui-card-${Date.now()}`,
      workspace_id: workspace,
      issue_id: issueID,
      depth: params?.depth,
    });
  }, [issueID, params?.workspace, params?.depth]);
}

export function useRefreshOrchestration() {
  const [isLoading, setIsLoading] = useState(false);
  const [error, setError] = useState<Error | null>(null);

  const refresh = useCallback(async (workspace?: string) => {
    setIsLoading(true);
    setError(null);
    try {
      const resolvedWorkspace =
        workspace ||
        (typeof process !== "undefined" && typeof process.cwd === "function"
          ? process.cwd()
          : undefined);
      const result = await refreshOrchestration({
        request_id: `tui-refresh-${Date.now()}`,
        workspace_id: resolvedWorkspace,
      });
      setIsLoading(false);
      return result;
    } catch (err) {
      setError(err instanceof Error ? err : new Error(String(err)));
      setIsLoading(false);
      throw err;
    }
  }, []);

  return { refresh, isLoading, error };
}

export function useApplyOrchestrationCardAction() {
  const [isLoading, setIsLoading] = useState(false);
  const [error, setError] = useState<Error | null>(null);

  const apply = useCallback(
    async (params: {
      workspace?: string;
      issueID: string;
      action: OrchestrationCardAction;
    }) => {
      setIsLoading(true);
      setError(null);
      try {
        const resolvedWorkspace =
          params.workspace ||
          (typeof process !== "undefined" && typeof process.cwd === "function"
            ? process.cwd()
            : undefined);
        const result = await applyOrchestrationCardAction({
          request_id: `tui-card-action-${Date.now()}`,
          workspace_id: resolvedWorkspace,
          issue_id: params.issueID,
          action: params.action,
        });
        setIsLoading(false);
        return result;
      } catch (err) {
        setError(err instanceof Error ? err : new Error(String(err)));
        setIsLoading(false);
        throw err;
      }
    },
    [],
  );

  return { apply, isLoading, error };
}

export function useDispatchOrchestrationIssue() {
  const [isLoading, setIsLoading] = useState(false);
  const [error, setError] = useState<Error | null>(null);

  const dispatch = useCallback(
    async (params: {
      workspace?: string;
      card: OrchestrationCard;
      prompt?: string;
      parentAgentID?: string;
    }) => {
      setIsLoading(true);
      setError(null);
      try {
        const resolvedWorkspace =
          params.workspace ||
          params.card.workspace_id ||
          (typeof process !== "undefined" && typeof process.cwd === "function"
            ? process.cwd()
            : undefined);
        const result = await dispatchOrchestrationIssue({
          request_id: `tui-dispatch-${Date.now()}`,
          workspace_id: resolvedWorkspace,
          issue_id: params.card.issue_id,
          issue_identifier: params.card.issue_identifier,
          title: params.card.title,
          prompt: params.prompt,
          parent_agent_id: params.parentAgentID,
        });
        setIsLoading(false);
        return result;
      } catch (err) {
        setError(err instanceof Error ? err : new Error(String(err)));
        setIsLoading(false);
        throw err;
      }
    },
    [],
  );

  return { dispatch, isLoading, error };
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
  return useQuery(async () => {
    if (!params.digest) return undefined;
    return readCASObject({
      digest: params.digest,
      page: params.page,
      pageSize: params.pageSize,
    });
  }, [params.digest, params.page, params.pageSize]);
}

// Memory
export function useMemoryEntries(params?: { type?: string; limit?: number }) {
  return useQuery(async () => {
    const result = await getMemoryEntries(params);
    return { memories: result.memories, total: result.total };
  }, [params?.type, params?.limit]);
}

export function useMemoryTypes() {
  return useQuery(async () => {
    const result = await getMemoryTypes();
    return result.types;
  }, []);
}

export function useMemoryEntry(id: string | undefined) {
  return useQuery(async () => {
    if (!id) return undefined;
    return getMemoryEntry(id);
  }, [id]);
}

export function useSaveMemory() {
  const [isLoading, setIsLoading] = useState(false);
  const [error, setError] = useState<Error | null>(null);

  const save = useCallback(
    async (params: {
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
    },
    [],
  );

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
  return useQuery(async () => {
    const result = await getSessions(params);
    return { sessions: result.sessions, total: result.total };
  }, [params?.limit, params?.offset]);
}

export function useSession(id: string | undefined) {
  return useQuery(async () => {
    if (!id) return undefined;
    const result = await getSession(id);
    return result.session;
  }, [id]);
}

// Agents
export function useAgents(params?: { state?: AgentState; limit?: number }) {
  return useQuery(async () => {
    const result = await getAgents(params);
    return { agents: result.agents, total: result.total };
  }, [params?.state, params?.limit]);
}

export function useAgent(id: string | undefined) {
  return useQuery(async () => {
    if (!id) return undefined;
    const result = await getAgent(id);
    return result.agent;
  }, [id]);
}

export function useAgentMemoryStats(id: string | undefined) {
  return useQuery(async () => {
    if (!id) return undefined;
    return getAgentMemoryStats(id);
  }, [id]);
}

export function useAgentMemoryContext(
  id: string | undefined,
  params?: { conversationID?: string },
) {
  return useQuery(async () => {
    if (!id) return undefined;
    return getAgentMemoryContext(id, {
      conversation_id: params?.conversationID,
    });
  }, [id, params?.conversationID]);
}

export function useAgentMemorySearch(
  id: string | undefined,
  params?: { query?: string; limit?: number; conversationID?: string },
) {
  return useQuery(async () => {
    if (!id || !params?.query) return undefined;
    return searchAgentMemory(id, {
      q: params.query,
      limit: params.limit,
      conversation_id: params.conversationID,
    });
  }, [id, params?.query, params?.limit, params?.conversationID]);
}

export function useCompressAgentMemory() {
  const [isLoading, setIsLoading] = useState(false);
  const [error, setError] = useState<Error | null>(null);

  const compress = useCallback(
    async (params: {
      agentID: string;
      conversationID?: string;
      distill?: boolean;
    }) => {
      setIsLoading(true);
      setError(null);
      try {
        const result = await compressAgentMemory(params.agentID, {
          conversation_id: params.conversationID,
          distill: params.distill,
        });
        setIsLoading(false);
        return result;
      } catch (err) {
        setError(err instanceof Error ? err : new Error(String(err)));
        setIsLoading(false);
        throw err;
      }
    },
    [],
  );

  return { compress, isLoading, error };
}

export function useRoom(
  roomID: string | undefined,
  params?: { workspace?: string },
) {
  return useQuery(async () => {
    if (!roomID) return undefined;
    const workspace =
      params?.workspace ||
      (typeof process !== "undefined" && typeof process.cwd === "function"
        ? process.cwd()
        : undefined);
    if (!workspace) return undefined;
    try {
      const result = await getRoom(roomID, { workspace_id: workspace });
      return result.room;
    } catch (err) {
      const message = err instanceof Error ? err.message.toLowerCase() : "";
      if (message.includes("404") || message.includes("room not found")) {
        return undefined;
      }
      throw err;
    }
  }, [roomID, params?.workspace]);
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
      return result;
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

  const send = useCallback(
    async (
      agentId: string,
      params: {
        subject: string;
        body?: string;
        kind?: string;
        priority?: number;
        sender?: string;
      },
    ) => {
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
    },
    [],
  );

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

  const acknowledge = useCallback(
    async (messageId: string, actorId?: string) => {
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
    },
    [],
  );

  return { acknowledge, isLoading, error };
}

// Trajectories
export function useTrajectories(params?: {
  status?: string;
  agent_role?: string;
  limit?: number;
}) {
  return useQuery(async () => {
    const result = await getTrajectories(params);
    return { trajectories: result.trajectories, total: result.total };
  }, [params?.status, params?.agent_role, params?.limit]);
}

export function useTrajectoryEvents(
  trajectoryId: string | undefined,
  params?: { kind?: string; limit?: number },
) {
  return useQuery(async () => {
    if (!trajectoryId) return undefined;
    const result = await getTrajectoryEvents(trajectoryId, params);
    return result.events;
  }, [trajectoryId, params?.kind, params?.limit]);
}

// Trajectory feedback (DSPy training)
export function useTrajectoryFeedback(trajectoryId: string | undefined) {
  return useQuery(async () => {
    if (!trajectoryId) return undefined;
    const result = await getTrajectoryFeedback(trajectoryId);
    return result.feedback;
  }, [trajectoryId]);
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
    [],
  );

  return { submit, isLoading, error };
}

// Scorer weights (learnable scoring)
export function useScorerWeights() {
  return useQuery(async () => {
    const result = await getScorerWeights();
    return { weights: result.weights, history: result.history };
  }, []);
}

export function useUserRequests(params?: { limit?: number }) {
  return useQuery(async () => {
    const result = await getUserRequests(params);
    return result.requests;
  }, [params?.limit]);
}

// Session Messages (for turn viewer)
export function useSessionMessages(
  sessionId: string | undefined,
  params?: { limit?: number; offset?: number },
) {
  return useQuery(async () => {
    if (!sessionId) return undefined;
    const result = await getSessionMessages(sessionId, params);
    return {
      messages: result.messages,
      total: result.total,
      limit: result.limit,
      offset: result.offset,
    };
  }, [sessionId, params?.limit, params?.offset]);
}

// Session Context Windows
export function useSessionContextWindows(sessionId: string | undefined) {
  return useQuery(async () => {
    if (!sessionId) return undefined;
    const result = await getSessionContextWindows(sessionId);
    return {
      context_windows: result.context_windows,
      total: result.total,
    };
  }, [sessionId]);
}

// Console Sessions
export function useConsoles(params?: { limit?: number }) {
  return useQuery(async () => {
    const result = await getConsoles(params);
    return { consoles: result.consoles, total: result.total };
  }, [params?.limit]);
}

export function useConsole(id: string | undefined) {
  return useQuery(async () => {
    if (!id) return undefined;
    return getConsole(id);
  }, [id]);
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

const MAX_CONSOLE_HISTORY = 2000;
const MAX_EVENT_CHARS = 20000;

function clampText(value: string, max: number): string {
  if (value.length <= max) return value;
  return value.slice(0, max) + "\n...[truncated]";
}

function pushConsoleHistory(
  prev: ConsoleHistoryEvent[],
  next: ConsoleHistoryEvent,
): ConsoleHistoryEvent[] {
  const nextClamped = {
    ...next,
    content: clampText(next.content || "", MAX_EVENT_CHARS),
  };

  if (
    nextClamped.type === "event" &&
    nextClamped.kind === "progress" &&
    nextClamped.askId
  ) {
    const last = prev[prev.length - 1];
    if (
      last?.type === "event" &&
      last.kind === "progress" &&
      last.askId === nextClamped.askId
    ) {
      const out = prev.slice();
      out[out.length - 1] = nextClamped;
      return out;
    }
  }

  if (prev.length < MAX_CONSOLE_HISTORY) {
    return [...prev, nextClamped];
  }

  return [...prev.slice(prev.length - MAX_CONSOLE_HISTORY + 1), nextClamped];
}

// Console SSE subscription hook
export function useConsoleEvents(
  consoleId: string | undefined,
  onEvent?: (event: SSEEvent) => void,
) {
  const [connected, setConnected] = useState(false);
  const [error, setError] = useState<Error | undefined>(undefined);
  const [history, setHistory] = useState<ConsoleHistoryEvent[]>([]);

  const onEventRef = useRef(onEvent);
  useEffect(() => {
    onEventRef.current = onEvent;
  }, [onEvent]);

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
          setHistory((prev) => pushConsoleHistory(prev, historyEvent));
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
          setHistory((prev) => pushConsoleHistory(prev, historyEvent));
        }

        // Call custom handler if provided
        onEventRef.current?.(event);
      },
      (err) => {
        setError(new Error("SSE connection error"));
        setConnected(false);
      },
      () => {
        setConnected(true);
        setError(undefined);
      },
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
    setHistory((prev) => pushConsoleHistory(prev, event));
  }, []);

  const clearHistory = useCallback(() => {
    setHistory([]);
  }, []);

  return { connected, error, history, addUserMessage, clearHistory };
}

// Console mutation helpers
export interface UseConsoleMutations {
  createSession: (
    actorId: string,
    sessionId?: string,
    meta?: Record<string, unknown>,
  ) => Promise<ConsoleSession>;
  sendMessage: (
    consoleId: string,
    prompt: string,
    context?: Record<string, unknown>,
  ) => Promise<{ messageId: string; askId: string }>;
  cancel: (consoleId: string, askId?: string) => Promise<{ cmdId: string }>;
  submitFeedback: (
    consoleId: string,
    rating: number,
    trajectoryId?: string,
    askId?: string,
    comment?: string,
  ) => Promise<{ success: boolean }>;
}

export function useConsoleMutations(): UseConsoleMutations {
  const createSession = useCallback(
    async (
      actorId: string,
      sessionId?: string,
      meta?: Record<string, unknown>,
    ) => {
      return createConsole({ actor_id: actorId, session_id: sessionId, meta });
    },
    [],
  );

  const sendMessage = useCallback(
    async (
      consoleId: string,
      prompt: string,
      context?: Record<string, unknown>,
    ) => {
      const result = await sendConsoleMessage(consoleId, { prompt, context });
      return { messageId: result.message_id, askId: result.ask_id };
    },
    [],
  );

  const cancel = useCallback(async (consoleId: string, askId?: string) => {
    const result = await cancelConsoleRequest(
      consoleId,
      askId ? { ask_id: askId } : undefined,
    );
    return { cmdId: result.cmd_id };
  }, []);

  const submitFeedback = useCallback(
    async (
      consoleId: string,
      rating: number,
      trajectoryId?: string,
      askId?: string,
      comment?: string,
    ) => {
      const result = await submitConsoleFeedback(consoleId, {
        rating,
        trajectory_id: trajectoryId,
        ask_id: askId,
        comment,
      });
      return { success: result.success };
    },
    [],
  );

  return { createSession, sendMessage, cancel, submitFeedback };
}
