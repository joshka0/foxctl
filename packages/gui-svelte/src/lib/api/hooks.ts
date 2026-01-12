import { createQuery } from "@tanstack/svelte-query";
import * as api from "@agentctl/data";
import type { AgentState } from "@agentctl/data";
import { queryKeys } from "./queryClient";

// Jobs
export function useJobs(params?: { state?: string; limit?: number }) {
  return createQuery({
    queryKey: queryKeys.jobs(params),
    queryFn: () => api.getJobs(params),
  });
}

export function useJobDetail(id: string) {
  return createQuery({
    queryKey: queryKeys.jobDetail(id),
    queryFn: () => api.getJobDetail(id),
    enabled: !!id,
  });
}

// Tasks
export function useTasks(params?: { limit?: number }) {
  return createQuery({
    queryKey: queryKeys.tasks(params),
    queryFn: () => api.getTasks(params),
  });
}

// Agents
export function useAgents(params?: { state?: AgentState; limit?: number }) {
  return createQuery({
    queryKey: queryKeys.agents(params),
    queryFn: () => api.getAgents(params),
  });
}

// Stats
export function useStats() {
  return createQuery({
    queryKey: queryKeys.stats(),
    queryFn: api.getStats,
  });
}

// Insights
export function useInsights() {
  return createQuery({
    queryKey: queryKeys.insights(),
    queryFn: api.getInsights,
  });
}

// Mailbox
export function useMailbox(params?: { actor?: string; limit?: number; workspace?: string }) {
  return createQuery({
    queryKey: queryKeys.mailbox(params),
    queryFn: () => api.getMailbox(params),
    enabled: !!params?.workspace,
  });
}

// Reservations
export function useReservations(params?: { workspace?: string }) {
  return createQuery({
    queryKey: queryKeys.reservations(params),
    queryFn: () => api.getReservations(params),
    enabled: !!params?.workspace,
  });
}

// Blackboard
export function useBlackboard(params?: { ns?: string; topic?: string; limit?: number }) {
  return createQuery({
    queryKey: queryKeys.blackboard(params),
    queryFn: () => api.getBlackboard(params),
  });
}

// SQLite
export function useSQLiteDatabases() {
  return createQuery({
    queryKey: queryKeys.sqlite.databases(),
    queryFn: api.getSQLiteDatabases,
  });
}

export function useSQLiteTables(db: string) {
  return createQuery({
    queryKey: queryKeys.sqlite.tables(db),
    queryFn: () => api.getSQLiteTables(db),
    enabled: !!db,
  });
}

export function useSQLiteData(db: string, table: string, limit = 100, offset = 0) {
  return createQuery({
    queryKey: queryKeys.sqlite.data(db, table, offset),
    queryFn: () => api.getSQLiteData(db, table, limit, offset),
    enabled: !!db && !!table,
  });
}

export function useSQLiteSchema(db: string, table: string) {
  return createQuery({
    queryKey: queryKeys.sqlite.schema(db, table),
    queryFn: () => api.getSQLiteSchema(db, table),
    enabled: !!db && !!table,
  });
}

export function useSQLiteIndexes(db: string, table?: string) {
  return createQuery({
    queryKey: queryKeys.sqlite.indexes(db, table),
    queryFn: () => api.getSQLiteIndexes(db, table),
    enabled: !!db,
  });
}

// Search
export function useSearch(params: {
  q: string;
  limit?: number;
  rerank?: boolean;
  scope?: string;
  workspace?: string;
}) {
  return createQuery({
    queryKey: queryKeys.search(params),
    queryFn: () => api.search(params),
    enabled: !!params.q && !!params.workspace,
  });
}

// Workspaces
export function useWorkspaces() {
  return createQuery({
    queryKey: queryKeys.workspaces(),
    queryFn: api.getWorkspaces,
  });
}

// Sessions
export function useSessions(params?: { limit?: number; offset?: number }) {
  return createQuery({
    queryKey: queryKeys.sessions(params),
    queryFn: () => api.getSessions(params),
  });
}

export function useSession(id: string) {
  return createQuery({
    queryKey: queryKeys.session(id),
    queryFn: () => api.getSession(id),
    enabled: !!id,
  });
}

export function useSessionMessages(id: string, params?: { limit?: number; offset?: number }) {
  return createQuery({
    queryKey: queryKeys.sessionMessages(id, params),
    queryFn: () => api.getSessionMessages(id, params),
    enabled: !!id,
  });
}

// Codemaps
export function useCodemaps(params?: { workspace?: string; limit?: number }) {
  return createQuery({
    queryKey: queryKeys.codemaps(params),
    queryFn: () => api.getCodemaps(params),
    enabled: !!params?.workspace,
  });
}

export function useCodemap(id: string, workspace?: string) {
  return createQuery({
    queryKey: queryKeys.codemap(id, workspace),
    queryFn: () => api.getCodemap(id, workspace),
    enabled: !!id && !!workspace,
  });
}

// Consoles
export function useConsoles(params?: { limit?: number }) {
  return createQuery({
    queryKey: queryKeys.consoles(params),
    queryFn: () => api.getConsoles(params),
  });
}

export function useConsole(id: string) {
  return createQuery({
    queryKey: queryKeys.console(id),
    queryFn: () => api.getConsole(id),
    enabled: !!id,
  });
}
