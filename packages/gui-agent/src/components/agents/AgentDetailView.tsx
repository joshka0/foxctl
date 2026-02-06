import { useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { ScrollArea } from '@/components/ui/scroll-area'
import { Button } from '@/components/ui/button'
import { Badge } from '@/components/ui/badge'
import { Card, CardContent, CardHeader } from '@/components/ui/card'
import { cn, formatRelativeTime } from '@/lib/utils'
import {
  listPersistedSessions,
  getSessionMessages,
  createConsoleSession,
  listCompanionConversations,
  type PersistedSession,
  type SessionMessage,
} from '@/api/client'
import type { Agent } from '@/api/types'
import { useChatStore } from '@/stores/chatStore'
import { useViewStore } from '@/stores/viewStore'
import type { ConsoleMessage } from '@/api/client'
import {
  ArrowLeft,
  Bot,
  Clock,
  MessageSquare,
  MessageCircle,
  Wrench,
  FileText,
  ChevronDown,
  ChevronRight,
  Folder,
  Cpu,
  User,
  Sparkles,
  AlertCircle,
} from 'lucide-react'

interface AgentDetailViewProps {
  agent: Agent
  onBack: () => void
}

/**
 * Renders a detailed view for an agent including header, actions, linked conversation, a list of persisted sessions, and a session detail/messages pane.
 *
 * @param agent - The agent object whose details and sessions are displayed.
 * @param onBack - Callback invoked when the back button is clicked to navigate away from the view.
 * @returns The Agent detail view UI component.
 */
export function AgentDetailView({ agent, onBack }: AgentDetailViewProps) {
  const [selectedSession, setSelectedSession] = useState<PersistedSession | null>(null)
  const [isChatLoading, setIsChatLoading] = useState(false)
  const { setSessionId, setSession, setMessages, setInflight, setPersistedSessionId, setInitializing, setSourceAgent } = useChatStore()
  const setActiveView = useViewStore((s) => s.setActiveView)

  // Fetch sessions for this agent
  const { data: sessionsData, isLoading: loadingSessions } = useQuery({
    queryKey: ['agent-sessions', agent.id, agent.ns],
    queryFn: async () => {
      const data = await listPersistedSessions({ limit: 200, workspace: agent.ns || undefined })
      if (data.sessions.length === 0 && agent.ns) {
        return listPersistedSessions({ limit: 200 })
      }
      return data
    },
  })

  // Fetch linked conversation if agent has conversation_id
  const { data: conversationsData } = useQuery({
    queryKey: ['companion-conversations'],
    queryFn: () => listCompanionConversations(100),
    enabled: !!agent.conversation_id,
  })

  // Find the linked conversation
  const linkedConversation = agent.conversation_id
    ? conversationsData?.conversations?.find((c) => c.id === agent.conversation_id)
    : null

  // Navigate to linked conversation
  const handleGoToConversation = () => {
    if (agent.conversation_id) {
      // Store the conversation ID to auto-select it when ConversationsList mounts
      localStorage.setItem('gui-agent-auto-select-conversation', agent.conversation_id)
      setActiveView('conversations')
    }
  }

  // Filter sessions by agent_id
  const actorId = `actor:agent:${agent.id}`
  const workspaceSessions = sessionsData?.sessions ?? []
  const agentSessions = workspaceSessions.filter((s) => {
    if (s.agent_id === agent.id || s.agent_id === actorId) return true
    if (agent.role && s.agent_type === agent.role) return true
    return false
  })

  // Get most recent session for this agent
  const mostRecentSession = agentSessions.length > 0
    ? agentSessions.sort((a, b) => new Date(b.started_at).getTime() - new Date(a.started_at).getTime())[0]
    : workspaceSessions.length > 0
      ? workspaceSessions[0]
      : null

  // Handle loading agent into companion chat
  // If no persistedSession provided, will auto-load from most recent session for this agent
  const handleChat = async (persistedSession?: PersistedSession) => {
    setIsChatLoading(true)
    setInitializing(true) // Prevent CompanionChat from auto-initializing

    // Determine which persisted session to load from (before creating console session)
    const sessionToLoad = persistedSession || mostRecentSession

    try {
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
${sessionToLoad ? `- Continuing from previous session: ${sessionToLoad.id.slice(0, 16)}... (${sessionToLoad.message_count} messages)` : ''}

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

      // Load messages from persisted session if available
      if (sessionToLoad) {
        const messagesData = await getSessionMessages(sessionToLoad.id, { limit: 200 })
        // Convert SessionMessage to ConsoleMessage format (handle legacy 'human' type).
        const consoleMessages: ConsoleMessage[] = messagesData.messages
          .filter((msg) => msg.type === 'user' || msg.type === 'assistant' || msg.type === 'human')
          .map((msg) => ({
            role: msg.type === 'human' ? 'user' : (msg.type as 'user' | 'assistant'),
            content: msg.summary || msg.error || '[No content]',
            timestamp: msg.timestamp || new Date().toISOString(),
          }))
        setMessages(consoleMessages)
        setPersistedSessionId(sessionToLoad.id)
      } else {
        setMessages([])
        setPersistedSessionId(null)
      }

      setInflight(false)

      // Switch to conversations view to show the new chat
      setActiveView('conversations')

      // Set localStorage AFTER switching view and loading messages
      localStorage.setItem('gui-agent-session-id', newSessionId)

      // Clear init flag now that session and messages are ready.
      setInitializing(false)
    } catch (err) {
      console.error('Failed to create chat session for agent:', err)
      setInitializing(false)
    } finally {
      setIsChatLoading(false)
    }
  }

  return (
    <div className="flex flex-col h-full">
      {/* Header */}
      <div className="p-4 border-b border-border">
        <div className="flex items-center justify-between">
          <div className="flex items-center gap-3">
            <Button variant="ghost" size="icon" onClick={onBack} className="h-8 w-8">
              <ArrowLeft className="h-4 w-4" />
            </Button>
            <div className="flex items-center gap-3">
              <div className={cn(
                'h-10 w-10 rounded-lg flex items-center justify-center',
                agent.state === 'running' ? 'bg-green-500/10' : 'bg-muted'
              )}>
                <Bot className={cn(
                  'h-5 w-5',
                  agent.state === 'running' ? 'text-green-500' : 'text-muted-foreground'
                )} />
              </div>
              <div>
                <div className="flex items-center gap-2">
                  <h2 className="text-lg font-semibold text-foreground">
                    {agent.name || agent.slug || agent.role || 'Agent'}
                  </h2>
                  {agent.name && agent.role && (
                    <Badge variant="secondary" className="text-xs capitalize">
                      {agent.role}
                    </Badge>
                  )}
                </div>
                <div className="flex items-center gap-2 text-xs text-muted-foreground">
                  <span className="font-mono">{agent.id.slice(0, 12)}...</span>
                  <Badge variant={agent.state === 'running' ? 'default' : 'outline'} className="text-xs">
                    {agent.state}
                  </Badge>
                </div>
              </div>
            </div>
          </div>
          <Button
            variant="default"
            size="sm"
            onClick={() => handleChat()}
            disabled={isChatLoading || loadingSessions}
            className="gap-1"
          >
            <MessageCircle className={cn('h-4 w-4', (isChatLoading || loadingSessions) && 'animate-pulse')} />
            {isChatLoading ? 'Loading...' : loadingSessions ? 'Loading sessions...' : mostRecentSession ? `Continue (${agentSessions.length} sessions)` : 'Chat'}
          </Button>
        </div>

        {/* Agent Info */}
        <div className="mt-4 grid grid-cols-2 gap-3 text-sm">
          <div className="flex items-center gap-2 text-muted-foreground">
            <Cpu className="h-4 w-4" />
            <span>{agent.llm_model || 'default model'}</span>
          </div>
          {agent.ns && (
            <div className="flex items-center gap-2 text-muted-foreground">
              <Folder className="h-4 w-4" />
              <span className="truncate">{agent.ns}</span>
            </div>
          )}
          {agent.created_at && (
            <div className="flex items-center gap-2 text-muted-foreground">
              <Clock className="h-4 w-4" />
              <span>Created {formatRelativeTime(agent.created_at)}</span>
            </div>
          )}
        </div>
      </div>

      {/* Content */}
      <div className="flex-1 flex overflow-hidden">
        {/* Sessions List */}
        <div className="w-72 border-r border-border flex flex-col">
          {/* Linked Conversation Section */}
          {agent.conversation_id && (
            <div className="p-3 border-b border-border">
              <h3 className="text-sm font-medium text-foreground mb-2">Linked Conversation</h3>
              <button
                onClick={handleGoToConversation}
                className="w-full p-2 rounded-md text-left bg-primary/5 hover:bg-primary/10 border border-primary/20 transition-colors"
              >
                <div className="flex items-center gap-2">
                  <MessageCircle className="h-4 w-4 text-primary" />
                  <div className="flex-1 min-w-0">
                    <div className="text-sm font-medium text-foreground truncate">
                      {linkedConversation?.name || linkedConversation?.id?.slice(0, 16) || agent.conversation_id.slice(0, 16)}...
                    </div>
                    {linkedConversation && (
                      <div className="flex items-center gap-2 text-xs text-muted-foreground mt-0.5">
                        <span>{linkedConversation.message_count} messages</span>
                      </div>
                    )}
                  </div>
                  <ChevronRight className="h-4 w-4 text-muted-foreground" />
                </div>
              </button>
            </div>
          )}

          <div className="p-3 border-b border-border">
            <h3 className="text-sm font-medium text-foreground">Sessions</h3>
            <p className="text-xs text-muted-foreground mt-1">
              {agentSessions.length} session{agentSessions.length !== 1 ? 's' : ''} found
            </p>
          </div>
          <ScrollArea className="flex-1">
            <div className="p-2 space-y-1">
              {loadingSessions ? (
                <div className="p-4 text-center text-muted-foreground text-sm">
                  Loading sessions...
                </div>
              ) : agentSessions.length === 0 ? (
                <div className="p-4 text-center text-muted-foreground text-sm">
                  No sessions found for this agent
                </div>
              ) : (
                agentSessions.map((session) => (
                  <button
                    key={session.id}
                    onClick={() => setSelectedSession(session)}
                    className={cn(
                      'w-full p-2 rounded-md text-left hover:bg-accent/50 transition-colors',
                      selectedSession?.id === session.id && 'bg-accent'
                    )}
                  >
                    <div className="flex items-center justify-between">
                      <span className="text-xs font-mono text-foreground truncate">
                        {session.id.slice(0, 10)}...
                      </span>
                      <Badge variant="outline" className="text-xs shrink-0">
                        {session.status}
                      </Badge>
                    </div>
                    <div className="flex items-center gap-2 mt-1 text-xs text-muted-foreground">
                      <span className="flex items-center gap-1">
                        <MessageSquare className="h-3 w-3" />
                        {session.message_count}
                      </span>
                      <span className="flex items-center gap-1">
                        <Wrench className="h-3 w-3" />
                        {session.tool_invocations}
                      </span>
                      <span className="flex items-center gap-1">
                        <Clock className="h-3 w-3" />
                        {formatRelativeTime(session.started_at)}
                      </span>
                    </div>
                    {session.summary && (
                      <p className="text-xs text-muted-foreground mt-1 line-clamp-2">
                        {session.summary}
                      </p>
                    )}
                  </button>
                ))
              )}
            </div>
          </ScrollArea>
        </div>

        {/* Session Detail / Messages */}
        <div className="flex-1 flex flex-col overflow-hidden">
          {selectedSession ? (
            <SessionDetail session={selectedSession} onContinue={handleChat} />
          ) : (
            <div className="flex-1 flex items-center justify-center text-muted-foreground">
              <div className="text-center">
                <MessageSquare className="h-12 w-12 mx-auto mb-3 opacity-30" />
                <p>Select a session to view messages</p>
              </div>
            </div>
          )}
        </div>
      </div>
    </div>
  )
}

/**
 * Renders detailed information and messages for a persisted session, with an optional action to continue the session.
 *
 * Displays session metadata (id, message/tool counts, status), optional summary and accomplished items, and a scrollable list of session messages. Each message shows type, index, timestamp, tool usage, error badge, file touches, and expandable content for long messages.
 *
 * @param session - The persisted session to display.
 * @param onContinue - Optional callback invoked with `session` when the "Continue Session" action is triggered.
 * @returns The SessionDetail UI element for the provided session.
 */
function SessionDetail({ session, onContinue }: { session: PersistedSession; onContinue?: (session: PersistedSession) => void | Promise<void> }) {
  const [expandedMessages, setExpandedMessages] = useState<Set<number>>(new Set())

  const { data: messagesData, isLoading } = useQuery({
    queryKey: ['session-messages', session.id],
    queryFn: () => getSessionMessages(session.id, { limit: 200 }),
  })

  const messages = messagesData?.messages ?? []

  const toggleExpanded = (index: number) => {
    setExpandedMessages((prev) => {
      const next = new Set(prev)
      if (next.has(index)) {
        next.delete(index)
      } else {
        next.add(index)
      }
      return next
    })
  }

  const getMessageIcon = (type: string) => {
    switch (type) {
      case 'user':
      case 'human':
        return <User className="h-4 w-4 text-blue-500" />
      case 'assistant':
        return <Sparkles className="h-4 w-4 text-purple-500" />
      default:
        return <MessageSquare className="h-4 w-4 text-muted-foreground" />
    }
  }

  const getMessageContent = (msg: SessionMessage): string => {
    if (msg.summary) return msg.summary
    if (msg.error) return `Error: ${msg.error}`
    if (msg.message?.content) {
      if (typeof msg.message.content === 'string') {
        return msg.message.content
      }
      if (Array.isArray(msg.message.content)) {
        // Extract text from content blocks
        return msg.message.content
          .map((block: unknown) => {
            if (typeof block === 'string') return block
            if (typeof block === 'object' && block !== null) {
              const b = block as Record<string, unknown>
              if (b.type === 'text' && typeof b.text === 'string') return b.text
              if (b.type === 'tool_use') return `[Tool: ${b.name}]`
              if (b.type === 'tool_result') return `[Tool Result]`
            }
            return ''
          })
          .filter(Boolean)
          .join('\n')
      }
    }
    return '[No content]'
  }

  return (
    <div className="flex flex-col h-full">
      {/* Session Header */}
      <div className="p-4 border-b border-border">
        <div className="flex items-center justify-between">
          <div>
            <h3 className="text-sm font-medium text-foreground">
              Session {session.id.slice(0, 12)}...
            </h3>
            <p className="text-xs text-muted-foreground mt-1">
              {session.message_count} messages, {session.tool_invocations} tool calls
            </p>
          </div>
          <div className="flex items-center gap-2">
            <Button
              variant="default"
              size="sm"
              onClick={() => onContinue?.(session)}
              disabled={!onContinue}
              className="gap-1"
            >
              <MessageCircle className="h-4 w-4" />
              Continue Session
            </Button>
            <Badge variant={session.status === 'completed' ? 'secondary' : 'outline'}>
              {session.status}
            </Badge>
          </div>
        </div>

        {/* Session Summary */}
        {session.summary && (
          <div className="mt-3 p-3 bg-muted/30 rounded-md">
            <p className="text-sm text-foreground">{session.summary}</p>
          </div>
        )}

        {/* Accomplished */}
        {session.accomplished && session.accomplished.length > 0 && (
          <div className="mt-3">
            <h4 className="text-xs font-medium text-muted-foreground mb-1">Accomplished:</h4>
            <ul className="text-sm text-foreground space-y-1">
              {session.accomplished.map((item, i) => (
                <li key={i} className="flex items-start gap-2">
                  <span className="text-green-500">-</span>
                  {item}
                </li>
              ))}
            </ul>
          </div>
        )}
      </div>

      {/* Messages */}
      <ScrollArea className="flex-1">
        <div className="p-4 space-y-2">
          {isLoading ? (
            <div className="text-center py-8 text-muted-foreground">
              Loading messages...
            </div>
          ) : messages.length === 0 ? (
            <div className="text-center py-8 text-muted-foreground">
              No messages found
            </div>
          ) : (
            messages.map((msg, idx) => {
              const isExpanded = expandedMessages.has(idx)
              const content = getMessageContent(msg)
              const isLong = content.length > 200

              return (
                <Card key={idx} className="overflow-hidden">
                  <CardHeader
                    className="py-2 px-3 cursor-pointer hover:bg-accent/30"
                    onClick={() => isLong && toggleExpanded(idx)}
                  >
                    <div className="flex items-center justify-between">
                      <div className="flex items-center gap-2">
                        {getMessageIcon(msg.type)}
                        <span className="text-xs font-medium capitalize text-foreground">
                          {msg.type === 'human' ? 'User' : msg.type}
                        </span>
                        <span className="text-xs text-muted-foreground">
                          #{msg.index}
                        </span>
                        {msg.tool_calls && msg.tool_calls.length > 0 && (
                          <Badge variant="outline" className="text-xs">
                            <Wrench className="h-3 w-3 mr-1" />
                            {msg.tool_calls.length}
                          </Badge>
                        )}
                        {msg.error && (
                          <Badge variant="destructive" className="text-xs">
                            <AlertCircle className="h-3 w-3 mr-1" />
                            Error
                          </Badge>
                        )}
                      </div>
                      <div className="flex items-center gap-2">
                        {msg.timestamp && (
                          <span className="text-xs text-muted-foreground">
                            {new Date(msg.timestamp).toLocaleTimeString()}
                          </span>
                        )}
                        {isLong && (
                          isExpanded ? (
                            <ChevronDown className="h-4 w-4 text-muted-foreground" />
                          ) : (
                            <ChevronRight className="h-4 w-4 text-muted-foreground" />
                          )
                        )}
                      </div>
                    </div>
                  </CardHeader>
                  <CardContent className="py-2 px-3">
                    <p className={cn(
                      'text-sm text-foreground whitespace-pre-wrap',
                      !isExpanded && isLong && 'line-clamp-3'
                    )}>
                      {content}
                    </p>
                    {msg.files_touched && msg.files_touched.length > 0 && (
                      <div className="mt-2 flex items-center gap-1 flex-wrap">
                        <FileText className="h-3 w-3 text-muted-foreground" />
                        {msg.files_touched.slice(0, 5).map((file, i) => (
                          <Badge key={i} variant="secondary" className="text-xs font-mono">
                            {file.split('/').pop()}
                          </Badge>
                        ))}
                        {msg.files_touched.length > 5 && (
                          <Badge variant="secondary" className="text-xs">
                            +{msg.files_touched.length - 5}
                          </Badge>
                        )}
                      </div>
                    )}
                  </CardContent>
                </Card>
              )
            })
          )}
        </div>
      </ScrollArea>
    </div>
  )
}