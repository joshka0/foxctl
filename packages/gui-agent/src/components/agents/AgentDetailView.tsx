import {
  createElement,
  useCallback,
  useEffect,
  useMemo,
  useRef,
  useState,
} from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { ScrollArea } from "@/components/ui/scroll-area";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import { Input } from "@/components/ui/input";
import { HelpTooltip, Tooltip } from "@/components/ui/tooltip";
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
import { AgentDetailSupportRail } from "@/components/agents/AgentDetailSupportRail";
import {
  askAgentStream,
  cancelAgentStream,
  companionChat,
  createRoom,
  getAgent,
  getAgentRuntime,
  getCompanionCoChange,
  getCompanionConversationMessages,
  getCompanionMemoryStats,
  getRoom,
  listAgents,
  listCompanionConversations,
  listPersistedSessions,
  listRooms,
  listWorkspaces,
  patchAgent,
  spawnAgent,
  subscribeToRoomEvents,
  type CompanionMessage,
  type CompanionMemoryStats,
  type ConsoleMessage,
  type PersistedSession,
} from "@/api/client";
import type {
  Agent,
  AgentChatStreamEvent,
  AgentRuntimeTreeNode,
  Room,
} from "@/api/types";
import { useAgentOperations } from "@/hooks/useAgentOperations";
import { useViewStore } from "@/stores/viewStore";
import { cn, formatRelativeTime } from "@/lib/utils";
import {
  getAgentDisplayName,
  getAgentRepoDisplayName,
  getPromptSummaryOrSubtitle,
  getRoleIcon,
  isSandboxBackedAgent,
} from "@/lib/agent-utils";
import { resolveConversationIDForAgent } from "@/lib/conversation-utils";
import { resolveRoomWorkspacePath, roomDisplayName } from "@/lib/room-utils";
import {
  ArrowLeft,
  Bot,
  ChevronRight,
  Clock,
  Cpu,
  Folder,
  GitBranch,
  Link2,
  Play,
  RefreshCw,
  Sparkles,
  Square,
  Trash2,
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

function sessionConversationReadyStorageKey(agentID: string): string {
  return `${sessionConversationStorageKey(agentID)}:ready`;
}

function isBootstrapSessionConversationID(
  agentID: string,
  conversationID: string,
): boolean {
  return conversationID.startsWith(`agent-session-${agentID}`);
}

function loadSessionConversationState(agentID: string): {
  conversationID: string;
  ready: boolean;
} {
  const key = sessionConversationStorageKey(agentID);
  const readyKey = sessionConversationReadyStorageKey(agentID);
  if (typeof window === "undefined") {
    const conversationID = `agent-session-${agentID}`;
    return {
      conversationID,
      ready: !isBootstrapSessionConversationID(agentID, conversationID),
    };
  }
  const existing = window.sessionStorage.getItem(key);
  const conversationID =
    existing && existing.trim()
      ? existing
      : localRequestID(`agent-session-${agentID}`);
  if (!existing || !existing.trim()) {
    window.sessionStorage.setItem(key, conversationID);
  }
  const storedReady = window.sessionStorage.getItem(readyKey);
  if (storedReady === "1") {
    return { conversationID, ready: true };
  }
  if (storedReady === "0") {
    return { conversationID, ready: false };
  }
  return {
    conversationID,
    ready: !isBootstrapSessionConversationID(agentID, conversationID),
  };
}

function saveSessionConversationState(
  agentID: string,
  conversationID: string,
  ready: boolean,
) {
  if (typeof window === "undefined") return;
  window.sessionStorage.setItem(
    sessionConversationStorageKey(agentID),
    conversationID,
  );
  window.sessionStorage.setItem(
    sessionConversationReadyStorageKey(agentID),
    ready ? "1" : "0",
  );
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
  const roleIcon = getRoleIcon(agent.role);
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
        {createElement(roleIcon, { className: "h-4 w-4" })}
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
  foxctlStatus?: string;
  profile?: string;
  lastError?: string;
} {
  if (!node || !isRecord(node.state)) return {};
  const state = node.state;
  const summary: {
    foxctlStatus?: string;
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

  if (isRecord(state.foxctl)) {
    const foxctl = state.foxctl;
    if (typeof foxctl.status === "string" && foxctl.status.trim()) {
      summary.foxctlStatus = foxctl.status.trim();
    }
    if (typeof foxctl.last_error === "string" && foxctl.last_error.trim()) {
      summary.lastError = foxctl.last_error.trim();
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
          {summary.foxctlStatus && (
            <Badge variant="outline" className="text-[10px]">
              {summary.foxctlStatus}
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
  const setSelectedConversationID = useViewStore(
    (s) => s.setSelectedConversationID,
  );
  const setActiveView = useViewStore((s) => s.setActiveView);
  const setSelectedRoom = useViewStore((s) => s.setSelectedRoom);
  const eventSourceRef = useRef<EventSource | null>(null);
  const [messages, setMessages] = useState<ConsoleMessage[]>([]);
  const [messageError, setMessageError] = useState<string | null>(null);
  const [chatSending, setChatSending] = useState(false);
  const [chatStatus, setChatStatus] = useState<string | null>(null);
  const [activeCorrelationID, setActiveCorrelationID] = useState<string | null>(
    null,
  );
  const [sessionConversation, setSessionConversation] = useState(() =>
    loadSessionConversationState(agent.id),
  );
  const sessionConversationID = sessionConversation.conversationID;
  const sessionConversationReady = sessionConversation.ready;
  const [controlRoomBusyAgentID, setControlRoomBusyAgentID] = useState<
    string | null
  >(null);
  const [roomStatus, setRoomStatus] = useState<string | null>(null);
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
  const workspacePaths = useMemo(
    () => (workspacesData?.workspaces ?? []).map((workspace) => workspace.path),
    [workspacesData?.workspaces],
  );
  const currentWorkspacePath = workspacesData?.current;
  const roomWorkspacePath = useMemo(
    () =>
      resolveRoomWorkspacePath(
        activeAgent.ns,
        workspacePaths,
        currentWorkspacePath,
      ),
    [activeAgent.ns, currentWorkspacePath, workspacePaths],
  );
  const RoleIcon = getRoleIcon(activeAgent.role);
  const activeMemoryScope = normalizeMemoryScope(activeAgent.memory_scope);
  const activeMemoryRetention = normalizeMemoryRetention(
    activeAgent.memory_retention,
  );
  const sandboxBacked = isSandboxBackedAgent(activeAgent);
  const repoLabel = getAgentRepoDisplayName(activeAgent.repo_url);
  const [agentConversationIDOverride, setAgentConversationIDOverride] =
    useState<string | null>(null);

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
  const { data: companionConversationsData } = useQuery({
    queryKey: ["companion-conversations", "agent-detail"],
    queryFn: () => listCompanionConversations(200),
    staleTime: 30000,
    refetchInterval: 30000,
  });
  const linkedConversations = useMemo(
    () =>
      (companionConversationsData?.conversations ?? []).filter(
        (conversation) => conversation.agent_id === activeAgent.id,
      ),
    [activeAgent.id, companionConversationsData?.conversations],
  );
  const resolvedAgentConversationID = useMemo(() => {
    if (agentConversationIDOverride?.trim()) return agentConversationIDOverride;
    return resolveConversationIDForAgent(activeAgent, linkedConversations);
  }, [activeAgent, agentConversationIDOverride, linkedConversations]);
  const conversationID =
    activeMemoryScope === "session"
      ? sessionConversationID
      : resolvedAgentConversationID;
  const conversationExplicit =
    activeMemoryScope === "agent" && conversationID !== activeAgent.id;
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

  useEffect(() => {
    setSessionConversation(loadSessionConversationState(activeAgent.id));
    setAgentConversationIDOverride(null);
    setActiveCorrelationID(null);
    setRoomStatus(null);
    if (eventSourceRef.current) {
      eventSourceRef.current.close();
      eventSourceRef.current = null;
    }
  }, [activeAgent.id, activeAgent.memory_retention, activeAgent.memory_scope]);

  useEffect(() => {
    if (activeMemoryScope !== "session") return;
    if (!sessionConversationID.trim()) return;
    saveSessionConversationState(
      activeAgent.id,
      sessionConversationID,
      sessionConversationReady,
    );
  }, [
    activeAgent.id,
    activeMemoryScope,
    sessionConversationID,
    sessionConversationReady,
  ]);

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

  const {
    data: conversationMessages,
    isLoading: loadingConversation,
    refetch: refetchConversation,
  } = useQuery({
    queryKey: ["agent-conversation", conversationID],
    enabled: activeMemoryScope !== "session" || sessionConversationReady,
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
    enabled: activeMemoryScope !== "session" || sessionConversationReady,
    queryFn: () => getCompanionMemoryStats(conversationID),
    retry: false,
  });

  const cochangeQuery = (activeAgent.prompt_summary || activeAgent.role || "")
    .trim()
    .slice(0, 120);
  const {
    data: cochangeData,
    isLoading: loadingCoChange,
  } = useQuery({
    queryKey: ["agent-cochange", activeAgent.id, activeAgent.ns, cochangeQuery],
    enabled: Boolean(activeAgent.ns && cochangeQuery),
    queryFn: () =>
      getCompanionCoChange({
        workspace: activeAgent.ns || "",
        query: cochangeQuery,
        limit: 5,
      }),
    retry: false,
  });

  useEffect(() => {
    setMessages(conversationMessages || []);
    setMessageError(null);
    setChatStatus(null);
  }, [activeAgent.id, conversationID, conversationMessages]);

  const adoptConversationID = useCallback(
    async (nextConversationID: string, persistAgentLink = false) => {
      const normalizedID = nextConversationID.trim();
      if (!normalizedID) return;
      setSelectedConversationID(normalizedID);
      if (activeMemoryScope === "session") {
        if (
          normalizedID !== sessionConversationID ||
          !sessionConversationReady
        ) {
          setSessionConversation({
            conversationID: normalizedID,
            ready: true,
          });
          saveSessionConversationState(activeAgent.id, normalizedID, true);
        }
        return;
      }
      setAgentConversationIDOverride(normalizedID);
      if (!persistAgentLink || activeAgent.conversation_id === normalizedID) {
        return;
      }
      try {
        await patchAgent(activeAgent.id, { conversation_id: normalizedID });
        await Promise.all([
          queryClient.invalidateQueries({ queryKey: ["agents"] }),
          queryClient.invalidateQueries({ queryKey: ["agent", activeAgent.id] }),
          queryClient.invalidateQueries({
            queryKey: ["companion-conversations"],
          }),
          queryClient.invalidateQueries({
            queryKey: ["companion-conversations", "agent-detail"],
          }),
        ]);
      } catch (err) {
        console.warn(
          "[AgentDetailView] Failed to adopt canonical conversation id:",
          err,
        );
      }
    },
    [
      activeAgent.conversation_id,
      activeAgent.id,
      activeMemoryScope,
      queryClient,
      sessionConversationID,
      sessionConversationReady,
      setSelectedConversationID,
    ],
  );

  const ensureConversationLink = async () => {
    if (activeMemoryScope === "session") return;
    if (conversationExplicit) return;
    try {
      await patchAgent(activeAgent.id, { conversation_id: conversationID });
      setAgentConversationIDOverride(conversationID);
      setSelectedConversationID(conversationID);
      await Promise.all([
        queryClient.invalidateQueries({ queryKey: ["agents"] }),
        queryClient.invalidateQueries({ queryKey: ["agent", activeAgent.id] }),
        queryClient.invalidateQueries({
          queryKey: ["companion-conversations"],
        }),
        queryClient.invalidateQueries({
          queryKey: ["companion-conversations", "agent-detail"],
        }),
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
        ]);
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
    roomWorkspacePath,
  ]);

  useEffect(() => {
    const cleanup = subscribeToRoomEvents(controlRoomID, roomWorkspacePath, (_event) => {
      void Promise.all([
        queryClient.invalidateQueries({
          queryKey: ["rooms", roomWorkspacePath],
        }),
        queryClient.invalidateQueries({
          queryKey: ["agent-room", roomWorkspacePath, controlRoomID],
        }),
      ]);
    });
    return cleanup;
  }, [controlRoomID, queryClient, roomWorkspacePath]);

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

  const openControlRoomMutation = useMutation({
    mutationFn: async () => {
      if (!roomWorkspacePath) {
        throw new Error("Workspace is required to open the control room");
      }
      if (controlRoom) {
        return { room: controlRoom, created: false };
      }
      const result = await createRoom({
        workspace_id: roomWorkspacePath,
        id: controlRoomID,
        title: controlRoomTitle,
        description: `Coordination room for ${getAgentDisplayName(activeAgent)}`,
        dispatch_policy: "all_subtree",
        members: controlRoomMembers,
      });
      return { room: result.room, created: true };
    },
    onSuccess: async (result) => {
      if (result.room) {
        openRoom(result.room);
      }
      setRoomStatus(
        result.created
          ? "Control room created and opened in Rooms"
          : "Opened control room in Rooms",
      );
      await Promise.all([
        controlRoomQuery.refetch(),
        queryClient.invalidateQueries({ queryKey: ["rooms", roomWorkspacePath] }),
      ]);
    },
    onError: (error) => {
      setRoomStatus(
        error instanceof Error ? error.message : "Failed to open control room",
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
        response_keys: /\bjson\b/i.test(content)
          ? ["owner", "codename", "deploy_window", "rollback_color"]
          : undefined,
      });
      await adoptConversationID(
        response.conversation_id,
        activeMemoryScope === "agent",
      );
      if (!response.accepted) {
        setMessages((prev) =>
          applyStreamMessageUpdate(prev, correlationID, (message) => ({
            ...message,
            content:
              message.content?.trim() ||
              "[Pending reply. Live stream updates were not available.]",
            timestamp: new Date().toISOString(),
          })),
        );
        setChatSending(false);
        setActiveCorrelationID(null);
        setChatStatus("Agent reply queued without live stream updates");
        return;
      }
      setChatStatus("Streaming reply from agent runtime...");
    } catch {
      try {
        const response = await companionChat({
          conversation_id: conversationID,
          message: content,
          workspace: activeAgent.ns || "/",
          llm_provider: activeAgent.llm_provider || undefined,
          llm_model: activeAgent.llm_model || undefined,
          context: buildAgentContext(activeAgent),
          response_keys: /\bjson\b/i.test(content)
            ? ["owner", "codename", "deploy_window", "rollback_color"]
            : undefined,
        });
        await adoptConversationID(
          response.conversation_id,
          activeMemoryScope === "agent",
        );
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

  const handleOpenGlobalConversation = () => {
    setSelectedConversationID(conversationID);
    setActiveView("companion");
  };

  const handleOpenCompanionHistory = () => {
    setSelectedConversationID(null);
    setActiveView("companion");
  };

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
                <Badge
                  variant="outline"
                  className={cn(
                    sandboxBacked &&
                      "border-sky-500/30 bg-sky-500/5 text-sky-600",
                  )}
                >
                  {sandboxBacked ? "sandbox clone" : "local runtime"}
                </Badge>
                {sandboxBacked && activeAgent.sandbox_provider && (
                  <Badge variant="secondary">
                    {activeAgent.sandbox_provider}
                  </Badge>
                )}
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
                {repoLabel && (
                  <span className="inline-flex items-center gap-1">
                    <GitBranch className="h-3 w-3" />
                    {repoLabel}
                    {activeAgent.repo_ref ? ` @ ${activeAgent.repo_ref}` : ""}
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
                <div className="flex items-center gap-1.5">
                  <CardTitle className="text-sm">Agent Snapshot</CardTitle>
                  <HelpTooltip
                    side="top"
                    content="Quick summary of the agent's state, execution mode, workspace, and conversation lineage."
                  />
                </div>
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
                  <MemoryStat
                    label="Workspace"
                    value={sandboxBacked ? "sandbox" : "local"}
                  />
                  <MemoryStat
                    label="Sandbox"
                    value={
                      sandboxBacked
                        ? activeAgent.sandbox_provider || "disabled"
                        : "-"
                    }
                  />
                </div>
                <div className="rounded-lg border border-border bg-background/60 p-3">
                  <div className="inline-flex items-center gap-1 text-[11px] uppercase tracking-wider text-muted-foreground">
                    <span>Conversation Lineage</span>
                    <HelpTooltip
                      side="top"
                      content="The conversation ID root this workbench uses for companion chat and memory continuity."
                    />
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
                <div className="rounded-lg border border-border bg-background/60 p-3">
                  <div className="text-[11px] uppercase tracking-wider text-muted-foreground">
                    Execution Workspace
                  </div>
                  <div className="mt-1 text-xs text-muted-foreground">
                    {sandboxBacked
                      ? "Provisioned repo clone owned by the sandbox-backed execution path."
                      : "Uses the local foxctl runtime workspace and namespace."}
                  </div>
                  <div className="mt-2 grid gap-1 text-[11px] text-muted-foreground">
                    <div>
                      mode <code>{sandboxBacked ? "sandbox" : "local"}</code>
                    </div>
                    {repoLabel && (
                      <div>
                        repo{" "}
                        <code>
                          {repoLabel}
                          {activeAgent.repo_ref ? ` @ ${activeAgent.repo_ref}` : ""}
                        </code>
                      </div>
                    )}
                    {activeAgent.workspace_root && (
                      <div>
                        root <code>{activeAgent.workspace_root}</code>
                      </div>
                    )}
                    {activeAgent.sandbox_id && (
                      <div>
                        sandbox <code>{activeAgent.sandbox_id}</code>
                      </div>
                    )}
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
                    <div className="flex items-center gap-1.5">
                      <CardTitle className="text-sm">Live Runtime</CardTitle>
                      <HelpTooltip
                        side="top"
                        content="Live runtime subtree from the Jido bridge, including child agents and current execution state."
                      />
                    </div>
                    <CardDescription>
                      Jido runtime tree, child hierarchy, and current bridge
                      state.
                    </CardDescription>
                  </div>
                  <Tooltip content="Refresh the live runtime subtree for this agent.">
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
                  </Tooltip>
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
                    label="Foxctl"
                    value={runtimeSummary.foxctlStatus || "-"}
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
                <div className="flex items-center gap-1.5">
                  <CardTitle className="text-sm">Hierarchy</CardTitle>
                  <HelpTooltip
                    side="top"
                    content="Shows where this agent sits in the parent/child tree and which immediate workers are attached."
                  />
                </div>
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
                    <div className="flex items-center gap-1.5">
                      <CardTitle className="text-sm">Child Agents</CardTitle>
                      <HelpTooltip
                        side="top"
                        content="Spawn and manage immediate child agents from this parent workbench."
                      />
                    </div>
                    <CardDescription>
                      Spawn child agents and manage immediate workers from this
                      parent surface.
                    </CardDescription>
                  </div>
                  <Workflow className="h-4 w-4 text-muted-foreground" />
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
                <div className="flex items-center gap-1.5">
                  <CardTitle className="text-sm">Agent Conversation</CardTitle>
                  <HelpTooltip
                    side="top"
                    content="Human-facing chat thread for this agent. Depending on memory scope, it uses either a detached session thread or the agent's stable conversation lineage."
                  />
                </div>
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
                        : "Send a message to anchor a long-lived companion thread for this agent. The reply path writes through the layered memory system used by foxctl."}
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

        <AgentDetailSupportRail
          activeAgent={activeAgent}
          activeMemoryScope={activeMemoryScope}
          activeMemoryRetention={activeMemoryRetention}
          conversationExplicit={conversationExplicit}
          memoryStats={memoryStats ?? null}
          loadingMemoryStats={loadingMemoryStats}
          cochangeHits={cochangeData?.cochange_hits ?? []}
          cochangeLoading={loadingCoChange}
          controlRoom={controlRoom}
          controlRoomID={controlRoomID}
          controlRoomMembersCount={controlRoomMembers.length}
          roomWorkspacePath={roomWorkspacePath}
          openControlRoomPending={openControlRoomMutation.isPending}
          onOpenControlRoom={() => openControlRoomMutation.mutate()}
          onOpenRooms={() => setActiveView("rooms")}
          onOpenCompanionMemory={handleOpenGlobalConversation}
          persistedSessions={persistedSessions}
          onOpenCompanionHistory={handleOpenCompanionHistory}
        />
      </div>
    </div>
  );
}
