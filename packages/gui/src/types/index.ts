// API Types - matching Go structs from cmd/agentctl_web/templates/types.go

export interface JobSummary {
  id: string;
  command: string;
  type: string;      // e.g. "skill", "job", "cron"
  category: string;  // e.g. "code", "fs", "text"
  skill: string;     // e.g. "symbols", "ls", "grep"
  state: string;
  created_at: string;
  error?: string;
}

export interface JobDetail extends JobSummary {
  result_data?: unknown;
  stderr?: string;
  artifacts?: string[];
}

export interface TaskSummary {
  id: string;
  title: string;
  description?: string;
  status?: string;
  priority?: number;
  score: number;
  created_at: string;
  completed_at?: string;
  notes?: string;
  depends_on?: string[];
  pagerank?: number;
  in_degree?: number;
  out_degree?: number;
  critical_path_score?: number;
}

export interface TaskStats {
  total: number;
  pending: number;
  in_progress: number;
  completed: number;
}

export interface AgentSummary {
  id: string;
  parent_id?: string | null;
  ns: string;
  role?: string | null;
  skills_allow?: string | null;
  policy?: string | null;
  share_bb?: string | null;
  state: string;
  llm_provider?: string | null;
  llm_model?: string | null;
  created_at: string;
  heartbeat_at?: string | null;
}

export interface AgentDetail extends AgentSummary {
  prompt?: string | null;
  llm_api_key?: string | null;
}

export interface AgentDaemonStartResult {
  actor_id: string;
  agentId?: string;
  status?: string;
  error?: string;
}

export interface JobStats {
  total: number;
  by_state: Record<string, number>;
  by_command: Record<string, number>;
  recent: RecentStats;
}

export interface RecentStats {
  last_hour: number;
  last_day: number;
}

export interface InsightsData {
  nodes: GraphNode[];
  cycles: string[][];
  topological_order: string[];
}

export interface GraphNode {
  task_id: string;
  title?: string;
  pagerank: number;
  critical_path_score: number;
  in_degree: number;
  out_degree: number;
}

export interface MailboxMessage {
  id: string;
  sender: string;
  subject: string;
  body: string;
  kind: string;
  priority: number;
  status: string;
  created_at: string;
}

export interface Reservation {
  id: string;
  path: string;
  holder: string;
  mode: string;
  expires_at: string;
}

export interface BlackboardRecord {
  id: string;
  ns: string;
  topic: string;
  ts: number;
  ttl_sec: number;
  payload: string;
}

export type DatabaseDriver = "sqlite" | "turso" | "libsql";

export interface SQLiteDatabase {
  name: string;
  friendly_name: string;
  path: string;
  size: number;
  driver?: DatabaseDriver;      // sqlite, turso, or libsql
  turso_url?: string;           // Turso cloud URL if configured
  has_replica?: boolean;        // True if local replica exists
  replica_path?: string;        // Path to local replica
}

export interface SQLiteTable {
  name: string;
  row_count: number;
}

export interface SQLiteColumn {
  name: string;
  type: string;
  not_null: boolean;
  default_value?: string;
  is_pk: boolean;
}

export interface SQLiteIndex {
  name: string;
  table: string;
  unique: boolean;
  columns: string[];
  sql?: string;
}

export interface SQLiteQueryResult {
  columns: string[];
  rows: Record<string, unknown>[];
  rows_affected: number;
  error?: string;
}

export interface SearchResult {
  source: string;
  id: string;
  name?: string;
  path: string;
  line?: number;
  snippet?: string;
  summary?: string;
  similarity: number;
  rerank_score?: number;
  final_score: number;
  rank: number;
  source_rank: number;
}

export interface SearchStats {
  total_results: number;
  source_counts: Record<string, number>;
  reranked: boolean;
  embedding_dimensions: number;
  latency_ms: number;
}

export interface Workspace {
  path: string;
  name: string;
  session_count: number;
  last_used: string;
}

// Sessions
export interface Session {
  id: string;
  workspace_path: string;
  project_name?: string;
  git_branch?: string;
  started_at: string;
  ended_at?: string;
  summary?: string;
  accomplished?: string;
  decisions?: string;
  gotchas?: string;
  message_count: number;
  user_turns: number;
  tool_invocations: number;
  raw_jsonl_path?: string;
  status: string;
  agent_id: string;
}

export interface SessionMessage {
  index: number;
  type: string;
  userType?: string;
  uuid?: string;
  parentUuid?: string;
  timestamp?: string;
  message?: {
    role?: string;
    content?: Array<{
      type: string;
      text?: string;
      name?: string;
      input?: unknown;
    }>;
  };
  summary?: string;
  error?: string;
  raw?: string;
}

export interface SessionSearchResult {
  session_id: string;
  session_summary?: string;
  session_started_at?: string;
  message_index: number;
  type?: string;
  preview: string;
  match?: string;
}

// Codemaps
export interface Codemap {
  id: string;
  title: string;
  description: string;
  query: string;
  workspace: string;
  file_count: number;
  symbol_count: number;
  traces: CodemapTrace[];
  created_at: string;
}

export interface CodemapTrace {
  number: number;
  title: string;
  summary: string;
  tree: string;
  annotations: CodemapAnnotation[];
}

export interface CodemapAnnotation {
  label: string;
  title: string;
  description: string;
  path: string;
}

export interface CodemapListItem {
  id: string;
  title: string;
  query: string;
  file_count: number;
  symbol_count: number;
  created_at: string;
}

// API Response types
export interface APIResponse<T> {
  data: T;
  error?: string;
}
