import { type ReactNode, useMemo } from 'react'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { HelpTooltip, Tooltip } from '@/components/ui/tooltip'
import { AgentSidebar } from './AgentSidebar'
import { SpawnAgentPanel } from './SpawnAgentPanel'
import { useActivityStream } from '@/hooks/useActivityStream'
import { useActivityStore } from '@/stores/activityStore'
import { useViewStore } from '@/stores/viewStore'
import { getAgentDisplayName } from '@/lib/agent-utils'
import type { AuthSessionResponse } from '@/api/client'
import type { ViewType } from '@/stores/viewStore'

interface AppShellProps {
  children: ReactNode
  activeView: ViewType
  onViewChange: (view: ViewType) => void
  authSession: AuthSessionResponse
  onSignOut: () => void
  signingOut?: boolean
}

export function AppShell({
  children,
  activeView,
  onViewChange,
  authSession,
  onSignOut,
  signingOut = false,
}: AppShellProps) {
  const {
    spawnAgentOpen,
    setSpawnAgentOpen,
    selectedAgentID,
    selectedAgent,
    selectedRoomID,
    selectedRoomWorkspaceID,
    selectedConversationID,
  } = useViewStore()
  const connected = useActivityStore((s) => s.connected)
  const activityErrorCount = useActivityStore(
    (s) => s.events.filter((event) => event.status === 'error').length,
  )
  const titles: Record<ViewType, string> = {
    runtime: 'Runtime',
    rooms: 'Rooms',
    orchestration: 'Orchestration',
    turns: 'Turns',
    context: 'Memory',
    artifacts: 'Artifacts',
    events: 'Events',
    companion: 'Companion',
  }
  const descriptions: Record<ViewType, string> = {
    runtime: 'Live agent operations, lifecycle state, and current work.',
    rooms: 'Shared discussion spaces for coordinated multi-agent work.',
    orchestration: 'Planning boards and coordinated issue execution.',
    turns: 'How agent turns were assembled, tooled, and completed.',
    context: 'Project memory, remembered sources, and suggested updates.',
    artifacts: 'Retrieved files, embeddings, and artifact search behavior.',
    events: 'Operational logs and diagnostics across the control plane.',
    companion: 'Conversation view for chatting with agents and reviewing context.',
  }
  const surfaceTier = useMemo(
    () =>
      activeView === 'turns' ||
      activeView === 'context' ||
      activeView === 'artifacts'
        ? 'Evidence'
        : 'Primary',
    [activeView],
  )
  const focusLabel = useMemo(() => {
    if (selectedAgent) {
      return {
        title: getAgentDisplayName(selectedAgent),
        subtitle: `${selectedAgent.role || 'agent'} · #${selectedAgent.id.slice(0, 8)}`,
      }
    }
    if (selectedRoomID) {
      return {
        title: `room:${selectedRoomID}`,
        subtitle: selectedRoomWorkspaceID || 'unscoped room',
      }
    }
    if (selectedConversationID) {
      return {
        title: `conversation:${selectedConversationID.slice(0, 8)}`,
        subtitle: 'companion handoff',
      }
    }
    if (selectedAgentID) {
      return {
        title: `agent:${selectedAgentID.slice(0, 8)}`,
        subtitle: 'runtime selection',
      }
    }
    return null
  }, [
    selectedAgent,
    selectedAgentID,
    selectedConversationID,
    selectedRoomID,
    selectedRoomWorkspaceID,
  ])

  // Initialize SSE connection
  useActivityStream()

  return (
    <div className="flex h-screen w-screen bg-background dark">
      {/* Left Sidebar - Agent Panel */}
      <AgentSidebar activeView={activeView} onViewChange={onViewChange} />

      {/* Main Content */}
      <div className="flex-1 flex flex-col overflow-hidden">
        {/* Top Bar */}
        <header className="h-14 border-b border-border flex items-center justify-between px-4 gap-3">
          <div className="min-w-0">
            <div className="flex items-center gap-2">
              <span className="text-[10px] uppercase tracking-[0.18em] text-muted-foreground">
                Control Plane
              </span>
              <Tooltip content="Surface group. Primary covers live operations, while Evidence covers what agents saw, used, and remembered.">
                <Badge variant="outline" className="text-[10px]">
                  {surfaceTier}
                </Badge>
              </Tooltip>
              <Tooltip
                content={
                  connected
                    ? 'The GUI is receiving the live activity stream.'
                    : 'The live activity stream is disconnected. Some views may be stale.'
                }
              >
                <Badge
                  variant="outline"
                  className="text-[10px] gap-1 inline-flex items-center"
                >
                  <span
                    className={`h-1.5 w-1.5 rounded-full ${connected ? 'bg-green-500' : 'bg-red-500'}`}
                  />
                  activity
                </Badge>
              </Tooltip>
            </div>
            <div className="flex items-center gap-1.5">
              <h1 className="text-sm font-semibold text-foreground">
                {titles[activeView]}
              </h1>
              <HelpTooltip content={descriptions[activeView]} side="bottom" />
            </div>
          </div>
          <div className="flex items-center gap-2 min-w-0">
            {activityErrorCount > 0 && (
              <Tooltip content="The number of error events currently visible in the live event stream.">
                <Badge className="text-[10px] bg-red-500/10 text-red-500 border-red-500/20">
                  {activityErrorCount} errors
                </Badge>
              </Tooltip>
            )}
            {focusLabel && (
              <div className="hidden md:flex flex-col min-w-0 text-right">
                <span className="text-xs font-medium text-foreground truncate">
                  {focusLabel.title}
                </span>
                <span className="text-[11px] text-muted-foreground truncate">
                  {focusLabel.subtitle}
                </span>
              </div>
            )}
            <div className="hidden lg:flex flex-col min-w-0 text-right border-l border-border pl-3">
              <span className="text-xs font-medium text-foreground truncate">
                {authSession.user.name || authSession.user.email}
              </span>
              <span className="text-[11px] text-muted-foreground truncate">
                {authSession.user.email}
              </span>
            </div>
            <Tooltip content="Current GUI session state. Local development uses a fallback session when the public auth gateway is absent.">
              <Badge variant="outline" className="text-[10px] hidden sm:inline-flex">
                Signed in
              </Badge>
            </Tooltip>
            <Tooltip content="End the current GUI session.">
              <Button
                variant="ghost"
                size="sm"
                onClick={onSignOut}
                disabled={signingOut}
              >
                {signingOut ? 'Signing out…' : 'Sign out'}
              </Button>
            </Tooltip>
          </div>
        </header>

        {/* Content Area */}
        <div className="flex-1 flex overflow-hidden">
          {/* Main Panel */}
          <main className="flex-1 overflow-hidden">
            {children}
          </main>

          {/* Global Spawn Agent Panel */}
          {spawnAgentOpen && (
            <SpawnAgentPanel
              onClose={() => setSpawnAgentOpen(false)}
              onViewChange={onViewChange}
            />
          )}
        </div>
      </div>
    </div>
  )
}
