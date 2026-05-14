import { useMemo, useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { cn } from '@/lib/utils'
import { Button } from '@/components/ui/button'
import { Badge } from '@/components/ui/badge'
import { ScrollArea } from '@/components/ui/scroll-area'
import { Tooltip } from '@/components/ui/tooltip'
import { useActivityStore } from '@/stores/activityStore'
import { useViewStore, type ViewType } from '@/stores/viewStore'
import { getContextOverview, listAgents, listWorkspaces } from '@/api/client'
import { getAgentDisplayName, isWorkerAgent } from '@/lib/agent-utils'
import {
  Activity,
  ChevronLeft,
  ChevronRight,
  Cpu,
  FileSearch,
  Hash,
  Layers,
  LayoutGrid,
  MessagesSquare,
  Plus,
  Workflow,
} from 'lucide-react'

interface SidebarItem {
  id: ViewType
  icon: React.ReactNode
  label: string
  badge?: number
  tooltip?: string
}

interface AgentSidebarProps {
  activeView: ViewType
  onViewChange: (view: ViewType) => void
}

const PRIMARY_SURFACES: SidebarItem[] = [
  {
    id: 'runtime',
    icon: <Cpu className="h-4 w-4" />,
    label: 'Runtime',
    tooltip: 'Watch agents, sessions, and live runtime state.',
  },
  {
    id: 'companion',
    icon: <MessagesSquare className="h-4 w-4" />,
    label: 'Companion',
    tooltip: 'Chat with agents and inspect conversation context.',
  },
  {
    id: 'events',
    icon: <Activity className="h-4 w-4" />,
    label: 'Events',
    tooltip: 'Inspect logs, errors, and event-level diagnostics.',
  },
  {
    id: 'rooms',
    icon: <Hash className="h-4 w-4" />,
    label: 'Rooms',
    tooltip: 'Coordinate work in shared rooms and message streams.',
  },
  {
    id: 'orchestration',
    icon: <LayoutGrid className="h-4 w-4" />,
    label: 'Orchestration',
    tooltip: 'Manage issue flow, boards, and coordinated execution.',
  },
]

const EVIDENCE_SURFACES: SidebarItem[] = [
  {
    id: 'turns',
    icon: <Workflow className="h-4 w-4" />,
    label: 'Turns',
    tooltip: 'Understand how agent turns were built and resolved.',
  },
  {
    id: 'context',
    icon: <Layers className="h-4 w-4" />,
    label: 'Memory',
    tooltip: 'Review project memory, suggested updates, and remembered sources.',
  },
  {
    id: 'artifacts',
    icon: <FileSearch className="h-4 w-4" />,
    label: 'Artifacts',
    tooltip: 'Inspect retrieved artifacts, embeddings, and search results.',
  },
]

export function AgentSidebar({ activeView, onViewChange }: AgentSidebarProps) {
  const [collapsed, setCollapsed] = useState(false)
  const activityEvents = useActivityStore((s) => s.events)
  const connected = useActivityStore((s) => s.connected)
  const {
    selectedAgentID,
    selectedAgent,
    selectedContextWorkspace,
    selectedConversationID,
    selectedRoomID,
    selectedRoomWorkspaceID,
    setSelectedAgent,
    setSpawnAgentOpen,
  } = useViewStore()

  const activityErrorCount = useMemo(
    () => activityEvents.filter((event) => event.status === 'error').length,
    [activityEvents],
  )
  const selectedWorkspaceRoot = selectedAgent?.workspace_root?.trim() ?? ''

  const { data: agentsData } = useQuery({
    queryKey: ['agents'],
    queryFn: () => listAgents(100),
    refetchInterval: 10000,
  })

  const { data: workspacesData } = useQuery({
    queryKey: ['workspaces'],
    queryFn: listWorkspaces,
    staleTime: 10000,
  })

  const agents = useMemo(() => agentsData?.agents ?? [], [agentsData?.agents])
  const conversationalAgents = useMemo(
    () => agents.filter((agent) => !isWorkerAgent(agent)),
    [agents],
  )
  const workerAgents = useMemo(
    () => agents.filter((agent) => isWorkerAgent(agent)),
    [agents],
  )
  const activeAgents = useMemo(
    () => agents.filter((agent) => agent.state === 'running' || agent.state === 'idle').length,
    [agents],
  )
  const erroredAgents = useMemo(
    () => agents.filter((agent) => agent.state === 'error').length,
    [agents],
  )
  const stoppedAgents = useMemo(
    () => agents.filter((agent) => agent.state === 'stopped').length,
    [agents],
  )

  const primaryItems = useMemo(
    () =>
      PRIMARY_SURFACES.map((item) =>
        item.id === 'events'
          ? {
              ...item,
              badge:
                activityErrorCount > 0 ? Math.min(activityErrorCount, 99) : undefined,
            }
          : item,
      ),
    [activityErrorCount],
  )

  const contextWorkspaceRoot =
    selectedContextWorkspace?.trim() ||
    selectedWorkspaceRoot ||
    workspacesData?.current?.trim() ||
    ''

  const { data: contextWikiOverview } = useQuery({
    queryKey: ['contextwiki-overview', contextWorkspaceRoot],
    queryFn: () =>
      getContextOverview({
        workspace: contextWorkspaceRoot,
      }),
    enabled: contextWorkspaceRoot.length > 0,
    staleTime: 5000,
    refetchInterval: 15000,
    refetchOnWindowFocus: false,
  })

  const preparedMergeBadge = useMemo(() => {
    const total =
      (contextWikiOverview?.stats.prepared_merge_count ?? 0) +
      (contextWikiOverview?.stats.claimed_merge_count ?? 0)
    return total > 0 ? Math.min(total, 99) : undefined
  }, [contextWikiOverview?.stats.claimed_merge_count, contextWikiOverview?.stats.prepared_merge_count])

  const focusContext = useMemo(() => {
    if (selectedAgent) {
      return {
        title: getAgentDisplayName(selectedAgent),
        subtitle: `${selectedAgent.role || 'agent'} · #${selectedAgent.id.slice(0, 8)}`,
        actionLabel: 'Open Runtime',
        onOpen: () => onViewChange('runtime'),
      }
    }
    if (selectedRoomID) {
      return {
        title: `room:${selectedRoomID}`,
        subtitle: selectedRoomWorkspaceID || 'unscoped room',
        actionLabel: 'Open Rooms',
        onOpen: () => onViewChange('rooms'),
      }
    }
    if (selectedConversationID) {
      return {
        title: `conversation:${selectedConversationID.slice(0, 8)}`,
        subtitle: 'companion handoff',
        actionLabel: 'Open Companion',
        onOpen: () => onViewChange('companion'),
      }
    }
    if (selectedAgentID) {
      return {
        title: `agent:${selectedAgentID.slice(0, 8)}`,
        subtitle: 'runtime selection',
        actionLabel: 'Open Runtime',
        onOpen: () => onViewChange('runtime'),
      }
    }
    return null
  }, [
    onViewChange,
    selectedAgent,
    selectedAgentID,
    selectedConversationID,
    selectedRoomID,
    selectedRoomWorkspaceID,
  ])

  const renderSection = (label: string, items: SidebarItem[]) => (
    <div className="px-2 mb-4">
      {!collapsed && (
        <span className="text-[10px] font-medium text-muted-foreground uppercase tracking-[0.18em] px-2">
          {label}
        </span>
      )}
      {items.map((item) => (
        <SidebarButton
          key={item.id}
          item={item}
          collapsed={collapsed}
          active={activeView === item.id}
          connected={item.id === 'events' ? connected : undefined}
          onClick={() => {
            if (item.id !== 'runtime') setSelectedAgent(null)
            onViewChange(item.id)
          }}
        />
      ))}
    </div>
  )

  return (
    <aside
      className={cn(
        'flex flex-col bg-card border-r border-border transition-all duration-200',
        collapsed ? 'w-14' : 'w-64',
      )}
    >
      <div className="h-14 flex items-center justify-between px-3 border-b border-border">
        {!collapsed && (
          <div className="min-w-0">
            <div className="text-[10px] uppercase tracking-[0.2em] text-muted-foreground">
              Control Plane
            </div>
            <div className="text-sm font-semibold text-foreground">gui-agent</div>
          </div>
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

      <ScrollArea className="flex-1 py-3">
        {!collapsed && (
          <div className="px-2 mb-4">
            <div className="rounded-lg border border-border bg-muted/20 p-3 space-y-3">
              <div className="space-y-1">
                <div className="text-[10px] uppercase tracking-[0.18em] text-muted-foreground">
                  Runtime Snapshot
                </div>
                <div className="text-xs text-muted-foreground">
                  Primary operator view for agent lifecycle, incidents, and handoffs.
                </div>
              </div>

              <div className="flex flex-wrap gap-1.5">
                <Badge variant="secondary" className="text-[10px]">
                  {agents.length} total
                </Badge>
                <Badge className="text-[10px] bg-green-500/10 text-green-500 border-green-500/20">
                  {activeAgents} active
                </Badge>
                {erroredAgents > 0 && (
                  <Badge className="text-[10px] bg-red-500/10 text-red-500 border-red-500/20">
                    {erroredAgents} errors
                  </Badge>
                )}
                <Badge variant="outline" className="text-[10px]">
                  {conversationalAgents.length} conversational
                </Badge>
                <Badge variant="outline" className="text-[10px]">
                  {workerAgents.length} workers
                </Badge>
                {stoppedAgents > 0 && (
                  <Badge variant="outline" className="text-[10px]">
                    {stoppedAgents} stopped
                  </Badge>
                )}
              </div>

              <div className="grid grid-cols-2 gap-2">
                <Button
                  variant={activeView === 'runtime' ? 'secondary' : 'outline'}
                  size="sm"
                  className="h-8 text-xs"
                  onClick={() => {
                    setSelectedAgent(null)
                    onViewChange('runtime')
                  }}
                >
                  Open Runtime
                </Button>
                <Button
                  variant="outline"
                  size="sm"
                  className="h-8 text-xs"
                  onClick={() => {
                    setSelectedAgent(null)
                    setSpawnAgentOpen(true)
                  }}
                >
                  <Plus className="mr-1 h-3.5 w-3.5" />
                  Spawn Agent
                </Button>
              </div>

              {activityErrorCount > 0 && (
                <Button
                  variant="outline"
                  size="sm"
                  className="w-full h-8 text-xs justify-start"
                  onClick={() => onViewChange('events')}
                >
                  <Activity className="mr-1.5 h-3.5 w-3.5 text-red-400" />
                  Review {activityErrorCount} active event errors
                </Button>
              )}
            </div>
          </div>
        )}

        {renderSection('Primary', primaryItems)}
        {renderSection(
          'Evidence',
          EVIDENCE_SURFACES.map((item) =>
            item.id === 'context'
              ? {
                  ...item,
                  badge: preparedMergeBadge,
                }
              : item,
          ),
        )}

        {!collapsed && focusContext && (
          <div className="px-2">
            <div className="rounded-lg border border-border bg-background/40 p-3 space-y-2">
              <div className="text-[10px] uppercase tracking-[0.18em] text-muted-foreground">
                Current Focus
              </div>
              <div className="text-sm font-medium text-foreground truncate">
                {focusContext.title}
              </div>
              <div className="text-xs text-muted-foreground truncate">
                {focusContext.subtitle}
              </div>
              <Button
                variant="outline"
                size="sm"
                className="h-8 w-full text-xs"
                onClick={focusContext.onOpen}
              >
                {focusContext.actionLabel}
              </Button>
            </div>
          </div>
        )}
      </ScrollArea>
    </aside>
  )
}

interface SidebarButtonProps {
  item: SidebarItem
  collapsed: boolean
  active: boolean
  onClick: () => void
  connected?: boolean
}

function SidebarButton({
  item,
  collapsed,
  active,
  onClick,
  connected,
}: SidebarButtonProps) {
  return (
    <Tooltip
      content={item.tooltip || item.label}
      side={collapsed ? 'right' : 'top'}
      className="w-full"
      bubbleClassName={collapsed ? 'w-56' : 'w-64'}
      disabled={!item.tooltip}
    >
      <Button
        variant={active ? 'secondary' : 'ghost'}
        className={cn('w-full justify-start mb-1 relative px-2', collapsed && 'justify-center')}
        onClick={onClick}
      >
        <span className="relative">
          {item.icon}
          {connected !== undefined && (
            <span
              className={cn(
                'absolute -top-1 -right-1 h-2 w-2 rounded-full',
                connected ? 'bg-green-500' : 'bg-red-500',
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
    </Tooltip>
  )
}
