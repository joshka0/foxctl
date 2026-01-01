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
  Codemap,
  CodemapListItem,
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
}): Promise<{ messages: MailboxMessage[] }> {
  const searchParams = new URLSearchParams();
  if (params?.actor) searchParams.set("actor", params.actor);
  if (params?.limit) searchParams.set("limit", String(params.limit));
  const query = searchParams.toString();
  return request(`/api/mailbox${query ? `?${query}` : ""}`);
}

// Reservations
export async function getReservations(): Promise<{
  reservations: Reservation[];
}> {
  return request("/api/reservations");
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
}): Promise<{
  results: SearchResult[];
  stats: SearchStats;
}> {
  const searchParams = new URLSearchParams();
  searchParams.set("q", params.q);
  if (params.limit) searchParams.set("limit", String(params.limit));
  if (params.rerank) searchParams.set("rerank", "true");
  if (params.scope) searchParams.set("scope", params.scope);
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

export async function getCodemap(id: string): Promise<Codemap> {
  return request(`/api/codemaps/${encodeURIComponent(id)}`);
}

export async function deleteCodemap(id: string): Promise<void> {
  return requestVoid(`/api/codemaps/${encodeURIComponent(id)}`, {
    method: "DELETE",
  });
}

export async function searchCodemaps(params: {
  query: string;
  limit?: number;
}): Promise<{ results: SearchResult[] }> {
  const searchParams = new URLSearchParams();
  searchParams.set("q", params.query);
  if (params.limit) searchParams.set("limit", String(params.limit));
  return request(`/api/codemaps/search?${searchParams.toString()}`);
}

// SSE for real-time updates (browser only)
export function subscribeToEvents(
  onMessage: (event: { type: string; data: unknown }) => void,
  onError?: (error: Event) => void
): () => void {
  if (typeof EventSource === "undefined") {
    console.warn("EventSource not available in this environment");
    return () => {};
  }

  const eventSource = new EventSource(`${API_BASE}/api/events`);

  eventSource.onmessage = (event) => {
    try {
      const data = JSON.parse(event.data);
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
