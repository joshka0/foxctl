// API Client - connects to the foxctl backend
// Works in both browser (Vite) and Bun environments

import type {
  JobSummary,
  JobDetail,
  JobActionResult,
  JobProgressResult,
  V2EventStreamEvent,
  V2RuntimeEvent,
  V2StreamType,
  TaskSummary,
  TaskStats,
  JobStats,
  InsightsData,
  MailboxMessage,
  Reservation,
  BlackboardRecord,
  SQLiteDatabase,
  SQLiteTable,
  SQLiteColumn,
  SQLiteIndex,
  SQLiteQueryResult,
  SearchResult,
  SearchStats,
  Workspace,
  Session,
  SessionMessage,
  SessionSearchResult,
  ContextWindow,
  Codemap,
  CodemapListItem,
  CASObject,
  CASReadResult,
  SSEEvent,
  MemoryEntry,
  MemoryEntryDetail,
  MemoryTypeCount,
  Agent,
  AgentMemoryCompressResult,
  AgentMemoryContextResult,
  AgentMemorySearchResult,
  AgentMemoryStatsResult,
  AgentState,
  Room,
  RoomTask,
  Trajectory,
  TrajectoryEvent,
  UserRequest,
  ConsoleSession,
  ConsoleSessionCreate,
  ConsoleSendRequest,
  ConsoleSendResponse,
  ConsoleCancelRequest,
  ConsoleCancelResponse,
  ConsoleFeedbackRequest,
  ConsoleFeedbackResponse,
  OrchestrationBoard,
  OrchestrationBoardArtifactRef,
  OrchestrationBoardResult,
  OrchestrationBoardCardRuntimeResult,
  OrchestrationCardAction,
  OrchestrationCardActionResult,
  OrchestrationDispatchResult,
  OrchestrationLaneID,
  OrchestrationRefreshResult,
} from "./types";

// Get API base URL - works in both Vite (browser) and Bun environments
function getApiBase(): string {
  // Vite environment (browser bundled with Vite)
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  const meta = import.meta as any;
  if (typeof meta !== "undefined" && meta.env?.VITE_API_URL) {
    return meta.env.VITE_API_URL as string;
  }
  // Bun/Node environment
  if (typeof process !== "undefined" && process.env?.FOXCTL_API_URL) {
    return process.env.FOXCTL_API_URL;
  }
  // Default: relative URLs (works with proxy or same-origin)
  return "";
}

const API_BASE = getApiBase();

export class APIError extends Error {
  status: number;

  constructor(status: number, message: string) {
    super(message);
    this.name = "APIError";
    this.status = status;
  }
}

interface ApiEnvelope<T> {
  version: number;
  status: "ok" | "error" | "progress";
  command: string;
  data: T;
  meta: { ts: string; [key: string]: unknown };
  error: { code?: string; message?: string };
}

async function request<T>(
  endpoint: string,
  options: RequestInit = {},
): Promise<T> {
  const url = `${API_BASE}${endpoint}`;

  const response = await fetch(url, {
    ...options,
    headers: {
      "Content-Type": "application/json",
      ...options.headers,
    },
    credentials: "include", // For cookies (workspace)
  });

  if (!response.ok) {
    const text = await response.text();
    throw new APIError(response.status, text || `HTTP ${response.status}`);
  }

  const text = await response.text();
  if (!text) {
    throw new APIError(response.status, "Empty response body");
  }

  try {
    return JSON.parse(text) as T;
  } catch {
    throw new APIError(response.status, "Invalid JSON response");
  }
}

async function requestVoid(
  endpoint: string,
  options: RequestInit = {},
): Promise<void> {
  const url = `${API_BASE}${endpoint}`;

  const response = await fetch(url, {
    ...options,
    headers: {
      "Content-Type": "application/json",
      ...options.headers,
    },
    credentials: "include",
  });

  if (!response.ok) {
    const text = await response.text();
    throw new APIError(response.status, text || `HTTP ${response.status}`);
  }
}

function unwrapEnvelope<T>(env: ApiEnvelope<T>): T {
  if (env.status !== "ok") {
    throw new APIError(500, env.error?.message || "Request failed");
  }
  return env.data;
}

function normalizeBoardPayload(data: unknown): OrchestrationBoardResult {
  if (!data || typeof data !== "object") {
    return { board: null, artifact: null };
  }
  const asRecord = data as Record<string, unknown>;
  if (Array.isArray(asRecord.lanes)) {
    return { board: asRecord as unknown as OrchestrationBoard, artifact: null };
  }
  if (typeof asRecord.artifact === "string") {
    return {
      board: null,
      artifact: asRecord as unknown as OrchestrationBoardArtifactRef,
    };
  }
  return { board: null, artifact: null };
}

// Jobs
export async function getJobs(params?: {
  state?: string;
  limit?: number;
}): Promise<{ jobs: JobSummary[] }> {
  const searchParams = new URLSearchParams();
  if (params?.state) searchParams.set("state", params.state);
  if (params?.limit) searchParams.set("limit", String(params.limit));
  const query = searchParams.toString();
  return request(`/api/jobs${query ? `?${query}` : ""}`);
}

export async function getJobDetail(id: string): Promise<JobDetail> {
  return request(`/api/jobs/${encodeURIComponent(id)}`);
}

export async function getJobProgress(
  id: string,
  params?: { limit?: number },
): Promise<JobProgressResult> {
  const searchParams = new URLSearchParams();
  if (params?.limit) searchParams.set("limit", String(params.limit));
  const query = searchParams.toString();
  return request(
    `/api/jobs/${encodeURIComponent(id)}/progress${query ? `?${query}` : ""}`,
  );
}

export async function cancelJob(id: string): Promise<JobActionResult> {
  return request(`/api/jobs/${encodeURIComponent(id)}/cancel`, {
    method: "POST",
  });
}

export async function waitForJob(
  id: string,
  params?: { timeoutMs?: number; pollMs?: number },
): Promise<JobActionResult> {
  const searchParams = new URLSearchParams();
  if (typeof params?.timeoutMs === "number") {
    searchParams.set("timeout_ms", String(params.timeoutMs));
  }
  if (typeof params?.pollMs === "number") {
    searchParams.set("poll_ms", String(params.pollMs));
  }
  const query = searchParams.toString();
  return request(`/api/jobs/${encodeURIComponent(id)}/wait${query ? `?${query}` : ""}`, {
    method: "POST",
  });
}

export function subscribeToV2Events(
  params: {
    streamId: string;
    streamType?: V2StreamType;
    afterVersion?: number;
    limit?: number;
    buffer?: number;
    heartbeatMs?: number;
  },
  onEvent: (event: V2EventStreamEvent) => void,
  onError?: (error: Event) => void,
): () => void {
  if (typeof EventSource === "undefined") {
    console.warn("EventSource not available in this environment");
    return () => {};
  }

  const searchParams = new URLSearchParams();
  searchParams.set("stream_id", params.streamId);
  if (params.streamType) searchParams.set("stream_type", params.streamType);
  if (typeof params.afterVersion === "number") {
    searchParams.set("after_version", String(params.afterVersion));
  }
  if (typeof params.limit === "number") {
    searchParams.set("limit", String(params.limit));
  }
  if (typeof params.buffer === "number") {
    searchParams.set("buffer", String(params.buffer));
  }
  if (typeof params.heartbeatMs === "number") {
    searchParams.set("heartbeat_ms", String(params.heartbeatMs));
  }

  const eventSource = new EventSource(
    `${API_BASE}/api/v2/events/stream?${searchParams.toString()}`,
  );
  const parse = (type: V2EventStreamEvent["type"]) => (event: MessageEvent) => {
    try {
      onEvent({ type, data: JSON.parse(event.data) as unknown });
    } catch {
      onEvent({ type, data: event.data });
    }
  };

  eventSource.addEventListener("v2.connected", parse("v2.connected"));
  eventSource.addEventListener("v2.event", (event: MessageEvent) => {
    try {
      onEvent({
        type: "v2.event",
        data: JSON.parse(event.data) as V2RuntimeEvent,
      });
    } catch {
      onEvent({ type: "v2.event", data: event.data });
    }
  });
  eventSource.addEventListener(
    "v2.replay_complete",
    parse("v2.replay_complete"),
  );
  eventSource.addEventListener("v2.error", parse("v2.error"));
  eventSource.addEventListener("heartbeat", parse("heartbeat"));
  eventSource.onerror = (error) => {
    onError?.(error);
  };

  return () => eventSource.close();
}

// Tasks
export async function getTasks(params?: { limit?: number }): Promise<{
  tasks: TaskSummary[];
  stats: TaskStats;
}> {
  const searchParams = new URLSearchParams();
  if (params?.limit) searchParams.set("limit", String(params.limit));
  const query = searchParams.toString();
  return request(`/api/tasks${query ? `?${query}` : ""}`);
}

// Stats
export async function getStats(): Promise<JobStats> {
  return request("/api/stats");
}

// Insights
export async function getInsights(): Promise<InsightsData> {
  return request("/api/insights");
}

// Mailbox
export async function getMailbox(params?: {
  actor?: string;
  limit?: number;
  workspace?: string;
}): Promise<{ messages: MailboxMessage[] }> {
  const searchParams = new URLSearchParams();
  if (params?.actor) searchParams.set("actor", params.actor);
  if (params?.limit) searchParams.set("limit", String(params.limit));
  if (params?.workspace) searchParams.set("workspace_id", params.workspace);
  const query = searchParams.toString();
  return request(`/api/mailbox${query ? `?${query}` : ""}`);
}

// Reservations
export async function getReservations(params?: {
  workspace?: string;
}): Promise<{
  reservations: Reservation[];
}> {
  const searchParams = new URLSearchParams();
  if (params?.workspace) searchParams.set("workspace_id", params.workspace);
  const query = searchParams.toString();
  return request(`/api/reservations${query ? `?${query}` : ""}`);
}

// Blackboard
export async function getBlackboard(params?: {
  ns?: string;
  topic?: string;
  limit?: number;
}): Promise<{ records: BlackboardRecord[] }> {
  const searchParams = new URLSearchParams();
  if (params?.ns) searchParams.set("ns", params.ns);
  if (params?.topic) searchParams.set("topic", params.topic);
  if (params?.limit) searchParams.set("limit", String(params.limit));
  const query = searchParams.toString();
  return request(`/api/blackboard${query ? `?${query}` : ""}`);
}

export async function getOrchestrationBoard(params?: {
  request_id?: string;
  workspace_id?: string;
  limit?: number;
  cursor?: string;
  lane?: OrchestrationLaneID;
}): Promise<OrchestrationBoardResult> {
  const query = new URLSearchParams();
  if (params?.request_id) query.set("request_id", params.request_id);
  if (params?.workspace_id) query.set("workspace_id", params.workspace_id);
  if (typeof params?.limit === "number" && Number.isFinite(params.limit)) {
    query.set("limit", String(params.limit));
  }
  if (params?.cursor) query.set("cursor", params.cursor);
  if (params?.lane) query.set("lane", params.lane);

  const suffix = query.size > 0 ? `?${query.toString()}` : "";
  const env = await request<ApiEnvelope<unknown>>(
    `/api/orchestration/board-get${suffix}`,
  );
  return normalizeBoardPayload(unwrapEnvelope(env));
}

export async function getOrchestrationBoardCardRuntime(params: {
  request_id?: string;
  workspace_id?: string;
  issue_id: string;
  depth?: number;
}): Promise<OrchestrationBoardCardRuntimeResult> {
  const query = new URLSearchParams();
  if (params.request_id) query.set("request_id", params.request_id);
  if (params.workspace_id) query.set("workspace_id", params.workspace_id);
  query.set("issue_id", params.issue_id);
  if (typeof params.depth === "number" && Number.isFinite(params.depth)) {
    query.set("depth", String(params.depth));
  }

  const env = await request<ApiEnvelope<OrchestrationBoardCardRuntimeResult>>(
    `/api/orchestration/board-card-runtime-get?${query.toString()}`,
  );
  return unwrapEnvelope(env);
}

export async function applyOrchestrationCardAction(params: {
  request_id: string;
  workspace_id?: string;
  issue_id: string;
  action: OrchestrationCardAction;
}): Promise<OrchestrationCardActionResult> {
  const env = await request<ApiEnvelope<OrchestrationCardActionResult>>(
    "/api/orchestration/card-action",
    {
      method: "POST",
      body: JSON.stringify(params),
    },
  );
  return unwrapEnvelope(env);
}

export async function dispatchOrchestrationIssue(params: {
  request_id: string;
  workspace_id?: string;
  issue_id: string;
  issue_identifier?: string;
  title?: string;
  prompt?: string;
  parent_agent_id?: string;
}): Promise<OrchestrationDispatchResult> {
  const env = await request<ApiEnvelope<OrchestrationDispatchResult>>(
    "/api/orchestration/dispatch-issue",
    {
      method: "POST",
      body: JSON.stringify(params),
    },
  );
  return unwrapEnvelope(env);
}

export async function refreshOrchestration(params: {
  request_id: string;
  workspace_id?: string;
}): Promise<OrchestrationRefreshResult> {
  const env = await request<ApiEnvelope<OrchestrationRefreshResult>>(
    "/api/orchestration/refresh",
    {
      method: "POST",
      body: JSON.stringify(params),
    },
  );
  return unwrapEnvelope(env);
}

// SQLite
export async function getSQLiteDatabases(): Promise<{
  databases: SQLiteDatabase[];
}> {
  return request("/api/sqlite");
}

export async function getSQLiteTables(
  db: string,
): Promise<{ tables: SQLiteTable[] }> {
  return request(`/api/sqlite/${encodeURIComponent(db)}`);
}

export async function getSQLiteData(
  db: string,
  table: string,
  limit = 100,
  offset = 0,
): Promise<{
  columns: string[];
  rows: Record<string, unknown>[];
  total_count: number;
  limit: number;
  offset: number;
}> {
  return request(
    `/api/sqlite/${encodeURIComponent(db)}/${encodeURIComponent(table)}?limit=${limit}&offset=${offset}`,
  );
}

export async function getSQLiteSchema(
  db: string,
  table: string,
): Promise<{
  schema: string;
  columns: SQLiteColumn[];
}> {
  return request(
    `/api/sqlite/${encodeURIComponent(db)}/${encodeURIComponent(table)}/schema`,
  );
}

export async function getSQLiteIndexes(
  db: string,
  table?: string,
): Promise<{ indexes: SQLiteIndex[] }> {
  const params = table ? `?table=${encodeURIComponent(table)}` : "";
  return request(`/api/sqlite/${encodeURIComponent(db)}/indexes${params}`);
}

export async function executeSQLiteQuery(
  db: string,
  query: string,
  limit = 100,
): Promise<SQLiteQueryResult> {
  return request(`/api/sqlite/${encodeURIComponent(db)}/query`, {
    method: "POST",
    body: JSON.stringify({ query, limit }),
  });
}

// Search
export async function search(params: {
  q: string;
  limit?: number;
  rerank?: boolean;
  scope?: string;
  workspace?: string;
}): Promise<{
  results: SearchResult[];
  stats: SearchStats;
}> {
  const searchParams = new URLSearchParams();
  searchParams.set("q", params.q);
  if (params.limit) searchParams.set("limit", String(params.limit));
  if (params.rerank) searchParams.set("rerank", "true");
  if (params.scope) searchParams.set("scope", params.scope);
  if (params.workspace) searchParams.set("workspace", params.workspace);
  return request(`/api/search?${searchParams.toString()}`);
}

// Workspaces
export async function getWorkspaces(): Promise<{
  workspaces: Workspace[];
  current: string;
}> {
  return request("/api/workspaces");
}

export async function switchWorkspace(workspace: string): Promise<void> {
  return requestVoid(
    `/api/workspaces/switch?workspace=${encodeURIComponent(workspace)}`,
    { method: "POST" },
  );
}

// Sessions
export async function getSessions(params?: {
  limit?: number;
  offset?: number;
}): Promise<{
  sessions: Session[];
  total: number;
  limit: number;
  offset: number;
}> {
  const searchParams = new URLSearchParams();
  if (params?.limit) searchParams.set("limit", String(params.limit));
  if (params?.offset) searchParams.set("offset", String(params.offset));
  const query = searchParams.toString();
  return request(`/api/sessions${query ? `?${query}` : ""}`);
}

export async function getSession(id: string): Promise<{ session: Session }> {
  return request(`/api/sessions/${encodeURIComponent(id)}`);
}

export async function getSessionMessages(
  id: string,
  params?: { limit?: number; offset?: number },
): Promise<{
  messages: SessionMessage[];
  total: number;
  limit: number;
  offset: number;
  path: string;
}> {
  const searchParams = new URLSearchParams();
  if (params?.limit) searchParams.set("limit", String(params.limit));
  if (params?.offset) searchParams.set("offset", String(params.offset));
  const query = searchParams.toString();
  return request(
    `/api/sessions/${encodeURIComponent(id)}/messages${query ? `?${query}` : ""}`,
  );
}

export async function updateSessionMessage(
  sessionId: string,
  index: number,
  message: unknown,
): Promise<{ success: boolean; index: number }> {
  return request(
    `/api/sessions/${encodeURIComponent(sessionId)}/messages/${index}`,
    {
      method: "PUT",
      body: JSON.stringify({ message }),
    },
  );
}

export async function searchSessions(params: {
  pattern: string;
  limit?: number;
}): Promise<{
  results: SessionSearchResult[];
  total: number;
  pattern: string;
}> {
  const searchParams = new URLSearchParams();
  searchParams.set("pattern", params.pattern);
  if (params.limit) searchParams.set("limit", String(params.limit));
  return request(`/api/sessions/search?${searchParams.toString()}`);
}

export async function getSessionContextWindows(id: string): Promise<{
  context_windows: ContextWindow[];
  total: number;
}> {
  return request(`/api/sessions/${encodeURIComponent(id)}/context-windows`);
}

// Codemaps
export async function getCodemaps(params?: {
  workspace?: string;
  limit?: number;
}): Promise<{ codemaps: CodemapListItem[] }> {
  const searchParams = new URLSearchParams();
  if (params?.workspace) searchParams.set("workspace", params.workspace);
  if (params?.limit) searchParams.set("limit", String(params.limit));
  const query = searchParams.toString();
  return request(`/api/codemaps${query ? `?${query}` : ""}`);
}

export async function getCodemap(
  id: string,
  workspace?: string,
): Promise<Codemap> {
  const searchParams = new URLSearchParams();
  if (workspace) searchParams.set("workspace", workspace);
  const query = searchParams.toString();
  return request(
    `/api/codemaps/${encodeURIComponent(id)}${query ? `?${query}` : ""}`,
  );
}

export async function deleteCodemap(
  id: string,
  workspace?: string,
): Promise<void> {
  const searchParams = new URLSearchParams();
  if (workspace) searchParams.set("workspace", workspace);
  const query = searchParams.toString();
  return requestVoid(
    `/api/codemaps/${encodeURIComponent(id)}${query ? `?${query}` : ""}`,
    {
      method: "DELETE",
    },
  );
}

export async function searchCodemaps(params: {
  query: string;
  limit?: number;
  workspace?: string;
}): Promise<{ results: SearchResult[] }> {
  const searchParams = new URLSearchParams();
  searchParams.set("q", params.query);
  if (params.limit) searchParams.set("limit", String(params.limit));
  if (params.workspace) searchParams.set("workspace", params.workspace);
  return request(`/api/codemaps/search?${searchParams.toString()}`);
}

// ============================================================================
// CAS (Content Addressable Storage)
// ============================================================================

export async function listCASObjects(): Promise<{ objects: CASObject[] }> {
  return request("/api/cas");
}

export async function readCASObject(params: {
  digest: string;
  page?: number;
  pageSize?: number;
}): Promise<CASReadResult> {
  const searchParams = new URLSearchParams();
  if (params.page) searchParams.set("page", String(params.page));
  if (params.pageSize) searchParams.set("page_size", String(params.pageSize));

  const queryString = searchParams.toString();
  const url = `/api/cas/${encodeURIComponent(params.digest)}/read${queryString ? `?${queryString}` : ""}`;
  return request(url);
}

export async function pinCASObject(
  digest: string,
): Promise<{ ok: boolean; digest: string; pinned: boolean }> {
  return request(`/api/cas/${encodeURIComponent(digest)}/pin`, {
    method: "POST",
  });
}

export async function unpinCASObject(
  digest: string,
): Promise<{ ok: boolean; digest: string; pinned: boolean }> {
  return request(`/api/cas/${encodeURIComponent(digest)}/unpin`, {
    method: "POST",
  });
}

// ============================================================================
// Memory (named_memory from memory.db)
// ============================================================================

export async function getMemoryEntries(params?: {
  type?: string;
  limit?: number;
  offset?: number;
}): Promise<{
  memories: MemoryEntry[];
  total: number;
  limit: number;
  offset: number;
}> {
  const searchParams = new URLSearchParams();
  if (params?.type) searchParams.set("type", params.type);
  if (params?.limit) searchParams.set("limit", String(params.limit));
  if (params?.offset) searchParams.set("offset", String(params.offset));
  const query = searchParams.toString();
  return request(`/api/memory${query ? `?${query}` : ""}`);
}

export async function getMemoryTypes(): Promise<{
  types: MemoryTypeCount[];
}> {
  return request("/api/memory/types");
}

export async function getMemoryEntry(id: string): Promise<MemoryEntryDetail> {
  return request(`/api/memory/${encodeURIComponent(id)}`);
}

export async function saveMemory(params: {
  name: string;
  type: string;
  summary?: string;
  data?: unknown;
}): Promise<{ success: boolean; id?: string; error?: string }> {
  return request("/api/memory", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(params),
  });
}

export async function pinMemory(
  id: string,
): Promise<{ success: boolean; pinned: boolean; pinned_at: string | null }> {
  return request(`/api/memory/${encodeURIComponent(id)}/pin`, {
    method: "POST",
  });
}

export async function deleteMemory(
  id: string,
): Promise<{ success: boolean; deleted: string }> {
  return request(`/api/memory/${encodeURIComponent(id)}`, {
    method: "DELETE",
  });
}

// ============================================================================
// Agents
// ============================================================================

export async function getAgents(params?: {
  state?: AgentState;
  limit?: number;
}): Promise<{
  agents: Agent[];
  total: number;
}> {
  const searchParams = new URLSearchParams();
  if (params?.state) searchParams.set("state", params.state);
  if (params?.limit) searchParams.set("limit", String(params.limit));
  const query = searchParams.toString();
  return request(`/api/agents${query ? `?${query}` : ""}`);
}

export async function getAgent(id: string): Promise<{ agent: Agent }> {
  return request(`/api/agents/${encodeURIComponent(id)}`);
}

// Spawn a new agent
export interface SpawnAgentParams {
  workspace_id?: string;
  role?: string;
  parent_id?: string;
  prompt?: string;
  skills_allow?: string[];
  llm_provider?: string;
  llm_model?: string;
  name?: string;
  slug?: string;
  exec_mode?: string;
  think_interval?: number;
  max_iterations?: number;
  max_context_tokens?: number;
  max_auto_turns?: number;
  memory_scope?: "agent" | "session";
  memory_retention?: "companion" | "durable" | "task" | "ephemeral";
  room_id?: string;
  room_role?: string;
}

export async function spawnAgent(params: SpawnAgentParams): Promise<{
  session_id: string;
  actor_id: string;
  status: string;
  name?: string;
}> {
  return request("/api/agents", {
    method: "POST",
    body: JSON.stringify(params),
  });
}

export async function getAgentMemoryStats(
  id: string,
): Promise<AgentMemoryStatsResult> {
  return request(`/api/agents/${encodeURIComponent(id)}/memory/stats`);
}

export async function getAgentMemoryContext(
  id: string,
  params?: { conversation_id?: string },
): Promise<AgentMemoryContextResult> {
  const query = new URLSearchParams();
  if (params?.conversation_id) {
    query.set("conversation_id", params.conversation_id);
  }
  const suffix = query.size > 0 ? `?${query.toString()}` : "";
  return request(
    `/api/agents/${encodeURIComponent(id)}/memory/context${suffix}`,
  );
}

export async function searchAgentMemory(
  id: string,
  params: { q: string; limit?: number; conversation_id?: string },
): Promise<AgentMemorySearchResult> {
  const query = new URLSearchParams();
  query.set("q", params.q);
  if (typeof params.limit === "number" && Number.isFinite(params.limit)) {
    query.set("limit", String(params.limit));
  }
  if (params.conversation_id) {
    query.set("conversation_id", params.conversation_id);
  }
  return request(
    `/api/agents/${encodeURIComponent(id)}/memory/search?${query.toString()}`,
  );
}

export async function compressAgentMemory(
  id: string,
  params?: { conversation_id?: string; distill?: boolean },
): Promise<AgentMemoryCompressResult> {
  return request(`/api/agents/${encodeURIComponent(id)}/memory/compress`, {
    method: "POST",
    body: JSON.stringify(params || {}),
  });
}

export async function getRoom(
  roomId: string,
  params: { workspace_id: string; actor_id?: string },
): Promise<{ room: Room }> {
  const query = new URLSearchParams();
  query.set("workspace_id", params.workspace_id);
  if (params.actor_id) query.set("actor_id", params.actor_id);
  return request(
    `/api/rooms/${encodeURIComponent(roomId)}?${query.toString()}`,
  );
}

export async function getRooms(params: {
  workspace_id: string;
  actor_id?: string;
  limit?: number;
  archived_only?: boolean;
}): Promise<{ rooms: Room[]; count: number }> {
  const query = new URLSearchParams();
  query.set("workspace_id", params.workspace_id);
  if (params.actor_id) query.set("actor_id", params.actor_id);
  if (typeof params.limit === "number") query.set("limit", String(params.limit));
  if (params.archived_only) query.set("archived_only", "true");
  return request(`/api/rooms?${query.toString()}`);
}

export async function getRoomTasks(
  roomId: string,
  params: { workspace_id: string; status?: string; limit?: number },
): Promise<{ room: Room; tasks: RoomTask[]; count: number }> {
  const query = new URLSearchParams();
  query.set("workspace_id", params.workspace_id);
  if (params.status) query.set("status", params.status);
  if (typeof params.limit === "number") query.set("limit", String(params.limit));
  return request(
    `/api/rooms/${encodeURIComponent(roomId)}/tasks?${query.toString()}`,
  );
}

// Stop/kill an agent
export async function stopAgent(id: string): Promise<{
  stopped: boolean;
  agent_id: string;
  previous_state: string;
}> {
  return request(`/api/agents/${encodeURIComponent(id)}`, {
    method: "DELETE",
  });
}

// Update agent state
export async function updateAgentState(
  id: string,
  state: AgentState,
): Promise<{
  updated: boolean;
  agent_id: string;
  state: string;
}> {
  return request(`/api/agents/${encodeURIComponent(id)}`, {
    method: "PATCH",
    body: JSON.stringify({ state }),
  });
}

// Send a message to a specific agent
export async function sendAgentMessage(
  agentId: string,
  params: {
    subject: string;
    body?: string;
    kind?: string;
    priority?: number;
    sender?: string;
  },
): Promise<{
  message_id: string;
  recipient: string;
  status: string;
}> {
  return request(`/api/agents/${encodeURIComponent(agentId)}/message`, {
    method: "POST",
    body: JSON.stringify(params),
  });
}

// ============================================================================
// Mailbox Messaging
// ============================================================================

// Send a message via mailbox
export interface SendMailboxMessageParams {
  recipient: string;
  subject: string;
  body?: string;
  kind?: string;
  priority?: number;
  sender?: string;
  ack_required?: boolean;
  headers?: Record<string, string>;
}

export async function sendMailboxMessage(
  params: SendMailboxMessageParams,
): Promise<{
  message_id: string;
  status: string;
}> {
  return request("/api/mailbox/send", {
    method: "POST",
    body: JSON.stringify(params),
  });
}

// Acknowledge a mailbox message (mark as read/handled)
export async function acknowledgeMessage(
  messageId: string,
  actorId?: string,
): Promise<{
  acknowledged: boolean;
  message_id: string;
}> {
  return request(`/api/mailbox/${encodeURIComponent(messageId)}/ack`, {
    method: "POST",
    body: JSON.stringify({ actor_id: actorId }),
  });
}

// ============================================================================
// Trajectories
// ============================================================================

export async function getTrajectories(params?: {
  status?: string;
  agent_role?: string;
  limit?: number;
}): Promise<{
  trajectories: Trajectory[];
  total: number;
}> {
  const searchParams = new URLSearchParams();
  if (params?.status) searchParams.set("status", params.status);
  if (params?.agent_role) searchParams.set("agent_role", params.agent_role);
  if (params?.limit) searchParams.set("limit", String(params.limit));
  const query = searchParams.toString();
  return request(`/api/trajectories${query ? `?${query}` : ""}`);
}

export async function getTrajectoryEvents(
  trajectoryId: string,
  params?: { kind?: string; limit?: number },
): Promise<{ events: TrajectoryEvent[] }> {
  const searchParams = new URLSearchParams();
  if (params?.kind) searchParams.set("kind", params.kind);
  if (params?.limit) searchParams.set("limit", String(params.limit));
  const query = searchParams.toString();
  return request(
    `/api/trajectories/${encodeURIComponent(trajectoryId)}/events${query ? `?${query}` : ""}`,
  );
}

// Trajectory feedback (DSPy training)
export interface TrajectoryFeedback {
  rating: number;
  comment?: string | null;
  recorded_at?: string;
}

export async function getTrajectoryFeedback(
  trajectoryId: string,
): Promise<{ feedback: TrajectoryFeedback | null }> {
  return request(
    `/api/trajectories/${encodeURIComponent(trajectoryId)}/feedback`,
  );
}

export async function submitTrajectoryFeedback(
  trajectoryId: string,
  params: { rating: number; comment?: string },
): Promise<{
  success: boolean;
  trajectory_id: string;
  rating: number;
  comment: string | null;
  recorded_at: string;
}> {
  return request(
    `/api/trajectories/${encodeURIComponent(trajectoryId)}/feedback`,
    {
      method: "POST",
      body: JSON.stringify(params),
    },
  );
}

// Scorer weights (learnable scoring system)
export interface ScorerWeights {
  critical_path: number;
  page_rank: number;
  admin_mail: number;
  overseer_mail: number;
  recency: number;
  version: number;
  last_updated: string | null;
}

export interface WeightUpdate {
  previous_weights: ScorerWeights | null;
  new_weights: ScorerWeights | null;
  timestamp: string;
  reason: string;
  sample_size: number;
}

export async function getScorerWeights(): Promise<{
  weights: ScorerWeights;
  history: WeightUpdate[];
}> {
  return request("/api/weights");
}

export async function getUserRequests(params?: {
  limit?: number;
}): Promise<{ requests: UserRequest[] }> {
  const searchParams = new URLSearchParams();
  if (params?.limit) searchParams.set("limit", String(params.limit));
  const query = searchParams.toString();
  return request(`/api/user-requests${query ? `?${query}` : ""}`);
}

// SSE for real-time updates (browser only)
export function subscribeToEvents(
  onMessage: (event: SSEEvent) => void,
  onError?: (error: Event) => void,
  onConnected?: () => void,
): () => void {
  if (typeof EventSource === "undefined") {
    console.warn("EventSource not available in this environment");
    return () => {};
  }

  const eventSource = new EventSource(`${API_BASE}/api/events`);

  eventSource.onmessage = (event) => {
    try {
      const data = JSON.parse(event.data) as SSEEvent;
      if (data.type === "connected") {
        onConnected?.();
      }
      onMessage(data);
    } catch {
      // Ignore parse errors
    }
  };

  eventSource.onerror = (error) => {
    onError?.(error);
  };

  return () => eventSource.close();
}

// ============================================================================
// Console Sessions
// ============================================================================

export async function getConsoles(params?: {
  limit?: number;
}): Promise<{ consoles: ConsoleSession[]; total: number }> {
  const searchParams = new URLSearchParams();
  if (params?.limit) searchParams.set("limit", String(params.limit));
  const query = searchParams.toString();
  return request(`/api/consoles${query ? `?${query}` : ""}`);
}

export async function createConsole(
  data: ConsoleSessionCreate,
): Promise<ConsoleSession> {
  return request("/api/consoles", {
    method: "POST",
    body: JSON.stringify(data),
  });
}

export async function getConsole(id: string): Promise<ConsoleSession> {
  return request(`/api/consoles/${encodeURIComponent(id)}`);
}

export async function deleteConsole(id: string): Promise<void> {
  return requestVoid(`/api/consoles/${encodeURIComponent(id)}`, {
    method: "DELETE",
  });
}

export async function sendConsoleMessage(
  consoleId: string,
  data: ConsoleSendRequest,
): Promise<ConsoleSendResponse> {
  return request(`/api/consoles/${encodeURIComponent(consoleId)}/send`, {
    method: "POST",
    body: JSON.stringify(data),
  });
}

export async function cancelConsoleRequest(
  consoleId: string,
  data?: ConsoleCancelRequest,
): Promise<ConsoleCancelResponse> {
  return request(`/api/consoles/${encodeURIComponent(consoleId)}/cancel`, {
    method: "POST",
    body: JSON.stringify(data || {}),
  });
}

export async function submitConsoleFeedback(
  consoleId: string,
  data: ConsoleFeedbackRequest,
): Promise<ConsoleFeedbackResponse> {
  return request(`/api/consoles/${encodeURIComponent(consoleId)}/feedback`, {
    method: "POST",
    body: JSON.stringify(data),
  });
}

// SSE for console-specific events (browser only)
export function subscribeToConsoleEvents(
  consoleId: string,
  onMessage: (event: SSEEvent) => void,
  onError?: (error: Event) => void,
  onConnected?: () => void,
): () => void {
  if (typeof EventSource === "undefined") {
    console.warn("EventSource not available in this environment");
    return () => {};
  }

  const eventSource = new EventSource(
    `${API_BASE}/api/consoles/${encodeURIComponent(consoleId)}/events`,
  );

  eventSource.onmessage = (event) => {
    try {
      const data = JSON.parse(event.data) as SSEEvent;
      if (data.type === "connected") {
        onConnected?.();
      }
      onMessage(data);
    } catch {
      // Ignore parse errors
    }
  };

  eventSource.onerror = (error) => {
    onError?.(error);
  };

  return () => eventSource.close();
}
