// API Client - connects to the agentctl backend
// Works in both browser (Vite) and Bun environments

import type {
  JobSummary,
  JobDetail,
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
  AgentState,
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
  if (typeof process !== "undefined" && process.env?.AGENTCTL_API_URL) {
    return process.env.AGENTCTL_API_URL;
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

async function request<T>(
  endpoint: string,
  options: RequestInit = {}
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
  options: RequestInit = {}
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

// SQLite
export async function getSQLiteDatabases(): Promise<{
  databases: SQLiteDatabase[];
}> {
  return request("/api/sqlite");
}

export async function getSQLiteTables(
  db: string
): Promise<{ tables: SQLiteTable[] }> {
  return request(`/api/sqlite/${encodeURIComponent(db)}`);
}

export async function getSQLiteData(
  db: string,
  table: string,
  limit = 100,
  offset = 0
): Promise<{
  columns: string[];
  rows: Record<string, unknown>[];
  total_count: number;
  limit: number;
  offset: number;
}> {
  return request(`/api/sqlite/${encodeURIComponent(db)}/${encodeURIComponent(table)}?limit=${limit}&offset=${offset}`);
}

export async function getSQLiteSchema(
  db: string,
  table: string
): Promise<{
  schema: string;
  columns: SQLiteColumn[];
}> {
  return request(`/api/sqlite/${encodeURIComponent(db)}/${encodeURIComponent(table)}/schema`);
}

export async function getSQLiteIndexes(
  db: string,
  table?: string
): Promise<{ indexes: SQLiteIndex[] }> {
  const params = table ? `?table=${encodeURIComponent(table)}` : "";
  return request(`/api/sqlite/${encodeURIComponent(db)}/indexes${params}`);
}

export async function executeSQLiteQuery(
  db: string,
  query: string,
  limit = 100
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
    { method: "POST" }
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
  params?: { limit?: number; offset?: number }
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
  return request(`/api/sessions/${encodeURIComponent(id)}/messages${query ? `?${query}` : ""}`);
}

export async function updateSessionMessage(
  sessionId: string,
  index: number,
  message: unknown
): Promise<{ success: boolean; index: number }> {
  return request(`/api/sessions/${encodeURIComponent(sessionId)}/messages/${index}`, {
    method: "PUT",
    body: JSON.stringify({ message }),
  });
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

export async function getSessionContextWindows(
  id: string
): Promise<{
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

export async function getCodemap(id: string, workspace?: string): Promise<Codemap> {
  const searchParams = new URLSearchParams();
  if (workspace) searchParams.set("workspace", workspace);
  const query = searchParams.toString();
  return request(`/api/codemaps/${encodeURIComponent(id)}${query ? `?${query}` : ""}`);
}

export async function deleteCodemap(id: string, workspace?: string): Promise<void> {
  const searchParams = new URLSearchParams();
  if (workspace) searchParams.set("workspace", workspace);
  const query = searchParams.toString();
  return requestVoid(`/api/codemaps/${encodeURIComponent(id)}${query ? `?${query}` : ""}`, {
    method: "DELETE",
  });
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
  if (params.pageSize) searchParams.set("pageSize", String(params.pageSize));

  const queryString = searchParams.toString();
  const url = `/api/cas/${encodeURIComponent(params.digest)}${queryString ? `?${queryString}` : ""}`;
  return request(url);
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
  id: string
): Promise<{ success: boolean; pinned: boolean; pinned_at: string | null }> {
  return request(`/api/memory/${encodeURIComponent(id)}/pin`, {
    method: "POST",
  });
}

export async function deleteMemory(
  id: string
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
  ns: string;
  role?: string;
  parent_id?: string;
  prompt?: string;
  skills_allow?: string;
  policy?: string;
  share_bb?: "all" | "scoped" | "none";
  llm_provider?: string;
  llm_model?: string;
}

export async function spawnAgent(params: SpawnAgentParams): Promise<{
  agent: {
    id: string;
    ns: string;
    role: string;
    state: string;
    parent_id: string | null;
  };
}> {
  return request("/api/agents", {
    method: "POST",
    body: JSON.stringify(params),
  });
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
  state: AgentState
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
  }
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
  params: SendMailboxMessageParams
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
  actorId?: string
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
  params?: { kind?: string; limit?: number }
): Promise<{ events: TrajectoryEvent[] }> {
  const searchParams = new URLSearchParams();
  if (params?.kind) searchParams.set("kind", params.kind);
  if (params?.limit) searchParams.set("limit", String(params.limit));
  const query = searchParams.toString();
  return request(
    `/api/trajectories/${encodeURIComponent(trajectoryId)}/events${query ? `?${query}` : ""}`
  );
}

// Trajectory feedback (DSPy training)
export interface TrajectoryFeedback {
  rating: number;
  comment?: string | null;
  recorded_at?: string;
}

export async function getTrajectoryFeedback(
  trajectoryId: string
): Promise<{ feedback: TrajectoryFeedback | null }> {
  return request(`/api/trajectories/${encodeURIComponent(trajectoryId)}/feedback`);
}

export async function submitTrajectoryFeedback(
  trajectoryId: string,
  params: { rating: number; comment?: string }
): Promise<{
  success: boolean;
  trajectory_id: string;
  rating: number;
  comment: string | null;
  recorded_at: string;
}> {
  return request(`/api/trajectories/${encodeURIComponent(trajectoryId)}/feedback`, {
    method: "POST",
    body: JSON.stringify(params),
  });
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
  onConnected?: () => void
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
  data: ConsoleSessionCreate
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
  data: ConsoleSendRequest
): Promise<ConsoleSendResponse> {
  return request(`/api/consoles/${encodeURIComponent(consoleId)}/send`, {
    method: "POST",
    body: JSON.stringify(data),
  });
}

export async function cancelConsoleRequest(
  consoleId: string,
  data?: ConsoleCancelRequest
): Promise<ConsoleCancelResponse> {
  return request(`/api/consoles/${encodeURIComponent(consoleId)}/cancel`, {
    method: "POST",
    body: JSON.stringify(data || {}),
  });
}

export async function submitConsoleFeedback(
  consoleId: string,
  data: ConsoleFeedbackRequest
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
  onConnected?: () => void
): () => void {
  if (typeof EventSource === "undefined") {
    console.warn("EventSource not available in this environment");
    return () => {};
  }

  const eventSource = new EventSource(
    `${API_BASE}/api/consoles/${encodeURIComponent(consoleId)}/events`
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
