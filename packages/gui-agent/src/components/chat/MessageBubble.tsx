import { cn, formatRelativeTime } from '@/lib/utils'
import { Badge } from '@/components/ui/badge'
import { Bot, User, Wrench, ChevronDown, ChevronRight } from 'lucide-react'
import { useState } from 'react'
import type { ConsoleMessage, ToolCall } from '@/api/client'

interface MessageBubbleProps {
  message: ConsoleMessage
  showTimestamp?: boolean
  isSelected?: boolean
  onSelect?: (message: ConsoleMessage) => void
}

/**
 * Renders a chat message bubble with avatar, content, optional timestamp, and optional tool-call previews.
 *
 * @param message - The console message to display, including role, content, timestamp, and optional tool_calls.
 * @param showTimestamp - Whether to display the message's relative timestamp (default: true).
 * @param isSelected - Whether the message is visually marked as selected.
 * @param onSelect - Optional callback invoked with the message when a non-user message is clicked.
 * @returns The JSX element representing the styled message bubble.
 */
export function MessageBubble({ message, showTimestamp = true, isSelected, onSelect }: MessageBubbleProps) {
  const isUser = message.role === 'user'
  const hasToolCalls = message.tool_calls && message.tool_calls.length > 0
  const isClickable = !isUser && onSelect

  const handleClick = () => {
    if (isClickable) {
      onSelect(message)
    }
  }

  return (
    <div
      className={cn(
        'flex gap-3 p-3',
        isUser ? 'flex-row-reverse' : 'flex-row',
        isClickable && 'cursor-pointer hover:bg-accent/30 transition-colors rounded-lg',
        isSelected && 'bg-accent/50 ring-1 ring-primary/30 rounded-lg'
      )}
      onClick={handleClick}
    >
      {/* Avatar */}
      <div
        className={cn(
          'flex-shrink-0 h-8 w-8 rounded-full flex items-center justify-center',
          isUser ? 'bg-primary text-primary-foreground' : 'bg-muted'
        )}
      >
        {isUser ? (
          <User className="h-4 w-4" />
        ) : (
          <Bot className="h-4 w-4" />
        )}
      </div>

      {/* Content */}
      <div
        className={cn(
          'flex-1 max-w-[80%]',
          isUser ? 'items-end' : 'items-start'
        )}
      >
        <div
          className={cn(
            'rounded-lg px-4 py-2',
            isUser
              ? 'bg-primary text-primary-foreground'
              : 'bg-card border border-border'
          )}
        >
          {/* Message content */}
          <div className="text-sm whitespace-pre-wrap break-words">
            {message.content}
          </div>

          {/* Tool calls */}
          {hasToolCalls && (
            <div className="mt-3 space-y-2">
              {message.tool_calls?.map((tool, idx) => (
                <ToolCallCard key={idx} tool={tool} />
              ))}
            </div>
          )}
        </div>

        {/* Timestamp */}
        {showTimestamp && message.timestamp && (
          <div
            className={cn(
              'text-xs text-muted-foreground mt-1',
              isUser ? 'text-right' : 'text-left'
            )}
          >
            {formatRelativeTime(message.timestamp)}
          </div>
        )}
      </div>
    </div>
  )
}

interface ToolCallCardProps {
  tool: ToolCall
}

/**
 * Renders a collapsible card that summarizes a tool call and optionally shows its details.
 *
 * When collapsed the card shows the tool name and a status badge; when expanded it shows a JSON-formatted
 * "Input" section if the tool has input keys and an "Output" section with the tool output truncated to
 * 500 characters with an ellipsis if longer.
 *
 * @param tool - The tool call data to display (name, status, optional input and output).
 * @returns A React element containing the tool call card UI.
 */
function ToolCallCard({ tool }: ToolCallCardProps) {
  const [expanded, setExpanded] = useState(false)

  const statusColor = {
    pending: 'bg-yellow-500/20 text-yellow-500',
    completed: 'bg-green-500/20 text-green-500',
    error: 'bg-red-500/20 text-red-500',
  }[tool.status]

  return (
    <div className="rounded-md border border-border bg-background/50 overflow-hidden">
      <button
        onClick={() => setExpanded(!expanded)}
        className="w-full flex items-center gap-2 px-3 py-2 hover:bg-accent/50 transition-colors"
      >
        {expanded ? (
          <ChevronDown className="h-3 w-3 text-muted-foreground" />
        ) : (
          <ChevronRight className="h-3 w-3 text-muted-foreground" />
        )}
        <Wrench className="h-3 w-3 text-muted-foreground" />
        <span className="text-xs font-medium flex-1 text-left truncate">
          {tool.name}
        </span>
        <Badge className={cn('text-xs', statusColor)}>{tool.status}</Badge>
      </button>

      {expanded && (
        <div className="px-3 py-2 border-t border-border space-y-2">
          {tool.input && Object.keys(tool.input).length > 0 && (
            <div>
              <span className="text-xs font-medium text-muted-foreground">
                Input:
              </span>
              <pre className="text-xs bg-muted rounded p-2 mt-1 overflow-x-auto">
                {JSON.stringify(tool.input, null, 2)}
              </pre>
            </div>
          )}
          {tool.output && (
            <div>
              <span className="text-xs font-medium text-muted-foreground">
                Output:
              </span>
              <pre className="text-xs bg-muted rounded p-2 mt-1 overflow-x-auto max-h-40">
                {tool.output.slice(0, 500)}
                {tool.output.length > 500 && '...'}
              </pre>
            </div>
          )}
        </div>
      )}
    </div>
  )
}

/**
 * Renders a compact typing indicator showing a bot avatar and three animated dots.
 *
 * @returns A JSX element containing a bot avatar and a three-dot pulsing indicator.
 */
export function TypingIndicator() {
  return (
    <div className="flex gap-3 p-3">
      <div className="flex-shrink-0 h-8 w-8 rounded-full bg-muted flex items-center justify-center">
        <Bot className="h-4 w-4" />
      </div>
      <div className="flex items-center gap-1 px-4 py-3 rounded-lg bg-card border border-border">
        <span className="h-2 w-2 rounded-full bg-muted-foreground animate-bounce" />
        <span
          className="h-2 w-2 rounded-full bg-muted-foreground animate-bounce"
          style={{ animationDelay: '0.1s' }}
        />
        <span
          className="h-2 w-2 rounded-full bg-muted-foreground animate-bounce"
          style={{ animationDelay: '0.2s' }}
        />
      </div>
    </div>
  )
}