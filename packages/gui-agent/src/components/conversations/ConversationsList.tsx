import React, {
  useState,
  useEffect,
  useRef,
  useCallback,
  useMemo,
} from "react";
import { useQuery } from "@tanstack/react-query";
import {
  listCompanionConversations,
  getCompanionPersonality,
  updatePersonalityDimension,
  getCompanionConversationSettings,
  patchCompanionConversationSettings,
  deleteCompanionConversationSettings,
  getConsoleSession,
  askConsoleSession,
  cancelConsoleSession,
  companionChat,
  listAgents,
  listPersistedSessions,
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
} from "@/api/client";
import type { Agent } from "@/api/types";
import {
  PROVIDERS,
  getModelsForProvider,
} from "@/components/agents/spawnFormConstants";
import { ConversationInspector } from "@/components/conversations/ConversationInspector";
import { ConversationListSidebar } from "@/components/conversations/ConversationListSidebar";
import { ConversationWorkspace } from "@/components/conversations/ConversationWorkspace";
import { useConversationPaneHelpers } from "@/components/conversations/useConversationPaneHelpers";
import type { ContextInfo } from "@/components/conversations/types";
import { useConversationTransitions } from "@/components/conversations/useConversationTransitions";
import { useViewStore } from "@/stores/viewStore";
import { useAgentOperations } from "@/hooks/useAgentOperations";
import {
  getAgentDisplayName,
  isWorkerAgent,
} from "@/lib/agent-utils";
import {
  findAgentForConversationID,
  matchAgentToConversation,
} from "@/lib/conversation-utils";
import {
  buildAgentSections,
  buildFeedItems,
  buildGroupedConversations,
  filterAgentsBySearch,
  filterConversationsBySearch,
  type Conversation,
} from "@/lib/conversation-list-models";

const API_BASE = "/api";

type ExecMode = "" | "reactive" | "autonomous" | "proactive" | "tick" | "story";

const DEFAULT_OPENROUTER_MODEL = "google/gemini-3.1-flash-lite-preview";

export function ConversationsList() {
  const setSelectedAgent = useViewStore((s) => s.setSelectedAgent);
  const selectedConversationID = useViewStore((s) => s.selectedConversationID);
  const setSelectedConversationID = useViewStore(
    (s) => s.setSelectedConversationID,
  );
  const setActiveView = useViewStore((s) => s.setActiveView);
  const [searchQuery, setSearchQuery] = useState("");
  const [selectedConversation, setSelectedConversation] =
    useState<Conversation | null>(null);
  const [selectedPersistedSession, setSelectedPersistedSession] =
    useState<PersistedSession | null>(null);
  const [messages, setMessages] = useState<ConsoleMessage[]>([]);
  const [inflight, setInflight] = useState(false);
  const [sessionId, setSessionId] = useState<string | null>(null);
  const [, setSession] = useState<ConsoleSession | null>(null);
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
  const agents = useMemo(() => {
    const raw = agentsData?.agents ?? [];
    const seen = new Set<string>();
    const deduped: Agent[] = [];
    for (const agent of raw) {
      if (!agent?.id || seen.has(agent.id)) continue;
      seen.add(agent.id);
      deduped.push(agent);
    }
    return deduped;
  }, [agentsData?.agents]);
  const linkableAgents = useMemo(() => {
    const rankState = (state: string): number => {
      switch ((state || "").toLowerCase()) {
        case "running":
          return 0;
        case "error":
          return 1;
        case "idle":
          return 2;
        case "stopped":
          return 3;
        default:
          return 4;
      }
    };
    const timestamp = (agent: Agent): number => {
      const raw = agent.heartbeat_at || agent.updated_at || agent.created_at;
      if (!raw) return 0;
      const parsed = Date.parse(raw);
      return Number.isFinite(parsed) ? parsed : 0;
    };

    return agents
      .filter((agent) => !isWorkerAgent(agent))
      .sort((a, b) => {
        const byState = rankState(a.state) - rankState(b.state);
        if (byState !== 0) return byState;
        const byTime = timestamp(b) - timestamp(a);
        if (byTime !== 0) return byTime;
        return getAgentDisplayName(a).localeCompare(getAgentDisplayName(b));
      });
  }, [agents]);

  const filteredConversations = useMemo(
    () => filterConversationsBySearch(conversations, searchQuery, agents),
    [conversations, searchQuery, agents],
  );

  const groupedConversations = useMemo(
    () =>
      buildGroupedConversations({
        filteredConversations,
        agents,
        selectedConversation,
        linkedAgent,
      }),
    [filteredConversations, agents, selectedConversation, linkedAgent],
  );

  const filteredAgents = useMemo(
    () => filterAgentsBySearch(agents, searchQuery),
    [agents, searchQuery],
  );

  const agentSections = useMemo(
    () => buildAgentSections(filteredAgents),
    [filteredAgents],
  );

  const feedItems = useMemo(
    () =>
      buildFeedItems({
        searchQuery,
        agents,
        conversations,
        persistedSessions,
      }),
    [searchQuery, agents, conversations, persistedSessions],
  );

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

    const agent = matchAgentToConversation(selectedConversation, agents);
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

  // Resolve route-selected conversation after conversation data loads.
  useEffect(() => {
    if (
      !selectedConversationID ||
      conversations.length === 0 ||
      selectedPersistedSession
    ) {
      return;
    }
    if (selectedConversation?.id === selectedConversationID) {
      return;
    }
    const conversation = conversations.find(
      (candidate) => candidate.id === selectedConversationID,
    );
    if (conversation) {
      void handleSelectConversation(conversation);
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [
    conversations,
    selectedConversation?.id,
    selectedConversationID,
    selectedPersistedSession,
  ]);

  const {
    beginConversationLoad,
    activateConsoleSession,
  } = useConversationPaneHelpers({
    eventSourceRef,
    setLinkedAgent,
    setInflight,
    setMessages,
    setSessionId,
    setSession,
    setContextInfo,
    setPersonalityInfo,
    setSelectedMessage,
    setShowContextPanel,
    setIsCompressing,
    setLastCompression,
    setMemoryStats,
    setMemoryContext,
    setShowMemoryContext,
    setIsLoadingMessages,
  });

  const {
    handleSelectConversation,
    handleSelectSession,
    handleNewConversation,
    handleNewConversationWithAgent,
    handleStartHistoricalFollowUp,
  } = useConversationTransitions({
    selectedConversationID,
    setSelectedConversationID,
    selectedConversation,
    setSelectedConversation,
    selectedPersistedSession,
    setSelectedPersistedSession,
    conversations,
    agents,
    setLinkedAgent,
    messages,
    setMessages,
    setContextInfo,
    setPersonalityInfo,
    setMemoryStats,
    setMemoryContext,
    setShowMemoryContext,
    setIsLoadingMessages,
    setExpandedAgents,
    setSelectedAgentForNew,
    pendingLinkedAgentRef,
    beginConversationLoad,
    activateConsoleSession,
    refetch,
    toolModel,
    responseModel,
  });

  /* legacy extraction complete
    setSelectedConversationID(null);
    setSelectedConversation(null);
    setSelectedPersistedSession(null);
    setLinkedAgent(agent);
    beginConversationLoad({
      preserveLinkedAgent: true,
    });
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
      activateConsoleSession(sessionData);

      // Create a placeholder conversation for the UI
      const newConv: Conversation = {
        id: sessionData.session.id,
        title: `Chat with ${getAgentDisplayName(agent)}`,
        created_at: new Date().toISOString(),
        updated_at: new Date().toISOString(),
        message_count: 0,
      };
      setSelectedConversation(newConv);
      setSelectedConversationID(newConv.id);

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
  */

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
      if (
        selectedConversation?.id === conversationId ||
        selectedConversationID === conversationId
      ) {
        setSelectedConversationID(null);
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
    return findAgentForConversationID(convId, conversations, agents);
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
  const isHistoricalSessionView = selectedPersistedSession !== null;

  return (
    <div className="flex h-full">
      <ConversationListSidebar
        isLoading={isLoading}
        isFetching={isFetching}
        searchQuery={searchQuery}
        onSearchQueryChange={setSearchQuery}
        onNewConversation={handleNewConversation}
        onRefresh={() => {
          void refetch();
          void refetchSessions();
        }}
        agentSections={agentSections}
        groupedConversations={groupedConversations}
        expandedAgents={expandedAgents}
        selectedConversationId={selectedConversation?.id}
        linkableAgents={linkableAgents}
        editingConversationId={editingConversationId}
        editTitle={editTitle}
        onEditTitleChange={setEditTitle}
        editLinkedAgentId={editLinkedAgentId}
        onEditLinkedAgentIdChange={setEditLinkedAgentId}
        onToggleExpanded={toggleAgentExpanded}
        onSelectAgent={setSelectedAgent}
        onNewConversationWithAgent={handleNewConversationWithAgent}
        onSelectConversation={handleSelectConversation}
        onSaveRename={handleSaveRename}
        onCancelRename={handleCancelRename}
        onStartRename={handleStartRename}
        onDeleteConversation={handleDeleteConversation}
        feedItems={feedItems}
        selectedPersistedSessionId={selectedPersistedSession?.id}
        onSelectSession={handleSelectSession}
      />

      <ConversationWorkspace
        selectedConversation={selectedConversation}
        selectedPersistedSession={selectedPersistedSession}
        linkedAgent={linkedAgent}
        messages={messages}
        inflight={inflight}
        isLoadingMessages={isLoadingMessages}
        selectedMessage={selectedMessage}
        showContextPanel={showContextPanel}
        sessionId={sessionId}
        selectedAgentForNew={selectedAgentForNew}
        linkableAgents={linkableAgents}
        scrollRef={scrollRef}
        onSelectMessage={(message) => {
          setSelectedMessage(message);
          if (!isHistoricalSessionView) {
            setShowContextPanel(true);
          }
        }}
        onDeleteMessage={handleDeleteMessage}
        onToggleContextPanel={() => setShowContextPanel(!showContextPanel)}
        onStartHistoricalFollowUp={() => void handleStartHistoricalFollowUp()}
        onSend={handleSend}
        onCancel={handleCancel}
        onSelectedAgentForNewChange={setSelectedAgentForNew}
        onNewConversation={() => void handleNewConversation()}
        onNewConversationWithAgent={(agent) =>
          void handleNewConversationWithAgent(agent)
        }
        onOpenRuntime={(agent) => {
          if (agent) {
            setSelectedAgent(agent);
          }
          setActiveView("runtime");
        }}
      />

      {showContextPanel && selectedConversation && (
        <ConversationInspector
          selectedConversation={selectedConversation}
          onClose={() => setShowContextPanel(false)}
          agentSectionOpen={agentSectionOpen}
          onAgentSectionOpenChange={setAgentSectionOpen}
          agentOps={agentOps}
          chatModelDisplay={chatModelDisplay}
          conversationSettings={conversationSettings}
          defaultProvider={defaultProvider}
          linkedAgent={linkedAgent}
          providerAvailability={providerAvailability}
          selectedProvider={selectedProvider}
          setSelectedProvider={setSelectedProvider}
          maxHistoryTurns={maxHistoryTurns}
          setMaxHistoryTurns={setMaxHistoryTurns}
          selectedModel={selectedModel}
          setSelectedModel={setSelectedModel}
          customModelEnabled={customModelEnabled}
          setCustomModelEnabled={setCustomModelEnabled}
          customModel={customModel}
          setCustomModel={setCustomModel}
          customModelValue={customModelValue}
          isCustomSaved={isCustomSaved}
          isCustomDirty={isCustomDirty}
          saveCustomModel={saveCustomModel}
          compressionProvider={compressionProvider}
          setCompressionProvider={setCompressionProvider}
          compressionModel={compressionModel}
          setCompressionModel={setCompressionModel}
          isCompressing={isCompressing}
          onCompressMemory={handleCompressMemory}
          lastCompression={lastCompression}
          effectiveExecMode={effectiveExecMode}
          execModeOverride={execModeOverride}
          setExecModeOverride={setExecModeOverride}
          toolModel={toolModel}
          setToolModel={setToolModel}
          responseModel={responseModel}
          setResponseModel={setResponseModel}
          patchConversationSettings={patchConversationSettings}
          toolsAllowDraft={toolsAllowDraft}
          setToolsAllowDraft={setToolsAllowDraft}
          settingsError={settingsError}
          resetConversationSettings={resetConversationSettings}
          memoryStats={memoryStats}
          showMemoryContext={showMemoryContext}
          memoryContext={memoryContext}
          onToggleMemoryContext={async () => {
            if (showMemoryContext) {
              setShowMemoryContext(false);
              return;
            }
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
          contextInfo={contextInfo}
          messages={messages}
          editingSystemPrompt={editingSystemPrompt}
          setEditingSystemPrompt={setEditingSystemPrompt}
          systemPromptDraft={systemPromptDraft}
          setSystemPromptDraft={setSystemPromptDraft}
          setContextInfo={setContextInfo}
          personalityInfo={personalityInfo}
          onUpdatePersonalityDimension={(name, value) => {
            setPersonalityInfo((prev) => {
              if (!prev?.profile) return prev;
              return {
                ...prev,
                profile: {
                  ...prev.profile,
                  dimensions: prev.profile.dimensions.map((dimension) =>
                    dimension.name === name
                      ? { ...dimension, value }
                      : dimension,
                  ),
                },
              };
            });
            const existingTimer = personalityDebounceRefs.current.get(name);
            if (existingTimer) clearTimeout(existingTimer);
            const timer = setTimeout(async () => {
              try {
                await updatePersonalityDimension(
                  selectedConversation.id,
                  name,
                  value,
                );
              } catch (err) {
                console.error(
                  "Failed to update personality dimension:",
                  err,
                );
              }
              personalityDebounceRefs.current.delete(name);
            }, 300);
            personalityDebounceRefs.current.set(name, timer);
          }}
          selectedMessage={selectedMessage}
          setSelectedMessage={setSelectedMessage}
        />
      )}
    </div>
  );
}
