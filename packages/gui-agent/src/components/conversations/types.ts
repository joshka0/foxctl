export interface ToolCallInfo {
  name: string;
  args?: string;
  result?: string;
  injectedContext?: string;
}

export type ExecMode =
  | ""
  | "reactive"
  | "autonomous"
  | "proactive"
  | "tick"
  | "story";

export interface ContextInfo {
  systemPrompt?: string;
  workspace?: string;
  profile?: string;
  createdAt?: string;
  lastActivity?: string;
  toolCalls?: ToolCallInfo[];
  continuity?: {
    source?: string;
    visibleSummary?: string;
    memoryQuery?: string;
    subcallPrompt?: string;
    layerHits?: string[];
    subcallCount?: number;
    artifactRefs?: string[];
  };
  injectedContexts?: Array<{
    source: string;
    content: string;
    toolName?: string;
  }>;
}
