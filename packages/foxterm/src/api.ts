import type {
  OrchestrationBoard,
  OrchestrationBoardArtifactRef,
  OrchestrationCard,
  Room,
  RoomTask,
  V2RuntimeEvent,
  V2StreamType,
} from "@foxctl/data/types";

const DEFAULT_API_BASE = "http://127.0.0.1:8090";
const DEFAULT_WORKSPACE_ID = ".";

export const API_BASE = normalizeApiBase(
  process.env.FOXCTL_API_URL ?? DEFAULT_API_BASE,
);
export const WORKSPACE_ID =
  process.env.FOXTERM_WORKSPACE_ID ??
  process.env.FOXCTL_WORKSPACE_ID ??
  DEFAULT_WORKSPACE_ID;

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

export interface RoomTaskWorkItem {
  id: string;
  room: Room;
  task?: RoomTask;
}

export interface RoomTaskWorkResult {
  rooms: Room[];
  items: RoomTaskWorkItem[];
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

export async function createRun(
  input: CreateRunInput,
): Promise<CreateRunResult> {
  const prompt = input.prompt.trim();
  if (prompt === "") {
    throw new Error("prompt is required");
  }
  const ids = newRunIDs();
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
        run_id: ids.runID,
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

async function requestJSON<T>(path: string): Promise<T> {
  const url = apiURL(path);
  let response: Response;
  try {
    response = await fetch(url, {
      headers: { Accept: "application/json" },
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
