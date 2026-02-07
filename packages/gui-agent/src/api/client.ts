import type {
  AgentsListResponse,
  AgentSpawnResponse,
  MailboxListResponse,
  BlackboardListResponse,
  LogsListResponse,
  AgentSession,
} from './types'

const API_BASE = '/api'

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
  options: RequestInit = {}
): Promise<T> {
  const response = await fetch(`${API_BASE}${endpoint}`, {
    headers: {
      'Content-Type': 'application/json',
      ...options.headers,
    },
    ...options,
  })

  const text = await response.text()
  if (!response.ok) {
    throw new Error(text || `Request failed: ${response.status}`)
  }
  if (!text) {
    return undefined as T
  }
  try {
    return JSON.parse(text) as T
  } catch {
    throw new Error('Invalid JSON response')
  }
}

// ApiEnvelope matches the canonical agentctl envelope returned by some API endpoints.
interface ApiEnvelope<T> {
  version: number
  status: 'ok' | 'error' | 'progress'
  command: string
  data: T
  meta: { ts: string; [key: string]: unknown }
  error: { code?: string; message?: string }
}

/**
 * Retrieve a list of agents from the server.
 *
 * @param limit - Maximum number of agents to return (default: 100)
 * @returns An `AgentsListResponse` containing the array of agents and the total count
 */
export async function listAgents(limit = 100): Promise<AgentsListResponse> {
  return request<AgentsListResponse>(`/agents?limit=${limit}`)
}

/**
 * Fetches a single agent by ID.
 *
 * @param id - The agent's identifier.
 * @returns The object containing the requested `agent`.
 */
export async function getAgent(id: string) {
  return request<{ agent: AgentsListResponse['agents'][0] }>(`/agents/${id}`)
}

export interface SpawnAgentParams {
  role: string
  prompt: string
  workspace_id?: string
  skills_allow?: string[]
  // Agent metadata
  name?: string // Human name (auto-generated if empty)
  slug?: string // Human-readable handle
  // Execution config
  exec_mode?: 'reactive' | 'autonomous' | 'proactive' | 'story'
  max_iterations?: number
  max_context_tokens?: number
  max_auto_turns?: number
  // LLM override
  llm_provider?: string
  llm_model?: string
}

/**
 * Spawn a new agent using the provided configuration parameters.
 *
 * @param params - Parameters for the agent to create (e.g., role, prompt, workspace_id, allowed skills, metadata, execution configuration, and LLM overrides)
 * @returns The `AgentSpawnResponse` containing details about the spawned agent and the operation status
 */
export async function spawnAgent(params: SpawnAgentParams): Promise<AgentSpawnResponse> {
  return request<AgentSpawnResponse>('/agents/spawn', {
    method: 'POST',
    body: JSON.stringify(params),
  })
}

/**
 * Delete an agent identified by its ID.
 *
 * @returns An object containing `status` describing the outcome and `agent_id` with the id of the deleted agent.
 */
export async function trashAgent(agentId: string): Promise<{ status: string; agent_id: string }> {
  return request(`/agents/${agentId}`, {
    method: 'DELETE',
  })
}

/**
 * Sends a kill command to the daemon managing the specified agent.
 *
 * @param agentId - The ID of the agent to kill
 * @returns `ok` indicates whether the kill request succeeded, `session_id` is the daemon session identifier, `status` is the resulting agent status, and `message` contains server-provided details
 */
export async function killAgent(agentId: string): Promise<{
  ok: boolean
  session_id: string
  status: string
  message: string
}> {
  return request(`/agents/${agentId}/daemon/kill`, {
    method: 'POST',
  })
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
  params?: { prompt?: string; workspace?: string }
): Promise<{
  session_id: string
  actor_id: string
  status: string
}> {
  return request(`/agents/${agentId}/daemon/start`, {
    method: 'POST',
    body: JSON.stringify(params || {}),
  })
}

export interface PatchAgentParams {
  conversation_id?: string // empty string to unlink
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
  params: PatchAgentParams
): Promise<{ agent: AgentsListResponse['agents'][0] }> {
  return request(`/agents/${agentId}`, {
    method: 'PATCH',
    body: JSON.stringify(params),
  })
}

/**
 * Fetches daemon sessions for the specified agent.
 *
 * @param agentId - The agent's identifier.
 * @returns An object containing `sessions`, an array of `AgentSession` objects, and `count`, the total number of sessions.
 */
export async function listAgentSessions(agentId: string): Promise<{
  sessions: AgentSession[]
  count: number
}> {
  return request(`/agents/${agentId}/daemon/sessions`)
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
  workspace_id: string
  actor_id?: string
  only_unread?: boolean
  limit?: number
}): Promise<MailboxListResponse> {
  const query = new URLSearchParams()
  query.set('workspace_id', params.workspace_id)
  if (params.actor_id) query.set('actor_id', params.actor_id)
  if (params.only_unread) query.set('only_unread', 'true')
  if (params.limit) query.set('limit', String(params.limit))

  return request<MailboxListResponse>(`/mailbox?${query}`)
}

/**
 * Sends a mailbox message within a workspace.
 *
 * @param params - Message payload containing `workspace_id`, `sender`, `recipient`, `subject`, and `body`; optional `kind` and `priority` may be provided
 * @returns The created message `id` and its `status`
 */
export async function sendMessage(params: {
  workspace_id: string
  sender: string
  recipient: string
  subject: string
  body: string
  kind?: string
  priority?: number
}): Promise<{ id: string; status: string }> {
  return request('/mailbox', {
    method: 'POST',
    body: JSON.stringify(params),
  })
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
  ns?: string
  topic?: string
  all?: boolean
  limit?: number
}): Promise<BlackboardListResponse> {
  const query = new URLSearchParams()
  if (params.ns) query.set('ns', params.ns)
  if (params.topic) query.set('topic', params.topic)
  if (params.all) query.set('all', 'true')
  if (params.limit) query.set('limit', String(params.limit))

  return request<BlackboardListResponse>(`/blackboard?${query}`)
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
  ns?: string
  topic: string
  payload: string
  ttl_sec?: number
}): Promise<{ id: string; status: string }> {
  return request('/blackboard', {
    method: 'POST',
    body: JSON.stringify(params),
  })
}

/**
 * Delete a blackboard entry identified by `id`.
 *
 * @param id - The blackboard entry identifier to remove
 */
export async function deleteBlackboard(id: string): Promise<void> {
  await request(`/blackboard/${id}`, { method: 'DELETE' })
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
  limit?: number
  since?: string
  component?: string
  operation?: string
  errors_only?: boolean
}): Promise<LogsListResponse> {
  const query = new URLSearchParams()
  if (params.limit) query.set('limit', String(params.limit))
  if (params.since) query.set('since', params.since)
  if (params.component) query.set('component', params.component)
  if (params.operation) query.set('operation', params.operation)
  if (params.errors_only) query.set('errors_only', 'true')

  return request<LogsListResponse>(`/logs?${query}`)
}

// Skills
export interface SkillParameter {
  name: string
  type: string
  required: boolean
  description: string
  enum?: string[]
  default?: unknown
}

export interface Skill {
  name: string
  version: string
  description: string
  tags: string[]
  command: string
  parameters: SkillParameter[]
  returns: SkillParameter[]
}

export interface SkillsListResponse {
  skills: Skill[]
  count: number
}

/**
 * Retrieves the list of available skills from the server.
 *
 * @returns The skills list response containing `skills` and `count`.
 */
export async function listSkills(): Promise<SkillsListResponse> {
  return request<SkillsListResponse>('/skills')
}

/**
 * Retrieves the service health status.
 *
 * @returns An object with `status` containing the current health state (for example, `"ok"`).
 */
export async function getHealth(): Promise<{ status: string }> {
  return request('/health')
}

// Console Sessions
export interface ConsoleSession {
  id: string
  workspace: string
  profile: string
  created: string
  last_activity?: string
  message_count: number
  client_count?: number
}

export interface ConsoleMessage {
  id?: string
  role: 'user' | 'assistant'
  content: string
  timestamp: string
  correlation_id?: string
  tool_calls?: ToolCall[]
}

export interface ToolCall {
  name: string
  input?: Record<string, unknown>
  output?: string
  status: 'pending' | 'completed' | 'error'
}

/**
 * Fetches console sessions, optionally filtered by workspace.
 *
 * @param workspace - Optional workspace identifier to filter returned sessions
 * @returns An object containing `sessions` (array of ConsoleSession) and `count` (total number of sessions matching the query)
 */
export async function listConsoleSessions(workspace?: string): Promise<{
  sessions: ConsoleSession[]
  count: number
}> {
  const query = workspace ? `?workspace=${encodeURIComponent(workspace)}` : ''
  return request(`/console/sessions${query}`)
}

/**
 * Create a new console session.
 *
 * @param params - Parameters for the new session
 * @param params.workspace - Optional workspace identifier to associate with the session
 * @param params.profile - Optional profile name to use for the session
 * @param params.system_prompt - Optional system prompt to seed the session
 * @param params.conversation_id - Optional existing conversation ID to attach the session to
 * @param params.tool_model - Optional model override for tool execution
 * @param params.response_model - Optional model override for responses
 * @returns The created `session`
 */
export async function createConsoleSession(params: {
  workspace?: string
  profile?: string
  system_prompt?: string
  conversation_id?: string
  tool_model?: string
  response_model?: string
}): Promise<{ session: ConsoleSession }> {
  return request('/console/sessions', {
    method: 'POST',
    body: JSON.stringify(params),
  })
}

/**
 * Fetches a console session by ID and returns its metadata, messages, and inflight state.
 *
 * @param sessionId - The ID of the console session to retrieve
 * @returns An object containing `session` (the session metadata), `messages` (array of console messages), and `inflight` (`true` when the session has active inflight activity, `false` otherwise)
 */
export async function getConsoleSession(sessionId: string): Promise<{
  session: ConsoleSession
  messages: ConsoleMessage[]
  inflight: boolean
}> {
  return request(`/console/sessions/${sessionId}`)
}

/**
 * Delete a console session by its ID.
 */
export async function deleteConsoleSession(sessionId: string): Promise<void> {
  await request(`/console/sessions/${sessionId}`, { method: 'DELETE' })
}

/**
 * Submits content to a console session for processing.
 *
 * @param correlationId - Optional client-provided correlation identifier to track the message across requests
 * @param models - Optional model overrides; `tool_model` selects the tool model, `response_model` selects the response model
 * @returns `{ ok: boolean; correlation_id: string }` where `ok` indicates the request was accepted and `correlation_id` is the identifier for the submitted message
 */
export async function askConsoleSession(
  sessionId: string,
  content: string,
  correlationId?: string,
  models?: { tool_model?: string; response_model?: string }
): Promise<{ ok: boolean; correlation_id: string }> {
  return request(`/console/sessions/${sessionId}/ask`, {
    method: 'POST',
    body: JSON.stringify({ content, correlation_id: correlationId, ...models }),
  })
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
  correlationId?: string
): Promise<{ ok: boolean }> {
  return request(`/console/sessions/${sessionId}/cancel`, {
    method: 'POST',
    body: JSON.stringify({ correlation_id: correlationId }),
  })
}

/**
 * Fetches messages for a console session.
 *
 * @param sessionId - The ID of the console session to retrieve messages from
 * @returns An object with `messages` — an array of ConsoleMessage entries, and `count` — the total number of messages
 */
export async function getConsoleMessages(sessionId: string): Promise<{
  messages: ConsoleMessage[]
  count: number
}> {
  return request(`/console/sessions/${sessionId}/messages`)
}

// Companion Chat

// Detailed tool call information from companion chat
export interface ToolCallDetail {
  id: string
  name: string
  arguments?: unknown
  output?: string // Result from executing the tool
}

// Context injected by hooks during tool execution
export interface InjectedContextDetail {
  tool_call_id: string
  source?: string
  content: string
}

export interface CompanionChatResponse {
  response: string
  conversation_id: string
  memory_context?: string
  tools_used?: string[]
  tool_calls?: ToolCallDetail[]
  injected_contexts?: InjectedContextDetail[]
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
  conversation_id: string
  message: string
  workspace?: string
  max_history_turns?: number
  llm_provider?: string
  llm_model?: string
}): Promise<CompanionChatResponse> {
  return request('/companion/chat', {
    method: 'POST',
    body: JSON.stringify(params),
  })
}

/**
 * Fetches a list of companion conversations.
 *
 * @param limit - Maximum number of conversations to return (default: 50)
 * @returns An object with a `conversations` array. Each conversation contains `id`, optional `title`, optional `name` (custom title from database), `created_at`, `updated_at`, and `message_count`.
 */
export async function listCompanionConversations(limit = 50): Promise<{
  conversations: Array<{
    id: string
    title?: string
    name?: string  // Custom title from database
    created_at: string
    updated_at: string
    message_count: number
  }>
}> {
  return request(`/companion/conversations?limit=${limit}`)
}

export interface CompanionMessage {
  id: string
  conversation_id: string
  role: 'user' | 'assistant'
  content: string
  token_count: number
  created_at: string
  tool_calls?: ToolCallDetail[]
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
  limit = 100
): Promise<{
  conversation_id: string
  messages: CompanionMessage[]
  count: number
}> {
  return request(`/companion/conversations/${conversationId}/messages?limit=${limit}`)
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
  messageId: string
): Promise<{ ok: boolean; message: string }> {
  return request(`/companion/conversations/${conversationId}/messages/${messageId}`, {
    method: 'DELETE',
  })
}

/**
 * Soft-delete a companion conversation by marking it deleted without removing its data.
 *
 * @returns An object where `ok` is `true` if the operation succeeded, and `message` contains a status description.
 */
export async function deleteCompanionConversation(
  conversationId: string
): Promise<{ ok: boolean; message: string }> {
  return request(`/companion/conversations/${conversationId}`, {
    method: 'DELETE',
  })
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
  title: string
): Promise<{ ok: boolean; message: string }> {
  return request(`/companion/conversations/${conversationId}`, {
    method: 'PATCH',
    body: JSON.stringify({ title }),
  })
}

export interface CompanionCompressionResult {
  conversation_id: string
  processed_dates?: string[]
  summarized: number
  skipped: number
  distilled: boolean
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
    include_today?: boolean
    max_days?: number
    force?: boolean
    distill?: boolean
    llm_provider?: string
    llm_model?: string
  } = {}
): Promise<CompanionCompressionResult> {
  const env = await request<ApiEnvelope<CompanionCompressionResult>>(
    `/companion/conversations/${conversationId}/compress`,
    {
      method: 'POST',
      body: JSON.stringify(params),
    }
  )
  if (env.status !== 'ok') {
    throw new Error(env.error?.message || 'Compression failed')
  }
  return env.data
}

// Personality dimensions (0.0 to 1.0 scale)
export interface PersonalityDimension {
  name: string
  description: string
  value: number
  min_label: string
  max_label: string
}

// Full personality profile
export interface PersonalityProfile {
  dimensions: PersonalityDimension[]
  learned_traits: string[]
  interests: string[]
  dislikes: string[]
}

// Personality info response
export interface PersonalityInfo {
  profile: PersonalityProfile
  system_prompt: string
  memory_context?: string
}

/**
 * Fetches the personality information for a companion conversation.
 *
 * @param conversationId - The companion conversation's identifier
 * @returns The conversation's `PersonalityInfo` containing the personality profile, system prompt, and memory context
 */
export async function getCompanionPersonality(
  conversationId: string
): Promise<PersonalityInfo> {
  return request(`/companion/conversations/${conversationId}/personality`)
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
  value: number
): Promise<{ success: boolean; name: string; value: number }> {
  return request(`/companion/conversations/${conversationId}/personality/dimension`, {
    method: 'PATCH',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ name, value }),
  })
}

// Persisted Sessions (Claude Code sessions stored in sessions.db)
export interface PersistedSession {
  id: string
  workspace_path: string
  project_name?: string
  git_branch?: string
  claude_version?: string
  started_at: string
  ended_at?: string
  summary?: string
  accomplished?: string[]
  decisions?: string[]
  gotchas?: string[]
  tags?: string[]
  key_files?: string[]
  tools_pattern?: string
  message_count: number
  user_turns: number
  tool_invocations: number
  total_tokens: number
  status: string
  agent_id: string
  agent_type?: string
  parent_session_id?: string
}

export interface SessionMessage {
  index: number
  type: string
  timestamp: string
  uuid?: string
  summary?: string
  error?: string
  message?: {
    role?: string
    content?: unknown
  }
  tool_calls?: string[]
  files_touched?: string[]
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
  workspace?: string
  limit?: number
  offset?: number
}): Promise<{
  sessions: PersistedSession[]
  total: number
  limit: number
  offset: number
}> {
  const query = new URLSearchParams()
  if (params?.workspace) query.set('workspace', params.workspace)
  if (params?.limit) query.set('limit', String(params.limit))
  if (params?.offset) query.set('offset', String(params.offset))

  const queryStr = query.toString()
  return request(`/sessions${queryStr ? `?${queryStr}` : ''}`)
}

/**
 * Fetches a persisted session by its ID.
 *
 * @param sessionId - The ID of the persisted session to retrieve
 * @returns An object containing the `session` (`PersistedSession`)
 */
export async function getPersistedSession(sessionId: string): Promise<{
  session: PersistedSession
}> {
  return request(`/sessions/${sessionId}`)
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
  params?: { limit?: number; offset?: number }
): Promise<{
  messages: SessionMessage[]
  total: number
  limit: number
  offset: number
  path?: string
}> {
  const query = new URLSearchParams()
  if (params?.limit) query.set('limit', String(params.limit))
  if (params?.offset) query.set('offset', String(params.offset))

  const queryStr = query.toString()
  return request(`/sessions/${sessionId}/messages${queryStr ? `?${queryStr}` : ''}`)
}
