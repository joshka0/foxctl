const API_BASE = "";

async function request<T>(path: string, init?: RequestInit): Promise<T> {
  const response = await fetch(`${API_BASE}${path}`, {
    headers: {
      "Content-Type": "application/json",
      ...(init?.headers || {}),
    },
    credentials: "include",
    ...init,
  });

  if (!response.ok) {
    const text = await response.text();
    throw new Error(text || `HTTP ${response.status}`);
  }

  return response.json() as Promise<T>;
}

export interface ConsoleSessionInfo {
  id: string;
  workspace: string;
  profile: string;
  created: string;
  message_count: number;
  client_count: number;
}

export interface ConsolePayload {
  type: "ask" | "cmd" | "event" | "reply";
  actor_id?: string;
  console_id?: string;
  correlation_id?: string;
  content?: string;
  metadata?: Record<string, unknown>;
  cmd?: {
    name: string;
    correlation_id?: string;
  };
}

export async function listConsoleSessions(params?: {
  workspace?: string;
}): Promise<{ sessions: ConsoleSessionInfo[]; count: number }> {
  const searchParams = new URLSearchParams();
  if (params?.workspace) searchParams.set("workspace", params.workspace);
  const query = searchParams.toString();
  return request(`/api/console/sessions${query ? `?${query}` : ""}`);
}

export async function createConsoleSession(data: {
  workspace?: string;
  profile?: string;
  system_prompt?: string;
}): Promise<{ session: ConsoleSessionInfo }> {
  return request("/api/console/sessions", {
    method: "POST",
    body: JSON.stringify(data),
  });
}

export async function deleteConsoleSession(id: string): Promise<{ ok: boolean }> {
  return request(`/api/console/sessions/${encodeURIComponent(id)}`, {
    method: "DELETE",
  });
}

export async function askConsoleSession(
  id: string,
  content: string,
  correlationId?: string
): Promise<{ correlation_id: string }> {
  return request(`/api/console/sessions/${encodeURIComponent(id)}/ask`, {
    method: "POST",
    body: JSON.stringify({
      content,
      correlation_id: correlationId,
    }),
  });
}

export async function cancelConsoleSession(
  id: string,
  correlationId?: string
): Promise<{ ok: boolean }> {
  return request(`/api/console/sessions/${encodeURIComponent(id)}/cancel`, {
    method: "POST",
    body: JSON.stringify({
      correlation_id: correlationId,
    }),
  });
}

export function subscribeToConsoleSessionEvents(
  id: string,
  onPayload: (payload: ConsolePayload) => void,
  onError?: (error: Event) => void
): () => void {
  if (typeof EventSource === "undefined") {
    console.warn("EventSource not available in this environment");
    return () => {};
  }

  const eventSource = new EventSource(
    `${API_BASE}/api/console/sessions/${encodeURIComponent(id)}/events?format=payload`
  );

  const handler = (event: MessageEvent) => {
    try {
      const payload = JSON.parse(event.data) as ConsolePayload;
      onPayload(payload);
    } catch {
      // Ignore parse errors
    }
  };

  ["connected", "heartbeat", "ask", "cmd", "event", "reply"].forEach((type) => {
    eventSource.addEventListener(type, handler);
  });

  eventSource.onerror = (error) => {
    onError?.(error);
  };

  return () => eventSource.close();
}
