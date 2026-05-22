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

export interface ApiEnvelope<T> {
  version: number;
  status: "ok" | "error" | "progress";
  command: string;
  data: T;
  meta: { ts: string; [key: string]: unknown };
  error: { code?: string; message?: string };
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
  reply_expected?: boolean;
  interrupt?: boolean;
  related_message_id?: string;
  created_at: string;
  task_id?: string;
  stream?: string;
}

export interface RoomMessage extends MailboxMessage {}

export interface MailboxSendRequest {
  workspace_id: string;
  related_message_id?: string;
  sender: string;
  recipient: string;
  subject: string;
  body: string;
  kind?: string;
  priority?: number;
  ack_required?: boolean;
  task_id?: string;
  stream?: string;
}

export interface MailboxSendResponse {
  id: string;
  status: string;
  message?: string;
}

export interface MailboxStatusUpdateRequest {
  workspace_id: string;
  actor_id?: string;
  action: "read" | "surfaced" | "ack";
  message_ids: string[];
}

export interface MailboxStatusUpdateResponse {
  action: string;
  updated: number;
  status: string;
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
  cas_ref?: string;
  lease_by?: string;
  lease_exp?: number;
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
  archived_at?: string;
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

export interface OrchestrationBoardCardResult {
  card: OrchestrationCard;
  runtime?: OrchestrationCardRuntime;
}

export type OrchestrationCardAction = "retry-now" | "release" | "mark-done";

export interface OrchestrationCardActionResult {
  request_id: string;
  action: OrchestrationCardAction;
  card: OrchestrationCard;
  idempotent?: boolean;
  ts: string;
}

export interface OrchestrationSeedCardInput {
  issue_id?: string;
  issue_identifier?: string;
  title: string;
  state?: string;
  tracker_state?: string;
  policy_status?: string;
  last_outcome?: string;
  eligibility?: string;
}

export interface OrchestrationSeedCardsRequest {
  request_id: string;
  workspace_id?: string;
  cards: OrchestrationSeedCardInput[];
}

export interface OrchestrationSeedCardsResult {
  request_id: string;
  created: number;
  skipped?: number;
  ts: string;
}

export interface OrchestrationCleanupCardsRequest {
  request_id: string;
  workspace_id: string;
  issue_ids?: string[];
}

export interface OrchestrationCleanupCardsResult {
  request_id: string;
  deleted_cards: number;
  deleted_events: number;
  ts: string;
}

export interface OrchestrationArchiveCardsRequest {
  request_id: string;
  workspace_id: string;
  issue_ids?: string[];
}

export interface OrchestrationArchiveCardsResult {
  request_id: string;
  updated: number;
  action: "archived" | "restored" | string;
  ts: string;
}

export type OrchestrationRestoreCardsRequest = OrchestrationArchiveCardsRequest;
export type OrchestrationRestoreCardsResult = OrchestrationArchiveCardsResult;

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

export interface OrchestrationCardRuntime {
  enabled: boolean;
  agent_id?: string;
  status?: string;
  state?: unknown;
  children?: Record<string, unknown>;
  error?: string;
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
export type AgentState =
  | "starting"
  | "running"
  | "idle"
  | "stopped"
  | "error"
  | "unknown";
export type BlackboardShareMode = "all" | "scoped" | "none";
export type AgentMemoryScope = "agent" | "session";
export type AgentMemoryRetention =
  | "companion"
  | "durable"
  | "task"
  | "ephemeral";
export type AgentWorkspaceSource = "local" | "sandbox" | string;
export type AgentExecutionMode =
  | "reactive"
  | "autonomous"
  | "proactive"
  | "tick"
  | "story";

export interface Agent {
  id: string;
  parent_id?: string;
  ns: string; // namespace (unique)
  name?: string;
  slug?: string;
  role?: string;
  prompt?: string;
  prompt_summary?: string;
  skills_allow: string[];
  policy?: Record<string, unknown> | string;
  share_bb: BlackboardShareMode | string;
  state: AgentState;
  updated_at?: string;
  llm_provider?: string;
  llm_model?: string;
  llm_base_url?: string;
  llm_auth_mode?: string;
  llm_auth_header?: string;
  llm_auth_prefix?: string;
  exec_mode?: AgentExecutionMode | string;
  think_interval?: number;
  conversation_id?: string;
  memory_scope?: AgentMemoryScope | string;
  memory_retention?: AgentMemoryRetention | string;
  max_iterations?: number;
  max_auto_turns?: number;
  workspace_root?: string;
  workspace_source?: AgentWorkspaceSource;
  sandbox_provider?: string;
  sandbox_id?: string;
  repo_url?: string;
  repo_ref?: string;
  created_at: string;
  heartbeat_at?: string;
}

export interface AgentsListResponse {
  agents: Agent[];
  total: number;
}

export interface AgentSpawnRequest {
  role: string;
  prompt: string;
  workspace_id?: string;
  workspace_root?: string;
  workspace_source?: "local" | "sandbox";
  sandbox_provider?: string;
  sandbox_id?: string;
  repo_url?: string;
  repo_ref?: string;
  sandbox_image?: string;
  sandbox_timeout_s?: number;
  allow_egress?: string[];
  skills_allow?: string[];
  parent_id?: string;
  memory_scope?: AgentMemoryScope;
  memory_retention?: AgentMemoryRetention;
  room_id?: string;
  room_role?: string;
  name?: string;
  slug?: string;
  exec_mode?: AgentExecutionMode;
  think_interval?: number;
  max_iterations?: number;
  max_context_tokens?: number;
  max_auto_turns?: number;
  llm_provider?: string;
  llm_model?: string;
}

export interface AgentSpawnResponse {
  session_id: string;
  actor_id: string;
  status: string;
  name?: string;
  workspace_id?: string;
  workspace_root?: string;
  workspace_source?: AgentWorkspaceSource;
  sandbox_provider?: string;
  sandbox_id?: string;
  repo_url?: string;
  repo_ref?: string;
}

export interface AgentPatchRequest {
  conversation_id?: string;
  memory_scope?: AgentMemoryScope;
  memory_retention?: AgentMemoryRetention;
}

export interface AgentAskRequest {
  message: string;
  response_schema?: Record<string, unknown>;
  response_keys?: string[];
}

export interface AgentAskResponse {
  reply: string;
  conversation_id: string;
}

export interface AgentSession {
  session_id: string;
  actor_id: string;
  role: string;
  status: string;
  iterations: number;
  started_at: string;
}

export interface AgentRuntimeTreeNode {
  tag?: string;
  agent_id?: string;
  pid?: string;
  metadata?: Record<string, unknown>;
  status?: string;
  state?: unknown;
  error?: string;
  children?: AgentRuntimeTreeNode[];
}

export interface AgentRuntimeTree {
  enabled: boolean;
  agent_id?: string;
  depth: number;
  root?: AgentRuntimeTreeNode;
  error?: string;
}

export interface AgentAskStreamRequest {
  message: string;
  correlation_id?: string;
  conversation_id?: string;
  context?: Record<string, unknown>;
  response_schema?: Record<string, unknown>;
  response_keys?: string[];
}

export interface AgentAskStreamResponse {
  accepted: boolean;
  agent_id: string;
  correlation_id: string;
  conversation_id: string;
}

export interface AgentAskStreamCancelRequest {
  correlation_id?: string;
}

export interface AgentAskStreamCancelResponse {
  ok: boolean;
  agent_id: string;
  correlation_id?: string;
  cancelled: number;
}

export interface AgentChatStreamEvent {
  agent_id: string;
  conversation_id: string;
  correlation_id: string;
  phase:
    | "started"
    | "delta"
    | "tool_call"
    | "tool_result"
    | "completed"
    | "cancelled"
    | "error"
    | string;
  content?: string;
  content_delta?: string;
  tool_name?: string;
  tool_call_id?: string;
  tool_arguments?: unknown;
  tool_output?: string;
  context_queries?: number;
  error?: string;
  metadata?: Record<string, unknown>;
}

export interface CoChangeHit {
  name: string;
  anchor_path: string;
  summary: string;
  score: number;
  neighbors?: string[];
  updated_at?: string;
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
  last_active_at?: string;
  status?: "online" | "idle" | "stale" | string;
  session_id?: string;
  unbound?: boolean;
  delivery_binding?: RoomDeliveryBinding;
}

export interface RoomDeliveryBinding {
  mux_backend?: string;
  mux_session?: string;
  mux_pane_id?: string;
  transport_endpoint?: string;
  transport_kind?: string;
  submit_mode?: string;
  health?: string;
  fallback_policy?: string;
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
  archived_at?: string;
}

export type ParticipantMembership = "active" | "unbound" | "none";
export type TransportAvailability =
  | "available"
  | "unknown"
  | "unavailable"
  | "none";
export type RuntimeAvailability = "live" | "unknown" | "stopped" | "none";
export type PresentationAttachment = "attached" | "detached" | "none";

export interface ParticipantState {
  actor_id: string;
  membership: ParticipantMembership;
  transport_endpoint?: string;
  transport: TransportAvailability;
  runtime: RuntimeAvailability;
  presentation: PresentationAttachment;
  mux_backend?: string;
  reason?: string;
  can_trigger_turn: boolean;
}

export interface RoomMessageEvent {
  workspace_id: string;
  room_id: string;
  stream: string;
  message_id?: string;
  correlation_id?: string;
  sender?: string;
  recipient?: string;
  subject?: string;
  phase?:
    | "sent"
    | "agent_started"
    | "agent_delta"
    | "agent_tool_call"
    | "agent_tool_result"
    | "agent_completed"
    | "agent_error"
    | string;
  agent_id?: string;
  content?: string;
  content_delta?: string;
  tool_name?: string;
  tool_call_id?: string;
  tool_output?: string;
  is_error?: boolean;
  dispatched?: number;
  skipped?: number;
  error?: string;
}

export interface RoomLiveRelayResult {
  backend: string;
  delivered_count?: number;
  failed_count?: number;
  delivered_to?: string[];
  failed_members?: string[];
  skipped_members?: string[];
  error?: string;
}

export interface RoomSendMessageResult {
  id: string;
  room_id: string;
  stream: string;
  status: string;
  message?: string;
  dispatched?: number;
  skipped?: number;
  delivery_owner?: string;
  delivery_pending?: boolean;
  live_relay?: RoomLiveRelayResult[];
}

export interface RoomReminder {
  id: string;
  workspace_id: string;
  room_id: string;
  root_message_id: string;
  task_id?: string;
  story_id?: string;
  milestone_id?: string;
  sender: string;
  recipient: string;
  subject: string;
  body: string;
  ack_required: boolean;
  reply_expected: boolean;
  interrupt: boolean;
  passive?: boolean;
  interval: string;
  max_iterations: number;
  sent_count: number;
  active: boolean;
  last_sent_at?: string;
  created_at?: string;
  updated_at?: string;
}

export interface RoomStatusEntry {
  id: string;
  sender: string;
  recipient: string;
  subject: string;
  priority: number;
  status: string;
  created_at: string;
  category: string;
  flags?: string[];
  preview?: string;
}

export interface RoomStatusParticipant {
  actor_id: string;
  role?: string;
  unbound?: boolean;
  transport_status?: string;
  runtime_binding_status?: string;
  last_active_at?: string;
  status: "active" | "idle" | "stale" | string;
  assigned_task_count: number;
  owned_task_count: number;
  actionable_inbox_count: number;
  latest_actionable?: RoomStatusEntry;
  transport: ParticipantState;
}

export interface RoomTaskPulseSummary {
  pending: number;
  assigned_unclaimed?: number;
  in_progress: number;
  blocked: number;
  stale: number;
  completed: number;
}

export interface RoomStatusTaskSignal {
  id: string;
  title: string;
  status: string;
  assigned_actor_id?: string;
  owner_actor_id?: string;
  blocked_reason?: string;
  heartbeat_at?: string;
  signals?: string[];
}

export interface RoomStatusBacklog {
  pending_acks: number;
  pending_replies: number;
  stale_tasks: number;
  blocked_tasks: number;
  assigned_unclaimed?: number;
  participants_with_pending?: number;
  latest_by_participant?: RoomStatusEntry[];
  filter?: string[];
  top_entries?: RoomStatusEntry[];
  top_tasks?: RoomStatusTaskSignal[];
}

export interface RoomStatus {
  room: Room;
  coordinator_actor_id?: string;
  participants: RoomStatusParticipant[];
  task_pulse: RoomTaskPulseSummary;
  actionable_backlog: RoomStatusBacklog;
}

export interface LeadChangeEvent {
  room_id: string;
  previous_lead: string;
  new_lead: string;
  note?: string;
  changed_at: string;
  changed_by: string;
}

export interface BulkResolveRequest {
  workspace_id: string;
  actor?: string;
  filter?: {
    kind?: string;
    sender?: string;
    subject_contains?: string;
  };
}

export interface RoomInbox {
  room_id: string;
  entries: MailboxMessage[];
  count: number;
}

export type RoomTaskStatus =
  | "pending"
  | "in_progress"
  | "blocked"
  | "completed"
  | "abandoned";

export interface RoomTask {
  id: string;
  workspace_id: string;
  room_id: string;
  epic_id?: string;
  milestone_id?: string;
  title: string;
  description?: string;
  scope_path?: string;
  parent_id?: string;
  children?: string[];
  depends_on?: string[];
  status: RoomTaskStatus;
  priority: number;
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
  nudge_count: number;
  last_nudged_at?: string;
  stale?: boolean;
  stale_duration_ms?: number;
  reclaim_audit?: {
    previous_owner: string;
    reclaimed_by: string;
    reclaimed_at: string;
    reclaim_reason: string;
    stale_duration_ms: number;
  };
  reassign_audit?: {
    previous_assignee: string;
    reassigned_by: string;
    reassigned_at: string;
    reassign_reason: string;
  };
}

export interface RoomLoopDeliveryTrace {
  workspace_id?: string;
  room_id?: string;
  message_id?: string;
  task_id?: string;
  recipient?: string;
  delivery_lease_name?: string;
  delivery_owner_id?: string;
  relay_backend?: string;
  chosen_actor_id?: string;
  chosen_mux_backend?: string;
  chosen_mux_session?: string;
  chosen_mux_pane_id?: string;
  chosen_transport_endpoint?: string;
  chosen_transport_kind?: string;
  chosen_submit_mode?: string;
  fallback_attempted?: boolean;
  outcome?: string;
  delivered_count?: number;
  failed_count?: number;
  delivered_to?: string[];
  failed_members?: string[];
  cursor_before_message_id?: string;
  cursor_after_message_id?: string;
  cursor_advanced?: boolean;
  observed_at?: string;
}

export interface RoomLoop {
  enabled: boolean;
  managed_by: string;
  last_tick_at?: string;
  delivery_lease_name?: string;
  delivery_owner_id?: string;
  delivery_cursor_message_id?: string;
  delivery_cursor_at?: string;
  pulse_interval: string;
  task_followup_interval: string;
  reply_stale_after: string;
  task_stale_after: string;
  min_pulse_floor: string;
  interrupt_attempt_limit: number;
  reminder_backoff_cap: number;
  coordinator_pulse_enabled: boolean;
  coordinator_escalation_enabled: boolean;
  last_delivery_trace?: RoomLoopDeliveryTrace;
}

export interface RoomLoopResult {
  room_id: string;
  loop: RoomLoop;
}

export interface RoomLoopHealth {
  status: string;
  reason?: string;
  last_tick_age?: string;
}

export interface RoomControlInbox {
  actor_id: string;
  count: number;
  ack_required: number;
  reply_expected: number;
  latest_actionable?: RoomStatusEntry[];
}

export interface RoomControlMessage {
  id: string;
  task_id?: string;
  related_message_id?: string;
  sender: string;
  recipient: string;
  subject: string;
  kind: string;
  status: string;
  ack_required?: boolean;
  reply_expected?: boolean;
  priority: number;
  created_at: string;
  preview?: string;
}

export interface RoomControlReminder extends RoomReminder {}

export interface RoomLinkedOrchestrationCard {
  issue_id: string;
  issue_identifier?: string;
  title?: string;
  state: string;
  lane?: string;
  tracker_state?: string;
  policy_status?: string;
  last_outcome?: string;
  eligibility?: string;
  run_id?: string;
  agent_id?: string;
  linked_task_id?: string;
}

export interface RoomControlSnapshot {
  room: Room;
  participants: RoomStatusParticipant[];
  loop: RoomLoop;
  loop_health: RoomLoopHealth;
  inbox: RoomControlInbox;
  tasks: RoomTask[];
  task_count: number;
  reminders: RoomControlReminder[];
  messages: RoomControlMessage[];
  linked_orchestration_cards: RoomLinkedOrchestrationCard[];
  task_card_link?: string;
  task_filter?: string;
  issue_filter?: string;
}

export interface MuxPane {
  backend: "tmux" | "zellij" | string;
  id?: string;
  session: string;
  session_pane?: string;
  pane_name?: string;
  label?: string;
  participant_id?: string;
  provider?: string;
  room_id?: string;
  current_command?: string;
  display_command?: string;
  wrapped?: boolean;
  socket_path?: string;
  ready_path?: string;
  state?: string;
  active?: boolean;
}

export interface MuxPaneCapture {
  target: string;
  resolved_target: string;
  lines_requested: number;
  content: string;
  lines?: string[];
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
