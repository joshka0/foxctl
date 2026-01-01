// Data fetching hooks for TUI
// Uses @agentctl/data client with simple React state management

import { useState, useEffect, useCallback } from "react";
import {
  getJobs,
  getJobDetail,
  getTasks,
  getStats,
  getInsights,
  getMailbox,
  getReservations,
  getBlackboard,
  getSQLiteDatabases,
  getSQLiteTables,
  getSQLiteData,
  search,
  type JobSummary,
  type JobDetail,
  type TaskSummary,
  type TaskStats,
  type JobStats,
  type InsightsData,
  type MailboxMessage,
  type Reservation,
  type BlackboardRecord,
  type SQLiteDatabase,
  type SQLiteTable,
  type SearchResult,
  type SearchStats,
} from "@agentctl/data";

export interface UseQueryResult<T> {
  data: T | undefined;
  isLoading: boolean;
  error: Error | undefined;
  refetch: () => void;
}

// Generic hook for data fetching
function useQuery<T>(
  fetcher: () => Promise<T>,
  deps: unknown[] = []
): UseQueryResult<T> {
  const [data, setData] = useState<T | undefined>(undefined);
  const [isLoading, setIsLoading] = useState(true);
  const [error, setError] = useState<Error | undefined>(undefined);

  const refetch = useCallback(() => {
    setIsLoading(true);
    setError(undefined);
    fetcher()
      .then(setData)
      .catch(setError)
      .finally(() => setIsLoading(false));
  }, deps);

  useEffect(() => {
    refetch();
  }, [refetch]);

  return { data, isLoading, error, refetch };
}

// Jobs
export function useJobs(params?: { state?: string; limit?: number }) {
  return useQuery(
    async () => {
      const result = await getJobs(params);
      return result.jobs;
    },
    [params?.state, params?.limit]
  );
}

export function useJobDetail(id: string | undefined) {
  return useQuery(
    async () => {
      if (!id) return undefined;
      return getJobDetail(id);
    },
    [id]
  );
}

// Tasks
export function useTasks(params?: { limit?: number }) {
  return useQuery(
    async () => {
      const result = await getTasks(params);
      return { tasks: result.tasks, stats: result.stats };
    },
    [params?.limit]
  );
}

// SQLite
export function useSQLiteDatabases() {
  return useQuery(async () => {
    const result = await getSQLiteDatabases();
    return result.databases;
  }, []);
}

export function useSQLiteTables(db: string | undefined) {
  return useQuery(
    async () => {
      if (!db) return undefined;
      const result = await getSQLiteTables(db);
      return result.tables;
    },
    [db]
  );
}

export function useSQLiteData(
  db: string | undefined,
  table: string | undefined,
  limit = 100
) {
  return useQuery(
    async () => {
      if (!db || !table) return undefined;
      return getSQLiteData(db, table, limit);
    },
    [db, table, limit]
  );
}

// Search
export function useSearch(params: {
  q: string;
  limit?: number;
  rerank?: boolean;
  scope?: string;
}) {
  return useQuery(
    async () => {
      if (!params.q) return { results: [], stats: undefined };
      const result = await search(params);
      return { results: result.results, stats: result.stats };
    },
    [params.q, params.limit, params.rerank, params.scope]
  );
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
export function useMailbox(params?: { actor?: string; limit?: number }) {
  return useQuery(
    async () => {
      const result = await getMailbox(params);
      return result.messages;
    },
    [params?.actor, params?.limit]
  );
}

// Reservations
export function useReservations() {
  return useQuery(async () => {
    const result = await getReservations();
    return result.reservations;
  }, []);
}

// Blackboard
export function useBlackboard(params?: {
  ns?: string;
  topic?: string;
  limit?: number;
}) {
  return useQuery(
    async () => {
      const result = await getBlackboard(params);
      return result.records;
    },
    [params?.ns, params?.topic, params?.limit]
  );
}
