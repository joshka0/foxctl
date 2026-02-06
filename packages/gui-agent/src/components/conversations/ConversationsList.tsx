import React, { useState, useEffect, useRef, useCallback, useMemo } from 'react'
import { useQuery } from '@tanstack/react-query'
import { ScrollArea } from '@/components/ui/scroll-area'
import { Button } from '@/components/ui/button'
import { Badge } from '@/components/ui/badge'
import { Card } from '@/components/ui/card'
import { Input } from '@/components/ui/input'
import { cn, formatRelativeTime } from '@/lib/utils'
import {
  listCompanionConversations,
  getCompanionConversationMessages,
  getCompanionPersonality,
  updatePersonalityDimension,
  createConsoleSession,
  getConsoleSession,
  askConsoleSession,
  cancelConsoleSession,
  companionChat,
  listAgents,
  deleteCompanionConversation,
  deleteCompanionMessage,
  renameCompanionConversation,
  patchAgent,
  type ConsoleMessage,
  type ConsoleSession,
  type PersonalityInfo,
} from '@/api/client'
import type { Agent } from '@/api/types'
import { PROVIDERS, getModelsForProvider, COMPANION_TOOL_MODELS, COMPANION_RESPONSE_MODELS } from '@/components/agents/spawnFormConstants'
import { ChatInput } from '@/components/chat/ChatInput'
import { MessageBubble, TypingIndicator } from '@/components/chat/MessageBubble'
import {
  MessageCircle,
  RefreshCw,
  Search,
  Hash,
  MessagesSquare,
  Plus,
  Bot,
  PanelRightOpen,
  PanelRightClose,
  Cpu,
  Folder,
  Clock,
  FileText,
  Settings2,
  X,
  Sparkles,
  Trash2,
  Pencil,
  Check,
  Coins,
  Sliders,
  Save,
  RotateCcw,
  Wrench,
  ChevronDown,
  ChevronRight,
  User,
} from 'lucide-react'
import { Textarea } from '@/components/ui/textarea'
import { Slider } from '@/components/ui/slider'

const API_BASE = '/api'

interface Conversation {
  id: string
  title?: string
  name?: string  // Custom title from database
  created_at: string
  updated_at: string
  message_count: number
}

interface ToolCallInfo {
  name: string
  args?: string
  result?: string
  injectedContext?: string // Context injected by hooks after tool execution
}

interface ContextInfo {
  systemPrompt?: string
  workspace?: string
  profile?: string
  createdAt?: string
  lastActivity?: string
  toolCalls?: ToolCallInfo[]
  injectedContexts?: Array<{ source: string; content: string; toolName?: string }>
}

export function ConversationsList() {
  const [searchQuery, setSearchQuery] = useState('')
  const [selectedConversation, setSelectedConversation] = useState<Conversation | null>(null)
  const [messages, setMessages] = useState<ConsoleMessage[]>([])
  const [inflight, setInflight] = useState(false)
  const [sessionId, setSessionId] = useState<string | null>(null)
  const [session, setSession] = useState<ConsoleSession | null>(null)
  const [isLoadingMessages, setIsLoadingMessages] = useState(false)
  const [linkedAgent, setLinkedAgent] = useState<Agent | null>(null)
  const [showContextPanel, setShowContextPanel] = useState(false)
  const [contextInfo, setContextInfo] = useState<ContextInfo>({})
  const [personalityInfo, setPersonalityInfo] = useState<PersonalityInfo | null>(null)
  const [selectedMessage, setSelectedMessage] = useState<ConsoleMessage | null>(null)
  const [editingConversationId, setEditingConversationId] = useState<string | null>(null)
  const [editTitle, setEditTitle] = useState('')
  const [editLinkedAgentId, setEditLinkedAgentId] = useState<string>('')
  const [selectedAgentForNew, setSelectedAgentForNew] = useState<string>('')
  const [expandedAgents, setExpandedAgents] = useState<Set<string>>(new Set())
  const [showOrphanConversations, setShowOrphanConversations] = useState(true)
  // Model HUD state
  const [editingSystemPrompt, setEditingSystemPrompt] = useState(false)
  // Debounce timer refs for personality sliders
  const personalityDebounceRefs = useRef<Map<string, ReturnType<typeof setTimeout>>>(new Map())
  const [systemPromptDraft, setSystemPromptDraft] = useState('')
  const [selectedProvider, setSelectedProvider] = useState('')
  const [selectedModel, setSelectedModel] = useState('')
  // Companion 2-stage model configuration
  const [toolModel, setToolModel] = useState(COMPANION_TOOL_MODELS[0]?.id || '')
  const [responseModel, setResponseModel] = useState(COMPANION_RESPONSE_MODELS[0]?.id || '')
  const [maxHistoryTurns, setMaxHistoryTurns] = useState(50)

  const scrollRef = useRef<HTMLDivElement>(null)
  const eventSourceRef = useRef<EventSource | null>(null)

  // Fetch conversations
  const { data, isLoading, refetch, isFetching } = useQuery({
    queryKey: ['companion-conversations'],
    queryFn: () => listCompanionConversations(100),
    refetchInterval: 30000,
  })

  // Fetch agents to find linked agent
  const { data: agentsData, refetch: refetchAgents } = useQuery({
    queryKey: ['agents'],
    queryFn: () => listAgents(100),
    staleTime: 60000,
  })

  const conversations = useMemo(() => data?.conversations ?? [], [data?.conversations])
  const agents = useMemo(() => agentsData?.agents ?? [], [agentsData?.agents])

  const filteredConversations = searchQuery
    ? conversations.filter(
        (c) =>
          c.id.toLowerCase().includes(searchQuery.toLowerCase()) ||
          c.title?.toLowerCase().includes(searchQuery.toLowerCase())
      )
    : conversations

  // Group conversations by agent
  const groupedConversations = React.useMemo(() => {
    const agentGroups: Map<string, { agent: Agent; conversations: Conversation[] }> = new Map()
    const orphanConversations: Conversation[] = []

    filteredConversations.forEach((conv) => {
      const linkedAgent = agents.find(
        (a) => a.conversation_id === conv.id || a.id === conv.id
      )
      if (linkedAgent) {
        const key = linkedAgent.id
        if (!agentGroups.has(key)) {
          agentGroups.set(key, { agent: linkedAgent, conversations: [] })
        }
        agentGroups.get(key)!.conversations.push(conv)
      } else {
        orphanConversations.push(conv)
      }
    })

    return {
      agentGroups: Array.from(agentGroups.values()),
      orphanConversations,
    }
  }, [filteredConversations, agents])

  // Auto-scroll to bottom on new messages
  useEffect(() => {
    if (scrollRef.current) {
      scrollRef.current.scrollTop = scrollRef.current.scrollHeight
    }
  }, [messages, inflight])

  // Find linked agent when conversation changes
  useEffect(() => {
    if (!selectedConversation) {
      setLinkedAgent(null)
      return
    }

    // Check if any agent has this conversation_id linked
    const agent = agents.find(
      (a) => a.conversation_id === selectedConversation.id || a.id === selectedConversation.id
    )
    setLinkedAgent(agent || null)
  }, [selectedConversation, agents])

  // Auto-select conversation when navigating from AgentDetailView
  useEffect(() => {
    const autoSelectId = localStorage.getItem('gui-agent-auto-select-conversation')
    if (autoSelectId && conversations.length > 0 && !selectedConversation) {
      const conversation = conversations.find((c) => c.id === autoSelectId)
      if (conversation) {
        // Clear the flag first to prevent re-selecting
        localStorage.removeItem('gui-agent-auto-select-conversation')
        // Select the conversation
        handleSelectConversation(conversation)
      } else {
        // Conversation not found, clear the flag
        localStorage.removeItem('gui-agent-auto-select-conversation')
      }
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [conversations, selectedConversation])

  // Handle selecting a conversation
  const handleSelectConversation = async (conversation: Conversation) => {
    if (selectedConversation?.id === conversation.id) return

    // Close existing SSE connection
    if (eventSourceRef.current) {
      eventSourceRef.current.close()
      eventSourceRef.current = null
    }

    setSelectedConversation(conversation)
    setIsLoadingMessages(true)
    setMessages([])
    setSessionId(null)
    setSession(null)
    setContextInfo({})
    setPersonalityInfo(null)
    setSelectedMessage(null)

    try {
      // Load messages for this conversation from companion memory
      const messagesData = await getCompanionConversationMessages(conversation.id, 200)
      const consoleMessages: ConsoleMessage[] = messagesData.messages.map((msg) => ({
        id: msg.id,
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
      setMessages(consoleMessages)

      // Check if this conversation is linked to an agent
      const isAgentConversation = agents.some(
        (a) => a.conversation_id === conversation.id || a.id === conversation.id
      )

      if (isAgentConversation) {
        // For agent-linked conversations, use companion chat directly
        // No console session needed - messages go to companion memory
        const agent = agents.find(
          (a) => a.conversation_id === conversation.id || a.id === conversation.id
        )
        setContextInfo({
          workspace: agent?.ns || '/',
          profile: 'agent',
          createdAt: conversation.created_at,
        })
      } else {
        // For regular conversations, create a console session for SSE
        const sessionData = await createConsoleSession({
          workspace: '/',
          profile: 'companion',
          conversation_id: conversation.id,
        })
        setSessionId(sessionData.session.id)
        setSession(sessionData.session)
        setContextInfo({
          workspace: sessionData.session.workspace,
          profile: sessionData.session.profile,
          createdAt: sessionData.session.created,
          lastActivity: sessionData.session.last_activity,
        })
      }

      // Fetch personality info for this conversation
      try {
        const personality = await getCompanionPersonality(conversation.id)
        setPersonalityInfo(personality)
        // Also set the system prompt in contextInfo for display
        setContextInfo((prev) => ({
          ...prev,
          systemPrompt: personality.system_prompt,
        }))
      } catch (personalityErr) {
        console.warn('Failed to load personality info:', personalityErr)
      }
    } catch (err) {
      console.error('Failed to load conversation:', err)
    } finally {
      setIsLoadingMessages(false)
    }
  }

  // Handle creating a new conversation
  const handleNewConversation = async () => {
    // Close existing SSE connection
    if (eventSourceRef.current) {
      eventSourceRef.current.close()
      eventSourceRef.current = null
    }

    setSelectedConversation(null)
    setMessages([])
    setIsLoadingMessages(true)
    setSessionId(null)
    setSession(null)
    setLinkedAgent(null)

    try {
      // Create a new console session
      const sessionData = await createConsoleSession({
        workspace: '/',
        profile: 'companion',
        tool_model: toolModel,
        response_model: responseModel,
      })
      setSessionId(sessionData.session.id)
      setSession(sessionData.session)

      // Create a placeholder conversation for the UI
      const newConv: Conversation = {
        id: sessionData.session.id,
        title: 'New Conversation',
        created_at: new Date().toISOString(),
        updated_at: new Date().toISOString(),
        message_count: 0,
      }
      setSelectedConversation(newConv)
      setContextInfo({
        workspace: sessionData.session.workspace,
        profile: sessionData.session.profile,
        createdAt: sessionData.session.created,
      })
    } catch (err) {
      console.error('Failed to create new conversation:', err)
    } finally {
      setIsLoadingMessages(false)
    }
  }

  // Handle creating a new conversation linked to an agent
  const handleNewConversationWithAgent = async (agent: Agent) => {
    // Close existing SSE connection
    if (eventSourceRef.current) {
      eventSourceRef.current.close()
      eventSourceRef.current = null
    }

    setSelectedConversation(null)
    setMessages([])
    setIsLoadingMessages(true)
    setSessionId(null)
    setSession(null)
    setLinkedAgent(agent)

    try {
      // Create a new console session for this agent
      const sessionData = await createConsoleSession({
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

Help the user understand and interact with this agent's work.`,
        tool_model: toolModel,
        response_model: responseModel,
      })
      setSessionId(sessionData.session.id)
      setSession(sessionData.session)

      // Create a placeholder conversation for the UI
      const newConv: Conversation = {
        id: sessionData.session.id,
        title: `Chat with ${agent.name || agent.role || 'Agent'}`,
        created_at: new Date().toISOString(),
        updated_at: new Date().toISOString(),
        message_count: 0,
      }
      setSelectedConversation(newConv)
      setContextInfo({
        workspace: sessionData.session.workspace,
        profile: sessionData.session.profile,
        createdAt: sessionData.session.created,
      })

      // Link the conversation to the agent in the backend
      try {
        await patchAgent(agent.id, { conversation_id: sessionData.session.id })
        // Refetch agents so grouping updates
        refetchAgents()
      } catch (linkErr) {
        console.warn('Failed to link conversation to agent:', linkErr)
      }

      // Reset agent selector
      setSelectedAgentForNew('')
    } catch (err) {
      console.error('Failed to create new conversation with agent:', err)
    } finally {
      setIsLoadingMessages(false)
    }
  }

  // Subscribe to session events via SSE
  useEffect(() => {
    if (!sessionId) return

    const eventSource = new EventSource(
      `${API_BASE}/console/sessions/${sessionId}/events`
    )
    eventSourceRef.current = eventSource

    eventSource.addEventListener('message', (event) => {
      handleSSEMessage(JSON.parse(event.data))
    })

    eventSource.addEventListener('chunk', (event) => {
      const data = JSON.parse(event.data)
      if (data.data?.content) {
        setMessages((prev) => {
          const updated = [...prev]
          if (updated.length > 0) {
            const last = updated[updated.length - 1]
            if (last.role === 'assistant') {
              updated[updated.length - 1] = {
                ...last,
                content: last.content + data.data.content,
              }
            }
          }
          return updated
        })
      }
    })

    eventSource.addEventListener('done', () => {
      setInflight(false)
    })

    eventSource.addEventListener('error', () => {
      setInflight(false)
    })

    return () => {
      eventSource.close()
      eventSourceRef.current = null
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [sessionId])

  const handleSSEMessage = useCallback((event: { type: string; data: unknown }) => {
    switch (event.type) {
      case 'start':
        setInflight(true)
        setMessages((prev) => [
          ...prev,
          {
            role: 'assistant',
            content: '',
            timestamp: new Date().toISOString(),
          },
        ])
        break
      case 'chunk':
        if (typeof event.data === 'object' && event.data !== null) {
          const content = (event.data as { content?: string }).content
          if (content) {
            setMessages((prev) => {
              const updated = [...prev]
              if (updated.length > 0) {
                const last = updated[updated.length - 1]
                if (last.role === 'assistant') {
                  updated[updated.length - 1] = {
                    ...last,
                    content: last.content + content,
                  }
                }
              }
              return updated
            })
          }
        }
        break
      case 'done':
        setInflight(false)
        refetch()
        if (sessionId) {
          void (async () => {
            try {
              const data = await getConsoleSession(sessionId)
              setMessages(data.messages)
              setSession(data.session)
            } catch (err) {
              console.error('Failed to refresh console session:', err)
            }
          })()
        }
        break
      case 'tool_call':
      case 'event':
        // Track tool calls and results in context
        // The actual tool data is in event.data.metadata (Payload structure from backend)
        if (typeof event.data === 'object' && event.data !== null) {
          const payload = event.data as {
            type?: string
            content?: string
            metadata?: {
              tool?: string
              tool_id?: string
              arguments?: unknown
              phase?: 'call' | 'result'
              partial?: boolean
            }
          }
          const toolData = payload.metadata
          const toolName = toolData?.tool

          if (toolName && toolData?.phase === 'call') {
            // Tool call phase
            const argsStr = toolData.arguments
              ? typeof toolData.arguments === 'string'
                ? toolData.arguments
                : JSON.stringify(toolData.arguments)
              : undefined
            setContextInfo((prev) => ({
              ...prev,
              toolCalls: [
                ...(prev.toolCalls || []),
                { name: toolName, args: argsStr },
              ],
            }))
          } else if (toolName && toolData?.phase === 'result') {
            // Tool result phase - check for injected context
            // The result content is in payload.content (broadcast as display text)
            const resultContent = payload.content || ''

            // Parse injected context (appears after "---\n" separator)
            const separatorIndex = resultContent.indexOf('\n\n---\n')
            if (separatorIndex !== -1) {
              const injectedContent = resultContent.slice(separatorIndex + 5) // Skip "\n\n---\n"
              if (injectedContent.trim()) {
                setContextInfo((prev) => ({
                  ...prev,
                  injectedContexts: [
                    ...(prev.injectedContexts || []),
                    {
                      source: `PostToolUse:${toolName}`,
                      content: injectedContent,
                      toolName,
                    },
                  ],
                }))
              }
            }

            // Update the last matching tool call with its result
            setContextInfo((prev) => {
              const toolCalls = prev.toolCalls ? [...prev.toolCalls] : []
              // Find the last tool call with matching name that doesn't have a result
              for (let i = toolCalls.length - 1; i >= 0; i--) {
                if (toolCalls[i].name === toolName && !toolCalls[i].result) {
                  const actualResult = separatorIndex !== -1
                    ? resultContent.slice(0, separatorIndex)
                    : resultContent
                  toolCalls[i] = { ...toolCalls[i], result: actualResult.slice(0, 500) }
                  break
                }
              }
              return { ...prev, toolCalls }
            })
          }
        }
        break
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [refetch])

  const handleSend = async (content: string) => {
    // Need either a session (console mode) or a conversation (companion mode)
    if (!sessionId && !selectedConversation) return

    const userMessage: ConsoleMessage = {
      role: 'user',
      content,
      timestamp: new Date().toISOString(),
    }
    setMessages((prev) => [...prev, userMessage])
    setInflight(true)

    try {
      // For existing companion conversations, use companion chat
      // This persists messages to companion memory
      if (selectedConversation) {
        const response = await companionChat({
          conversation_id: selectedConversation.id,
          message: content,
          workspace: linkedAgent?.ns || contextInfo.workspace || '/',
          max_history_turns: maxHistoryTurns,
        })

        // Add the response with attached tool calls (for inline display)
        const toolCallsForMessage = response.tool_calls?.map((tc) => ({
          name: tc.name,
          input: tc.arguments as Record<string, unknown> | undefined,
          output: tc.output,
          status: 'completed' as const,
        })) || []

        setMessages((prev) => [
          ...prev,
          {
            role: 'assistant',
            content: response.response,
            timestamp: new Date().toISOString(),
            tool_calls: toolCallsForMessage.length > 0 ? toolCallsForMessage : undefined,
          },
        ])

        // Also update context panel for quick reference
        if (response.tool_calls || response.injected_contexts) {
          setContextInfo((prev) => ({
            ...prev,
            toolCalls: response.tool_calls?.map((tc) => ({
              name: tc.name,
              args: tc.arguments ? JSON.stringify(tc.arguments) : undefined,
              result: tc.output,
            })) || [],
            injectedContexts: response.injected_contexts?.map((ic) => ({
              source: ic.source || 'hook',
              content: ic.content,
            })) || [],
          }))
        }

        setInflight(false)
        refetch() // Refresh conversation list
      } else if (sessionId) {
        // Use console session with SSE for new conversations only
        await askConsoleSession(sessionId, content, undefined, {
          tool_model: toolModel,
          response_model: responseModel,
        })
      }
    } catch (err) {
      console.error('Failed to send message:', err)
      setInflight(false)
      setMessages((prev) => [
        ...prev,
        {
          role: 'assistant',
          content: `Error: Failed to send message. ${err instanceof Error ? err.message : ''}`,
          timestamp: new Date().toISOString(),
        },
      ])
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


  // Calculate approximate token usage from messages
  // Uses rough estimate: ~4 chars per token
  const calculateTokenUsage = () => {
    let inputTokens = 0
    let outputTokens = 0
    messages.forEach((msg) => {
      const tokens = Math.ceil(msg.content.length / 4)
      if (msg.role === 'user') {
        inputTokens += tokens
      } else {
        outputTokens += tokens
      }
    })
    return { inputTokens, outputTokens, totalTokens: inputTokens + outputTokens }
  }

  const tokenUsage = calculateTokenUsage()

  // Handle deleting (soft delete) a conversation
  const handleDeleteConversation = async (e: React.MouseEvent, conversationId: string) => {
    e.stopPropagation() // Prevent selecting the conversation when clicking delete

    if (!window.confirm('Are you sure you want to delete this conversation? This action cannot be undone.')) {
      return
    }

    try {
      await deleteCompanionConversation(conversationId)
      // Refresh the list
      refetch()
      // If the deleted conversation was selected, clear the selection
      if (selectedConversation?.id === conversationId) {
        setSelectedConversation(null)
        setMessages([])
        setSessionId(null)
        setSession(null)
      }
    } catch (err) {
      console.error('Failed to delete conversation:', err)
    }
  }

  // Handle deleting a single message
  const handleDeleteMessage = async (message: ConsoleMessage) => {
    if (!selectedConversation || !message.id) return

    if (!window.confirm('Delete this message?')) return

    try {
      await deleteCompanionMessage(selectedConversation.id, message.id)
      // Remove from local state
      setMessages((prev) => prev.filter((m) => m.id !== message.id))
      if (selectedMessage?.id === message.id) {
        setSelectedMessage(null)
      }
    } catch (err) {
      console.error('Failed to delete message:', err)
    }
  }

  // Handle starting to edit a conversation title
  const handleStartRename = (e: React.MouseEvent, conversation: Conversation) => {
    e.stopPropagation() // Prevent selecting the conversation

    setEditingConversationId(conversation.id)
    setEditTitle(conversation.name || '')

    // Find current linked agent
    const currentAgent = agents.find(a => a.conversation_id === conversation.id)

    setEditLinkedAgentId(currentAgent?.id || '')
  }

  // Handle saving the renamed conversation
  const handleSaveRename = async (e: React.MouseEvent | React.KeyboardEvent, conversationId: string) => {
    e.stopPropagation()

    try {
      // Save title
      await renameCompanionConversation(conversationId, editTitle.trim())

      // Update agent linking
      const previousAgent = agents.find(a => a.conversation_id === conversationId)
      if (previousAgent?.id !== editLinkedAgentId) {
        // Unlink previous agent (send empty string, not null)
        if (previousAgent) {
          await patchAgent(previousAgent.id, { conversation_id: '' })
        }

        // Link new agent
        if (editLinkedAgentId) {
          await patchAgent(editLinkedAgentId, { conversation_id: conversationId })
        }

        refetchAgents()
      }

      refetch()
      setEditingConversationId(null)
      setEditTitle('')
      setEditLinkedAgentId('')
    } catch (err) {
      console.error('[handleSaveRename] Failed:', err)
    }
  }

  // Handle canceling rename
  const handleCancelRename = (e: React.MouseEvent | React.KeyboardEvent) => {
    e.stopPropagation()
    setEditingConversationId(null)
    setEditTitle('')
    setEditLinkedAgentId('')
  }

  // Toggle agent expansion
  const toggleAgentExpanded = (agentId: string) => {
    setExpandedAgents((prev) => {
      const next = new Set(prev)
      if (next.has(agentId)) {
        next.delete(agentId)
      } else {
        next.add(agentId)
      }
      return next
    })
  }

  // Auto-expand agent when its conversation is selected
  useEffect(() => {
    if (selectedConversation && linkedAgent) {
      setExpandedAgents((prev) => {
        if (!prev.has(linkedAgent.id)) {
          const next = new Set(prev)
          next.add(linkedAgent.id)
          return next
        }
        return prev
      })
    }
  }, [selectedConversation, linkedAgent])

  return (
    <div className="flex h-full">
      {/* Left Panel - Conversation List */}
      <div className="w-80 border-r border-border flex flex-col">
        {/* Header */}
        <div className="p-3 border-b border-border space-y-2">
          <div className="flex items-center justify-between">
            <div className="flex items-center gap-2">
              <MessagesSquare className="h-4 w-4" />
              <h2 className="text-sm font-semibold text-foreground">Conversations</h2>
            </div>
            <div className="flex items-center gap-1">
              <Button
                variant="ghost"
                size="icon"
                onClick={handleNewConversation}
                className="h-7 w-7"
                title="New conversation"
              >
                <Plus className="h-4 w-4" />
              </Button>
              <Button
                variant="ghost"
                size="icon"
                onClick={() => refetch()}
                disabled={isFetching}
                className="h-7 w-7"
              >
                <RefreshCw className={cn('h-4 w-4', isFetching && 'animate-spin')} />
              </Button>
            </div>
          </div>

          {/* Search */}
          <div className="relative">
            <Search className="absolute left-2 top-2 h-3.5 w-3.5 text-muted-foreground" />
            <Input
              placeholder="Search..."
              value={searchQuery}
              onChange={(e) => setSearchQuery(e.target.value)}
              className="pl-7 h-8 text-sm"
            />
          </div>
        </div>

        {/* Sidebar List - Agents with Collapsible Conversations */}
        <ScrollArea className="flex-1">
          <div className="p-2 space-y-1">
            {isLoading ? (
              <div className="text-center py-8 text-muted-foreground">
                <RefreshCw className="h-5 w-5 mx-auto mb-2 animate-spin" />
                <p className="text-xs">Loading...</p>
              </div>
            ) : agents.length === 0 && filteredConversations.length === 0 ? (
              <div className="text-center py-8 text-muted-foreground">
                <Bot className="h-8 w-8 mx-auto mb-2 opacity-40" />
                <p className="text-sm">No agents yet</p>
                <Button
                  variant="outline"
                  size="sm"
                  className="mt-2"
                  onClick={handleNewConversation}
                >
                  <Plus className="h-3 w-3 mr-1" />
                  New Chat
                </Button>
              </div>
            ) : (
              <>
                {/* Agents Section */}
                {agents.length > 0 && (
                  <div className="space-y-1">
                    {agents.map((agent) => {
                      const agentConvs = groupedConversations.agentGroups.find(g => g.agent.id === agent.id)?.conversations || []
                      const isExpanded = expandedAgents.has(agent.id)
                      const hasSelectedConv = agentConvs.some(c => c.id === selectedConversation?.id)

                      return (
                        <div key={agent.id}>
                          {/* Agent Header - Clickable to expand/collapse */}
                          <div
                            className={cn(
                              'flex items-center gap-2 px-2 py-2 rounded-lg cursor-pointer transition-colors group',
                              'hover:bg-accent/50',
                              hasSelectedConv && 'bg-accent/30'
                            )}
                            onClick={() => toggleAgentExpanded(agent.id)}
                          >
                            {/* Expand/Collapse Icon */}
                            <div className="flex-shrink-0 w-4 h-4 flex items-center justify-center">
                              {agentConvs.length > 0 ? (
                                isExpanded ? (
                                  <ChevronDown className="h-3.5 w-3.5 text-muted-foreground" />
                                ) : (
                                  <ChevronRight className="h-3.5 w-3.5 text-muted-foreground" />
                                )
                              ) : (
                                <div className="h-1.5 w-1.5 rounded-full bg-muted-foreground/30" />
                              )}
                            </div>

                            {/* Agent Avatar */}
                            <div className={cn(
                              'h-7 w-7 rounded-lg flex items-center justify-center flex-shrink-0',
                              agent.state === 'running' ? 'bg-green-500/20' : 'bg-primary/10'
                            )}>
                              <Bot className={cn(
                                'h-3.5 w-3.5',
                                agent.state === 'running' ? 'text-green-500' : 'text-primary'
                              )} />
                            </div>

                            {/* Agent Info */}
                            <div className="flex-1 min-w-0">
                              <div className="flex items-center gap-1.5">
                                <span className="text-sm font-medium truncate">
                                  {agent.name || agent.slug || agent.role || 'Agent'}
                                </span>
                                {agent.state === 'running' && (
                                  <span className="h-1.5 w-1.5 rounded-full bg-green-500 animate-pulse" />
                                )}
                              </div>
                              <div className="text-[10px] text-muted-foreground truncate">
                                {agent.role || 'agent'} · {agentConvs.length} {agentConvs.length === 1 ? 'chat' : 'chats'}
                              </div>
                            </div>

                            {/* New Chat Button */}
                            <Button
                              variant="ghost"
                              size="icon"
                              className="h-6 w-6 opacity-0 group-hover:opacity-100 transition-opacity flex-shrink-0"
                              onClick={(e) => {
                                e.stopPropagation()
                                handleNewConversationWithAgent(agent)
                              }}
                              title="New chat with this agent"
                            >
                              <Plus className="h-3.5 w-3.5" />
                            </Button>
                          </div>

                          {/* Expanded Conversations */}
                          {isExpanded && agentConvs.length > 0 && (
                            <div className="ml-6 pl-2 border-l border-border/50 space-y-0.5 mt-0.5">
                              {agentConvs.map((conversation) => (
                                <div
                                  key={conversation.id}
                                  className={cn(
                                    'flex items-center gap-2 px-2 py-1.5 rounded-md cursor-pointer transition-colors group',
                                    'hover:bg-accent/50',
                                    selectedConversation?.id === conversation.id && 'bg-accent border-l-2 border-primary -ml-0.5 pl-2.5'
                                  )}
                                  onClick={() => handleSelectConversation(conversation)}
                                >
                                  <MessageCircle className="h-3 w-3 text-muted-foreground flex-shrink-0" />
                                  <div className="flex-1 min-w-0">
                                    {editingConversationId === conversation.id ? (
                                      <div className="space-y-1" onClick={(e) => e.stopPropagation()}>
                                        <Input
                                          value={editTitle}
                                          onChange={(e) => setEditTitle(e.target.value)}
                                          onKeyDown={(e) => {
                                            if (e.key === 'Enter') handleSaveRename(e, conversation.id)
                                            if (e.key === 'Escape') handleCancelRename(e)
                                          }}
                                          className="h-5 text-xs py-0 px-1"
                                          placeholder="Title..."
                                          autoFocus
                                        />
                                        <div className="flex items-center gap-1">
                                          <select
                                            value={editLinkedAgentId}
                                            onChange={(e) => setEditLinkedAgentId(e.target.value)}
                                            className="flex-1 h-5 text-[10px] bg-muted border border-border rounded px-1"
                                          >
                                            <option value="">No agent</option>
                                            {agents.map((a) => (
                                              <option key={a.id} value={a.id}>
                                                {a.name || a.slug || a.role || a.id.slice(0, 8)}
                                              </option>
                                            ))}
                                          </select>
                                          <Button variant="ghost" size="icon" className="h-5 w-5" onClick={(e) => handleSaveRename(e, conversation.id)}>
                                            <Check className="h-3 w-3 text-green-500" />
                                          </Button>
                                          <Button variant="ghost" size="icon" className="h-5 w-5" onClick={handleCancelRename}>
                                            <X className="h-3 w-3" />
                                          </Button>
                                        </div>
                                      </div>
                                    ) : (
                                      <div className="flex items-center gap-1.5">
                                        <span className="text-xs truncate">
                                          {conversation.name || conversation.id.slice(0, 12)}
                                        </span>
                                        <Badge variant="secondary" className="text-[9px] px-1 py-0 flex-shrink-0">
                                          {conversation.message_count}
                                        </Badge>
                                      </div>
                                    )}
                                  </div>
                                  {editingConversationId !== conversation.id && (
                                    <div className="flex items-center gap-0.5 opacity-0 group-hover:opacity-100 transition-opacity flex-shrink-0">
                                      <Button variant="ghost" size="icon" className="h-5 w-5" onClick={(e) => handleStartRename(e, conversation)} title="Rename">
                                        <Pencil className="h-2.5 w-2.5 text-muted-foreground" />
                                      </Button>
                                      <Button variant="ghost" size="icon" className="h-5 w-5" onClick={(e) => handleDeleteConversation(e, conversation.id)} title="Delete">
                                        <Trash2 className="h-2.5 w-2.5 text-muted-foreground" />
                                      </Button>
                                    </div>
                                  )}
                                </div>
                              ))}
                            </div>
                          )}
                        </div>
                      )
                    })}
                  </div>
                )}

                {/* Divider if both agents and orphan conversations exist */}
                {agents.length > 0 && groupedConversations.orphanConversations.length > 0 && (
                  <div className="my-2 border-t border-border" />
                )}

                {/* Orphan Conversations - Collapsible */}
                {groupedConversations.orphanConversations.length > 0 && (
                  <div>
                    {/* Header */}
                    <div
                      className="flex items-center gap-2 px-2 py-1.5 cursor-pointer text-muted-foreground hover:text-foreground transition-colors"
                      onClick={() => setShowOrphanConversations(!showOrphanConversations)}
                    >
                      {showOrphanConversations ? (
                        <ChevronDown className="h-3.5 w-3.5" />
                      ) : (
                        <ChevronRight className="h-3.5 w-3.5" />
                      )}
                      <User className="h-3.5 w-3.5" />
                      <span className="text-xs font-medium">Unlinked Chats</span>
                      <Badge variant="outline" className="text-[9px] px-1 py-0 ml-auto">
                        {groupedConversations.orphanConversations.length}
                      </Badge>
                    </div>

                    {/* Orphan Conversations List */}
                    {showOrphanConversations && (
                      <div className="space-y-0.5 mt-0.5">
                        {groupedConversations.orphanConversations.map((conversation) => (
                          <div
                            key={conversation.id}
                            className={cn(
                              'flex items-center gap-2 px-2 py-2 rounded-md cursor-pointer transition-colors group',
                              'hover:bg-accent/50',
                              selectedConversation?.id === conversation.id && 'bg-accent border-l-2 border-primary'
                            )}
                            onClick={() => handleSelectConversation(conversation)}
                          >
                            <MessageCircle className="h-3.5 w-3.5 text-muted-foreground flex-shrink-0" />
                            <div className="flex-1 min-w-0">
                              {editingConversationId === conversation.id ? (
                                <div className="space-y-1" onClick={(e) => e.stopPropagation()}>
                                  <Input
                                    value={editTitle}
                                    onChange={(e) => setEditTitle(e.target.value)}
                                    onKeyDown={(e) => {
                                      if (e.key === 'Enter') handleSaveRename(e, conversation.id)
                                      if (e.key === 'Escape') handleCancelRename(e)
                                    }}
                                    className="h-5 text-xs py-0 px-1"
                                    placeholder="Title..."
                                    autoFocus
                                  />
                                  <div className="flex items-center gap-1">
                                    <select
                                      value={editLinkedAgentId}
                                      onChange={(e) => setEditLinkedAgentId(e.target.value)}
                                      className="flex-1 h-5 text-[10px] bg-muted border border-border rounded px-1"
                                    >
                                      <option value="">Link to agent...</option>
                                      {agents.map((a) => (
                                        <option key={a.id} value={a.id}>
                                          {a.name || a.slug || a.role || a.id.slice(0, 8)}
                                        </option>
                                      ))}
                                    </select>
                                    <Button variant="ghost" size="icon" className="h-5 w-5" onClick={(e) => handleSaveRename(e, conversation.id)}>
                                      <Check className="h-3 w-3 text-green-500" />
                                    </Button>
                                    <Button variant="ghost" size="icon" className="h-5 w-5" onClick={handleCancelRename}>
                                      <X className="h-3 w-3" />
                                    </Button>
                                  </div>
                                </div>
                              ) : (
                                <>
                                  <div className="flex items-center gap-1.5">
                                    <span className="text-xs font-medium truncate">
                                      {conversation.name || conversation.id.slice(0, 16)}
                                    </span>
                                    <Badge variant="secondary" className="text-[9px] px-1 py-0 flex-shrink-0">
                                      {conversation.message_count}
                                    </Badge>
                                  </div>
                                  <div className="text-[10px] text-muted-foreground">
                                    {formatRelativeTime(conversation.updated_at)}
                                  </div>
                                </>
                              )}
                            </div>
                            {editingConversationId !== conversation.id && (
                              <div className="flex items-center gap-0.5 opacity-0 group-hover:opacity-100 transition-opacity flex-shrink-0">
                                <Button variant="ghost" size="icon" className="h-5 w-5" onClick={(e) => handleStartRename(e, conversation)} title="Edit & Link">
                                  <Pencil className="h-3 w-3 text-muted-foreground" />
                                </Button>
                                <Button variant="ghost" size="icon" className="h-5 w-5" onClick={(e) => handleDeleteConversation(e, conversation.id)} title="Delete">
                                  <Trash2 className="h-3 w-3 text-muted-foreground" />
                                </Button>
                              </div>
                            )}
                          </div>
                        ))}
                      </div>
                    )}
                  </div>
                )}
              </>
            )}
          </div>
        </ScrollArea>
      </div>

      {/* Middle Panel - Chat */}
      <div className="flex-1 flex flex-col min-w-0">
        {selectedConversation ? (
          <>
            {/* Chat Header with Agent Info */}
            <div className="border-b border-border">
              <div className="h-12 flex items-center justify-between px-4">
                <div className="flex items-center gap-3 min-w-0">
                  {linkedAgent ? (
                    <>
                      <div className="h-8 w-8 rounded-lg bg-primary/10 flex items-center justify-center flex-shrink-0">
                        <Bot className="h-4 w-4 text-primary" />
                      </div>
                      <div className="min-w-0">
                        <div className="flex items-center gap-2">
                          <span className="text-sm font-medium truncate">
                            {linkedAgent.name || linkedAgent.slug || 'Agent'}
                          </span>
                          <Badge variant="outline" className="text-[10px]">
                            {linkedAgent.role || 'agent'}
                          </Badge>
                        </div>
                        <div className="text-[10px] text-muted-foreground flex items-center gap-2">
                          <span className="flex items-center gap-0.5">
                            <Cpu className="h-2.5 w-2.5" />
                            {linkedAgent.llm_model || 'default'}
                          </span>
                          {linkedAgent.ns && (
                            <span className="flex items-center gap-0.5">
                              <Folder className="h-2.5 w-2.5" />
                              {linkedAgent.ns.split('/').pop()}
                            </span>
                          )}
                        </div>
                      </div>
                    </>
                  ) : (
                    <>
                      <Bot className="h-4 w-4 text-primary flex-shrink-0" />
                      <div className="min-w-0">
                        <span className="text-sm font-medium truncate block">
                          {selectedConversation.title || selectedConversation.id.slice(0, 20)}
                        </span>
                        <div className="flex items-center gap-2 text-[10px] text-muted-foreground">
                          <span>{messages.length} messages</span>
                          {linkedAgent !== null && (
                            <Badge variant="secondary" className="text-[9px] bg-primary/10 text-primary">
                              <Bot className="h-2.5 w-2.5 mr-0.5" />
                              {(linkedAgent as Agent).name || (linkedAgent as Agent).role || 'Agent'}
                            </Badge>
                          )}
                        </div>
                      </div>
                    </>
                  )}
                </div>
                <div className="flex items-center gap-1">
                  <Badge variant="secondary" className="text-xs">
                    {messages.length} msgs
                  </Badge>
                  <Button
                    variant="ghost"
                    size="icon"
                    className="h-8 w-8"
                    onClick={() => setShowContextPanel(!showContextPanel)}
                    title={showContextPanel ? 'Hide context' : 'Show context'}
                  >
                    {showContextPanel ? (
                      <PanelRightClose className="h-4 w-4" />
                    ) : (
                      <PanelRightOpen className="h-4 w-4" />
                    )}
                  </Button>
                </div>
              </div>

              {/* Agent Stats Bar (if linked) */}
              {linkedAgent && (
                <div className="px-4 py-1.5 bg-muted/30 border-t border-border flex items-center gap-4 text-[10px] text-muted-foreground">
                  <span className="flex items-center gap-1">
                    <Hash className="h-2.5 w-2.5" />
                    {linkedAgent.id.slice(0, 12)}
                  </span>
                  <span className="flex items-center gap-1">
                    <Sparkles className="h-2.5 w-2.5" />
                    {linkedAgent.state}
                  </span>
                  {linkedAgent.llm_provider && (
                    <span className="flex items-center gap-1">
                      <Settings2 className="h-2.5 w-2.5" />
                      {linkedAgent.llm_provider}
                    </span>
                  )}
                </div>
              )}
            </div>

            {/* Messages */}
            <ScrollArea className="flex-1 p-4" ref={scrollRef}>
              {isLoadingMessages ? (
                <div className="flex items-center justify-center h-full">
                  <RefreshCw className="h-6 w-6 animate-spin text-muted-foreground" />
                </div>
              ) : messages.length === 0 ? (
                <div className="flex flex-col items-center justify-center h-full text-muted-foreground">
                  <Bot className="h-12 w-12 mb-3 opacity-30" />
                  <p className="text-sm">Start the conversation</p>
                </div>
              ) : (
                <div className="space-y-4">
                  {messages.map((message, index) => (
                    <MessageBubble
                      key={message.id || index}
                      message={message}
                      isSelected={selectedMessage === message}
                      onSelect={(msg) => {
                        setSelectedMessage(msg)
                        setShowContextPanel(true)
                      }}
                      onDelete={handleDeleteMessage}
                    />
                  ))}
                  {inflight && messages[messages.length - 1]?.role !== 'assistant' && (
                    <TypingIndicator />
                  )}
                </div>
              )}
            </ScrollArea>

            {/* Chat Input */}
            <div className="p-4 border-t border-border">
              <ChatInput
                onSend={handleSend}
                onCancel={handleCancel}
                disabled={!sessionId && !selectedConversation}
                inflight={inflight}
              />
            </div>
          </>
        ) : (
          /* Empty State */
          <div className="flex-1 flex flex-col items-center justify-center text-muted-foreground">
            <div className="h-20 w-20 rounded-2xl bg-muted flex items-center justify-center mb-4">
              <MessagesSquare className="h-10 w-10 opacity-40" />
            </div>
            <h3 className="text-lg font-medium text-foreground mb-1">No conversation selected</h3>
            <p className="text-sm mb-4">Select a conversation or start a new one</p>

            {/* Agent Selector for New Conversation */}
            <div className="w-64 mb-4">
              <label className="text-xs text-muted-foreground mb-1 block">Link to agent (optional)</label>
              <select
                value={selectedAgentForNew}
                onChange={(e) => setSelectedAgentForNew(e.target.value)}
                className="w-full h-9 rounded-md border border-input bg-background px-3 text-sm"
              >
                <option value="">No agent</option>
                {agents.map((agent) => (
                  <option key={agent.id} value={agent.id}>
                    {agent.name || agent.slug || agent.role || agent.id.slice(0, 12)}
                  </option>
                ))}
              </select>
            </div>

            <Button onClick={() => {
              if (selectedAgentForNew) {
                const agent = agents.find((a) => a.id === selectedAgentForNew)
                if (agent) {
                  // Use handleChat logic from AgentList to create linked conversation
                  handleNewConversationWithAgent(agent)
                  return
                }
              }
              handleNewConversation()
            }}>
              <Plus className="h-4 w-4 mr-2" />
              New Conversation
            </Button>
          </div>
        )}
      </div>

      {/* Right Panel - Context */}
      {showContextPanel && selectedConversation && (
        <div className="w-80 border-l border-border flex flex-col bg-muted/20">
          <div className="h-12 border-b border-border flex items-center justify-between px-4">
            <div className="flex items-center gap-2">
              <FileText className="h-4 w-4" />
              <span className="text-sm font-medium">Context</span>
            </div>
            <Button
              variant="ghost"
              size="icon"
              className="h-7 w-7"
              onClick={() => setShowContextPanel(false)}
            >
              <X className="h-4 w-4" />
            </Button>
          </div>

          {/* MODEL HUD - Top Section */}
          <div className="border-b border-border bg-card/50">
            <div className="p-3 space-y-3">
              {/* Provider Selector */}
              <div className="flex items-center justify-between">
                <div className="flex items-center gap-2">
                  <Folder className="h-4 w-4 text-primary" />
                  <span className="text-xs font-medium">Provider</span>
                </div>
                <select
                  value={selectedProvider}
                  onChange={(e) => {
                    setSelectedProvider(e.target.value)
                    setSelectedModel('')
                  }}
                  className="text-xs bg-muted border border-border rounded-md px-2 py-1 font-mono focus:outline-none focus:ring-1 focus:ring-ring"
                >
                  {PROVIDERS.map((provider) => (
                    <option key={provider.id} value={provider.id}>
                      {provider.id === ''
                        ? `Default (${linkedAgent?.llm_provider || 'openai'})`
                        : provider.name}
                    </option>
                  ))}
                </select>
              </div>

              {/* Model Selector */}
              <div className="flex items-center justify-between">
                <div className="flex items-center gap-2">
                  <Cpu className="h-4 w-4 text-primary" />
                  <span className="text-xs font-medium">Model</span>
                </div>
                <select
                  value={selectedModel}
                  onChange={(e) => setSelectedModel(e.target.value)}
                  className="text-xs bg-muted border border-border rounded-md px-2 py-1 font-mono focus:outline-none focus:ring-1 focus:ring-ring max-w-[160px]"
                >
                  {getModelsForProvider(selectedProvider).map((model) => (
                    <option key={model.id} value={model.id}>
                      {model.id === ''
                        ? `Default (${linkedAgent?.llm_model || 'gpt-4o-mini'})`
                        : model.name}
                    </option>
                  ))}
                </select>
              </div>

              {/* Companion 2-Stage Models */}
              <div className="pt-2 border-t border-border space-y-2">
                <div className="text-[10px] font-medium text-muted-foreground uppercase tracking-wider">Companion Models (2-Stage)</div>
                
                {/* Tool Model Selector */}
                <div className="flex items-center justify-between">
                  <div className="flex items-center gap-2">
                    <Wrench className="h-4 w-4 text-orange-500" />
                    <span className="text-xs font-medium">Tool Model</span>
                  </div>
                  <select
                    value={toolModel}
                    onChange={(e) => setToolModel(e.target.value)}
                    className="text-xs bg-muted border border-border rounded-md px-2 py-1 font-mono focus:outline-none focus:ring-1 focus:ring-ring max-w-[140px]"
                  >
                    {COMPANION_TOOL_MODELS.map((model) => (
                      <option key={model.id} value={model.id}>
                        {model.name}
                      </option>
                    ))}
                  </select>
                </div>

                {/* Response Model Selector */}
                <div className="flex items-center justify-between">
                  <div className="flex items-center gap-2">
                    <Bot className="h-4 w-4 text-purple-500" />
                    <span className="text-xs font-medium">Response Model</span>
                  </div>
                  <select
                    value={responseModel}
                    onChange={(e) => setResponseModel(e.target.value)}
                    className="text-xs bg-muted border border-border rounded-md px-2 py-1 font-mono focus:outline-none focus:ring-1 focus:ring-ring max-w-[140px]"
                  >
                    {COMPANION_RESPONSE_MODELS.map((model) => (
                      <option key={model.id} value={model.id}>
                        {model.name}
                      </option>
                    ))}
                  </select>
                </div>

                {/* Max History Turns */}
                <div className="flex items-center justify-between">
                  <div className="flex items-center gap-2">
                    <Clock className="h-4 w-4 text-blue-500" />
                    <span className="text-xs font-medium">History Turns</span>
                  </div>
                  <select
                    value={maxHistoryTurns}
                    onChange={(e) => setMaxHistoryTurns(Number(e.target.value))}
                    className="text-xs bg-muted border border-border rounded-md px-2 py-1 font-mono focus:outline-none focus:ring-1 focus:ring-ring max-w-[140px]"
                  >
                    <option value={10}>10 turns</option>
                    <option value={20}>20 turns</option>
                    <option value={50}>50 turns</option>
                    <option value={100}>100 turns</option>
                    <option value={-1}>Disabled</option>
                  </select>
                </div>
              </div>

              {/* Token Usage */}
              <div className="pt-2 border-t border-border space-y-2">
                <div className="flex items-center gap-2">
                  <Coins className="h-4 w-4 text-yellow-500" />
                  <span className="text-xs font-medium">Token Usage</span>
                </div>
                <div className="grid grid-cols-3 gap-2 text-center">
                  <div className="bg-muted/50 rounded-md p-2">
                    <div className="text-xs text-muted-foreground">Input</div>
                    <div className="text-sm font-mono font-medium">{tokenUsage.inputTokens.toLocaleString()}</div>
                  </div>
                  <div className="bg-muted/50 rounded-md p-2">
                    <div className="text-xs text-muted-foreground">Output</div>
                    <div className="text-sm font-mono font-medium">{tokenUsage.outputTokens.toLocaleString()}</div>
                  </div>
                  <div className="bg-primary/10 rounded-md p-2">
                    <div className="text-xs text-muted-foreground">Total</div>
                    <div className="text-sm font-mono font-medium text-primary">{tokenUsage.totalTokens.toLocaleString()}</div>
                  </div>
                </div>
              </div>

              {/* System Prompt (Editable) */}
              <div className="pt-2 border-t border-border space-y-2">
                <div className="flex items-center justify-between">
                  <div className="flex items-center gap-2">
                    <Sliders className="h-4 w-4 text-blue-500" />
                    <span className="text-xs font-medium">System Prompt</span>
                  </div>
                  {!editingSystemPrompt ? (
                    <Button
                      variant="ghost"
                      size="sm"
                      className="h-6 text-xs"
                      onClick={() => {
                        setEditingSystemPrompt(true)
                        setSystemPromptDraft(contextInfo.systemPrompt || '')
                      }}
                    >
                      <Pencil className="h-3 w-3 mr-1" />
                      Edit
                    </Button>
                  ) : (
                    <div className="flex gap-1">
                      <Button
                        variant="ghost"
                        size="sm"
                        className="h-6 w-6 p-0"
                        onClick={() => {
                          setEditingSystemPrompt(false)
                          // TODO: Save system prompt via API
                          setContextInfo((prev) => ({ ...prev, systemPrompt: systemPromptDraft }))
                        }}
                      >
                        <Save className="h-3 w-3 text-green-500" />
                      </Button>
                      <Button
                        variant="ghost"
                        size="sm"
                        className="h-6 w-6 p-0"
                        onClick={() => {
                          setEditingSystemPrompt(false)
                          setSystemPromptDraft('')
                        }}
                      >
                        <RotateCcw className="h-3 w-3 text-muted-foreground" />
                      </Button>
                    </div>
                  )}
                </div>
                {editingSystemPrompt ? (
                  <Textarea
                    value={systemPromptDraft}
                    onChange={(e: React.ChangeEvent<HTMLTextAreaElement>) => setSystemPromptDraft(e.target.value)}
                    className="text-xs min-h-[100px] max-h-[150px] font-mono"
                    placeholder="Enter system prompt..."
                  />
                ) : contextInfo.systemPrompt ? (
                  <Card className="p-2 max-h-[100px] overflow-y-auto">
                    <pre className="text-[11px] text-muted-foreground whitespace-pre-wrap">
                      {contextInfo.systemPrompt.slice(0, 300)}
                      {contextInfo.systemPrompt.length > 300 && '...'}
                    </pre>
                  </Card>
                ) : (
                  <div className="text-xs text-muted-foreground italic">No system prompt configured</div>
                )}
              </div>

              {/* Personality Dimensions (Sliders) */}
              {personalityInfo?.profile?.dimensions && personalityInfo.profile.dimensions.length > 0 && (
                <div className="pt-2 border-t border-border space-y-3">
                  <div className="flex items-center gap-2">
                    <Sparkles className="h-4 w-4 text-purple-500" />
                    <span className="text-xs font-medium">Personality</span>
                  </div>
                  <div className="space-y-3">
                    {personalityInfo.profile.dimensions.map((dim) => (
                      <div key={dim.name} className="space-y-1.5">
                        <div className="flex justify-between text-xs">
                          <span className="capitalize text-muted-foreground">{dim.name}</span>
                          <span className="font-mono text-primary">
                            {(dim.value * 100).toFixed(0)}%
                          </span>
                        </div>
                        <Slider
                          value={dim.value}
                          min={0}
                          max={1}
                          step={0.05}
                          onChange={(value) => {
                            // Update local state immediately for responsiveness
                            setPersonalityInfo((prev) => {
                              if (!prev?.profile) return prev
                              return {
                                ...prev,
                                profile: {
                                  ...prev.profile,
                                  dimensions: prev.profile.dimensions.map((d) =>
                                    d.name === dim.name ? { ...d, value } : d
                                  ),
                                },
                              }
                            })
                            
                            // Debounce API call
                            if (!selectedConversation) return
                            const existingTimer = personalityDebounceRefs.current.get(dim.name)
                            if (existingTimer) clearTimeout(existingTimer)
                            
                            const timer = setTimeout(async () => {
                              try {
                                await updatePersonalityDimension(selectedConversation.id, dim.name, value)
                              } catch (err) {
                                console.error('Failed to update personality dimension:', err)
                              }
                              personalityDebounceRefs.current.delete(dim.name)
                            }, 300)
                            personalityDebounceRefs.current.set(dim.name, timer)
                          }}
                        />
                        <div className="flex justify-between text-[10px] text-muted-foreground">
                          <span>{dim.min_label}</span>
                          <span>{dim.max_label}</span>
                        </div>
                      </div>
                    ))}
                  </div>
                </div>
              )}

              {/* Built Prompt Preview */}
              <div className="space-y-2">
                <div className="flex items-center justify-between">
                  <h4 className="text-xs font-semibold text-muted-foreground uppercase tracking-wider flex items-center gap-1">
                    <FileText className="h-3 w-3" />
                    Built Prompt Preview
                  </h4>
                  <Badge variant="outline" className="text-[10px]">
                    {messages.length} messages
                  </Badge>
                </div>
                <Card className="p-2 max-h-[200px] overflow-y-auto">
                  <div className="space-y-2 text-[10px] font-mono">
                    {contextInfo.systemPrompt && (
                      <div className="p-2 bg-blue-500/10 rounded border-l-2 border-blue-500">
                        <span className="font-semibold text-blue-600">SYSTEM:</span>
                        <pre className="whitespace-pre-wrap text-muted-foreground mt-1">
                          {contextInfo.systemPrompt.slice(0, 200)}
                          {contextInfo.systemPrompt.length > 200 && "..."}
                        </pre>
                      </div>
                    )}
                    {messages.slice(-5).map((msg, i) => (
                      <div 
                        key={i} 
                        className={cn(
                          "p-2 rounded border-l-2",
                          msg.role === "user" 
                            ? "bg-green-500/10 border-green-500" 
                            : "bg-purple-500/10 border-purple-500"
                        )}
                      >
                        <span className={cn(
                          "font-semibold",
                          msg.role === "user" ? "text-green-600" : "text-purple-600"
                        )}>
                          {msg.role.toUpperCase()}:
                        </span>
                        <pre className="whitespace-pre-wrap text-muted-foreground mt-1">
                          {msg.content.slice(0, 150)}
                          {msg.content.length > 150 && "..."}
                        </pre>
                      </div>
                    ))}
                    {messages.length > 5 && (
                      <div className="text-center text-muted-foreground py-1">
                        ... {messages.length - 5} earlier messages ...
                      </div>
                    )}
                  </div>
                </Card>
              </div>
            </div>
          </div>

          {/* METADATA - Bottom Section */}
          <ScrollArea className="flex-1">
            <div className="p-4 space-y-4">
              {/* Selected Message Details */}
              {selectedMessage && (
                <div className="space-y-2">
                  <div className="flex items-center justify-between">
                    <h4 className="text-xs font-semibold text-muted-foreground uppercase tracking-wider">
                      Selected Message
                    </h4>
                    <Button
                      variant="ghost"
                      size="sm"
                      className="h-6 text-xs"
                      onClick={() => setSelectedMessage(null)}
                    >
                      Clear
                    </Button>
                  </div>
                  <Card className="p-3 space-y-3">
                    <div className="flex items-center justify-between text-xs">
                      <span className="text-muted-foreground">Role</span>
                      <Badge variant={selectedMessage.role === 'assistant' ? 'default' : 'secondary'}>
                        {selectedMessage.role}
                      </Badge>
                    </div>
                    {selectedMessage.timestamp && (
                      <div className="flex items-center justify-between text-xs">
                        <span className="text-muted-foreground">Time</span>
                        <span className="flex items-center gap-1">
                          <Clock className="h-3 w-3" />
                          {formatRelativeTime(selectedMessage.timestamp)}
                        </span>
                      </div>
                    )}
                    {selectedMessage.content && (
                      <div className="pt-2 border-t border-border">
                        <span className="text-xs font-medium text-muted-foreground">Content Preview</span>
                        <p className="text-xs text-foreground mt-1 line-clamp-3">
                          {selectedMessage.content.slice(0, 200)}
                          {selectedMessage.content.length > 200 && '...'}
                        </p>
                      </div>
                    )}
                  </Card>

                  {/* Tool Calls for Selected Message */}
                  {selectedMessage.tool_calls && selectedMessage.tool_calls.length > 0 && (
                    <div className="space-y-2">
                      <h4 className="text-xs font-semibold text-muted-foreground uppercase tracking-wider">
                        Tool Calls ({selectedMessage.tool_calls.length})
                      </h4>
                      <div className="space-y-2">
                        {selectedMessage.tool_calls.map((tool, i) => (
                          <Card key={i} className="p-2">
                            <div className="flex items-center gap-2">
                              <Settings2 className="h-3 w-3 text-primary" />
                              <span className="text-xs font-mono flex-1">{tool.name}</span>
                              <Badge
                                variant="secondary"
                                className={cn(
                                  'text-[10px]',
                                  tool.status === 'completed' && 'bg-green-500/20 text-green-600',
                                  tool.status === 'error' && 'bg-red-500/20 text-red-600',
                                  tool.status === 'pending' && 'bg-yellow-500/20 text-yellow-600'
                                )}
                              >
                                {tool.status}
                              </Badge>
                            </div>
                            {tool.input && Object.keys(tool.input).length > 0 && (
                              <div className="mt-2">
                                <span className="text-[10px] font-medium text-muted-foreground">Input (raw JSON):</span>
                                <pre className="text-[10px] text-muted-foreground overflow-auto max-h-64 mt-1 p-2 bg-muted rounded font-mono whitespace-pre-wrap break-all">
                                  {JSON.stringify(tool.input, null, 2)}
                                </pre>
                              </div>
                            )}
                            {tool.output && (
                              <div className="mt-2">
                                <span className="text-[10px] font-medium text-muted-foreground">Output (raw):</span>
                                <pre className="text-[10px] text-muted-foreground overflow-auto max-h-96 mt-1 p-2 bg-muted rounded font-mono whitespace-pre-wrap break-all">
                                  {tool.output}
                                </pre>
                              </div>
                            )}
                          </Card>
                        ))}
                      </div>
                    </div>
                  )}
                </div>
              )}

              {/* Session Info */}
              <div className="space-y-2">
                <h4 className="text-xs font-semibold text-muted-foreground uppercase tracking-wider">
                  Session
                </h4>
                <Card className="p-3 space-y-2 text-sm">
                  <div className="flex items-center justify-between">
                    <span className="text-muted-foreground">Profile</span>
                    <Badge variant="secondary" className="text-xs">
                      {contextInfo.profile || session?.profile || 'companion'}
                    </Badge>
                  </div>
                  <div className="flex items-center justify-between">
                    <span className="text-muted-foreground">Workspace</span>
                    <span className="text-xs font-mono truncate max-w-[140px]" title={contextInfo.workspace}>
                      {contextInfo.workspace || '/'}
                    </span>
                  </div>
                  {contextInfo.createdAt && (
                    <div className="flex items-center justify-between">
                      <span className="text-muted-foreground">Created</span>
                      <span className="text-xs flex items-center gap-1">
                        <Clock className="h-3 w-3" />
                        {formatRelativeTime(contextInfo.createdAt)}
                      </span>
                    </div>
                  )}
                </Card>
              </div>

              {/* Linked Agent */}
              {linkedAgent && (
                <div className="space-y-2">
                  <h4 className="text-xs font-semibold text-muted-foreground uppercase tracking-wider">
                    Linked Agent
                  </h4>
                  <Card className="p-3 space-y-2">
                    <div className="flex items-center gap-2">
                      <div className="h-8 w-8 rounded-lg bg-primary/10 flex items-center justify-center">
                        <Bot className="h-4 w-4 text-primary" />
                      </div>
                      <div>
                        <div className="text-sm font-medium">
                          {linkedAgent.name || linkedAgent.slug || 'Unnamed'}
                        </div>
                        <div className="text-xs text-muted-foreground">
                          {linkedAgent.role || 'agent'}
                        </div>
                      </div>
                    </div>
                    <div className="pt-2 border-t border-border space-y-1 text-xs">
                      <div className="flex justify-between">
                        <span className="text-muted-foreground">ID</span>
                        <span className="font-mono">{linkedAgent.id.slice(0, 16)}...</span>
                      </div>
                      <div className="flex justify-between">
                        <span className="text-muted-foreground">Model</span>
                        <span>{linkedAgent.llm_model || 'default'}</span>
                      </div>
                      <div className="flex justify-between">
                        <span className="text-muted-foreground">State</span>
                        <Badge
                          variant={linkedAgent.state === 'running' ? 'default' : 'secondary'}
                          className="text-[10px]"
                        >
                          {linkedAgent.state}
                        </Badge>
                      </div>
                    </div>
                  </Card>
                </div>
              )}

              {/* Conversation Info */}
              <div className="space-y-2">
                <h4 className="text-xs font-semibold text-muted-foreground uppercase tracking-wider">
                  Conversation
                </h4>
                <Card className="p-3 space-y-2 text-xs">
                  <div className="flex justify-between">
                    <span className="text-muted-foreground">ID</span>
                    <span className="font-mono truncate max-w-[140px]" title={selectedConversation.id}>
                      {selectedConversation.id.slice(0, 20)}...
                    </span>
                  </div>
                  <div className="flex justify-between">
                    <span className="text-muted-foreground">Messages</span>
                    <span>{messages.length}</span>
                  </div>
                  <div className="flex justify-between">
                    <span className="text-muted-foreground">Updated</span>
                    <span>{formatRelativeTime(selectedConversation.updated_at)}</span>
                  </div>
                </Card>
              </div>



              {/* Injected Context (from hooks) */}
              {contextInfo.injectedContexts && contextInfo.injectedContexts.length > 0 && (
                <div className="space-y-2">
                  <h4 className="text-xs font-semibold text-muted-foreground uppercase tracking-wider">
                    Injected Context ({contextInfo.injectedContexts.length})
                  </h4>
                  <div className="space-y-2">
                    {contextInfo.injectedContexts.slice(-3).map((ctx, i) => (
                      <Card key={i} className="p-2">
                        <div className="flex items-center gap-2 mb-1">
                          <Sparkles className="h-3 w-3 text-yellow-500" />
                          <span className="text-xs font-mono text-muted-foreground">
                            {ctx.source}
                          </span>
                        </div>
                        <pre className="text-[10px] text-muted-foreground whitespace-pre-wrap overflow-x-auto max-h-32">
                          {ctx.content.slice(0, 500)}
                          {ctx.content.length > 500 && '...'}
                        </pre>
                      </Card>
                    ))}
                  </div>
                </div>
              )}

              {/* Personality Traits (learned traits, interests, dislikes - dimensions are sliders above) */}
              {personalityInfo?.profile && (personalityInfo.profile.learned_traits?.length > 0 || personalityInfo.profile.interests?.length > 0 || personalityInfo.profile.dislikes?.length > 0) && (
                <div className="space-y-2">
                  <h4 className="text-xs font-semibold text-muted-foreground uppercase tracking-wider">
                    Personality Traits
                  </h4>
                  <Card className="p-3 space-y-3">
                    {/* Learned Traits */}
                    {personalityInfo.profile.learned_traits?.length > 0 && (
                      <div>
                        <span className="text-xs font-medium text-muted-foreground">Learned Traits</span>
                        <div className="flex flex-wrap gap-1 mt-1">
                          {personalityInfo.profile.learned_traits.map((trait, i) => (
                            <Badge key={i} variant="secondary" className="text-[10px]">
                              {trait}
                            </Badge>
                          ))}
                        </div>
                      </div>
                    )}

                    {/* Interests */}
                    {personalityInfo.profile.interests?.length > 0 && (
                      <div className={personalityInfo.profile.learned_traits?.length > 0 ? "pt-2 border-t border-border" : ""}>
                        <span className="text-xs font-medium text-muted-foreground">Interests</span>
                        <div className="flex flex-wrap gap-1 mt-1">
                          {personalityInfo.profile.interests.map((interest, i) => (
                            <Badge key={i} variant="outline" className="text-[10px] text-green-600">
                              {interest}
                            </Badge>
                          ))}
                        </div>
                      </div>
                    )}

                    {/* Dislikes */}
                    {personalityInfo.profile.dislikes?.length > 0 && (
                      <div className={(personalityInfo.profile.learned_traits?.length > 0 || personalityInfo.profile.interests?.length > 0) ? "pt-2 border-t border-border" : ""}>
                        <span className="text-xs font-medium text-muted-foreground">Dislikes</span>
                        <div className="flex flex-wrap gap-1 mt-1">
                          {personalityInfo.profile.dislikes.map((dislike, i) => (
                            <Badge key={i} variant="outline" className="text-[10px] text-red-600">
                              {dislike}
                            </Badge>
                          ))}
                        </div>
                      </div>
                    )}
                  </Card>
                </div>
              )}


            </div>
          </ScrollArea>
        </div>
      )}
    </div>
  )
}
