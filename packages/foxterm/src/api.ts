import type {
  OrchestrationBoard,
  OrchestrationBoardArtifactRef,
  OrchestrationCard,
  Room,
  RoomTask,
  V2RunTranscript,
  V2RuntimeEvent,
  V2StreamType,
} from "@foxctl/data/types";

const DEFAULT_API_BASE = "http://127.0.0.1:8090";
const DEFAULT_WORKSPACE_ID = ".";
const DEFAULT_ACTOR_ID = "dev-local-user";

export const API_BASE = normalizeApiBase(
  process.env.FOXCTL_API_URL ?? DEFAULT_API_BASE,
);
export const WORKSPACE_ID =
  process.env.FOXTERM_WORKSPACE_ID ??
  process.env.FOXCTL_WORKSPACE_ID ??
  DEFAULT_WORKSPACE_ID;
export const ACTOR_ID =
  process.env.FOXTERM_ACTOR_ID ??
  process.env.FOXCTL_ACTOR_ID ??
  DEFAULT_ACTOR_ID;
export const WORKSPACE_ROOT =
  process.env.FOXTERM_WORKSPACE_ROOT ??
  process.env.FOXCTL_WORKSPACE_ROOT ??
  process.env.PWD ??
  "";

export interface ApiEnvelope<T> {
  version: number;
  status: "ok" | "error" | "progress";
  command: string;
  data: T;
  meta: { ts: string; [key: string]: unknown };
  error: { code?: string; message?: string };
}

export interface RunListItem {
  run_id: string;
  status: string;
  command?: string;
  request_id?: string;
  actor_id?: string;
  updated_at?: string;
}

export interface RunListResult {
  items: RunListItem[];
  count: number;
}

export interface RunDetail extends RunListItem {}

export interface CreateRunInput {
  prompt: string;
  runId?: string;
  profile?: string;
  maxIterations?: number;
  async?: boolean;
}

export interface CreateRunResult {
  run_id: string;
  turn_id: string;
  request_id: string;
  correlation_id?: string;
  profile: string;
  status?: string;
  async?: boolean;
  output?: {
    turn_id?: string;
    summary?: string;
    iterations?: number;
    tool_calls?: number;
    degraded?: boolean;
  };
}

export interface KillRunResult {
  run_id: string;
  status: string;
}

export interface V2ModelEndpoint {
  provider: string;
  model: string;
  base_url: string;
  auth_mode?: string;
}

export interface RoomTaskWorkItem {
  id: string;
  room: Room;
  task?: RoomTask;
}

export interface RoomTaskWorkResult {
  rooms: Room[];
  items: RoomTaskWorkItem[];
}

export interface CreateRoomInput {
  title: string;
  id?: string;
  description?: string;
  workspaceId?: string;
  members?: RoomMemberInput[];
}

export interface CreateRoomResult {
  room: Room;
}

export interface RoomMemberInput {
  actor_id: string;
  role?: string;
}

export interface RoomMessage {
  id: string;
  related_message_id?: string;
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
  created_at: string;
  task_id?: string;
  stream?: string;
}

export interface RoomMessagesResult {
  room_id: string;
  stream: string;
  messages: RoomMessage[];
  count: number;
}

export interface SendRoomMessageInput {
  roomId: string;
  body: string;
  workspaceId?: string;
  sender?: string;
  subject?: string;
  recipient?: string;
  kind?: string;
  taskId?: string;
  dispatchAgents?: boolean;
  dispatchAgentIds?: string[];
}

export interface SendRoomMessageResult {
  id: string;
  room_id: string;
  stream: string;
  status: string;
  message?: string;
  dispatched?: number;
  skipped?: number;
  delivery_owner?: string;
  delivery_pending?: boolean;
}

export interface RoomLoop {
  enabled: boolean;
  managed_by?: string;
  last_tick_at?: string;
  delivery_lease_name?: string;
  delivery_owner_id?: string;
  delivery_cursor_message_id?: string;
  delivery_cursor_at?: string;
  pulse_interval?: string;
  task_followup_interval?: string;
  reply_stale_after?: string;
  task_stale_after?: string;
  min_pulse_floor?: string;
  interrupt_attempt_limit?: number;
  reminder_backoff_cap?: number;
  coordinator_pulse_enabled?: boolean;
  coordinator_escalation_enabled?: boolean;
  last_delivery_trace?: {
    message_id?: string;
    recipient?: string;
    outcome?: string;
    delivered_count?: number;
    failed_count?: number;
    observed_at?: string;
  };
}

export interface RoomLoopResult {
  room_id: string;
  loop: RoomLoop;
}

export interface SpawnAgentInput {
  role: string;
  prompt?: string;
  workspaceId?: string;
  workspaceRoot?: string;
  roomId?: string;
  roomRole?: string;
  name?: string;
  slug?: string;
  execMode?: string;
  maxIterations?: number;
  maxAutoTurns?: number;
  llmProvider?: string;
  llmModel?: string;
}

export interface SpawnAgentResult {
  session_id: string;
  actor_id: string;
  status: string;
  name?: string;
  workspace_id?: string;
  workspace_root?: string;
  workspace_source?: string;
}

export interface AgentSummary {
  id: string;
  parent_id?: string;
  namespace?: string;
  workspace_root?: string;
  workspace_source?: string;
  name?: string;
  slug?: string;
  role?: string;
  state?: string;
  llm_provider?: string;
  llm_model?: string;
  exec_mode?: string;
  heartbeat_at?: string;
  room_id?: string;
}

export interface AgentListResult {
  agents: AgentSummary[];
  total: number;
}

export interface ATCPSession {
  id: string;
  status: string;
  pid: number;
  created_at: string;
  cmd: string[];
  cwd?: string;
  adapter?: string;
  submit_key?: string;
  output_bytes_total?: number;
  output_rate_bps?: number;
  last_output_at?: string;
}

export interface ATCPRoom {
  id: string;
  workspace: string;
  title?: string;
  description?: string;
  created_at: string;
}

export interface ATCPMember {
  room_id: string;
  agent_id: string;
  session_id: string;
  role?: string;
  can_mutate: boolean;
  joined_at: string;
}

export interface SpawnATCPCLIInput {
  roomId: string;
  workspaceId?: string;
  agentId: string;
  adapter?: string;
  cmd: string[];
  cwd?: string;
  role?: string;
  canMutate?: boolean;
}

export interface SpawnATCPCLIResult {
  room: ATCPRoom;
  session: ATCPSession;
  member: ATCPMember;
}

export interface OrchestrationCardWorkItem {
  id: string;
  laneId: string;
  laneTitle: string;
  card: OrchestrationCard;
}

export interface OrchestrationCardWorkResult {
  board: OrchestrationBoard | null;
  artifact: OrchestrationBoardArtifactRef | null;
  items: OrchestrationCardWorkItem[];
}

export async function getRuns(params?: {
  limit?: number;
  status?: string;
}): Promise<RunListResult> {
  const query = new URLSearchParams();
  if (params?.limit) query.set("limit", String(params.limit));
  if (params?.status) query.set("status", params.status);
  const suffix = query.toString();
  const envelope = await requestEnvelope<RunListResult>(
    `/api/v2/runs${suffix ? `?${suffix}` : ""}`,
  );
  const data = unwrapEnvelope(envelope);
  const items = safeArray(data?.items);
  return {
    items,
    count: typeof data?.count === "number" ? data.count : items.length,
  };
}

export async function getRun(runId: string): Promise<RunDetail> {
  const envelope = await requestEnvelope<RunDetail>(
    `/api/v2/runs/${encodeURIComponent(runId)}`,
  );
  return unwrapEnvelope(envelope);
}

export async function getRunTranscript(
  runId: string,
): Promise<V2RunTranscript> {
  const envelope = await requestEnvelope<V2RunTranscript>(
    `/api/v2/runs/${encodeURIComponent(runId)}/transcript`,
  );
  const data = unwrapEnvelope(envelope);
  const items = safeArray(data?.items);
  return {
    run_id: safeString(data?.run_id, runId),
    count: typeof data?.count === "number" ? data.count : items.length,
    items,
  };
}

export async function getV2ModelEndpoint(): Promise<V2ModelEndpoint> {
  const envelope = await requestEnvelope<V2ModelEndpoint>("/api/v2/model");
  return unwrapEnvelope(envelope);
}

export async function createRun(
  input: CreateRunInput,
): Promise<CreateRunResult> {
  const prompt = input.prompt.trim();
  if (prompt === "") {
    throw new Error("prompt is required");
  }
  const ids = newRunIDs();
  const runID = input.runId?.trim() || ids.runID;
  const query = new URLSearchParams();
  query.set("profile", input.profile ?? "worker");
  if (input.async ?? true) {
    query.set("async", "true");
  }
  const envelope = await requestEnvelope<CreateRunResult>(
    `/api/v2/runs?${query.toString()}`,
    {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({
        run_id: runID,
        turn_id: ids.turnID,
        request_id: ids.requestID,
        prompt,
        max_iterations: input.maxIterations ?? 1,
      }),
    },
  );
  return unwrapEnvelope(envelope);
}

export async function killRun(runId: string): Promise<KillRunResult> {
  const trimmed = runId.trim();
  if (trimmed === "") {
    throw new Error("run_id is required");
  }
  const envelope = await requestEnvelope<KillRunResult>(
    `/api/v2/runs/${encodeURIComponent(trimmed)}/kill`,
    {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ request_id: `req-foxterm-${newIDFragment()}` }),
    },
  );
  return unwrapEnvelope(envelope);
}

export async function createRoom(
  input: CreateRoomInput,
): Promise<CreateRoomResult> {
  const title = input.title.trim();
  if (title === "") {
    throw new Error("room title is required");
  }
  const workspaceId = input.workspaceId ?? WORKSPACE_ID;
  const body: Record<string, unknown> = {
    workspace_id: workspaceId,
    title,
    members: input.members ?? [{ actor_id: ACTOR_ID, role: "coordinator" }],
  };
  const id = input.id?.trim();
  const description = input.description?.trim();
  if (id) body.id = id;
  if (description) body.description = description;
  return requestJSON<CreateRoomResult>("/api/rooms", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(body),
  });
}

export async function getRoomMessages(params: {
  roomId: string;
  workspaceId?: string;
  limit?: number;
}): Promise<RoomMessagesResult> {
  const roomId = params.roomId.trim();
  if (roomId === "") {
    throw new Error("room_id is required");
  }
  const query = new URLSearchParams();
  query.set("workspace_id", params.workspaceId ?? WORKSPACE_ID);
  query.set("limit", String(params.limit ?? 20));
  const result = await requestJSON<RoomMessagesResult>(
    `/api/rooms/${encodeURIComponent(roomId)}/messages?${query.toString()}`,
  );
  const messages = safeArray(result?.messages).filter(hasID);
  return {
    room_id: safeString(result?.room_id, roomId),
    stream: safeString(result?.stream, `room:${roomId}`),
    count: typeof result?.count === "number" ? result.count : messages.length,
    messages,
  };
}

export async function sendRoomMessage(
  input: SendRoomMessageInput,
): Promise<SendRoomMessageResult> {
  const roomId = input.roomId.trim();
  const body = input.body.trim();
  if (roomId === "") {
    throw new Error("room_id is required");
  }
  if (body === "") {
    throw new Error("message body is required");
  }
  const payload: Record<string, unknown> = {
    workspace_id: input.workspaceId ?? WORKSPACE_ID,
    sender: input.sender ?? ACTOR_ID,
    body,
    kind: input.kind ?? "info",
  };
  const subject = input.subject?.trim();
  const recipient = input.recipient?.trim();
  const taskId = input.taskId?.trim();
  if (subject) payload.subject = subject;
  if (recipient) payload.recipient = recipient;
  if (taskId) payload.task_id = taskId;
  if (typeof input.dispatchAgents === "boolean") {
    payload.dispatch_agents = input.dispatchAgents;
  }
  if (input.dispatchAgentIds && input.dispatchAgentIds.length > 0) {
    payload.dispatch_agent_ids = input.dispatchAgentIds;
  }
  return requestJSON<SendRoomMessageResult>(
    `/api/rooms/${encodeURIComponent(roomId)}/messages`,
    {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(payload),
    },
  );
}

export async function getRoomLoop(params: {
  roomId: string;
  workspaceId?: string;
  actorId?: string;
}): Promise<RoomLoopResult> {
  const roomId = params.roomId.trim();
  if (roomId === "") {
    throw new Error("room_id is required");
  }
  const query = new URLSearchParams();
  query.set("workspace_id", params.workspaceId ?? WORKSPACE_ID);
  query.set("actor_id", params.actorId ?? ACTOR_ID);
  return requestJSON<RoomLoopResult>(
    `/api/rooms/${encodeURIComponent(roomId)}/loop?${query.toString()}`,
  );
}

export async function spawnAgent(
  input: SpawnAgentInput,
): Promise<SpawnAgentResult> {
  const role = input.role.trim();
  if (role === "") {
    throw new Error("agent role is required");
  }
  const payload: Record<string, unknown> = {
    role,
    workspace_id: input.workspaceId ?? WORKSPACE_ID,
    exec_mode: input.execMode ?? "reactive",
    max_iterations: input.maxIterations ?? 10,
    max_auto_turns: input.maxAutoTurns ?? 1,
  };
  const workspaceRoot = input.workspaceRoot ?? WORKSPACE_ROOT;
  if (workspaceRoot.trim() !== "") payload.workspace_root = workspaceRoot;
  const prompt = input.prompt?.trim();
  const roomId = input.roomId?.trim();
  const roomRole = input.roomRole?.trim();
  const name = input.name?.trim();
  const slug = input.slug?.trim();
  const llmProvider = input.llmProvider?.trim();
  const llmModel = input.llmModel?.trim();
  if (prompt) payload.prompt = prompt;
  if (roomId) payload.room_id = roomId;
  if (roomRole) payload.room_role = roomRole;
  if (name) payload.name = name;
  if (slug) payload.slug = slug;
  if (llmProvider) payload.llm_provider = llmProvider;
  if (llmModel) payload.llm_model = llmModel;
  return requestJSON<SpawnAgentResult>("/api/agents/spawn", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(payload),
  });
}

export async function getAgents(params?: { limit?: number }): Promise<AgentListResult> {
  const query = new URLSearchParams();
  query.set("limit", String(params?.limit ?? 100));
  const result = await requestJSON<AgentListResult>(
    `/api/agents?${query.toString()}`,
  );
  const agents = safeArray(result?.agents).filter(hasID);
  return {
    agents,
    total: typeof result?.total === "number" ? result.total : agents.length,
  };
}

export async function spawnATCPCLIForRoom(
  input: SpawnATCPCLIInput,
): Promise<SpawnATCPCLIResult> {
  const roomId = input.roomId.trim();
  const agentId = input.agentId.trim();
  if (roomId === "") {
    throw new Error("room_id is required");
  }
  if (agentId === "") {
    throw new Error("agent_id is required");
  }
  if (input.cmd.length === 0 || input.cmd[0]?.trim() === "") {
    throw new Error("cmd is required");
  }
  const payload: Record<string, unknown> = {
    workspace_id: input.workspaceId ?? WORKSPACE_ID,
    agent_id: agentId,
    cmd: input.cmd,
    can_mutate: input.canMutate ?? true,
  };
  const adapter = input.adapter?.trim();
  const cwd = input.cwd?.trim();
  const role = input.role?.trim();
  if (adapter) payload.adapter = adapter;
  if (cwd) payload.cwd = cwd;
  if (role) payload.role = role;
  return requestJSON<SpawnATCPCLIResult>(
    `/api/atcp/foxctl-rooms/${encodeURIComponent(roomId)}/spawn-cli`,
    {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(payload),
    },
  );
}

export async function getRoomTaskWork(params?: {
  workspaceId?: string;
  roomLimit?: number;
  taskLimit?: number;
}): Promise<RoomTaskWorkResult> {
  const workspaceId = params?.workspaceId ?? WORKSPACE_ID;
  const roomsResult = await requestJSON<{ rooms: Room[]; count: number }>(
    `/api/rooms?${roomQuery({ workspaceId, limit: params?.roomLimit ?? 25 })}`,
  );
  const rooms = safeArray(roomsResult?.rooms).filter(hasID);
  const taskResults = await Promise.all(
    rooms.map(async (room) => {
      const tasksResult = await requestJSON<{
        room: Room;
        tasks: RoomTask[];
        count: number;
      }>(
        `/api/rooms/${encodeURIComponent(room.id)}/tasks?${roomQuery({
          workspaceId,
          limit: params?.taskLimit ?? 100,
        })}`,
      );
      const responseRoom = hasID(tasksResult?.room) ? tasksResult.room : room;
      const tasks = safeArray(tasksResult?.tasks).filter(hasID);
      if (tasks.length === 0) {
        return [{ id: `room:${room.id}`, room: responseRoom }];
      }
      return tasks.map((task) => ({
        id: `${room.id}:${task.id}`,
        room: responseRoom,
        task,
      }));
    }),
  );
  return { rooms, items: taskResults.flat() };
}

export async function getOrchestrationCardWork(params?: {
  workspaceId?: string;
  limit?: number;
}): Promise<OrchestrationCardWorkResult> {
  const workspaceId = params?.workspaceId ?? WORKSPACE_ID;
  const query = new URLSearchParams();
  query.set("workspace_id", workspaceId);
  query.set("limit", String(params?.limit ?? 100));
  const envelope = await requestEnvelope<unknown>(
    `/api/orchestration/board-get?${query.toString()}`,
  );
  const result = normalizeBoardPayload(unwrapEnvelope(envelope));
  const items =
    safeArray(result.board?.lanes).flatMap((lane) =>
      safeArray(lane.cards).filter(hasIssueID).map((card) => ({
        id: card.issue_id,
        laneId: safeString(lane.id, "Other"),
        laneTitle: safeString(lane.title, safeString(lane.id, "Other")),
        card,
      })),
    );
  return { ...result, items };
}

export function subscribeToV2Stream(params: {
  streamId: string;
  streamType?: V2StreamType;
  afterVersion?: number;
  onEvent: (event: V2RuntimeEvent) => void;
  onStatus?: (message: string) => void;
  onError?: (message: string) => void;
}): () => void {
  const controller = new AbortController();
  void streamV2Events(params, controller.signal);
  return () => controller.abort();
}

async function streamV2Events(
  params: {
    streamId: string;
    streamType?: V2StreamType;
    afterVersion?: number;
    onEvent: (event: V2RuntimeEvent) => void;
    onStatus?: (message: string) => void;
    onError?: (message: string) => void;
  },
  signal: AbortSignal,
): Promise<void> {
  const query = new URLSearchParams();
  query.set("stream_id", params.streamId);
  query.set("stream_type", params.streamType ?? "run");
  if (typeof params.afterVersion === "number") {
    query.set("after_version", String(params.afterVersion));
  }
  const url = apiURL(`/api/v2/events/stream?${query.toString()}`);
  let response: Response;
  try {
    response = await fetch(url, {
      headers: { Accept: "text/event-stream" },
      signal,
    });
  } catch (error) {
    if (!signal.aborted) params.onError?.(connectionErrorMessage(error));
    return;
  }

  if (!response.ok) {
    const body = (await response.text()).trim();
    params.onError?.(
      `foxctl event stream ${response.status} at ${API_BASE}: ${
        body || response.statusText
      }`,
    );
    return;
  }

  if (!response.body) {
    params.onError?.(`foxctl event stream at ${API_BASE} returned no body`);
    return;
  }

  const reader = response.body.getReader();
  const decoder = new TextDecoder();
  let buffer = "";
  try {
    while (!signal.aborted) {
      const { done, value } = await reader.read();
      if (done) break;
      buffer += decoder.decode(value, { stream: true });
      const chunks = buffer.split(/\r?\n\r?\n/);
      buffer = chunks.pop() ?? "";
      for (const chunk of chunks) {
        handleSSEChunk(chunk, params);
      }
    }
    buffer += decoder.decode();
    if (buffer.trim() !== "") handleSSEChunk(buffer, params);
  } catch (error) {
    if (!signal.aborted) {
      params.onError?.(`v2 event stream failed: ${errorMessage(error)}`);
    }
  } finally {
    reader.releaseLock();
  }
}

async function requestEnvelope<T>(
  path: string,
  init?: RequestInit,
): Promise<ApiEnvelope<T>> {
  const url = apiURL(path);
  let response: Response;
  try {
    response = await fetch(url, {
      ...init,
      headers: {
        Accept: "application/json",
        ...(init?.headers ?? {}),
      },
    });
  } catch (error) {
    throw new Error(connectionErrorMessage(error));
  }
  if (!response.ok) {
    const body = (await response.text()).trim();
    throw new Error(
      `foxctl API ${response.status} at ${API_BASE}: ${body || response.statusText}`,
    );
  }
  try {
    return (await response.json()) as ApiEnvelope<T>;
  } catch (error) {
    throw new Error(
      `foxctl API at ${API_BASE} returned invalid JSON: ${errorMessage(error)}`,
    );
  }
}

async function requestJSON<T>(path: string, init?: RequestInit): Promise<T> {
  const url = apiURL(path);
  let response: Response;
  try {
    response = await fetch(url, {
      ...init,
      headers: {
        Accept: "application/json",
        ...(init?.headers ?? {}),
      },
    });
  } catch (error) {
    throw new Error(connectionErrorMessage(error));
  }
  if (!response.ok) {
    const body = (await response.text()).trim();
    throw new Error(
      `foxctl API ${response.status} at ${API_BASE}: ${body || response.statusText}`,
    );
  }
  try {
    return (await response.json()) as T;
  } catch (error) {
    throw new Error(
      `foxctl API at ${API_BASE} returned invalid JSON: ${errorMessage(error)}`,
    );
  }
}

function unwrapEnvelope<T>(envelope: ApiEnvelope<T>): T {
  if (!envelope || envelope.status !== "ok") {
    throw new Error(envelope.error?.message ?? "request failed");
  }
  return envelope.data;
}

function normalizeBoardPayload(data: unknown): {
  board: OrchestrationBoard | null;
  artifact: OrchestrationBoardArtifactRef | null;
} {
  if (!data || typeof data !== "object") {
    return { board: null, artifact: null };
  }
  const record = data as Record<string, unknown>;
  if (Array.isArray(record.lanes)) {
    const board = data as OrchestrationBoard;
    return {
      board: { ...board, lanes: safeArray(board.lanes) },
      artifact: null,
    };
  }
  if (typeof record.artifact === "string") {
    return {
      board: null,
      artifact: data as OrchestrationBoardArtifactRef,
    };
  }
  return { board: null, artifact: null };
}

function apiURL(path: string): string {
  return `${API_BASE}${path.startsWith("/") ? path : `/${path}`}`;
}

function normalizeApiBase(raw: string): string {
  const trimmed = raw.trim();
  if (trimmed === "") return DEFAULT_API_BASE;
  return trimmed.replace(/\/+$/, "");
}

function newRunIDs(): { runID: string; turnID: string; requestID: string } {
  const suffix = newIDFragment();
  return {
    runID: `run-foxterm-${suffix}`,
    turnID: `turn-foxterm-${suffix}`,
    requestID: `req-foxterm-${suffix}`,
  };
}

function newIDFragment(): string {
  if (typeof crypto !== "undefined" && "randomUUID" in crypto) {
    return crypto.randomUUID().slice(0, 12);
  }
  return `${Date.now().toString(36)}-${Math.random().toString(36).slice(2, 8)}`;
}

function roomQuery(params: { workspaceId: string; limit: number }): string {
  const query = new URLSearchParams();
  query.set("workspace_id", params.workspaceId);
  query.set("limit", String(params.limit));
  return query.toString();
}

function safeArray<T>(value: T[] | null | undefined): T[] {
  return Array.isArray(value) ? value : [];
}

function safeString(value: unknown, fallback: string): string {
  return typeof value === "string" && value.trim() !== "" ? value : fallback;
}

function hasID<T extends { id?: unknown }>(
  value: T | null | undefined,
): value is T & { id: string } {
  return typeof value?.id === "string" && value.id.trim() !== "";
}

function hasIssueID<T extends { issue_id?: unknown }>(
  value: T | null | undefined,
): value is T & { issue_id: string } {
  return typeof value?.issue_id === "string" && value.issue_id.trim() !== "";
}

function connectionErrorMessage(error: unknown): string {
  return [
    `Cannot connect to foxctl API at ${API_BASE}.`,
    `Start it with "bun run dev:server" or set FOXCTL_API_URL.`,
    errorMessage(error),
  ]
    .filter(Boolean)
    .join(" ");
}

function errorMessage(error: unknown): string {
  return error instanceof Error ? error.message : String(error ?? "");
}

function handleSSEChunk(
  chunk: string,
  params: {
    onEvent: (event: V2RuntimeEvent) => void;
    onStatus?: (message: string) => void;
    onError?: (message: string) => void;
  },
): void {
  const parsed = parseSSEChunk(chunk);
  if (!parsed) return;

  switch (parsed.event) {
    case "v2.connected":
      params.onStatus?.("connected");
      return;
    case "v2.replay_complete":
      params.onStatus?.("live");
      return;
    case "v2.event":
      try {
        params.onEvent(JSON.parse(parsed.data) as V2RuntimeEvent);
      } catch (error) {
        params.onError?.(`failed to parse v2 event: ${errorMessage(error)}`);
      }
      return;
    case "v2.error":
      params.onError?.(parsed.data || "v2 event stream error");
      return;
    case "heartbeat":
      return;
    default:
      return;
  }
}

function parseSSEChunk(chunk: string): { event: string; data: string } | null {
  let event = "message";
  const data: string[] = [];
  for (const line of chunk.split(/\r?\n/)) {
    if (line === "" || line.startsWith(":")) continue;
    const colon = line.indexOf(":");
    const field = colon < 0 ? line : line.slice(0, colon);
    let value = colon < 0 ? "" : line.slice(colon + 1);
    if (value.startsWith(" ")) value = value.slice(1);

    if (field === "event") event = value;
    if (field === "data") data.push(value);
  }
  if (data.length === 0 && event === "message") return null;
  return { event, data: data.join("\n") };
}
