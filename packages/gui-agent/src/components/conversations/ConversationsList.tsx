import React, {
  useState,
  useEffect,
  useRef,
  useCallback,
  useMemo,
} from "react";
import { useQuery } from "@tanstack/react-query";
import { ScrollArea } from "@/components/ui/scroll-area";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import { Card } from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import { cn, formatRelativeTime } from "@/lib/utils";
import {
  listCompanionConversations,
  getCompanionConversationMessages,
  getCompanionPersonality,
  updatePersonalityDimension,
  getCompanionConversationSettings,
  patchCompanionConversationSettings,
  deleteCompanionConversationSettings,
  createConsoleSession,
  getConsoleSession,
  askConsoleSession,
  cancelConsoleSession,
  companionChat,
  listAgents,
  listPersistedSessions,
  getSessionMessages,
  deleteCompanionConversation,
  deleteCompanionMessage,
  renameCompanionConversation,
  compressCompanionConversation,
  type CompanionCompressionResult,
  getProviderAvailability,
  getCompanionMemoryStats,
  getCompanionMemoryContext,
  type CompanionMemoryStats,
  type ConversationSettings,
  type ConversationSettingsPatch,
  type ConsoleMessage,
  type ConsoleSession,
  type PersonalityInfo,
  type PersistedSession,
  type SessionMessage,
} from "@/api/client";
import type { Agent } from "@/api/types";
import {
  PROVIDERS,
  getModelsForProvider,
  COMPANION_TOOL_MODELS,
  COMPANION_RESPONSE_MODELS,
} from "@/components/agents/spawnFormConstants";
import { ChatInput } from "@/components/chat/ChatInput";
import {
  MessageBubble,
  TypingIndicator,
} from "@/components/chat/MessageBubble";
import {
  MessageCircle,
  RefreshCw,
  Search,
  Hash,
  MessagesSquare,
  Plus,
  Bot,
  PanelRightOpen,
  PanelRightClose,
  Cpu,
  FileText,
  Settings2,
  X,
  Sparkles,
  Trash2,
  Pencil,
  Check,
  Coins,
  Sliders,
  Save,
  RotateCcw,
  ChevronDown,
  ChevronRight,
  Square,
  Activity,
  Zap,
  Brain,
  Bug,
  Play,
  Clock,
  Folder,
} from "lucide-react";
import { Textarea } from "@/components/ui/textarea";
import { Slider } from "@/components/ui/slider";
import { useViewStore } from "@/stores/viewStore";
import { CollapsibleSection } from "@/components/ui/collapsible-section";
import { useAgentOperations } from "@/hooks/useAgentOperations";
import type { AgentSession } from "@/api/types";
import { ToolAllowlistEditor } from "@/components/conversations/ToolAllowlistEditor";
import {
  getRoleIcon,
  getAgentDisplayName,
  getPromptSummaryOrSubtitle,
  isWorkerAgent,
} from "@/lib/agent-utils";

const API_BASE = "/api";

interface Conversation {
  id: string;
  title?: string;
  name?: string; // Custom title from database
  agent_id?: string; // Linked agent ID (stored on conversation side)
  created_at: string;
  updated_at: string;
  message_count: number;
}

interface ToolCallInfo {
  name: string;
  args?: string;
  result?: string;
  injectedContext?: string; // Context injected by hooks after tool execution
}

interface ContextInfo {
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

type ExecMode = "" | "reactive" | "autonomous" | "proactive" | "story";
type FeedItem =
  | {
      kind: "companion";
      conversation: Conversation;
      agent?: Agent;
      sortAt: number;
    }
  | { kind: "session"; session: PersistedSession; sortAt: number };

const DEFAULT_OPENROUTER_MODEL = "mistralai/devstral-2512";

interface AgentConversationGroupProps {
  agent: Agent;
  conversations: Conversation[];
  isExpanded: boolean;
  hasSelectedConversation: boolean;
  selectedConversationId?: string;
  agents: Agent[];
  editingConversationId: string | null;
  editTitle: string;
  onEditTitleChange: (value: string) => void;
  editLinkedAgentId: string;
  onEditLinkedAgentIdChange: (value: string) => void;
  onToggleExpanded: (agentId: string) => void;
  onSelectAgent: (agent: Agent) => void;
  onNewConversationWithAgent: (agent: Agent) => void;
  onSelectConversation: (conversation: Conversation) => void;
  onSaveRename: (
    e: React.MouseEvent | React.KeyboardEvent,
    conversationId: string,
  ) => void;
  onCancelRename: (e: React.MouseEvent | React.KeyboardEvent) => void;
  onStartRename: (e: React.MouseEvent, conversation: Conversation) => void;
  onDeleteConversation: (e: React.MouseEvent, conversationId: string) => void;
}

function AgentConversationGroup({
  agent,
  conversations,
  isExpanded,
  hasSelectedConversation,
  selectedConversationId,
  agents,
  editingConversationId,
  editTitle,
  onEditTitleChange,
  editLinkedAgentId,
  onEditLinkedAgentIdChange,
  onToggleExpanded,
  onSelectAgent,
  onNewConversationWithAgent,
  onSelectConversation,
  onSaveRename,
  onCancelRename,
  onStartRename,
  onDeleteConversation,
}: AgentConversationGroupProps) {
  return (
    <div>
      <div
        className={cn(
          "flex items-center gap-2 px-2 py-2 rounded-lg cursor-pointer transition-colors group",
          "hover:bg-accent/50",
          hasSelectedConversation && "bg-accent/30",
        )}
        onClick={() => {
          onToggleExpanded(agent.id);
          onSelectAgent(agent);
        }}
      >
        <div className="flex-shrink-0 w-4 h-4 flex items-center justify-center">
          {isExpanded ? (
            <ChevronDown className="h-3.5 w-3.5 text-muted-foreground" />
          ) : (
            <ChevronRight className="h-3.5 w-3.5 text-muted-foreground" />
          )}
        </div>

        <div
          className={cn(
            "h-7 w-7 rounded-lg flex items-center justify-center flex-shrink-0",
            agent.state === "running" ? "bg-green-500/20" : "bg-primary/10",
          )}
        >
          {React.createElement(getRoleIcon(agent.role), {
            className: cn(
              "h-3.5 w-3.5",
              agent.state === "running" ? "text-green-500" : "text-primary",
            ),
          })}
        </div>

        <div className="flex-1 min-w-0">
          <div className="flex items-center gap-1.5">
            <span className="text-sm font-medium truncate">
              {getAgentDisplayName(agent)}
            </span>
            <Badge
              variant="secondary"
              className="text-[9px] px-1 py-0 flex-shrink-0"
            >
              {conversations.length}
            </Badge>
            {agent.state === "running" && (
              <span className="h-1.5 w-1.5 rounded-full bg-green-500 animate-pulse" />
            )}
          </div>
          <div className="text-[10px] text-muted-foreground truncate">
            {getPromptSummaryOrSubtitle(agent)}
          </div>
        </div>

        <Button
          variant="ghost"
          size="icon"
          className="h-6 w-6 opacity-0 group-hover:opacity-100 transition-opacity flex-shrink-0"
          onClick={(e) => {
            e.stopPropagation();
            onNewConversationWithAgent(agent);
          }}
          title="New chat with this agent"
        >
          <Plus className="h-3.5 w-3.5" />
        </Button>
      </div>

      {isExpanded && (
        <div className="ml-6 pl-2 border-l border-border/50 space-y-0.5 mt-0.5">
          {conversations.length === 0 && (
            <div
              className="flex items-center gap-2 px-2 py-1.5 rounded-md cursor-pointer text-muted-foreground hover:text-foreground hover:bg-accent/50 transition-colors"
              onClick={(e) => {
                e.stopPropagation();
                onNewConversationWithAgent(agent);
              }}
            >
              <Plus className="h-3 w-3" />
              <span className="text-xs">Start a conversation</span>
            </div>
          )}
          {conversations.map((conversation) => (
            <div
              key={conversation.id}
              className={cn(
                "flex items-center gap-2 px-2 py-1.5 rounded-md cursor-pointer transition-colors group",
                "hover:bg-accent/50",
                selectedConversationId === conversation.id &&
                  "bg-accent border-l-2 border-primary -ml-0.5 pl-2.5",
              )}
              onClick={() => onSelectConversation(conversation)}
            >
              <MessageCircle className="h-3 w-3 text-muted-foreground flex-shrink-0" />
              <div className="flex-1 min-w-0">
                {editingConversationId === conversation.id ? (
                  <div
                    className="space-y-1"
                    onClick={(e) => e.stopPropagation()}
                  >
                    <Input
                      value={editTitle}
                      onChange={(e) => onEditTitleChange(e.target.value)}
                      onKeyDown={(e) => {
                        if (e.key === "Enter") onSaveRename(e, conversation.id);
                        if (e.key === "Escape") onCancelRename(e);
                      }}
                      className="h-5 text-xs py-0 px-1"
                      placeholder="Title..."
                      autoFocus
                    />
                    <div className="flex items-center gap-1">
                      <select
                        value={editLinkedAgentId}
                        onChange={(e) =>
                          onEditLinkedAgentIdChange(e.target.value)
                        }
                        className="flex-1 h-5 text-[10px] bg-muted border border-border rounded px-1"
                      >
                        <option value="">No agent</option>
                        {agents.map((a) => (
                          <option key={a.id} value={a.id}>
                            {getAgentDisplayName(a)}
                          </option>
                        ))}
                      </select>
                      <Button
                        variant="ghost"
                        size="icon"
                        className="h-5 w-5"
                        onClick={(e) => onSaveRename(e, conversation.id)}
                      >
                        <Check className="h-3 w-3 text-green-500" />
                      </Button>
                      <Button
                        variant="ghost"
                        size="icon"
                        className="h-5 w-5"
                        onClick={onCancelRename}
                      >
                        <X className="h-3 w-3" />
                      </Button>
                    </div>
                  </div>
                ) : (
                  <div className="flex items-center gap-1.5">
                    <span className="text-xs truncate">
                      {conversation.name || conversation.id.slice(0, 12)}
                    </span>
                    <Badge
                      variant="secondary"
                      className="text-[9px] px-1 py-0 flex-shrink-0"
                    >
                      {conversation.message_count}
                    </Badge>
                  </div>
                )}
              </div>
              {editingConversationId !== conversation.id && (
                <div className="flex items-center gap-0.5 opacity-0 group-hover:opacity-100 transition-opacity flex-shrink-0">
                  <Button
                    variant="ghost"
                    size="icon"
                    className="h-5 w-5"
                    onClick={(e) => onStartRename(e, conversation)}
                    title="Rename"
                  >
                    <Pencil className="h-2.5 w-2.5 text-muted-foreground" />
                  </Button>
                  <Button
                    variant="ghost"
                    size="icon"
                    className="h-5 w-5"
                    onClick={(e) => onDeleteConversation(e, conversation.id)}
                    title="Delete"
                  >
                    <Trash2 className="h-2.5 w-2.5 text-muted-foreground" />
                  </Button>
                </div>
              )}
            </div>
          ))}
        </div>
      )}
    </div>
  );
}

function CompanionFeedRow({
  conversation,
  agent,
  selected,
  onClick,
}: {
  conversation: Conversation;
  agent?: Agent;
  selected: boolean;
  onClick: () => void;
}) {
  return (
    <button
      type="button"
      className={cn(
        "w-full text-left flex items-center gap-2 px-2 py-2 rounded-md transition-colors",
        "hover:bg-accent/50",
        selected && "bg-accent border-l-2 border-primary",
      )}
      onClick={onClick}
    >
      <MessageCircle className="h-3.5 w-3.5 text-muted-foreground flex-shrink-0" />
      <div className="flex-1 min-w-0">
        <div className="flex items-center gap-1.5">
          <span className="text-xs font-medium truncate">
            {conversation.name ||
              conversation.title ||
              conversation.id.slice(0, 16)}
          </span>
          <Badge
            variant="secondary"
            className="text-[9px] px-1 py-0 flex-shrink-0"
          >
            {conversation.message_count}
          </Badge>
        </div>
        <div className="text-[10px] text-muted-foreground truncate">
          {agent ? getAgentDisplayName(agent) : "Companion chat"} •{" "}
          {formatRelativeTime(conversation.updated_at)}
        </div>
      </div>
    </button>
  );
}

function SessionFeedRow({
  session,
  selected,
  onClick,
}: {
  session: PersistedSession;
  selected: boolean;
  onClick: () => void;
}) {
  const title =
    session.project_name ||
    session.workspace_path.split("/").pop() ||
    "Session";
  return (
    <button
      type="button"
      className={cn(
        "w-full text-left flex items-center gap-2 px-2 py-2 rounded-md transition-colors",
        "hover:bg-accent/50",
        selected && "bg-accent border-l-2 border-primary",
      )}
      onClick={onClick}
    >
      <FileText className="h-3.5 w-3.5 text-blue-500 flex-shrink-0" />
      <div className="flex-1 min-w-0">
        <div className="flex items-center gap-1.5">
          <span className="text-xs font-medium truncate">{title}</span>
          <Badge
            variant="secondary"
            className="text-[9px] px-1 py-0 flex-shrink-0"
          >
            {session.message_count}
          </Badge>
        </div>
        <div className="text-[10px] text-muted-foreground truncate">
          {session.summary || session.status || "Historical session"} •{" "}
          {formatRelativeTime(session.started_at)}
        </div>
      </div>
    </button>
  );
}

export function ConversationsList() {
  const setSelectedAgent = useViewStore((s) => s.setSelectedAgent);
  const [searchQuery, setSearchQuery] = useState("");
  const [selectedConversation, setSelectedConversation] =
    useState<Conversation | null>(null);
  const [selectedPersistedSession, setSelectedPersistedSession] =
    useState<PersistedSession | null>(null);
  const [messages, setMessages] = useState<ConsoleMessage[]>([]);
  const [inflight, setInflight] = useState(false);
  const [sessionId, setSessionId] = useState<string | null>(null);
  const [session, setSession] = useState<ConsoleSession | null>(null);
  const [isLoadingMessages, setIsLoadingMessages] = useState(false);
  const [linkedAgent, setLinkedAgent] = useState<Agent | null>(null);
  const [showContextPanel, setShowContextPanel] = useState(false);
  const [contextInfo, setContextInfo] = useState<ContextInfo>({});
  const [personalityInfo, setPersonalityInfo] =
    useState<PersonalityInfo | null>(null);
  const [selectedMessage, setSelectedMessage] = useState<ConsoleMessage | null>(
    null,
  );
  const [editingConversationId, setEditingConversationId] = useState<
    string | null
  >(null);
  const [editTitle, setEditTitle] = useState("");
  const [editLinkedAgentId, setEditLinkedAgentId] = useState<string>("");
  const [selectedAgentForNew, setSelectedAgentForNew] = useState<string>("");
  const [expandedAgents, setExpandedAgents] = useState<Set<string>>(new Set());
  // Model HUD state
  const [editingSystemPrompt, setEditingSystemPrompt] = useState(false);
  // Debounce timer refs for personality sliders
  const personalityDebounceRefs = useRef<
    Map<string, ReturnType<typeof setTimeout>>
  >(new Map());
  // Cleanup personality debounce timers on unmount
  useEffect(() => {
    const refs = personalityDebounceRefs;
    return () => {
      refs.current.forEach((timer) => clearTimeout(timer));
      refs.current.clear();
    };
  }, []);
  const [systemPromptDraft, setSystemPromptDraft] = useState("");
  const [selectedProvider, setSelectedProvider] = useState("");
  const [selectedModel, setSelectedModel] = useState("");
  const [customModelEnabled, setCustomModelEnabled] = useState(false);
  const [customModel, setCustomModel] = useState("");
  // Persisted per-conversation settings (models, exec mode, tool allowlist)
  const [conversationSettings, setConversationSettings] =
    useState<ConversationSettings | null>(null);
  const [settingsError, setSettingsError] = useState<string | null>(null);
  const [execModeOverride, setExecModeOverride] = useState<ExecMode>("");
  const [toolsAllowDraft, setToolsAllowDraft] = useState<string[]>([]);
  // Companion 2-stage model configuration
  const [toolModel, setToolModel] = useState("");
  const [responseModel, setResponseModel] = useState("");
  const [maxHistoryTurns, setMaxHistoryTurns] = useState(50);
  const [isCompressing, setIsCompressing] = useState(false);
  const [lastCompression, setLastCompression] =
    useState<CompanionCompressionResult | null>(null);
  // Compression model (separate from chat provider/model)
  const [compressionProvider, setCompressionProvider] = useState("");
  const [compressionModel, setCompressionModel] = useState("");
  // Provider availability from server
  const [providerAvailability, setProviderAvailability] = useState<
    Map<string, boolean>
  >(new Map());
  const [defaultProvider, setDefaultProvider] = useState<string>("");
  // Memory stats
  const [memoryStats, setMemoryStats] = useState<CompanionMemoryStats | null>(
    null,
  );
  const [memoryContext, setMemoryContext] = useState<string | null>(null);
  const [showMemoryContext, setShowMemoryContext] = useState(false);
  const [agentSectionOpen, setAgentSectionOpen] = useState(false);

  // Agent operations (start/stop/delete, sessions) — replaces AgentHUD
  const agentOps = useAgentOperations(linkedAgent);

  const scrollRef = useRef<HTMLDivElement>(null);
  const eventSourceRef = useRef<EventSource | null>(null);
  const modelInitKeyRef = useRef<string>("");
  const settingsRevisionRef = useRef(0);
  // Track agent linked locally (before DB refetch completes)
  const pendingLinkedAgentRef = useRef<Agent | null>(null);

  // Fetch conversations
  const { data, isLoading, refetch, isFetching } = useQuery({
    queryKey: ["companion-conversations"],
    queryFn: () => listCompanionConversations(100),
    refetchInterval: 30000,
  });

  const { data: sessionsData, refetch: refetchSessions } = useQuery({
    queryKey: ["persisted-sessions"],
    queryFn: () => listPersistedSessions({ limit: 200 }),
    refetchInterval: 30000,
  });

  // Fetch agents to find linked agent
  const { data: agentsData } = useQuery({
    queryKey: ["agents"],
    queryFn: () => listAgents(100),
    staleTime: 60000,
  });

  // Fetch provider availability on mount
  useEffect(() => {
    getProviderAvailability()
      .then((data) => {
        const map = new Map(data.providers.map((p) => [p.id, p.available]));
        setProviderAvailability(map);
        setDefaultProvider(data.default_provider);
      })
      .catch(() => {}); // silent fail - all providers shown as available if endpoint unreachable
  }, []);

  const conversations = useMemo(
    () => data?.conversations ?? [],
    [data?.conversations],
  );
  const persistedSessions = useMemo(
    () => sessionsData?.sessions ?? [],
    [sessionsData?.sessions],
  );
  const agents = useMemo(() => agentsData?.agents ?? [], [agentsData?.agents]);

  const filteredConversations = searchQuery
    ? conversations.filter(
        (c) =>
          c.id.toLowerCase().includes(searchQuery.toLowerCase()) ||
          c.title?.toLowerCase().includes(searchQuery.toLowerCase()),
      )
    : conversations;

  // Group conversations by agent
  const groupedConversations = React.useMemo(() => {
    const agentGroups: Map<
      string,
      { agent: Agent; conversations: Conversation[] }
    > = new Map();

    // Build the list of conversations to group — include the selectedConversation
    // placeholder if it's not yet in the API list (e.g. just created, no messages sent)
    const knownIds = new Set(filteredConversations.map((c) => c.id));
    const allConversations = [...filteredConversations];
    if (selectedConversation && !knownIds.has(selectedConversation.id)) {
      allConversations.push(selectedConversation);
    }

    // If the current conversation is locally linked to an agent (e.g. just created via
    // "Start a conversation"), use that as a fallback — the agent's DB conversation_id
    // may not have been refetched yet or may point to an older conversation.
    const localLinkedConvId =
      selectedConversation && linkedAgent ? selectedConversation.id : null;

    allConversations.forEach((conv) => {
      // Match by conversation-side agent_id (many-to-one), then agent-side conversation_id (legacy)
      let matchedAgent = agents.find(
        (a) => conv.agent_id === a.id || a.conversation_id === conv.id,
      );
      if (!matchedAgent && localLinkedConvId === conv.id && linkedAgent) {
        matchedAgent = linkedAgent;
      }

      if (matchedAgent) {
        const key = matchedAgent.id;
        if (!agentGroups.has(key)) {
          agentGroups.set(key, { agent: matchedAgent, conversations: [] });
        }
        agentGroups.get(key)!.conversations.push(conv);
      }
    });

    return {
      agentGroups: Array.from(agentGroups.values()),
    };
  }, [filteredConversations, agents, selectedConversation, linkedAgent]);

  const filteredAgents = useMemo(() => {
    if (!searchQuery) return agents;
    const q = searchQuery.toLowerCase();
    return agents.filter(
      (a) =>
        (a.name || "").toLowerCase().includes(q) ||
        (a.slug || "").toLowerCase().includes(q) ||
        (a.role || "").toLowerCase().includes(q) ||
        a.id.toLowerCase().includes(q),
    );
  }, [agents, searchQuery]);

  const agentSections = useMemo(() => {
    const sections = {
      active: [] as Agent[],
      errored: [] as Agent[],
    };

    for (const agent of filteredAgents) {
      if (isWorkerAgent(agent)) continue;
      const state = (agent.state || "").toLowerCase();
      if (state === "running" || state === "idle") {
        sections.active.push(agent);
        continue;
      }
      if (state === "error") {
        sections.errored.push(agent);
      }
    }
    return sections;
  }, [filteredAgents]);

  const feedItems = useMemo(() => {
    const lowerQuery = searchQuery.trim().toLowerCase();
    const matchesQuery = (...values: Array<string | undefined>): boolean => {
      if (!lowerQuery) return true;
      return values.some((value) =>
        (value || "").toLowerCase().includes(lowerQuery),
      );
    };

    const knownAgentIDs = new Set(agents.map((agent) => agent.id));
    const items: FeedItem[] = [];

    for (const conversation of conversations) {
      if (conversation.message_count <= 0) continue;
      const agent = conversation.agent_id
        ? agents.find((a) => a.id === conversation.agent_id)
        : agents.find((a) => a.conversation_id === conversation.id);
      if (
        !matchesQuery(
          conversation.id,
          conversation.name,
          conversation.title,
          agent ? getAgentDisplayName(agent) : undefined,
        )
      ) {
        continue;
      }
      items.push({
        kind: "companion",
        conversation,
        agent,
        sortAt: Date.parse(conversation.updated_at) || 0,
      });
    }

    for (const persisted of persistedSessions) {
      if (persisted.message_count <= 0) continue;
      if (persisted.agent_id && knownAgentIDs.has(persisted.agent_id)) continue;
      if (
        !matchesQuery(
          persisted.id,
          persisted.project_name,
          persisted.workspace_path,
          persisted.summary,
          persisted.status,
        )
      ) {
        continue;
      }
      items.push({
        kind: "session",
        session: persisted,
        sortAt: Date.parse(persisted.started_at) || 0,
      });
    }

    items.sort((a, b) => b.sortAt - a.sortAt);
    return items;
  }, [searchQuery, agents, conversations, persistedSessions]);

  // Auto-scroll to bottom on new messages
  useEffect(() => {
    if (scrollRef.current) {
      scrollRef.current.scrollTop = scrollRef.current.scrollHeight;
    }
  }, [messages, inflight]);

  // Find linked agent when conversation changes
  useEffect(() => {
    let cancelled = false;

    if (!selectedConversation) {
      setLinkedAgent(null);
      pendingLinkedAgentRef.current = null;
      modelInitKeyRef.current = "";
      setConversationSettings(null);
      setSettingsError(null);
      setExecModeOverride("");
      setToolsAllowDraft([]);
      setToolModel("");
      setResponseModel("");
      return;
    }

    // Check conversation-side link first (many-to-one), then agent-side (legacy)
    const agent = selectedConversation.agent_id
      ? agents.find((a) => a.id === selectedConversation.agent_id)
      : agents.find((a) => a.conversation_id === selectedConversation.id);
    // Fall back to the pending linked agent (set by handleNewConversationWithAgent
    // before the DB refetch completes)
    const resolved = agent || pendingLinkedAgentRef.current;
    setLinkedAgent(resolved || null);

    // Initialize settings + model overrides once per (conversation, agent) pair.
    const initKey = `${selectedConversation.id}:${agent?.id || ""}`;
    if (modelInitKeyRef.current !== initKey) {
      modelInitKeyRef.current = initKey;

      setConversationSettings(null);
      setSettingsError(null);
      setExecModeOverride("");
      setToolsAllowDraft([]);
      setToolModel("");
      setResponseModel("");

      const initProviderModel = (provider: string, model: string) => {
        const providerCfg = PROVIDERS.find((p) => p.id === provider);
        const knownModels = getModelsForProvider(provider);

        setSelectedProvider(provider);

        if (
          providerCfg?.allowCustom &&
          model &&
          !knownModels.some((m) => m.id === model)
        ) {
          setCustomModelEnabled(true);
          setCustomModel(model);
          setSelectedModel("");
        } else {
          setCustomModelEnabled(false);
          setCustomModel("");
          setSelectedModel(model);
        }
      };

      // Immediate fallback to linked agent defaults while settings load.
      initProviderModel(
        resolved?.llm_provider || "",
        resolved?.llm_model || "",
      );

      const conversationID = selectedConversation.id;
      const expectedRevision = settingsRevisionRef.current;
      void (async () => {
        try {
          const data = await getCompanionConversationSettings(conversationID);
          if (cancelled || settingsRevisionRef.current !== expectedRevision)
            return;

          let settings = data.settings;
          // If OpenRouter was selected previously without a model, default it to Devstral.
          if (
            (settings.llm_provider || "").trim() === "openrouter" &&
            (settings.llm_model || "").trim() === ""
          ) {
            try {
              const patched = await patchCompanionConversationSettings(
                conversationID,
                {
                  llm_model: DEFAULT_OPENROUTER_MODEL,
                },
              );
              if (cancelled || settingsRevisionRef.current !== expectedRevision)
                return;
              settingsRevisionRef.current += 1;
              settings = patched.settings;
            } catch (err) {
              // Non-fatal: keep existing settings and let the user pick a model manually.
              console.warn("Failed to default OpenRouter model:", err);
            }
          }

          setConversationSettings(settings);

          const providerOverride = (settings.llm_provider || "").trim();
          const modelOverride = (settings.llm_model || "").trim();
          initProviderModel(
            providerOverride || resolved?.llm_provider || "",
            modelOverride || resolved?.llm_model || "",
          );

          const nextExecMode = (settings.exec_mode || "").trim();
          if (
            nextExecMode === "reactive" ||
            nextExecMode === "autonomous" ||
            nextExecMode === "proactive" ||
            nextExecMode === "story"
          ) {
            setExecModeOverride(nextExecMode);
          } else {
            setExecModeOverride("");
          }
          setToolModel((settings.story_gather_model || "").trim());
          setResponseModel((settings.story_dialogue_model || "").trim());
          setToolsAllowDraft(settings.tools_allow || []);
        } catch (err) {
          if (cancelled) return;
          console.warn("Failed to load conversation settings:", err);
          setConversationSettings(null);
          setSettingsError(null);
          setExecModeOverride("");
          setToolModel("");
          setResponseModel("");
          setToolsAllowDraft([]);
        }
      })();
    }
    return () => {
      cancelled = true;
    };
  }, [selectedConversation, agents]);

  // Auto-select conversation when navigating from AgentDetailView
  useEffect(() => {
    const autoSelectId = localStorage.getItem(
      "gui-agent-auto-select-conversation",
    );
    if (
      autoSelectId &&
      conversations.length > 0 &&
      !selectedConversation &&
      !selectedPersistedSession
    ) {
      const conversation = conversations.find((c) => c.id === autoSelectId);
      if (conversation) {
        // Clear the flag first to prevent re-selecting
        localStorage.removeItem("gui-agent-auto-select-conversation");
        // Select the conversation
        handleSelectConversation(conversation);
      } else {
        // Conversation not found, clear the flag
        localStorage.removeItem("gui-agent-auto-select-conversation");
      }
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [conversations, selectedConversation, selectedPersistedSession]);

  const getSessionMessageContent = (msg: SessionMessage): string => {
    if (msg.summary) return msg.summary;
    if (msg.error) return `Error: ${msg.error}`;
    if (msg.message?.content) {
      if (typeof msg.message.content === "string") return msg.message.content;
      if (Array.isArray(msg.message.content)) {
        return msg.message.content
          .map((block: unknown) => {
            if (typeof block === "string") return block;
            if (typeof block === "object" && block !== null) {
              const b = block as Record<string, unknown>;
              if (b.type === "text" && typeof b.text === "string")
                return b.text;
              if (b.type === "tool_use")
                return `[Tool: ${String(b.name || "unknown")}]`;
              if (b.type === "tool_result") return "[Tool Result]";
            }
            return "";
          })
          .filter(Boolean)
          .join("\n");
      }
    }
    if (msg.tool_calls && msg.tool_calls.length > 0) {
      const toolNames = msg.tool_calls
        .map((toolCall) => {
          if (typeof toolCall === "string") return toolCall;
          if (typeof toolCall === "object" && toolCall !== null) {
            const tc = toolCall as Record<string, unknown>;
            if (typeof tc.name === "string") return tc.name;
            if (
              typeof tc.function === "object" &&
              tc.function !== null &&
              typeof (tc.function as Record<string, unknown>).name === "string"
            ) {
              return String((tc.function as Record<string, unknown>).name);
            }
          }
          return "unknown";
        })
        .join(", ");
      return `[Used ${msg.tool_calls.length} tool${msg.tool_calls.length > 1 ? "s" : ""}: ${toolNames}]`;
    }
    return "[No content]";
  };

  const handleSelectSession = async (persisted: PersistedSession) => {
    if (selectedPersistedSession?.id === persisted.id) return;

    if (eventSourceRef.current) {
      eventSourceRef.current.close();
      eventSourceRef.current = null;
    }

    setSelectedPersistedSession(persisted);
    setSelectedConversation(null);
    setLinkedAgent(null);
    setIsLoadingMessages(true);
    setMessages([]);
    setInflight(false);
    setSessionId(null);
    setSession(null);
    setContextInfo({
      workspace: persisted.workspace_path || "/",
      profile: "historical",
      createdAt: persisted.started_at,
      lastActivity: persisted.ended_at || persisted.started_at,
    });
    setPersonalityInfo(null);
    setSelectedMessage(null);
    setShowContextPanel(false);
    setIsCompressing(false);
    setLastCompression(null);
    setMemoryStats(null);
    setMemoryContext(null);
    setShowMemoryContext(false);

    try {
      const messagesData = await getSessionMessages(persisted.id, {
        limit: 200,
      });
      const consoleMessages: ConsoleMessage[] = messagesData.messages
        .filter(
          (msg) =>
            msg.type === "user" ||
            msg.type === "assistant" ||
            msg.type === "human",
        )
        .map((msg) => ({
          role:
            msg.type === "human" ? "user" : (msg.type as "user" | "assistant"),
          content: getSessionMessageContent(msg),
          timestamp:
            msg.timestamp || persisted.started_at || new Date().toISOString(),
        }));
      setMessages(consoleMessages);
    } catch (err) {
      console.error("Failed to load persisted session:", err);
      setMessages([
        {
          role: "assistant",
          content: `Error loading historical session: ${err instanceof Error ? err.message : String(err)}`,
          timestamp: new Date().toISOString(),
        },
      ]);
    } finally {
      setIsLoadingMessages(false);
    }
  };

  // Handle selecting a conversation
  const handleSelectConversation = async (conversation: Conversation) => {
    if (selectedConversation?.id === conversation.id) return;

    // Close existing SSE connection
    if (eventSourceRef.current) {
      eventSourceRef.current.close();
      eventSourceRef.current = null;
    }

    setSelectedPersistedSession(null);
    setSelectedConversation(conversation);
    setIsLoadingMessages(true);
    setMessages([]);
    setSessionId(null);
    setSession(null);
    setContextInfo({});
    setPersonalityInfo(null);
    setSelectedMessage(null);
    setIsCompressing(false);
    setLastCompression(null);
    setMemoryStats(null);
    setMemoryContext(null);
    setShowMemoryContext(false);

    try {
      // Load messages for this conversation from companion memory
      const messagesData = await getCompanionConversationMessages(
        conversation.id,
        200,
      );
      const consoleMessages: ConsoleMessage[] = messagesData.messages.map(
        (msg) => ({
          id: msg.id,
          role: msg.role as "user" | "assistant",
          content: msg.content,
          timestamp: msg.created_at,
          // Map tool calls from companion format to console format
          tool_calls: msg.tool_calls?.map((tc) => ({
            name: tc.name,
            input: tc.arguments as Record<string, unknown>,
            status: "completed" as const,
          })),
        }),
      );
      setMessages(consoleMessages);

      // Check if this conversation is linked to an agent
      const isAgentConversation = agents.some(
        (a) =>
          a.conversation_id === conversation.id || a.id === conversation.id,
      );

      if (isAgentConversation) {
        // For agent-linked conversations, use companion chat directly
        // No console session needed - messages go to companion memory
        const agent = agents.find(
          (a) =>
            a.conversation_id === conversation.id || a.id === conversation.id,
        );
        setContextInfo({
          workspace: agent?.ns || "/",
          profile: "agent",
          createdAt: conversation.created_at,
        });
      } else {
        // For regular conversations, create a console session for SSE
        const sessionData = await createConsoleSession({
          workspace: "/",
          profile: "companion",
          conversation_id: conversation.id,
        });
        setSessionId(sessionData.session.id);
        setSession(sessionData.session);
        setContextInfo({
          workspace: sessionData.session.workspace,
          profile: sessionData.session.profile,
          createdAt: sessionData.session.created,
          lastActivity: sessionData.session.last_activity,
        });
      }

      // Fetch personality info for this conversation
      try {
        const personality = await getCompanionPersonality(conversation.id);
        setPersonalityInfo(personality);
        // Also set the system prompt in contextInfo for display
        setContextInfo((prev) => ({
          ...prev,
          systemPrompt: personality.system_prompt,
        }));
      } catch (personalityErr) {
        console.warn("Failed to load personality info:", personalityErr);
      }

      // Fetch memory stats
      try {
        const stats = await getCompanionMemoryStats(conversation.id);
        setMemoryStats(stats);
      } catch {
        setMemoryStats(null);
      }
      setMemoryContext(null);
      setShowMemoryContext(false);
    } catch (err) {
      console.error("Failed to load conversation:", err);
    } finally {
      setIsLoadingMessages(false);
    }
  };

  // Handle creating a new conversation
  const handleNewConversation = async () => {
    // Close existing SSE connection
    if (eventSourceRef.current) {
      eventSourceRef.current.close();
      eventSourceRef.current = null;
    }

    setSelectedConversation(null);
    setSelectedPersistedSession(null);
    setMessages([]);
    setIsLoadingMessages(true);
    setSessionId(null);
    setSession(null);
    setLinkedAgent(null);

    try {
      // Create a new console session
      const sessionData = await createConsoleSession({
        workspace: "/",
        profile: "companion",
        tool_model: toolModel,
        response_model: responseModel,
      });
      setSessionId(sessionData.session.id);
      setSession(sessionData.session);

      // Create a placeholder conversation for the UI
      const newConv: Conversation = {
        id: sessionData.session.id,
        title: "New Conversation",
        created_at: new Date().toISOString(),
        updated_at: new Date().toISOString(),
        message_count: 0,
      };
      setSelectedConversation(newConv);
      setContextInfo({
        workspace: sessionData.session.workspace,
        profile: sessionData.session.profile,
        createdAt: sessionData.session.created,
      });
    } catch (err) {
      console.error("Failed to create new conversation:", err);
    } finally {
      setIsLoadingMessages(false);
    }
  };

  // Handle creating a new conversation linked to an agent
  const handleNewConversationWithAgent = async (agent: Agent) => {
    // Close existing SSE connection
    if (eventSourceRef.current) {
      eventSourceRef.current.close();
      eventSourceRef.current = null;
    }

    setSelectedConversation(null);
    setSelectedPersistedSession(null);
    setMessages([]);
    setIsLoadingMessages(true);
    setSessionId(null);
    setSession(null);
    setLinkedAgent(agent);
    // Keep a ref so the useEffect doesn't overwrite linkedAgent before DB catches up
    pendingLinkedAgentRef.current = agent;

    try {
      // Create a new console session for this agent
      const sessionData = await createConsoleSession({
        workspace: agent.ns || "/",
        profile: "companion",
        system_prompt: `You are chatting in the context of an agent session.

Agent Details:
- Name: ${getAgentDisplayName(agent)}
- Role: ${agent.role || "N/A"}
- ID: ${agent.id}
- Workspace: ${agent.ns || "/"}
- Model: ${agent.llm_model || "default"}
- State: ${agent.state}

Help the user understand and interact with this agent's work.`,
        tool_model: toolModel,
        response_model: responseModel,
      });
      setSessionId(sessionData.session.id);
      setSession(sessionData.session);

      // Create a placeholder conversation for the UI
      const newConv: Conversation = {
        id: sessionData.session.id,
        title: `Chat with ${getAgentDisplayName(agent)}`,
        created_at: new Date().toISOString(),
        updated_at: new Date().toISOString(),
        message_count: 0,
      };
      setSelectedConversation(newConv);
      setContextInfo({
        workspace: sessionData.session.workspace,
        profile: sessionData.session.profile,
        createdAt: sessionData.session.created,
      });

      // Link the conversation to the agent (conversation-side, supports many-to-one)
      try {
        await renameCompanionConversation(
          sessionData.session.id,
          `Chat with ${getAgentDisplayName(agent)}`,
          agent.id,
        );
        // Refetch conversations so grouping updates immediately
        await refetch();
        // DB is now up-to-date; clear the pending ref
        pendingLinkedAgentRef.current = null;
      } catch (linkErr) {
        console.error("Failed to link conversation to agent:", linkErr);
      }

      // Auto-expand the agent in the sidebar so the new chat is visible
      setExpandedAgents((prev) => {
        const next = new Set(prev);
        next.add(agent.id);
        return next;
      });

      // Reset agent selector
      setSelectedAgentForNew("");
    } catch (err) {
      console.error("Failed to create new conversation with agent:", err);
    } finally {
      setIsLoadingMessages(false);
    }
  };

  const handleSSEMessage = useCallback(
    (payload: { type?: string; content?: string; metadata?: unknown }) => {
      switch (payload.type) {
        case "reply": {
          setInflight(false);
          refetch();
          if (!sessionId) return;
          void (async () => {
            try {
              const data = await getConsoleSession(sessionId);
              // Backend can emit a reply payload without persisting an assistant message
              // (e.g. runner/config errors). In that case, fall back to the payload content
              // so the user sees the error in the UI.
              const replyContent =
                typeof payload.content === "string" ? payload.content : "";
              const nextMessages: ConsoleMessage[] = (() => {
                if (!replyContent.trim()) return data.messages;
                const last = data.messages[data.messages.length - 1];
                if (last?.role === "assistant") return data.messages;
                return [
                  ...data.messages,
                  {
                    role: "assistant",
                    content: replyContent,
                    timestamp: new Date().toISOString(),
                  },
                ];
              })();
              setMessages(nextMessages);
              setSession(data.session);
              setInflight(data.inflight);

              // Prefer deriving tool call context from the persisted assistant message metadata.
              const lastAssistant = [...data.messages]
                .reverse()
                .find((m) => m.role === "assistant");
              const meta = (
                lastAssistant as { metadata?: Record<string, unknown> }
              )?.metadata;
              const metaToolCalls = Array.isArray(meta?.tool_calls)
                ? (meta?.tool_calls as unknown[])
                : null;
              const metaInjected = Array.isArray(meta?.injected_contexts)
                ? (meta?.injected_contexts as unknown[])
                : null;

              if (metaToolCalls || metaInjected) {
                const toolCalls = (metaToolCalls || [])
                  .map((tc) => {
                    if (!tc || typeof tc !== "object") return null;
                    const tco = tc as {
                      id?: string;
                      name?: string;
                      arguments?: unknown;
                      result?: unknown;
                    };
                    if (!tco.name) return null;
                    return {
                      id: tco.id,
                      name: tco.name,
                      args: tco.arguments
                        ? JSON.stringify(tco.arguments)
                        : undefined,
                      result:
                        typeof tco.result === "string"
                          ? tco.result.slice(0, 500)
                          : undefined,
                    };
                  })
                  .filter(Boolean) as Array<{
                  id?: string;
                  name: string;
                  args?: string;
                  result?: string;
                }>;

                const toolNameByID = new Map<string, string>();
                for (const tc of toolCalls) {
                  if (tc.id) toolNameByID.set(tc.id, tc.name);
                }

                const injectedContexts = (metaInjected || [])
                  .map((ic) => {
                    if (!ic || typeof ic !== "object") return null;
                    const ico = ic as {
                      tool_call_id?: string;
                      source?: unknown;
                      content?: unknown;
                    };
                    const source =
                      typeof ico.source === "string" ? ico.source : "hook";
                    const content =
                      typeof ico.content === "string" ? ico.content : "";
                    if (!content) return null;
                    return {
                      source,
                      content,
                      toolName: ico.tool_call_id
                        ? toolNameByID.get(ico.tool_call_id)
                        : undefined,
                    };
                  })
                  .filter(Boolean) as Array<{
                  source: string;
                  content: string;
                  toolName?: string;
                }>;

                setContextInfo((prev) => ({
                  ...prev,
                  toolCalls: toolCalls.map((tc) => ({
                    name: tc.name,
                    args: tc.args,
                    result: tc.result,
                  })),
                  injectedContexts,
                }));
              }
            } catch (err) {
              console.error("Failed to refresh console session:", err);
            }
          })();
          break;
        }
        case "event": {
          const meta = payload.metadata;
          if (!meta || typeof meta !== "object") break;
          const toolData = meta as {
            tool?: string;
            arguments?: unknown;
            phase?: "call" | "result";
            cancelled?: boolean;
          };
          const toolName = toolData.tool;

          if (toolData.cancelled) {
            setInflight(false);
            break;
          }

          if (toolName && toolData.phase === "call") {
            const argsStr = toolData.arguments
              ? JSON.stringify(toolData.arguments)
              : undefined;
            setContextInfo((prev) => ({
              ...prev,
              toolCalls: [
                ...(prev.toolCalls || []),
                { name: toolName, args: argsStr },
              ],
            }));
          } else if (toolName && toolData.phase === "result") {
            const resultContent = payload.content || "";
            setContextInfo((prev) => {
              const toolCalls = prev.toolCalls ? [...prev.toolCalls] : [];
              for (let i = toolCalls.length - 1; i >= 0; i--) {
                if (toolCalls[i].name === toolName && !toolCalls[i].result) {
                  toolCalls[i] = {
                    ...toolCalls[i],
                    result: resultContent.slice(0, 500),
                  };
                  break;
                }
              }
              return { ...prev, toolCalls };
            });
          }
          break;
        }
      }
    },
    [refetch, sessionId],
  );

  // Subscribe to session events via SSE
  useEffect(() => {
    if (!sessionId) return;

    const eventSource = new EventSource(
      `${API_BASE}/console/sessions/${sessionId}/events?format=payload`,
    );
    eventSourceRef.current = eventSource;

    eventSource.addEventListener("message", (event) => {
      try {
        handleSSEMessage(
          JSON.parse(event.data) as {
            type?: string;
            content?: string;
            metadata?: unknown;
          },
        );
      } catch (err) {
        console.error("Failed to parse console SSE message:", err);
      }
    });

    eventSource.onerror = () => {
      // If the stream drops, stop showing "typing".
      setInflight(false);
    };

    return () => {
      eventSource.close();
      eventSourceRef.current = null;
    };
  }, [sessionId, handleSSEMessage]);

  const patchConversationSettings = async (
    patch: ConversationSettingsPatch,
  ) => {
    if (!selectedConversation) return;
    setSettingsError(null);
    try {
      const data = await patchCompanionConversationSettings(
        selectedConversation.id,
        patch,
      );
      settingsRevisionRef.current += 1;
      setConversationSettings(data.settings);
      // Keep draft in sync with normalized backend output.
      if (patch.tools_allow !== undefined) {
        setToolsAllowDraft(data.settings.tools_allow || []);
      }
    } catch (err) {
      console.error("Failed to patch conversation settings:", err);
      setSettingsError(err instanceof Error ? err.message : String(err));
    }
  };

  const resetConversationSettings = async () => {
    if (!selectedConversation) return;
    setSettingsError(null);
    try {
      await deleteCompanionConversationSettings(selectedConversation.id);
      settingsRevisionRef.current += 1;
      setConversationSettings(null);
      setExecModeOverride("");
      setToolModel("");
      setResponseModel("");
      setToolsAllowDraft([]);

      // Reset UI to linked agent defaults (or empty = server defaults).
      const provider = linkedAgent?.llm_provider || "";
      const model = linkedAgent?.llm_model || "";
      const providerCfg = PROVIDERS.find((p) => p.id === provider);
      const knownModels = getModelsForProvider(provider);

      setSelectedProvider(provider);
      if (
        providerCfg?.allowCustom &&
        model &&
        !knownModels.some((m) => m.id === model)
      ) {
        setCustomModelEnabled(true);
        setCustomModel(model);
        setSelectedModel("");
      } else {
        setCustomModelEnabled(false);
        setCustomModel("");
        setSelectedModel(model);
      }
    } catch (err) {
      console.error("Failed to reset conversation settings:", err);
      setSettingsError(err instanceof Error ? err.message : String(err));
    }
  };

  const handleSend = async (content: string) => {
    if (selectedPersistedSession) return;
    // Need either a session (console mode) or a conversation (companion mode)
    if (!sessionId && !selectedConversation) return;

    const userMessage: ConsoleMessage = {
      role: "user",
      content,
      timestamp: new Date().toISOString(),
    };
    setMessages((prev) => [...prev, userMessage]);
    setInflight(true);

    try {
      // For existing companion conversations, use companion chat
      // This persists messages to companion memory
      if (selectedConversation) {
        const llmModelOverride = (
          customModelEnabled ? customModel : selectedModel
        ).trim();
        const llmProviderOverride = selectedProvider.trim();

        const effectiveContextProvider =
          llmProviderOverride ||
          (conversationSettings?.llm_provider || "").trim() ||
          (linkedAgent?.llm_provider || "").trim() ||
          undefined;
        const effectiveContextModel =
          llmModelOverride ||
          (conversationSettings?.llm_model || "").trim() ||
          (linkedAgent?.llm_model || "").trim() ||
          undefined;

        // Build request context so the companion knows about its linked agent.
        // Include effective chat model/provider so the model can "see" per-conversation overrides.
        const agentContext: Record<string, unknown> | undefined = (() => {
          const ctx: Record<string, unknown> = {};
          if (effectiveContextProvider)
            ctx.chat_llm_provider = effectiveContextProvider;
          if (effectiveContextModel) ctx.chat_llm_model = effectiveContextModel;

          if (linkedAgent) {
            ctx.agent_id = linkedAgent.id;
            ctx.agent_name = getAgentDisplayName(linkedAgent);
            ctx.agent_role = linkedAgent.role || undefined;
            ctx.agent_state = linkedAgent.state;
            ctx.agent_exec_mode = linkedAgent.exec_mode || undefined;
            ctx.agent_workspace = linkedAgent.ns || undefined;
            // Keep the agent's configured model in identity; effective chat settings are shown under Runtime.
            ctx.agent_model = linkedAgent.llm_model || undefined;
          }

          return Object.keys(ctx).length > 0 ? ctx : undefined;
        })();
        const response = await companionChat({
          conversation_id: selectedConversation.id,
          message: content,
          workspace: linkedAgent?.ns || contextInfo.workspace || "/",
          max_history_turns: maxHistoryTurns,
          llm_provider: llmProviderOverride || undefined,
          llm_model: llmModelOverride || undefined,
          exec_mode: execModeOverride === "" ? undefined : execModeOverride,
          story_gather_model: toolModel || undefined,
          story_dialogue_model: responseModel || undefined,
          context: agentContext,
        });

        // Add the response with attached tool calls (for inline display)
        const toolCallsForMessage =
          response.tool_calls?.map((tc) => ({
            name: tc.name,
            input: tc.arguments as Record<string, unknown> | undefined,
            output: tc.output,
            status: "completed" as const,
          })) || [];

        setMessages((prev) => [
          ...prev,
          {
            role: "assistant",
            content: response.response,
            timestamp: new Date().toISOString(),
            tool_calls:
              toolCallsForMessage.length > 0 ? toolCallsForMessage : undefined,
          },
        ]);

        // Also update context panel for quick reference
        if (response.tool_calls || response.injected_contexts) {
          setContextInfo((prev) => ({
            ...prev,
            toolCalls:
              response.tool_calls?.map((tc) => ({
                name: tc.name,
                args: tc.arguments ? JSON.stringify(tc.arguments) : undefined,
                result: tc.output,
              })) || [],
            injectedContexts:
              response.injected_contexts?.map((ic) => ({
                source: ic.source || "hook",
                content: ic.content,
              })) || [],
          }));
        }

        setInflight(false);
        refetch(); // Refresh conversation list
      } else if (sessionId) {
        // Use console session with SSE for new conversations only
        await askConsoleSession(sessionId, content, undefined, {
          tool_model: toolModel,
          response_model: responseModel,
        });
      }
    } catch (err) {
      console.error("Failed to send message:", err);
      setInflight(false);
      setMessages((prev) => [
        ...prev,
        {
          role: "assistant",
          content: `Error: Failed to send message. ${err instanceof Error ? err.message : ""}`,
          timestamp: new Date().toISOString(),
        },
      ]);
    }
  };

  const handleCompressMemory = async () => {
    if (!selectedConversation) return;
    if (isCompressing) return;

    setIsCompressing(true);
    try {
      const result = await compressCompanionConversation(
        selectedConversation.id,
        {
          include_today: true,
          max_days: 14,
          distill: true,
          llm_provider: compressionProvider || selectedProvider || undefined,
          llm_model: compressionModel || selectedModel || undefined,
        },
      );
      setLastCompression(result);

      // Refresh personality/context panel so the user can see updated memory context quickly.
      try {
        const personality = await getCompanionPersonality(
          selectedConversation.id,
        );
        setPersonalityInfo(personality);
        setContextInfo((prev) => ({
          ...prev,
          systemPrompt: personality.system_prompt,
        }));
      } catch (personalityErr) {
        console.warn(
          "Failed to reload personality info after compression:",
          personalityErr,
        );
      }

      // Refresh memory stats
      try {
        const stats = await getCompanionMemoryStats(selectedConversation.id);
        setMemoryStats(stats);
      } catch {
        /* ignore */
      }
    } catch (err) {
      console.error("Failed to compress conversation memory:", err);
    } finally {
      setIsCompressing(false);
    }
  };

  const handleCancel = async () => {
    if (!sessionId) return;
    try {
      await cancelConsoleSession(sessionId);
      setInflight(false);
    } catch (err) {
      console.error("Failed to cancel:", err);
    }
  };

  // Calculate approximate token usage from messages
  // Uses rough estimate: ~4 chars per token
  const calculateTokenUsage = () => {
    let inputTokens = 0;
    let outputTokens = 0;
    messages.forEach((msg) => {
      const tokens = Math.ceil(msg.content.length / 4);
      if (msg.role === "user") {
        inputTokens += tokens;
      } else {
        outputTokens += tokens;
      }
    });
    return {
      inputTokens,
      outputTokens,
      totalTokens: inputTokens + outputTokens,
    };
  };

  const tokenUsage = calculateTokenUsage();

  // Handle deleting (soft delete) a conversation
  const handleDeleteConversation = async (
    e: React.MouseEvent,
    conversationId: string,
  ) => {
    e.stopPropagation(); // Prevent selecting the conversation when clicking delete

    if (
      !window.confirm(
        "Are you sure you want to delete this conversation? This action cannot be undone.",
      )
    ) {
      return;
    }

    try {
      await deleteCompanionConversation(conversationId);
      // Refresh the list
      refetch();
      // If the deleted conversation was selected, clear the selection
      if (selectedConversation?.id === conversationId) {
        setSelectedConversation(null);
        setMessages([]);
        setSessionId(null);
        setSession(null);
      }
    } catch (err) {
      console.error("Failed to delete conversation:", err);
    }
  };

  // Handle deleting a single message
  const handleDeleteMessage = async (message: ConsoleMessage) => {
    if (!selectedConversation || !message.id) return;

    if (!window.confirm("Delete this message?")) return;

    try {
      await deleteCompanionMessage(selectedConversation.id, message.id);
      // Remove from local state
      setMessages((prev) => prev.filter((m) => m.id !== message.id));
      if (selectedMessage?.id === message.id) {
        setSelectedMessage(null);
      }
    } catch (err) {
      console.error("Failed to delete message:", err);
    }
  };

  // Match agent to conversation — same broad match used by groupedConversations
  const findAgentForConversation = (convId: string) => {
    const conv = conversations.find((c) => c.id === convId);
    if (conv?.agent_id) {
      const agentById = agents.find((a) => a.id === conv.agent_id);
      if (agentById) return agentById;
    }
    return agents.find((a) => a.conversation_id === convId);
  };

  // Handle starting to edit a conversation title
  const handleStartRename = (
    e: React.MouseEvent,
    conversation: Conversation,
  ) => {
    e.stopPropagation(); // Prevent selecting the conversation

    setEditingConversationId(conversation.id);
    setEditTitle(conversation.name || "");

    // Find current linked agent (using broad match)
    const currentAgent = findAgentForConversation(conversation.id);
    setEditLinkedAgentId(currentAgent?.id || "");
  };

  // Handle saving the renamed conversation
  const handleSaveRename = async (
    e: React.MouseEvent | React.KeyboardEvent,
    conversationId: string,
  ) => {
    e.stopPropagation();

    // Save title and agent link in a single PATCH call (conversation-side linking)
    try {
      const previousAgent = findAgentForConversation(conversationId);
      const agentChanged = previousAgent?.id !== editLinkedAgentId;
      await renameCompanionConversation(
        conversationId,
        editTitle.trim(),
        agentChanged ? editLinkedAgentId || null : undefined,
      );

      if (agentChanged && editLinkedAgentId) {
        // Auto-expand the agent so the moved conversation is visible
        setExpandedAgents((prev) => {
          const next = new Set(prev);
          next.add(editLinkedAgentId);
          return next;
        });
      }
    } catch (err) {
      console.warn("[handleSaveRename] Save failed:", err);
      return;
    }

    await refetch();
    setEditingConversationId(null);
    setEditTitle("");
    setEditLinkedAgentId("");
  };

  // Handle canceling rename
  const handleCancelRename = (e: React.MouseEvent | React.KeyboardEvent) => {
    e.stopPropagation();
    setEditingConversationId(null);
    setEditTitle("");
    setEditLinkedAgentId("");
  };

  // Toggle agent expansion
  const toggleAgentExpanded = (agentId: string) => {
    setExpandedAgents((prev) => {
      const next = new Set(prev);
      if (next.has(agentId)) {
        next.delete(agentId);
      } else {
        next.add(agentId);
      }
      return next;
    });
  };

  // Auto-expand agent when its conversation is selected
  useEffect(() => {
    if (selectedConversation && linkedAgent) {
      setExpandedAgents((prev) => {
        if (!prev.has(linkedAgent.id)) {
          const next = new Set(prev);
          next.add(linkedAgent.id);
          return next;
        }
        return prev;
      });
      // Auto-expand Agent section in Inspector
      setAgentSectionOpen(true);
    } else {
      setAgentSectionOpen(false);
    }
  }, [selectedConversation, linkedAgent]);

  const effectiveExecMode = (
    execModeOverride ||
    linkedAgent?.exec_mode ||
    "reactive"
  ).trim();
  const savedChatModel = (conversationSettings?.llm_model || "").trim();
  const customModelValue = customModel.trim();
  const isCustomSaved =
    customModelEnabled &&
    customModelValue !== "" &&
    savedChatModel === customModelValue;
  const isCustomDirty =
    customModelEnabled &&
    customModelValue !== "" &&
    savedChatModel !== customModelValue;
  const saveCustomModel = () => {
    const m = customModel.trim();
    if (!m) return;
    void patchConversationSettings({ llm_model: m });
  };

  const chatProviderValue = selectedProvider.trim();
  const chatModelValue = (
    customModelEnabled ? customModel : selectedModel
  ).trim();
  const chatModelDisplay = (() => {
    if (!chatProviderValue && !chatModelValue) return "default";
    if (!chatProviderValue) return chatModelValue || "default";
    if (!chatModelValue) return `${chatProviderValue}/(default)`;
    if (chatModelValue.startsWith(`${chatProviderValue}/`))
      return chatModelValue;
    return `${chatProviderValue}/${chatModelValue}`;
  })();
  const linkedRoleIcon = linkedAgent ? getRoleIcon(linkedAgent.role) : Bot;
  const inspectorRoleIcon = getRoleIcon(agentOps.targetAgent?.role);
  const isHistoricalSessionView = selectedPersistedSession !== null;

  return (
    <div className="flex h-full">
      {/* Left Panel - Conversation List */}
      <div className="w-80 border-r border-border flex flex-col">
        {/* Header */}
        <div className="p-3 border-b border-border space-y-2">
          <div className="flex items-center justify-between">
            <div className="flex items-center gap-2">
              <MessagesSquare className="h-4 w-4" />
              <h2 className="text-sm font-semibold text-foreground">
                Conversations
              </h2>
            </div>
            <div className="flex items-center gap-1">
              <Button
                variant="ghost"
                size="icon"
                onClick={handleNewConversation}
                className="h-7 w-7"
                title="New conversation"
              >
                <Plus className="h-4 w-4" />
              </Button>
              <Button
                variant="ghost"
                size="icon"
                onClick={() => {
                  void refetch();
                  void refetchSessions();
                }}
                disabled={isFetching}
                className="h-7 w-7"
              >
                <RefreshCw
                  className={cn("h-4 w-4", isFetching && "animate-spin")}
                />
              </Button>
            </div>
          </div>

          {/* Search */}
          <div className="relative">
            <Search className="absolute left-2 top-2 h-3.5 w-3.5 text-muted-foreground" />
            <Input
              placeholder="Search..."
              value={searchQuery}
              onChange={(e) => setSearchQuery(e.target.value)}
              className="pl-7 h-8 text-sm"
            />
          </div>
        </div>

        {/* Sidebar List - Agents with Collapsible Conversations */}
        <ScrollArea className="flex-1">
          <div className="p-2 space-y-1">
            {isLoading ? (
              <div className="text-center py-8 text-muted-foreground">
                <RefreshCw className="h-5 w-5 mx-auto mb-2 animate-spin" />
                <p className="text-xs">Loading...</p>
              </div>
            ) : agentSections.active.length === 0 &&
              agentSections.errored.length === 0 &&
              feedItems.length === 0 ? (
              <div className="text-center py-8 text-muted-foreground">
                <Bot className="h-8 w-8 mx-auto mb-2 opacity-40" />
                <p className="text-sm">
                  {searchQuery ? "No matches" : "No agents yet"}
                </p>
                <Button
                  variant="outline"
                  size="sm"
                  className="mt-2"
                  onClick={handleNewConversation}
                >
                  <Plus className="h-3 w-3 mr-1" />
                  New Chat
                </Button>
              </div>
            ) : (
              <>
                {/* Agents Section */}
                {agentSections.active.length > 0 && (
                  <CollapsibleSection
                    title="Active"
                    icon={<Play className="h-3.5 w-3.5" />}
                    defaultOpen
                    badge={String(agentSections.active.length)}
                  >
                    <div className="space-y-1">
                      {agentSections.active.map((agent) => (
                        <AgentConversationGroup
                          key={agent.id}
                          agent={agent}
                          conversations={
                            groupedConversations.agentGroups.find(
                              (g) => g.agent.id === agent.id,
                            )?.conversations || []
                          }
                          isExpanded={expandedAgents.has(agent.id)}
                          hasSelectedConversation={(
                            groupedConversations.agentGroups.find(
                              (g) => g.agent.id === agent.id,
                            )?.conversations || []
                          ).some((c) => c.id === selectedConversation?.id)}
                          selectedConversationId={selectedConversation?.id}
                          agents={agents}
                          editingConversationId={editingConversationId}
                          editTitle={editTitle}
                          onEditTitleChange={setEditTitle}
                          editLinkedAgentId={editLinkedAgentId}
                          onEditLinkedAgentIdChange={setEditLinkedAgentId}
                          onToggleExpanded={toggleAgentExpanded}
                          onSelectAgent={setSelectedAgent}
                          onNewConversationWithAgent={
                            handleNewConversationWithAgent
                          }
                          onSelectConversation={handleSelectConversation}
                          onSaveRename={handleSaveRename}
                          onCancelRename={handleCancelRename}
                          onStartRename={handleStartRename}
                          onDeleteConversation={handleDeleteConversation}
                        />
                      ))}
                    </div>
                  </CollapsibleSection>
                )}
                {agentSections.errored.length > 0 && (
                  <CollapsibleSection
                    title="Errored"
                    icon={<Bug className="h-3.5 w-3.5" />}
                    defaultOpen
                    badge={String(agentSections.errored.length)}
                  >
                    <div className="space-y-1">
                      {agentSections.errored.map((agent) => (
                        <AgentConversationGroup
                          key={agent.id}
                          agent={agent}
                          conversations={
                            groupedConversations.agentGroups.find(
                              (g) => g.agent.id === agent.id,
                            )?.conversations || []
                          }
                          isExpanded={expandedAgents.has(agent.id)}
                          hasSelectedConversation={(
                            groupedConversations.agentGroups.find(
                              (g) => g.agent.id === agent.id,
                            )?.conversations || []
                          ).some((c) => c.id === selectedConversation?.id)}
                          selectedConversationId={selectedConversation?.id}
                          agents={agents}
                          editingConversationId={editingConversationId}
                          editTitle={editTitle}
                          onEditTitleChange={setEditTitle}
                          editLinkedAgentId={editLinkedAgentId}
                          onEditLinkedAgentIdChange={setEditLinkedAgentId}
                          onToggleExpanded={toggleAgentExpanded}
                          onSelectAgent={setSelectedAgent}
                          onNewConversationWithAgent={
                            handleNewConversationWithAgent
                          }
                          onSelectConversation={handleSelectConversation}
                          onSaveRename={handleSaveRename}
                          onCancelRename={handleCancelRename}
                          onStartRename={handleStartRename}
                          onDeleteConversation={handleDeleteConversation}
                        />
                      ))}
                    </div>
                  </CollapsibleSection>
                )}
                <CollapsibleSection
                  title="All Conversations"
                  icon={<MessagesSquare className="h-3.5 w-3.5" />}
                  defaultOpen
                  badge={String(feedItems.length)}
                >
                  <div className="space-y-1">
                    {feedItems.length === 0 ? (
                      <div className="px-2 py-2 text-xs text-muted-foreground">
                        {searchQuery
                          ? "No matching conversations"
                          : "No conversations with messages yet"}
                      </div>
                    ) : (
                      feedItems.map((item) =>
                        item.kind === "companion" ? (
                          <CompanionFeedRow
                            key={`companion-${item.conversation.id}`}
                            conversation={item.conversation}
                            agent={item.agent}
                            selected={
                              selectedConversation?.id === item.conversation.id
                            }
                            onClick={() =>
                              handleSelectConversation(item.conversation)
                            }
                          />
                        ) : (
                          <SessionFeedRow
                            key={`session-${item.session.id}`}
                            session={item.session}
                            selected={
                              selectedPersistedSession?.id === item.session.id
                            }
                            onClick={() => handleSelectSession(item.session)}
                          />
                        ),
                      )
                    )}
                  </div>
                </CollapsibleSection>
              </>
            )}
          </div>
        </ScrollArea>
      </div>

      {/* Middle Panel - Chat */}
      <div className="flex-1 flex flex-col min-w-0">
        {selectedConversation || selectedPersistedSession ? (
          <>
            {/* Chat Header with Agent Info */}
            <div className="border-b border-border">
              <div className="h-12 flex items-center justify-between px-4">
                <div className="flex items-center gap-3 min-w-0">
                  {selectedPersistedSession ? (
                    <>
                      <div className="h-8 w-8 rounded-lg bg-blue-500/10 flex items-center justify-center flex-shrink-0">
                        <FileText className="h-4 w-4 text-blue-500" />
                      </div>
                      <div className="min-w-0">
                        <div className="flex items-center gap-2">
                          <span className="text-sm font-medium truncate">
                            {selectedPersistedSession.project_name ||
                              selectedPersistedSession.workspace_path
                                .split("/")
                                .pop() ||
                              "Historical Session"}
                          </span>
                          <Badge variant="outline" className="text-[10px]">
                            Historical Session
                          </Badge>
                        </div>
                        <div className="text-[10px] text-muted-foreground flex items-center gap-2">
                          <span>
                            {selectedPersistedSession.message_count} messages
                          </span>
                          <span>{selectedPersistedSession.status}</span>
                        </div>
                      </div>
                    </>
                  ) : linkedAgent ? (
                    <>
                      <div className="h-8 w-8 rounded-lg bg-primary/10 flex items-center justify-center flex-shrink-0">
                        {React.createElement(linkedRoleIcon, {
                          className: "h-4 w-4 text-primary",
                        })}
                      </div>
                      <div className="min-w-0">
                        <div className="flex items-center gap-2">
                          <span className="text-sm font-medium truncate">
                            {getAgentDisplayName(linkedAgent)}
                          </span>
                          <Badge variant="outline" className="text-[10px]">
                            {linkedAgent.role || "agent"}
                          </Badge>
                        </div>
                        <div className="text-[10px] text-muted-foreground flex items-center gap-2">
                          <span className="flex items-center gap-0.5">
                            <Cpu className="h-2.5 w-2.5" />
                            {linkedAgent.llm_model || "default"}
                          </span>
                          {linkedAgent.ns && (
                            <span className="flex items-center gap-0.5">
                              <Folder className="h-2.5 w-2.5" />
                              {linkedAgent.ns.split("/").pop()}
                            </span>
                          )}
                        </div>
                      </div>
                    </>
                  ) : (
                    <>
                      <Bot className="h-4 w-4 text-primary flex-shrink-0" />
                      <div className="min-w-0">
                        <span className="text-sm font-medium truncate block">
                          {selectedConversation?.title ||
                            selectedConversation?.id.slice(0, 20)}
                        </span>
                        <div className="flex items-center gap-2 text-[10px] text-muted-foreground">
                          <span>{messages.length} messages</span>
                          {linkedAgent !== null && (
                            <Badge
                              variant="secondary"
                              className="text-[9px] bg-primary/10 text-primary"
                            >
                              {React.createElement(linkedRoleIcon, {
                                className: "h-2.5 w-2.5 mr-0.5",
                              })}
                              {getAgentDisplayName(linkedAgent as Agent)}
                            </Badge>
                          )}
                        </div>
                      </div>
                    </>
                  )}
                </div>
                <div className="flex items-center gap-1">
                  <Badge variant="secondary" className="text-xs">
                    {messages.length} msgs
                  </Badge>
                  {!isHistoricalSessionView && (
                    <Button
                      variant="ghost"
                      size="icon"
                      className="h-8 w-8"
                      onClick={() => setShowContextPanel(!showContextPanel)}
                      title={showContextPanel ? "Hide context" : "Show context"}
                    >
                      {showContextPanel ? (
                        <PanelRightClose className="h-4 w-4" />
                      ) : (
                        <PanelRightOpen className="h-4 w-4" />
                      )}
                    </Button>
                  )}
                </div>
              </div>

              {/* Agent Stats Bar (if linked) */}
              {linkedAgent && !selectedPersistedSession && (
                <div className="px-4 py-1.5 bg-muted/30 border-t border-border flex items-center gap-4 text-[10px] text-muted-foreground">
                  <span className="flex items-center gap-1">
                    <Hash className="h-2.5 w-2.5" />
                    {linkedAgent.id.slice(0, 12)}
                  </span>
                  <span className="flex items-center gap-1">
                    <Sparkles className="h-2.5 w-2.5" />
                    {linkedAgent.state}
                  </span>
                  {linkedAgent.llm_provider && (
                    <span className="flex items-center gap-1">
                      <Settings2 className="h-2.5 w-2.5" />
                      {linkedAgent.llm_provider}
                    </span>
                  )}
                </div>
              )}
            </div>

            {/* Messages */}
            <ScrollArea className="flex-1 p-4" ref={scrollRef}>
              {isLoadingMessages ? (
                <div className="flex items-center justify-center h-full">
                  <RefreshCw className="h-6 w-6 animate-spin text-muted-foreground" />
                </div>
              ) : messages.length === 0 ? (
                <div className="flex flex-col items-center justify-center h-full text-muted-foreground">
                  <Bot className="h-12 w-12 mb-3 opacity-30" />
                  <p className="text-sm">
                    {isHistoricalSessionView
                      ? "No session messages found"
                      : "Start the conversation"}
                  </p>
                </div>
              ) : (
                <div className="space-y-4">
                  {messages.map((message, index) => (
                    <MessageBubble
                      key={message.id || index}
                      message={message}
                      isSelected={selectedMessage === message}
                      onSelect={(msg) => {
                        setSelectedMessage(msg);
                        if (!isHistoricalSessionView) {
                          setShowContextPanel(true);
                        }
                      }}
                      onDelete={handleDeleteMessage}
                    />
                  ))}
                  {inflight &&
                    messages[messages.length - 1]?.role !== "assistant" && (
                      <TypingIndicator />
                    )}
                </div>
              )}
            </ScrollArea>

            {/* Chat Input */}
            <div className="p-4 border-t border-border">
              <ChatInput
                onSend={handleSend}
                onCancel={handleCancel}
                disabled={
                  isHistoricalSessionView ||
                  (!sessionId && !selectedConversation)
                }
                inflight={inflight}
              />
              {isHistoricalSessionView && (
                <p className="mt-2 text-[11px] text-muted-foreground">
                  Historical sessions are read-only.
                </p>
              )}
            </div>
          </>
        ) : (
          /* Empty State */
          <div className="flex-1 flex flex-col items-center justify-center text-muted-foreground">
            <div className="h-20 w-20 rounded-2xl bg-muted flex items-center justify-center mb-4">
              <MessagesSquare className="h-10 w-10 opacity-40" />
            </div>
            <h3 className="text-lg font-medium text-foreground mb-1">
              No conversation selected
            </h3>
            <p className="text-sm mb-4">
              Select a conversation or start a new one
            </p>

            {/* Agent Selector for New Conversation */}
            <div className="w-64 mb-4">
              <label className="text-xs text-muted-foreground mb-1 block">
                Link to agent (optional)
              </label>
              <select
                value={selectedAgentForNew}
                onChange={(e) => setSelectedAgentForNew(e.target.value)}
                className="w-full h-9 rounded-md border border-input bg-background px-3 text-sm"
              >
                <option value="">No agent</option>
                {agents.map((agent) => (
                  <option key={agent.id} value={agent.id}>
                    {getAgentDisplayName(agent)}
                  </option>
                ))}
              </select>
            </div>

            <Button
              onClick={() => {
                if (selectedAgentForNew) {
                  const agent = agents.find(
                    (a) => a.id === selectedAgentForNew,
                  );
                  if (agent) {
                    // Use handleChat logic from AgentList to create linked conversation
                    handleNewConversationWithAgent(agent);
                    return;
                  }
                }
                handleNewConversation();
              }}
            >
              <Plus className="h-4 w-4 mr-2" />
              New Conversation
            </Button>
          </div>
        )}
      </div>

      {/* Right Panel - Inspector */}
      {showContextPanel && selectedConversation && (
        <div className="w-80 border-l border-border flex flex-col bg-muted/20">
          <div className="h-12 border-b border-border flex items-center justify-between px-4">
            <div className="flex items-center gap-2">
              <Settings2 className="h-4 w-4" />
              <span className="text-sm font-medium">Inspector</span>
            </div>
            <Button
              variant="ghost"
              size="icon"
              className="h-7 w-7"
              onClick={() => setShowContextPanel(false)}
            >
              <X className="h-4 w-4" />
            </Button>
          </div>

          {/* Scrollable body */}
          <ScrollArea className="flex-1">
            {/* === 1. AGENT === */}
            <CollapsibleSection
              title="Agent"
              icon={React.createElement(inspectorRoleIcon, {
                className: "h-3.5 w-3.5 text-green-500",
              })}
              open={agentSectionOpen}
              onToggle={setAgentSectionOpen}
              badge={
                agentOps.targetAgent ? agentOps.targetAgent.state : undefined
              }
            >
              {agentOps.targetAgent ? (
                <div className="space-y-3">
                  {/* Agent header */}
                  <div className="flex items-center gap-3">
                    <div
                      className={cn(
                        "h-10 w-10 rounded-lg flex items-center justify-center",
                        agentOps.targetAgent.state === "running"
                          ? "bg-green-500/10"
                          : "bg-muted",
                      )}
                    >
                      {React.createElement(inspectorRoleIcon, {
                        className: cn(
                          "h-5 w-5",
                          agentOps.targetAgent.state === "running"
                            ? "text-green-500"
                            : "text-muted-foreground",
                        ),
                      })}
                    </div>
                    <div className="flex-1 min-w-0">
                      <div className="flex items-center gap-2">
                        <span className="text-sm font-semibold truncate">
                          {getAgentDisplayName(agentOps.targetAgent)}
                        </span>
                        <Badge
                          variant={
                            agentOps.targetAgent.state === "running"
                              ? "default"
                              : "outline"
                          }
                          className="text-xs"
                        >
                          {agentOps.targetAgent.state}
                        </Badge>
                      </div>
                      <p className="text-xs text-muted-foreground font-mono truncate">
                        {agentOps.targetAgent.id.slice(0, 16)}...
                      </p>
                    </div>
                  </div>

                  {/* Action buttons */}
                  <div className="flex gap-2">
                    {agentOps.targetAgent.state === "running" ? (
                      <Button
                        variant="destructive"
                        size="sm"
                        className="flex-1 gap-1"
                        onClick={() => {
                          if (
                            window.confirm(
                              `Stop "${getAgentDisplayName(agentOps.targetAgent!)}"?`,
                            )
                          ) {
                            agentOps.killAgent.mutate(agentOps.targetAgent!.id);
                          }
                        }}
                        disabled={agentOps.killAgent.isPending}
                      >
                        <Square className="h-3 w-3" />
                        {agentOps.killAgent.isPending ? "Stopping..." : "Stop"}
                      </Button>
                    ) : (
                      <Button
                        variant="default"
                        size="sm"
                        className="flex-1 gap-1"
                        onClick={() =>
                          agentOps.startAgent.mutate(agentOps.targetAgent!.id)
                        }
                        disabled={agentOps.startAgent.isPending}
                      >
                        <Play className="h-3 w-3" />
                        {agentOps.startAgent.isPending
                          ? "Starting..."
                          : "Start"}
                      </Button>
                    )}
                    <Button
                      variant="outline"
                      size="sm"
                      className="gap-1"
                      onClick={() => {
                        if (
                          window.confirm(
                            `Remove "${getAgentDisplayName(agentOps.targetAgent!)}"? This cannot be undone.`,
                          )
                        ) {
                          agentOps.trashAgent.mutate(agentOps.targetAgent!.id);
                        }
                      }}
                      disabled={
                        agentOps.trashAgent.isPending ||
                        agentOps.targetAgent.state === "running"
                      }
                      title={
                        agentOps.targetAgent.state === "running"
                          ? "Stop agent before trashing"
                          : "Delete agent"
                      }
                    >
                      <Trash2 className="h-3 w-3" />
                    </Button>
                  </div>

                  {/* Workspace */}
                  {agentOps.targetAgent.ns && (
                    <div className="flex items-center gap-2 p-2 rounded-md bg-muted/30">
                      <Folder className="h-3.5 w-3.5 text-muted-foreground flex-shrink-0" />
                      <div className="min-w-0">
                        <span className="text-[10px] text-muted-foreground">
                          Workspace
                        </span>
                        <p
                          className="text-xs font-mono truncate"
                          title={agentOps.targetAgent.ns}
                        >
                          {agentOps.targetAgent.ns}
                        </p>
                      </div>
                    </div>
                  )}

                  {/* 2x2 stats grid */}
                  <div className="grid grid-cols-2 gap-2">
                    <div className="bg-muted/30 rounded-md p-2">
                      <div className="flex items-center gap-1.5 text-muted-foreground mb-0.5">
                        <Cpu className="h-3 w-3" />
                        <span className="text-[10px]">Chat Model</span>
                      </div>
                      <p
                        className="text-xs font-medium truncate"
                        title={`Agent model: ${agentOps.targetAgent.llm_model || "default"}`}
                      >
                        {chatModelDisplay}
                      </p>
                    </div>
                    <div className="bg-muted/30 rounded-md p-2">
                      <div className="flex items-center gap-1.5 text-muted-foreground mb-0.5">
                        <Zap className="h-3 w-3" />
                        <span className="text-[10px]">Role</span>
                      </div>
                      <p className="text-xs font-medium truncate">
                        {agentOps.targetAgent.role || "agent"}
                      </p>
                    </div>
                    <div className="bg-muted/30 rounded-md p-2">
                      <div className="flex items-center gap-1.5 text-muted-foreground mb-0.5">
                        <Activity className="h-3 w-3" />
                        <span className="text-[10px]">Sessions</span>
                      </div>
                      <p className="text-xs font-medium">
                        {agentOps.sessions.length}
                      </p>
                    </div>
                    <div className="bg-muted/30 rounded-md p-2">
                      <div className="flex items-center gap-1.5 text-muted-foreground mb-0.5">
                        <Clock className="h-3 w-3" />
                        <span className="text-[10px]">Created</span>
                      </div>
                      <p className="text-xs font-medium truncate">
                        {agentOps.targetAgent.created_at
                          ? formatRelativeTime(agentOps.targetAgent.created_at)
                          : "-"}
                      </p>
                    </div>
                  </div>

                  {/* Active sessions */}
                  {agentOps.sessions.length > 0 && (
                    <div className="space-y-1.5">
                      <span className="text-[10px] font-medium text-muted-foreground uppercase tracking-wider">
                        Active Sessions
                      </span>
                      {agentOps.sessions
                        .slice(0, 5)
                        .map((sess: AgentSession) => (
                          <Card
                            key={sess.session_id}
                            className="p-2 bg-muted/30"
                          >
                            <div className="flex items-center justify-between mb-0.5">
                              <span className="text-[10px] font-mono text-muted-foreground">
                                {sess.session_id.slice(0, 12)}...
                              </span>
                              <Badge
                                variant={
                                  sess.status === "running"
                                    ? "default"
                                    : "outline"
                                }
                                className="text-[9px]"
                              >
                                {sess.status}
                              </Badge>
                            </div>
                            <div className="flex items-center gap-3 text-[10px] text-muted-foreground">
                              <span>Iters: {sess.iterations || 0}</span>
                              <span>{sess.role}</span>
                            </div>
                          </Card>
                        ))}
                    </div>
                  )}
                </div>
              ) : (
                <div className="text-xs text-muted-foreground italic text-center py-2">
                  No agent linked to this conversation
                </div>
              )}
            </CollapsibleSection>

            {/* === 2. MODELS === */}
            <CollapsibleSection
              title="Models"
              icon={<Cpu className="h-3.5 w-3.5 text-blue-500" />}
              defaultOpen
            >
              {conversationSettings?.updated_at && (
                <div className="text-[10px] text-muted-foreground mb-2">
                  Settings saved{" "}
                  {formatRelativeTime(conversationSettings.updated_at)}
                </div>
              )}
              {/* Chat */}
              <div className="text-[10px] font-medium text-muted-foreground uppercase tracking-wider mb-1">
                Chat
              </div>
              <div className="flex items-center justify-between">
                <span className="text-xs font-medium">Provider</span>
                <select
                  value={selectedProvider}
                  onChange={(e) => {
                    const nextProvider = e.target.value;
                    setSelectedProvider(nextProvider);
                    setCustomModelEnabled(false);
                    setCustomModel("");

                    const nextModel =
                      nextProvider === "openrouter"
                        ? DEFAULT_OPENROUTER_MODEL
                        : "";
                    setSelectedModel(nextModel);
                    void patchConversationSettings({
                      llm_provider: nextProvider,
                      llm_model: nextModel,
                    });
                  }}
                  className="text-xs bg-muted border border-border rounded-md px-2 py-1 font-mono focus:outline-none focus:ring-1 focus:ring-ring max-w-[160px]"
                >
                  {PROVIDERS.map((provider) => (
                    <option key={provider.id} value={provider.id}>
                      {provider.id === ""
                        ? `Default (${defaultProvider || linkedAgent?.llm_provider || "openai"})`
                        : `${provider.name}${providerAvailability.has(provider.id) && !providerAvailability.get(provider.id) ? " \u26A0" : ""}`}
                    </option>
                  ))}
                </select>
              </div>
              {selectedProvider !== "" &&
                providerAvailability.has(selectedProvider) &&
                !providerAvailability.get(selectedProvider) && (
                  <div className="text-[10px] text-destructive">
                    No API key configured for this provider
                  </div>
                )}
              <div className="flex items-center justify-between">
                <span className="text-xs font-medium">Model</span>
                {PROVIDERS.find((p) => p.id === selectedProvider)
                  ?.allowCustom ? (
                  <div className="space-y-1">
                    <select
                      value={customModelEnabled ? "__custom__" : selectedModel}
                      onChange={(e) => {
                        const nextModel = e.target.value;
                        if (nextModel === "__custom__") {
                          setCustomModelEnabled(true);
                          setSelectedModel("");
                          return;
                        }
                        setCustomModelEnabled(false);
                        setCustomModel("");
                        setSelectedModel(nextModel);
                        void patchConversationSettings({
                          llm_model: nextModel,
                        });
                      }}
                      className="text-xs bg-muted border border-border rounded-md px-2 py-1 font-mono focus:outline-none focus:ring-1 focus:ring-ring max-w-[160px]"
                    >
                      <option value="">Default</option>
                      {getModelsForProvider(selectedProvider).map((model) => (
                        <option key={model.id} value={model.id}>
                          {model.name}
                        </option>
                      ))}
                      <option value="__custom__">
                        {customModelValue
                          ? `Custom (${customModelValue})`
                          : "Custom..."}
                      </option>
                    </select>
                    {customModelEnabled && (
                      <div className="flex items-center gap-2">
                        <Input
                          value={customModel}
                          onChange={(e) => setCustomModel(e.target.value)}
                          onKeyDown={(e) => {
                            if (e.key === "Enter") {
                              e.preventDefault();
                              saveCustomModel();
                            }
                          }}
                          placeholder="e.g., openrouter/deepseek-r1"
                          className="h-7 text-xs font-mono flex-1"
                        />
                        <Button
                          type="button"
                          variant="outline"
                          size="sm"
                          className="h-7 px-2 text-[10px]"
                          disabled={!customModelValue || isCustomSaved}
                          onClick={saveCustomModel}
                          title={isCustomSaved ? "Saved" : "Save custom model"}
                        >
                          Save
                        </Button>
                        {customModelEnabled &&
                          (customModelValue ? (
                            <Badge
                              variant={
                                isCustomSaved
                                  ? "success"
                                  : isCustomDirty
                                    ? "warning"
                                    : "secondary"
                              }
                              className="text-[10px]"
                            >
                              {isCustomSaved ? "Saved" : "Unsaved"}
                            </Badge>
                          ) : (
                            <Badge variant="secondary" className="text-[10px]">
                              Enter model
                            </Badge>
                          ))}
                      </div>
                    )}
                  </div>
                ) : (
                  <select
                    value={selectedModel}
                    onChange={(e) => {
                      const nextModel = e.target.value;
                      setSelectedModel(nextModel);
                      void patchConversationSettings({ llm_model: nextModel });
                    }}
                    className="text-xs bg-muted border border-border rounded-md px-2 py-1 font-mono focus:outline-none focus:ring-1 focus:ring-ring max-w-[160px]"
                  >
                    {getModelsForProvider(selectedProvider).map((model) => (
                      <option key={model.id} value={model.id}>
                        {model.id === ""
                          ? `Default (${linkedAgent?.llm_model || "gpt-4o-mini"})`
                          : model.name}
                      </option>
                    ))}
                  </select>
                )}
              </div>
              <div className="flex items-center justify-between">
                <span className="text-xs font-medium">History Turns</span>
                <select
                  value={maxHistoryTurns}
                  onChange={(e) => setMaxHistoryTurns(Number(e.target.value))}
                  className="text-xs bg-muted border border-border rounded-md px-2 py-1 font-mono focus:outline-none focus:ring-1 focus:ring-ring max-w-[140px]"
                >
                  <option value={10}>10 turns</option>
                  <option value={20}>20 turns</option>
                  <option value={50}>50 turns</option>
                  <option value={100}>100 turns</option>
                  <option value={-1}>Disabled</option>
                </select>
              </div>

              {/* Compression */}
              <div className="pt-2 border-t border-border space-y-2">
                <div className="text-[10px] font-medium text-muted-foreground uppercase tracking-wider">
                  Compression
                </div>
                <div className="flex items-center justify-between">
                  <span className="text-xs font-medium">Provider</span>
                  <select
                    value={compressionProvider}
                    onChange={(e) => {
                      setCompressionProvider(e.target.value);
                      setCompressionModel("");
                    }}
                    className="text-xs bg-muted border border-border rounded-md px-2 py-1 font-mono focus:outline-none focus:ring-1 focus:ring-ring max-w-[160px]"
                  >
                    <option value="">Same as Chat</option>
                    {PROVIDERS.filter((p) => p.id !== "").map((provider) => (
                      <option key={provider.id} value={provider.id}>
                        {`${provider.name}${providerAvailability.has(provider.id) && !providerAvailability.get(provider.id) ? " \u26A0" : ""}`}
                      </option>
                    ))}
                  </select>
                </div>
                {compressionProvider !== "" && (
                  <div className="flex items-center justify-between">
                    <span className="text-xs font-medium">Model</span>
                    <select
                      value={compressionModel}
                      onChange={(e) => setCompressionModel(e.target.value)}
                      className="text-xs bg-muted border border-border rounded-md px-2 py-1 font-mono focus:outline-none focus:ring-1 focus:ring-ring max-w-[160px]"
                    >
                      <option value="">Default</option>
                      {getModelsForProvider(compressionProvider).map(
                        (model) => (
                          <option key={model.id} value={model.id}>
                            {model.name}
                          </option>
                        ),
                      )}
                    </select>
                  </div>
                )}
                <div className="flex items-center justify-between">
                  <span className="text-xs font-medium">Compress</span>
                  <Button
                    type="button"
                    variant="outline"
                    size="sm"
                    className="h-7 text-xs"
                    disabled={isCompressing}
                    onClick={handleCompressMemory}
                  >
                    {isCompressing ? "Running..." : "Run"}
                  </Button>
                </div>
                {lastCompression && (
                  <div className="text-[10px] text-muted-foreground">
                    {lastCompression.summarized} summarized,{" "}
                    {lastCompression.skipped} skipped
                    {lastCompression.distilled ? ", distilled" : ""}
                  </div>
                )}
              </div>

              {/* Story Pipeline — only shown when exec mode is "story" */}
              {effectiveExecMode === "story" && (
                <div className="pt-2 border-t border-border space-y-2">
                  <div className="text-[10px] font-medium text-muted-foreground uppercase tracking-wider">
                    Story Pipeline
                  </div>
                  {providerAvailability.size > 0 &&
                    !providerAvailability.get("openrouter") && (
                      <div className="text-[10px] text-destructive">
                        OpenRouter key not configured
                      </div>
                    )}
                  <div className="flex items-center justify-between">
                    <div className="flex items-center gap-2">
                      <Search className="h-4 w-4 text-orange-500" />
                      <span className="text-xs font-medium">Gather Model</span>
                    </div>
                    <select
                      value={toolModel}
                      onChange={(e) => {
                        const nextModel = e.target.value;
                        setToolModel(nextModel);
                        void patchConversationSettings({
                          story_gather_model: nextModel,
                        });
                      }}
                      className="text-xs bg-muted border border-border rounded-md px-2 py-1 font-mono focus:outline-none focus:ring-1 focus:ring-ring max-w-[140px]"
                    >
                      <option value="">Default</option>
                      {toolModel &&
                        !COMPANION_TOOL_MODELS.some(
                          (m) => m.id === toolModel,
                        ) && <option value={toolModel}>{toolModel}</option>}
                      {COMPANION_TOOL_MODELS.map((model) => (
                        <option key={model.id} value={model.id}>
                          {model.name}
                        </option>
                      ))}
                    </select>
                  </div>
                  <div className="flex items-center justify-between">
                    <div className="flex items-center gap-2">
                      <MessageCircle className="h-4 w-4 text-purple-500" />
                      <span className="text-xs font-medium">
                        Dialogue Model
                      </span>
                    </div>
                    <select
                      value={responseModel}
                      onChange={(e) => {
                        const nextModel = e.target.value;
                        setResponseModel(nextModel);
                        void patchConversationSettings({
                          story_dialogue_model: nextModel,
                        });
                      }}
                      className="text-xs bg-muted border border-border rounded-md px-2 py-1 font-mono focus:outline-none focus:ring-1 focus:ring-ring max-w-[140px]"
                    >
                      <option value="">Default</option>
                      {responseModel &&
                        !COMPANION_RESPONSE_MODELS.some(
                          (m) => m.id === responseModel,
                        ) && (
                          <option value={responseModel}>{responseModel}</option>
                        )}
                      {COMPANION_RESPONSE_MODELS.map((model) => (
                        <option key={model.id} value={model.id}>
                          {model.name}
                        </option>
                      ))}
                    </select>
                  </div>
                </div>
              )}

              {/* Execution mode (per-conversation default) */}
              <div className="pt-2 border-t border-border space-y-1.5">
                <div className="flex items-center justify-between">
                  <span className="text-xs font-medium">Exec Mode</span>
                  <select
                    value={execModeOverride}
                    onChange={(e) => {
                      const nextMode = e.target.value as ExecMode;
                      setExecModeOverride(nextMode);
                      void patchConversationSettings({ exec_mode: nextMode });
                    }}
                    className="text-xs bg-muted border border-border rounded-md px-2 py-1 font-mono focus:outline-none focus:ring-1 focus:ring-ring max-w-[160px]"
                  >
                    <option value="">
                      Default ({linkedAgent?.exec_mode || "reactive"})
                    </option>
                    <option value="reactive">Reactive</option>
                    <option value="autonomous">Autonomous</option>
                    <option value="proactive">Proactive</option>
                    <option value="story">Story</option>
                  </select>
                </div>
                <p className="text-[10px] text-muted-foreground">
                  {effectiveExecMode === "reactive" &&
                    "Single-turn: responds to each message independently"}
                  {effectiveExecMode === "autonomous" &&
                    "Multi-turn: continues working until task complete"}
                  {effectiveExecMode === "proactive" &&
                    "Self-directed: initiates work via think cycles"}
                  {effectiveExecMode === "story" &&
                    "Two-stage: gather + dialogue with structured outputs"}
                </p>
              </div>

              {/* Tools access (per-conversation) */}
              <div className="pt-2 border-t border-border">
                <ToolAllowlistEditor
                  value={toolsAllowDraft}
                  onChange={setToolsAllowDraft}
                  onSave={() =>
                    void patchConversationSettings({
                      tools_allow: toolsAllowDraft,
                    })
                  }
                  onClear={() =>
                    void patchConversationSettings({ tools_allow: [] })
                  }
                  error={settingsError}
                />
              </div>

              {/* Reset */}
              <div className="pt-2 border-t border-border">
                <Button
                  type="button"
                  variant="outline"
                  size="sm"
                  className="h-7 text-xs w-full"
                  onClick={() => {
                    if (!selectedConversation) return;
                    if (
                      window.confirm(
                        "Reset all conversation settings to defaults?",
                      )
                    ) {
                      void resetConversationSettings();
                    }
                  }}
                >
                  Reset Conversation Settings
                </Button>
              </div>
            </CollapsibleSection>

            {/* === 3. MEMORY === */}
            <CollapsibleSection
              title="Memory"
              icon={<Brain className="h-3.5 w-3.5 text-purple-500" />}
              badge={
                memoryStats ? `${memoryStats.day_summaries} days` : undefined
              }
            >
              {memoryStats ? (
                <div className="space-y-2">
                  <div className="text-[10px] text-muted-foreground space-y-0.5">
                    <div>
                      {memoryStats.total_turns} turns,{" "}
                      {memoryStats.day_summaries} day summaries
                      {memoryStats.has_distilled_history ? ", distilled" : ""}
                    </div>
                    {memoryStats.last_summarized_date && (
                      <div>
                        Last summarized: {memoryStats.last_summarized_date}
                      </div>
                    )}
                  </div>
                  {memoryStats.day_summaries > 0 && (
                    <Button
                      variant="ghost"
                      size="sm"
                      className="h-6 text-[10px] px-2"
                      onClick={async () => {
                        if (showMemoryContext) {
                          setShowMemoryContext(false);
                          return;
                        }
                        if (!selectedConversation) return;
                        try {
                          const data = await getCompanionMemoryContext(
                            selectedConversation.id,
                          );
                          setMemoryContext(data.context);
                          setShowMemoryContext(true);
                        } catch {
                          setMemoryContext(null);
                        }
                      }}
                    >
                      {showMemoryContext
                        ? "Hide memory context"
                        : "Show memory context"}
                    </Button>
                  )}
                  {showMemoryContext && memoryContext && (
                    <Card className="p-2 max-h-[120px] overflow-y-auto">
                      <pre className="text-[10px] text-muted-foreground whitespace-pre-wrap">
                        {memoryContext}
                      </pre>
                    </Card>
                  )}
                </div>
              ) : (
                <div className="text-xs text-muted-foreground italic">
                  No memory data available
                </div>
              )}
            </CollapsibleSection>

            {/* === 4. CONVERSATION === */}
            <CollapsibleSection
              title="Conversation"
              icon={<MessageCircle className="h-3.5 w-3.5 text-yellow-500" />}
              badge={`${messages.length} msgs`}
            >
              {/* Token Usage */}
              <div className="space-y-2">
                <div className="flex items-center gap-2">
                  <Coins className="h-3.5 w-3.5 text-yellow-500" />
                  <span className="text-xs font-medium">Token Usage</span>
                </div>
                <div className="grid grid-cols-3 gap-2 text-center">
                  <div className="bg-muted/50 rounded-md p-2">
                    <div className="text-[10px] text-muted-foreground">
                      Input
                    </div>
                    <div className="text-xs font-mono font-medium">
                      {tokenUsage.inputTokens.toLocaleString()}
                    </div>
                  </div>
                  <div className="bg-muted/50 rounded-md p-2">
                    <div className="text-[10px] text-muted-foreground">
                      Output
                    </div>
                    <div className="text-xs font-mono font-medium">
                      {tokenUsage.outputTokens.toLocaleString()}
                    </div>
                  </div>
                  <div className="bg-primary/10 rounded-md p-2">
                    <div className="text-[10px] text-muted-foreground">
                      Total
                    </div>
                    <div className="text-xs font-mono font-medium text-primary">
                      {tokenUsage.totalTokens.toLocaleString()}
                    </div>
                  </div>
                </div>
              </div>

              {/* Session Info */}
              <Card className="p-3 space-y-2 text-xs">
                <div className="flex items-center justify-between">
                  <span className="text-muted-foreground">Profile</span>
                  <Badge variant="secondary" className="text-[10px]">
                    {contextInfo.profile || session?.profile || "companion"}
                  </Badge>
                </div>
                <div className="flex items-center justify-between">
                  <span className="text-muted-foreground">Workspace</span>
                  <span
                    className="text-[10px] font-mono truncate max-w-[140px]"
                    title={contextInfo.workspace}
                  >
                    {contextInfo.workspace || "/"}
                  </span>
                </div>
                {contextInfo.createdAt && (
                  <div className="flex items-center justify-between">
                    <span className="text-muted-foreground">Created</span>
                    <span className="text-[10px] flex items-center gap-1">
                      <Clock className="h-3 w-3" />
                      {formatRelativeTime(contextInfo.createdAt)}
                    </span>
                  </div>
                )}
              </Card>

              {/* Conversation metadata */}
              <Card className="p-3 space-y-2 text-xs">
                <div className="flex justify-between">
                  <span className="text-muted-foreground">ID</span>
                  <span
                    className="font-mono truncate max-w-[140px]"
                    title={selectedConversation.id}
                  >
                    {selectedConversation.id.slice(0, 20)}...
                  </span>
                </div>
                <div className="flex justify-between">
                  <span className="text-muted-foreground">Messages</span>
                  <span>{messages.length}</span>
                </div>
                <div className="flex justify-between">
                  <span className="text-muted-foreground">Updated</span>
                  <span>
                    {formatRelativeTime(selectedConversation.updated_at)}
                  </span>
                </div>
              </Card>
            </CollapsibleSection>

            {/* === 5. PERSONALITY === */}
            <CollapsibleSection
              title="Personality"
              icon={<Sparkles className="h-3.5 w-3.5 text-purple-500" />}
            >
              {/* Dimension Sliders */}
              {personalityInfo?.profile?.dimensions &&
                personalityInfo.profile.dimensions.length > 0 && (
                  <div className="space-y-3">
                    {personalityInfo.profile.dimensions.map((dim) => (
                      <div key={dim.name} className="space-y-1.5">
                        <div className="flex justify-between text-xs">
                          <span className="capitalize text-muted-foreground">
                            {dim.name}
                          </span>
                          <span className="font-mono text-primary">
                            {(dim.value * 100).toFixed(0)}%
                          </span>
                        </div>
                        <Slider
                          value={dim.value}
                          min={0}
                          max={1}
                          step={0.05}
                          onChange={(value) => {
                            setPersonalityInfo((prev) => {
                              if (!prev?.profile) return prev;
                              return {
                                ...prev,
                                profile: {
                                  ...prev.profile,
                                  dimensions: prev.profile.dimensions.map(
                                    (d) =>
                                      d.name === dim.name ? { ...d, value } : d,
                                  ),
                                },
                              };
                            });
                            if (!selectedConversation) return;
                            const existingTimer =
                              personalityDebounceRefs.current.get(dim.name);
                            if (existingTimer) clearTimeout(existingTimer);
                            const timer = setTimeout(async () => {
                              try {
                                await updatePersonalityDimension(
                                  selectedConversation.id,
                                  dim.name,
                                  value,
                                );
                              } catch (err) {
                                console.error(
                                  "Failed to update personality dimension:",
                                  err,
                                );
                              }
                              personalityDebounceRefs.current.delete(dim.name);
                            }, 300);
                            personalityDebounceRefs.current.set(
                              dim.name,
                              timer,
                            );
                          }}
                        />
                        <div className="flex justify-between text-[10px] text-muted-foreground">
                          <span>{dim.min_label}</span>
                          <span>{dim.max_label}</span>
                        </div>
                      </div>
                    ))}
                  </div>
                )}

              {/* Learned traits / interests / dislikes */}
              {personalityInfo?.profile &&
                (personalityInfo.profile.learned_traits?.length > 0 ||
                  personalityInfo.profile.interests?.length > 0 ||
                  personalityInfo.profile.dislikes?.length > 0) && (
                  <Card className="p-3 space-y-3">
                    {personalityInfo.profile.learned_traits?.length > 0 && (
                      <div>
                        <span className="text-xs font-medium text-muted-foreground">
                          Learned Traits
                        </span>
                        <div className="flex flex-wrap gap-1 mt-1">
                          {personalityInfo.profile.learned_traits.map(
                            (trait, i) => (
                              <Badge
                                key={i}
                                variant="secondary"
                                className="text-[10px]"
                              >
                                {trait}
                              </Badge>
                            ),
                          )}
                        </div>
                      </div>
                    )}
                    {personalityInfo.profile.interests?.length > 0 && (
                      <div
                        className={
                          personalityInfo.profile.learned_traits?.length > 0
                            ? "pt-2 border-t border-border"
                            : ""
                        }
                      >
                        <span className="text-xs font-medium text-muted-foreground">
                          Interests
                        </span>
                        <div className="flex flex-wrap gap-1 mt-1">
                          {personalityInfo.profile.interests.map(
                            (interest, i) => (
                              <Badge
                                key={i}
                                variant="outline"
                                className="text-[10px] text-green-600"
                              >
                                {interest}
                              </Badge>
                            ),
                          )}
                        </div>
                      </div>
                    )}
                    {personalityInfo.profile.dislikes?.length > 0 && (
                      <div
                        className={
                          personalityInfo.profile.learned_traits?.length > 0 ||
                          personalityInfo.profile.interests?.length > 0
                            ? "pt-2 border-t border-border"
                            : ""
                        }
                      >
                        <span className="text-xs font-medium text-muted-foreground">
                          Dislikes
                        </span>
                        <div className="flex flex-wrap gap-1 mt-1">
                          {personalityInfo.profile.dislikes.map(
                            (dislike, i) => (
                              <Badge
                                key={i}
                                variant="outline"
                                className="text-[10px] text-red-600"
                              >
                                {dislike}
                              </Badge>
                            ),
                          )}
                        </div>
                      </div>
                    )}
                  </Card>
                )}

              {!personalityInfo?.profile?.dimensions?.length &&
                !personalityInfo?.profile?.learned_traits?.length && (
                  <div className="text-xs text-muted-foreground italic">
                    No personality data
                  </div>
                )}
            </CollapsibleSection>

            {/* === 6. PROMPT === */}
            <CollapsibleSection
              title="Prompt"
              icon={<Sliders className="h-3.5 w-3.5 text-blue-500" />}
            >
              {/* System Prompt (Editable) */}
              <div className="space-y-2">
                <div className="flex items-center justify-between">
                  <span className="text-xs font-medium">System Prompt</span>
                  {!editingSystemPrompt ? (
                    <Button
                      variant="ghost"
                      size="sm"
                      className="h-6 text-xs"
                      onClick={() => {
                        setEditingSystemPrompt(true);
                        setSystemPromptDraft(contextInfo.systemPrompt || "");
                      }}
                    >
                      <Pencil className="h-3 w-3 mr-1" />
                      Edit
                    </Button>
                  ) : (
                    <div className="flex gap-1">
                      <Button
                        variant="ghost"
                        size="sm"
                        className="h-6 w-6 p-0"
                        onClick={() => {
                          setEditingSystemPrompt(false);
                          setContextInfo((prev) => ({
                            ...prev,
                            systemPrompt: systemPromptDraft,
                          }));
                        }}
                      >
                        <Save className="h-3 w-3 text-green-500" />
                      </Button>
                      <Button
                        variant="ghost"
                        size="sm"
                        className="h-6 w-6 p-0"
                        onClick={() => {
                          setEditingSystemPrompt(false);
                          setSystemPromptDraft("");
                        }}
                      >
                        <RotateCcw className="h-3 w-3 text-muted-foreground" />
                      </Button>
                    </div>
                  )}
                </div>
                {editingSystemPrompt ? (
                  <Textarea
                    value={systemPromptDraft}
                    onChange={(e: React.ChangeEvent<HTMLTextAreaElement>) =>
                      setSystemPromptDraft(e.target.value)
                    }
                    className="text-xs min-h-[100px] max-h-[150px] font-mono"
                    placeholder="Enter system prompt..."
                  />
                ) : contextInfo.systemPrompt ? (
                  <Card className="p-2 max-h-[100px] overflow-y-auto">
                    <pre className="text-[11px] text-muted-foreground whitespace-pre-wrap">
                      {contextInfo.systemPrompt.slice(0, 300)}
                      {contextInfo.systemPrompt.length > 300 && "..."}
                    </pre>
                  </Card>
                ) : (
                  <div className="text-xs text-muted-foreground italic">
                    No system prompt configured
                  </div>
                )}
              </div>

              {/* Built Prompt Preview */}
              <div className="space-y-2">
                <div className="flex items-center justify-between">
                  <span className="text-xs font-medium flex items-center gap-1">
                    <FileText className="h-3 w-3" />
                    Built Prompt Preview
                  </span>
                  <Badge variant="outline" className="text-[10px]">
                    {messages.length} messages
                  </Badge>
                </div>
                <Card className="p-2 max-h-[200px] overflow-y-auto">
                  <div className="space-y-2 text-[10px] font-mono">
                    {contextInfo.systemPrompt && (
                      <div className="p-2 bg-blue-500/10 rounded border-l-2 border-blue-500">
                        <span className="font-semibold text-blue-600">
                          SYSTEM:
                        </span>
                        <pre className="whitespace-pre-wrap text-muted-foreground mt-1">
                          {contextInfo.systemPrompt.slice(0, 200)}
                          {contextInfo.systemPrompt.length > 200 && "..."}
                        </pre>
                      </div>
                    )}
                    {messages.slice(-5).map((msg, i) => (
                      <div
                        key={i}
                        className={cn(
                          "p-2 rounded border-l-2",
                          msg.role === "user"
                            ? "bg-green-500/10 border-green-500"
                            : "bg-purple-500/10 border-purple-500",
                        )}
                      >
                        <span
                          className={cn(
                            "font-semibold",
                            msg.role === "user"
                              ? "text-green-600"
                              : "text-purple-600",
                          )}
                        >
                          {msg.role.toUpperCase()}:
                        </span>
                        <pre className="whitespace-pre-wrap text-muted-foreground mt-1">
                          {msg.content.slice(0, 150)}
                          {msg.content.length > 150 && "..."}
                        </pre>
                      </div>
                    ))}
                    {messages.length > 5 && (
                      <div className="text-center text-muted-foreground py-1">
                        ... {messages.length - 5} earlier messages ...
                      </div>
                    )}
                  </div>
                </Card>
              </div>
            </CollapsibleSection>

            {/* === 7. DEBUG === */}
            <CollapsibleSection
              title="Debug"
              icon={<Bug className="h-3.5 w-3.5 text-red-500" />}
              badge={selectedMessage ? "1 selected" : undefined}
            >
              {/* Selected Message Details */}
              {selectedMessage ? (
                <div className="space-y-2">
                  <div className="flex items-center justify-between">
                    <span className="text-xs font-medium">
                      Selected Message
                    </span>
                    <Button
                      variant="ghost"
                      size="sm"
                      className="h-6 text-xs"
                      onClick={() => setSelectedMessage(null)}
                    >
                      Clear
                    </Button>
                  </div>
                  <Card className="p-3 space-y-3">
                    <div className="flex items-center justify-between text-xs">
                      <span className="text-muted-foreground">Role</span>
                      <Badge
                        variant={
                          selectedMessage.role === "assistant"
                            ? "default"
                            : "secondary"
                        }
                      >
                        {selectedMessage.role}
                      </Badge>
                    </div>
                    {selectedMessage.timestamp && (
                      <div className="flex items-center justify-between text-xs">
                        <span className="text-muted-foreground">Time</span>
                        <span className="flex items-center gap-1">
                          <Clock className="h-3 w-3" />
                          {formatRelativeTime(selectedMessage.timestamp)}
                        </span>
                      </div>
                    )}
                    {selectedMessage.content && (
                      <div className="pt-2 border-t border-border">
                        <span className="text-xs font-medium text-muted-foreground">
                          Content Preview
                        </span>
                        <p className="text-xs text-foreground mt-1 line-clamp-3">
                          {selectedMessage.content.slice(0, 200)}
                          {selectedMessage.content.length > 200 && "..."}
                        </p>
                      </div>
                    )}
                  </Card>

                  {/* Tool Calls for Selected Message */}
                  {selectedMessage.tool_calls &&
                    selectedMessage.tool_calls.length > 0 && (
                      <div className="space-y-2">
                        <span className="text-xs font-medium">
                          Tool Calls ({selectedMessage.tool_calls.length})
                        </span>
                        <div className="space-y-2">
                          {selectedMessage.tool_calls.map((tool, i) => (
                            <Card key={i} className="p-2">
                              <div className="flex items-center gap-2">
                                <Settings2 className="h-3 w-3 text-primary" />
                                <span className="text-xs font-mono flex-1">
                                  {tool.name}
                                </span>
                                <Badge
                                  variant="secondary"
                                  className={cn(
                                    "text-[10px]",
                                    tool.status === "completed" &&
                                      "bg-green-500/20 text-green-600",
                                    tool.status === "error" &&
                                      "bg-red-500/20 text-red-600",
                                    tool.status === "pending" &&
                                      "bg-yellow-500/20 text-yellow-600",
                                  )}
                                >
                                  {tool.status}
                                </Badge>
                              </div>
                              {tool.input &&
                                Object.keys(tool.input).length > 0 && (
                                  <div className="mt-2">
                                    <span className="text-[10px] font-medium text-muted-foreground">
                                      Input (raw JSON):
                                    </span>
                                    <pre className="text-[10px] text-muted-foreground overflow-auto max-h-64 mt-1 p-2 bg-muted rounded font-mono whitespace-pre-wrap break-all">
                                      {JSON.stringify(tool.input, null, 2)}
                                    </pre>
                                  </div>
                                )}
                              {tool.output && (
                                <div className="mt-2">
                                  <span className="text-[10px] font-medium text-muted-foreground">
                                    Output (raw):
                                  </span>
                                  <pre className="text-[10px] text-muted-foreground overflow-auto max-h-96 mt-1 p-2 bg-muted rounded font-mono whitespace-pre-wrap break-all">
                                    {tool.output}
                                  </pre>
                                </div>
                              )}
                            </Card>
                          ))}
                        </div>
                      </div>
                    )}
                </div>
              ) : (
                <div className="text-xs text-muted-foreground italic">
                  Click a message to inspect it
                </div>
              )}

              {/* Injected Context (from hooks) */}
              {contextInfo.injectedContexts &&
                contextInfo.injectedContexts.length > 0 && (
                  <div className="space-y-2 pt-2 border-t border-border">
                    <span className="text-xs font-medium">
                      Injected Context ({contextInfo.injectedContexts.length})
                    </span>
                    <div className="space-y-2">
                      {contextInfo.injectedContexts.slice(-3).map((ctx, i) => (
                        <Card key={i} className="p-2">
                          <div className="flex items-center gap-2 mb-1">
                            <Sparkles className="h-3 w-3 text-yellow-500" />
                            <span className="text-xs font-mono text-muted-foreground">
                              {ctx.source}
                            </span>
                          </div>
                          <pre className="text-[10px] text-muted-foreground whitespace-pre-wrap overflow-x-auto max-h-32">
                            {ctx.content.slice(0, 500)}
                            {ctx.content.length > 500 && "..."}
                          </pre>
                        </Card>
                      ))}
                    </div>
                  </div>
                )}
            </CollapsibleSection>
          </ScrollArea>
        </div>
      )}
    </div>
  );
}
