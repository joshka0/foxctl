import { useEffect, useMemo, useRef, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { ScrollArea } from "@/components/ui/scroll-area";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import { Input } from "@/components/ui/input";
import { Textarea } from "@/components/ui/textarea";
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card";
import { ChatInput } from "@/components/chat/ChatInput";
import {
  MessageBubble,
  TypingIndicator,
} from "@/components/chat/MessageBubble";
import {
  askAgentStream,
  cancelAgentStream,
  companionChat,
  compressAgentMemory,
  createRoom,
  getAgent,
  getAgentRuntime,
  getCompanionConversationMessages,
  getCompanionMemoryContext,
  getCompanionMemoryStats,
  getRoom,
  getSessionMessages,
  listAgents,
  listPersistedSessions,
  listRooms,
  listWorkspaces,
  listRoomMessages,
  patchAgent,
  patchRoom,
  patchRoomMembers,
  spawnAgent,
  type CompanionMessage,
  type CompanionMemoryStats,
  type ConsoleMessage,
  type PersistedSession,
  type SessionMessage,
  sendRoomMessage,
} from "@/api/client";
import type {
  Agent,
  AgentChatStreamEvent,
  AgentRuntimeTreeNode,
  Room,
  RoomMessageEvent,
} from "@/api/types";
import { useAgentOperations } from "@/hooks/useAgentOperations";
import { useViewStore } from "@/stores/viewStore";
import { cn, formatRelativeTime } from "@/lib/utils";
import {
  getAgentDisplayName,
  getPromptSummaryOrSubtitle,
  getRoleIcon,
} from "@/lib/agent-utils";
import { resolveRoomWorkspacePath, roomDisplayName } from "@/lib/room-utils";
import {
  ArrowLeft,
  Bot,
  Brain,
  ChevronRight,
  Clock,
  Cpu,
  FileText,
  Folder,
  GitBranch,
  Layers,
  Link2,
  Network,
  Play,
  RefreshCw,
  Sparkles,
  Square,
  Trash2,
  UserCircle2,
  Wrench,
  Workflow,
} from "lucide-react";

interface AgentDetailViewProps {
  agent: Agent;
  onBack: () => void;
}

type AgentNodeSummary = {
  parent: Agent | null;
  ancestors: Agent[];
  children: Agent[];
};

type MemoryScope = "agent" | "session";
type MemoryRetention = "companion" | "durable" | "task" | "ephemeral";
type DispatchTargetPolicy =
  | "all_subtree"
  | "children_only"
  | "lead_only"
  | "selected";

type ChildDraft = {
  name: string;
  role: string;
  execMode: "reactive" | "autonomous" | "proactive" | "tick" | "story";
  thinkInterval: number;
  memoryScope: MemoryScope;
  memoryRetention: MemoryRetention;
  prompt: string;
};

type AgentRoomMember = {
  actor_id: string;
  role?: string;
};

type LiveRoomMessage = {
  key: string;
  correlationID?: string;
  messageID?: string;
  sender: string;
  subject?: string;
  body: string;
  createdAt: string;
  pending: boolean;
  error?: string;
  toolActivity: string[];
};

type RoomTimelineMessage = {
  key: string;
  id?: string;
  sender: string;
  subject?: string;
  body: string;
  createdAt: string;
  pending: boolean;
  error?: string;
  toolActivity: string[];
};

function conversationIDForAgent(agent: Agent): string {
  const linked = (agent.conversation_id || "").trim();
  if (linked) return linked;
  return agent.id;
}

function isExplicitConversation(agent: Agent): boolean {
  return (agent.conversation_id || "").trim().length > 0;
}

function mapCompanionMessage(message: CompanionMessage): ConsoleMessage {
  return {
    id: message.id,
    role: message.role,
    content: message.content,
    timestamp: message.created_at,
    tool_calls: message.tool_calls?.map((tool) => ({
      name: tool.name,
      input: tool.arguments as Record<string, unknown> | undefined,
      output: tool.output,
      status: "completed" as const,
    })),
  };
}

function buildAgentContext(agent: Agent): Record<string, unknown> {
  return {
    agent_id: agent.id,
    agent_name: getAgentDisplayName(agent),
    agent_role: agent.role || undefined,
    agent_state: agent.state,
    agent_exec_mode: agent.exec_mode || undefined,
    agent_workspace: agent.ns || undefined,
    agent_model: agent.llm_model || undefined,
  };
}

function deriveAgentTree(agents: Agent[], agent: Agent): AgentNodeSummary {
  const byID = new Map<string, Agent>();
  for (const candidate of agents) {
    byID.set(candidate.id, candidate);
  }

  const parent = agent.parent_id ? byID.get(agent.parent_id) || null : null;
  const children = agents
    .filter((candidate) => candidate.parent_id === agent.id)
    .sort((a, b) => {
      const left = getAgentDisplayName(a);
      const right = getAgentDisplayName(b);
      return left.localeCompare(right);
    });

  const ancestors: Agent[] = [];
  let cursor = parent;
  const seen = new Set<string>();
  for (
    ;
    cursor;
    cursor = cursor.parent_id ? byID.get(cursor.parent_id) || null : null
  ) {
    if (seen.has(cursor.id)) break;
    seen.add(cursor.id);
    ancestors.unshift(cursor);
  }

  return { parent, ancestors, children };
}

function agentRoomID(agentID: string): string {
  return `agent-${agentID}`;
}

function buildAgentRoomMembers(
  agent: Agent,
  subtreeAgents: Agent[],
): AgentRoomMember[] {
  const members = [
    { actor_id: agent.id, role: agent.role || "lead" },
    ...subtreeAgents.map((subtreeAgent) => ({
      actor_id: subtreeAgent.id,
      role: subtreeAgent.role || "worker",
    })),
  ];
  const seen = new Set<string>();
  return members.filter((member) => {
    if (!member.actor_id || seen.has(member.actor_id)) return false;
    seen.add(member.actor_id);
    return true;
  });
}

function normalizeMemoryScope(scope?: string): MemoryScope {
  return scope === "session" ? "session" : "agent";
}

function normalizeMemoryRetention(retention?: string): MemoryRetention {
  switch (retention) {
    case "companion":
    case "task":
    case "ephemeral":
      return retention;
    case "durable":
    default:
      return "durable";
  }
}

function recommendedMemoryScopeForRetention(
  retention: MemoryRetention,
): MemoryScope {
  return retention === "task" || retention === "ephemeral"
    ? "session"
    : "agent";
}

function defaultDistillForRetention(retention: MemoryRetention): boolean {
  return retention !== "task" && retention !== "ephemeral";
}

function describeMemoryRetention(retention: MemoryRetention): string {
  switch (retention) {
    case "companion":
      return "Long-lived layered memory for companion-style agents.";
    case "task":
      return "Task-scoped memory that stays useful for a unit of work.";
    case "ephemeral":
      return "Scratch memory with minimal persistence.";
    case "durable":
    default:
      return "Stable memory for long-running agents without companion semantics.";
  }
}

function collectSubtreeAgents(agents: Agent[], rootID: string): Agent[] {
  const byParent = new Map<string, Agent[]>();
  for (const candidate of agents) {
    const parentID = (candidate.parent_id || "").trim();
    if (!parentID) continue;
    const siblings = byParent.get(parentID) || [];
    siblings.push(candidate);
    byParent.set(parentID, siblings);
  }

  const out: Agent[] = [];
  const seen = new Set<string>();
  const walk = (parentID: string) => {
    const children = [...(byParent.get(parentID) || [])].sort((left, right) =>
      getAgentDisplayName(left).localeCompare(getAgentDisplayName(right)),
    );
    for (const child of children) {
      if (seen.has(child.id)) continue;
      seen.add(child.id);
      out.push(child);
      walk(child.id);
    }
  };
  walk(rootID);
  return out;
}

function arraysEqual(left: string[], right: string[]): boolean {
  if (left.length !== right.length) return false;
  return left.every((value, index) => value === right[index]);
}

function roomEventKey(event: RoomMessageEvent): string {
  const correlationID = (event.correlation_id || "").trim();
  if (correlationID) return correlationID;
  const messageID = (event.message_id || "").trim();
  if (messageID) return messageID;
  const agentID = (event.agent_id || event.sender || "room").trim();
  return `${agentID}:${event.phase || "event"}`;
}

function applyRoomEvent(
  liveMessages: Record<string, LiveRoomMessage>,
  event: RoomMessageEvent,
  timestamp: string,
): Record<string, LiveRoomMessage> {
  if (!event.phase || event.phase === "sent") {
    return liveMessages;
  }
  const key = roomEventKey(event);
  const existing = liveMessages[key];
  const next: LiveRoomMessage = existing || {
    key,
    correlationID: event.correlation_id,
    messageID: event.message_id,
    sender: (event.sender || event.agent_id || "agent").trim() || "agent",
    subject: (event.subject || "").trim() || undefined,
    body: "",
    createdAt: timestamp,
    pending: true,
    toolActivity: [],
  };

  switch (event.phase) {
    case "agent_started":
      return {
        ...liveMessages,
        [key]: {
          ...next,
          correlationID: event.correlation_id || next.correlationID,
          sender:
            (event.sender || event.agent_id || next.sender).trim() ||
            next.sender,
          subject: (event.subject || next.subject || "").trim() || undefined,
          pending: true,
          error: undefined,
          createdAt: next.createdAt || timestamp,
        },
      };
    case "agent_delta":
      return {
        ...liveMessages,
        [key]: {
          ...next,
          body: `${next.body}${event.content_delta || ""}`,
          pending: true,
        },
      };
    case "agent_tool_call":
      return {
        ...liveMessages,
        [key]: {
          ...next,
          toolActivity: [
            ...next.toolActivity,
            `Tool call: ${event.tool_name || "tool"}`,
          ],
          pending: true,
        },
      };
    case "agent_tool_result":
      return {
        ...liveMessages,
        [key]: {
          ...next,
          toolActivity: [
            ...next.toolActivity,
            event.tool_output?.trim()
              ? `Tool result: ${event.tool_name || "tool"}${event.is_error ? " (error)" : ""}\n${event.tool_output}`
              : `Tool result: ${event.tool_name || "tool"}${event.is_error ? " (error)" : ""}`,
          ],
          pending: true,
        },
      };
    case "agent_completed":
      return {
        ...liveMessages,
        [key]: {
          ...next,
          messageID: event.message_id || next.messageID,
          body: (event.content || "").trim() || next.body || "Completed",
          pending: false,
          error: undefined,
        },
      };
    case "agent_error":
      return {
        ...liveMessages,
        [key]: {
          ...next,
          pending: false,
          error: event.error || "room dispatch failed",
        },
      };
    default:
      return liveMessages;
  }
}

function mergeRoomTimeline(
  persistedMessages: Array<{
    id: string;
    sender: string;
    subject?: string;
    body: string;
    created_at: string;
  }>,
  liveMessages: Record<string, LiveRoomMessage>,
): RoomTimelineMessage[] {
  const persistedIDs = new Set(persistedMessages.map((message) => message.id));
  const persistedTimeline = persistedMessages.map<RoomTimelineMessage>(
    (message) => ({
      key: message.id,
      id: message.id,
      sender: message.sender,
      subject: message.subject,
      body: message.body,
      createdAt: message.created_at,
      pending: false,
      toolActivity: [],
    }),
  );
  const liveTimeline = Object.values(liveMessages)
    .filter(
      (message) => !message.messageID || !persistedIDs.has(message.messageID),
    )
    .map<RoomTimelineMessage>((message) => ({
      key: message.key,
      id: message.messageID,
      sender: message.sender,
      subject: message.subject,
      body: message.body,
      createdAt: message.createdAt,
      pending: message.pending,
      error: message.error,
      toolActivity: message.toolActivity,
    }));

  return [...persistedTimeline, ...liveTimeline].sort(
    (left, right) =>
      Date.parse(left.createdAt || "") - Date.parse(right.createdAt || ""),
  );
}

function localRequestID(prefix: string): string {
  if (
    typeof crypto !== "undefined" &&
    typeof crypto.randomUUID === "function"
  ) {
    return `${prefix}-${crypto.randomUUID()}`;
  }
  return `${prefix}-${Date.now()}-${Math.random().toString(16).slice(2)}`;
}

function sessionConversationStorageKey(agentID: string): string {
  return `gui-agent-workbench-session:${agentID}`;
}

function loadSessionConversationID(agentID: string): string {
  const key = sessionConversationStorageKey(agentID);
  if (typeof window === "undefined") {
    return `agent-session-${agentID}`;
  }
  const existing = window.sessionStorage.getItem(key);
  if (existing && existing.trim()) return existing;
  const created = localRequestID(`agent-session-${agentID}`);
  window.sessionStorage.setItem(key, created);
  return created;
}

function applyStreamMessageUpdate(
  messages: ConsoleMessage[],
  correlationID: string,
  update: (message: ConsoleMessage) => ConsoleMessage,
): ConsoleMessage[] {
  const key = `stream-${correlationID}`;
  const index = messages.findIndex(
    (message) => message.correlation_id === correlationID || message.id === key,
  );
  if (index >= 0) {
    const next = [...messages];
    next[index] = update(next[index]);
    return next;
  }
  return [
    ...messages,
    update({
      id: key,
      role: "assistant",
      content: "",
      timestamp: new Date().toISOString(),
      correlation_id: correlationID,
      tool_calls: [],
    }),
  ];
}

function appendStreamDelta(
  messages: ConsoleMessage[],
  event: AgentChatStreamEvent,
): ConsoleMessage[] {
  return applyStreamMessageUpdate(
    messages,
    event.correlation_id,
    (message) => ({
      ...message,
      content: `${message.content || ""}${event.content_delta || ""}`,
    }),
  );
}

function attachStreamToolCall(
  messages: ConsoleMessage[],
  event: AgentChatStreamEvent,
): ConsoleMessage[] {
  return applyStreamMessageUpdate(
    messages,
    event.correlation_id,
    (message) => ({
      ...message,
      tool_calls: [
        ...(message.tool_calls || []),
        {
          name: event.tool_name || "tool",
          input: isRecord(event.tool_arguments)
            ? event.tool_arguments
            : undefined,
          status: "pending",
        },
      ],
    }),
  );
}

function attachStreamToolResult(
  messages: ConsoleMessage[],
  event: AgentChatStreamEvent,
): ConsoleMessage[] {
  return applyStreamMessageUpdate(messages, event.correlation_id, (message) => {
    const toolCalls = [...(message.tool_calls || [])];
    const index = toolCalls.findIndex(
      (tool) => tool.name === event.tool_name && tool.status === "pending",
    );
    const toolStatus: "completed" | "error" = event.metadata?.is_error
      ? "error"
      : "completed";
    const nextTool = {
      name: event.tool_name || "tool",
      output: event.tool_output,
      status: toolStatus,
    };
    if (index >= 0) {
      toolCalls[index] = {
        ...toolCalls[index],
        output: event.tool_output,
        status: toolStatus,
      };
    } else {
      toolCalls.push(nextTool);
    }
    return {
      ...message,
      tool_calls: toolCalls,
    };
  });
}

function finalizeStreamMessage(
  messages: ConsoleMessage[],
  event: AgentChatStreamEvent,
): ConsoleMessage[] {
  return applyStreamMessageUpdate(
    messages,
    event.correlation_id,
    (message) => ({
      ...message,
      content: (event.content || "").trim() || message.content || "Completed",
      timestamp: new Date().toISOString(),
    }),
  );
}

function failStreamMessage(
  messages: ConsoleMessage[],
  event: AgentChatStreamEvent,
): ConsoleMessage[] {
  return applyStreamMessageUpdate(
    messages,
    event.correlation_id,
    (message) => ({
      ...message,
      content: message.content?.trim()
        ? `${message.content}\n\nError: ${event.error || "stream failed"}`
        : `Error: ${event.error || "stream failed"}`,
      timestamp: new Date().toISOString(),
    }),
  );
}

function filterPersistedSessions(
  sessions: PersistedSession[],
  agent: Agent,
): PersistedSession[] {
  const actorID = `actor:agent:${agent.id}`;
  return sessions
    .filter((session) => {
      if (session.agent_id === agent.id || session.agent_id === actorID)
        return true;
      if (agent.role && session.agent_type === agent.role) return true;
      return false;
    })
    .sort((a, b) => Date.parse(b.started_at) - Date.parse(a.started_at));
}

function sessionMessageSummary(message: SessionMessage): string {
  const summary = (message.summary || "").trim();
  if (summary) return summary;
  if (message.error) return `Error: ${message.error}`;
  const content = message.message?.content;
  if (typeof content === "string" && content.trim()) return content;
  if (content !== undefined) return JSON.stringify(content);
  if (message.tool_calls && message.tool_calls.length > 0) {
    return `Used ${message.tool_calls.length} tool${message.tool_calls.length > 1 ? "s" : ""}`;
  }
  return message.type || "message";
}

function MemoryStat({
  label,
  value,
  accent,
}: {
  label: string;
  value: string | number;
  accent?: string;
}) {
  return (
    <div className="rounded-lg border border-border bg-background/60 p-3">
      <div className="text-[11px] uppercase tracking-wider text-muted-foreground">
        {label}
      </div>
      <div className={cn("mt-1 text-lg font-semibold text-foreground", accent)}>
        {value}
      </div>
    </div>
  );
}

function HierarchyLink({
  agent,
  label,
  onSelect,
}: {
  agent: Agent;
  label?: string;
  onSelect: (agent: Agent) => void;
}) {
  const RoleIcon = getRoleIcon(agent.role);
  return (
    <button
      type="button"
      onClick={() => onSelect(agent)}
      className="flex w-full items-center gap-3 rounded-lg border border-border bg-background/60 px-3 py-2 text-left transition-colors hover:bg-accent/40"
    >
      <div
        className={cn(
          "flex h-9 w-9 items-center justify-center rounded-lg",
          agent.state === "running"
            ? "bg-green-500/10 text-green-500"
            : "bg-muted text-muted-foreground",
        )}
      >
        <RoleIcon className="h-4 w-4" />
      </div>
      <div className="min-w-0 flex-1">
        <div className="flex items-center gap-2">
          <span className="truncate text-sm font-medium text-foreground">
            {getAgentDisplayName(agent)}
          </span>
          {label && (
            <Badge variant="secondary" className="text-[10px]">
              {label}
            </Badge>
          )}
        </div>
        <div className="truncate text-xs text-muted-foreground">
          {agent.role || "agent"} · {agent.id.slice(0, 8)} · {agent.state}
        </div>
      </div>
      <ChevronRight className="h-4 w-4 text-muted-foreground" />
    </button>
  );
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}

function runtimeNodeLabel(
  node: AgentRuntimeTreeNode,
  linkedAgent?: Agent,
): string {
  if (linkedAgent) return getAgentDisplayName(linkedAgent);
  const metadataName =
    isRecord(node.metadata) && typeof node.metadata.name === "string"
      ? node.metadata.name.trim()
      : "";
  if (metadataName) return metadataName;
  const tag = (node.tag || "").trim();
  if (tag) return tag;
  const agentID = (node.agent_id || "").trim();
  if (agentID) return agentID;
  return "runtime node";
}

function summarizeRuntimeNode(node?: AgentRuntimeTreeNode | null): {
  agentctlStatus?: string;
  profile?: string;
  lastError?: string;
} {
  if (!node || !isRecord(node.state)) return {};
  const state = node.state;
  const summary: {
    agentctlStatus?: string;
    profile?: string;
    lastError?: string;
  } = {};

  if (typeof state.profile === "string" && state.profile.trim()) {
    summary.profile = state.profile.trim();
  } else if (
    isRecord(node.metadata) &&
    typeof node.metadata.profile === "string" &&
    node.metadata.profile.trim()
  ) {
    summary.profile = node.metadata.profile.trim();
  }

  if (isRecord(state.agentctl)) {
    const agentctl = state.agentctl;
    if (typeof agentctl.status === "string" && agentctl.status.trim()) {
      summary.agentctlStatus = agentctl.status.trim();
    }
    if (typeof agentctl.last_error === "string" && agentctl.last_error.trim()) {
      summary.lastError = agentctl.last_error.trim();
    }
  }

  return summary;
}

function RuntimeTreeNodeCard({
  node,
  agentsByID,
  onSelect,
  onStart,
  onStop,
  busyAgentID,
}: {
  node: AgentRuntimeTreeNode;
  agentsByID: Map<string, Agent>;
  onSelect: (agent: Agent) => void;
  onStart?: (agent: Agent) => void;
  onStop?: (agent: Agent) => void;
  busyAgentID?: string | null;
}) {
  const linkedAgent = node.agent_id ? agentsByID.get(node.agent_id) : undefined;
  const summary = summarizeRuntimeNode(node);
  const label = runtimeNodeLabel(node, linkedAgent);
  const actionBusy = linkedAgent && busyAgentID === linkedAgent.id;

  return (
    <div className="rounded-lg border border-border bg-background/60 p-3">
      <div className="flex items-start justify-between gap-3">
        <div className="min-w-0 flex-1">
          {linkedAgent ? (
            <button
              type="button"
              onClick={() => onSelect(linkedAgent)}
              className="truncate text-left text-sm font-medium text-foreground transition-colors hover:text-primary"
            >
              {label}
            </button>
          ) : (
            <div className="truncate text-sm font-medium text-foreground">
              {label}
            </div>
          )}
          <div className="mt-1 truncate font-mono text-[11px] text-muted-foreground">
            {node.agent_id || node.tag || "unknown"}
          </div>
        </div>
        <div className="flex flex-wrap items-center justify-end gap-2">
          {node.status && (
            <Badge
              variant={node.status === "ok" ? "secondary" : "outline"}
              className="text-[10px]"
            >
              {node.status}
            </Badge>
          )}
          {summary.agentctlStatus && (
            <Badge variant="outline" className="text-[10px]">
              {summary.agentctlStatus}
            </Badge>
          )}
          {linkedAgent && (
            <>
              <Button
                type="button"
                variant="ghost"
                size="sm"
                className="h-7 px-2 text-[11px]"
                onClick={() => onSelect(linkedAgent)}
              >
                Open
              </Button>
              {linkedAgent.state === "running" ? (
                <Button
                  type="button"
                  variant="outline"
                  size="sm"
                  className="h-7 px-2 text-[11px]"
                  onClick={() => onStop?.(linkedAgent)}
                  disabled={!onStop || actionBusy}
                >
                  <Square className="h-3 w-3" />
                  Stop
                </Button>
              ) : (
                <Button
                  type="button"
                  variant="outline"
                  size="sm"
                  className="h-7 px-2 text-[11px]"
                  onClick={() => onStart?.(linkedAgent)}
                  disabled={!onStart || actionBusy}
                >
                  <Play className="h-3 w-3" />
                  Start
                </Button>
              )}
            </>
          )}
        </div>
      </div>

      <div className="mt-2 flex flex-wrap gap-3 text-[11px] text-muted-foreground">
        {summary.profile && <span>profile: {summary.profile}</span>}
        {node.children && node.children.length > 0 && (
          <span>children: {node.children.length}</span>
        )}
        {node.pid && <span>pid: {node.pid}</span>}
      </div>

      {summary.lastError && (
        <div className="mt-2 rounded border border-destructive/20 bg-destructive/5 px-2 py-1 text-xs text-destructive">
          {summary.lastError}
        </div>
      )}
      {node.error && (
        <div className="mt-2 rounded border border-destructive/20 bg-destructive/5 px-2 py-1 text-xs text-destructive">
          {node.error}
        </div>
      )}

      {node.children && node.children.length > 0 && (
        <div className="mt-3 space-y-2 border-l border-border pl-3">
          {node.children.map((child, index) => (
            <RuntimeTreeNodeCard
              key={`${child.agent_id || child.tag || "runtime-child"}-${index}`}
              node={child}
              agentsByID={agentsByID}
              onSelect={onSelect}
              onStart={onStart}
              onStop={onStop}
              busyAgentID={busyAgentID}
            />
          ))}
        </div>
      )}
    </div>
  );
}

export function AgentDetailView({ agent, onBack }: AgentDetailViewProps) {
  const queryClient = useQueryClient();
  const setSelectedAgent = useViewStore((s) => s.setSelectedAgent);
  const setActiveView = useViewStore((s) => s.setActiveView);
  const setSelectedRoom = useViewStore((s) => s.setSelectedRoom);
  const eventSourceRef = useRef<EventSource | null>(null);
  const [messages, setMessages] = useState<ConsoleMessage[]>([]);
  const [messageError, setMessageError] = useState<string | null>(null);
  const [showMemoryContext, setShowMemoryContext] = useState(false);
  const [memoryContext, setMemoryContext] = useState("");
  const [memoryContextError, setMemoryContextError] = useState<string | null>(
    null,
  );
  const [memoryContextLoading, setMemoryContextLoading] = useState(false);
  const [selectedSessionID, setSelectedSessionID] = useState<string | null>(
    null,
  );
  const [chatSending, setChatSending] = useState(false);
  const [chatStatus, setChatStatus] = useState<string | null>(null);
  const [compressing, setCompressing] = useState(false);
  const [activeCorrelationID, setActiveCorrelationID] = useState<string | null>(
    null,
  );
  const [sessionConversationID, setSessionConversationID] = useState(() =>
    loadSessionConversationID(agent.id),
  );
  const [memoryScopeDraft, setMemoryScopeDraft] = useState<MemoryScope>(
    normalizeMemoryScope(agent.memory_scope),
  );
  const [memoryRetentionDraft, setMemoryRetentionDraft] =
    useState<MemoryRetention>(normalizeMemoryRetention(agent.memory_retention));
  const [controlRoomBusyAgentID, setControlRoomBusyAgentID] = useState<
    string | null
  >(null);
  const [roomDraft, setRoomDraft] = useState("");
  const [roomStatus, setRoomStatus] = useState<string | null>(null);
  const [liveRoomMessages, setLiveRoomMessages] = useState<
    Record<string, LiveRoomMessage>
  >({});
  const [dispatchTargetPolicy, setDispatchTargetPolicy] =
    useState<DispatchTargetPolicy>("all_subtree");
  const [selectedDispatchTargets, setSelectedDispatchTargets] = useState<
    string[]
  >([]);
  const [spawnDraft, setSpawnDraft] = useState<ChildDraft>({
    name: "",
    role: "coder",
    execMode: "reactive",
    thinkInterval: 60,
    memoryScope: "session",
    memoryRetention: "task",
    prompt: "",
  });

  const { data: freshAgentData } = useQuery({
    queryKey: ["agent", agent.id],
    queryFn: () => getAgent(agent.id),
    refetchInterval: 10000,
  });
  const { data: workspacesData } = useQuery({
    queryKey: ["workspaces"],
    queryFn: listWorkspaces,
    staleTime: 10000,
  });
  const resolvedAgent = freshAgentData?.agent || agent;

  const {
    targetAgent,
    sessions: daemonSessions,
    startAgent: startAgentMutation,
    killAgent: killAgentMutation,
    trashAgent: trashAgentMutation,
  } = useAgentOperations(resolvedAgent);
  const activeAgent = targetAgent || resolvedAgent;
  const roomWorkspacePath = useMemo(
    () =>
      resolveRoomWorkspacePath(
        activeAgent.ns,
        (workspacesData?.workspaces ?? []).map((workspace) => workspace.path),
        workspacesData?.current,
      ),
    [activeAgent.ns, workspacesData?.current, workspacesData?.workspaces],
  );
  const RoleIcon = getRoleIcon(activeAgent.role);
  const activeMemoryScope = normalizeMemoryScope(activeAgent.memory_scope);
  const activeMemoryRetention = normalizeMemoryRetention(
    activeAgent.memory_retention,
  );
  const conversationID =
    activeMemoryScope === "session"
      ? sessionConversationID
      : conversationIDForAgent(activeAgent);
  const conversationExplicit =
    activeMemoryScope === "agent" && isExplicitConversation(activeAgent);

  const { data: allAgentsData } = useQuery({
    queryKey: ["agents"],
    queryFn: () => listAgents(200),
    refetchInterval: 10000,
  });
  const hierarchy = useMemo(
    () => deriveAgentTree(allAgentsData?.agents || [], activeAgent),
    [activeAgent, allAgentsData?.agents],
  );
  const subtreeAgents = useMemo(
    () => collectSubtreeAgents(allAgentsData?.agents || [], activeAgent.id),
    [activeAgent.id, allAgentsData?.agents],
  );
  const agentsByID = useMemo(
    () =>
      new Map(
        (allAgentsData?.agents || []).map((candidate) => [
          candidate.id,
          candidate,
        ]),
      ),
    [allAgentsData?.agents],
  );
  const controlRoomID = useMemo(
    () => agentRoomID(activeAgent.id),
    [activeAgent.id],
  );
  const controlRoomTitle = useMemo(
    () => `${getAgentDisplayName(activeAgent)} Control Room`,
    [activeAgent],
  );
  const controlRoomMembers = useMemo(
    () => buildAgentRoomMembers(activeAgent, subtreeAgents),
    [activeAgent, subtreeAgents],
  );
  const dispatchTargetOptions = useMemo(
    () => [
      { actor_id: activeAgent.id, role: activeAgent.role || "lead" },
      ...subtreeAgents.map((candidate) => ({
        actor_id: candidate.id,
        role: candidate.role || "worker",
      })),
    ],
    [activeAgent.id, activeAgent.role, subtreeAgents],
  );
  const effectiveDispatchTargets = useMemo(() => {
    switch (dispatchTargetPolicy) {
      case "lead_only":
        return [activeAgent.id];
      case "children_only":
        return subtreeAgents.map((candidate) => candidate.id);
      case "selected":
        return selectedDispatchTargets.filter(
          (id, index, values) => id && values.indexOf(id) === index,
        );
      case "all_subtree":
      default:
        return [
          activeAgent.id,
          ...subtreeAgents.map((candidate) => candidate.id),
        ];
    }
  }, [
    activeAgent.id,
    dispatchTargetPolicy,
    selectedDispatchTargets,
    subtreeAgents,
  ]);

  const {
    data: runtimeData,
    isLoading: loadingRuntime,
    isFetching: refreshingRuntime,
    refetch: refetchRuntime,
  } = useQuery({
    queryKey: ["agent-runtime", activeAgent.id, 3],
    queryFn: () => getAgentRuntime(activeAgent.id, { depth: 3 }),
    refetchInterval: activeAgent.state === "running" ? 5000 : 15000,
    refetchOnWindowFocus: false,
  });
  const runtimeTree = runtimeData?.runtime;
  const runtimeRoot = runtimeTree?.root;
  const runtimeSummary = useMemo(
    () => summarizeRuntimeNode(runtimeRoot),
    [runtimeRoot],
  );

  const controlRoomQuery = useQuery({
    queryKey: [
      "agent-room",
      roomWorkspacePath,
      controlRoomID,
      controlRoomMembers.map((member) => member.actor_id).join(","),
    ],
    enabled: !!roomWorkspacePath,
    retry: false,
    queryFn: async (): Promise<{ room: Room | null }> => {
      if (!roomWorkspacePath) {
        return { room: null };
      }
      try {
        const result = await getRoom(controlRoomID, {
          workspace_id: roomWorkspacePath,
        });
        return result;
      } catch (error) {
        const message =
          error instanceof Error ? error.message.toLowerCase() : "";
        if (message.includes("room not found") || message.includes("404")) {
          return { room: null };
        }
        throw error;
      }
    },
  });
  const controlRoom = controlRoomQuery.data?.room || null;

  const controlRoomMessagesQuery = useQuery({
    queryKey: ["agent-room-messages", roomWorkspacePath, controlRoomID],
    enabled: !!roomWorkspacePath && !!controlRoom,
    retry: false,
    queryFn: () =>
      listRoomMessages(controlRoomID, {
        workspace_id: roomWorkspacePath,
        limit: 100,
      }),
  });
  const controlRoomTimeline = useMemo(
    () =>
      mergeRoomTimeline(
        controlRoomMessagesQuery.data?.messages || [],
        liveRoomMessages,
      ),
    [controlRoomMessagesQuery.data?.messages, liveRoomMessages],
  );

  useEffect(() => {
    const validTargets = new Set(
      dispatchTargetOptions.map((target) => target.actor_id),
    );
    setSelectedDispatchTargets((current) =>
      current.filter((candidate) => validTargets.has(candidate)),
    );
  }, [dispatchTargetOptions]);

  useEffect(() => {
    const persistedIDs = new Set(
      (controlRoomMessagesQuery.data?.messages || []).map(
        (message) => message.id,
      ),
    );
    if (persistedIDs.size === 0) return;
    setLiveRoomMessages((prev) => {
      let changed = false;
      const next: Record<string, LiveRoomMessage> = {};
      for (const [key, message] of Object.entries(prev)) {
        if (message.messageID && persistedIDs.has(message.messageID)) {
          changed = true;
          continue;
        }
        next[key] = message;
      }
      return changed ? next : prev;
    });
  }, [controlRoomMessagesQuery.data?.messages]);

  useEffect(() => {
    if (!controlRoom) return;
    const nextPolicy =
      controlRoom.dispatch_policy === "children_only" ||
      controlRoom.dispatch_policy === "lead_only" ||
      controlRoom.dispatch_policy === "selected"
        ? controlRoom.dispatch_policy
        : "all_subtree";
    setDispatchTargetPolicy(nextPolicy);
    setSelectedDispatchTargets(controlRoom.dispatch_agent_ids || []);
  }, [
    controlRoom?.id,
    controlRoom?.dispatch_policy,
    (controlRoom?.dispatch_agent_ids || []).join(","),
  ]);

  useEffect(() => {
    setSessionConversationID(loadSessionConversationID(activeAgent.id));
    setMemoryScopeDraft(normalizeMemoryScope(activeAgent.memory_scope));
    setMemoryRetentionDraft(
      normalizeMemoryRetention(activeAgent.memory_retention),
    );
    setActiveCorrelationID(null);
    setRoomDraft("");
    setRoomStatus(null);
    setLiveRoomMessages({});
    setDispatchTargetPolicy("all_subtree");
    setSelectedDispatchTargets([]);
    if (eventSourceRef.current) {
      eventSourceRef.current.close();
      eventSourceRef.current = null;
    }
  }, [activeAgent.id, activeAgent.memory_retention, activeAgent.memory_scope]);

  const { data: persistedSessionsData } = useQuery({
    queryKey: ["persisted-sessions", activeAgent.id, activeAgent.ns],
    queryFn: async () =>
      listPersistedSessions({
        workspace: activeAgent.ns || undefined,
        limit: 200,
      }),
  });
  const persistedSessions = useMemo(
    () =>
      filterPersistedSessions(
        persistedSessionsData?.sessions || [],
        activeAgent,
      ),
    [activeAgent, persistedSessionsData?.sessions],
  );

  useEffect(() => {
    setSelectedSessionID((current) => {
      if (
        current &&
        persistedSessions.some((session) => session.id === current)
      ) {
        return current;
      }
      return persistedSessions[0]?.id || null;
    });
  }, [persistedSessions]);

  const selectedSession = useMemo(
    () =>
      persistedSessions.find((session) => session.id === selectedSessionID) ||
      null,
    [persistedSessions, selectedSessionID],
  );

  const agentRoomsQuery = useQuery({
    queryKey: ["agent-rooms", roomWorkspacePath, activeAgent.id],
    enabled: !!roomWorkspacePath,
    queryFn: async () => {
      const result = await listRooms({
        workspace_id: roomWorkspacePath,
        limit: 100,
      });
      return result.rooms.filter(
        (room) =>
          (room.members ?? []).some(
            (member) => member.actor_id === activeAgent.id,
          ) || (room.participants ?? []).includes(activeAgent.id),
      );
    },
    staleTime: 5000,
  });

  const { data: selectedSessionMessagesData } = useQuery({
    queryKey: ["persisted-session-messages", selectedSessionID],
    queryFn: () => getSessionMessages(selectedSessionID!, { limit: 40 }),
    enabled: !!selectedSessionID,
  });

  const {
    data: conversationMessages,
    isLoading: loadingConversation,
    refetch: refetchConversation,
  } = useQuery({
    queryKey: ["agent-conversation", conversationID],
    queryFn: async () => {
      const data = await getCompanionConversationMessages(conversationID, 200);
      return data.messages.map(mapCompanionMessage);
    },
    retry: false,
  });

  useEffect(() => {
    setMessages(conversationMessages || []);
  }, [conversationID, conversationMessages]);

  const {
    data: memoryStats,
    isLoading: loadingMemoryStats,
    refetch: refetchMemoryStats,
  } = useQuery<CompanionMemoryStats>({
    queryKey: ["agent-memory-stats", conversationID],
    queryFn: () => getCompanionMemoryStats(conversationID),
    retry: false,
  });

  useEffect(() => {
    setMessages(conversationMessages || []);
    setMessageError(null);
    setChatStatus(null);
    setShowMemoryContext(false);
    setMemoryContext("");
    setMemoryContextError(null);
  }, [activeAgent.id, conversationID, conversationMessages]);

  const loadMemoryContext = async (force = false) => {
    if (!force && memoryContextLoading) return;
    if (!force && memoryContext) return;
    setMemoryContextLoading(true);
    setMemoryContextError(null);
    try {
      const data = await getCompanionMemoryContext(conversationID);
      setMemoryContext(data.context || "");
    } catch (err) {
      setMemoryContextError(
        err instanceof Error
          ? err.message
          : "Failed to load layered memory context",
      );
    } finally {
      setMemoryContextLoading(false);
    }
  };

  const ensureConversationLink = async () => {
    if (activeMemoryScope === "session") return;
    if (conversationExplicit) return;
    try {
      await patchAgent(activeAgent.id, { conversation_id: conversationID });
      await Promise.all([
        queryClient.invalidateQueries({ queryKey: ["agents"] }),
        queryClient.invalidateQueries({ queryKey: ["agent", activeAgent.id] }),
      ]);
    } catch (err) {
      console.warn(
        "[AgentDetailView] Failed to persist conversation link:",
        err,
      );
    }
  };

  useEffect(() => {
    if (typeof window === "undefined") return;

    const eventSource = new EventSource("/api/events");
    eventSourceRef.current = eventSource;

    eventSource.onmessage = (rawEvent) => {
      let parsed: { type?: string; data?: unknown; ts?: string } | null = null;
      try {
        parsed = JSON.parse(rawEvent.data) as {
          type?: string;
          data?: unknown;
          ts?: string;
        };
      } catch {
        return;
      }
      if (!parsed || !isRecord(parsed.data)) {
        return;
      }
      if (parsed.type === "room.message") {
        const roomEvent = parsed.data as unknown as RoomMessageEvent;
        if (
          roomEvent.room_id !== controlRoomID ||
          roomEvent.workspace_id !== roomWorkspacePath
        ) {
          return;
        }
        const timestamp =
          typeof parsed.ts === "string" && parsed.ts.trim()
            ? parsed.ts
            : new Date().toISOString();
        setLiveRoomMessages((prev) =>
          applyRoomEvent(prev, roomEvent, timestamp),
        );
        if (
          roomEvent.phase === "sent" ||
          roomEvent.phase === "agent_completed"
        ) {
          void Promise.all([
            queryClient.invalidateQueries({
              queryKey: ["rooms", roomWorkspacePath],
            }),
            queryClient.invalidateQueries({
              queryKey: ["agent-room", roomWorkspacePath, controlRoomID],
            }),
            queryClient.invalidateQueries({
              queryKey: ["agent-room-messages", roomWorkspacePath, controlRoomID],
            }),
          ]);
        }
        if (roomEvent.phase === "agent_started" && roomEvent.agent_id) {
          setRoomStatus(`Awaiting reply from ${roomEvent.agent_id}`);
        } else if (roomEvent.phase === "agent_delta" && roomEvent.agent_id) {
          setRoomStatus(`Streaming reply from ${roomEvent.agent_id}`);
        } else if (
          roomEvent.phase === "agent_completed" &&
          roomEvent.agent_id
        ) {
          setRoomStatus(`Reply received from ${roomEvent.agent_id}`);
        } else if (roomEvent.phase === "agent_error") {
          setRoomStatus(
            roomEvent.error ||
              `Room dispatch failed for ${roomEvent.agent_id || "agent"}`,
          );
        }
        return;
      }
      if (parsed.type !== "agent.chat") {
        return;
      }

      const event = parsed.data as unknown as AgentChatStreamEvent;
      if (event.agent_id !== activeAgent.id) return;
      if (activeCorrelationID && event.correlation_id !== activeCorrelationID)
        return;

      if (event.phase === "delta") {
        setMessages((prev) => appendStreamDelta(prev, event));
        return;
      }
      if (event.phase === "tool_call") {
        setMessages((prev) => attachStreamToolCall(prev, event));
        return;
      }
      if (event.phase === "tool_result") {
        setMessages((prev) => attachStreamToolResult(prev, event));
        void refetchRuntime();
        return;
      }
      if (event.phase === "completed") {
        setMessages((prev) => finalizeStreamMessage(prev, event));
        setChatSending(false);
        setActiveCorrelationID(null);
        setMessageError(null);
        setChatStatus(
          activeMemoryScope === "session"
            ? "Reply stored in detached workbench memory"
            : `Reply stored in ${conversationExplicit ? "linked" : "implicit"} agent memory`,
        );
        void Promise.all([
          refetchConversation(),
          refetchMemoryStats(),
          refetchRuntime(),
          queryClient.invalidateQueries({
            queryKey: ["companion-conversations"],
          }),
          queryClient.invalidateQueries({ queryKey: ["agents"] }),
          queryClient.invalidateQueries({
            queryKey: ["agent", activeAgent.id],
          }),
        ]).then(async () => {
          if (showMemoryContext) {
            setMemoryContext("");
            await loadMemoryContext(true);
          }
        });
        return;
      }
      if (event.phase === "cancelled") {
        setMessages((prev) =>
          applyStreamMessageUpdate(prev, event.correlation_id, (message) => ({
            ...message,
            content: message.content?.trim()
              ? `${message.content}\n\n[Cancelled]`
              : "[Cancelled]",
            timestamp: new Date().toISOString(),
          })),
        );
        setChatSending(false);
        setActiveCorrelationID(null);
        setChatStatus("Agent turn cancelled");
        return;
      }
      if (event.phase === "error") {
        setMessages((prev) => failStreamMessage(prev, event));
        setChatSending(false);
        setActiveCorrelationID(null);
        setMessageError(event.error || "Streaming request failed");
      }
    };

    return () => {
      eventSource.close();
      if (eventSourceRef.current === eventSource) {
        eventSourceRef.current = null;
      }
    };
  }, [
    activeAgent.id,
    activeCorrelationID,
    activeMemoryScope,
    conversationExplicit,
    controlRoomID,
    queryClient,
    refetchConversation,
    refetchMemoryStats,
    refetchRuntime,
    showMemoryContext,
    roomWorkspacePath,
  ]);

  const memoryScopeMutation = useMutation({
    mutationFn: async (scope: MemoryScope) =>
      patchAgent(activeAgent.id, { memory_scope: scope }),
    onSuccess: async ({ agent: updated }) => {
      const scope = normalizeMemoryScope(updated.memory_scope);
      setMemoryScopeDraft(scope);
      setChatStatus(
        scope === "session"
          ? "Agent workbench now uses detached session memory"
          : "Agent workbench now uses stable agent memory",
      );
      await Promise.all([
        queryClient.invalidateQueries({ queryKey: ["agents"] }),
        queryClient.invalidateQueries({ queryKey: ["agent", activeAgent.id] }),
      ]);
    },
    onError: (error) => {
      setMemoryScopeDraft(activeMemoryScope);
      setMemoryContextError(
        error instanceof Error
          ? error.message
          : "Failed to update memory scope",
      );
    },
  });

  const memoryRetentionMutation = useMutation({
    mutationFn: async (retention: MemoryRetention) =>
      patchAgent(activeAgent.id, { memory_retention: retention }),
    onSuccess: async ({ agent: updated }) => {
      const retention = normalizeMemoryRetention(updated.memory_retention);
      setMemoryRetentionDraft(retention);
      setChatStatus(`Agent memory retention is now ${retention}`);
      await Promise.all([
        queryClient.invalidateQueries({ queryKey: ["agents"] }),
        queryClient.invalidateQueries({ queryKey: ["agent", activeAgent.id] }),
      ]);
    },
    onError: (error) => {
      setMemoryRetentionDraft(activeMemoryRetention);
      setMemoryContextError(
        error instanceof Error
          ? error.message
          : "Failed to update memory retention",
      );
    },
  });

  const spawnChildMutation = useMutation({
    mutationFn: async () => {
      const prompt = spawnDraft.prompt.trim();
      if (!prompt) {
        throw new Error("Child prompt is required");
      }
      return spawnAgent({
        role: spawnDraft.role,
        prompt,
        name: spawnDraft.name.trim() || undefined,
        workspace_id: activeAgent.ns || undefined,
        parent_id: activeAgent.id,
        exec_mode: spawnDraft.execMode,
        think_interval:
          spawnDraft.execMode === "proactive" || spawnDraft.execMode === "tick"
            ? spawnDraft.thinkInterval
            : undefined,
        memory_scope: spawnDraft.memoryScope,
        memory_retention: spawnDraft.memoryRetention,
        llm_provider: activeAgent.llm_provider || undefined,
        llm_model: activeAgent.llm_model || undefined,
      });
    },
    onSuccess: async () => {
      setSpawnDraft((prev) => ({ ...prev, name: "", prompt: "" }));
      setChatStatus("Child agent spawned under this parent");
      await Promise.all([
        queryClient.invalidateQueries({ queryKey: ["agents"] }),
        refetchRuntime(),
      ]);
    },
    onError: (error) => {
      setMessageError(
        error instanceof Error ? error.message : "Failed to spawn child agent",
      );
    },
  });

  const cancelStreamMutation = useMutation({
    mutationFn: async () => {
      if (!activeCorrelationID) {
        return { ok: true, agent_id: activeAgent.id, cancelled: 0 };
      }
      return cancelAgentStream(activeAgent.id, activeCorrelationID);
    },
    onSuccess: (result) => {
      if ((result.cancelled || 0) > 0) {
        setChatStatus("Cancellation requested for the in-flight agent turn");
      }
    },
    onError: (error) => {
      setMessageError(
        error instanceof Error
          ? error.message
          : "Failed to cancel agent stream",
      );
    },
  });

  const ensureRoomMutation = useMutation({
    mutationFn: async () => {
      if (!roomWorkspacePath) {
        throw new Error("Workspace is required to manage control rooms");
      }
      const dispatchAgentIDs = effectiveDispatchTargets;
      if (controlRoom) {
        await patchRoom(controlRoomID, {
          workspace_id: roomWorkspacePath,
          title: controlRoomTitle,
          description: `Coordination room for ${getAgentDisplayName(activeAgent)}`,
          dispatch_policy: dispatchTargetPolicy,
          dispatch_agent_ids: dispatchAgentIDs,
        });
        return patchRoomMembers(controlRoomID, {
          workspace_id: roomWorkspacePath,
          members: controlRoomMembers,
        });
      }
      return createRoom({
        workspace_id: roomWorkspacePath,
        id: controlRoomID,
        title: controlRoomTitle,
        description: `Coordination room for ${getAgentDisplayName(activeAgent)}`,
        dispatch_policy: dispatchTargetPolicy,
        dispatch_agent_ids: dispatchAgentIDs,
        members: controlRoomMembers,
      });
    },
    onSuccess: async () => {
      setRoomStatus(
        "Control room synced with current subtree members and dispatch defaults",
      );
      await Promise.all([
        controlRoomQuery.refetch(),
        controlRoomMessagesQuery.refetch(),
      ]);
    },
    onError: (error) => {
      setRoomStatus(
        error instanceof Error ? error.message : "Failed to sync control room",
      );
    },
  });

  const sendRoomMutation = useMutation({
    mutationFn: async () => {
      if (!roomWorkspacePath) {
        throw new Error("Workspace is required to send room messages");
      }
      const body = roomDraft.trim();
      if (!body) {
        throw new Error("Room message is required");
      }
      if (effectiveDispatchTargets.length === 0) {
        throw new Error("Select at least one dispatch target");
      }
      const persistedTargets = controlRoom?.dispatch_agent_ids || [];
      const policyUsesExplicitTargets = dispatchTargetPolicy !== "all_subtree";
      const needsRoomSync =
        !controlRoom ||
        (controlRoom.dispatch_policy || "all_subtree") !==
          dispatchTargetPolicy ||
        (policyUsesExplicitTargets &&
          !arraysEqual(persistedTargets, effectiveDispatchTargets));
      if (needsRoomSync) {
        await ensureRoomMutation.mutateAsync();
      }
      return sendRoomMessage(controlRoomID, {
        workspace_id: roomWorkspacePath,
        sender: "human:gui",
        body,
        dispatch_agents: true,
        context: buildAgentContext(activeAgent),
      });
    },
    onSuccess: async (result) => {
      setRoomDraft("");
      const dispatched = result.dispatched || 0;
      setRoomStatus(
        dispatched > 0
          ? `Control room message sent; queued ${dispatched} agent ${dispatched === 1 ? "reply" : "replies"}`
          : "Control room message sent",
      );
      await Promise.all([
        controlRoomQuery.refetch(),
        controlRoomMessagesQuery.refetch(),
      ]);
    },
    onError: (error) => {
      setRoomStatus(
        error instanceof Error
          ? error.message
          : "Failed to send control room message",
      );
    },
  });

  const handleControlRoomStart = async (target: Agent) => {
    setControlRoomBusyAgentID(target.id);
    try {
      await startAgentMutation.mutateAsync(target.id);
      await Promise.all([
        queryClient.invalidateQueries({ queryKey: ["agents"] }),
        refetchRuntime(),
      ]);
    } catch (error) {
      setMessageError(
        error instanceof Error ? error.message : "Failed to start agent",
      );
    } finally {
      setControlRoomBusyAgentID(null);
    }
  };

  const handleControlRoomStop = async (target: Agent) => {
    setControlRoomBusyAgentID(target.id);
    try {
      await killAgentMutation.mutateAsync(target.id);
      await Promise.all([
        queryClient.invalidateQueries({ queryKey: ["agents"] }),
        refetchRuntime(),
      ]);
    } catch (error) {
      setMessageError(
        error instanceof Error ? error.message : "Failed to stop agent",
      );
    } finally {
      setControlRoomBusyAgentID(null);
    }
  };

  const openRoom = (room: Room) => {
    setSelectedRoom(room.id, room.workspace_id);
    setSelectedAgent(null);
    setActiveView("rooms");
  };

  const handleSend = async (content: string) => {
    if (!content.trim() || chatSending) return;

    const userMessage: ConsoleMessage = {
      role: "user",
      content,
      timestamp: new Date().toISOString(),
    };
    const correlationID = localRequestID(`agent-chat-${activeAgent.id}`);
    const placeholder: ConsoleMessage = {
      id: `stream-${correlationID}`,
      role: "assistant",
      content: "",
      timestamp: new Date().toISOString(),
      correlation_id: correlationID,
      tool_calls: [],
    };

    setChatSending(true);
    setActiveCorrelationID(correlationID);
    setChatStatus(null);
    setMessageError(null);
    setMessages((prev) => [...prev, userMessage, placeholder]);

    try {
      await ensureConversationLink();
      const response = await askAgentStream(activeAgent.id, {
        message: content,
        correlation_id: correlationID,
        conversation_id: conversationID,
        context: buildAgentContext(activeAgent),
      });
      setChatStatus(
        response.accepted
          ? "Streaming reply from agent runtime..."
          : "Waiting for agent reply...",
      );
    } catch (err) {
      try {
        const response = await companionChat({
          conversation_id: conversationID,
          message: content,
          workspace: activeAgent.ns || "/",
          llm_provider: activeAgent.llm_provider || undefined,
          llm_model: activeAgent.llm_model || undefined,
          context: buildAgentContext(activeAgent),
        });
        setMessages((prev) =>
          applyStreamMessageUpdate(prev, correlationID, (message) => ({
            ...message,
            content: response.response,
            timestamp: new Date().toISOString(),
            tool_calls: response.tool_calls?.map((tool) => ({
              name: tool.name,
              input: tool.arguments as Record<string, unknown> | undefined,
              output: tool.output,
              status: "completed" as const,
            })),
          })),
        );
        setChatStatus(
          "Stream unavailable; reply completed via direct companion call",
        );
        await Promise.all([
          refetchConversation(),
          refetchMemoryStats(),
          queryClient.invalidateQueries({
            queryKey: ["companion-conversations"],
          }),
        ]);
      } catch (fallbackErr) {
        const message =
          fallbackErr instanceof Error
            ? fallbackErr.message
            : "Failed to send message";
        setMessageError(message);
        setMessages((prev) =>
          applyStreamMessageUpdate(prev, correlationID, (messageEntry) => ({
            ...messageEntry,
            content: `Error: ${message}`,
            timestamp: new Date().toISOString(),
          })),
        );
      } finally {
        setActiveCorrelationID(null);
        setChatSending(false);
      }
    }
  };

  const handleCompressMemory = async () => {
    if (compressing) return;
    setCompressing(true);
    setMemoryContextError(null);
    try {
      await ensureConversationLink();
      const result = await compressAgentMemory(activeAgent.id, {
        conversation_id: conversationID,
        distill: defaultDistillForRetention(activeMemoryRetention),
      });
      await refetchMemoryStats();
      if (showMemoryContext) {
        setMemoryContext("");
        await loadMemoryContext(true);
      }
      setChatStatus(
        result.distilled
          ? "Agent memory compressed, distilled, and refreshed"
          : "Agent memory compressed and refreshed",
      );
    } catch (err) {
      setMemoryContextError(
        err instanceof Error ? err.message : "Failed to compress memory",
      );
    } finally {
      setCompressing(false);
    }
  };

  const handleOpenGlobalConversation = () => {
    localStorage.setItem("gui-agent-auto-select-conversation", conversationID);
    setActiveView("companion");
  };

  const handleApplyMemoryScope = async () => {
    if (memoryScopeDraft === activeMemoryScope || memoryScopeMutation.isPending)
      return;
    setMemoryContextError(null);
    try {
      await memoryScopeMutation.mutateAsync(memoryScopeDraft);
    } catch {
      // Error state is handled by the mutation callbacks.
    }
  };

  const handleApplyMemoryRetention = async () => {
    if (
      memoryRetentionDraft === activeMemoryRetention ||
      memoryRetentionMutation.isPending
    )
      return;
    setMemoryContextError(null);
    try {
      await memoryRetentionMutation.mutateAsync(memoryRetentionDraft);
    } catch {
      // Error state is handled by the mutation callbacks.
    }
  };

  const toggleDispatchTarget = (agentID: string) => {
    setSelectedDispatchTargets((current) =>
      current.includes(agentID)
        ? current.filter((candidate) => candidate !== agentID)
        : [...current, agentID],
    );
  };

  const selectedSessionMessages = selectedSessionMessagesData?.messages || [];
  const currentSession = daemonSessions[0];

  return (
    <div className="flex h-full flex-col">
      <div className="border-b border-border px-4 py-3">
        <div className="flex items-start justify-between gap-4">
          <div className="flex min-w-0 items-start gap-3">
            <Button
              variant="ghost"
              size="icon"
              onClick={onBack}
              className="h-8 w-8 flex-shrink-0"
            >
              <ArrowLeft className="h-4 w-4" />
            </Button>
            <div
              className={cn(
                "mt-0.5 flex h-11 w-11 flex-shrink-0 items-center justify-center rounded-xl",
                activeAgent.state === "running"
                  ? "bg-green-500/10 text-green-500"
                  : "bg-muted text-muted-foreground",
              )}
            >
              <RoleIcon className="h-5 w-5" />
            </div>
            <div className="min-w-0">
              <div className="flex flex-wrap items-center gap-2">
                <h2 className="truncate text-lg font-semibold text-foreground">
                  {getAgentDisplayName(activeAgent)}
                </h2>
                <Badge
                  variant={
                    activeAgent.state === "running" ? "default" : "outline"
                  }
                >
                  {activeAgent.state}
                </Badge>
                <Badge variant="secondary">{activeAgent.role || "agent"}</Badge>
                <Badge variant="outline">
                  {activeMemoryScope === "session"
                    ? "session memory"
                    : conversationExplicit
                      ? "linked memory"
                      : "implicit memory"}
                </Badge>
              </div>
              <div className="mt-1 flex flex-wrap items-center gap-3 text-xs text-muted-foreground">
                <span className="inline-flex items-center gap-1">
                  <Bot className="h-3 w-3" />
                  {activeAgent.id.slice(0, 12)}...
                </span>
                {activeAgent.ns && (
                  <span className="inline-flex items-center gap-1">
                    <Folder className="h-3 w-3" />
                    {activeAgent.ns}
                  </span>
                )}
                {activeAgent.llm_model && (
                  <span className="inline-flex items-center gap-1">
                    <Cpu className="h-3 w-3" />
                    {activeAgent.llm_model}
                  </span>
                )}
                {activeAgent.exec_mode && (
                  <span className="inline-flex items-center gap-1">
                    <Sparkles className="h-3 w-3" />
                    {activeAgent.exec_mode}
                  </span>
                )}
              </div>
              <p className="mt-2 max-w-4xl text-sm text-muted-foreground">
                {getPromptSummaryOrSubtitle(activeAgent, 180)}
              </p>
            </div>
          </div>

          <div className="flex flex-wrap items-center justify-end gap-2">
            <Button
              variant="outline"
              size="sm"
              onClick={handleOpenGlobalConversation}
            >
              <Link2 className="h-4 w-4" />
              Global Conversation
            </Button>
            {activeAgent.state === "stopped" ? (
              <Button
                size="sm"
                onClick={() => startAgentMutation.mutate(activeAgent.id)}
                disabled={startAgentMutation.isPending}
              >
                <Play className="h-4 w-4" />
                Start
              </Button>
            ) : (
              <Button
                variant="outline"
                size="sm"
                onClick={() => killAgentMutation.mutate(activeAgent.id)}
                disabled={killAgentMutation.isPending}
              >
                <Square className="h-4 w-4" />
                Stop
              </Button>
            )}
            <Button
              variant="outline"
              size="sm"
              onClick={() => trashAgentMutation.mutate(activeAgent.id)}
              disabled={
                trashAgentMutation.isPending || activeAgent.state !== "stopped"
              }
            >
              <Trash2 className="h-4 w-4" />
              Trash
            </Button>
          </div>
        </div>
      </div>

      <div className="grid min-h-0 flex-1 gap-4 p-4 lg:grid-cols-[320px,minmax(0,1fr),360px]">
        <ScrollArea className="min-h-0 pr-2">
          <div className="space-y-4">
            <Card>
              <CardHeader className="pb-3">
                <CardTitle className="text-sm">Agent Snapshot</CardTitle>
                <CardDescription>
                  Stored agent metadata, daemon presence, and conversation
                  lineage.
                </CardDescription>
              </CardHeader>
              <CardContent className="space-y-3 text-sm">
                <div className="grid grid-cols-2 gap-2">
                  <MemoryStat
                    label="State"
                    value={activeAgent.state}
                    accent={
                      activeAgent.state === "running"
                        ? "text-green-500"
                        : undefined
                    }
                  />
                  <MemoryStat
                    label="Exec Mode"
                    value={activeAgent.exec_mode || "-"}
                  />
                </div>
                <div className="rounded-lg border border-border bg-background/60 p-3">
                  <div className="text-[11px] uppercase tracking-wider text-muted-foreground">
                    Conversation Lineage
                  </div>
                  <div className="mt-1 font-mono text-xs text-foreground">
                    {conversationID}
                  </div>
                  <div className="mt-2 text-xs text-muted-foreground">
                    {activeMemoryScope === "session"
                      ? "Detached workbench session. This lineage is local to the current browser session."
                      : conversationExplicit
                        ? "Persisted directly on the agent record."
                        : "Using the stable agent ID as the default conversation root."}
                  </div>
                </div>
                <div className="space-y-2 text-xs text-muted-foreground">
                  <div className="flex items-center gap-2">
                    <Clock className="h-3.5 w-3.5" />
                    Created {formatRelativeTime(activeAgent.created_at)}
                  </div>
                  {activeAgent.heartbeat_at && (
                    <div className="flex items-center gap-2">
                      <RefreshCw className="h-3.5 w-3.5" />
                      Heartbeat {formatRelativeTime(activeAgent.heartbeat_at)}
                    </div>
                  )}
                  {currentSession && (
                    <div className="flex items-center gap-2">
                      <GitBranch className="h-3.5 w-3.5" />
                      Active session {currentSession.session_id.slice(0, 12)}...
                    </div>
                  )}
                </div>
              </CardContent>
            </Card>

            <Card>
              <CardHeader className="pb-3">
                <div className="flex items-start justify-between gap-3">
                  <div>
                    <CardTitle className="text-sm">Live Runtime</CardTitle>
                    <CardDescription>
                      Jido runtime tree, child hierarchy, and current bridge
                      state.
                    </CardDescription>
                  </div>
                  <Button
                    variant="ghost"
                    size="icon"
                    className="h-8 w-8"
                    onClick={() => void refetchRuntime()}
                    disabled={refreshingRuntime}
                  >
                    <RefreshCw
                      className={cn(
                        "h-4 w-4",
                        refreshingRuntime && "animate-spin",
                      )}
                    />
                  </Button>
                </div>
              </CardHeader>
              <CardContent className="space-y-3">
                <div className="grid grid-cols-2 gap-2">
                  <MemoryStat
                    label="Bridge"
                    value={
                      runtimeRoot?.status ||
                      (loadingRuntime ? "..." : "offline")
                    }
                  />
                  <MemoryStat
                    label="Agentctl"
                    value={runtimeSummary.agentctlStatus || "-"}
                  />
                  <MemoryStat
                    label="Children"
                    value={runtimeRoot?.children?.length ?? 0}
                  />
                  <MemoryStat
                    label="Profile"
                    value={runtimeSummary.profile || "-"}
                  />
                </div>

                {runtimeTree?.error && (
                  <div className="rounded-lg border border-destructive/20 bg-destructive/5 px-3 py-2 text-xs text-destructive">
                    {runtimeTree.error}
                  </div>
                )}

                {loadingRuntime && !runtimeRoot ? (
                  <div className="rounded-lg border border-dashed border-border px-3 py-2 text-xs text-muted-foreground">
                    Loading live runtime state...
                  </div>
                ) : runtimeRoot ? (
                  <RuntimeTreeNodeCard
                    node={runtimeRoot}
                    agentsByID={agentsByID}
                    onSelect={setSelectedAgent}
                    onStart={(child) => void handleControlRoomStart(child)}
                    onStop={(child) => void handleControlRoomStop(child)}
                    busyAgentID={controlRoomBusyAgentID}
                  />
                ) : (
                  <div className="rounded-lg border border-dashed border-border px-3 py-2 text-xs text-muted-foreground">
                    No live runtime subtree is available. This panel reads from
                    the configured Jido JSON-RPC bridge.
                  </div>
                )}
              </CardContent>
            </Card>

            <Card>
              <CardHeader className="pb-3">
                <CardTitle className="text-sm">Hierarchy</CardTitle>
                <CardDescription>
                  Agent tree position and immediate worker lineage.
                </CardDescription>
              </CardHeader>
              <CardContent className="space-y-3">
                {hierarchy.parent ? (
                  <HierarchyLink
                    agent={hierarchy.parent}
                    label="parent"
                    onSelect={setSelectedAgent}
                  />
                ) : (
                  <div className="rounded-lg border border-dashed border-border px-3 py-2 text-xs text-muted-foreground">
                    Root-level agent in the current tree.
                  </div>
                )}

                {hierarchy.ancestors.length > 1 && (
                  <div className="space-y-2">
                    <div className="text-[11px] uppercase tracking-wider text-muted-foreground">
                      Ancestors
                    </div>
                    <div className="space-y-2">
                      {hierarchy.ancestors.slice(0, -1).map((ancestor) => (
                        <HierarchyLink
                          key={ancestor.id}
                          agent={ancestor}
                          label="ancestor"
                          onSelect={setSelectedAgent}
                        />
                      ))}
                    </div>
                  </div>
                )}

                <div className="space-y-2">
                  <div className="flex items-center justify-between text-[11px] uppercase tracking-wider text-muted-foreground">
                    <span>Children</span>
                    <Badge variant="secondary" className="text-[10px]">
                      {hierarchy.children.length}
                    </Badge>
                  </div>
                  {hierarchy.children.length > 0 ? (
                    <div className="space-y-2">
                      {hierarchy.children.map((child) => (
                        <HierarchyLink
                          key={child.id}
                          agent={child}
                          label="child"
                          onSelect={setSelectedAgent}
                        />
                      ))}
                    </div>
                  ) : (
                    <div className="rounded-lg border border-dashed border-border px-3 py-2 text-xs text-muted-foreground">
                      No direct child agents are linked right now.
                    </div>
                  )}
                </div>
              </CardContent>
            </Card>

            <Card>
              <CardHeader className="pb-3">
                <div className="flex items-start justify-between gap-3">
                  <div>
                    <CardTitle className="text-sm">Control Room</CardTitle>
                    <CardDescription>
                      Spawn child agents and manage immediate workers from this
                      parent surface.
                    </CardDescription>
                  </div>
                  <div className="flex items-center gap-2">
                    <Button
                      type="button"
                      variant="ghost"
                      size="sm"
                      className="h-8 px-2 text-[11px]"
                      onClick={() => setActiveView("rooms")}
                    >
                      Open Rooms
                    </Button>
                    <Button
                      type="button"
                      variant="outline"
                      size="sm"
                      className="h-8 px-2 text-[11px]"
                      onClick={() => ensureRoomMutation.mutate()}
                      disabled={ensureRoomMutation.isPending || !activeAgent.ns}
                    >
                      Sync Room
                    </Button>
                    <Workflow className="h-4 w-4 text-muted-foreground" />
                  </div>
                </div>
              </CardHeader>
              <CardContent className="space-y-3">
                {roomStatus && (
                  <div
                    className={cn(
                      "rounded border px-3 py-2 text-xs",
                      roomStatus.toLowerCase().includes("failed") ||
                        roomStatus.toLowerCase().includes("required")
                        ? "border-destructive/20 bg-destructive/5 text-destructive"
                        : "border-border bg-background/60 text-muted-foreground",
                    )}
                  >
                    {roomStatus}
                  </div>
                )}
                <div className="grid gap-3">
                  <div className="grid gap-2">
                    <label className="text-[11px] uppercase tracking-wider text-muted-foreground">
                      Child Name
                    </label>
                    <Input
                      value={spawnDraft.name}
                      onChange={(e) =>
                        setSpawnDraft((prev) => ({
                          ...prev,
                          name: e.target.value,
                        }))
                      }
                      placeholder="Auto-generated if empty"
                    />
                  </div>
                  <div className="grid grid-cols-2 gap-3">
                    <div className="grid gap-2">
                      <label className="text-[11px] uppercase tracking-wider text-muted-foreground">
                        Role
                      </label>
                      <select
                        value={spawnDraft.role}
                        onChange={(e) =>
                          setSpawnDraft((prev) => ({
                            ...prev,
                            role: e.target.value,
                          }))
                        }
                        className="h-9 rounded-md border border-input bg-background px-3 text-sm"
                      >
                        <option value="researcher">researcher</option>
                        <option value="coder">coder</option>
                        <option value="reviewer">reviewer</option>
                        <option value="planner">planner</option>
                        <option value="overseer">overseer</option>
                      </select>
                    </div>
                    <div className="grid gap-2">
                      <label className="text-[11px] uppercase tracking-wider text-muted-foreground">
                        Exec Mode
                      </label>
                      <select
                        value={spawnDraft.execMode}
                        onChange={(e) =>
                          setSpawnDraft((prev) => ({
                            ...prev,
                            execMode: e.target.value as ChildDraft["execMode"],
                          }))
                        }
                        className="h-9 rounded-md border border-input bg-background px-3 text-sm"
                      >
                        <option value="reactive">reactive</option>
                        <option value="autonomous">autonomous</option>
                        <option value="proactive">proactive</option>
                        <option value="tick">tick</option>
                        <option value="story">story</option>
                      </select>
                    </div>
                  </div>
                  {(spawnDraft.execMode === "proactive" ||
                    spawnDraft.execMode === "tick") && (
                    <div className="grid gap-2">
                      <label className="text-[11px] uppercase tracking-wider text-muted-foreground">
                        Tick Interval (s)
                      </label>
                      <Input
                        type="number"
                        min={1}
                        max={86400}
                        value={spawnDraft.thinkInterval}
                        onChange={(e) =>
                          setSpawnDraft((prev) => ({
                            ...prev,
                            thinkInterval: parseInt(e.target.value, 10) || 60,
                          }))
                        }
                        className="h-9 rounded-md border border-input bg-background px-3 text-sm"
                      />
                    </div>
                  )}
                  <div className="grid gap-2">
                    <label className="text-[11px] uppercase tracking-wider text-muted-foreground">
                      Memory Retention
                    </label>
                    <select
                      value={spawnDraft.memoryRetention}
                      onChange={(e) => {
                        const retention = normalizeMemoryRetention(
                          e.target.value,
                        );
                        setSpawnDraft((prev) => ({
                          ...prev,
                          memoryRetention: retention,
                          memoryScope:
                            recommendedMemoryScopeForRetention(retention),
                        }));
                      }}
                      className="h-9 rounded-md border border-input bg-background px-3 text-sm"
                    >
                      <option value="companion">companion</option>
                      <option value="durable">durable</option>
                      <option value="task">task</option>
                      <option value="ephemeral">ephemeral</option>
                    </select>
                  </div>
                  <div className="grid gap-2">
                    <label className="text-[11px] uppercase tracking-wider text-muted-foreground">
                      Workbench Memory
                    </label>
                    <select
                      value={spawnDraft.memoryScope}
                      onChange={(e) =>
                        setSpawnDraft((prev) => ({
                          ...prev,
                          memoryScope:
                            e.target.value === "session" ? "session" : "agent",
                        }))
                      }
                      className="h-9 rounded-md border border-input bg-background px-3 text-sm"
                    >
                      <option value="session">session</option>
                      <option value="agent">agent</option>
                    </select>
                  </div>
                  <div className="grid gap-2">
                    <label className="text-[11px] uppercase tracking-wider text-muted-foreground">
                      Child Prompt
                    </label>
                    <textarea
                      value={spawnDraft.prompt}
                      onChange={(e) =>
                        setSpawnDraft((prev) => ({
                          ...prev,
                          prompt: e.target.value,
                        }))
                      }
                      placeholder="What should this child handle?"
                      className="min-h-[96px] rounded-md border border-input bg-background px-3 py-2 text-sm"
                    />
                  </div>
                  <Button
                    type="button"
                    onClick={() => spawnChildMutation.mutate()}
                    disabled={
                      spawnChildMutation.isPending || !spawnDraft.prompt.trim()
                    }
                  >
                    <Workflow className="h-4 w-4" />
                    Spawn Child
                  </Button>
                  {spawnChildMutation.error instanceof Error && (
                    <div className="rounded border border-destructive/20 bg-destructive/5 px-3 py-2 text-xs text-destructive">
                      {spawnChildMutation.error.message}
                    </div>
                  )}
                </div>

                <div className="space-y-2">
                  <div className="flex items-center justify-between text-[11px] uppercase tracking-wider text-muted-foreground">
                    <span>Immediate Workers</span>
                    <Badge variant="secondary" className="text-[10px]">
                      {hierarchy.children.length}
                    </Badge>
                  </div>
                  {hierarchy.children.length > 0 ? (
                    hierarchy.children.map((child) => (
                      <div
                        key={`control-room-${child.id}`}
                        className="rounded-lg border border-border bg-background/60 p-3"
                      >
                        <div className="flex items-center justify-between gap-3">
                          <div className="min-w-0">
                            <button
                              type="button"
                              onClick={() => setSelectedAgent(child)}
                              className="truncate text-left text-sm font-medium text-foreground transition-colors hover:text-primary"
                            >
                              {getAgentDisplayName(child)}
                            </button>
                            <div className="truncate text-xs text-muted-foreground">
                              {child.role || "agent"} · {child.state} ·{" "}
                              {child.id.slice(0, 10)}
                            </div>
                          </div>
                          <div className="flex items-center gap-2">
                            <Button
                              type="button"
                              variant="ghost"
                              size="sm"
                              className="h-7 px-2 text-[11px]"
                              onClick={() => setSelectedAgent(child)}
                            >
                              Open
                            </Button>
                            {child.state === "running" ? (
                              <Button
                                type="button"
                                variant="outline"
                                size="sm"
                                className="h-7 px-2 text-[11px]"
                                onClick={() =>
                                  void handleControlRoomStop(child)
                                }
                                disabled={controlRoomBusyAgentID === child.id}
                              >
                                Stop
                              </Button>
                            ) : (
                              <Button
                                type="button"
                                variant="outline"
                                size="sm"
                                className="h-7 px-2 text-[11px]"
                                onClick={() =>
                                  void handleControlRoomStart(child)
                                }
                                disabled={controlRoomBusyAgentID === child.id}
                              >
                                Start
                              </Button>
                            )}
                          </div>
                        </div>
                      </div>
                    ))
                  ) : (
                    <div className="rounded-lg border border-dashed border-border px-3 py-2 text-xs text-muted-foreground">
                      No direct child agents are linked right now.
                    </div>
                  )}
                </div>
              </CardContent>
            </Card>

            <Card>
              <CardHeader className="pb-3">
                <CardTitle className="text-sm">Daemon Sessions</CardTitle>
                <CardDescription>
                  Runtime sessions currently attached to this agent.
                </CardDescription>
              </CardHeader>
              <CardContent className="space-y-2">
                {daemonSessions.length > 0 ? (
                  daemonSessions.map((session) => (
                    <div
                      key={session.session_id}
                      className="rounded-lg border border-border bg-background/60 p-3"
                    >
                      <div className="flex items-center justify-between gap-3">
                        <div className="min-w-0">
                          <div className="truncate text-sm font-medium text-foreground">
                            {session.session_id}
                          </div>
                          <div className="text-xs text-muted-foreground">
                            {session.status} · {session.iterations} iterations
                          </div>
                        </div>
                        <Badge
                          variant={
                            session.status === "running" ? "default" : "outline"
                          }
                        >
                          {session.status}
                        </Badge>
                      </div>
                    </div>
                  ))
                ) : (
                  <div className="rounded-lg border border-dashed border-border px-3 py-2 text-xs text-muted-foreground">
                    No active daemon sessions reported.
                  </div>
                )}
              </CardContent>
            </Card>

            <Card>
              <CardHeader className="pb-3">
                <div>
                  <CardTitle className="text-sm">Affiliated Rooms</CardTitle>
                  <CardDescription>
                    Shared room timelines this agent belongs to.
                  </CardDescription>
                </div>
              </CardHeader>
              <CardContent className="space-y-3">
                {agentRoomsQuery.data && agentRoomsQuery.data.length > 0 ? (
                  agentRoomsQuery.data.map((room) => (
                    <div
                      key={room.id}
                      className="rounded-lg border border-border bg-background/60 p-3"
                    >
                      <div className="flex items-center justify-between gap-3">
                        <div className="min-w-0">
                          <button
                            type="button"
                            className="truncate text-left text-sm font-medium text-foreground transition-colors hover:text-primary"
                            onClick={() => openRoom(room)}
                          >
                            {roomDisplayName(room)}
                          </button>
                          <div className="mt-1 text-xs text-muted-foreground font-mono">
                            {room.id}
                          </div>
                        </div>
                        <div className="flex items-center gap-2">
                          <Badge variant="secondary" className="text-[10px]">
                            {room.message_count} msgs
                          </Badge>
                          <Button
                            type="button"
                            variant="outline"
                            size="sm"
                            className="h-7 px-2 text-[11px]"
                            onClick={() => openRoom(room)}
                          >
                            Open Room
                          </Button>
                        </div>
                      </div>
                      <div className="mt-2 flex flex-wrap gap-2 text-[11px] text-muted-foreground">
                        {room.latest_message_at && (
                          <span>
                            updated {formatRelativeTime(room.latest_message_at)}
                          </span>
                        )}
                        {room.unread_count > 0 && (
                          <span>{room.unread_count} unread</span>
                        )}
                        {room.members?.length ? (
                          <span>{room.members.length} members</span>
                        ) : null}
                      </div>
                    </div>
                  ))
                ) : (
                  <div className="rounded-lg border border-dashed border-border px-3 py-2 text-xs text-muted-foreground">
                    This agent is not attached to any explicit rooms yet.
                  </div>
                )}
              </CardContent>
            </Card>
          </div>
        </ScrollArea>

        <Card className="flex min-h-0 flex-col overflow-hidden">
          <CardHeader className="border-b border-border pb-4">
            <div className="flex items-center justify-between gap-3">
              <div>
                <CardTitle className="text-sm">Agent Conversation</CardTitle>
                <CardDescription>
                  {activeMemoryScope === "session"
                    ? "Human-facing chat backed by a detached workbench memory thread."
                    : "Human-facing chat backed by the agent's stable layered memory context."}
                </CardDescription>
              </div>
              <Badge variant="outline" className="font-mono text-[10px]">
                {conversationID.slice(0, 16)}
              </Badge>
            </div>
            {chatStatus && (
              <div className="rounded-lg border border-green-500/20 bg-green-500/5 px-3 py-2 text-xs text-green-500">
                {chatStatus}
              </div>
            )}
            {messageError && (
              <div className="rounded-lg border border-destructive/20 bg-destructive/5 px-3 py-2 text-xs text-destructive">
                {messageError}
              </div>
            )}
          </CardHeader>

          <ScrollArea className="min-h-0 flex-1">
            <div className="space-y-1 p-4">
              {loadingConversation && messages.length === 0 ? (
                <TypingIndicator />
              ) : messages.length > 0 ? (
                messages.map((message, index) => (
                  <MessageBubble
                    key={message.id || `${message.timestamp}-${index}`}
                    message={message}
                    showTimestamp
                  />
                ))
              ) : (
                <div className="flex h-full min-h-[320px] flex-col items-center justify-center gap-3 text-center text-muted-foreground">
                  <div className="flex h-14 w-14 items-center justify-center rounded-full bg-muted">
                    <Bot className="h-6 w-6" />
                  </div>
                  <div className="max-w-md space-y-1">
                    <div className="text-sm font-medium text-foreground">
                      No conversation turns yet
                    </div>
                    <p className="text-sm">
                      {activeMemoryScope === "session"
                        ? "Send a message to start a detached workbench session for this agent. The reply path still uses the layered memory pipeline, but it stays scoped to this browser session."
                        : "Send a message to anchor a long-lived companion thread for this agent. The reply path writes through the layered memory system used by agentctl."}
                    </p>
                  </div>
                </div>
              )}
              {chatSending && <TypingIndicator />}
            </div>
          </ScrollArea>

          <ChatInput
            onSend={(message) => void handleSend(message)}
            onCancel={() => cancelStreamMutation.mutate()}
            disabled={trashAgentMutation.isPending}
            inflight={chatSending}
            placeholder={`Message ${getAgentDisplayName(activeAgent)}...`}
          />
        </Card>

        <ScrollArea className="min-h-0 pr-2">
          <div className="space-y-4">
            <Card>
              <CardHeader className="pb-3">
                <div className="flex items-start justify-between gap-3">
                  <div>
                    <CardTitle className="text-sm">Layered Memory</CardTitle>
                    <CardDescription>
                      L0/L1/L2 context, summary coverage, and compaction
                      controls.
                    </CardDescription>
                  </div>
                  <Layers className="h-4 w-4 text-muted-foreground" />
                </div>
              </CardHeader>
              <CardContent className="space-y-3">
                <div className="grid grid-cols-2 gap-2">
                  <MemoryStat
                    label="Turns"
                    value={
                      memoryStats?.total_turns ??
                      (loadingMemoryStats ? "..." : 0)
                    }
                  />
                  <MemoryStat
                    label="Day Summaries"
                    value={
                      memoryStats?.day_summaries ??
                      (loadingMemoryStats ? "..." : 0)
                    }
                  />
                  <MemoryStat
                    label="Distilled"
                    value={memoryStats?.has_distilled_history ? "yes" : "no"}
                  />
                  <MemoryStat
                    label="Lineage"
                    value={
                      activeMemoryScope === "session"
                        ? "session"
                        : conversationExplicit
                          ? "explicit"
                          : "implicit"
                    }
                  />
                </div>
                <div className="rounded-lg border border-border bg-background/60 p-3 space-y-3">
                  <div className="flex items-center justify-between gap-3">
                    <div>
                      <div className="text-[11px] uppercase tracking-wider text-muted-foreground">
                        Workbench Memory Policy
                      </div>
                      <div className="mt-1 text-xs text-muted-foreground">
                        Retention presets shape how long memory should live.
                        Lineage scope stays explicitly overridable.
                      </div>
                    </div>
                    <Badge variant="outline" className="text-[10px]">
                      {activeMemoryRetention}
                    </Badge>
                  </div>
                  <div className="flex items-center gap-2">
                    <select
                      value={memoryRetentionDraft}
                      onChange={(e) => {
                        const retention = normalizeMemoryRetention(
                          e.target.value,
                        );
                        setMemoryRetentionDraft(retention);
                        setMemoryScopeDraft(
                          recommendedMemoryScopeForRetention(retention),
                        );
                      }}
                      className="h-9 flex-1 rounded-md border border-input bg-background px-3 text-sm"
                      disabled={memoryRetentionMutation.isPending}
                    >
                      <option value="companion">companion</option>
                      <option value="durable">durable</option>
                      <option value="task">task</option>
                      <option value="ephemeral">ephemeral</option>
                    </select>
                    <Button
                      type="button"
                      variant="outline"
                      size="sm"
                      onClick={() => void handleApplyMemoryRetention()}
                      disabled={
                        memoryRetentionMutation.isPending ||
                        memoryRetentionDraft === activeMemoryRetention
                      }
                    >
                      Apply
                    </Button>
                  </div>
                  <div className="text-[11px] text-muted-foreground">
                    {describeMemoryRetention(memoryRetentionDraft)}
                  </div>
                  <div className="flex items-center gap-2">
                    <select
                      value={memoryScopeDraft}
                      onChange={(e) =>
                        setMemoryScopeDraft(
                          e.target.value === "session" ? "session" : "agent",
                        )
                      }
                      className="h-9 flex-1 rounded-md border border-input bg-background px-3 text-sm"
                      disabled={memoryScopeMutation.isPending}
                    >
                      <option value="agent">agent</option>
                      <option value="session">session</option>
                    </select>
                    <Button
                      type="button"
                      variant="outline"
                      size="sm"
                      onClick={() => void handleApplyMemoryScope()}
                      disabled={
                        memoryScopeMutation.isPending ||
                        memoryScopeDraft === activeMemoryScope
                      }
                    >
                      Apply
                    </Button>
                  </div>
                </div>
                <div className="flex flex-wrap gap-2">
                  <Button
                    variant="outline"
                    size="sm"
                    onClick={() => {
                      if (showMemoryContext) {
                        setShowMemoryContext(false);
                        return;
                      }
                      setShowMemoryContext(true);
                      void loadMemoryContext();
                    }}
                  >
                    <Brain className="h-4 w-4" />
                    {showMemoryContext ? "Hide Context" : "Show Context"}
                  </Button>
                  <Button
                    variant="outline"
                    size="sm"
                    onClick={() => void loadMemoryContext(true)}
                    disabled={memoryContextLoading}
                  >
                    <RefreshCw
                      className={cn(
                        "h-4 w-4",
                        memoryContextLoading && "animate-spin",
                      )}
                    />
                    Refresh
                  </Button>
                  <Button
                    variant="outline"
                    size="sm"
                    onClick={() => void handleCompressMemory()}
                    disabled={compressing}
                  >
                    <Sparkles
                      className={cn("h-4 w-4", compressing && "animate-pulse")}
                    />
                    Compress
                  </Button>
                </div>
                {(memoryContextError || showMemoryContext) && (
                  <div className="rounded-lg border border-border bg-background/60 p-3">
                    {memoryContextError ? (
                      <div className="text-xs text-destructive">
                        {memoryContextError}
                      </div>
                    ) : memoryContextLoading ? (
                      <div className="text-xs text-muted-foreground">
                        Loading layered memory context...
                      </div>
                    ) : (
                      <pre className="max-h-[420px] whitespace-pre-wrap text-xs text-muted-foreground">
                        {memoryContext ||
                          "No layered memory context has been materialized yet."}
                      </pre>
                    )}
                  </div>
                )}
              </CardContent>
            </Card>

            <Card>
              <CardHeader className="pb-3">
                <div className="flex items-start justify-between gap-3">
                  <div>
                    <CardTitle className="text-sm">
                      Control Room Stream
                    </CardTitle>
                    <CardDescription>
                      Room-scoped coordination timeline for this agent subtree.
                    </CardDescription>
                  </div>
                  <div className="flex items-center gap-2">
                    <Badge variant="outline" className="text-[10px] font-mono">
                      {controlRoomID}
                    </Badge>
                    <Button
                      type="button"
                      variant="ghost"
                      size="icon"
                      className="h-8 w-8"
                      onClick={() => void controlRoomMessagesQuery.refetch()}
                      disabled={
                        controlRoomMessagesQuery.isFetching || !controlRoom
                      }
                    >
                      <RefreshCw
                        className={cn(
                          "h-4 w-4",
                          controlRoomMessagesQuery.isFetching && "animate-spin",
                        )}
                      />
                    </Button>
                  </div>
                </div>
              </CardHeader>
              <CardContent className="space-y-3">
                <div className="flex flex-wrap gap-2">
                  <Badge variant="secondary" className="text-[10px]">
                    {controlRoom
                      ? `${controlRoom.message_count} messages`
                      : "room missing"}
                  </Badge>
                  <Badge variant="outline" className="text-[10px]">
                    {controlRoomMembers.length} members
                  </Badge>
                  <Badge variant="outline" className="text-[10px]">
                    {effectiveDispatchTargets.length} dispatch target
                    {effectiveDispatchTargets.length === 1 ? "" : "s"}
                  </Badge>
                </div>
                <div className="rounded-lg border border-border bg-background/60 p-3 space-y-3">
                  <div className="grid gap-2">
                    <label className="text-[11px] uppercase tracking-wider text-muted-foreground">
                      Dispatch Targets
                    </label>
                    <select
                      value={dispatchTargetPolicy}
                      onChange={(e) =>
                        setDispatchTargetPolicy(
                          (e.target.value ||
                            "all_subtree") as DispatchTargetPolicy,
                        )
                      }
                      className="h-9 rounded-md border border-input bg-background px-3 text-sm"
                    >
                      <option value="all_subtree">all subtree</option>
                      <option value="children_only">children only</option>
                      <option value="lead_only">lead only</option>
                      <option value="selected">selected</option>
                    </select>
                  </div>
                  {dispatchTargetPolicy === "selected" && (
                    <div className="grid gap-2">
                      <div className="text-[11px] uppercase tracking-wider text-muted-foreground">
                        Selected Agents
                      </div>
                      <div className="max-h-40 space-y-2 overflow-y-auto pr-1">
                        {dispatchTargetOptions.map((target) => (
                          <label
                            key={`dispatch-target-${target.actor_id}`}
                            className="flex items-center gap-2 rounded border border-border bg-background px-2 py-2 text-xs"
                          >
                            <input
                              type="checkbox"
                              checked={selectedDispatchTargets.includes(
                                target.actor_id,
                              )}
                              onChange={() =>
                                toggleDispatchTarget(target.actor_id)
                              }
                            />
                            <span className="truncate font-mono">
                              {target.actor_id}
                            </span>
                            <span className="text-muted-foreground">
                              {target.role || "agent"}
                            </span>
                          </label>
                        ))}
                      </div>
                    </div>
                  )}
                </div>
                <div className="rounded-lg border border-border bg-background/60 p-3">
                  {controlRoom ? (
                    <div className="space-y-2">
                      {controlRoomTimeline.length ? (
                        controlRoomTimeline.slice(-8).map((message) => (
                          <div
                            key={message.key}
                            className="rounded bg-background/70 px-2 py-2"
                          >
                            <div className="flex items-center justify-between gap-2 text-[10px] uppercase tracking-wider text-muted-foreground">
                              <span className="truncate">
                                {message.sender || "unknown"}
                              </span>
                              <div className="flex items-center gap-2">
                                {message.pending && (
                                  <Badge
                                    variant="outline"
                                    className="text-[9px]"
                                  >
                                    live
                                  </Badge>
                                )}
                                <span>
                                  {formatRelativeTime(message.createdAt)}
                                </span>
                              </div>
                            </div>
                            {message.subject && (
                              <div className="mt-1 text-xs font-medium text-foreground">
                                {message.subject}
                              </div>
                            )}
                            <div className="mt-1 whitespace-pre-wrap text-xs text-muted-foreground">
                              {message.body ||
                                (message.pending ? "Thinking…" : "")}
                            </div>
                            {message.toolActivity.length > 0 && (
                              <div className="mt-2 space-y-1">
                                {message.toolActivity
                                  .slice(-2)
                                  .map((entry, index) => (
                                    <div
                                      key={`${message.key}-tool-${index}`}
                                      className="whitespace-pre-wrap text-[10px] text-muted-foreground/90"
                                    >
                                      {entry}
                                    </div>
                                  ))}
                              </div>
                            )}
                            {message.error && (
                              <div className="mt-2 text-[10px] text-destructive">
                                {message.error}
                              </div>
                            )}
                          </div>
                        ))
                      ) : (
                        <div className="text-xs text-muted-foreground">
                          No room messages yet. Use this stream for parent/child
                          coordination notes.
                        </div>
                      )}
                    </div>
                  ) : (
                    <div className="text-xs text-muted-foreground">
                      No control room exists yet. Sync the room to create a
                      subtree coordination stream.
                    </div>
                  )}
                </div>
                <div className="space-y-2">
                  <Textarea
                    value={roomDraft}
                    onChange={(e) => setRoomDraft(e.target.value)}
                    placeholder="Send a coordination note into the control room..."
                    className="min-h-[84px]"
                  />
                  <div className="flex items-center justify-between gap-2">
                    <div className="text-[11px] text-muted-foreground">
                      Sender: `human:gui`
                    </div>
                    <Button
                      type="button"
                      size="sm"
                      onClick={() => sendRoomMutation.mutate()}
                      disabled={
                        sendRoomMutation.isPending ||
                        !roomDraft.trim() ||
                        !activeAgent.ns
                      }
                    >
                      Send To Room
                    </Button>
                  </div>
                </div>
              </CardContent>
            </Card>

            <Card>
              <CardHeader className="pb-3">
                <div className="flex items-start justify-between gap-3">
                  <div>
                    <CardTitle className="text-sm">
                      Persisted Sessions
                    </CardTitle>
                    <CardDescription>
                      Durable session history tied to this agent and workspace.
                    </CardDescription>
                  </div>
                  <FileText className="h-4 w-4 text-muted-foreground" />
                </div>
              </CardHeader>
              <CardContent className="space-y-3">
                {persistedSessions.length > 0 ? (
                  <>
                    <div className="space-y-2">
                      {persistedSessions.slice(0, 6).map((session) => (
                        <button
                          key={session.id}
                          type="button"
                          onClick={() => setSelectedSessionID(session.id)}
                          className={cn(
                            "flex w-full items-start justify-between gap-3 rounded-lg border px-3 py-2 text-left transition-colors",
                            selectedSessionID === session.id
                              ? "border-primary bg-primary/5"
                              : "border-border bg-background/60 hover:bg-accent/40",
                          )}
                        >
                          <div className="min-w-0">
                            <div className="truncate text-sm font-medium text-foreground">
                              {session.id}
                            </div>
                            <div className="truncate text-xs text-muted-foreground">
                              {formatRelativeTime(session.started_at)} ·{" "}
                              {session.message_count} messages ·{" "}
                              {session.total_tokens} tokens
                            </div>
                          </div>
                          <Badge variant="outline">{session.status}</Badge>
                        </button>
                      ))}
                    </div>

                    {selectedSession && (
                      <div className="rounded-lg border border-border bg-background/60 p-3">
                        <div className="flex items-center justify-between gap-3">
                          <div className="min-w-0">
                            <div className="truncate text-sm font-medium text-foreground">
                              Session Detail
                            </div>
                            <div className="text-xs text-muted-foreground">
                              {selectedSession.agent_type ||
                                activeAgent.role ||
                                "agent"}{" "}
                              · {selectedSession.status}
                            </div>
                          </div>
                          <Badge variant="secondary" className="text-[10px]">
                            {selectedSession.user_turns} user turns
                          </Badge>
                        </div>
                        <div className="mt-3 space-y-2 text-xs text-muted-foreground">
                          {selectedSession.summary && (
                            <div>
                              <div className="mb-1 text-[11px] uppercase tracking-wider">
                                Summary
                              </div>
                              <div>{selectedSession.summary}</div>
                            </div>
                          )}
                          {selectedSession.decisions &&
                            selectedSession.decisions.length > 0 && (
                              <div>
                                <div className="mb-1 text-[11px] uppercase tracking-wider">
                                  Decisions
                                </div>
                                <ul className="space-y-1">
                                  {selectedSession.decisions
                                    .slice(0, 4)
                                    .map((item) => (
                                      <li
                                        key={item}
                                        className="rounded bg-background/70 px-2 py-1"
                                      >
                                        {item}
                                      </li>
                                    ))}
                                </ul>
                              </div>
                            )}
                          {selectedSession.gotchas &&
                            selectedSession.gotchas.length > 0 && (
                              <div>
                                <div className="mb-1 text-[11px] uppercase tracking-wider">
                                  Gotchas
                                </div>
                                <ul className="space-y-1">
                                  {selectedSession.gotchas
                                    .slice(0, 4)
                                    .map((item) => (
                                      <li
                                        key={item}
                                        className="rounded bg-background/70 px-2 py-1"
                                      >
                                        {item}
                                      </li>
                                    ))}
                                </ul>
                              </div>
                            )}
                          {selectedSessionMessages.length > 0 && (
                            <div>
                              <div className="mb-1 text-[11px] uppercase tracking-wider">
                                Recent Transcript
                              </div>
                              <div className="space-y-1">
                                {selectedSessionMessages
                                  .slice(-6)
                                  .map((message) => (
                                    <div
                                      key={`${selectedSession.id}-${message.index}`}
                                      className="rounded bg-background/70 px-2 py-1"
                                    >
                                      <div className="text-[10px] uppercase tracking-wider text-muted-foreground">
                                        {message.type} ·{" "}
                                        {formatRelativeTime(message.timestamp)}
                                      </div>
                                      <div className="mt-1 line-clamp-3 text-xs text-foreground">
                                        {sessionMessageSummary(message)}
                                      </div>
                                    </div>
                                  ))}
                              </div>
                            </div>
                          )}
                        </div>
                      </div>
                    )}
                  </>
                ) : (
                  <div className="rounded-lg border border-dashed border-border px-3 py-2 text-xs text-muted-foreground">
                    No persisted sessions have been recorded for this agent yet.
                  </div>
                )}
              </CardContent>
            </Card>

            <Card>
              <CardHeader className="pb-3">
                <CardTitle className="text-sm">Identity Context</CardTitle>
                <CardDescription>
                  What gets threaded into companion requests for this agent.
                </CardDescription>
              </CardHeader>
              <CardContent>
                <div className="space-y-2 rounded-lg border border-border bg-background/60 p-3 text-xs text-muted-foreground">
                  <div className="flex items-center gap-2">
                    <UserCircle2 className="h-3.5 w-3.5" />
                    Name: {getAgentDisplayName(activeAgent)}
                  </div>
                  <div className="flex items-center gap-2">
                    <Wrench className="h-3.5 w-3.5" />
                    Role: {activeAgent.role || "agent"}
                  </div>
                  <div className="flex items-center gap-2">
                    <Network className="h-3.5 w-3.5" />
                    Workspace: {activeAgent.ns || "/"}
                  </div>
                  <div className="flex items-center gap-2">
                    <Cpu className="h-3.5 w-3.5" />
                    Model: {activeAgent.llm_model || "default"}
                  </div>
                </div>
              </CardContent>
            </Card>
          </div>
        </ScrollArea>
      </div>
    </div>
  );
}
