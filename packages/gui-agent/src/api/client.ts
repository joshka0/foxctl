import type {
  ACAMemoryProposal,
  ACAOverview,
  ACANextProposalMergeResult,
  AgentsListResponse,
  AgentRuntimeTree,
  AgentSpawnResponse,
  CoChangeHit,
  MailboxListResponse,
  MailboxMessage,
  BulkResolveRequest,
  Room,
  RoomMember,
  RoomStatus,
  RoomInbox,
  RoomTask,
  RoomLoop,
  MuxPane,
  MuxPaneCapture,
  RoomSendMessageResult,
  RoomReminder,
  BlackboardListResponse,
  LogsListResponse,
  AgentSession,
  PresenceBundle,
  OrchestrationBoard,
  OrchestrationBoardArtifactRef,
  OrchestrationBoardCardRuntimeResult,
  OrchestrationCard,
  OrchestrationCardAction,
  OrchestrationCardActionResult,
  OrchestrationLaneID,
  OrchestrationRefreshResult,
  OrchestrationSeedCardInput,
  OrchestrationSeedCardsResult,
} from "./types";

const API_BASE = "/api";
const IS_DEV = import.meta.env.DEV;

const DEV_AUTH_SESSION: AuthSessionResponse = {
  session: {
    id: "dev-local-session",
  },
  user: {
    id: "dev-local-user",
    email: "local@agentctl.dev",
    name: "Local Dev",
    emailVerified: true,
  },
};

export class APIUnauthorizedError extends Error {
  readonly status = 401;

  constructor(message = "Authentication required") {
    super(message);
    this.name = "APIUnauthorizedError";
  }
}

function mergeHeaders(
  defaults: Record<string, string>,
  headers: RequestInit["headers"],
): Headers {
  const merged = new Headers();

  for (const [k, v] of Object.entries(defaults)) {
    merged.set(k, v);
  }

  if (!headers) {
    return merged;
  }
  if (headers instanceof Headers) {
    headers.forEach((v, k) => merged.set(k, v));
    return merged;
  }
  if (Array.isArray(headers)) {
    for (const [k, v] of headers) {
      merged.set(k, v);
    }
    return merged;
  }

  for (const [k, v] of Object.entries(headers)) {
    if (typeof v === "undefined") continue;
    merged.set(k, String(v));
  }
  return merged;
}

/**
 * Send an HTTP request to the API base URL and parse the JSON response.
 *
 * @param endpoint - Path relative to the module's API base (appended to `API_BASE`)
 * @param options - Fetch options to merge with defaults; provided headers are merged and `'Content-Type'` defaults to `'application/json'`
 * @returns The response body parsed as JSON typed as `T`, or `undefined` if the response body is empty
 * @throws Error if the HTTP response is not OK (includes response text or status) or if the response body is not valid JSON
 */
async function request<T>(
  endpoint: string,
  options: RequestInit = {},
): Promise<T> {
  const defaults: Record<string, string> = { Accept: "application/json" };
  // Only set JSON Content-Type when sending a JSON body. (Avoid overriding
  // FormData/multipart or triggering unnecessary CORS preflights.)
  if (typeof options.body === "string" && options.body.length > 0) {
    defaults["Content-Type"] = "application/json";
  }

  const headers = mergeHeaders(defaults, options.headers);

  const response = await fetch(`${API_BASE}${endpoint}`, {
    ...options,
    headers,
  });

  const text = await response.text();
  if (!response.ok) {
    if (response.status === 401 && !text) {
      throw new APIUnauthorizedError();
    }
    if (text) {
      try {
        const parsed = JSON.parse(text) as {
          error?: { message?: string; code?: string };
          message?: string;
        };
        const envelopeMessage = parsed?.error?.message || parsed?.message;
        if (envelopeMessage) {
          if (response.status === 401) {
            throw new APIUnauthorizedError(envelopeMessage);
          }
          throw new Error(envelopeMessage);
        }
      } catch (parseErr) {
        if (parseErr instanceof SyntaxError) {
          // Non-JSON error body; fall back to raw text/status below.
        } else if (parseErr instanceof Error && parseErr.message) {
          throw parseErr;
        }
      }
    }
    if (response.status === 401) {
      throw new APIUnauthorizedError();
    }
    throw new Error(text || `Request failed: ${response.status}`);
  }
  if (!text) {
    return undefined as T;
  }
  try {
    return JSON.parse(text) as T;
  } catch {
    throw new Error("Invalid JSON response");
  }
}

// ApiEnvelope matches the canonical agentctl envelope returned by some API endpoints.
interface ApiEnvelope<T> {
  version: number;
  status: "ok" | "error" | "progress";
  command: string;
  data: T;
  meta: { ts: string; [key: string]: unknown };
  error: { code?: string; message?: string };
}

function unwrapEnvelope<T>(env: ApiEnvelope<T>): T {
  if (env.status !== "ok") {
    throw new Error(env.error?.message || "Request failed");
  }
  return env.data;
}

export interface AuthSessionResponse {
  session: {
    id: string;
    expiresAt?: string;
  };
  user: {
    id: string;
    email: string;
    name?: string;
    image?: string | null;
    emailVerified?: boolean;
  };
}

function isLocalDevAuthFallbackEligible(): boolean {
  if (typeof window === "undefined") return false;
  const host = window.location.hostname.trim().toLowerCase();
  return host === "localhost" || host === "127.0.0.1" || host === "0.0.0.0";
}

export async function getAuthSession(): Promise<AuthSessionResponse | null> {
  if (IS_DEV && isLocalDevAuthFallbackEligible()) {
    return DEV_AUTH_SESSION;
  }
  try {
    return await request<AuthSessionResponse>("/auth/session");
  } catch (error) {
    if (error instanceof APIUnauthorizedError) {
      return null;
    }
    if (IS_DEV && error instanceof Error && error.message.includes("404")) {
      return DEV_AUTH_SESSION;
    }
    throw error;
  }
}

export async function signOutAuthSession(): Promise<void> {
  const response = await fetch("/logout", {
    method: "POST",
    headers: {
      Accept: "application/json",
    },
  });

  if (!response.ok) {
    if (IS_DEV && response.status === 404) {
      return;
    }
    const text = await response.text();
    if (
      isLocalDevAuthFallbackEligible() &&
      (response.status === 404 || /page not found/i.test(text))
    ) {
      return;
    }
    throw new Error(text || `Sign out failed: ${response.status}`);
  }
}

export interface OrchestrationBoardResult {
  board: OrchestrationBoard | null;
  artifact: OrchestrationBoardArtifactRef | null;
}

export interface OrchestrationBoardGetParams {
  request_id?: string;
  workspace_id?: string;
  limit?: number;
  cursor?: string;
  lane?: OrchestrationLaneID;
  archived_only?: boolean;
}

function normalizeBoardPayload(data: unknown): OrchestrationBoardResult {
  if (!data || typeof data !== "object") {
    return { board: null, artifact: null };
  }
  const asRecord = data as Record<string, unknown>;
  if (Array.isArray(asRecord.lanes)) {
    return { board: asRecord as unknown as OrchestrationBoard, artifact: null };
  }
  if (typeof asRecord.artifact === "string") {
    return {
      board: null,
      artifact: asRecord as unknown as OrchestrationBoardArtifactRef,
    };
  }
  return { board: null, artifact: null };
}

export async function getOrchestrationBoard(
  params: OrchestrationBoardGetParams = {},
): Promise<OrchestrationBoardResult> {
  const query = new URLSearchParams();
  if (params.request_id) query.set("request_id", params.request_id);
  if (params.workspace_id) query.set("workspace_id", params.workspace_id);
  if (typeof params.limit === "number" && Number.isFinite(params.limit)) {
    query.set("limit", String(params.limit));
  }
  if (params.cursor) query.set("cursor", params.cursor);
  if (params.lane) query.set("lane", params.lane);
  if (params.archived_only) query.set("archived_only", "true");

  const suffix = query.size > 0 ? `?${query.toString()}` : "";
  const env = await request<ApiEnvelope<unknown>>(
    `/orchestration/board-get${suffix}`,
  );
  return normalizeBoardPayload(unwrapEnvelope(env));
}

export interface OrchestrationBoardCardGetParams {
  request_id?: string;
  workspace_id?: string;
  issue_id: string;
}

export async function getOrchestrationBoardCard(
  params: OrchestrationBoardCardGetParams,
): Promise<OrchestrationCard> {
  const query = new URLSearchParams();
  if (params.request_id) query.set("request_id", params.request_id);
  if (params.workspace_id) query.set("workspace_id", params.workspace_id);
  query.set("issue_id", params.issue_id);

  const env = await request<ApiEnvelope<{ card: OrchestrationCard }>>(
    `/orchestration/board-card-get?${query.toString()}`,
  );
  const data = unwrapEnvelope(env);
  if (!data?.card) {
    throw new Error("Missing card payload");
  }
  return data.card;
}

export interface OrchestrationBoardCardRuntimeGetParams {
  request_id?: string;
  workspace_id?: string;
  issue_id: string;
  depth?: number;
}

export async function getOrchestrationBoardCardRuntime(
  params: OrchestrationBoardCardRuntimeGetParams,
): Promise<OrchestrationBoardCardRuntimeResult> {
  const query = new URLSearchParams();
  if (params.request_id) query.set("request_id", params.request_id);
  if (params.workspace_id) query.set("workspace_id", params.workspace_id);
  query.set("issue_id", params.issue_id);
  if (typeof params.depth === "number" && Number.isFinite(params.depth)) {
    query.set("depth", String(params.depth));
  }

  const env = await request<ApiEnvelope<OrchestrationBoardCardRuntimeResult>>(
    `/orchestration/board-card-runtime-get?${query.toString()}`,
  );
  return unwrapEnvelope(env);
}

export async function applyOrchestrationCardAction(params: {
  request_id: string;
  workspace_id?: string;
  issue_id: string;
  action: OrchestrationCardAction;
}): Promise<OrchestrationCardActionResult> {
  const env = await request<ApiEnvelope<OrchestrationCardActionResult>>(
    "/orchestration/card-action",
    {
      method: "POST",
      body: JSON.stringify(params),
    },
  );
  return unwrapEnvelope(env);
}

export async function refreshOrchestration(params: {
  request_id: string;
  workspace_id?: string;
}): Promise<OrchestrationRefreshResult> {
  const env = await request<ApiEnvelope<OrchestrationRefreshResult>>(
    "/orchestration/refresh",
    {
      method: "POST",
      body: JSON.stringify(params),
    },
  );
  return unwrapEnvelope(env);
}

export async function seedOrchestrationCards(params: {
  request_id: string;
  workspace_id?: string;
  cards: OrchestrationSeedCardInput[];
}): Promise<OrchestrationSeedCardsResult> {
  const env = await request<ApiEnvelope<OrchestrationSeedCardsResult>>(
    "/orchestration/seed-cards",
    {
      method: "POST",
      body: JSON.stringify(params),
    },
  );
  return unwrapEnvelope(env);
}

export async function cleanupOrchestrationCards(params: {
  request_id: string;
  workspace_id: string;
  issue_ids?: string[];
}): Promise<{
  request_id: string;
  deleted_cards: number;
  deleted_events: number;
  ts: string;
}> {
  const env = await request<
    ApiEnvelope<{
      request_id: string;
      deleted_cards: number;
      deleted_events: number;
      ts: string;
    }>
  >("/orchestration/cleanup-cards", {
    method: "POST",
    body: JSON.stringify(params),
  });
  return unwrapEnvelope(env);
}

export async function archiveOrchestrationCards(params: {
  request_id: string;
  workspace_id: string;
  issue_ids?: string[];
}): Promise<{
  request_id: string;
  updated: number;
  action: string;
  ts: string;
}> {
  const env = await request<
    ApiEnvelope<{
      request_id: string;
      updated: number;
      action: string;
      ts: string;
    }>
  >("/orchestration/archive-cards", {
    method: "POST",
    body: JSON.stringify(params),
  });
  return unwrapEnvelope(env);
}

export async function restoreOrchestrationCards(params: {
  request_id: string;
  workspace_id: string;
  issue_ids?: string[];
}): Promise<{
  request_id: string;
  updated: number;
  action: string;
  ts: string;
}> {
  const env = await request<
    ApiEnvelope<{
      request_id: string;
      updated: number;
      action: string;
      ts: string;
    }>
  >("/orchestration/restore-cards", {
    method: "POST",
    body: JSON.stringify(params),
  });
  return unwrapEnvelope(env);
}

export async function getContextOverview(params: {
  workspace: string;
  vault_path?: string;
  limit?: number;
  maintenance_limit?: number;
}): Promise<ACAOverview> {
  const query = new URLSearchParams();
  query.set("workspace", params.workspace);
  if (params.vault_path) query.set("vault_path", params.vault_path);
  if (typeof params.limit === "number" && Number.isFinite(params.limit)) {
    query.set("limit", String(params.limit));
  }
  if (
    typeof params.maintenance_limit === "number" &&
    Number.isFinite(params.maintenance_limit)
  ) {
    query.set("maintenance_limit", String(params.maintenance_limit));
  }
  const env = await request<ApiEnvelope<ACAOverview>>(
    `/context/overview?${query.toString()}`,
  );
  return unwrapEnvelope(env);
}

export async function getContextNextProposalMerge(params: {
  workspace: string;
  vault_path?: string;
  limit?: number;
  claim?: boolean;
}): Promise<ACANextProposalMergeResult> {
  if (params.claim) {
    const env = await request<ApiEnvelope<ACANextProposalMergeResult>>(
      "/context/next-proposal-merge/claim",
      {
        method: "POST",
        body: JSON.stringify({
          workspace: params.workspace,
          vault_path: params.vault_path,
          limit: params.limit,
        }),
      },
    );
    return unwrapEnvelope(env);
  }
  const query = new URLSearchParams();
  query.set("workspace", params.workspace);
  if (params.vault_path) query.set("vault_path", params.vault_path);
  if (typeof params.limit === "number" && Number.isFinite(params.limit)) {
    query.set("limit", String(params.limit));
  }
  const env = await request<ApiEnvelope<ACANextProposalMergeResult>>(
    `/context/next-proposal-merge?${query.toString()}`,
  );
  return unwrapEnvelope(env);
}

export async function releaseContextProposalMerge(params: {
  workspace: string;
  proposal_id: string;
}): Promise<{ workspace_path: string; proposal: ACAMemoryProposal }> {
  const env = await request<
    ApiEnvelope<{ workspace_path: string; proposal: ACAMemoryProposal }>
  >(`/context/proposals/${params.proposal_id}/release-merge`, {
    method: "POST",
    body: JSON.stringify({
      workspace: params.workspace,
    }),
  });
  return unwrapEnvelope(env);
}

export async function mergeContextProposal(params: {
  workspace: string;
  proposal_id: string;
  vault_path?: string;
  vault_name?: string;
  draft_path?: string;
  target_path?: string;
  heading?: string;
}): Promise<{
  workspace_path: string;
  vault_path: string;
  proposal: ACAMemoryProposal;
  merge: Record<string, unknown>;
  work_packet: Record<string, unknown>;
}> {
  const env = await request<
    ApiEnvelope<{
      workspace_path: string;
      vault_path: string;
      proposal: ACAMemoryProposal;
      merge: Record<string, unknown>;
      work_packet: Record<string, unknown>;
    }>
  >(`/context/proposals/${params.proposal_id}/merge`, {
    method: "POST",
    body: JSON.stringify({
      workspace: params.workspace,
      vault_path: params.vault_path,
      vault_name: params.vault_name,
      draft_path: params.draft_path,
      target_path: params.target_path,
      heading: params.heading,
    }),
  });
  return unwrapEnvelope(env);
}

/**
 * Retrieve a list of agents from the server.
 *
 * @param limit - Maximum number of agents to return (default: 100)
 * @returns An `AgentsListResponse` containing the array of agents and the total count
 */
export async function listAgents(limit = 100): Promise<AgentsListResponse> {
  return request<AgentsListResponse>(`/agents?limit=${limit}`);
}

/**
 * Fetches a single agent by ID.
 *
 * @param id - The agent's identifier.
 * @returns The object containing the requested `agent`.
 */
export async function getAgent(id: string) {
  return request<{ agent: AgentsListResponse["agents"][0] }>(`/agents/${id}`);
}

export async function getAgentRuntime(
  id: string,
  params: { depth?: number } = {},
): Promise<{ runtime: AgentRuntimeTree }> {
  const query = new URLSearchParams();
  if (typeof params.depth === "number" && Number.isFinite(params.depth)) {
    query.set("depth", String(params.depth));
  }
  const suffix = query.size > 0 ? `?${query.toString()}` : "";
  return request<{ runtime: AgentRuntimeTree }>(
    `/agents/${id}/runtime${suffix}`,
  );
}

export interface SpawnAgentParams {
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
  memory_scope?: "agent" | "session";
  memory_retention?: "companion" | "durable" | "task" | "ephemeral";
  room_id?: string;
  room_role?: string;
  // Agent metadata
  name?: string; // Human name (auto-generated if empty)
  slug?: string; // Human-readable handle
  // Execution config
  exec_mode?: "reactive" | "autonomous" | "proactive" | "tick" | "story";
  think_interval?: number;
  max_iterations?: number;
  max_context_tokens?: number;
  max_auto_turns?: number;
  // LLM override
  llm_provider?: string;
  llm_model?: string;
}

/**
 * Spawn a new agent using the provided configuration parameters.
 *
 * @param params - Parameters for the agent to create (e.g., role, prompt, workspace_id, allowed skills, metadata, execution configuration, and LLM overrides)
 * @returns The `AgentSpawnResponse` containing details about the spawned agent and the operation status
 */
export async function spawnAgent(
  params: SpawnAgentParams,
): Promise<AgentSpawnResponse> {
  return request<AgentSpawnResponse>("/agents/spawn", {
    method: "POST",
    body: JSON.stringify(params),
  });
}

/**
 * Delete an agent identified by its ID.
 *
 * @returns An object containing `status` describing the outcome and `agent_id` with the id of the deleted agent.
 */
export async function trashAgent(
  agentId: string,
): Promise<{ status: string; agent_id: string }> {
  return request(`/agents/${agentId}`, {
    method: "DELETE",
  });
}

/**
 * Sends a kill command to the daemon managing the specified agent.
 *
 * @param agentId - The ID of the agent to kill
 * @returns `ok` indicates whether the kill request succeeded, `session_id` is the daemon session identifier, `status` is the resulting agent status, and `message` contains server-provided details
 */
export async function killAgent(agentId: string): Promise<{
  ok: boolean;
  session_id: string;
  status: string;
  message: string;
}> {
  return request(`/agents/${agentId}/daemon/kill`, {
    method: "POST",
  });
}

/**
 * Start an agent's daemon session.
 *
 * @param agentId - The ID of the agent to start
 * @param params - Optional start parameters:
 *   - `prompt`: initial prompt to provide to the agent
 *   - `workspace`: workspace identifier to run the agent in
 * @returns An object containing the started session's `session_id`, `actor_id`, and `status`
 */
export async function startAgent(
  agentId: string,
  params?: { prompt?: string; workspace?: string },
): Promise<{
  session_id: string;
  actor_id: string;
  status: string;
}> {
  return request(`/agents/${agentId}/daemon/start`, {
    method: "POST",
    body: JSON.stringify(params || {}),
  });
}

export interface PatchAgentParams {
  conversation_id?: string; // empty string to unlink
  memory_scope?: "agent" | "session";
  memory_retention?: "companion" | "durable" | "task" | "ephemeral";
}

/**
 * Update properties of an existing agent.
 *
 * @param agentId - The ID of the agent to update
 * @param params - Fields to update on the agent. If `conversation_id` is provided as an empty string, the agent will be unlinked from its conversation.
 * @returns An object with an `agent` property containing the updated agent
 */
export async function patchAgent(
  agentId: string,
  params: PatchAgentParams,
): Promise<{ agent: AgentsListResponse["agents"][0] }> {
  return request(`/agents/${agentId}`, {
    method: "PATCH",
    body: JSON.stringify(params),
  });
}

export async function askAgentStream(
  agentId: string,
  params: {
    message: string;
    correlation_id?: string;
    conversation_id?: string;
    context?: Record<string, unknown>;
    response_schema?: Record<string, unknown>;
    response_keys?: string[];
  },
): Promise<{
  accepted: boolean;
  agent_id: string;
  correlation_id: string;
  conversation_id: string;
}> {
  return request(`/agents/${agentId}/ask-stream`, {
    method: "POST",
    body: JSON.stringify(params),
  });
}

export async function cancelAgentStream(
  agentId: string,
  correlationId?: string,
): Promise<{
  ok: boolean;
  agent_id: string;
  correlation_id?: string;
  cancelled: number;
}> {
  return request(`/agents/${agentId}/ask-stream/cancel`, {
    method: "POST",
    body: JSON.stringify({ correlation_id: correlationId }),
  });
}

export async function compressAgentMemory(
  agentId: string,
  params: {
    conversation_id?: string;
    distill?: boolean;
  } = {},
): Promise<{
  conversation_id: string;
  summarized: number;
  skipped: number;
  distilled: boolean;
  processed_dates?: string[];
  policy: {
    memory_scope: string;
    memory_retention: string;
    default_distill: boolean;
  };
}> {
  return request(`/agents/${agentId}/memory/compress`, {
    method: "POST",
    body: JSON.stringify(params),
  });
}

/**
 * Fetches daemon sessions for the specified agent.
 *
 * @param agentId - The agent's identifier.
 * @returns An object containing `sessions`, an array of `AgentSession` objects, and `count`, the total number of sessions.
 */
export async function listAgentSessions(agentId: string): Promise<{
  sessions: AgentSession[];
  count: number;
}> {
  return request(`/agents/${agentId}/daemon/sessions`);
}

/**
 * Fetches mailbox messages for a workspace with optional actor filtering, unread-only filtering, and result limiting.
 *
 * @param params - Query parameters:
 *   - `workspace_id`: the workspace to query (required)
 *   - `actor_id`: optional actor to filter messages for
 *   - `only_unread`: if `true`, return only unread messages
 *   - `limit`: maximum number of messages to return
 * @returns MailboxListResponse containing the matching messages and metadata (e.g., count)
 */
export async function listMailbox(params: {
  workspace_id: string;
  actor_id?: string;
  only_unread?: boolean;
  limit?: number;
}): Promise<MailboxListResponse> {
  const query = new URLSearchParams();
  query.set("workspace_id", params.workspace_id);
  if (params.actor_id) query.set("actor_id", params.actor_id);
  if (params.only_unread) query.set("only_unread", "true");
  if (params.limit) query.set("limit", String(params.limit));

  return request<MailboxListResponse>(`/mailbox?${query}`);
}

/**
 * Sends a mailbox message within a workspace.
 *
 * @param params - Message payload containing `workspace_id`, `sender`, `recipient`, `subject`, and `body`; optional `kind` and `priority` may be provided
 * @returns The created message `id` and its `status`
 */
export async function sendMessage(params: {
  workspace_id: string;
  sender: string;
  recipient: string;
  subject: string;
  body: string;
  kind?: string;
  priority?: number;
}): Promise<{ id: string; status: string }> {
  return request("/mailbox", {
    method: "POST",
    body: JSON.stringify(params),
  });
}

export async function listRooms(params: {
  workspace_id: string;
  actor_id?: string;
  limit?: number;
  archived_only?: boolean;
}): Promise<{ rooms: Room[]; count: number }> {
  const query = new URLSearchParams();
  query.set("workspace_id", params.workspace_id);
  if (params.actor_id) query.set("actor_id", params.actor_id);
  if (params.limit) query.set("limit", String(params.limit));
  if (params.archived_only) query.set("archived_only", "true");

  return request<{ rooms: Room[]; count: number }>(`/rooms?${query}`);
}

export async function createRoom(params: {
  workspace_id: string;
  id?: string;
  title: string;
  description?: string;
  dispatch_policy?: "all_subtree" | "children_only" | "lead_only" | "selected";
  dispatch_agent_ids?: string[];
  members?: Array<{ actor_id: string; role?: string }>;
}): Promise<{ room: Room }> {
  return request<{ room: Room }>("/rooms", {
    method: "POST",
    body: JSON.stringify(params),
  });
}

export async function getRoom(
  roomId: string,
  params: { workspace_id: string; actor_id?: string },
): Promise<{ room: Room }> {
  const query = new URLSearchParams();
  query.set("workspace_id", params.workspace_id);
  if (params.actor_id) query.set("actor_id", params.actor_id);

  return request<{ room: Room }>(
    `/rooms/${encodeURIComponent(roomId)}?${query}`,
  );
}

export async function patchRoom(
  roomId: string,
  params: {
    workspace_id: string;
    title?: string;
    description?: string;
    dispatch_policy?:
      | "all_subtree"
      | "children_only"
      | "lead_only"
      | "selected";
    dispatch_agent_ids?: string[];
  },
): Promise<{ room: Room }> {
  const query = new URLSearchParams();
  query.set("workspace_id", params.workspace_id);

  return request<{ room: Room }>(
    `/rooms/${encodeURIComponent(roomId)}?${query}`,
    {
      method: "PATCH",
      body: JSON.stringify({
        title: params.title,
        description: params.description,
        dispatch_policy: params.dispatch_policy,
        dispatch_agent_ids: params.dispatch_agent_ids,
      }),
    },
  );
}

export async function deleteRoom(
  roomId: string,
  params: { workspace_id: string },
): Promise<{ status: string; room_id: string; workspace_id: string }> {
  const query = new URLSearchParams();
  query.set("workspace_id", params.workspace_id);

  return request<{ status: string; room_id: string; workspace_id: string }>(
    `/rooms/${encodeURIComponent(roomId)}?${query}`,
    {
      method: "DELETE",
    },
  );
}

export async function archiveRoom(
  roomId: string,
  params: { workspace_id: string },
): Promise<{ status: string; room_id: string; workspace_id: string }> {
  const query = new URLSearchParams();
  query.set("workspace_id", params.workspace_id);
  return request(`/rooms/${encodeURIComponent(roomId)}/archive?${query}`, {
    method: "POST",
  });
}

export async function restoreRoom(
  roomId: string,
  params: { workspace_id: string },
): Promise<{ status: string; room_id: string; workspace_id: string }> {
  const query = new URLSearchParams();
  query.set("workspace_id", params.workspace_id);
  return request(`/rooms/${encodeURIComponent(roomId)}/restore?${query}`, {
    method: "POST",
  });
}

export async function patchRoomMembers(
  roomId: string,
  params: {
    workspace_id: string;
    members: Array<{ actor_id: string; role?: string }>;
  },
): Promise<{ room: Room }> {
  const query = new URLSearchParams();
  query.set("workspace_id", params.workspace_id);

  return request<{ room: Room }>(
    `/rooms/${encodeURIComponent(roomId)}/members?${query}`,
    {
      method: "PATCH",
      body: JSON.stringify({ members: params.members }),
    },
  );
}

export async function listRoomMessages(
  roomId: string,
  params: { workspace_id: string; limit?: number },
): Promise<{
  room_id: string;
  stream: string;
  messages: MailboxListResponse["messages"];
  count: number;
}> {
  const query = new URLSearchParams();
  query.set("workspace_id", params.workspace_id);
  if (params.limit) query.set("limit", String(params.limit));

  return request<{
    room_id: string;
    stream: string;
    messages: MailboxListResponse["messages"];
    count: number;
  }>(`/rooms/${encodeURIComponent(roomId)}/messages?${query}`);
}

export async function updateRoomMemberBinding(
  roomId: string,
  actorId: string,
  params: {
    workspace_id: string;
    backend?: string;
    session?: string;
    pane_id?: string;
    unbound?: boolean;
    transport_endpoint?: string;
    transport_kind?: string;
  },
): Promise<{ member?: RoomMember }> {
  return request(`/rooms/${encodeURIComponent(roomId)}/members/${encodeURIComponent(actorId)}/binding?workspace_id=${encodeURIComponent(params.workspace_id)}`, {
    method: "PUT",
    body: JSON.stringify({
      backend: params.backend,
      session: params.session,
      pane_id: params.pane_id,
      unbound: params.unbound,
      transport_endpoint: params.transport_endpoint,
      transport_kind: params.transport_kind,
    }),
  });
}

export async function sendRoomMessage(
  roomId: string,
  params: {
    workspace_id: string;
    sender: string;
    body: string;
    recipient?: string;
    subject?: string;
    kind?: string;
    priority?: number;
    ack_required?: boolean;
    reply_expected?: boolean;
    interrupt?: boolean;
    related_message_id?: string;
    task_id?: string;
    dispatch_agents?: boolean;
    dispatch_agent_ids?: string[];
    context?: Record<string, unknown>;
  },
): Promise<RoomSendMessageResult> {
  return request(`/rooms/${encodeURIComponent(roomId)}/messages`, {
    method: "POST",
    body: JSON.stringify(params),
  });
}

export async function listRoomReminders(
  roomId: string,
  params: { workspace_id: string; all?: boolean },
): Promise<{ room_id: string; count: number; reminders: RoomReminder[] }> {
  const query = new URLSearchParams()
  query.set("workspace_id", params.workspace_id)
  if (params.all) query.set("all", String(params.all))
  return request(`/rooms/${encodeURIComponent(roomId)}/reminders?${query}`)
}

export async function addRoomReminder(
  roomId: string,
  params: {
    workspace_id: string
    sender: string
    recipient: string
    subject?: string
    body: string
    every: string
    max_iterations?: number
    ack_required?: boolean
    reply_expected?: boolean
    interrupt?: boolean
    allow_passive?: boolean
  },
): Promise<{ room_id: string; message: MailboxMessage; reminder: RoomReminder; live_relay?: RoomSendMessageResult["live_relay"] }> {
  return request(`/rooms/${encodeURIComponent(roomId)}/reminders`, {
    method: "POST",
    body: JSON.stringify(params),
  })
}

export async function cancelRoomReminder(
  roomId: string,
  reminderId: string,
  params: { workspace_id: string; actor?: string },
): Promise<{ room_id: string; cancelled: boolean; reminder: RoomReminder }> {
  return request(`/rooms/${encodeURIComponent(roomId)}/reminders/${encodeURIComponent(reminderId)}/cancel?workspace_id=${encodeURIComponent(params.workspace_id)}`, {
    method: "POST",
    body: JSON.stringify({ actor: params.actor }),
  })
}

export async function getRoomStatus(
  roomId: string,
  params: {
    workspace_id: string;
    actor_id?: string;
    only?: string;
    verbose?: boolean;
    limit?: number;
  },
): Promise<RoomStatus> {
  const query = new URLSearchParams();
  query.set("workspace_id", params.workspace_id);
  if (params.actor_id) query.set("actor_id", params.actor_id);
  if (params.only) query.set("only", params.only);
  if (params.verbose) query.set("verbose", String(params.verbose));
  if (params.limit) query.set("limit", String(params.limit));

  const response = await request<{
    room: RoomStatus["room"];
    coordinator_actor_id?: string;
    participants: RoomStatus["participants"];
    task_pulse: RoomStatus["task_pulse"];
    actionable_backlog?: RoomStatus["actionable_backlog"];
    action_required?: {
      pending_acks?: number;
      pending_replies?: number;
      stale_tasks?: number;
      blocked_tasks?: number;
    };
  }>(
    `/rooms/${encodeURIComponent(roomId)}/status?${query}`,
  );
  return {
    room: response.room,
    coordinator_actor_id: response.coordinator_actor_id,
    participants: response.participants,
    task_pulse: response.task_pulse,
    actionable_backlog: response.actionable_backlog ?? {
      pending_acks: response.action_required?.pending_acks ?? 0,
      pending_replies: response.action_required?.pending_replies ?? 0,
      stale_tasks: response.action_required?.stale_tasks ?? 0,
      blocked_tasks: response.action_required?.blocked_tasks ?? 0,
    },
  };
}

export async function getRoomInbox(
  roomId: string,
  params: {
    workspace_id: string;
    actor_id: string;
    only?: string;
    limit?: number;
  },
): Promise<RoomInbox> {
  const query = new URLSearchParams();
  query.set("workspace_id", params.workspace_id);
  query.set("actor_id", params.actor_id);
  if (params.only) query.set("only", params.only);
  if (params.limit) query.set("limit", String(params.limit));

  return request<RoomInbox>(
    `/rooms/${encodeURIComponent(roomId)}/inbox?${query}`,
  );
}

export async function getRoomTasks(
  roomId: string,
  params: {
    workspace_id: string;
    stale_after?: string;
    include_completed?: boolean;
  },
): Promise<RoomTask[]> {
  const query = new URLSearchParams();
  query.set("workspace_id", params.workspace_id);
  if (params.stale_after) query.set("stale_after", params.stale_after);
  if (params.include_completed)
    query.set("include_completed", String(params.include_completed));

  const response = await request<{ tasks?: RoomTask[] }>(
    `/rooms/${encodeURIComponent(roomId)}/tasks?${query}`,
  );
  return response.tasks ?? [];
}

export async function getRoomLoop(
  roomId: string,
  workspaceId: string,
  actorId: string,
): Promise<RoomLoop> {
  const query = new URLSearchParams();
  query.set("workspace_id", workspaceId);
  query.set("actor_id", actorId);
  const response = await request<{ loop?: RoomLoop }>(
    `/rooms/${encodeURIComponent(roomId)}/loop?${query}`,
  );
  if (!response.loop) {
    throw new Error("Room loop payload missing");
  }
  return response.loop;
}

export async function listMuxPanes(
  backend: "tmux" | "zellij",
  params?: { session?: string },
): Promise<MuxPane[]> {
  const query = new URLSearchParams();
  query.set("backend", backend);
  if (params?.session) query.set("session", params.session);
  const response = await request<{ panes?: MuxPane[] }>(`/mux/panes?${query}`);
  return response.panes ?? [];
}

export async function readMuxPane(
  target: string,
  params?: { backend?: "tmux" | "zellij"; lines?: number },
): Promise<MuxPaneCapture> {
  const query = new URLSearchParams()
  query.set("target", target)
  query.set("backend", params?.backend || "tmux")
  if (typeof params?.lines === "number" && params.lines > 0) {
    query.set("lines", String(params.lines))
  }
  const response = await request<{ capture?: MuxPaneCapture }>(`/mux/read?${query}`)
  if (!response.capture) {
    throw new Error("Mux pane capture missing")
  }
  return response.capture
}

export async function ackRoomMessage(
  roomId: string,
  messageId: string,
  params: { workspace_id: string; actor?: string },
): Promise<void> {
  await request(
    `/rooms/${encodeURIComponent(roomId)}/messages/${messageId}/ack`,
    {
      method: "POST",
      body: JSON.stringify(params),
    },
  );
}

export async function resolveRoomMessage(
  roomId: string,
  messageId: string,
  params: { workspace_id: string; actor?: string },
): Promise<void> {
  await request(
    `/rooms/${encodeURIComponent(roomId)}/messages/${messageId}/resolve`,
    {
      method: "POST",
      body: JSON.stringify(params),
    },
  );
}

export async function bulkResolveRoomMessages(
  roomId: string,
  params: BulkResolveRequest,
): Promise<{ count: number }> {
  return request<{ count: number }>(
    `/rooms/${encodeURIComponent(roomId)}/messages/resolve`,
    {
      method: "POST",
      body: JSON.stringify(params),
    },
  );
}

export async function transferRoomCoordinator(
  roomId: string,
  params: { workspace_id: string; to: string; note?: string },
): Promise<void> {
  await request(`/rooms/${encodeURIComponent(roomId)}/coordinator`, {
    method: "POST",
    body: JSON.stringify(params),
  });
}

export async function claimRoomTask(
  roomId: string,
  taskId: string,
  params: { workspace_id: string; actor?: string },
): Promise<void> {
  await request(`/rooms/${encodeURIComponent(roomId)}/tasks/${taskId}/claim`, {
    method: "POST",
    body: JSON.stringify(params),
  });
}

export async function touchRoomTask(
  roomId: string,
  taskId: string,
  params: { workspace_id: string; actor?: string },
): Promise<void> {
  await request(`/rooms/${encodeURIComponent(roomId)}/tasks/${taskId}/touch`, {
    method: "POST",
    body: JSON.stringify(params),
  });
}

export async function blockRoomTask(
  roomId: string,
  taskId: string,
  params: { workspace_id: string; reason: string; actor?: string },
): Promise<void> {
  await request(`/rooms/${encodeURIComponent(roomId)}/tasks/${taskId}/block`, {
    method: "POST",
    body: JSON.stringify(params),
  });
}

export async function unblockRoomTask(
  roomId: string,
  taskId: string,
  params: { workspace_id: string; actor?: string },
): Promise<void> {
  await request(`/rooms/${encodeURIComponent(roomId)}/tasks/${taskId}/unblock`, {
    method: "POST",
    body: JSON.stringify(params),
  });
}

export async function completeRoomTask(
  roomId: string,
  taskId: string,
  params: { workspace_id: string; notes?: string; actor?: string },
): Promise<void> {
  await request(
    `/rooms/${encodeURIComponent(roomId)}/tasks/${taskId}/complete`,
    {
      method: "POST",
      body: JSON.stringify(params),
    },
  );
}

export async function abandonRoomTask(
  roomId: string,
  taskId: string,
  params: { workspace_id: string; actor?: string },
): Promise<void> {
  await request(
    `/rooms/${encodeURIComponent(roomId)}/tasks/${taskId}/abandon`,
    {
      method: "POST",
      body: JSON.stringify(params),
    },
  );
}

export async function reassignRoomTask(
  roomId: string,
  taskId: string,
  params: { workspace_id: string; to: string; reason?: string; actor?: string },
): Promise<void> {
  await request(
    `/rooms/${encodeURIComponent(roomId)}/tasks/${taskId}/reassign`,
    {
      method: "POST",
      body: JSON.stringify(params),
    },
  );
}

export async function reclaimRoomTask(
  roomId: string,
  taskId: string,
  params: { workspace_id: string; reason: string; actor?: string },
): Promise<void> {
  await request(
    `/rooms/${encodeURIComponent(roomId)}/tasks/${taskId}/reclaim`,
    {
      method: "POST",
      body: JSON.stringify(params),
    },
  );
}

export async function patchRoomLoop(
  roomId: string,
  params: { workspace_id: string; actor_id: string } & Partial<RoomLoop>,
): Promise<RoomLoop> {
  const response = await request<{ loop?: RoomLoop }>(`/rooms/${encodeURIComponent(roomId)}/loop`, {
    method: "PATCH",
    body: JSON.stringify(params),
  });
  if (!response.loop) {
    throw new Error("Room loop payload missing");
  }
  return response.loop;
}

/**
 * Fetches blackboard entries using optional filters.
 *
 * @param params - Filter and pagination options
 * @param params.ns - Namespace to filter entries by
 * @param params.topic - Topic to filter entries by
 * @param params.all - If `true`, include all entries (override default visibility)
 * @param params.limit - Maximum number of entries to return
 * @returns A BlackboardListResponse containing matching entries and associated metadata (for example, `count`)
 */
export async function listBlackboard(params: {
  ns?: string;
  topic?: string;
  all?: boolean;
  limit?: number;
}): Promise<BlackboardListResponse> {
  const query = new URLSearchParams();
  if (params.ns) query.set("ns", params.ns);
  if (params.topic) query.set("topic", params.topic);
  if (params.all) query.set("all", "true");
  if (params.limit) query.set("limit", String(params.limit));

  return request<BlackboardListResponse>(`/blackboard?${query}`);
}

/**
 * Posts an entry to the blackboard under a namespace and topic.
 *
 * @param params.ns - Optional namespace for the blackboard entry
 * @param params.topic - Topic name for the entry
 * @param params.payload - String payload to store in the blackboard entry
 * @param params.ttl_sec - Optional time-to-live in seconds for the entry
 * @returns An object with the created entry's `id` and the operation `status`
 */
export async function postBlackboard(params: {
  ns?: string;
  topic: string;
  payload: string;
  ttl_sec?: number;
}): Promise<{ id: string; status: string }> {
  return request("/blackboard", {
    method: "POST",
    body: JSON.stringify(params),
  });
}

/**
 * Delete a blackboard entry identified by `id`.
 *
 * @param id - The blackboard entry identifier to remove
 */
export async function deleteBlackboard(id: string): Promise<void> {
  await request(`/blackboard/${id}`, { method: "DELETE" });
}

/**
 * Fetches server logs filtered by the provided query parameters.
 *
 * @param params.limit - Maximum number of log entries to return.
 * @param params.since - ISO-8601 timestamp string; only include logs created at or after this time.
 * @param params.component - Filter logs by component name.
 * @param params.operation - Filter logs by operation identifier.
 * @param params.errors_only - If `true`, include only error entries.
 * @returns A LogsListResponse containing the matching log entries and a count.
 */
export async function getLogs(params: {
  limit?: number;
  since?: string;
  component?: string;
  operation?: string;
  errors_only?: boolean;
}): Promise<LogsListResponse> {
  const query = new URLSearchParams();
  if (params.limit) query.set("limit", String(params.limit));
  if (params.since) query.set("since", params.since);
  if (params.component) query.set("component", params.component);
  if (params.operation) query.set("operation", params.operation);
  if (params.errors_only) query.set("errors_only", "true");

  return request<LogsListResponse>(`/logs?${query}`);
}

export async function cleanupLogs(params: {
  component?: string;
  operation?: string;
  workspace?: string;
  errors_only?: boolean;
  text_query?: string;
  session_id?: string;
  trace_ids?: string[];
  dry_run?: boolean;
}): Promise<{
  status: string;
  deleted: number;
  kept: number;
  files: number;
  errors?: string[];
}> {
  return request(`/logs/cleanup`, {
    method: "POST",
    body: JSON.stringify(params),
  });
}

// Skills
export interface SkillParameter {
  name: string;
  type: string;
  required: boolean;
  description: string;
  enum?: string[];
  default?: unknown;
}

export interface Skill {
  name: string;
  version: string;
  description: string;
  tags: string[];
  command: string;
  parameters: SkillParameter[];
  returns: SkillParameter[];
}

export interface SkillsListResponse {
  skills: Skill[];
  count: number;
}

export interface SkillRunResponse {
  ok: boolean;
  skill: string;
  output?: unknown;
  error?: string;
  duration_ms: number;
}

/**
 * Retrieves the list of available skills from the server.
 *
 * @returns The skills list response containing `skills` and `count`.
 */
export async function listSkills(): Promise<SkillsListResponse> {
  return request<SkillsListResponse>("/skills");
}

/**
 * Execute a skill via the web API and return the skill runner result.
 */
export async function runSkill(
  skill: string,
  input: Record<string, unknown>,
): Promise<SkillRunResponse> {
  return request<SkillRunResponse>("/skills/run", {
    method: "POST",
    body: JSON.stringify({ skill, input }),
  });
}

export interface WorkspaceInfo {
  path: string;
  name: string;
  session_count: number;
  last_active?: string;
  is_active: boolean;
}

// Conversation / session settings
export interface ConversationSettings {
  conversation_id: string;
  tools_allow?: string[];
  llm_provider?: string;
  llm_model?: string;
  exec_mode?: "reactive" | "autonomous" | "proactive" | "tick" | "story" | string;
  story_gather_model?: string;
  story_dialogue_model?: string;
  presence_enabled?: boolean;
  updated_at?: string;
}

export interface ConversationSettingsPatch {
  // Pass an empty array to clear. Omit to leave unchanged.
  tools_allow?: string[];
  // Pass empty string to clear. Omit to leave unchanged.
  llm_provider?: string;
  llm_model?: string;
  // Pass empty string to clear. Omit to leave unchanged.
  exec_mode?: "" | "reactive" | "autonomous" | "proactive" | "tick" | "story";
  story_gather_model?: string;
  story_dialogue_model?: string;
  presence_enabled?: boolean;
}

export async function listWorkspaces(): Promise<{
  workspaces: WorkspaceInfo[];
  count: number;
  current: string;
}> {
  return request("/workspaces");
}

export async function switchWorkspace(path: string): Promise<{
  ok: boolean;
  workspace: string;
  name: string;
}> {
  return request("/workspaces/switch", {
    method: "POST",
    body: JSON.stringify({ path }),
  });
}

/**
 * Retrieves the service health status.
 *
 * @returns An object with `status` containing the current health state (for example, `"ok"`).
 */
export async function getHealth(): Promise<{ status: string }> {
  return request("/health");
}

/**
 * Fetch per-conversation settings for a companion conversation.
 */
export async function getCompanionConversationSettings(
  conversationId: string,
): Promise<{
  conversation_id: string;
  settings: ConversationSettings;
}> {
  return request(`/companion/conversations/${conversationId}/settings`);
}

/**
 * Patch per-conversation settings for a companion conversation.
 *
 * Note: Passing empty strings clears model/provider overrides. Passing an empty array clears tools allowlist.
 */
export async function patchCompanionConversationSettings(
  conversationId: string,
  patch: ConversationSettingsPatch,
): Promise<{
  conversation_id: string;
  settings: ConversationSettings;
}> {
  return request(`/companion/conversations/${conversationId}/settings`, {
    method: "PATCH",
    body: JSON.stringify(patch),
  });
}

/**
 * Delete all per-conversation settings for a companion conversation (reset to defaults).
 */
export async function deleteCompanionConversationSettings(
  conversationId: string,
): Promise<{ ok: boolean; message: string }> {
  return request(`/companion/conversations/${conversationId}/settings`, {
    method: "DELETE",
  });
}

/**
 * Fetch per-session settings for a console session.
 */
export async function getConsoleSessionSettings(sessionId: string): Promise<{
  session_id: string;
  settings: ConversationSettings;
}> {
  return request(`/console/sessions/${sessionId}/settings`);
}

/**
 * Patch per-session settings for a console session.
 */
export async function patchConsoleSessionSettings(
  sessionId: string,
  patch: ConversationSettingsPatch,
): Promise<{
  session_id: string;
  settings: ConversationSettings;
}> {
  return request(`/console/sessions/${sessionId}/settings`, {
    method: "PATCH",
    body: JSON.stringify(patch),
  });
}

/**
 * Delete all per-session settings for a console session (reset to defaults).
 */
export async function deleteConsoleSessionSettings(
  sessionId: string,
): Promise<{ ok: boolean; message: string }> {
  return request(`/console/sessions/${sessionId}/settings`, {
    method: "DELETE",
  });
}

// Console Sessions
export interface ConsoleSession {
  id: string;
  workspace: string;
  profile: string;
  created: string;
  last_activity?: string;
  message_count: number;
  client_count?: number;
}

export interface ConsoleMessage {
  id?: string;
  role: "user" | "assistant";
  content: string;
  // Server may return unix millis; UI may construct ISO strings.
  timestamp: string | number;
  correlation_id?: string;
  tool_calls?: ToolCall[];
  // Raw metadata returned by the console session backend (tool calls, injected contexts, etc.)
  metadata?: Record<string, unknown>;
  presence?: PresenceBundle;
}

export interface ToolCall {
  name: string;
  input?: Record<string, unknown>;
  output?: string;
  status: "pending" | "completed" | "error";
}

interface RawConsoleMessage {
  id?: string;
  role: string;
  content: string;
  timestamp: string | number;
  correlation_id?: string;
  tool_calls?: unknown;
  metadata?: Record<string, unknown>;
  presence?: unknown;
}

function normalizeConsoleMessage(msg: RawConsoleMessage): ConsoleMessage {
  return {
    id: msg.id,
    role: msg.role === "user" ? "user" : "assistant",
    content: msg.content,
    timestamp: msg.timestamp,
    correlation_id: msg.correlation_id,
    tool_calls: extractToolCalls(msg),
    metadata: msg.metadata,
    presence: extractPresence(msg),
  };
}

function extractPresence(msg: RawConsoleMessage): PresenceBundle | undefined {
  const candidate = msg.presence ?? msg.metadata?.["presence"];
  if (!candidate || typeof candidate !== "object" || Array.isArray(candidate)) {
    return undefined;
  }
  return candidate as PresenceBundle;
}

function extractToolCalls(msg: RawConsoleMessage): ToolCall[] | undefined {
  const meta = msg.metadata;
  const metaToolCalls = meta?.["tool_calls"];
  const rawToolCalls = Array.isArray(metaToolCalls)
    ? metaToolCalls
    : Array.isArray(msg.tool_calls)
      ? msg.tool_calls
      : null;
  if (!rawToolCalls || rawToolCalls.length === 0) return undefined;

  const normalized: ToolCall[] = [];
  for (const tc of rawToolCalls) {
    if (!tc || typeof tc !== "object") continue;
    const tco = tc as Record<string, unknown>;

    const name =
      typeof tco.name === "string"
        ? tco.name
        : typeof tco.tool === "string"
          ? tco.tool
          : "";
    if (!name) continue;

    const args = tco.arguments ?? tco.input;
    let input: Record<string, unknown> | undefined;
    if (args && typeof args === "object" && !Array.isArray(args)) {
      input = args as Record<string, unknown>;
    }

    const output =
      typeof tco.result === "string"
        ? tco.result
        : typeof tco.output === "string"
          ? tco.output
          : undefined;
    const isError = Boolean(tco.is_error ?? tco.error ?? false);

    normalized.push({
      name,
      input,
      output,
      status: isError ? "error" : output ? "completed" : "pending",
    });
  }

  return normalized.length > 0 ? normalized : undefined;
}

/**
 * Fetches console sessions, optionally filtered by workspace.
 *
 * @param workspace - Optional workspace identifier to filter returned sessions
 * @returns An object containing `sessions` (array of ConsoleSession) and `count` (total number of sessions matching the query)
 */
export async function listConsoleSessions(workspace?: string): Promise<{
  sessions: ConsoleSession[];
  count: number;
}> {
  const query = workspace ? `?workspace=${encodeURIComponent(workspace)}` : "";
  return request(`/console/sessions${query}`);
}

/**
 * Create a new console session.
 *
 * @param params - Parameters for the new session
 * @param params.workspace - Optional workspace identifier to associate with the session
 * @param params.profile - Optional profile name to use for the session
 * @param params.system_prompt - Optional system prompt to seed the session
 * @param params.llm_provider - Optional provider override (also persisted as a session default)
 * @param params.llm_model - Optional model override (also persisted as a session default)
 * @param params.tools_allow - Optional allowlist of engine tool names (persisted as a session default)
 * @param params.exec_mode - Optional exec mode default (persisted)
 * @param params.story_gather_model - Optional story gather model default (persisted)
 * @param params.story_dialogue_model - Optional story dialogue model default (persisted)
 * @param params.tool_model - Deprecated. When set, maps to `story_gather_model` and implies openrouter.
 * @param params.response_model - Deprecated. When set, maps to `llm_model` and `story_dialogue_model` and implies openrouter.
 * @returns The created `session`
 */
export async function createConsoleSession(params: {
  workspace?: string;
  profile?: string;
  system_prompt?: string;
  llm_provider?: string;
  llm_model?: string;
  tools_allow?: string[];
  exec_mode?: "reactive" | "autonomous" | "proactive" | "tick" | "story";
  story_gather_model?: string;
  story_dialogue_model?: string;
  // Deprecated/back-compat fields
  conversation_id?: string;
  tool_model?: string;
  response_model?: string;
}): Promise<{ session: ConsoleSession }> {
  const body: Record<string, unknown> = {
    workspace: params.workspace,
    profile: params.profile,
    system_prompt: params.system_prompt,
  };

  // Preferred: explicit provider/model.
  if (params.llm_provider !== undefined)
    body.llm_provider = params.llm_provider;
  if (params.llm_model !== undefined) body.llm_model = params.llm_model;

  // Preferred: tool allowlist + exec mode.
  if (params.tools_allow !== undefined) body.tools_allow = params.tools_allow;
  if (params.exec_mode !== undefined) body.exec_mode = params.exec_mode;

  // Preferred: story defaults.
  if (params.story_gather_model !== undefined)
    body.story_gather_model = params.story_gather_model;
  if (params.story_dialogue_model !== undefined)
    body.story_dialogue_model = params.story_dialogue_model;

  // Back-compat: 2-stage model fields. Console sessions currently support a single model;
  // we map the "response model" to `llm_model` and default provider to openrouter.
  const legacyToolModel =
    typeof params.tool_model === "string" ? params.tool_model.trim() : "";
  const legacyResponseModel =
    typeof params.response_model === "string"
      ? params.response_model.trim()
      : "";
  if (
    params.llm_provider === undefined &&
    (legacyToolModel || legacyResponseModel)
  ) {
    body.llm_provider = "openrouter";
  }
  if (params.llm_model === undefined && legacyResponseModel) {
    body.llm_model = legacyResponseModel;
  }
  if (params.story_gather_model === undefined && legacyToolModel) {
    body.story_gather_model = legacyToolModel;
  }
  if (params.story_dialogue_model === undefined && legacyResponseModel) {
    body.story_dialogue_model = legacyResponseModel;
  }

  return request("/console/sessions", {
    method: "POST",
    body: JSON.stringify(body),
  });
}

/**
 * Fetches a console session by ID and returns its metadata, messages, and inflight state.
 *
 * @param sessionId - The ID of the console session to retrieve
 * @returns An object containing `session` (the session metadata), `messages` (array of console messages), and `inflight` (`true` when the session has active inflight activity, `false` otherwise)
 */
export async function getConsoleSession(sessionId: string): Promise<{
  session: ConsoleSession;
  messages: ConsoleMessage[];
  inflight: boolean;
}> {
  const data = await request<{
    session: ConsoleSession;
    messages: RawConsoleMessage[];
    inflight: string;
  }>(`/console/sessions/${sessionId}`);

  return {
    session: data.session,
    messages: (data.messages || []).map(normalizeConsoleMessage),
    inflight: Boolean(data.inflight),
  };
}

/**
 * Delete a console session by its ID.
 */
export async function deleteConsoleSession(sessionId: string): Promise<void> {
  await request(`/console/sessions/${sessionId}`, { method: "DELETE" });
}

/**
 * Submits content to a console session for processing.
 *
 * @param correlationId - Optional client-provided correlation identifier to track the message across requests
 * @param overrides - Optional per-turn LLM overrides.
 * @returns `{ ok: boolean; correlation_id: string }` where `ok` indicates the request was accepted and `correlation_id` is the identifier for the submitted message
 */
export async function askConsoleSession(
  sessionId: string,
  content: string,
  correlationId?: string,
  overrides?: {
    llm_provider?: string;
    llm_model?: string;
    tool_model?: string;
    response_model?: string;
  },
): Promise<{ ok: boolean; correlation_id: string }> {
  const body: Record<string, unknown> = {
    content,
    correlation_id: correlationId,
  };

  if (overrides) {
    if (overrides.llm_provider !== undefined)
      body.llm_provider = overrides.llm_provider;
    if (overrides.llm_model !== undefined) body.llm_model = overrides.llm_model;

    const legacyToolModel =
      typeof overrides.tool_model === "string"
        ? overrides.tool_model.trim()
        : "";
    const legacyResponseModel =
      typeof overrides.response_model === "string"
        ? overrides.response_model.trim()
        : "";
    if (
      overrides.llm_provider === undefined &&
      (legacyToolModel || legacyResponseModel)
    ) {
      body.llm_provider = "openrouter";
    }
    if (overrides.llm_model === undefined && legacyResponseModel) {
      body.llm_model = legacyResponseModel;
    }
  }

  return request(`/console/sessions/${sessionId}/ask`, {
    method: "POST",
    body: JSON.stringify(body),
  });
}

/**
 * Cancel an in-flight console session request or all in-flight requests for the given session.
 *
 * @param sessionId - The console session identifier to target for cancellation
 * @param correlationId - Optional correlation identifier of a specific request to cancel; omit to cancel all in-flight requests for the session
 * @returns `ok` is true if the server accepted the cancellation request, `false` otherwise
 */
export async function cancelConsoleSession(
  sessionId: string,
  correlationId?: string,
): Promise<{ ok: boolean }> {
  return request(`/console/sessions/${sessionId}/cancel`, {
    method: "POST",
    body: JSON.stringify({ correlation_id: correlationId }),
  });
}

/**
 * Fetches messages for a console session.
 *
 * @param sessionId - The ID of the console session to retrieve messages from
 * @returns An object with `messages` — an array of ConsoleMessage entries, and `count` — the total number of messages
 */
export async function getConsoleMessages(sessionId: string): Promise<{
  messages: ConsoleMessage[];
  count: number;
}> {
  const data = await request<{
    messages: RawConsoleMessage[];
    count: number;
  }>(`/console/sessions/${sessionId}/messages`);

  return {
    messages: (data.messages || []).map(normalizeConsoleMessage),
    count: data.count,
  };
}

// Provider Availability

export interface ProviderAvailability {
  id: string;
  available: boolean;
  configured?: boolean;
  reachable?: boolean;
  message?: string;
  base_url?: string;
  models?: { id: string; name: string }[];
}

export interface ProvidersResponse {
  ok: boolean;
  providers: ProviderAvailability[];
  default_provider: string;
  voyage_available: boolean;
}

/**
 * Fetches which LLM providers have API keys configured on the server.
 *
 * @returns A ProvidersResponse with availability info per provider, the default provider, and Voyage status
 */
export async function getProviderAvailability(): Promise<ProvidersResponse> {
  return request<ProvidersResponse>("/companion/providers");
}

// Companion Memory

export interface CompanionMemoryStats {
  conversation_id: string;
  total_turns: number;
  day_summaries: number;
  has_distilled_history: boolean;
  last_summarized_date?: string;
  last_distilled_date?: string;
}

/**
 * Fetches memory statistics for a companion conversation.
 */
export async function getCompanionMemoryStats(
  conversationId: string,
): Promise<CompanionMemoryStats> {
  return request<CompanionMemoryStats>(
    `/companion/memory/${conversationId}/stats`,
  );
}

/**
 * Fetches the formatted memory context for a companion conversation.
 */
export async function getCompanionMemoryContext(
  conversationId: string,
): Promise<{ context: string }> {
  return request<{ context: string }>(
    `/companion/memory/${conversationId}/context`,
  );
}

export async function getCompanionCoChange(params: {
  workspace: string;
  query: string;
  limit?: number;
}): Promise<{ cochange_hits: CoChangeHit[]; count: number }> {
  const query = new URLSearchParams();
  query.set("workspace", params.workspace);
  query.set("query", params.query);
  if (params.limit) query.set("limit", String(params.limit));
  return request(`/companion/cochange?${query.toString()}`);
}

// Companion Chat

// Detailed tool call information from companion chat
export interface ToolCallDetail {
  id: string;
  name: string;
  arguments?: unknown;
  output?: string; // Result from executing the tool
}

// Context injected by hooks during tool execution
export interface InjectedContextDetail {
  tool_call_id: string;
  source?: string;
  content: string;
}

export interface CompanionChatResponse {
  response: string;
  conversation_id: string;
  memory_context?: string;
  tools_used?: string[];
  tool_calls?: ToolCallDetail[];
  injected_contexts?: InjectedContextDetail[];
  presence?: PresenceBundle;
  error?: string;
  continuity?: {
    source?: string;
    visible_summary?: string;
    memory_query?: string;
    subcall_prompt?: string;
    layer_hits?: string[];
    subcall_count?: number;
    artifact_refs?: string[];
  };
}

/**
 * Send a message to a companion conversation and retrieve the companion's response.
 *
 * @param conversation_id - The ID of the companion conversation to target
 * @param message - The message content to send to the companion
 * @param workspace - Optional workspace identifier to scope the conversation
 * @returns A CompanionChatResponse containing the companion's reply, conversation ID, memory context, tools used, tool call details, and any injected contexts
 */
export async function companionChat(params: {
  conversation_id: string;
  message: string;
  workspace?: string;
  max_history_turns?: number;
  llm_provider?: string;
  llm_model?: string;
  exec_mode?: "reactive" | "autonomous" | "proactive" | "tick" | "story";
  story_gather_model?: string;
  story_dialogue_model?: string;
  context?: Record<string, unknown>;
  response_schema?: Record<string, unknown>;
  response_keys?: string[];
}): Promise<CompanionChatResponse> {
  return request("/companion/chat", {
    method: "POST",
    body: JSON.stringify(params),
  });
}

/**
 * Fetches a list of companion conversations.
 *
 * @param limit - Maximum number of conversations to return (default: 50)
 * @returns An object with a `conversations` array. Each conversation contains `id`, optional `title`, optional `name` (custom title from database), `created_at`, `updated_at`, and `message_count`.
 */
export async function listCompanionConversations(limit = 50): Promise<{
  conversations: Array<{
    id: string;
    title?: string;
    name?: string; // Custom title from database
    agent_id?: string; // Linked agent ID
    created_at: string;
    updated_at: string;
    message_count: number;
  }>;
}> {
  return request(`/companion/conversations?limit=${limit}`);
}

export interface CompanionMessage {
  id: string;
  conversation_id: string;
  role: "user" | "assistant";
  content: string;
  token_count: number;
  created_at: string;
  tool_calls?: ToolCallDetail[];
}

/**
 * Fetches messages for a companion conversation.
 *
 * @param conversationId - The ID of the companion conversation to fetch messages from
 * @param limit - Maximum number of messages to return (default: 100)
 * @returns An object containing `conversation_id`, an array of `messages`, and `count` which is the number of messages returned
 */
export async function getCompanionConversationMessages(
  conversationId: string,
  limit = 100,
): Promise<{
  conversation_id: string;
  messages: CompanionMessage[];
  count: number;
}> {
  return request(
    `/companion/conversations/${conversationId}/messages?limit=${limit}`,
  );
}

/**
 * Delete a single message from a companion conversation.
 *
 * @param conversationId - The ID of the conversation containing the message
 * @param messageId - The ID of the message to delete
 * @returns An object where `ok` is `true` if the operation succeeded, and `message` contains a status description.
 */
export async function deleteCompanionMessage(
  conversationId: string,
  messageId: string,
): Promise<{ ok: boolean; message: string }> {
  return request(
    `/companion/conversations/${conversationId}/messages/${messageId}`,
    {
      method: "DELETE",
    },
  );
}

/**
 * Soft-delete a companion conversation by marking it deleted without removing its data.
 *
 * @returns An object where `ok` is `true` if the operation succeeded, and `message` contains a status description.
 */
export async function deleteCompanionConversation(
  conversationId: string,
): Promise<{ ok: boolean; message: string }> {
  return request(`/companion/conversations/${conversationId}`, {
    method: "DELETE",
  });
}

/**
 * Set or update the custom title of a companion conversation.
 *
 * @param conversationId - The ID of the conversation to rename
 * @param title - The new title to assign to the conversation
 * @returns An object with `ok` set to `true` if the rename succeeded, and `message` containing the server status message
 */
export async function renameCompanionConversation(
  conversationId: string,
  title: string,
  agentId?: string | null,
): Promise<{ ok: boolean; message: string }> {
  const body: Record<string, unknown> = { title };
  if (agentId !== undefined) {
    body.agent_id = agentId ?? "";
  }
  return request(`/companion/conversations/${conversationId}`, {
    method: "PATCH",
    body: JSON.stringify(body),
  });
}

export interface CompanionCompressionResult {
  conversation_id: string;
  processed_dates?: string[];
  summarized: number;
  skipped: number;
  distilled: boolean;
}

/**
 * Trigger on-demand memory compression for a companion conversation.
 *
 * This generates or updates day summaries (L1) and may run distillation (L2) to keep
 * long-running conversations queryable even when L0 history injection is truncated.
 */
export async function compressCompanionConversation(
  conversationId: string,
  params: {
    include_today?: boolean;
    max_days?: number;
    force?: boolean;
    distill?: boolean;
    llm_provider?: string;
    llm_model?: string;
  } = {},
): Promise<CompanionCompressionResult> {
  const env = await request<ApiEnvelope<CompanionCompressionResult>>(
    `/companion/conversations/${conversationId}/compress`,
    {
      method: "POST",
      body: JSON.stringify(params),
    },
  );
  return unwrapEnvelope(env);
}

// Personality dimensions (0.0 to 1.0 scale)
export interface PersonalityDimension {
  name: string;
  description: string;
  value: number;
  min_label: string;
  max_label: string;
}

// Full personality profile
export interface PersonalityProfile {
  dimensions: PersonalityDimension[];
  learned_traits: string[];
  interests: string[];
  dislikes: string[];
}

// Personality info response
export interface PersonalityInfo {
  profile: PersonalityProfile;
  system_prompt: string;
  memory_context?: string;
}

/**
 * Fetches the personality information for a companion conversation.
 *
 * @param conversationId - The companion conversation's identifier
 * @returns The conversation's `PersonalityInfo` containing the personality profile, system prompt, and memory context
 */
export async function getCompanionPersonality(
  conversationId: string,
): Promise<PersonalityInfo> {
  return request(`/companion/conversations/${conversationId}/personality`);
}

/**
 * Update the value of a personality dimension for a companion conversation.
 *
 * @param conversationId - ID of the companion conversation to update
 * @param name - Name of the personality dimension to set
 * @param value - New numeric value for the dimension
 * @returns The updated dimension object `{ success: boolean; name: string; value: number }` where `success` is `true` if the update succeeded
 */
export async function updatePersonalityDimension(
  conversationId: string,
  name: string,
  value: number,
): Promise<{ success: boolean; name: string; value: number }> {
  return request(
    `/companion/conversations/${conversationId}/personality/dimension`,
    {
      method: "PATCH",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ name, value }),
    },
  );
}

// Persisted Sessions (Claude Code sessions stored in sessions.db)
export interface PersistedSession {
  id: string;
  workspace_path: string;
  project_name?: string;
  git_branch?: string;
  claude_version?: string;
  started_at: string;
  ended_at?: string;
  summary?: string;
  accomplished?: string[];
  decisions?: string[];
  gotchas?: string[];
  tags?: string[];
  key_files?: string[];
  tools_pattern?: string;
  message_count: number;
  user_turns: number;
  tool_invocations: number;
  total_tokens: number;
  status: string;
  agent_id: string;
  agent_type?: string;
  parent_session_id?: string;
}

export interface SessionMessage {
  index: number;
  type: string;
  timestamp: string;
  uuid?: string;
  summary?: string;
  error?: string;
  message?: {
    role?: string;
    content?: unknown;
  };
  tool_calls?: string[];
  files_touched?: string[];
}

/**
 * Fetches a paginated list of persisted sessions, optionally filtered by workspace.
 *
 * @param params.workspace - Workspace path to filter sessions by.
 * @param params.limit - Maximum number of sessions to return.
 * @param params.offset - Number of sessions to skip for pagination.
 * @returns An object with `sessions` (array of persisted sessions), `total` (total matching sessions), `limit` (applied limit) and `offset` (applied offset).
 */
export async function listPersistedSessions(params?: {
  workspace?: string;
  limit?: number;
  offset?: number;
}): Promise<{
  sessions: PersistedSession[];
  total: number;
  limit: number;
  offset: number;
}> {
  const query = new URLSearchParams();
  if (params?.workspace) query.set("workspace", params.workspace);
  if (params?.limit) query.set("limit", String(params.limit));
  if (params?.offset) query.set("offset", String(params.offset));

  const queryStr = query.toString();
  return request(`/sessions${queryStr ? `?${queryStr}` : ""}`);
}

/**
 * Fetches a persisted session by its ID.
 *
 * @param sessionId - The ID of the persisted session to retrieve
 * @returns An object containing the `session` (`PersistedSession`)
 */
export async function getPersistedSession(sessionId: string): Promise<{
  session: PersistedSession;
}> {
  return request(`/sessions/${sessionId}`);
}

/**
 * Fetches paginated messages for a persisted session.
 *
 * @param sessionId - The persisted session's identifier
 * @param params - Optional pagination options: `limit` is the maximum number of messages to return, `offset` is the zero-based starting index
 * @returns An object with `messages` (array of `SessionMessage`), `total` message count, the effective `limit` and `offset`, and an optional `path`
 */
export async function getSessionMessages(
  sessionId: string,
  params?: { limit?: number; offset?: number },
): Promise<{
  messages: SessionMessage[];
  total: number;
  limit: number;
  offset: number;
  path?: string;
}> {
  const query = new URLSearchParams();
  if (params?.limit) query.set("limit", String(params.limit));
  if (params?.offset) query.set("offset", String(params.offset));

  const queryStr = query.toString();
  return request(
    `/sessions/${sessionId}/messages${queryStr ? `?${queryStr}` : ""}`,
  );
}
