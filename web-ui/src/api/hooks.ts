import { useQuery, useQueryClient } from "@tanstack/react-query";
import { useEffect } from "react";
import * as api from "./client";

// Query keys
export const queryKeys = {
  jobs: (params?: { state?: string; limit?: number }) => ["jobs", params] as const,
  jobDetail: (id: string) => ["jobs", id] as const,
  tasks: (params?: { limit?: number }) => ["tasks", params] as const,
  stats: () => ["stats"] as const,
  insights: () => ["insights"] as const,
  mailbox: (params?: { actor?: string; limit?: number }) => ["mailbox", params] as const,
  reservations: () => ["reservations"] as const,
  blackboard: (params?: { ns?: string; topic?: string; limit?: number }) => ["blackboard", params] as const,
  sqlite: {
    databases: () => ["sqlite", "databases"] as const,
    tables: (db: string) => ["sqlite", "tables", db] as const,
    data: (db: string, table: string, offset: number) => ["sqlite", "data", db, table, offset] as const,
    schema: (db: string, table: string) => ["sqlite", "schema", db, table] as const,
    indexes: (db: string, table?: string) => ["sqlite", "indexes", db, table] as const,
  },
  search: (params: { q: string; limit?: number; rerank?: boolean; scope?: string }) => ["search", params] as const,
  workspaces: () => ["workspaces"] as const,
  sessions: (params?: { limit?: number; offset?: number }) => ["sessions", params] as const,
  session: (id: string) => ["sessions", id] as const,
  sessionMessages: (id: string, params?: { limit?: number; offset?: number }) => ["sessions", id, "messages", params] as const,
  sessionSearch: (params: { pattern: string; limit?: number }) => ["sessions", "search", params] as const,
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
export function useTasks(params?: { limit?: number }) {
  return useQuery({
    queryKey: queryKeys.tasks(params),
    queryFn: () => api.getTasks(params),
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
export function useInsights() {
  return useQuery({
    queryKey: queryKeys.insights(),
    queryFn: api.getInsights,
  });
}

// Mailbox
export function useMailbox(params?: { actor?: string; limit?: number }) {
  return useQuery({
    queryKey: queryKeys.mailbox(params),
    queryFn: () => api.getMailbox(params),
  });
}

// Reservations
export function useReservations() {
  return useQuery({
    queryKey: queryKeys.reservations(),
    queryFn: api.getReservations,
  });
}

// Blackboard
export function useBlackboard(params?: { ns?: string; topic?: string; limit?: number }) {
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
export function useSearch(params: { q: string; limit?: number; rerank?: boolean; scope?: string }) {
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
export function useSessions(params?: { limit?: number; offset?: number }) {
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
