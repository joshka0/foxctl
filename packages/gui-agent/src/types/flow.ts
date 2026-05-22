export type FlowState = "draft" | "running" | "paused" | "stopped" | "errored";
export type FlowNodeKind = "skill" | "pty" | "http" | "playwright" | "image" | "transform" | "agent";
export type FlowTransformKind =
  | "passthrough"
  | "regex_extract"
  | "template"
  | "jq_filter"
  | "split_lines"
  | "map_fields"
  | "file_write";
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

export interface FlowDetail extends Flow {
  nodes: FlowNode[];
  edges: FlowEdge[];
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

export function isFlowStatusResponse(value: unknown): value is FlowStatusResponse {
  const record = asRecord(value)
  return (
    !!record &&
    typeof record.flow_id === "string" &&
    typeof record.state === "string" &&
    Array.isArray(record.nodes) &&
    Array.isArray(record.edges)
  )
}

export interface FlowRunLog {
  id: string;
  run_id: string;
  node_id: string;
  seq: number;
  envelope: Record<string, unknown>;
  created_at: string;
}

function asRecord(value: unknown): Record<string, unknown> | null {
  return value && typeof value === "object" && !Array.isArray(value)
    ? (value as Record<string, unknown>)
    : null
}
