import { useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { cn } from '@/lib/utils'
import { Button } from '@/components/ui/button'
import { Badge } from '@/components/ui/badge'
import { ScrollArea } from '@/components/ui/scroll-area'
import { useActivityStore } from '@/stores/activityStore'
import { useViewStore } from '@/stores/viewStore'
import { listAgents } from '@/api/client'
import type { Agent } from '@/api/types'
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
  Bot,
} from 'lucide-react'
import type { ViewType } from '@/stores/viewStore'

interface SidebarItem {
  id: ViewType
  icon: React.ReactNode
  label: string
  badge?: number
  active?: boolean
  type: 'agent' | 'action'
}

interface AgentSidebarProps {
  activeView: ViewType
  onViewChange: (view: ViewType) => void
}

export function AgentSidebar({ activeView, onViewChange }: AgentSidebarProps) {
  const [collapsed, setCollapsed] = useState(false)
  const activityCount = useActivityStore((s) => s.events.length)
  const connected = useActivityStore((s) => s.connected)
  const { selectedAgent, setSelectedAgent, setSpawnAgentOpen } = useViewStore()

  const { data: agentsData } = useQuery({
    queryKey: ['agents'],
    queryFn: () => listAgents(100),
    refetchInterval: 10000,
  })

  const agents = agentsData?.agents ?? []

  const quickActions: SidebarItem[] = [
    { id: 'search', icon: <Search className="h-4 w-4" />, label: 'Search', type: 'action' },
    { id: 'logs', icon: <FileText className="h-4 w-4" />, label: 'Logs', type: 'action' },
    { id: 'skills', icon: <Zap className="h-4 w-4" />, label: 'Run Skill', type: 'action' },
  ]

  const handleAgentClick = (agent: Agent) => {
    setSelectedAgent(agent)
    onViewChange('agents')
  }

  return (
    <aside
      className={cn(
        'flex flex-col bg-card border-r border-border transition-all duration-200',
        collapsed ? 'w-14' : 'w-56'
      )}
    >
      <div className="h-12 flex items-center justify-between px-3 border-b border-border">
        {!collapsed && <span className="text-sm font-semibold text-foreground">Agents</span>}
        <Button variant="ghost" size="icon" className="h-7 w-7" onClick={() => setCollapsed(!collapsed)}>
          {collapsed ? <ChevronRight className="h-4 w-4" /> : <ChevronLeft className="h-4 w-4" />}
        </Button>
      </div>

      <ScrollArea className="flex-1 py-2">
        <div className="px-2 mb-4">
          {!collapsed && <span className="text-xs font-medium text-muted-foreground uppercase tracking-wider px-2">Chat</span>}
          <SidebarButton
            item={{ id: 'conversations', icon: <MessagesSquare className="h-4 w-4" />, label: 'Conversations', type: 'agent' }}
            collapsed={collapsed}
            active={activeView === 'conversations'}
            onClick={() => { setSelectedAgent(null); onViewChange('conversations') }}
          />
        </div>

        <div className="px-2 mb-4">
          {!collapsed && (
            <div className="flex items-center justify-between mb-2 px-2">
              <span className="text-xs font-medium text-muted-foreground uppercase tracking-wider">Agents</span>
              <Button
                variant="ghost"
                size="icon"
                className="h-5 w-5"
                title="Spawn new agent"
                onClick={() => {
                  setSelectedAgent(null)
                  setSpawnAgentOpen(true)
                }}
              >
                <Plus className="h-3 w-3" />
              </Button>
            </div>
          )}
          {agents.length === 0 ? (
            !collapsed && <div className="px-2 py-2 text-xs text-muted-foreground">No agents yet</div>
          ) : (
            agents.map((agent) => (
              <AgentButton key={agent.id} agent={agent} collapsed={collapsed} selected={selectedAgent?.id === agent.id} onClick={() => handleAgentClick(agent)} />
            ))
          )}
        </div>

        <div className="px-2 mb-4">
          {!collapsed && <span className="text-xs font-medium text-muted-foreground uppercase tracking-wider px-2">Activity</span>}
          <SidebarButton
            item={{ id: 'activity', icon: <Activity className="h-4 w-4" />, label: 'Activity', badge: activityCount > 0 ? Math.min(activityCount, 99) : undefined, type: 'action' }}
            collapsed={collapsed}
            active={activeView === 'activity'}
            onClick={() => onViewChange('activity')}
            connected={connected}
          />
        </div>

        <div className="px-2">
          {!collapsed && <span className="text-xs font-medium text-muted-foreground uppercase tracking-wider px-2">Quick Actions</span>}
          {quickActions.map((item) => (
            <SidebarButton key={item.id} item={item} collapsed={collapsed} active={activeView === item.id} onClick={() => onViewChange(item.id)} />
          ))}
        </div>
      </ScrollArea>

      <div className="p-2 border-t border-border">
        <SidebarButton item={{ id: 'settings', icon: <Settings className="h-4 w-4" />, label: 'Settings', type: 'action' }} collapsed={collapsed} active={activeView === 'settings'} onClick={() => onViewChange('settings')} />
      </div>
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

function SidebarButton({ item, collapsed, active, onClick, connected }: SidebarButtonProps) {
  return (
    <Button variant={active ? 'secondary' : 'ghost'} className={cn('w-full justify-start mb-1 relative px-2')} onClick={onClick}>
      <span className="relative">
        {item.icon}
        {connected !== undefined && <span className={cn('absolute -top-1 -right-1 h-2 w-2 rounded-full', connected ? 'bg-green-500' : 'bg-red-500')} />}
      </span>
      {!collapsed && (
        <>
          <span className="ml-2 truncate">{item.label}</span>
          {item.badge !== undefined && item.badge > 0 && <Badge variant="secondary" className="ml-auto">{item.badge}</Badge>}
        </>
      )}
    </Button>
  )
}

interface AgentButtonProps {
  agent: Agent
  collapsed: boolean
  selected: boolean
  onClick: () => void
}

function AgentButton({ agent, collapsed, selected, onClick }: AgentButtonProps) {
  const isRunning = agent.state === 'running'
  const displayName = agent.name || agent.slug || agent.role || 'Agent'

  return (
    <Button variant={selected ? 'secondary' : 'ghost'} className={cn('w-full justify-start mb-1 relative px-2')} onClick={onClick} title={displayName}>
      <span className="relative">
        <Bot className={cn('h-4 w-4', isRunning ? 'text-green-500' : 'text-muted-foreground')} />
        {isRunning && <span className="absolute -top-1 -right-1 h-2 w-2 rounded-full bg-green-500" />}
      </span>
      {!collapsed && (
        <>
          <span className="ml-2 truncate">{displayName}</span>
          {agent.role && <Badge variant="outline" className="ml-auto text-xs">{agent.role.slice(0, 3)}</Badge>}
        </>
      )}
    </Button>
  )
}
