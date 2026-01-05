// API Types - matching Go structs from agentctl backend

export interface JobSummary {
  id: string;
  command: string;
  type: string; // e.g. "skill", "job", "cron"
  category: string; // e.g. "code", "fs", "text"
  skill: string; // e.g. "symbols", "ls", "grep"
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
  recipient: string;
  subject: string;
  body: string;
  kind: string;
  priority: number;
  status: string;
  ack_required?: boolean;
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
  driver?: DatabaseDriver;
  turso_url?: string;
  has_replica?: boolean;
  replica_path?: string;
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

// Memory types (named_memory from memory.db)
export interface MemoryEntry {
  id: string;
  name: string;
  type: string; // e.g., "codemap", "session_snapshot", "plan_sync_state", "file_embedding"
  workspace: string;
  summary?: string;
  created_at: string;
  updated_at: string;
  last_accessed: string;
  access_count: number;
  session_id?: string;
  pinned_at?: string | null;
}

export interface MemoryEntryDetail extends MemoryEntry {
  data?: unknown;
  digests?: string[];
}

export interface MemoryTypeCount {
  type: string;
  count: number;
}

// CAS (Content Addressable Storage) types
export interface CASObject {
  digest: string;
  size_bytes?: number;
  kind?: string;
  created_at?: string;
  tags?: string[];
}

export interface CASReadResult {
  digest: string;
  content: string;
  page: number;
  total_pages: number;
  page_size: number;
  total_bytes: number;
  content_type: string;
  next_page?: number;
  prev_page?: number;
}

// API Response types
export interface APIResponse<T> {
  data: T;
  error?: string;
}

// Agent types (from agents.db)
export type AgentState = "starting" | "running" | "stopped" | "error";
export type BlackboardShareMode = "all" | "scoped" | "none";

export interface Agent {
  id: string;
  parent_id?: string;
  ns: string; // namespace (unique)
  role?: string;
  prompt?: string;
  skills_allow: string; // comma-separated skill patterns
  policy: string;
  share_bb: BlackboardShareMode;
  state: AgentState;
  llm_provider?: string;
  llm_model?: string;
  created_at: string;
  heartbeat_at?: string;
}

// Trajectory types (from trajectory.db)
export type TrajectoryStatus = "ok" | "partial" | "error" | "running";
export type TrajectoryEventKind =
  | "hook_call"
  | "hook_result"
  | "user_request"
  | "tool_result"
  | "task_transition";

export interface Trajectory {
  id: string;
  workspace_id: string;
  root_request_id?: string;
  task_ids_json?: string;
  epic_id?: string;
  agent_role?: string;
  job_id?: string;
  trace_id?: string;
  status: TrajectoryStatus;
  summary?: string;
  artifact_digest?: string;
  created_at: string;
  updated_at: string;
}

export interface TrajectoryEvent {
  id: string;
  trajectory_id: string;
  workspace_id: string;
  ts: string;
  kind: TrajectoryEventKind;
  actor?: string;
  command?: string;
  status?: string;
  data_inline_json?: string;
  data_artifact?: string;
  meta_json?: string;
}

export interface UserRequest {
  id: string;
  workspace_id: string;
  actor: string;
  source: string;
  ts: string;
  text: string;
  command_context_json?: string;
  task_hints_json?: string;
}

// SSE (Server-Sent Events) types
export type SSEEventType =
  | "connected"
  | "heartbeat"
  | "job"
  | "task"
  | "mailbox"
  | "blackboard"
  | "agent"
  | "trajectory"
  | "console.event"
  | "console.reply";

export interface SSEEventData {
  id?: string;
  state?: string;
  status?: string;
  title?: string;
  actor?: string;
  from?: string;
  subject?: string;
  message_id?: string;
  ns?: string;
  topic?: string;
  updated?: boolean;
}

export interface SSEEvent {
  type: SSEEventType;
  data?: SSEEventData;
  ts: number;
}

// Console event types (for interactive agent sessions)
export interface ConsoleEventData {
  ask_id: string;
  kind: "thought" | "tool_call" | "tool_result" | "progress";
  content: string;
  seq: number;
  iteration?: number;
  tool_name?: string;
}

export interface ConsoleReplyData {
  ask_id: string;
  response: string;
  status: "ok" | "error" | "cancelled";
  metrics?: {
    duration_ms?: number;
  };
}

// Console Session types
export interface ConsoleSession {
  id: string;
  actor_id: string;
  session_id?: string;
  workspace: string;
  created_at: string;
  last_attached_at: string;
  meta?: Record<string, unknown>;
}

export interface ConsoleSessionCreate {
  actor_id: string;
  session_id?: string;
  meta?: Record<string, unknown>;
}

export interface ConsoleSendRequest {
  prompt: string;
  context?: Record<string, unknown>;
}

export interface ConsoleSendResponse {
  message_id: string;
  ask_id: string;
  status: "sent";
}

export interface ConsoleCancelRequest {
  ask_id?: string;
}

export interface ConsoleCancelResponse {
  cmd_id: string;
  status: "sent";
}

export interface ConsoleFeedbackRequest {
  trajectory_id?: string;
  ask_id?: string;
  rating: number; // 1-5
  comment?: string;
}

export interface ConsoleFeedbackResponse {
  success: boolean;
  trajectory_id?: string;
  feedback_id?: string;
  rating: number;
}

// Context Window types (from session_context_windows table)
export interface ContextWindow {
  id: string;
  session_id: string;
  window_index: number; // 0, 1, 2... per session
  started_at: string; // First message timestamp in window
  ended_at: string; // compact_boundary timestamp (or session end)
  pre_compact_tokens: number; // From compactMetadata.preTokens
  trigger: string; // 'auto' or 'manual'
  chunk_start: number; // First chunk_index in window
  chunk_end: number; // Last chunk_index in window
  message_count: number; // Messages in this window
  summary?: string; // Per-window summary (optional)
  created_at: string;
}
