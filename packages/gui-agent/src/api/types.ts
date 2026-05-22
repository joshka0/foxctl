import type {
  ApiEnvelope as DataApiEnvelope,
  BlackboardRecord as DataBlackboardRecord,
  BulkResolveRequest as DataBulkResolveRequest,
  LeadChangeEvent as DataLeadChangeEvent,
  MailboxMessage as DataMailboxMessage,
  MuxPane as DataMuxPane,
  MuxPaneCapture as DataMuxPaneCapture,
  OrchestrationBoard as DataOrchestrationBoard,
  OrchestrationBoardArtifactRef as DataOrchestrationBoardArtifactRef,
  OrchestrationBoardCardRuntimeResult as DataOrchestrationBoardCardRuntimeResult,
  OrchestrationBoardResult as DataOrchestrationBoardResult,
  OrchestrationArchiveCardsRequest as DataOrchestrationArchiveCardsRequest,
  OrchestrationArchiveCardsResult as DataOrchestrationArchiveCardsResult,
  OrchestrationCard as DataOrchestrationCard,
  OrchestrationCardAction as DataOrchestrationCardAction,
  OrchestrationCardActionResult as DataOrchestrationCardActionResult,
  OrchestrationCleanupCardsRequest as DataOrchestrationCleanupCardsRequest,
  OrchestrationCleanupCardsResult as DataOrchestrationCleanupCardsResult,
  OrchestrationLane as DataOrchestrationLane,
  OrchestrationLaneID as DataOrchestrationLaneID,
  OrchestrationRefreshResult as DataOrchestrationRefreshResult,
  OrchestrationRestoreCardsRequest as DataOrchestrationRestoreCardsRequest,
  OrchestrationRestoreCardsResult as DataOrchestrationRestoreCardsResult,
  OrchestrationRuntimeTree as DataOrchestrationRuntimeTree,
  OrchestrationRuntimeTreeNode as DataOrchestrationRuntimeTreeNode,
  OrchestrationSeedCardInput as DataOrchestrationSeedCardInput,
  OrchestrationSeedCardsRequest as DataOrchestrationSeedCardsRequest,
  OrchestrationSeedCardsResult as DataOrchestrationSeedCardsResult,
  ParticipantState as DataParticipantState,
  Room as DataRoom,
  RoomDeliveryBinding as DataRoomDeliveryBinding,
  RoomInbox as DataRoomInbox,
  RoomLiveRelayResult as DataRoomLiveRelayResult,
  RoomLoop as DataRoomLoop,
  RoomMember as DataRoomMember,
  RoomMessageEvent as DataRoomMessageEvent,
  RoomReminder as DataRoomReminder,
  RoomSendMessageResult as DataRoomSendMessageResult,
  RoomStatus as DataRoomStatus,
  RoomStatusParticipant as DataRoomStatusParticipant,
  RoomTask as DataRoomTask,
} from "@foxctl/data/types";

export type ApiEnvelope<T> = DataApiEnvelope<T>;

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

export type RoomMessageEvent = DataRoomMessageEvent;
export type RoomLiveRelayResult = DataRoomLiveRelayResult;
export type RoomSendMessageResult = DataRoomSendMessageResult;
export type RoomReminder = DataRoomReminder;

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

// Shared room/mailbox DTOs live in @foxctl/data; this module re-exports them for
// existing gui-agent imports while UI-only DTOs remain local below.
export type MailboxMessage = DataMailboxMessage;
export type Room = DataRoom;
export type ParticipantState = DataParticipantState;

/** Derives the transport kind from the endpoint string. */
export function participantTransportKind(state?: ParticipantState): "pane_socket" | "mux_pane" | "none" {
  const ep = state?.transport_endpoint;
  if (!ep) return "none";
  if (ep.startsWith("/")) return "pane_socket";
  return "mux_pane";
}

export type RoomMember = DataRoomMember;
export type RoomDeliveryBinding = DataRoomDeliveryBinding;
export type RoomStatusParticipant = DataRoomStatusParticipant;
export type RoomStatus = DataRoomStatus;
export type LeadChangeEvent = DataLeadChangeEvent;
export type BulkResolveRequest = DataBulkResolveRequest;
export type RoomInbox = DataRoomInbox;
export type RoomTask = DataRoomTask;
export type RoomLoop = DataRoomLoop;
export type MuxPane = DataMuxPane;
export type MuxPaneCapture = DataMuxPaneCapture;

// Blackboard types
export type BlackboardRecord = DataBlackboardRecord;

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
export type OrchestrationLaneID = DataOrchestrationLaneID;
export type OrchestrationCard = DataOrchestrationCard;
export type OrchestrationCardAction = DataOrchestrationCardAction;
export type OrchestrationRuntimeTreeNode = DataOrchestrationRuntimeTreeNode;
export type OrchestrationRuntimeTree = DataOrchestrationRuntimeTree;
export type OrchestrationBoardCardRuntimeResult = DataOrchestrationBoardCardRuntimeResult;
export type OrchestrationLane = DataOrchestrationLane;
export type OrchestrationBoard = DataOrchestrationBoard;
export type OrchestrationBoardArtifactRef = DataOrchestrationBoardArtifactRef;
export type OrchestrationBoardResult = DataOrchestrationBoardResult;
export type OrchestrationRefreshResult = DataOrchestrationRefreshResult;
export type OrchestrationSeedCardInput = DataOrchestrationSeedCardInput;
export type OrchestrationSeedCardsRequest = DataOrchestrationSeedCardsRequest;
export type OrchestrationSeedCardsResult = DataOrchestrationSeedCardsResult;
export type OrchestrationCardActionResult = DataOrchestrationCardActionResult;
export type OrchestrationCleanupCardsRequest = DataOrchestrationCleanupCardsRequest;
export type OrchestrationCleanupCardsResult = DataOrchestrationCleanupCardsResult;
export type OrchestrationArchiveCardsRequest = DataOrchestrationArchiveCardsRequest;
export type OrchestrationArchiveCardsResult = DataOrchestrationArchiveCardsResult;
export type OrchestrationRestoreCardsRequest = DataOrchestrationRestoreCardsRequest;
export type OrchestrationRestoreCardsResult = DataOrchestrationRestoreCardsResult;

export interface ContextWikiProposalWorkPacket {
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

export interface ContextWikiMaintenanceTask {
  id: string;
  title: string;
  kind: string;
  priority: number;
  reason: string;
  source_refs?: string[];
  work_packet?: ContextWikiProposalWorkPacket | null;
  status: string;
  created_at: string;
}

export interface ContextWikiNextProposalMergeResult {
  workspace_path: string;
  vault_path?: string;
  found: boolean;
  task?: ContextWikiMaintenanceTask | null;
  work_packet?: ContextWikiProposalWorkPacket | null;
  claimed?: boolean;
}

export interface ContextWikiEvidenceImportRun {
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

export interface ContextWikiPromotionJob {
  id: string;
  source_ref: string;
  source_kind: string;
  note_type: string;
  title: string;
  draft_path: string;
  status: string;
  created_at: string;
}

export interface ContextWikiOverviewStats {
  proposal_count: number;
  active_proposal_count: number;
  prepared_merge_count: number;
  claimed_merge_count: number;
  evidence_import_count: number;
  promotion_draft_count: number;
  promotion_merged_count: number;
}

export interface ContextWikiOverview {
  workspace_path: string;
  vault_path?: string;
  stats: ContextWikiOverviewStats;
  next_proposal_merge?: ContextWikiMaintenanceTask | null;
  claimed_proposal_merge?: ContextWikiMaintenanceTask | null;
  proposals: ContextWikiMemoryProposal[];
  evidence_imports: ContextWikiEvidenceImportRun[];
  promotion_jobs: ContextWikiPromotionJob[];
}

export interface ContextWikiMemoryProposal {
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

// Flow / Canvas types
export type FlowState = "draft" | "running" | "paused" | "stopped" | "errored";
export type FlowNodeKind = "skill" | "pty" | "http" | "playwright" | "image" | "transform" | "agent";
export type FlowTransformKind = "passthrough" | "regex_extract" | "template" | "jq_filter" | "split_lines" | "map_fields" | "file_write";
export type FlowTriggerKind = "output_ready" | "screen_match" | "exit" | "manual";

export interface FlowPosition {
  x: number;
  y: number;
}

export interface Flow {
  id: string;
  name: string;
  workspace: string;
  state: FlowState;
  description?: string;
  room_id?: string;
  created_at: string;
  updated_at: string;
}

export interface FlowNode {
  id: string;
  flow_id: string;
  kind: FlowNodeKind;
  label: string;
  config: Record<string, unknown>;
  position?: FlowPosition;
}

export interface FlowEdge {
  id: string;
  flow_id: string;
  from_node_id: string;
  to_node_id: string;
  transform: FlowTransformKind;
  transform_config?: string;
  trigger: FlowTriggerKind;
  trigger_config?: string;
  condition?: string;
}

export interface FlowRun {
  id: string;
  flow_id: string;
  state: "running" | "completed" | "failed";
  started_at: string;
  completed_at?: string;
  error?: string;
}

export interface FlowNodeExecState {
  id: string;
  label: string;
  kind: string;
  state: string;
  error?: string;
  duration_ms?: number;
  session_id?: string;
}

export interface FlowEdgeExecState {
  id: string;
  from: string;
  to: string;
  delivery_count: number;
  last_delivery_at?: string;
}

export interface FlowStatusResponse {
  flow_id: string;
  state: string;
  run_id?: string;
  nodes: FlowNodeExecState[];
  edges: FlowEdgeExecState[];
}

export interface FlowRunLog {
  id: string;
  run_id: string;
  node_id: string;
  seq: number;
  envelope: Record<string, unknown>;
  created_at: string;
}
