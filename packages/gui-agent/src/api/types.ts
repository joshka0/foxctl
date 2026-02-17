// Agent types
export interface Agent {
  id: string
  parent_id?: string
  ns: string
  name?: string // Human name (e.g., "Luna", "Atlas")
  slug?: string // Human-readable handle (e.g., "researcher")
  role?: string
  skills_allow: string[]
  share_bb: string
  state: 'running' | 'idle' | 'stopped' | 'error' | 'unknown'
  created_at: string
  updated_at?: string
  heartbeat_at?: string
  prompt_summary?: string
  llm_provider?: string
  llm_model?: string
  exec_mode?: 'reactive' | 'autonomous' | 'proactive' | 'story'
  conversation_id?: string // Linked companion conversation ID
}

export interface AgentSession {
  session_id: string
  actor_id: string
  role: string
  status: string // 'running', 'stopped', etc.
  iterations: number
  started_at: string
}

// Activity/Observability types
export interface ActivityEvent {
  operation: string
  command?: string // Skill/hook name (e.g., "code/semantic_search")
  status: string
  component?: string
  trace_id?: string
  span_id?: string
  parent_id?: string
  service?: string
  version?: string
  subtype?: string
  session_id?: string
  agent_id?: string
  workspace_id?: string
  job_id?: string
  duration_ms?: number
  error_type?: string
  error_code?: string
  error_message?: string
  retriable?: boolean
  ts: string
  data?: Record<string, unknown>
}

export interface LogEntry {
  ts: string
  operation: string
  command?: string // Skill/hook name (e.g., "code/semantic_search")
  status: string
  component?: string
  session_id?: string
  agent_id?: string
  workspace_id?: string
  duration_ms?: number
  error_message?: string
  data?: Record<string, unknown>
}

// Mailbox types
export interface MailboxMessage {
  id: string
  sender: string
  recipient: string
  subject: string
  body: string
  kind: string
  priority: number
  status: string
  ack_required?: boolean
  created_at: string
  task_id?: string
  stream?: string
}

// Blackboard types
export interface BlackboardRecord {
  id: string
  ns: string
  topic: string
  ts: number
  ttl_sec: number
  payload: string
  cas_ref?: string
  lease_by?: string
  lease_exp?: number
}

// Companion types
export interface CompanionConversation {
  id: string
  title?: string
  created_at: string
  updated_at: string
  message_count: number
}

// API Response wrappers
export interface AgentsListResponse {
  agents: Agent[]
  total: number
}

export interface AgentSpawnResponse {
  session_id: string
  actor_id: string
  status: string
  name?: string // Generated or provided name
}

export interface MailboxListResponse {
  messages: MailboxMessage[]
}

export interface BlackboardListResponse {
  records: BlackboardRecord[]
}

export interface LogsListResponse {
  entries: LogEntry[]
  count: number
}
