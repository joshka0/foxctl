import { QueryClient } from "@tanstack/svelte-query";

export const queryClient = new QueryClient({
  defaultOptions: {
    queries: {
      staleTime: 1000 * 30, // 30 seconds
      refetchOnWindowFocus: true,
      retry: 1,
    },
  },
});

// Query keys for consistent cache management
export const queryKeys = {
  jobs: (params?: { state?: string; limit?: number }) => ["jobs", params] as const,
  jobDetail: (id: string) => ["jobs", id] as const,
  tasks: (params?: { limit?: number }) => ["tasks", params] as const,
  taskDetail: (id: string) => ["tasks", id] as const,
  agents: (params?: { state?: string; limit?: number }) => ["agents", params] as const,
  stats: () => ["stats"] as const,
  insights: () => ["insights"] as const,
  mailbox: (params?: { actor?: string; limit?: number; workspace?: string }) => ["mailbox", params] as const,
  reservations: (params?: { workspace?: string }) => ["reservations", params] as const,
  blackboard: (params?: { ns?: string; topic?: string; limit?: number }) => ["blackboard", params] as const,
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
  sessions: (params?: { limit?: number; offset?: number }) => ["sessions", params] as const,
  session: (id: string) => ["sessions", id] as const,
  sessionMessages: (id: string, params?: { limit?: number; offset?: number }) => ["sessions", id, "messages", params] as const,
  codemaps: (params?: { workspace?: string; limit?: number }) => ["codemaps", params] as const,
  codemap: (id: string, workspace?: string) => ["codemaps", id, workspace] as const,
  consoles: (params?: { limit?: number }) => ["consoles", params] as const,
  console: (id: string) => ["consoles", id] as const,
};
