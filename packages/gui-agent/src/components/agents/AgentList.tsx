import { createElement, useMemo, useRef, useState } from "react";
import {
  useQuery,
  useQueries,
  useMutation,
  useQueryClient,
} from "@tanstack/react-query";
import { ScrollArea } from "@/components/ui/scroll-area";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import { Card, CardContent } from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import { HelpTooltip, Tooltip } from "@/components/ui/tooltip";
import { cn, formatRelativeTime } from "@/lib/utils";
import {
  listAgents,
  listRooms,
  listWorkspaces,
  spawnAgent,
  trashAgent,
  killAgent,
  startAgent,
  type SpawnAgentParams,
} from "@/api/client";
import type { Agent, AgentSpawnResponse, Room } from "@/api/types";
import type { ActivityEvent } from "@/api/types";
import { useActivityStore } from "@/stores/activityStore";
import { useActivityFocusStore } from "@/stores/activityFocusStore";
import { useViewStore } from "@/stores/viewStore";
import { AgentDetailView } from "./AgentDetailView";
import { SpawnAgentFormCore } from "./SpawnAgentFormCore";
import { RuntimeRoomPanel } from "@/components/rooms/RuntimeRoomPanel";
import {
  getRoleIcon,
  getAgentDisplayName,
  getPromptSummaryOrSubtitle,
  isWorkerAgent,
  getAgentActivityTimestamp,
  getAgentRepoDisplayName,
  isSandboxBackedAgent,
} from "@/lib/agent-utils";
import {
  indexRoomsByActor,
  isPathWorkspace,
  resolveRoomWorkspacePath,
  roomDisplayName,
} from "@/lib/room-utils";
import {
  AlertTriangle,
  Bot,
  Plus,
  RefreshCw,
  Search,
  Play,
  Square,
  Clock,
  Cpu,
  Users,
  Folder,
  Hash,
  Calendar,
  MessageSquare,
  Trash2,
  Wrench,
} from "lucide-react";

const STALE_RUNNING_MS = 10 * 60 * 1000;
const RECENT_STOPPED_MS = 2 * 60 * 60 * 1000;

type AttentionSignal = {
  agent: Agent;
  reason: string;
  severity: "error" | "warn" | "info";
};

/**
 * Renders the Agents management UI: list, search, spawn form, per-agent actions, and chat integration.
 *
 * Displays a searchable list of agents fetched from the server, shows state summary badges, and provides per-agent controls
 * (start, stop/kill, trash, open details, open chat). Includes a Spawn Agent form to create new agents and automatically
 * opens a chat session for newly spawned agents. Integrates with the chat and view stores to initialize console sessions
 * and load companion conversation history when starting a chat.
 *
 * @returns The React element for the agents management view.
 */
export function AgentList() {
  const [searchQuery, setSearchQuery] = useState("");
  const [showStopped, setShowStopped] = useState(false);
  const [trashLoadingAgentId, setTrashLoadingAgentId] = useState<string | null>(
    null,
  );
  const [killLoadingAgentId, setKillLoadingAgentId] = useState<string | null>(
    null,
  );
  const [startLoadingAgentId, setStartLoadingAgentId] = useState<string | null>(
    null,
  );
  const [bulkTrashLoading, setBulkTrashLoading] = useState(false);
  const queryClient = useQueryClient();
  const activityEvents = useActivityStore((s) => s.events);
  const setActivityFocus = useActivityFocusStore((s) => s.setFocus);

  const {
    selectedAgentID,
    selectedAgent,
    setSelectedAgent,
    setSelectedRoom,
    setActiveView,
    spawnAgentOpen,
    setSpawnAgentOpen,
  } = useViewStore();

  const { data, isLoading, refetch, isFetching } = useQuery({
    queryKey: ["agents"],
    queryFn: () => listAgents(100),
    refetchInterval: 10000,
  });
  const { data: workspacesData } = useQuery({
    queryKey: ["workspaces"],
    queryFn: listWorkspaces,
    staleTime: 10000,
  });

  const agents = useMemo(() => data?.agents ?? [], [data?.agents]);
  const workspaceEntries = useMemo(
    () => workspacesData?.workspaces ?? [],
    [workspacesData?.workspaces],
  );
  const currentWorkspace = workspacesData?.current;
  const normalizedQuery = searchQuery.trim().toLowerCase();
  const matchedAgents = normalizedQuery
    ? agents.filter(
        (a) =>
          a.name?.toLowerCase().includes(normalizedQuery) ||
          a.slug?.toLowerCase().includes(normalizedQuery) ||
          a.role?.toLowerCase().includes(normalizedQuery) ||
          a.id.toLowerCase().includes(normalizedQuery),
      )
    : agents;
  const sortedMatchedAgents = useMemo(() => {
    const rank: Record<Agent["state"], number> = {
      starting: 0,
      running: 0,
      error: 1,
      idle: 2,
      stopped: 3,
      unknown: 4,
    };
    const ts = (agent: Agent): number => {
      const raw = agent.heartbeat_at || agent.updated_at || agent.created_at;
      if (!raw) return 0;
      const parsed = Date.parse(raw);
      return Number.isFinite(parsed) ? parsed : 0;
    };

    return [...matchedAgents].sort((a, b) => {
      const byRank = rank[a.state] - rank[b.state];
      if (byRank !== 0) return byRank;
      const byTs = ts(b) - ts(a);
      if (byTs !== 0) return byTs;
      return getAgentDisplayName(a).localeCompare(getAgentDisplayName(b));
    });
  }, [matchedAgents]);
  const stoppedCountInResults = sortedMatchedAgents.filter(
    (a) => a.state === "stopped",
  ).length;
  const hideStoppedByDefault = normalizedQuery.length === 0 && !showStopped;
  const trashableStoppedAgents = useMemo(
    () => sortedMatchedAgents.filter((agent) => agent.state === "stopped"),
    [sortedMatchedAgents],
  );
  const visibleAgents = hideStoppedByDefault
    ? sortedMatchedAgents.filter((a) => a.state !== "stopped")
    : sortedMatchedAgents;
  const conversationalAgents = useMemo(
    () => visibleAgents.filter((agent) => !isWorkerAgent(agent)),
    [visibleAgents],
  );
  const workerAgents = useMemo(
    () => visibleAgents.filter((agent) => isWorkerAgent(agent)),
    [visibleAgents],
  );
  const conversationalTotal = useMemo(
    () => agents.filter((agent) => !isWorkerAgent(agent)).length,
    [agents],
  );
  const workerTotal = agents.length - conversationalTotal;
  const sandboxBackedCount = useMemo(
    () => agents.filter((agent) => isSandboxBackedAgent(agent)).length,
    [agents],
  );
  const latestStoppedAgent = useMemo(() => {
    return [...agents]
      .filter((agent) => agent.state === "stopped")
      .sort(
        (a, b) => getAgentActivityTimestamp(b) - getAgentActivityTimestamp(a),
      )[0];
  }, [agents]);
  const latestConversationalAgent = useMemo(() => {
    return [...agents]
      .filter((agent) => !isWorkerAgent(agent))
      .sort(
        (a, b) => getAgentActivityTimestamp(b) - getAgentActivityTimestamp(a),
      )[0];
  }, [agents]);
  const latestErrorEvent = useMemo(() => {
    return activityEvents.find((event) => event.status === "error");
  }, [activityEvents]);
  const needsAttention = useMemo<AttentionSignal[]>(() => {
    const now = Date.now();
    const out: AttentionSignal[] = [];
    const seen = new Set<string>();
    const push = (
      agent: Agent,
      reason: string,
      severity: AttentionSignal["severity"],
    ) => {
      if (seen.has(agent.id)) return;
      seen.add(agent.id);
      out.push({ agent, reason, severity });
    };

    const errored = [...agents]
      .filter((agent) => agent.state === "error")
      .sort(
        (a, b) => getAgentActivityTimestamp(b) - getAgentActivityTimestamp(a),
      );
    for (const agent of errored) {
      push(agent, "Errored and requires intervention.", "error");
    }

    const staleRunning = [...agents]
      .filter((agent) => {
        if (agent.state !== "running" && agent.state !== "idle") return false;
        const ts = getAgentActivityTimestamp(agent);
        return ts > 0 && now - ts > STALE_RUNNING_MS;
      })
      .sort(
        (a, b) => getAgentActivityTimestamp(a) - getAgentActivityTimestamp(b),
      );
    for (const agent of staleRunning) {
      push(
        agent,
        `No activity for ${Math.max(1, Math.floor((now - getAgentActivityTimestamp(agent)) / 60000))}m.`,
        "warn",
      );
    }

    const resumable = [...agents]
      .filter((agent) => {
        if (agent.state !== "stopped") return false;
        const ts = getAgentActivityTimestamp(agent);
        return ts > 0 && now - ts <= RECENT_STOPPED_MS;
      })
      .sort(
        (a, b) => getAgentActivityTimestamp(b) - getAgentActivityTimestamp(a),
      );
    for (const agent of resumable) {
      push(agent, "Recently stopped; likely resumable context.", "info");
    }

    return out.slice(0, 8);
  }, [agents]);
  const activeCount = agents.filter(
    (agent) => agent.state === "running" || agent.state === "idle",
  ).length;
  const resolvedSelectedAgent = useMemo(() => {
    if (selectedAgent) return selectedAgent;
    if (!selectedAgentID) return null;
    return agents.find((agent) => agent.id === selectedAgentID) ?? null;
  }, [agents, selectedAgent, selectedAgentID]);

  const roomWorkspaces = useMemo(
    () => {
      const knownPaths = workspaceEntries.map((workspace) => workspace.path);
      const fallbackCurrent = currentWorkspace;
      return [
        ...new Set(
          agents
            .map((agent) =>
              resolveRoomWorkspacePath(agent.ns, knownPaths, fallbackCurrent),
            )
            .filter((workspace) => isPathWorkspace(workspace)),
        ),
      ];
    },
    [agents, currentWorkspace, workspaceEntries],
  );
  const roomQueries = useQueries({
    queries: roomWorkspaces.map((workspaceID) => ({
      queryKey: ["rooms", workspaceID, "runtime-affinity"],
      queryFn: () => listRooms({ workspace_id: workspaceID, limit: 100 }),
      staleTime: 5000,
      retry: false,
    })),
  });
  const roomsByAgent = useMemo(() => {
    const allRooms: Room[] = [];
    for (const query of roomQueries) {
      for (const room of query.data?.rooms ?? []) {
        allRooms.push(room);
      }
    }
    return indexRoomsByActor(allRooms);
  }, [roomQueries]);

  // Runtime is the canonical workbench for agent-focused work in this slice.
  const handleOpenWorkbench = (agent: Agent) => {
    setSelectedAgent(agent);
    setActiveView("runtime");
  };

  const handleOpenRoom = (room: Room) => {
    setSelectedAgent(null);
    setSelectedRoom(room.id, room.workspace_id);
    setActiveView("rooms");
  };

  // Handle trashing a stopped agent
  const handleTrash = async (agent: Agent) => {
    if (agent.state !== "stopped") {
      console.error("Can only trash stopped agents");
      return;
    }

    // Confirm before trashing
    if (
      !window.confirm(
        `Are you sure you want to remove "${getAgentDisplayName(agent)}"? This action cannot be undone.`,
      )
    ) {
      return;
    }

    setTrashLoadingAgentId(agent.id);
    try {
      await trashAgent(agent.id);
      // Refresh the agent list
      queryClient.invalidateQueries({ queryKey: ["agents"] });
    } catch (err) {
      console.error("Failed to trash agent:", err);
      alert(err instanceof Error ? err.message : "Failed to trash agent");
    } finally {
      setTrashLoadingAgentId(null);
    }
  };

  // Handle killing a running agent
  const handleKill = async (agent: Agent) => {
    if (agent.state !== "running") {
      console.error("Can only kill running agents");
      return;
    }

    if (
      !window.confirm(
        `Are you sure you want to stop "${getAgentDisplayName(agent)}"?`,
      )
    ) {
      return;
    }

    setKillLoadingAgentId(agent.id);
    try {
      await killAgent(agent.id);
      // Refresh the agent list
      queryClient.invalidateQueries({ queryKey: ["agents"] });
    } catch (err) {
      console.error("Failed to kill agent:", err);
      alert(err instanceof Error ? err.message : "Failed to stop agent");
    } finally {
      setKillLoadingAgentId(null);
    }
  };

  // Handle starting/resuming a stopped agent
  const handleStart = async (agent: Agent) => {
    if (agent.state === "running") {
      console.error("Agent is already running");
      return;
    }

    setStartLoadingAgentId(agent.id);
    try {
      await startAgent(agent.id);
      // Refresh the agent list
      queryClient.invalidateQueries({ queryKey: ["agents"] });
    } catch (err) {
      console.error("Failed to start agent:", err);
      alert(err instanceof Error ? err.message : "Failed to start agent");
    } finally {
      setStartLoadingAgentId(null);
    }
  };

  const handleBulkTrashStopped = async () => {
    if (trashableStoppedAgents.length === 0) return;
    if (
      !window.confirm(
        `Trash ${trashableStoppedAgents.length} stopped agent${trashableStoppedAgents.length === 1 ? "" : "s"} currently in view? This removes their runtime records and cannot be undone.`,
      )
    ) {
      return;
    }

    setBulkTrashLoading(true);
    try {
      const results = await Promise.allSettled(
        trashableStoppedAgents.map((agent) => trashAgent(agent.id)),
      );
      const failures = results.filter(
        (result): result is PromiseRejectedResult => result.status === "rejected",
      );
      await queryClient.invalidateQueries({ queryKey: ["agents"] });
      if (failures.length > 0) {
        alert(
          `Trashed ${trashableStoppedAgents.length - failures.length} agent${trashableStoppedAgents.length - failures.length === 1 ? "" : "s"}, ${failures.length} failed.`,
        );
      }
    } catch (err) {
      alert(err instanceof Error ? err.message : "Failed to trash stopped agents");
    } finally {
      setBulkTrashLoading(false);
    }
  };
  const handleOpenLatestErrorTrace = (event: ActivityEvent | undefined) => {
    if (!event) return;
    if (!event.trace_id && !event.session_id) {
      setActiveView("events");
      return;
    }
    setActivityFocus({
      traceIDs: event.trace_id ? [event.trace_id] : [],
      sessionID: event.session_id,
      sourceSurface: "runtime",
      label: event.command
        ? `${event.command} (${event.operation})`
        : event.operation,
    });
    setActiveView("events");
  };

  // If an agent is selected, show detail view
  if (resolvedSelectedAgent) {
    return (
      <AgentDetailView
        agent={resolvedSelectedAgent}
        onBack={() => setSelectedAgent(null)}
      />
    );
  }

  // Count agents by state
  const agentCounts = agents.reduce(
    (acc, agent) => {
      acc[agent.state] = (acc[agent.state] || 0) + 1;
      return acc;
    },
    {} as Record<string, number>,
  );

  return (
    <div className="flex flex-col h-full">
      {/* Header */}
      <div className="p-4 border-b border-border space-y-3">
        <div className="flex items-center justify-between">
          <div className="flex items-center gap-2">
            <Users className="h-5 w-5" />
            <div>
              <div className="flex items-center gap-1.5">
                <h2 className="text-lg font-semibold text-foreground">Runtime</h2>
                <HelpTooltip
                  side="bottom"
                  content="Runtime is the main operations surface for starting, stopping, resuming, and inspecting agents."
                />
              </div>
              <div className="text-xs text-muted-foreground">
                Primary surface for agent lifecycle, coordination handoffs, and incident follow-up.
              </div>
            </div>
          </div>
          <div className="flex items-center gap-2">
            <Tooltip content="Reload the runtime agent list and current activity state.">
              <Button
                variant="ghost"
                size="icon"
                onClick={() => refetch()}
                disabled={isFetching}
                className="h-8 w-8"
              >
                <RefreshCw
                  className={cn("h-4 w-4", isFetching && "animate-spin")}
                />
              </Button>
            </Tooltip>
            <Tooltip content="Open the new-agent form and create another worker or conversational agent.">
              <Button
                size="sm"
                onClick={() => setSpawnAgentOpen(!spawnAgentOpen)}
              >
                <Plus className="h-4 w-4 mr-1" />
                Spawn Agent
              </Button>
            </Tooltip>
          </div>
        </div>

        {/* Agent state summary */}
        {agents.length > 0 && (
          <div className="flex items-center gap-2 flex-wrap">
            <Badge variant="secondary" className="text-xs">
              {agents.length} total
            </Badge>
            <Badge variant="secondary" className="text-xs">
              {conversationalTotal} conversational
            </Badge>
            <Badge variant="secondary" className="text-xs">
              {workerTotal} workers
            </Badge>
            {sandboxBackedCount > 0 && (
              <Badge
                variant="outline"
                className="text-xs border-sky-500/30 bg-sky-500/5 text-sky-600"
              >
                {sandboxBackedCount} sandbox-backed
              </Badge>
            )}
            {agentCounts.running > 0 && (
              <Badge className="text-xs bg-green-500/10 text-green-500 border-green-500/20">
                {agentCounts.running} running
              </Badge>
            )}
            {agentCounts.idle > 0 && (
              <Badge className="text-xs bg-yellow-500/10 text-yellow-500 border-yellow-500/20">
                {agentCounts.idle} idle
              </Badge>
            )}
            {agentCounts.stopped > 0 && (
              <Badge variant="outline" className="text-xs">
                {agentCounts.stopped} stopped
              </Badge>
            )}
            {agentCounts.error > 0 && (
              <Badge className="text-xs bg-red-500/10 text-red-500 border-red-500/20">
                {agentCounts.error}{" "}
                {agentCounts.error === 1 ? "error" : "errors"}
              </Badge>
            )}
          </div>
        )}

        {/* Search */}
        <div className="relative">
          <Search className="absolute left-2.5 top-2.5 h-4 w-4 text-muted-foreground" />
          <Input
            placeholder="Search by name, role, or ID..."
            value={searchQuery}
            onChange={(e) => setSearchQuery(e.target.value)}
            className="pl-9 h-9"
          />
        </div>
        {normalizedQuery.length === 0 && (
          <div className="flex items-center gap-2 flex-wrap">
            <span className="text-xs text-muted-foreground uppercase tracking-wider">
              Quick Actions
            </span>
            <Tooltip content="Restart the most recently stopped agent without searching through the full list.">
              <Button
                size="sm"
                variant="outline"
                className="h-7 text-xs"
                onClick={() =>
                  latestStoppedAgent && handleStart(latestStoppedAgent)
                }
                disabled={!latestStoppedAgent || !!startLoadingAgentId}
              >
                Resume Last Stopped
              </Button>
            </Tooltip>
            <Tooltip content="Jump directly to the newest error trace so you can inspect what failed.">
              <Button
                size="sm"
                variant="outline"
                className="h-7 text-xs"
                onClick={() => handleOpenLatestErrorTrace(latestErrorEvent)}
                disabled={
                  !latestErrorEvent ||
                  (!latestErrorEvent.trace_id && !latestErrorEvent.session_id)
                }
              >
                Open Latest Error Trace
              </Button>
            </Tooltip>
            <Tooltip content="Open the newest conversational agent workbench for a fast handoff into active work.">
              <Button
                size="sm"
                variant="outline"
                className="h-7 text-xs"
                onClick={() =>
                  latestConversationalAgent &&
                  handleOpenWorkbench(latestConversationalAgent)
                }
                disabled={!latestConversationalAgent}
              >
                Open Latest Workbench
              </Button>
            </Tooltip>
          </div>
        )}
        {normalizedQuery.length === 0 && (
          <Card className="bg-muted/30 border-border">
            <CardContent className="p-3 space-y-1.5">
              <div className="text-xs font-medium uppercase tracking-wider text-muted-foreground">
                Agent Utilization
              </div>
              <div className="text-xs text-foreground">
                Keep 1-3 conversational agents active; use workers for one-off
                tasks.
              </div>
              {agentCounts.stopped > activeCount && (
                <div className="text-xs text-muted-foreground">
                  Stopped backlog is high ({agentCounts.stopped}). Resume useful
                  agents or remove old ones.
                </div>
              )}
              {agentCounts.error > 0 && (
                <div className="text-xs text-muted-foreground">
                  {agentCounts.error} errored agents detected. Use error traces
                  before restarting.
                </div>
              )}
              {activeCount === 0 && (
                <div className="text-xs text-muted-foreground">
                  No active agents. Spawn one proactive researcher to keep
                  runtime alive.
                </div>
              )}
            </CardContent>
          </Card>
        )}
        {normalizedQuery.length === 0 && stoppedCountInResults > 0 && (
          <div className="flex items-center gap-2">
            <Button
              size="sm"
              variant="outline"
              className="h-7 text-xs"
              onClick={() => setShowStopped((prev) => !prev)}
            >
              {showStopped
                ? "Hide Stopped"
                : `Show Stopped (${stoppedCountInResults})`}
            </Button>
            {!showStopped && (
              <span className="text-xs text-muted-foreground">
                Runtime defaults to active/error agents to reduce noise.
              </span>
            )}
            {!hideStoppedByDefault && trashableStoppedAgents.length > 0 && (
              <Button
                size="sm"
                variant="outline"
                className="h-7 text-xs border-red-500/30 text-red-600 hover:bg-red-500/5"
                onClick={handleBulkTrashStopped}
                disabled={bulkTrashLoading}
              >
                <Trash2 className="h-3.5 w-3.5" />
                Trash Visible Stopped ({trashableStoppedAgents.length})
              </Button>
            )}
          </div>
        )}

      </div>

      {/* Spawn Form */}
      {spawnAgentOpen && (
        <SpawnAgentForm
          onClose={() => setSpawnAgentOpen(false)}
          onSuccess={async (
            actorId: string,
            spawnData: AgentSpawnResponse,
            params?: SpawnAgentParams,
          ) => {
            setSpawnAgentOpen(false);

            // Refresh the agent list
            await queryClient.invalidateQueries({ queryKey: ["agents"] });

            // Extract info from actor_id (format: "actor:role:UUID")
            const parts = actorId.split(":");
            const agentId = parts.pop() || actorId;
            const role = parts.length > 1 ? parts[1] : "agent";
            let resolvedAgent: Agent | undefined;

            try {
              const refreshed = await listAgents(100);
              resolvedAgent = refreshed.agents.find(
                (agent) => agent.id === agentId,
              );
            } catch {
              resolvedAgent = undefined;
            }

            // Construct a minimal agent from spawn response to open chat immediately
            const newAgent: Agent = {
              id: agentId,
              name: spawnData.name || "New Agent",
              slug: spawnData.name?.toLowerCase().replace(/\s+/g, "-"),
              role: role,
              memory_scope: params?.memory_scope,
              memory_retention: params?.memory_retention,
              skills_allow: [],
              share_bb: "none",
              state: (spawnData.status || "running") as Agent["state"],
              ns: resolvedAgent?.ns || params?.workspace_id || "/",
              created_at: new Date().toISOString(),
              updated_at: new Date().toISOString(),
            };

            if (resolvedAgent) {
              handleOpenWorkbench(resolvedAgent);
              return;
            }
            if (params?.workspace_id) {
              handleOpenWorkbench(newAgent);
              return;
            }
            // If namespace is not yet available, avoid opening chat in the wrong workspace.
            setActiveView("runtime");
          }}
        />
      )}

      {/* Agent List */}
      <ScrollArea className="flex-1">
        <div className="p-4 space-y-3">
          {normalizedQuery.length === 0 && <RuntimeRoomPanel agents={agents} />}
          {isLoading ? (
            <div className="text-center py-12 text-muted-foreground">
              <RefreshCw className="h-8 w-8 mx-auto mb-2 animate-spin" />
              <p>Loading agents...</p>
            </div>
          ) : visibleAgents.length === 0 ? (
            <div className="text-center py-12 text-muted-foreground">
              <div className="h-16 w-16 mx-auto mb-4 rounded-xl bg-muted flex items-center justify-center">
                <Bot className="h-8 w-8 opacity-40" />
              </div>
              <p className="text-lg font-medium text-foreground">
                {normalizedQuery
                  ? "No matching agents"
                  : stoppedCountInResults > 0
                    ? "Stopped agents are hidden"
                    : "No agents running"}
              </p>
              <p className="text-sm mt-1 max-w-xs mx-auto">
                {normalizedQuery
                  ? `No agents match "${searchQuery}". Try a different search.`
                  : stoppedCountInResults > 0
                    ? "Use “Show Stopped” above to inspect archived runtime agents."
                    : "Spawn autonomous agents to perform tasks like research, coding, or reviewing."}
              </p>
              {!normalizedQuery && stoppedCountInResults === 0 && (
                <Button
                  size="sm"
                  className="mt-4"
                  onClick={() => setSpawnAgentOpen(true)}
                >
                  <Plus className="h-4 w-4 mr-1" />
                  Spawn First Agent
                </Button>
              )}
            </div>
          ) : (
            <>
              {normalizedQuery.length === 0 && needsAttention.length > 0 && (
                <div className="space-y-2">
                  <div className="flex items-center justify-between">
                    <div className="text-xs font-medium uppercase tracking-wider text-muted-foreground inline-flex items-center gap-1.5">
                      <AlertTriangle className="h-3.5 w-3.5 text-amber-500" />
                      Needs Attention
                    </div>
                    <Badge variant="secondary" className="text-[10px]">
                      {needsAttention.length}
                    </Badge>
                  </div>
                  {needsAttention.map(({ agent, reason, severity }) => (
                    <Card
                      key={`attention-${agent.id}`}
                      className={cn(
                        severity === "error" && "border-red-500/30",
                        severity === "warn" && "border-amber-500/30",
                      )}
                    >
                      <CardContent className="p-3 flex items-center justify-between gap-3">
                        <div className="min-w-0">
                          <div className="text-sm font-medium text-foreground truncate">
                            {getAgentDisplayName(agent)}
                          </div>
                          <div className="text-xs text-muted-foreground truncate">
                            {reason}
                          </div>
                        </div>
                        <div className="flex items-center gap-1 flex-shrink-0">
                          {agent.state === "stopped" && (
                            <Button
                              variant="outline"
                              size="sm"
                              className="h-7 text-xs"
                              onClick={() => handleStart(agent)}
                              disabled={startLoadingAgentId === agent.id}
                            >
                              Start
                            </Button>
                          )}
                          <Button
                            variant="outline"
                            size="sm"
                            className="h-7 text-xs"
                            onClick={() => setSelectedAgent(agent)}
                          >
                            Open Workbench
                          </Button>
                        </div>
                      </CardContent>
                    </Card>
                  ))}
                </div>
              )}
              {conversationalAgents.length > 0 && (
                <div className="space-y-3">
                  <div className="flex items-center justify-between">
                    <div className="text-xs font-medium uppercase tracking-wider text-muted-foreground">
                      Conversational Agents
                    </div>
                    <Badge variant="secondary" className="text-[10px]">
                      {conversationalAgents.length}
                    </Badge>
                  </div>
                  {conversationalAgents.map((agent) => (
                    <AgentCard
                      key={agent.id}
                      agent={agent}
                      rooms={roomsByAgent.get(agent.id) ?? []}
                      onOpenRoom={handleOpenRoom}
                      onOpenWorkbench={handleOpenWorkbench}
                      onTrash={handleTrash}
                      onKill={handleKill}
                      onStart={handleStart}
                      isTrashLoading={trashLoadingAgentId === agent.id}
                      isKillLoading={killLoadingAgentId === agent.id}
                      isStartLoading={startLoadingAgentId === agent.id}
                    />
                  ))}
                </div>
              )}

              {workerAgents.length > 0 && (
                <div className="space-y-3 pt-1">
                  <div className="flex items-center justify-between">
                    <div className="text-xs font-medium uppercase tracking-wider text-muted-foreground inline-flex items-center gap-1.5">
                      <Wrench className="h-3.5 w-3.5" />
                      Worker Agents
                    </div>
                    <Badge variant="secondary" className="text-[10px]">
                      {workerAgents.length}
                    </Badge>
                  </div>
                  {workerAgents.map((agent) => (
                    <AgentCard
                      key={agent.id}
                      agent={agent}
                      rooms={roomsByAgent.get(agent.id) ?? []}
                      onOpenRoom={handleOpenRoom}
                      onOpenWorkbench={handleOpenWorkbench}
                      onTrash={handleTrash}
                      onKill={handleKill}
                      onStart={handleStart}
                      isTrashLoading={trashLoadingAgentId === agent.id}
                      isKillLoading={killLoadingAgentId === agent.id}
                      isStartLoading={startLoadingAgentId === agent.id}
                    />
                  ))}
                </div>
              )}
            </>
          )}
        </div>
      </ScrollArea>
    </div>
  );
}

interface AgentCardProps {
  agent: Agent;
  rooms: Room[];
  onOpenRoom: (room: Room) => void;
  onOpenWorkbench: (agent: Agent) => void;
  onTrash: (agent: Agent) => void;
  onKill: (agent: Agent) => void;
  onStart: (agent: Agent) => void;
  isTrashLoading?: boolean;
  isKillLoading?: boolean;
  isStartLoading?: boolean;
}

/**
 * Render a compact card showing an agent's metadata, status, and action buttons.
 *
 * Displays the agent's name, role, ID, workspace, model, timestamps, skills, and a status indicator,
 * and exposes controls to open a chat, view details, start, stop (kill), or remove (trash) the agent.
 *
 * @param agent - The agent object to display
 * @param onOpenWorkbench - Callback invoked when the workbench action is triggered
 * @param onTrash - Callback invoked when the "remove/trash" action is triggered
 * @param onKill - Callback invoked when the "stop/kill" action is triggered
 * @param onStart - Callback invoked when the "start/resume" action is triggered
 * @param isTrashLoading - Whether the trash action is currently loading (disables trash button)
 * @param isKillLoading - Whether the kill action is currently loading (disables kill button)
 * @param isStartLoading - Whether the start action is currently loading (disables start button)
 * @returns A JSX element representing the agent card
 */
function AgentCard({
  agent,
  rooms,
  onOpenRoom,
  onOpenWorkbench,
  onTrash,
  onKill,
  onStart,
  isTrashLoading,
  isKillLoading,
  isStartLoading,
}: AgentCardProps) {
  const roleIcon = getRoleIcon(agent.role);
  const sandboxBacked = isSandboxBackedAgent(agent);
  const repoLabel = getAgentRepoDisplayName(agent.repo_url);
  const stateColors: Record<string, string> = {
    running: "bg-green-500",
    idle: "bg-yellow-500",
    stopped: "bg-gray-500",
    error: "bg-red-500",
  };

  const stateLabels: Record<string, string> = {
    running: "Running",
    idle: "Idle",
    stopped: "Stopped",
    error: "Error",
  };

  const getWorkspaceDisplayName = (ns: string) => {
    if (!ns || ns === "/") return "root";
    const parts = ns.split("/");
    return parts[parts.length - 1] || ns;
  };

  const getWorkspaceRootDisplayName = (workspaceRoot?: string) => {
    if (!workspaceRoot) return null;
    const segments = workspaceRoot.split("/").filter(Boolean);
    const leaf = segments[segments.length - 1];
    return leaf || workspaceRoot;
  };

  return (
    <Card className="hover:bg-accent/30 transition-colors">
      <CardContent className="p-4">
        <div className="flex items-start justify-between">
          <div className="flex items-start gap-3 flex-1 min-w-0">
            <div className="relative flex-shrink-0">
                <div
                  className={cn(
                    "h-10 w-10 rounded-lg flex items-center justify-center",
                    agent.state === "running" ? "bg-green-500/10" : "bg-muted",
                  )}
                >
                {createElement(roleIcon, {
                  className: cn(
                    "h-5 w-5",
                    agent.state === "running"
                      ? "text-green-500"
                      : "text-muted-foreground",
                  ),
                })}
              </div>
              <span
                className={cn(
                  "absolute -bottom-0.5 -right-0.5 h-3 w-3 rounded-full border-2 border-card",
                  stateColors[agent.state] || "bg-gray-500",
                )}
                aria-label={stateLabels[agent.state] || agent.state}
              />
            </div>
            <div className="flex-1 min-w-0">
              <div className="flex items-center gap-2 flex-wrap">
                <span className="font-medium text-foreground capitalize">
                  {getAgentDisplayName(agent)}
                </span>
                {agent.role && agent.name && (
                  <Badge variant="secondary" className="text-xs">
                    {agent.name}
                  </Badge>
                )}
                <Badge
                  variant={agent.state === "running" ? "default" : "outline"}
                  className={cn(
                    "text-xs",
                    agent.state === "running" &&
                      "bg-green-500/10 text-green-500 border-green-500/20",
                  )}
                >
                  {stateLabels[agent.state] || agent.state}
                </Badge>
                <Badge
                  variant="outline"
                  className={cn(
                    "text-xs",
                    sandboxBacked &&
                      "border-sky-500/30 bg-sky-500/5 text-sky-600",
                  )}
                >
                  {sandboxBacked ? "sandbox clone" : "local runtime"}
                </Badge>
                {sandboxBacked && agent.sandbox_provider && (
                  <Badge variant="secondary" className="text-xs">
                    {agent.sandbox_provider}
                  </Badge>
                )}
              </div>

              {/* ID and Namespace */}
              <div className="flex items-center gap-3 mt-1 text-xs text-muted-foreground">
                <span
                  className="flex items-center gap-1 font-mono"
                  title={agent.id}
                >
                  <Hash className="h-3 w-3" />
                  {agent.id.slice(0, 8)}
                </span>
                {agent.ns && (
                  <span className="flex items-center gap-1" title={agent.ns}>
                    <Folder className="h-3 w-3" />
                    {getWorkspaceDisplayName(agent.ns)}
                  </span>
                )}
                {agent.workspace_root && (
                  <span
                    className="flex items-center gap-1"
                    title={agent.workspace_root}
                  >
                    <Folder className="h-3 w-3" />
                    {getWorkspaceRootDisplayName(agent.workspace_root)}
                  </span>
                )}
              </div>

              {/* Prompt Summary */}
              {agent.prompt_summary && (
                <p className="mt-1 text-xs text-muted-foreground line-clamp-1">
                  {getPromptSummaryOrSubtitle(agent)}
                </p>
              )}

              {(repoLabel || agent.sandbox_id) && (
                <div className="mt-2 flex items-center gap-1 flex-wrap">
                  {repoLabel && (
                    <Badge variant="outline" className="text-xs">
                      {repoLabel}
                      {agent.repo_ref ? ` @ ${agent.repo_ref}` : ""}
                    </Badge>
                  )}
                  {agent.sandbox_id && (
                    <Badge variant="secondary" className="text-xs font-mono">
                      sbx:{agent.sandbox_id.slice(0, 10)}
                    </Badge>
                  )}
                </div>
              )}

              {/* Model and Timing info */}
              <div className="flex items-center gap-3 mt-2 text-xs text-muted-foreground flex-wrap">
                <span
                  className="flex items-center gap-1"
                  title={`Provider: ${agent.llm_provider || "default"}`}
                >
                  <Cpu className="h-3 w-3" />
                  {agent.llm_model || "default model"}
                </span>
                {agent.created_at && (
                  <span
                    className="flex items-center gap-1"
                    title={`Created: ${new Date(agent.created_at).toLocaleString()}`}
                  >
                    <Calendar className="h-3 w-3" />
                    {formatRelativeTime(agent.created_at)}
                  </span>
                )}
                {agent.heartbeat_at && (
                  <span
                    className="flex items-center gap-1"
                    title={`Last heartbeat: ${new Date(agent.heartbeat_at).toLocaleString()}`}
                  >
                    <Clock className="h-3 w-3" />
                    {formatRelativeTime(agent.heartbeat_at)}
                  </span>
                )}
              </div>

              {/* Skills if present */}
              {agent.skills_allow && agent.skills_allow.length > 0 && (
                <div className="mt-2 flex items-center gap-1 flex-wrap">
                  {agent.skills_allow.slice(0, 3).map((skill) => (
                    <Badge
                      key={skill}
                      variant="secondary"
                      className="text-xs font-mono"
                    >
                      {skill}
                    </Badge>
                  ))}
                  {agent.skills_allow.length > 3 && (
                    <Badge variant="secondary" className="text-xs">
                      +{agent.skills_allow.length - 3}
                    </Badge>
                  )}
                </div>
              )}
              {rooms.length > 0 && (
                <div className="mt-2 flex items-center gap-1 flex-wrap">
                  {rooms.slice(0, 2).map((room) => (
                    <button
                      key={room.id}
                      type="button"
                      onClick={(event) => {
                        event.stopPropagation();
                        onOpenRoom(room);
                      }}
                    >
                      <Badge variant="outline" className="text-xs">
                        room:{roomDisplayName(room)}
                      </Badge>
                    </button>
                  ))}
                  {rooms.length > 2 && (
                    <Badge variant="outline" className="text-xs">
                      +{rooms.length - 2} rooms
                    </Badge>
                  )}
                </div>
              )}
            </div>
          </div>

          {/* Actions */}
          <div className="flex items-center gap-1 flex-shrink-0 flex-wrap justify-end">
            <Tooltip content="Open this agent's workbench to inspect its runtime, rooms, and conversation context.">
              <Button
                variant="outline"
                size="sm"
                className="h-8 text-xs gap-1.5 text-primary hover:text-primary hover:bg-primary/10"
                onClick={() => onOpenWorkbench(agent)}
              >
                <MessageSquare className="h-3.5 w-3.5" />
                Workbench
              </Button>
            </Tooltip>
            {rooms.length > 0 && (
              <Tooltip content="Jump straight into the first room currently linked to this agent.">
                <Button
                  variant="outline"
                  size="sm"
                  className="h-8 text-xs gap-1.5"
                  onClick={() => onOpenRoom(rooms[0])}
                >
                  <Hash className="h-3.5 w-3.5" />
                  Open Room
                </Button>
              </Tooltip>
            )}
            {agent.state === "running" ? (
              <Tooltip content="Stop the running agent. Use this when you want to halt work without deleting the agent record.">
                <Button
                  variant="outline"
                  size="sm"
                  className="h-8 text-xs gap-1.5 text-orange-500 hover:text-orange-600 hover:bg-orange-500/10"
                  onClick={(e) => {
                    e.stopPropagation();
                    onKill(agent);
                  }}
                  disabled={isKillLoading}
                >
                  {isKillLoading ? (
                    <RefreshCw className="h-3.5 w-3.5 animate-spin" />
                  ) : (
                    <Square className="h-3.5 w-3.5" />
                  )}
                  Stop
                </Button>
              </Tooltip>
            ) : agent.state === "stopped" ? (
              <>
                <Tooltip content="Start this stopped agent again and return it to active work.">
                  <Button
                    variant="outline"
                    size="sm"
                    className="h-8 text-xs gap-1.5 text-green-500 hover:text-green-600 hover:bg-green-500/10"
                    onClick={(e) => {
                      e.stopPropagation();
                      onStart(agent);
                    }}
                    disabled={isStartLoading}
                  >
                    {isStartLoading ? (
                      <RefreshCw className="h-3.5 w-3.5 animate-spin" />
                    ) : (
                      <Play className="h-3.5 w-3.5" />
                    )}
                    Start
                  </Button>
                </Tooltip>
                <Tooltip content="Delete this stopped agent from the runtime list. This does not preserve the agent as an active worker.">
                  <Button
                    variant="outline"
                    size="sm"
                    className="h-8 text-xs gap-1.5 text-red-500 hover:text-red-600 hover:bg-red-500/10"
                    onClick={() => onTrash(agent)}
                    disabled={isTrashLoading}
                  >
                    <Trash2
                      className={cn(
                        "h-3.5 w-3.5",
                        isTrashLoading && "animate-pulse",
                      )}
                    />
                    Remove
                  </Button>
                </Tooltip>
              </>
            ) : (
              <Tooltip content="Start or resume this agent so it can begin handling work again.">
                <Button
                  variant="outline"
                  size="sm"
                  className="h-8 text-xs gap-1.5 text-green-500 hover:text-green-600 hover:bg-green-500/10"
                  onClick={(e) => {
                    e.stopPropagation();
                    onStart(agent);
                  }}
                  disabled={isStartLoading}
                >
                  {isStartLoading ? (
                    <RefreshCw className="h-3.5 w-3.5 animate-spin" />
                  ) : (
                    <Play className="h-3.5 w-3.5" />
                  )}
                  Start
                </Button>
              </Tooltip>
            )}
          </div>
        </div>
      </CardContent>
    </Card>
  );
}

interface SpawnAgentFormProps {
  onClose: () => void;
  onSuccess: (
    actorId: string,
    spawnData: AgentSpawnResponse,
    params?: SpawnAgentParams,
  ) => void;
}

/**
 * Render a form UI for configuring and spawning a new agent.
 *
 * Wraps SpawnAgentFormCore with a mutation that calls the spawn API
 * and invokes the success callback with the returned actor id and data.
 *
 * @param onClose - Called when the user cancels or closes the form
 * @param onSuccess - Called after a successful spawn with the new agent's `actorId` and the full spawn response
 * @returns The spawn agent form React element
 */
function SpawnAgentForm({ onClose, onSuccess }: SpawnAgentFormProps) {
  const lastSubmittedRef = useRef<SpawnAgentParams | undefined>(undefined);
  const mutation = useMutation({
    mutationFn: (params: SpawnAgentParams) => spawnAgent(params),
    onSuccess: (data) => {
      onSuccess(data.actor_id, data, lastSubmittedRef.current);
    },
    onError: (error) => {
      console.error("[SpawnAgentForm] Spawn failed:", error);
    },
  });

  return (
    <div className="p-4 border-b border-border bg-muted/30 max-h-[70vh] overflow-y-auto">
      <SpawnAgentFormCore
        onSubmit={(params) => {
          lastSubmittedRef.current = params;
          mutation.mutate(params);
        }}
        onCancel={onClose}
        isPending={mutation.isPending}
        error={mutation.error instanceof Error ? mutation.error : null}
      />
    </div>
  );
}
