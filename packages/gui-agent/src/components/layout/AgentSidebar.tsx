import { createElement, useMemo, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { cn } from "@/lib/utils";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import { ScrollArea } from "@/components/ui/scroll-area";
import { useActivityStore } from "@/stores/activityStore";
import { useViewStore } from "@/stores/viewStore";
import { killAgent, listAgents, trashAgent } from "@/api/client";
import type { Agent } from "@/api/types";
import {
  getRoleIcon,
  getAgentActivityTimestamp,
  getAgentDisplayName,
  isWorkerAgent,
} from "@/lib/agent-utils";
import {
  Search,
  FileText,
  Zap,
  Settings,
  Plus,
  ChevronLeft,
  ChevronRight,
  Activity,
  MessagesSquare,
  Wrench,
} from "lucide-react";
import type { ViewType } from "@/stores/viewStore";

interface SidebarItem {
  id: ViewType;
  icon: React.ReactNode;
  label: string;
  badge?: number;
  active?: boolean;
  type: "agent" | "action";
}

interface AgentSidebarProps {
  activeView: ViewType;
  onViewChange: (view: ViewType) => void;
}

const ONE_DAY_MS = 24 * 60 * 60 * 1000;

export function AgentSidebar({ activeView, onViewChange }: AgentSidebarProps) {
  const [collapsed, setCollapsed] = useState(false);
  const [showWorkers, setShowWorkers] = useState(false);
  const [isMutatingWorkers, setIsMutatingWorkers] = useState(false);
  const activityCount = useActivityStore((s) => s.events.length);
  const connected = useActivityStore((s) => s.connected);
  const { selectedAgent, setSelectedAgent, setSpawnAgentOpen } = useViewStore();

  const { data: agentsData, refetch: refetchAgents } = useQuery({
    queryKey: ["agents"],
    queryFn: () => listAgents(100),
    refetchInterval: 10000,
  });

  const agents = useMemo(() => agentsData?.agents ?? [], [agentsData?.agents]);
  const { conversationalAgents, workerAgents } = useMemo(() => {
    const oneDayAgo = Date.now() - ONE_DAY_MS;
    const conversational: Agent[] = [];
    const workers: Agent[] = [];

    for (const agent of agents) {
      if (isWorkerAgent(agent)) {
        workers.push(agent);
      } else {
        const isImportantState =
          agent.state === "running" ||
          agent.state === "idle" ||
          agent.state === "error";
        const isRecent = getAgentActivityTimestamp(agent) >= oneDayAgo;
        if (isImportantState || isRecent || selectedAgent?.id === agent.id) {
          conversational.push(agent);
        }
      }
    }

    const statusRank = (state: Agent["state"]): number => {
      switch (state) {
        case "running":
          return 0;
        case "idle":
          return 1;
        case "error":
          return 2;
        case "stopped":
          return 3;
        default:
          return 4;
      }
    };

    conversational.sort((a, b) => {
      const byStatus = statusRank(a.state) - statusRank(b.state);
      if (byStatus !== 0) return byStatus;
      return getAgentActivityTimestamp(b) - getAgentActivityTimestamp(a);
    });
    workers.sort((a, b) => {
      const byStatus = statusRank(a.state) - statusRank(b.state);
      if (byStatus !== 0) return byStatus;
      return getAgentActivityTimestamp(b) - getAgentActivityTimestamp(a);
    });

    return {
      conversationalAgents: conversational,
      workerAgents: workers,
    };
  }, [agents, selectedAgent?.id]);

  const workerRunning = workerAgents.filter(
    (agent) => agent.state === "running" || agent.state === "idle",
  ).length;
  const workerErrored = workerAgents.filter(
    (agent) => agent.state === "error",
  ).length;
  const workerStopped = workerAgents.filter(
    (agent) => agent.state === "stopped",
  ).length;
  const workerTrashable = workerAgents.filter((agent) => {
    if (agent.state !== "stopped") return false;
    const ts = getAgentActivityTimestamp(agent);
    return ts > 0 && ts < Date.now() - ONE_DAY_MS;
  }).length;

  const quickActions: SidebarItem[] = [
    {
      id: "search",
      icon: <Search className="h-4 w-4" />,
      label: "Search",
      type: "action",
    },
    {
      id: "logs",
      icon: <FileText className="h-4 w-4" />,
      label: "Logs",
      type: "action",
    },
    {
      id: "skills",
      icon: <Zap className="h-4 w-4" />,
      label: "Run Skill",
      type: "action",
    },
  ];

  const handleAgentClick = (agent: Agent) => {
    setSelectedAgent(agent);
    onViewChange("agents");
  };

  const handleKillStoppedWorkers = async () => {
    const targets = workerAgents.filter((agent) => agent.state === "stopped");
    if (targets.length === 0) return;
    if (!window.confirm(`Stop ${targets.length} stopped workers now?`)) return;

    setIsMutatingWorkers(true);
    try {
      await Promise.allSettled(targets.map((agent) => killAgent(agent.id)));
      await refetchAgents();
    } finally {
      setIsMutatingWorkers(false);
    }
  };

  const handleTrashOldWorkers = async () => {
    const cutoff = Date.now() - ONE_DAY_MS;
    const targets = workerAgents.filter((agent) => {
      if (agent.state !== "stopped") return false;
      const ts = getAgentActivityTimestamp(agent);
      return ts > 0 && ts < cutoff;
    });
    if (targets.length === 0) return;
    if (!window.confirm(`Trash ${targets.length} old workers?`)) return;

    setIsMutatingWorkers(true);
    try {
      await Promise.allSettled(targets.map((agent) => trashAgent(agent.id)));
      await refetchAgents();
    } finally {
      setIsMutatingWorkers(false);
    }
  };

  return (
    <aside
      className={cn(
        "flex flex-col bg-card border-r border-border transition-all duration-200",
        collapsed ? "w-14" : "w-56",
      )}
    >
      <div className="h-12 flex items-center justify-between px-3 border-b border-border">
        {!collapsed && (
          <span className="text-sm font-semibold text-foreground">Agents</span>
        )}
        <Button
          variant="ghost"
          size="icon"
          className="h-7 w-7"
          onClick={() => setCollapsed(!collapsed)}
        >
          {collapsed ? (
            <ChevronRight className="h-4 w-4" />
          ) : (
            <ChevronLeft className="h-4 w-4" />
          )}
        </Button>
      </div>

      <ScrollArea className="flex-1 py-2">
        <div className="px-2 mb-4">
          {!collapsed && (
            <span className="text-xs font-medium text-muted-foreground uppercase tracking-wider px-2">
              Chat
            </span>
          )}
          <SidebarButton
            item={{
              id: "conversations",
              icon: <MessagesSquare className="h-4 w-4" />,
              label: "Conversations",
              type: "agent",
            }}
            collapsed={collapsed}
            active={activeView === "conversations"}
            onClick={() => {
              setSelectedAgent(null);
              onViewChange("conversations");
            }}
          />
        </div>

        <div className="px-2 mb-4">
          {!collapsed && (
            <div className="flex items-center justify-between mb-2 px-2">
              <span className="text-xs font-medium text-muted-foreground uppercase tracking-wider">
                Agents
              </span>
              <Button
                variant="ghost"
                size="icon"
                className="h-5 w-5"
                title="Spawn new agent"
                onClick={() => {
                  setSelectedAgent(null);
                  setSpawnAgentOpen(true);
                }}
              >
                <Plus className="h-3 w-3" />
              </Button>
            </div>
          )}
          {agents.length === 0
            ? !collapsed && (
                <div className="px-2 py-2 text-xs text-muted-foreground">
                  No agents yet
                </div>
              )
            : conversationalAgents.map((agent) => (
                <AgentButton
                  key={agent.id}
                  agent={agent}
                  collapsed={collapsed}
                  selected={selectedAgent?.id === agent.id}
                  onClick={() => handleAgentClick(agent)}
                />
              ))}

          {workerAgents.length > 0 && (
            <div
              className={cn(
                "mt-2",
                !collapsed && "border-t border-border pt-2",
              )}
            >
              <Button
                variant="ghost"
                className={cn(
                  "w-full justify-start mb-1 px-2",
                  collapsed && "justify-center",
                )}
                onClick={() => setShowWorkers((prev) => !prev)}
                title="Worker agents"
              >
                <Wrench className="h-4 w-4" />
                {!collapsed && (
                  <>
                    <span className="ml-2 truncate">
                      Workers ({workerAgents.length})
                    </span>
                    <Badge variant="secondary" className="ml-auto text-[10px]">
                      {workerRunning} run
                    </Badge>
                  </>
                )}
              </Button>
              {!collapsed && (
                <div className="px-2 pb-1 text-[10px] text-muted-foreground">
                  {workerRunning} running, {workerErrored} errored,{" "}
                  {workerStopped} stopped
                </div>
              )}
              {showWorkers && (
                <>
                  {!collapsed && (
                    <div className="px-2 mb-2 flex items-center gap-1">
                      <Button
                        variant="outline"
                        size="sm"
                        className="h-6 text-[10px] px-2"
                        onClick={() => void handleKillStoppedWorkers()}
                        disabled={workerStopped === 0 || isMutatingWorkers}
                      >
                        Stop Stopped
                      </Button>
                      <Button
                        variant="outline"
                        size="sm"
                        className="h-6 text-[10px] px-2"
                        onClick={() => void handleTrashOldWorkers()}
                        disabled={workerTrashable === 0 || isMutatingWorkers}
                      >
                        Trash Old
                      </Button>
                    </div>
                  )}
                  {workerAgents.map((agent) => (
                    <AgentButton
                      key={agent.id}
                      agent={agent}
                      collapsed={collapsed}
                      selected={selectedAgent?.id === agent.id}
                      onClick={() => handleAgentClick(agent)}
                    />
                  ))}
                </>
              )}
            </div>
          )}
        </div>

        <div className="px-2 mb-4">
          {!collapsed && (
            <span className="text-xs font-medium text-muted-foreground uppercase tracking-wider px-2">
              Activity
            </span>
          )}
          <SidebarButton
            item={{
              id: "activity",
              icon: <Activity className="h-4 w-4" />,
              label: "Activity",
              badge:
                activityCount > 0 ? Math.min(activityCount, 99) : undefined,
              type: "action",
            }}
            collapsed={collapsed}
            active={activeView === "activity"}
            onClick={() => onViewChange("activity")}
            connected={connected}
          />
        </div>

        <div className="px-2">
          {!collapsed && (
            <span className="text-xs font-medium text-muted-foreground uppercase tracking-wider px-2">
              Quick Actions
            </span>
          )}
          {quickActions.map((item) => (
            <SidebarButton
              key={item.id}
              item={item}
              collapsed={collapsed}
              active={activeView === item.id}
              onClick={() => onViewChange(item.id)}
            />
          ))}
        </div>
      </ScrollArea>

      <div className="p-2 border-t border-border">
        <SidebarButton
          item={{
            id: "settings",
            icon: <Settings className="h-4 w-4" />,
            label: "Settings",
            type: "action",
          }}
          collapsed={collapsed}
          active={activeView === "settings"}
          onClick={() => onViewChange("settings")}
        />
      </div>
    </aside>
  );
}

interface SidebarButtonProps {
  item: SidebarItem;
  collapsed: boolean;
  active: boolean;
  onClick: () => void;
  connected?: boolean;
}

function SidebarButton({
  item,
  collapsed,
  active,
  onClick,
  connected,
}: SidebarButtonProps) {
  return (
    <Button
      variant={active ? "secondary" : "ghost"}
      className={cn("w-full justify-start mb-1 relative px-2")}
      onClick={onClick}
    >
      <span className="relative">
        {item.icon}
        {connected !== undefined && (
          <span
            className={cn(
              "absolute -top-1 -right-1 h-2 w-2 rounded-full",
              connected ? "bg-green-500" : "bg-red-500",
            )}
          />
        )}
      </span>
      {!collapsed && (
        <>
          <span className="ml-2 truncate">{item.label}</span>
          {item.badge !== undefined && item.badge > 0 && (
            <Badge variant="secondary" className="ml-auto">
              {item.badge}
            </Badge>
          )}
        </>
      )}
    </Button>
  );
}

interface AgentButtonProps {
  agent: Agent;
  collapsed: boolean;
  selected: boolean;
  onClick: () => void;
}

function AgentButton({
  agent,
  collapsed,
  selected,
  onClick,
}: AgentButtonProps) {
  const isRunning = agent.state === "running";
  const displayName = getAgentDisplayName(agent);

  return (
    <Button
      variant={selected ? "secondary" : "ghost"}
      className={cn("w-full justify-start mb-1 relative px-2")}
      onClick={onClick}
      title={displayName}
    >
      <span className="relative">
        {createElement(getRoleIcon(agent.role), {
          className: cn(
            "h-4 w-4",
            isRunning ? "text-green-500" : "text-muted-foreground",
          ),
        })}
        {isRunning && (
          <span className="absolute -top-1 -right-1 h-2 w-2 rounded-full bg-green-500" />
        )}
      </span>
      {!collapsed && (
        <>
          <span className="ml-2 truncate capitalize">{displayName}</span>
        </>
      )}
    </Button>
  );
}
