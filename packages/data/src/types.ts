// API Types - matching Go structs from foxctl backend

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

export interface JobProgressEvent {
  ts: string;
  message?: string;
  percent?: number;
  meta?: Record<string, unknown>;
}

export interface JobProgressResult {
  job_id: string;
  state: string;
  events: JobProgressEvent[];
  count: number;
}

export interface JobActionResult {
  job_id: string;
  state?: string;
  status?: string;
  job?: JobSummary;
}

export type V2StreamType = "run" | "agent" | "turn";

export interface V2RuntimeEvent {
  id: string;
  stream_id: string;
  stream_type: V2StreamType;
  stream_version: number;
  sequence: number;
  event_type: string;
  occurred_at: string;
  correlation_id?: string;
  causation_id?: string;
  actor_id?: string;
  request_id?: string;
  command?: string;
  payload?: unknown;
}

export type V2TranscriptRole = "user" | "assistant" | "tool" | "system";

export type V2TranscriptKind =
  | "prompt"
  | "message"
  | "tool_call"
  | "tool_result"
  | "turn"
  | "status"
  | "error";

export interface V2RunTranscriptItem {
  id: string;
  role: V2TranscriptRole;
  kind: V2TranscriptKind;
  title?: string;
  text?: string;
  event_id: string;
  event_type: string;
  occurred_at: string;
  metadata?: Record<string, unknown>;
}

export interface V2RunTranscript {
  run_id: string;
  count: number;
  items: V2RunTranscriptItem[];
}

export type V2EventStreamEventType =
  | "v2.connected"
  | "v2.event"
  | "v2.replay_complete"
  | "v2.error"
  | "heartbeat";

export interface V2EventStreamEvent {
  type: V2EventStreamEventType;
  data: unknown;
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

// Orchestration (v2 runtime board)
export type OrchestrationLaneID =
  | "Todo"
  | "Claimed"
  | "Running"
  | "RetryQueued"
  | "Blocked"
  | "Review"
  | "Done";

export interface OrchestrationCard {
  workspace_id?: string;
  issue_id: string;
  issue_identifier?: string;
  title?: string;
  state: string;
  lane?: OrchestrationLaneID;
  tracker_state?: string;
  policy_status?: string;
  last_outcome?: string;
  eligibility?: string;
  denial_reason?: string;
  suggestion?: string;
  run_id?: string;
  agent_id?: string;
  actor_id?: string;
  attempt?: number;
  retry_due_at?: string;
  last_event_type?: string;
  last_event_at?: string;
}

export interface OrchestrationLane {
  id: OrchestrationLaneID;
  title: string;
  cards: OrchestrationCard[];
}

export interface OrchestrationBoard {
  generated_at: string;
  counts: Partial<Record<OrchestrationLaneID, number>>;
  lanes: OrchestrationLane[];
  next_cursor?: string;
}

export interface OrchestrationBoardArtifactRef {
  summary: string;
  artifact: string;
  hint?: string;
  generated_at?: string;
  counts?: Partial<Record<OrchestrationLaneID, number>>;
}

export interface OrchestrationBoardResult {
  board: OrchestrationBoard | null;
  artifact: OrchestrationBoardArtifactRef | null;
}

export type OrchestrationCardAction = "retry-now" | "release" | "mark-done";

export interface OrchestrationCardActionResult {
  request_id: string;
  action: OrchestrationCardAction;
  card: OrchestrationCard;
  idempotent?: boolean;
  ts: string;
}

export interface OrchestrationDispatchResult {
  request_id: string;
  workspace_id?: string;
  issue_id: string;
  issue_identifier?: string;
  status: string;
  policy_status?: string;
  last_outcome?: string;
  denial_reason?: string;
  suggestion?: string;
  run_id?: string;
  turn_id?: string;
  agent_id?: string;
  actor_id?: string;
  idempotent?: boolean;
  ts: string;
}

export interface OrchestrationRuntimeTreeNode {
  tag?: string;
  agent_id?: string;
  pid?: string;
  metadata?: Record<string, unknown>;
  status?: string;
  state?: unknown;
  error?: string;
  children?: OrchestrationRuntimeTreeNode[];
}

export interface OrchestrationRuntimeTree {
  enabled: boolean;
  agent_id?: string;
  depth: number;
  root?: OrchestrationRuntimeTreeNode;
  error?: string;
}

export interface OrchestrationBoardCardRuntimeResult {
  card: OrchestrationCard;
  runtime?: OrchestrationRuntimeTree;
}

export interface OrchestrationRefreshResult {
  request_id: string;
  queued: boolean;
  coalesced?: boolean;
  idempotent?: boolean;
  ts: string;
}

export type DatabaseDriver = "sqlite" | "turso" | "postgres";

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
  last_used?: string;
  last_active?: string;
  is_active?: boolean;
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
  pinned?: boolean;
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
export type AgentMemoryScope = "agent" | "session";
export type AgentMemoryRetention =
  | "companion"
  | "durable"
  | "task"
  | "ephemeral";

export interface Agent {
  id: string;
  parent_id?: string;
  ns: string; // namespace (unique)
  name?: string;
  slug?: string;
  role?: string;
  prompt?: string;
  skills_allow?: string[] | string;
  policy?: Record<string, unknown> | string;
  share_bb: BlackboardShareMode;
  state: AgentState;
  llm_provider?: string;
  llm_model?: string;
  exec_mode?: string;
  think_interval?: number;
  conversation_id?: string;
  memory_scope?: AgentMemoryScope | string;
  memory_retention?: AgentMemoryRetention | string;
  max_iterations?: number;
  max_auto_turns?: number;
  created_at: string;
  heartbeat_at?: string;
}

export interface AgentMemoryStats {
  total_turns?: number;
  vivid_turns?: number;
  recent_summaries?: number;
  daily_summaries?: number;
  daily_distillations?: number;
  total_tokens_estimate?: number;
  compression_runs?: number;
  [key: string]: unknown;
}

export interface AgentMemoryStatsResult {
  stats: AgentMemoryStats;
  policy?: {
    memory_scope?: AgentMemoryScope | string;
    memory_retention?: AgentMemoryRetention | string;
    default_distill?: boolean;
  };
}

export interface AgentMemoryContextResult {
  conversation_id: string;
  context: string;
  policy?: {
    memory_scope?: AgentMemoryScope | string;
    memory_retention?: AgentMemoryRetention | string;
    search_limit?: number;
    default_distill?: boolean;
    context_token_hint?: number;
  };
}

export interface AgentMemorySearchResultItem {
  name: string;
  type: string;
  score: number;
  summary: string;
  session_id?: string;
  updated_at?: string;
}

export interface AgentMemorySearchResult {
  conversation_id: string;
  query: string;
  limit: number;
  results: AgentMemorySearchResultItem[];
  policy?: {
    memory_scope?: AgentMemoryScope | string;
    memory_retention?: AgentMemoryRetention | string;
    default_limit?: number;
    effective_limit?: number;
  };
}

export interface AgentMemoryCompressResult {
  conversation_id: string;
  processed_dates?: string[];
  summarized?: number;
  skipped?: number;
  distilled?: number;
  default_distill?: boolean;
  effective_distill?: boolean;
  memory_scope?: AgentMemoryScope | string;
  memory_retention?: AgentMemoryRetention | string;
}

export interface RoomMember {
  actor_id: string;
  role?: string;
  joined_at?: string;
}

export interface Room {
  id: string;
  workspace_id: string;
  stream: string;
  title: string;
  description?: string;
  dispatch_policy?:
    | "all_subtree"
    | "children_only"
    | "lead_only"
    | "selected"
    | string;
  dispatch_agent_ids?: string[];
  created_at?: string;
  updated_at?: string;
  latest_subject?: string;
  latest_preview?: string;
  latest_sender?: string;
  latest_message_at?: string;
  message_count: number;
  unread_count: number;
  participants?: string[];
  task_ids?: string[];
  members?: RoomMember[];
}

export interface RoomTask {
  id: string;
  workspace_id: string;
  epic_id?: string;
  milestone_id?: string;
  title: string;
  description?: string;
  scope_path?: string;
  parent_id?: string;
  children?: string[];
  depends_on?: string[];
  status: string;
  created_at: string;
  completed_at?: string;
  assigned_actor_id?: string;
  assigned_at?: string;
  owner_actor_id?: string;
  claimed_at?: string;
  heartbeat_at?: string;
  blocked_reason?: string;
  blocked_at?: string;
  notes?: string;
  gotchas?: string;
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
  // Daemon status info (returned on create)
  daemon_status?:
    | "already_running"
    | "daemon_spawned"
    | "created_and_spawned"
    | "unknown"
    | "error";
  agent_id?: string;
  daemon_error?: string;
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
