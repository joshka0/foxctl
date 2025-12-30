// API Client - connects to the Go backend
// In development, Vite proxies /api/* to localhost:8090
// In production, API_BASE can be set via VITE_API_URL

const API_BASE = import.meta.env.VITE_API_URL || "";

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
    // For endpoints that return void, caller should use requestVoid instead
    return null as T;
  }

  return JSON.parse(text) as T;
}

// Specialized request for endpoints that return no body (void responses)
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

  // Don't parse response body for void endpoints
}

// Jobs
export async function getJobs(params?: {
  state?: string;
  limit?: number;
}): Promise<{ jobs: import("../types").JobSummary[] }> {
  const searchParams = new URLSearchParams();
  if (params?.state) searchParams.set("state", params.state);
  if (params?.limit) searchParams.set("limit", String(params.limit));
  const query = searchParams.toString();
  return request(`/api/jobs${query ? `?${query}` : ""}`);
}

export async function getJobDetail(
  id: string
): Promise<import("../types").JobDetail> {
  return request(`/api/jobs/${id}`);
}

// Tasks
export async function getTasks(params?: {
  limit?: number;
}): Promise<{ tasks: import("../types").TaskSummary[]; stats: import("../types").TaskStats }> {
  const searchParams = new URLSearchParams();
  if (params?.limit) searchParams.set("limit", String(params.limit));
  const query = searchParams.toString();
  return request(`/api/tasks${query ? `?${query}` : ""}`);
}

// Stats
export async function getStats(): Promise<import("../types").JobStats> {
  return request("/api/stats");
}

// Insights
export async function getInsights(): Promise<import("../types").InsightsData> {
  return request("/api/insights");
}

// Mailbox
export async function getMailbox(params?: {
  actor?: string;
  limit?: number;
}): Promise<{ messages: import("../types").MailboxMessage[] }> {
  const searchParams = new URLSearchParams();
  if (params?.actor) searchParams.set("actor", params.actor);
  if (params?.limit) searchParams.set("limit", String(params.limit));
  const query = searchParams.toString();
  return request(`/api/mailbox${query ? `?${query}` : ""}`);
}

// Reservations
export async function getReservations(): Promise<{
  reservations: import("../types").Reservation[];
}> {
  return request("/api/reservations");
}

// Blackboard
export async function getBlackboard(params?: {
  ns?: string;
  topic?: string;
  limit?: number;
}): Promise<{ records: import("../types").BlackboardRecord[] }> {
  const searchParams = new URLSearchParams();
  if (params?.ns) searchParams.set("ns", params.ns);
  if (params?.topic) searchParams.set("topic", params.topic);
  if (params?.limit) searchParams.set("limit", String(params.limit));
  const query = searchParams.toString();
  return request(`/api/blackboard${query ? `?${query}` : ""}`);
}

// SQLite
export async function getSQLiteDatabases(): Promise<{
  databases: import("../types").SQLiteDatabase[];
}> {
  return request("/api/sqlite");
}

export async function getSQLiteTables(
  db: string
): Promise<{ tables: import("../types").SQLiteTable[] }> {
  return request(`/api/sqlite/${db}`);
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
  return request(`/api/sqlite/${db}/${table}?limit=${limit}&offset=${offset}`);
}

export async function getSQLiteSchema(
  db: string,
  table: string
): Promise<{
  schema: string;
  columns: import("../types").SQLiteColumn[];
}> {
  return request(`/api/sqlite/${db}/${table}/schema`);
}

export async function getSQLiteIndexes(
  db: string,
  table?: string
): Promise<{ indexes: import("../types").SQLiteIndex[] }> {
  const params = table ? `?table=${encodeURIComponent(table)}` : "";
  return request(`/api/sqlite/${db}/indexes${params}`);
}

export async function executeSQLiteQuery(
  db: string,
  query: string,
  limit = 100
): Promise<import("../types").SQLiteQueryResult> {
  return request(`/api/sqlite/${db}/query`, {
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
  results: import("../types").SearchResult[];
  stats: import("../types").SearchStats;
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
  workspaces: import("../types").Workspace[];
  current: string;
}> {
  return request("/api/workspaces");
}

export async function switchWorkspace(workspace: string): Promise<void> {
  return requestVoid(`/api/workspaces/switch?workspace=${encodeURIComponent(workspace)}`, {
    method: "POST",
  });
}

// Sessions
export async function getSessions(params?: {
  limit?: number;
  offset?: number;
}): Promise<{
  sessions: import("../types").Session[];
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

export async function getSession(id: string): Promise<{
  session: import("../types").Session;
}> {
  return request(`/api/sessions/${id}`);
}

export async function getSessionMessages(
  id: string,
  params?: { limit?: number; offset?: number }
): Promise<{
  messages: import("../types").SessionMessage[];
  total: number;
  limit: number;
  offset: number;
  path: string;
}> {
  const searchParams = new URLSearchParams();
  if (params?.limit) searchParams.set("limit", String(params.limit));
  if (params?.offset) searchParams.set("offset", String(params.offset));
  const query = searchParams.toString();
  return request(`/api/sessions/${id}/messages${query ? `?${query}` : ""}`);
}

export async function updateSessionMessage(
  sessionId: string,
  index: number,
  message: unknown
): Promise<{ success: boolean; index: number }> {
  return request(`/api/sessions/${sessionId}/messages/${index}`, {
    method: "PUT",
    body: JSON.stringify({ message }),
  });
}

export async function searchSessions(params: {
  pattern: string;
  limit?: number;
}): Promise<{
  results: import("../types").SessionSearchResult[];
  total: number;
  pattern: string;
}> {
  const searchParams = new URLSearchParams();
  searchParams.set("pattern", params.pattern);
  if (params.limit) searchParams.set("limit", String(params.limit));
  return request(`/api/sessions/search?${searchParams.toString()}`);
}

// SSE for real-time updates
export function subscribeToEvents(
  onMessage: (event: { type: string; data: unknown }) => void,
  onError?: (error: Event) => void
): () => void {
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
