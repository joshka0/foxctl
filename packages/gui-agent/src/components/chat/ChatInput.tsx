import { useState, useRef, useEffect, type KeyboardEvent } from 'react'
import { cn } from '@/lib/utils'
import { Button } from '@/components/ui/button'
import { Send, Square, Slash } from 'lucide-react'

interface ChatInputProps {
  onSend: (message: string) => void
  onCancel?: () => void
  disabled?: boolean
  inflight?: boolean
  placeholder?: string
}

/**
 * Renders a chat input area with auto-resizing textarea, send/cancel actions, and command-mode UI.
 *
 * Provides an auto-expanding textarea (max 200px) that sends trimmed input on Enter (Shift+Enter inserts a newline),
 * detects command mode when the value starts with `/` (adds visual indicator and hint), and shows a cancel button while inflight.
 *
 * @param onSend - Callback invoked with the trimmed message when the user submits a non-empty input
 * @param onCancel - Optional callback invoked to cancel an inflight send operation
 * @param disabled - When true, disables the textarea and send action
 * @param inflight - When true, shows a cancel (destructive) button instead of the send button
 * @param placeholder - Placeholder text for the textarea; defaults to "Ask the companion..."
 * @returns The chat input JSX element
 */
export function ChatInput({
  onSend,
  onCancel,
  disabled,
  inflight,
  placeholder = 'Ask the companion...',
}: ChatInputProps) {
  const [value, setValue] = useState('')
  const textareaRef = useRef<HTMLTextAreaElement>(null)

  // Auto-resize textarea
  useEffect(() => {
    const textarea = textareaRef.current
    if (textarea) {
      textarea.style.height = 'auto'
      textarea.style.height = `${Math.min(textarea.scrollHeight, 200)}px`
    }
  }, [value])

  const handleSubmit = () => {
    const trimmed = value.trim()
    if (trimmed && !disabled && !inflight) {
      onSend(trimmed)
      setValue('')
    }
  }

  const handleKeyDown = (e: KeyboardEvent<HTMLTextAreaElement>) => {
    if (e.key === 'Enter' && !e.shiftKey) {
      e.preventDefault()
      handleSubmit()
    }
  }

  const isCommand = value.startsWith('/')

  return (
    <div className="border-t border-border p-4">
      <div className="flex gap-2 items-end">
        <div className="flex-1 relative">
          <textarea
            ref={textareaRef}
            value={value}
            onChange={(e) => setValue(e.target.value)}
            onKeyDown={handleKeyDown}
            placeholder={placeholder}
            disabled={disabled}
            aria-label="Chat message"
            rows={1}
            className={cn(
              'w-full resize-none rounded-lg border border-input bg-background px-3 py-2 text-sm',
              'placeholder:text-muted-foreground focus:outline-none focus:ring-2 focus:ring-ring',
              'disabled:cursor-not-allowed disabled:opacity-50',
              isCommand && 'border-primary'
            )}
          />
          {isCommand && (
            <div className="absolute right-2 top-2">
              <Slash className="h-4 w-4 text-primary" />
            </div>
          )}
        </div>
        {inflight ? (
          <Button
            variant="destructive"
            size="icon"
            onClick={onCancel}
            aria-label="Cancel"
            className="h-9 w-9 shrink-0"
          >
            <Square className="h-4 w-4" />
          </Button>
        ) : (
          <Button
            onClick={handleSubmit}
            disabled={!value.trim() || disabled}
            size="icon"
            aria-label="Send message"
            className="h-9 w-9 shrink-0"
          >
            <Send className="h-4 w-4" />
          </Button>
        )}
      </div>
      <div className="flex items-center justify-between mt-2 text-xs text-muted-foreground">
        <span>
          {isCommand ? (
            <span className="text-primary">Command mode</span>
          ) : (
            'Press Enter to send, Shift+Enter for newline'
          )}
        </span>
        <span>Type / for commands</span>
      </div>
    </div>
  )
}