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

export interface ActivityEvent {
  operation: string;
  command?: string;
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
  command?: string;
  status: string;
  component?: string;
  session_id?: string;
  agent_id?: string;
  workspace_id?: string;
  duration_ms?: number;
  error_message?: string;
  data?: Record<string, unknown>;
}

export interface LogsListResponse {
  entries: LogEntry[];
  count: number;
}
