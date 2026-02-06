import { useState } from 'react'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { ScrollArea } from '@/components/ui/scroll-area'
import { Button } from '@/components/ui/button'
import { Badge } from '@/components/ui/badge'
import { Card, CardContent } from '@/components/ui/card'
import { Input } from '@/components/ui/input'
import { cn, formatRelativeTime } from '@/lib/utils'
import { listAgents, spawnAgent, trashAgent, killAgent, startAgent, createConsoleSession, getCompanionConversationMessages, type SpawnAgentParams } from '@/api/client'
import type { Agent, AgentSpawnResponse } from '@/api/types'
import { useChatStore } from '@/stores/chatStore'
import { useViewStore } from '@/stores/viewStore'
import type { ConsoleMessage } from '@/api/client'
import { AgentDetailView } from './AgentDetailView'
import { SpawnAgentFormCore } from './SpawnAgentFormCore'
import {
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
  Eye,
  MessageSquare,
  Trash2,
} from 'lucide-react'

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
  const [searchQuery, setSearchQuery] = useState('')
  const [chatLoadingAgentId, setChatLoadingAgentId] = useState<string | null>(null)
  const [trashLoadingAgentId, setTrashLoadingAgentId] = useState<string | null>(null)
  const [killLoadingAgentId, setKillLoadingAgentId] = useState<string | null>(null)
  const [startLoadingAgentId, setStartLoadingAgentId] = useState<string | null>(null)
  const queryClient = useQueryClient()

  // Access chat store to switch sessions
  const { setSessionId, setSession, setMessages, setInflight, setPersistedSessionId, setInitializing, setSourceAgent } = useChatStore()
  const { selectedAgent, setSelectedAgent, setActiveView, spawnAgentOpen, setSpawnAgentOpen } = useViewStore()

  const { data, isLoading, refetch, isFetching } = useQuery({
    queryKey: ['agents'],
    queryFn: () => listAgents(50),
    refetchInterval: 10000,
  })

  const agents = data?.agents ?? []
  const filteredAgents = searchQuery
    ? agents.filter(
        (a) =>
          a.name?.toLowerCase().includes(searchQuery.toLowerCase()) ||
          a.slug?.toLowerCase().includes(searchQuery.toLowerCase()) ||
          a.role?.toLowerCase().includes(searchQuery.toLowerCase()) ||
          a.id.toLowerCase().includes(searchQuery.toLowerCase())
      )
    : agents

  // Handle loading an agent into the companion chat
  const handleChat = async (agent: Agent) => {
    setChatLoadingAgentId(agent.id)
    setInitializing(true)

    try {
      // Try to load companion conversation messages for this agent
      // Use linked conversation_id if available, otherwise fall back to agent.id
      const conversationId = agent.conversation_id || agent.id
      let companionMessages: ConsoleMessage[] = []
      let hasHistory = false

      try {
        const messagesData = await getCompanionConversationMessages(conversationId, 200)
        if (messagesData.messages && messagesData.messages.length > 0) {
          hasHistory = true
          companionMessages = messagesData.messages.map((msg) => ({
            role: msg.role as 'user' | 'assistant',
            content: msg.content,
            timestamp: msg.created_at,
            // Map tool calls from companion format to console format
            tool_calls: msg.tool_calls?.map((tc) => ({
              name: tc.name,
              input: tc.arguments as Record<string, unknown>,
              status: 'completed' as const,
            })),
          }))
        }
      } catch {
        // No companion conversation found for agent, starting fresh
      }

      // Create a new console session for this agent's workspace
      const data = await createConsoleSession({
        workspace: agent.ns || '/',
        profile: 'companion',
        system_prompt: `You are chatting in the context of an agent session.

Agent Details:
- Name: ${agent.name || agent.slug || 'Unnamed'}
- Role: ${agent.role || 'N/A'}
- ID: ${agent.id}
- Workspace: ${agent.ns || '/'}
- Model: ${agent.llm_model || 'default'}
- State: ${agent.state}
${hasHistory ? `- Continuing conversation with ${companionMessages.length} previous messages` : ''}

Help the user understand and interact with this agent's work.`,
      })

      const newSessionId = data.session.id
      setSessionId(newSessionId)
      setSession(data.session)
      setSourceAgent({
        id: agent.id,
        name: agent.name,
        role: agent.role,
        ns: agent.ns,
      })

      // Set messages from companion conversation
      if (companionMessages.length > 0) {
        setMessages(companionMessages)
        setPersistedSessionId(conversationId) // Use conversation_id for persisted session reference
      } else {
        setMessages([])
        setPersistedSessionId(null)
      }

      setInflight(false)

      // Switch to conversations view to show the new chat
      setActiveView('conversations')

      // Set localStorage AFTER switching view and loading messages
      localStorage.setItem('gui-agent-session-id', newSessionId)

      // Small delay to ensure ConversationsList has mounted before clearing the flag
      setTimeout(() => {
        setInitializing(false)
      }, 500)
    } catch (err) {
      console.error('Failed to create chat session for agent:', err)
      setInitializing(false)
    } finally {
      setChatLoadingAgentId(null)
    }
  }

  // Handle trashing a stopped agent
  const handleTrash = async (agent: Agent) => {
    if (agent.state !== 'stopped') {
      console.error('Can only trash stopped agents')
      return
    }

    // Confirm before trashing
    if (!window.confirm(`Are you sure you want to remove "${agent.name || agent.role || 'this agent'}"? This action cannot be undone.`)) {
      return
    }

    setTrashLoadingAgentId(agent.id)
    try {
      await trashAgent(agent.id)
      // Refresh the agent list
      queryClient.invalidateQueries({ queryKey: ['agents'] })
    } catch (err) {
      console.error('Failed to trash agent:', err)
      alert(err instanceof Error ? err.message : 'Failed to trash agent')
    } finally {
      setTrashLoadingAgentId(null)
    }
  }

  // Handle killing a running agent
  const handleKill = async (agent: Agent) => {
    if (agent.state !== 'running') {
      console.error('Can only kill running agents')
      return
    }

    if (!window.confirm(`Are you sure you want to stop "${agent.name || agent.role || 'this agent'}"?`)) {
      return
    }

    setKillLoadingAgentId(agent.id)
    try {
      await killAgent(agent.id)
      // Refresh the agent list
      queryClient.invalidateQueries({ queryKey: ['agents'] })
    } catch (err) {
      console.error('Failed to kill agent:', err)
      alert(err instanceof Error ? err.message : 'Failed to stop agent')
    } finally {
      setKillLoadingAgentId(null)
    }
  }

  // Handle starting/resuming a stopped agent
  const handleStart = async (agent: Agent) => {
    if (agent.state === 'running') {
      console.error('Agent is already running')
      return
    }

    setStartLoadingAgentId(agent.id)
    try {
      await startAgent(agent.id)
      // Refresh the agent list
      queryClient.invalidateQueries({ queryKey: ['agents'] })
    } catch (err) {
      console.error('Failed to start agent:', err)
      alert(err instanceof Error ? err.message : 'Failed to start agent')
    } finally {
      setStartLoadingAgentId(null)
    }
  }

  // If an agent is selected, show detail view
  if (selectedAgent) {
    return (
      <AgentDetailView
        agent={selectedAgent}
        onBack={() => setSelectedAgent(null)}
      />
    )
  }

  // Count agents by state
  const agentCounts = agents.reduce(
    (acc, agent) => {
      acc[agent.state] = (acc[agent.state] || 0) + 1
      return acc
    },
    {} as Record<string, number>
  )

  return (
    <div className="flex flex-col h-full">
      {/* Header */}
      <div className="p-4 border-b border-border space-y-3">
        <div className="flex items-center justify-between">
          <div className="flex items-center gap-2">
            <Users className="h-5 w-5" />
            <h2 className="text-lg font-semibold text-foreground">All Agents</h2>
          </div>
          <div className="flex items-center gap-2">
            <Button
              variant="ghost"
              size="icon"
              onClick={() => refetch()}
              disabled={isFetching}
              className="h-8 w-8"
            >
              <RefreshCw
                className={cn('h-4 w-4', isFetching && 'animate-spin')}
              />
            </Button>
            <Button size="sm" onClick={() => setSpawnAgentOpen(!spawnAgentOpen)}>
              <Plus className="h-4 w-4 mr-1" />
              Spawn
            </Button>
          </div>
        </div>

        {/* Agent state summary */}
        {agents.length > 0 && (
          <div className="flex items-center gap-2 flex-wrap">
            <Badge variant="secondary" className="text-xs">
              {agents.length} total
            </Badge>
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
                {agentCounts.error} error
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
      </div>

      {/* Spawn Form */}
      {spawnAgentOpen && (
        <SpawnAgentForm
          onClose={() => setSpawnAgentOpen(false)}
          onSuccess={async (actorId: string, spawnData: AgentSpawnResponse) => {
            setSpawnAgentOpen(false)

            // Refresh the agent list
            queryClient.invalidateQueries({ queryKey: ['agents'] })

            // Extract info from actor_id (format: "actor:role:UUID")
            const parts = actorId.split(':')
            const agentId = parts.pop() || actorId
            const role = parts.length > 1 ? parts[1] : 'agent'

            // Construct a minimal agent from spawn response to open chat immediately
            const newAgent: Agent = {
              id: agentId,
              name: spawnData.name || 'New Agent',
              slug: spawnData.name?.toLowerCase().replace(/\s+/g, '-'),
              role: role,
              skills_allow: [],
              share_bb: 'none',
              state: (spawnData.status || 'running') as Agent['state'],
              ns: '/',
              created_at: new Date().toISOString(),
              updated_at: new Date().toISOString(),
            }

            handleChat(newAgent)
          }}
        />
      )}

      {/* Agent List */}
      <ScrollArea className="flex-1">
        <div className="p-4 space-y-3">
          {isLoading ? (
            <div className="text-center py-12 text-muted-foreground">
              <RefreshCw className="h-8 w-8 mx-auto mb-2 animate-spin" />
              <p>Loading agents...</p>
            </div>
          ) : filteredAgents.length === 0 ? (
            <div className="text-center py-12 text-muted-foreground">
              <div className="h-16 w-16 mx-auto mb-4 rounded-xl bg-muted flex items-center justify-center">
                <Bot className="h-8 w-8 opacity-40" />
              </div>
              <p className="text-lg font-medium text-foreground">
                {searchQuery ? 'No matching agents' : 'No agents running'}
              </p>
              <p className="text-sm mt-1 max-w-xs mx-auto">
                {searchQuery
                  ? `No agents match "${searchQuery}". Try a different search.`
                  : 'Spawn autonomous agents to perform tasks like research, coding, or reviewing.'}
              </p>
              {!searchQuery && (
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
            filteredAgents.map((agent) => (
              <AgentCard
                key={agent.id}
                agent={agent}
                onViewDetails={setSelectedAgent}
                onChat={handleChat}
                onTrash={handleTrash}
                onKill={handleKill}
                onStart={handleStart}
                isChatLoading={chatLoadingAgentId === agent.id}
                isTrashLoading={trashLoadingAgentId === agent.id}
                isKillLoading={killLoadingAgentId === agent.id}
                isStartLoading={startLoadingAgentId === agent.id}
              />
            ))
          )}
        </div>
      </ScrollArea>
    </div>
  )
}

interface AgentCardProps {
  agent: Agent
  onViewDetails: (agent: Agent) => void
  onChat: (agent: Agent) => void
  onTrash: (agent: Agent) => void
  onKill: (agent: Agent) => void
  onStart: (agent: Agent) => void
  isChatLoading?: boolean
  isTrashLoading?: boolean
  isKillLoading?: boolean
  isStartLoading?: boolean
}

/**
 * Render a compact card showing an agent's metadata, status, and action buttons.
 *
 * Displays the agent's name, role, ID, workspace, model, timestamps, skills, and a status indicator,
 * and exposes controls to open a chat, view details, start, stop (kill), or remove (trash) the agent.
 *
 * @param agent - The agent object to display
 * @param onViewDetails - Callback invoked when the "view details" action is triggered
 * @param onChat - Callback invoked when the "chat" action is triggered
 * @param onTrash - Callback invoked when the "remove/trash" action is triggered
 * @param onKill - Callback invoked when the "stop/kill" action is triggered
 * @param onStart - Callback invoked when the "start/resume" action is triggered
 * @param isChatLoading - Whether the chat action is currently loading (disables chat button)
 * @param isTrashLoading - Whether the trash action is currently loading (disables trash button)
 * @param isKillLoading - Whether the kill action is currently loading (disables kill button)
 * @param isStartLoading - Whether the start action is currently loading (disables start button)
 * @returns A JSX element representing the agent card
 */
function AgentCard({ agent, onViewDetails, onChat, onTrash, onKill, onStart, isChatLoading, isTrashLoading, isKillLoading, isStartLoading }: AgentCardProps) {
  const stateColors: Record<string, string> = {
    running: 'bg-green-500',
    idle: 'bg-yellow-500',
    stopped: 'bg-gray-500',
    error: 'bg-red-500',
  }

  const stateLabels: Record<string, string> = {
    running: 'Running',
    idle: 'Idle',
    stopped: 'Stopped',
    error: 'Error',
  }

  const getWorkspaceDisplayName = (ns: string) => {
    if (!ns || ns === '/') return 'root'
    const parts = ns.split('/')
    return parts[parts.length - 1] || ns
  }

  return (
    <Card className="hover:bg-accent/30 transition-colors">
      <CardContent className="p-4">
        <div className="flex items-start justify-between">
          <div className="flex items-start gap-3 flex-1 min-w-0">
            <div className="relative flex-shrink-0">
              <div className={cn(
                'h-10 w-10 rounded-lg flex items-center justify-center',
                agent.state === 'running' ? 'bg-green-500/10' : 'bg-muted'
              )}>
                <Bot className={cn(
                  'h-5 w-5',
                  agent.state === 'running' ? 'text-green-500' : 'text-muted-foreground'
                )} />
              </div>
              <span
                className={cn(
                  'absolute -bottom-0.5 -right-0.5 h-3 w-3 rounded-full border-2 border-card',
                  stateColors[agent.state] || 'bg-gray-500'
                )}
                title={stateLabels[agent.state] || agent.state}
              />
            </div>
            <div className="flex-1 min-w-0">
              <div className="flex items-center gap-2 flex-wrap">
                <span className="font-medium text-foreground">
                  {agent.name || agent.slug || agent.role || 'Agent'}
                </span>
                {agent.name && agent.role && (
                  <Badge variant="secondary" className="text-xs capitalize">
                    {agent.role}
                  </Badge>
                )}
                <Badge
                  variant={agent.state === 'running' ? 'default' : 'outline'}
                  className={cn(
                    'text-xs',
                    agent.state === 'running' && 'bg-green-500/10 text-green-500 border-green-500/20'
                  )}
                >
                  {stateLabels[agent.state] || agent.state}
                </Badge>
              </div>

              {/* ID and Namespace */}
              <div className="flex items-center gap-3 mt-1 text-xs text-muted-foreground">
                <span className="flex items-center gap-1 font-mono" title={agent.id}>
                  <Hash className="h-3 w-3" />
                  {agent.id.slice(0, 8)}
                </span>
                {agent.ns && (
                  <span className="flex items-center gap-1" title={agent.ns}>
                    <Folder className="h-3 w-3" />
                    {getWorkspaceDisplayName(agent.ns)}
                  </span>
                )}
              </div>

              {/* Model and Timing info */}
              <div className="flex items-center gap-3 mt-2 text-xs text-muted-foreground flex-wrap">
                <span className="flex items-center gap-1" title={`Provider: ${agent.llm_provider || 'default'}`}>
                  <Cpu className="h-3 w-3" />
                  {agent.llm_model || 'default model'}
                </span>
                {agent.created_at && (
                  <span className="flex items-center gap-1" title={`Created: ${new Date(agent.created_at).toLocaleString()}`}>
                    <Calendar className="h-3 w-3" />
                    {formatRelativeTime(agent.created_at)}
                  </span>
                )}
                {agent.heartbeat_at && (
                  <span className="flex items-center gap-1" title={`Last heartbeat: ${new Date(agent.heartbeat_at).toLocaleString()}`}>
                    <Clock className="h-3 w-3" />
                    {formatRelativeTime(agent.heartbeat_at)}
                  </span>
                )}
              </div>

              {/* Skills if present */}
              {agent.skills_allow && agent.skills_allow.length > 0 && (
                <div className="mt-2 flex items-center gap-1 flex-wrap">
                  {agent.skills_allow.slice(0, 3).map((skill) => (
                    <Badge key={skill} variant="secondary" className="text-xs font-mono">
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
            </div>
          </div>

          {/* Actions */}
          <div className="flex items-center gap-1 flex-shrink-0">
            <Button
              variant="ghost"
              size="icon"
              className="h-8 w-8 text-primary hover:text-primary hover:bg-primary/10"
              title="Chat with agent"
              onClick={() => onChat(agent)}
              disabled={isChatLoading}
            >
              <MessageSquare className={cn('h-4 w-4', isChatLoading && 'animate-pulse')} />
            </Button>
            <Button
              variant="ghost"
              size="icon"
              className="h-8 w-8"
              title="View details"
              onClick={() => onViewDetails(agent)}
            >
              <Eye className="h-4 w-4" />
            </Button>
            {agent.state === 'running' ? (
              <Button
                variant="ghost"
                size="icon"
                className="h-8 w-8 text-orange-500 hover:text-orange-600 hover:bg-orange-500/10"
                title="Stop agent"
                onClick={(e) => {
                  e.stopPropagation()
                  onKill(agent)
                }}
                disabled={isKillLoading}
              >
                {isKillLoading ? (
                  <RefreshCw className="h-4 w-4 animate-spin" />
                ) : (
                  <Square className="h-4 w-4" />
                )}
              </Button>
            ) : agent.state === 'stopped' ? (
              <>
                <Button
                  variant="ghost"
                  size="icon"
                  className="h-8 w-8 text-green-500 hover:text-green-600 hover:bg-green-500/10"
                  title="Resume agent"
                  onClick={(e) => {
                    e.stopPropagation()
                    onStart(agent)
                  }}
                  disabled={isStartLoading}
                >
                  {isStartLoading ? (
                    <RefreshCw className="h-4 w-4 animate-spin" />
                  ) : (
                    <Play className="h-4 w-4" />
                  )}
                </Button>
                <Button
                  variant="ghost"
                  size="icon"
                  className="h-8 w-8 text-red-500 hover:text-red-600 hover:bg-red-500/10"
                  title="Remove agent"
                  onClick={() => onTrash(agent)}
                  disabled={isTrashLoading}
                >
                  <Trash2 className={cn('h-4 w-4', isTrashLoading && 'animate-pulse')} />
                </Button>
              </>
            ) : (
              <Button
                variant="ghost"
                size="icon"
                className="h-8 w-8 text-green-500 hover:text-green-600 hover:bg-green-500/10"
                title="Resume agent"
                onClick={(e) => {
                  e.stopPropagation()
                  onStart(agent)
                }}
                disabled={isStartLoading}
              >
                {isStartLoading ? (
                  <RefreshCw className="h-4 w-4 animate-spin" />
                ) : (
                  <Play className="h-4 w-4" />
                )}
              </Button>
            )}
          </div>
        </div>
      </CardContent>
    </Card>
  )
}

interface SpawnAgentFormProps {
  onClose: () => void
  onSuccess: (actorId: string, spawnData: AgentSpawnResponse) => void
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
  const mutation = useMutation({
    mutationFn: (params: SpawnAgentParams) => spawnAgent(params),
    onSuccess: (data) => {
      onSuccess(data.actor_id, data)
    },
    onError: (error) => {
      console.error('[SpawnAgentForm] Spawn failed:', error)
    },
  })

  return (
    <div className="p-4 border-b border-border bg-muted/30 max-h-[70vh] overflow-y-auto">
      <SpawnAgentFormCore
        onSubmit={(params) => mutation.mutate(params)}
        onCancel={onClose}
        isPending={mutation.isPending}
        error={mutation.error instanceof Error ? mutation.error : null}
      />
    </div>
  )
}