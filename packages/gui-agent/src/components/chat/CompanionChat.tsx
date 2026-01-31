import { useEffect, useRef, useCallback, useState } from 'react'
import { ScrollArea } from '@/components/ui/scroll-area'
import { Button } from '@/components/ui/button'
import { Badge } from '@/components/ui/badge'
import { useChatStore } from '@/stores/chatStore'
import { useActivityStore } from '@/stores/activityStore'
import { ChatInput } from './ChatInput'
import { MessageBubble, TypingIndicator } from './MessageBubble'
import {
  createConsoleSession,
  getConsoleSession,
  askConsoleSession,
  cancelConsoleSession,
  listConsoleSessions,
  type ConsoleMessage,
  type ConsoleSession,
} from '@/api/client'
import { Plus, History, Zap, Bot, MessageSquare, Folder, Copy, Check, ChevronDown, Clock, Cpu, Wrench } from 'lucide-react'
import { COMPANION_TOOL_MODELS, COMPANION_RESPONSE_MODELS } from '@/components/agents/spawnFormConstants'

const API_BASE = '/api'

/**
 * Renders the Companion chat UI for managing and interacting with a companion agent session.
 *
 * The component initializes or resumes a console session (with localStorage persistence), subscribes to server-sent events to stream assistant responses, and provides UI for sending messages, cancelling inflight responses, creating or switching sessions, viewing session metadata, and selecting tool/response models.
 *
 * @returns The React element tree for the Companion chat interface.
 */
export function CompanionChat() {
  const {
    sessionId,
    session,
    messages,
    inflight,
    persistedSessionId,
    sourceAgent,
    isInitializing,
    setSessionId,
    setSession,
    setMessages,
    addMessage,
    appendToLastMessage,
    setInflight,
  } = useChatStore()

  const activityCount = useActivityStore((s) => s.events.length)
  const scrollRef = useRef<HTMLDivElement>(null)
  const eventSourceRef = useRef<EventSource | null>(null)
  const hasInitializedRef = useRef(false)

  // Companion 2-stage model configuration (must be declared before useEffect that uses them)
  const [toolModel, setToolModel] = useState(COMPANION_TOOL_MODELS[0]?.id || '')
  const [responseModel, setResponseModel] = useState(COMPANION_RESPONSE_MODELS[0]?.id || '')

  // Auto-scroll to bottom on new messages
  useEffect(() => {
    if (scrollRef.current) {
      scrollRef.current.scrollTop = scrollRef.current.scrollHeight
    }
  }, [messages, inflight])

  // Load or create session on mount
  useEffect(() => {
    const initSession = async () => {
      console.log('[CompanionChat] initSession called, isInitializing:', isInitializing, 'hasInitialized:', hasInitializedRef.current, 'sessionId:', sessionId, 'messages.length:', messages.length)

      // Skip if AgentDetailView is initializing a session
      if (isInitializing) {
        console.log('[CompanionChat] AgentDetailView is initializing, skipping auto-init')
        return
      }

      // If we already have a session with messages loaded, don't overwrite
      if (sessionId && messages.length > 0) {
        console.log('[CompanionChat] Session already loaded with messages, skipping init')
        hasInitializedRef.current = true
        return
      }

      // If we already initialized this session, don't do it again
      if (hasInitializedRef.current && sessionId) {
        console.log('[CompanionChat] Already initialized this session, skipping')
        return
      }

      hasInitializedRef.current = true

      // Try to load existing session from localStorage
      const savedSessionId = localStorage.getItem('gui-agent-session-id')
      console.log('[CompanionChat] savedSessionId from localStorage:', savedSessionId)
      if (savedSessionId) {
        // Don't load if we already have this session ID set (AgentDetailView already set it)
        if (sessionId === savedSessionId) {
          console.log('[CompanionChat] Session ID already matches localStorage, skipping load')
          return
        }
        try {
          const data = await getConsoleSession(savedSessionId)
          console.log('[CompanionChat] Loaded session from localStorage, messages:', data.messages.length)
          setSessionId(savedSessionId)
          setSession(data.session)
          setMessages(data.messages)
          setInflight(data.inflight)
          return
        } catch {
          // Session expired or invalid, create new one
          console.log('[CompanionChat] Failed to load session from localStorage, creating new')
          localStorage.removeItem('gui-agent-session-id')
        }
      }

      // Create new session
      try {
        const data = await createConsoleSession({
          workspace: window.location.pathname,
          profile: 'companion',
          tool_model: toolModel,
          response_model: responseModel,
        })
        const newSessionId = data.session.id
        console.log('[CompanionChat] Created new session:', newSessionId)
        setSessionId(newSessionId)
        setSession(data.session)
        setMessages([])
        localStorage.setItem('gui-agent-session-id', newSessionId)
      } catch (err) {
        console.error('Failed to create session:', err)
      }
    }

    initSession()
  }, [setSessionId, setSession, setMessages, setInflight, sessionId, isInitializing, messages.length, toolModel, responseModel])



  const handleSSEMessage = useCallback(
    (event: { type: string; data?: unknown }) => {
      switch (event.type) {
        case 'start':
          // Assistant starting to respond
          setInflight(true)
          addMessage({
            role: 'assistant',
            content: '',
            timestamp: new Date().toISOString(),
          })
          break
        case 'chunk': {
          const payload = event.data
          const content =
            typeof payload === 'object' && payload !== null
              ? (payload as { content?: string; data?: { content?: string } }).content ??
                (payload as { data?: { content?: string } }).data?.content
              : undefined
          if (content) {
            appendToLastMessage(content)
          }
          break
        }
        case 'done':
        case 'error':
          setInflight(false)
          break
        case 'tool_call':
          // Tool call received - could update UI to show tool activity
          break
      }
    },
    [addMessage, appendToLastMessage, setInflight]
  )

  // Subscribe to session events via SSE
  useEffect(() => {
    if (!sessionId) return

    const eventSource = new EventSource(
      `${API_BASE}/console/sessions/${sessionId}/events`
    )
    eventSourceRef.current = eventSource

    eventSource.addEventListener('connected', () => {
      console.log('SSE connected to session', sessionId)
    })

    eventSource.addEventListener('message', (event) => {
      try {
        handleSSEMessage(JSON.parse(event.data))
      } catch (e) {
        console.error('Failed to parse SSE message:', e)
      }
    })

    eventSource.addEventListener('chunk', (event) => {
      try {
        handleSSEMessage({ type: 'chunk', data: JSON.parse(event.data) })
      } catch (e) {
        console.error('Failed to parse SSE chunk:', e)
      }
    })

    eventSource.addEventListener('done', () => {
      handleSSEMessage({ type: 'done', data: undefined })
    })

    eventSource.addEventListener('error', () => {
      handleSSEMessage({ type: 'error', data: undefined })
    })

    eventSource.onerror = () => {
      console.error('SSE connection error')
      // Reconnect logic could go here
    }

    return () => {
      eventSource.close()
      eventSourceRef.current = null
    }
  }, [sessionId, handleSSEMessage])

  const handleSend = async (content: string) => {
    if (!sessionId) return

    // Add user message immediately
    const userMessage: ConsoleMessage = {
      role: 'user',
      content,
      timestamp: new Date().toISOString(),
    }
    addMessage(userMessage)
    setInflight(true)

    try {
      await askConsoleSession(sessionId, content, undefined, {
        tool_model: toolModel,
        response_model: responseModel,
      })
      // Response will come via SSE
    } catch (err) {
      console.error('Failed to send message:', err)
      setInflight(false)
      // Add error message
      addMessage({
        role: 'assistant',
        content: `Error: Failed to send message. ${err instanceof Error ? err.message : ''}`,
        timestamp: new Date().toISOString(),
      })
    }
  }

  const handleCancel = async () => {
    if (!sessionId) return
    try {
      await cancelConsoleSession(sessionId)
      setInflight(false)
    } catch (err) {
      console.error('Failed to cancel:', err)
    }
  }

  const handleNewSession = async () => {
    try {
      const data = await createConsoleSession({
        workspace: window.location.pathname,
        profile: 'companion',
        tool_model: toolModel,
        response_model: responseModel,
      })
      const newSessionId = data.session.id
      setSessionId(newSessionId)
      setSession(data.session)
      setMessages([])
      localStorage.setItem('gui-agent-session-id', newSessionId)
    } catch (err) {
      console.error('Failed to create new session:', err)
    }
  }

  const [showSessionInfo, setShowSessionInfo] = useState(false)
  const [showSessionPicker, setShowSessionPicker] = useState(false)
  const [availableSessions, setAvailableSessions] = useState<ConsoleSession[]>([])
  const [copiedId, setCopiedId] = useState(false)

  // Load available sessions when picker is opened
  const loadSessions = async () => {
    try {
      const data = await listConsoleSessions()
      setAvailableSessions(data.sessions)
    } catch (err) {
      console.error('Failed to load sessions:', err)
    }
  }

  const handleSessionSelect = async (selectedSessionId: string) => {
    if (selectedSessionId === sessionId) {
      setShowSessionPicker(false)
      return
    }

    try {
      const data = await getConsoleSession(selectedSessionId)
      setSessionId(selectedSessionId)
      setSession(data.session)
      setMessages(data.messages)
      setInflight(!!data.inflight)
      localStorage.setItem('gui-agent-session-id', selectedSessionId)
      setShowSessionPicker(false)
    } catch (err) {
      console.error('Failed to switch session:', err)
    }
  }

  const formatSessionTime = (dateStr: string) => {
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

  const handleCopySessionId = () => {
    if (sessionId) {
      navigator.clipboard.writeText(sessionId)
      setCopiedId(true)
      setTimeout(() => setCopiedId(false), 2000)
    }
  }

  const getWorkspaceDisplayName = (workspace: string) => {
    if (!workspace || workspace === '/') return 'root'
    const parts = workspace.split('/')
    return parts[parts.length - 1] || workspace
  }

  return (
    <div className="flex flex-col h-full">
      {/* Header */}
      <div className="border-b border-border">
        <div className="h-12 flex items-center justify-between px-4">
          <div className="flex items-center gap-2">
            <div className="flex items-center gap-2">
              <Bot className="h-5 w-5 text-primary" />
              <h2 className="text-sm font-semibold text-foreground">
                {sourceAgent?.name || sourceAgent?.role ? `Companion • ${sourceAgent?.name || sourceAgent?.role}` : 'Companion'}
              </h2>
            </div>
            {session && (
              <>
                <Badge variant="outline" className="text-xs">
                  {session.profile}
                </Badge>
                <Badge variant="secondary" className="text-xs">
                  <MessageSquare className="h-3 w-3 mr-1" />
                  {messages.length}
                </Badge>
              </>
            )}
          </div>
          <div className="flex items-center gap-1">
            {activityCount > 0 && (
              <Badge variant="secondary" className="text-xs">
                <Zap className="h-3 w-3 mr-1" />
                {activityCount}
              </Badge>
            )}
            <div className="relative">
              <Button
                variant="ghost"
                size="sm"
                className="h-7 text-xs gap-1"
                title="Switch session"
                onClick={() => {
                  loadSessions()
                  setShowSessionPicker(!showSessionPicker)
                }}
              >
                <History className="h-3.5 w-3.5" />
                <ChevronDown className="h-3 w-3" />
              </Button>

              {/* Session picker dropdown */}
              {showSessionPicker && (
                <div className="absolute right-0 top-full mt-1 w-64 bg-popover border border-border rounded-md shadow-lg z-50 overflow-hidden">
                  <div className="p-2 border-b border-border flex items-center justify-between">
                    <span className="text-xs font-medium text-muted-foreground">Sessions</span>
                    <Button
                      variant="ghost"
                      size="sm"
                      className="h-6 text-xs"
                      onClick={() => {
                        handleNewSession()
                        setShowSessionPicker(false)
                      }}
                    >
                      <Plus className="h-3 w-3 mr-1" />
                      New
                    </Button>
                  </div>
                  <div className="max-h-60 overflow-y-auto">
                    {availableSessions.length === 0 ? (
                      <div className="p-3 text-xs text-muted-foreground text-center">
                        No sessions found
                      </div>
                    ) : (
                      availableSessions.map((s) => (
                        <button
                          key={s.id}
                          onClick={() => handleSessionSelect(s.id)}
                          className={`w-full px-3 py-2 text-left hover:bg-accent/50 flex items-center justify-between gap-2 ${
                            s.id === sessionId ? 'bg-accent' : ''
                          }`}
                        >
                          <div className="min-w-0 flex-1">
                            <div className="text-xs font-mono text-foreground truncate">
                              {s.id.slice(0, 12)}...
                            </div>
                            <div className="text-xs text-muted-foreground flex items-center gap-2 mt-0.5">
                              <span className="flex items-center gap-1">
                                <MessageSquare className="h-3 w-3" />
                                {s.message_count}
                              </span>
                              <span className="flex items-center gap-1">
                                <Clock className="h-3 w-3" />
                                {formatSessionTime(s.created)}
                              </span>
                            </div>
                          </div>
                          {s.id === sessionId && (
                            <Badge variant="secondary" className="text-xs shrink-0">
                              current
                            </Badge>
                          )}
                        </button>
                      ))
                    )}
                  </div>
                </div>
              )}
            </div>
            <Button
              variant="ghost"
              size="icon"
              className="h-7 w-7"
              title="Session details"
              onClick={() => setShowSessionInfo(!showSessionInfo)}
            >
              <Folder className="h-4 w-4" />
            </Button>
          </div>
        </div>

        {/* Expandable session info */}
        {showSessionInfo && session && (
          <div className="px-4 py-2 bg-muted/30 text-xs space-y-1.5 border-t border-border/50">
            <div className="flex items-center justify-between">
              <span className="text-muted-foreground">Session ID:</span>
              <div className="flex items-center gap-1">
                <code className="font-mono text-foreground">{sessionId?.slice(0, 16)}...</code>
                <Button
                  variant="ghost"
                  size="icon"
                  className="h-5 w-5"
                  onClick={handleCopySessionId}
                  title="Copy session ID"
                >
                  {copiedId ? (
                    <Check className="h-3 w-3 text-green-500" />
                  ) : (
                    <Copy className="h-3 w-3" />
                  )}
                </Button>
              </div>
            </div>
            <div className="flex items-center justify-between">
              <span className="text-muted-foreground">Workspace:</span>
              <span className="flex items-center gap-1 text-foreground">
                <Folder className="h-3 w-3" />
                {getWorkspaceDisplayName(session.workspace)}
              </span>
            </div>
            <div className="flex items-center justify-between">
              <span className="text-muted-foreground">Created:</span>
              <span className="text-foreground">
                {new Date(session.created).toLocaleString()}
              </span>
            </div>
            {session.last_activity && (
              <div className="flex items-center justify-between">
                <span className="text-muted-foreground">Last activity:</span>
                <span className="text-foreground">
                  {new Date(session.last_activity).toLocaleString()}
                </span>
              </div>
            )}
            {persistedSessionId && (
              <div className="flex items-center justify-between">
                <span className="text-muted-foreground">Source Session:</span>
                <code className="font-mono text-foreground text-xs">{persistedSessionId.slice(0, 16)}...</code>
              </div>
            )}

            {/* 2-Stage Model Configuration */}
            <div className="pt-2 mt-2 border-t border-border/50 space-y-2">
              <div className="text-xs font-medium text-muted-foreground">Model Configuration</div>

              {/* Tool Model Selector */}
              <div className="flex items-center justify-between gap-2">
                <span className="text-muted-foreground flex items-center gap-1">
                  <Wrench className="h-3 w-3" />
                  Tool Model:
                </span>
                <select
                  value={toolModel}
                  onChange={(e) => setToolModel(e.target.value)}
                  className="text-xs bg-background border border-border rounded px-2 py-1 font-mono focus:outline-none focus:ring-1 focus:ring-ring max-w-[140px]"
                >
                  {COMPANION_TOOL_MODELS.map((model) => (
                    <option key={model.id} value={model.id}>
                      {model.name}
                    </option>
                  ))}
                </select>
              </div>

              {/* Response Model Selector */}
              <div className="flex items-center justify-between gap-2">
                <span className="text-muted-foreground flex items-center gap-1">
                  <Cpu className="h-3 w-3" />
                  Response Model:
                </span>
                <select
                  value={responseModel}
                  onChange={(e) => setResponseModel(e.target.value)}
                  className="text-xs bg-background border border-border rounded px-2 py-1 font-mono focus:outline-none focus:ring-1 focus:ring-ring max-w-[140px]"
                >
                  {COMPANION_RESPONSE_MODELS.map((model) => (
                    <option key={model.id} value={model.id}>
                      {model.name}
                    </option>
                  ))}
                </select>
              </div>
            </div>
          </div>
        )}
      </div>

      {/* Messages */}
      <ScrollArea className="flex-1" ref={scrollRef}>
        <div className="py-4">
          {messages.length === 0 ? (
            <div className="text-center py-12 px-4">
              <div className="text-4xl mb-4">👋</div>
              <h3 className="text-lg font-medium text-foreground mb-2">
                Welcome to the Agent Operations Center
              </h3>
              <p className="text-sm text-muted-foreground max-w-md mx-auto">
                I'm your companion assistant. Ask me anything about the codebase,
                run skills, or manage agents. Type / to see available commands.
              </p>
            </div>
          ) : (
            messages.map((msg, idx) => (
              <MessageBubble
                key={`${msg.timestamp}-${idx}`}
                message={msg}
                showTimestamp={
                  idx === messages.length - 1 ||
                  messages[idx + 1]?.role !== msg.role
                }
              />
            ))
          )}
          {inflight && messages[messages.length - 1]?.role === 'user' && (
            <TypingIndicator />
          )}
        </div>
      </ScrollArea>

      {/* Input */}
      <ChatInput
        onSend={handleSend}
        onCancel={handleCancel}
        inflight={inflight}
        disabled={!sessionId}
      />
    </div>
  )
}