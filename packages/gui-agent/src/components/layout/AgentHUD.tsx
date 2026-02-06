import { useEffect } from 'react'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { cn, formatRelativeTime } from '@/lib/utils'
import { Button } from '@/components/ui/button'
import { Badge } from '@/components/ui/badge'
import { ScrollArea } from '@/components/ui/scroll-area'
import { Card, CardContent } from '@/components/ui/card'
import { useViewStore } from '@/stores/viewStore'
import {
  Bot,
  Play,
  Square,
  Trash2,
  Clock,
  Cpu,
  Folder,
  MessageCircle,
  Activity,
  Zap,
  Users,
} from 'lucide-react'
import {
  listAgents,
  startAgent,
  killAgent,
  trashAgent,
  listAgentSessions,
} from '@/api/client'
import type { AgentSession } from '@/api/types'

export function AgentHUD() {
  const selectedAgent = useViewStore((s) => s.selectedAgent)
  const setSelectedAgent = useViewStore((s) => s.setSelectedAgent)
  const queryClient = useQueryClient()

  // Fetch fresh agent list to reconcile stale selectedAgent
  const { data: agentsData } = useQuery({
    queryKey: ['agents'],
    queryFn: () => listAgents(100),
    refetchInterval: 10000,
  })

  // Reconcile selected agent with fresh data from the agents query
  useEffect(() => {
    if (!selectedAgent || !agentsData?.agents) return
    const fresh = agentsData.agents.find((a) => a.id === selectedAgent.id)
    if (fresh && (fresh.state !== selectedAgent.state || fresh.updated_at !== selectedAgent.updated_at)) {
      setSelectedAgent(fresh)
    }
  }, [agentsData, selectedAgent, setSelectedAgent])

  // Fetch sessions for selected agent
  const { data: sessionsData } = useQuery({
    queryKey: ['agent-sessions', selectedAgent?.id],
    queryFn: () => listAgentSessions(selectedAgent!.id),
    enabled: !!selectedAgent,
    refetchInterval: 5000,
  })

  // Start agent mutation
  const startMutation = useMutation({
    mutationFn: (agentId: string) => startAgent(agentId, {}),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['agents'] })
      queryClient.invalidateQueries({ queryKey: ['agent-sessions', selectedAgent?.id] })
    },
  })

  // Kill agent mutation
  const killMutation = useMutation({
    mutationFn: (agentId: string) => killAgent(agentId),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['agents'] })
      queryClient.invalidateQueries({ queryKey: ['agent-sessions', selectedAgent?.id] })
    },
  })

  // Trash agent mutation
  const trashMutation = useMutation({
    mutationFn: (agentId: string) => trashAgent(agentId),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['agents'] })
      setSelectedAgent(null)
    },
  })

  if (!selectedAgent) {
    return (
      <div className="flex flex-col h-full items-center justify-center p-6 text-center">
        <Users className="h-12 w-12 text-muted-foreground/30 mb-4" />
        <h3 className="text-sm font-medium text-foreground mb-1">No Agent Selected</h3>
        <p className="text-xs text-muted-foreground">
          Click on an agent in the sidebar to view details
        </p>
      </div>
    )
  }

  const isRunning = selectedAgent.state === 'running'
  const sessions = sessionsData?.sessions ?? []

  return (
    <div className="flex flex-col h-full">
      {/* Header with Agent Info */}
      <div className="p-4 border-b border-border">
        <div className="flex items-center gap-3 mb-3">
          <div className={cn(
            'h-10 w-10 rounded-lg flex items-center justify-center',
            isRunning ? 'bg-green-500/10' : 'bg-muted'
          )}>
            <Bot className={cn(
              'h-5 w-5',
              isRunning ? 'text-green-500' : 'text-muted-foreground'
            )} />
          </div>
          <div className="flex-1 min-w-0">
            <div className="flex items-center gap-2">
              <h2 className="text-sm font-semibold text-foreground truncate">
                {selectedAgent.name || selectedAgent.slug || selectedAgent.role || 'Agent'}
              </h2>
              <Badge variant={isRunning ? 'default' : 'outline'} className="text-xs">
                {selectedAgent.state}
              </Badge>
            </div>
            <p className="text-xs text-muted-foreground font-mono truncate">
              {selectedAgent.id.slice(0, 16)}...
            </p>
          </div>
        </div>

        {/* Action Buttons */}
        <div className="flex gap-2">
          {isRunning ? (
            <Button
              variant="destructive"
              size="sm"
              className="flex-1 gap-1"
              onClick={() => {
                if (window.confirm(`Are you sure you want to stop "${selectedAgent.name || selectedAgent.role || 'this agent'}"?`)) {
                  killMutation.mutate(selectedAgent.id)
                }
              }}
              disabled={killMutation.isPending}
            >
              <Square className="h-3 w-3" />
              {killMutation.isPending ? 'Stopping...' : 'Stop'}
            </Button>
          ) : (
            <Button
              variant="default"
              size="sm"
              className="flex-1 gap-1"
              onClick={() => startMutation.mutate(selectedAgent.id)}
              disabled={startMutation.isPending}
            >
              <Play className="h-3 w-3" />
              {startMutation.isPending ? 'Starting...' : 'Start'}
            </Button>
          )}
          <Button
            variant="outline"
            size="sm"
            className="gap-1"
            onClick={() => {
              if (window.confirm(`Are you sure you want to remove "${selectedAgent.name || selectedAgent.role || 'this agent'}"? This action cannot be undone.`)) {
                trashMutation.mutate(selectedAgent.id)
              }
            }}
            disabled={trashMutation.isPending || isRunning}
            title={isRunning ? 'Stop agent before trashing' : 'Delete agent'}
          >
            <Trash2 className="h-3 w-3" />
          </Button>
        </div>
      </div>

      {/* Agent Details */}
      <ScrollArea className="flex-1 p-4">
        <div className="space-y-4">
          {/* Quick Stats */}
          <div className="grid grid-cols-2 gap-2">
            <StatCard
              icon={<Cpu className="h-4 w-4" />}
              label="Model"
              value={selectedAgent.llm_model || 'default'}
            />
            <StatCard
              icon={<Zap className="h-4 w-4" />}
              label="Role"
              value={selectedAgent.role || 'agent'}
            />
            <StatCard
              icon={<Activity className="h-4 w-4" />}
              label="Sessions"
              value={sessions.length.toString()}
            />
            <StatCard
              icon={<Clock className="h-4 w-4" />}
              label="Created"
              value={selectedAgent.created_at ? formatRelativeTime(selectedAgent.created_at) : '-'}
            />
          </div>

          {/* Workspace */}
          {selectedAgent.ns && (
            <div className="p-3 rounded-lg bg-muted/50">
              <div className="flex items-center gap-2 text-xs text-muted-foreground mb-1">
                <Folder className="h-3 w-3" />
                Workspace
              </div>
              <p className="text-sm text-foreground truncate">{selectedAgent.ns}</p>
            </div>
          )}

          {/* Linked Conversation */}
          {selectedAgent.conversation_id && (
            <div className="p-3 rounded-lg bg-primary/5 border border-primary/20">
              <div className="flex items-center gap-2 text-xs text-primary mb-1">
                <MessageCircle className="h-3 w-3" />
                Linked Conversation
              </div>
              <p className="text-sm text-foreground truncate font-mono">
                {selectedAgent.conversation_id.slice(0, 20)}...
              </p>
            </div>
          )}

          {/* Role & Prompt */}
          {selectedAgent.role && (
            <div className="p-3 rounded-lg bg-muted/50">
              <div className="flex items-center gap-2 text-xs text-muted-foreground mb-1">
                <Bot className="h-3 w-3" />
                Role
              </div>
              <Badge variant="secondary" className="capitalize">
                {selectedAgent.role}
              </Badge>
            </div>
          )}



          {/* Active Sessions */}
          {sessions.length > 0 && (
            <div>
              <h4 className="text-xs font-medium text-muted-foreground uppercase tracking-wider mb-2">
                Active Sessions
              </h4>
              <div className="space-y-2">
                {sessions.slice(0, 5).map((session) => (
                  <SessionCard key={session.session_id} session={session} />
                ))}
              </div>
            </div>
          )}
        </div>
      </ScrollArea>
    </div>
  )
}

function StatCard({ icon, label, value }: { icon: React.ReactNode; label: string; value: string }) {
  return (
    <Card className="bg-muted/30">
      <CardContent className="p-2">
        <div className="flex items-center gap-2 text-muted-foreground mb-0.5">
          {icon}
          <span className="text-xs">{label}</span>
        </div>
        <p className="text-sm font-medium text-foreground truncate">{value}</p>
      </CardContent>
    </Card>
  )
}

function SessionCard({ session }: { session: AgentSession }) {
  return (
    <Card className="bg-muted/30 cursor-pointer hover:bg-muted/50 transition-colors">
      <CardContent className="p-2">
        <div className="flex items-center justify-between mb-1">
          <span className="text-xs font-mono text-muted-foreground">
            {session.session_id.slice(0, 12)}...
          </span>
          <Badge variant={session.status === 'running' ? 'default' : 'outline'} className="text-xs">
            {session.status}
          </Badge>
        </div>
        <div className="flex items-center gap-3 text-xs text-muted-foreground">
          <span>Iterations: {session.iterations || 0}</span>
          <span>{session.role}</span>
        </div>
      </CardContent>
    </Card>
  )
}
