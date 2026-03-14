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
  injectedContexts?: Array<{
    source: string;
    content: string;
    toolName?: string;
  }>;
}
