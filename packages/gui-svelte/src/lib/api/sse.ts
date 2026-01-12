import { queryClient } from "./queryClient";

const API_BASE = "";

/**
 * Start SSE subscription for real-time updates.
 * Invalidates relevant query caches when events are received.
 *
 * @returns Cleanup function to close the connection
 */
export function startSSE(): () => void {
  const eventSource = new EventSource(`${API_BASE}/api/events`);

  eventSource.onmessage = (event) => {
    try {
      const data = JSON.parse(event.data);
      handleSSEEvent(data);
    } catch {
      // Ignore parse errors
    }
  };

  eventSource.onerror = (error) => {
    console.error("SSE error:", error);
  };

  return () => eventSource.close();
}

function handleSSEEvent(event: { type: string; data?: unknown }) {
  switch (event.type) {
    case "invalidate": {
      const keys = (event.data as { keys?: string[] } | undefined)?.keys;
      if (keys && keys.length > 0) {
        keys.forEach((key) => {
          queryClient.invalidateQueries({ queryKey: [key] });
        });
      } else {
        queryClient.invalidateQueries();
      }
      break;
    }
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
    case "agent":
      queryClient.invalidateQueries({ queryKey: ["agents"] });
      break;
    case "session":
      queryClient.invalidateQueries({ queryKey: ["sessions"] });
      break;
    case "codemap":
      queryClient.invalidateQueries({ queryKey: ["codemaps"] });
      break;
    case "heartbeat":
    case "connected":
      // Ignore heartbeat/connected events
      break;
    default:
      // Unknown event type - could invalidate everything or ignore
      console.debug("Unknown SSE event type:", event.type);
  }
}
