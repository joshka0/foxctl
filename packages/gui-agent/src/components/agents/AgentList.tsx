import { useState, useMemo } from 'react'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { ScrollArea } from '@/components/ui/scroll-area'
import { Button } from '@/components/ui/button'
import { Badge } from '@/components/ui/badge'
import { Card, CardContent } from '@/components/ui/card'
import { Input } from '@/components/ui/input'
import { cn, formatRelativeTime } from '@/lib/utils'
import { listAgents, spawnAgent, trashAgent, killAgent, startAgent, createConsoleSession, getCompanionConversationMessages, companionChat, listSkills, type SpawnAgentParams } from '@/api/client'
import type { Agent, AgentSpawnResponse } from '@/api/types'
import { useChatStore } from '@/stores/chatStore'
import { useViewStore } from '@/stores/viewStore'
import type { ConsoleMessage } from '@/api/client'
import { AgentDetailView } from './AgentDetailView'
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
  ChevronDown,
  ChevronRight,
  Trash2,
  Sparkles,
} from 'lucide-react'
import { RoleSelector } from './RoleSelector'
import { EXEC_MODES, PROVIDERS, getRoleById, getProviderById } from './spawnFormConstants'

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
  const [selectedAgent, setSelectedAgent] = useState<Agent | null>(null)
  const [chatLoadingAgentId, setChatLoadingAgentId] = useState<string | null>(null)
  const [trashLoadingAgentId, setTrashLoadingAgentId] = useState<string | null>(null)
  const [killLoadingAgentId, setKillLoadingAgentId] = useState<string | null>(null)
  const [startLoadingAgentId, setStartLoadingAgentId] = useState<string | null>(null)
  const queryClient = useQueryClient()

  // Access chat store to switch sessions
  const { setSessionId, setSession, setMessages, setInflight, setPersistedSessionId, setInitializing, setSourceAgent } = useChatStore()
  const setActiveView = useViewStore((s) => s.setActiveView)
  const spawnAgentOpen = useViewStore((s) => s.spawnAgentOpen)
  const setSpawnAgentOpen = useViewStore((s) => s.setSpawnAgentOpen)

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
              state: spawnData.status || 'running',
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

  const getTimeSince = (dateStr: string) => {
    const date = new Date(dateStr)
    const now = new Date()
    const diffMs = now.getTime() - date.getTime()
    const diffMins = Math.floor(diffMs / 60000)
    const diffHours = Math.floor(diffMins / 60)
    const diffDays = Math.floor(diffHours / 24)

    if (diffDays > 0) return `${diffDays}d ago`
    if (diffHours > 0) return `${diffHours}h ago`
    if (diffMins > 0) return `${diffMins}m ago`
    return 'just now'
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
                    {getTimeSince(agent.created_at)}
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
 * The form supports selecting a role, editing or enhancing the system prompt,
 * choosing execution mode, optional provider/model configuration, iteration
 * limits, and selecting allowed skills. Submitting the form calls the spawn
 * API and invokes the success callback with the returned actor id and data.
 *
 * @param onClose - Called when the user cancels or closes the form
 * @param onSuccess - Called after a successful spawn with the new agent's `actorId` and the full spawn response
 * @returns The spawn agent form React element
 */
function SpawnAgentForm({ onClose, onSuccess }: SpawnAgentFormProps) {
  const [showAdvanced, setShowAdvanced] = useState(false)
  const [showSkills, setShowSkills] = useState(false)
  const [isEnhancing, setIsEnhancing] = useState(false)
  const [customModel, setCustomModel] = useState('')
  const [formData, setFormData] = useState<SpawnAgentParams>({
    role: 'coder',
    prompt: '',
    name: '',
    exec_mode: 'reactive',
    llm_provider: '',
    llm_model: '',
    max_iterations: 10,
    max_auto_turns: 1,
    skills_allow: [],
  })

  // Fetch available skills
  const { data: skillsData } = useQuery({
    queryKey: ['skills'],
    queryFn: listSkills,
  })

  const skills = useMemo(() => skillsData?.skills ?? [], [skillsData?.skills])
  // Group skills by toolkit (first tag) for easier selection
  const skillsByToolkit = useMemo(() => {
    const toolkits: Record<string, (typeof skills)[number][]> = {}
    const seen = new Set<string>()
    for (const skill of skills) {
      // Deduplicate by skill name
      if (seen.has(skill.name)) continue
      seen.add(skill.name)
      // Use first tag as toolkit, fall back to name prefix
      const toolkit = skill.tags?.[0] || skill.name.split('/')[0] || 'other'
      if (!toolkits[toolkit]) toolkits[toolkit] = []
      toolkits[toolkit].push(skill)
    }
    // Sort toolkits alphabetically
    return Object.fromEntries(
      Object.entries(toolkits).sort(([a], [b]) => a.localeCompare(b))
    )
  }, [skills])

  const mutation = useMutation({
    mutationFn: () => {
      const params: SpawnAgentParams = {
        role: formData.role,
        prompt: formData.prompt,
      }
      if (formData.name?.trim()) params.name = formData.name.trim()
      if (formData.exec_mode && formData.exec_mode !== 'reactive') {
        params.exec_mode = formData.exec_mode
      }
      if (formData.llm_provider) params.llm_provider = formData.llm_provider
      if (formData.llm_model) params.llm_model = formData.llm_model
      if (formData.max_iterations && formData.max_iterations !== 10) {
        params.max_iterations = formData.max_iterations
      }
      if (formData.max_auto_turns && formData.max_auto_turns !== 1) {
        params.max_auto_turns = formData.max_auto_turns
      }
      if (formData.skills_allow && formData.skills_allow.length > 0) {
        params.skills_allow = formData.skills_allow
      }
      return spawnAgent(params)
    },
    onSuccess: (data) => {
      console.log('[SpawnAgentForm] Spawn success:', data)
      onSuccess(data.actor_id, data)
    },
    onError: (error) => {
      console.error('[SpawnAgentForm] Spawn failed:', error)
    },
  })

  const handleSubmit = (e: React.FormEvent) => {
    e.preventDefault()
    if (formData.prompt.trim()) {
      mutation.mutate()
    }
  }

  // Handle role change - auto-fill default prompt if prompt is empty
  const handleRoleChange = (roleId: string) => {
    const role = getRoleById(roleId)
    const currentRole = getRoleById(formData.role)

    // Auto-fill if prompt is empty or matches previous role's default
    const shouldAutoFill = !formData.prompt.trim() ||
      (currentRole && formData.prompt.trim() === currentRole.defaultPrompt)

    setFormData({
      ...formData,
      role: roleId,
      prompt: shouldAutoFill && role ? role.defaultPrompt : formData.prompt,
    })
  }

  // Use default prompt for current role
  const handleUseDefaultPrompt = () => {
    const role = getRoleById(formData.role)
    if (role) {
      setFormData({ ...formData, prompt: role.defaultPrompt })
    }
  }

  // Enhance prompt with AI
  const handleEnhancePrompt = async () => {
    if (!formData.prompt.trim()) return

    setIsEnhancing(true)
    try {
      const result = await companionChat({
        conversation_id: `enhance-${Date.now()}`,
        message: `Improve and expand this agent system prompt for a ${formData.role} role. Make it more specific and actionable while keeping the same intent. Return ONLY the improved prompt text, no explanation:\n\n${formData.prompt}`,
      })
      if (result.response) {
        setFormData({ ...formData, prompt: result.response.trim() })
      }
    } catch (err) {
      console.error('Failed to enhance prompt:', err)
    } finally {
      setIsEnhancing(false)
    }
  }

  // Handle provider change - reset model
  const handleProviderChange = (providerId: string) => {
    const provider = getProviderById(providerId)
    const firstModel = provider?.models[0]?.id ?? ''
    setFormData({
      ...formData,
      llm_provider: providerId,
      llm_model: firstModel,
    })
    setCustomModel('')
  }

  // Handle skill toggle
  const handleSkillToggle = (skillName: string) => {
    const current = formData.skills_allow ?? []
    const updated = current.includes(skillName)
      ? current.filter((s) => s !== skillName)
      : [...current, skillName]
    setFormData({ ...formData, skills_allow: updated })
  }

  const selectedProvider = getProviderById(formData.llm_provider || '')
  const availableModels = selectedProvider?.models ?? []

  return (
    <div className="p-4 border-b border-border bg-muted/30 max-h-[70vh] overflow-y-auto">
      <form onSubmit={handleSubmit} className="space-y-4">
        {/* Name */}
        <div>
          <label className="text-sm font-medium text-foreground">Name</label>
          <Input
            value={formData.name}
            onChange={(e) => setFormData({ ...formData, name: e.target.value })}
            placeholder="Auto-generated if empty"
            className="h-9 text-sm mt-1"
          />
          <p className="text-xs text-muted-foreground mt-1">
            Optional - a memorable name for this agent
          </p>
        </div>

        {/* Role Card Grid */}
        <div>
          <label className="text-sm font-medium text-foreground mb-2 block">Role</label>
          <RoleSelector
            selectedRole={formData.role}
            onSelectRole={handleRoleChange}
          />
        </div>

        {/* Prompt */}
        <div>
          <div className="flex items-center justify-between mb-1">
            <label className="text-sm font-medium text-foreground">Prompt</label>
            <div className="flex items-center gap-2">
              <button
                type="button"
                onClick={handleUseDefaultPrompt}
                className="text-xs text-primary hover:underline"
              >
                Use default
              </button>
              <button
                type="button"
                onClick={handleEnhancePrompt}
                disabled={isEnhancing || !formData.prompt.trim()}
                className="text-xs text-primary hover:underline flex items-center gap-1 disabled:opacity-50"
                title="Enhance prompt with AI"
              >
                {isEnhancing ? (
                  <RefreshCw className="h-3 w-3 animate-spin" />
                ) : (
                  <Sparkles className="h-3 w-3" />
                )}
                Enhance
              </button>
            </div>
          </div>
          <textarea
            value={formData.prompt}
            onChange={(e) => setFormData({ ...formData, prompt: e.target.value })}
            placeholder="What should this agent do? Be specific about the task..."
            className="w-full h-24 rounded-md border border-input bg-background px-3 py-2 text-sm resize-none"
          />
        </div>

        {/* Execution Mode */}
        <div>
          <label className="text-sm font-medium text-foreground">Execution Mode</label>
          <select
            value={formData.exec_mode}
            onChange={(e) => setFormData({ ...formData, exec_mode: e.target.value as SpawnAgentParams['exec_mode'] })}
            className="w-full mt-1 h-9 rounded-md border border-input bg-background px-3 text-sm"
          >
            {EXEC_MODES.map((mode) => (
              <option key={mode.id} value={mode.id}>
                {mode.name} - {mode.description}
              </option>
            ))}
          </select>
          <p className="text-xs text-muted-foreground mt-1">
            {EXEC_MODES.find((m) => m.id === formData.exec_mode)?.details}
          </p>
        </div>

        {/* Advanced Options Toggle */}
        <button
          type="button"
          onClick={() => setShowAdvanced(!showAdvanced)}
          className="text-sm text-muted-foreground hover:text-foreground flex items-center gap-1"
        >
          {showAdvanced ? <ChevronDown className="h-4 w-4" /> : <ChevronRight className="h-4 w-4" />}
          Advanced Options
        </button>

        {showAdvanced && (
          <div className="space-y-3 pl-2 border-l-2 border-border">
            {/* Provider & Model Selection */}
            <div className="grid grid-cols-2 gap-3">
              <div>
                <label className="text-xs font-medium text-muted-foreground">Provider</label>
                <select
                  value={formData.llm_provider}
                  onChange={(e) => handleProviderChange(e.target.value)}
                  className="w-full mt-1 h-8 rounded-md border border-input bg-background px-2 text-sm"
                >
                  {PROVIDERS.map((provider) => (
                    <option key={provider.id} value={provider.id}>
                      {provider.name}
                    </option>
                  ))}
                </select>
              </div>
              <div>
                <label className="text-xs font-medium text-muted-foreground">Model</label>
                {selectedProvider?.allowCustom ? (
                  <div className="space-y-1 mt-1">
                    <select
                      value={customModel ? '' : formData.llm_model}
                      onChange={(e) => {
                        setFormData({ ...formData, llm_model: e.target.value })
                        setCustomModel('')
                      }}
                      className="w-full h-8 rounded-md border border-input bg-background px-2 text-sm"
                    >
                      {availableModels.map((model) => (
                        <option key={model.id} value={model.id}>
                          {model.name}
                        </option>
                      ))}
                      <option value="">Custom...</option>
                    </select>
                    {(customModel || formData.llm_model === '') && (
                      <Input
                        value={customModel}
                        onChange={(e) => {
                          setCustomModel(e.target.value)
                          setFormData({ ...formData, llm_model: e.target.value })
                        }}
                        placeholder="e.g., anthropic/claude-3-opus"
                        className="h-8 text-sm"
                      />
                    )}
                  </div>
                ) : (
                  <select
                    value={formData.llm_model}
                    onChange={(e) => setFormData({ ...formData, llm_model: e.target.value })}
                    className="w-full mt-1 h-8 rounded-md border border-input bg-background px-2 text-sm"
                  >
                    {availableModels.map((model) => (
                      <option key={model.id} value={model.id}>
                        {model.name}
                      </option>
                    ))}
                  </select>
                )}
              </div>
            </div>

            {/* Iteration Limits */}
            <div className="grid grid-cols-2 gap-3">
              <div>
                <label className="text-xs font-medium text-muted-foreground">Max Iterations</label>
                <Input
                  type="number"
                  value={formData.max_iterations}
                  onChange={(e) => setFormData({ ...formData, max_iterations: parseInt(e.target.value) || 10 })}
                  min={1}
                  max={100}
                  className="h-8 text-sm mt-1"
                />
                <p className="text-xs text-muted-foreground mt-0.5">Tool calls per turn</p>
              </div>
              <div>
                <label className="text-xs font-medium text-muted-foreground">Max Auto Turns</label>
                <Input
                  type="number"
                  value={formData.max_auto_turns}
                  onChange={(e) => setFormData({ ...formData, max_auto_turns: parseInt(e.target.value) || 1 })}
                  min={1}
                  max={20}
                  className="h-8 text-sm mt-1"
                />
                <p className="text-xs text-muted-foreground mt-0.5">Autonomous continuations</p>
              </div>
            </div>

            {/* Skills Section */}
            <div>
              <button
                type="button"
                onClick={() => setShowSkills(!showSkills)}
                className="text-xs text-muted-foreground hover:text-foreground flex items-center gap-1"
              >
                {showSkills ? <ChevronDown className="h-3 w-3" /> : <ChevronRight className="h-3 w-3" />}
                Skills ({formData.skills_allow?.length || 0} selected, empty = all)
              </button>

              {showSkills && (
                <div className="mt-2 space-y-2 max-h-48 overflow-y-auto">
                  <div className="flex gap-2 mb-2">
                    <button
                      type="button"
                      onClick={() => setFormData({ ...formData, skills_allow: skills.map((s) => s.name) })}
                      className="text-xs text-primary hover:underline"
                    >
                      Select All
                    </button>
                    <button
                      type="button"
                      onClick={() => setFormData({ ...formData, skills_allow: [] })}
                      className="text-xs text-primary hover:underline"
                    >
                      Clear All
                    </button>
                  </div>
                  {Object.entries(skillsByToolkit).map(([toolkit, toolkitSkills]) => {
                    const toolkitSkillNames = toolkitSkills.map((s) => s.name)
                    const selectedInToolkit = toolkitSkillNames.filter((name) => formData.skills_allow?.includes(name))
                    const allSelected = selectedInToolkit.length === toolkitSkillNames.length
                    const someSelected = selectedInToolkit.length > 0 && !allSelected

                    const handleToolkitToggle = () => {
                      const current = formData.skills_allow ?? []
                      if (allSelected) {
                        // Deselect all in this toolkit
                        setFormData({ ...formData, skills_allow: current.filter((s) => !toolkitSkillNames.includes(s)) })
                      } else {
                        // Select all in this toolkit
                        const newSkills = [...new Set([...current, ...toolkitSkillNames])]
                        setFormData({ ...formData, skills_allow: newSkills })
                      }
                    }

                    return (
                      <div key={toolkit} className="border border-border rounded-md p-2">
                        <label className="flex items-center gap-2 mb-2 cursor-pointer">
                          <input
                            type="checkbox"
                            checked={allSelected}
                            ref={(el) => { if (el) el.indeterminate = someSelected }}
                            onChange={handleToolkitToggle}
                            className="h-3.5 w-3.5"
                          />
                          <span className="text-xs font-medium text-foreground capitalize">{toolkit}</span>
                          <span className="text-xs text-muted-foreground">({selectedInToolkit.length}/{toolkitSkillNames.length})</span>
                        </label>
                        <div className="grid grid-cols-2 gap-1 pl-5">
                          {toolkitSkills.map((skill) => (
                            <label
                              key={skill.name}
                              className="flex items-start gap-2 text-xs cursor-pointer hover:bg-accent/50 p-1 rounded"
                            >
                              <input
                                type="checkbox"
                                checked={formData.skills_allow?.includes(skill.name) ?? false}
                                onChange={() => handleSkillToggle(skill.name)}
                                className="mt-0.5"
                              />
                              <span className="truncate" title={skill.description}>
                                {skill.name}
                              </span>
                            </label>
                          ))}
                        </div>
                      </div>
                    )
                  })}
                </div>
              )}
            </div>
          </div>
        )}

        {/* Actions */}
        <div className="flex items-center gap-2 pt-2">
          <Button type="submit" disabled={!formData.prompt.trim() || mutation.isPending}>
            {mutation.isPending ? (
              <>
                <RefreshCw className="h-4 w-4 mr-1 animate-spin" />
                Spawning...
              </>
            ) : (
              <>
                <Plus className="h-4 w-4 mr-1" />
                Spawn Agent
              </>
            )}
          </Button>
          <Button type="button" variant="ghost" onClick={onClose}>
            Cancel
          </Button>
        </div>

        {mutation.isError && (
          <p className="text-sm text-red-500">
            Error: {mutation.error instanceof Error ? mutation.error.message : 'Unknown error'}
          </p>
        )}
      </form>
    </div>
  )
}