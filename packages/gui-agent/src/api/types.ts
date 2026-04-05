// Agent types
export interface Agent {
  id: string;
  parent_id?: string;
  ns: string;
  name?: string; // Human name (e.g., "Luna", "Atlas")
  slug?: string; // Human-readable handle (e.g., "researcher")
  role?: string;
  skills_allow: string[];
  share_bb: string;
  state: "running" | "idle" | "stopped" | "error" | "unknown";
  created_at: string;
  updated_at?: string;
  heartbeat_at?: string;
  prompt_summary?: string;
  llm_provider?: string;
  llm_model?: string;
  exec_mode?: "reactive" | "autonomous" | "proactive" | "tick" | "story";
  think_interval?: number;
  conversation_id?: string; // Linked companion conversation ID
  workspace_root?: string;
  workspace_source?: "local" | "sandbox" | string;
  sandbox_provider?: string;
  sandbox_id?: string;
  repo_url?: string;
  repo_ref?: string;
  memory_scope?: "agent" | "session" | string;
  memory_retention?: "companion" | "durable" | "task" | "ephemeral" | string;
}

export interface CoChangeHit {
  name: string;
  anchor_path: string;
  summary: string;
  score: number;
  neighbors?: string[];
  updated_at?: string;
}

export interface AgentSession {
  session_id: string;
  actor_id: string;
  role: string;
  status: string; // 'running', 'stopped', etc.
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

export interface ActivityEventData extends Record<string, unknown> {
  refs?: string[];
  turn_refs?: string[];
  slice_refs?: string[];
  episode_refs?: string[];
  narrative_refs?: string[];
  artifact_refs?: string[];
  ref_count?: number;
  turn_ref_count?: number;
  slice_ref_count?: number;
  episode_ref_count?: number;
  narrative_ref_count?: number;
  artifact_ref_count?: number;
}

// Activity/Observability types
export interface ActivityEvent {
  operation: string;
  command?: string; // Skill/hook name (e.g., "code/semantic_search")
  status: string;
  component?: string;
  trace_id?: string;
  span_id?: string;
  parent_id?: string;
  service?: string;
  version?: string;
  subtype?: string;
  session_id?: string;
  agent_id?: string;
  workspace_id?: string;
  job_id?: string;
  duration_ms?: number;
  error_type?: string;
  error_code?: string;
  error_message?: string;
  retriable?: boolean;
  ts: string;
  data?: ActivityEventData;
}

export interface LogEntry {
  ts: string;
  operation: string;
  command?: string; // Skill/hook name (e.g., "code/semantic_search")
  status: string;
  component?: string;
  session_id?: string;
  agent_id?: string;
  workspace_id?: string;
  duration_ms?: number;
  error_message?: string;
  data?: Record<string, unknown>;
}

// Mailbox types
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
  related_message_id?: string;
  created_at: string;
  task_id?: string;
  stream?: string;
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

export interface RoomMember {
  actor_id: string;
  role?: string;
  joined_at?: string;
  last_active_at?: string;
  status?: "online" | "idle" | "stale" | string;
  transport?: "tmux" | "zellij" | "unknown" | string;
  session_id?: string;
  pane_id?: string;
  unbound?: boolean;
}

export interface RoomStatus {
  room: Room;
  coordinator_actor_id?: string;
  participants: RoomMember[];
  task_pulse: {
    pending: number;
    in_progress: number;
    blocked: number;
    stale: number;
    completed: number;
  };
  actionable_backlog: {
    pending_acks: number;
    pending_replies: number;
    stale_tasks: number;
    blocked_tasks: number;
  };
}
...
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

export interface RoomTask {
  id: string;
  workspace_id: string;
  room_id: string;
  title: string;
  description?: string;
  status: "pending" | "in_progress" | "blocked" | "completed" | "abandoned";
  priority: number;
  owner_actor_id?: string;
  assigned_actor_id?: string;
  claimed_at?: string;
  heartbeat_at?: string;
  blocked_at?: string;
  blocked_reason?: string;
  created_at: string;
  completed_at?: string;
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

export interface RoomLoop {
  enabled: boolean;
  managed_by: string;
  last_tick_at?: string;
  pulse_interval: string;
  reply_stale_after: string;
  task_stale_after: string;
  min_pulse_floor: string;
  coordinator_pulse_enabled: boolean;
}

// Blackboard types
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

// Companion presence bundle (multimodal metadata returned with assistant messages)
export interface PresenceBundle {
  emotion?: string;
  intensity?: number;
  display_text?: string;
  markers?: string[];
  detected_emoji?: string[];
  background_digest?: string;
  overlay_digest?: string;
  audio_digest?: string;
  audio_duration_ms?: number;
  cache_hits?: number;
  cache_misses?: number;
  errors?: string[];
}

// Companion types
export interface CompanionConversation {
  id: string;
  title?: string;
  created_at: string;
  updated_at: string;
  message_count: number;
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

export type OrchestrationCardAction = "retry-now" | "release" | "mark-done";

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

export interface OrchestrationRefreshResult {
  request_id: string;
  queued: boolean;
  coalesced?: boolean;
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

export interface OrchestrationSeedCardsResult {
  request_id: string;
  created: number;
  skipped?: number;
  ts: string;
}

export interface OrchestrationCardActionResult {
  request_id: string;
  action: OrchestrationCardAction;
  card: OrchestrationCard;
  idempotent?: boolean;
  ts: string;
}

export interface ACAProposalWorkPacket {
  proposal_id: string;
  proposal_kind: string;
  action: string;
  status: string;
  review_required: boolean;
  draft_path?: string;
  target_path?: string;
  heading?: string;
  policy_path?: string;
  promotion_job_id?: string;
  requires_vault_path?: boolean;
  vault_path?: string;
  next_command?: string;
}

export interface ACAMaintenanceTask {
  id: string;
  title: string;
  kind: string;
  priority: number;
  reason: string;
  source_refs?: string[];
  work_packet?: ACAProposalWorkPacket | null;
  status: string;
  created_at: string;
}

export interface ACANextProposalMergeResult {
  workspace_path: string;
  vault_path?: string;
  found: boolean;
  task?: ACAMaintenanceTask | null;
  work_packet?: ACAProposalWorkPacket | null;
  claimed?: boolean;
}

export interface ACAEvidenceImportRun {
  id: string;
  source_kind: string;
  source_ref: string;
  title: string;
  draft_path: string;
  artifact_digest?: string;
  processor_kind?: string;
  processor_model?: string;
  summary: string;
  status: string;
  created_at: string;
}

export interface ACAPromotionJob {
  id: string;
  source_ref: string;
  source_kind: string;
  note_type: string;
  title: string;
  draft_path: string;
  status: string;
  created_at: string;
}

export interface ACAOverviewStats {
  proposal_count: number;
  active_proposal_count: number;
  prepared_merge_count: number;
  claimed_merge_count: number;
  evidence_import_count: number;
  promotion_draft_count: number;
  promotion_merged_count: number;
}

export interface ACAOverview {
  workspace_path: string;
  vault_path?: string;
  stats: ACAOverviewStats;
  next_proposal_merge?: ACAMaintenanceTask | null;
  claimed_proposal_merge?: ACAMaintenanceTask | null;
  proposals: ACAMemoryProposal[];
  evidence_imports: ACAEvidenceImportRun[];
  promotion_jobs: ACAPromotionJob[];
}

export interface ACAMemoryProposal {
  id: string;
  dedupe_key?: string;
  kind: string;
  classification?: string;
  status: string;
  review_required: boolean;
  confidence: number;
  blast_radius?: string;
  summary: string;
  source_refs?: string[];
  proposed_change?: Record<string, unknown>;
  evaluation_status?: string;
  apply_status?: string;
  count: number;
  created_at: string;
  updated_at: string;
}

// API Response wrappers
export interface AgentsListResponse {
  agents: Agent[];
  total: number;
}

export interface AgentSpawnResponse {
  session_id: string;
  actor_id: string;
  status: string;
  name?: string; // Generated or provided name
  workspace_id?: string;
  workspace_root?: string;
  workspace_source?: "local" | "sandbox" | string;
  sandbox_provider?: string;
  sandbox_id?: string;
  repo_url?: string;
  repo_ref?: string;
}

export interface MailboxListResponse {
  messages: MailboxMessage[];
}

export interface BlackboardListResponse {
  records: BlackboardRecord[];
}

export interface LogsListResponse {
  entries: LogEntry[];
  count: number;
}
