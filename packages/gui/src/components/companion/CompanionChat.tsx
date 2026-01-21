import { useState, useRef, useEffect } from "react";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import { cn } from "@/lib/utils";
import { useCompanionChat } from "@/api/hooks";
import {
  Send,
  Square,
  User,
  Bot,
  Loader2,
  Sparkles,
  Clock,
  Zap,
} from "lucide-react";

export interface ChatMessage {
  id: string;
  role: "user" | "assistant";
  content: string;
  timestamp: Date;
  isStreaming?: boolean;
  metadata?: {
    contextQueries?: number;
    toolsUsed?: string[];
    durationMs?: number;
    tokenUsage?: {
      input: number;
      output: number;
      total: number;
    };
  };
}

interface CompanionChatProps {
  conversationId: string;
  personality?: string;
  onMessageSent?: (message: ChatMessage) => void;
  onResponseReceived?: (message: ChatMessage) => void;
  className?: string;
}

export function CompanionChat({
  conversationId,
  personality,
  onMessageSent,
  onResponseReceived,
  className,
}: CompanionChatProps) {
  const [messages, setMessages] = useState<ChatMessage[]>([]);
  const [inputValue, setInputValue] = useState("");
  const messagesEndRef = useRef<HTMLDivElement>(null);
  const inputRef = useRef<HTMLTextAreaElement>(null);

  const chatMutation = useCompanionChat();

  // Auto-scroll to bottom when new messages arrive
  useEffect(() => {
    messagesEndRef.current?.scrollIntoView({ behavior: "smooth" });
  }, [messages]);

  // Handle send message
  const handleSend = async () => {
    if (!inputValue.trim() || chatMutation.isPending) return;

    const userMessage: ChatMessage = {
      id: crypto.randomUUID(),
      role: "user",
      content: inputValue.trim(),
      timestamp: new Date(),
    };

    setMessages((prev) => [...prev, userMessage]);
    onMessageSent?.(userMessage);
    setInputValue("");

    // Add placeholder assistant message
    const assistantPlaceholderId = crypto.randomUUID();
    setMessages((prev) => [
      ...prev,
      {
        id: assistantPlaceholderId,
        role: "assistant",
        content: "",
        timestamp: new Date(),
        isStreaming: true,
      },
    ]);

    try {
      const response = await chatMutation.mutateAsync({
        conversation_id: conversationId,
        message: userMessage.content,
        personality,
      });

      const assistantMessage: ChatMessage = {
        id: assistantPlaceholderId,
        role: "assistant",
        content: response.response,
        timestamp: new Date(),
        isStreaming: false,
        metadata: {
          contextQueries: response.context_queries,
          toolsUsed: response.tools_used,
          durationMs: response.duration_ms,
          tokenUsage: {
            input: response.token_usage.input_tokens,
            output: response.token_usage.output_tokens,
            total: response.token_usage.total_tokens,
          },
        },
      };

      setMessages((prev) =>
        prev.map((m) => (m.id === assistantPlaceholderId ? assistantMessage : m))
      );
      onResponseReceived?.(assistantMessage);
    } catch (error) {
      // Update placeholder with error
      setMessages((prev) =>
        prev.map((m) =>
          m.id === assistantPlaceholderId
            ? {
                ...m,
                content: `Error: ${error instanceof Error ? error.message : "Failed to get response"}`,
                isStreaming: false,
              }
            : m
        )
      );
    }

    inputRef.current?.focus();
  };

  // Handle key press
  const handleKeyPress = (e: React.KeyboardEvent) => {
    if (e.key === "Enter" && !e.shiftKey) {
      e.preventDefault();
      handleSend();
    }
  };

  return (
    <div className={cn("flex flex-col h-full", className)}>
      {/* Messages area */}
      <div className="flex-1 overflow-y-auto p-4 space-y-4">
        {messages.length === 0 ? (
          <div className="flex h-full items-center justify-center">
            <div className="text-center text-muted-foreground space-y-2">
              <Sparkles className="h-12 w-12 mx-auto opacity-50" />
              <p className="text-lg font-medium">Start a conversation</p>
              <p className="text-sm">
                Your companion remembers context across sessions
              </p>
            </div>
          </div>
        ) : (
          <>
            {messages.map((message) => (
              <MessageBubble key={message.id} message={message} />
            ))}
            <div ref={messagesEndRef} />
          </>
        )}
      </div>

      {/* Input area */}
      <div className="border-t p-4">
        <div className="flex items-start gap-2">
          <textarea
            ref={inputRef}
            value={inputValue}
            onChange={(e) => setInputValue(e.target.value)}
            onKeyDown={handleKeyPress}
            placeholder="Type a message... (Shift+Enter for newline)"
            disabled={chatMutation.isPending}
            className="flex-1 min-h-[40px] max-h-[120px] px-3 py-2 text-sm rounded-md border border-input bg-background ring-offset-background placeholder:text-muted-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2 disabled:cursor-not-allowed disabled:opacity-50 resize-none"
            rows={1}
          />

          {chatMutation.isPending ? (
            <Button variant="destructive" disabled title="Processing...">
              <Square className="h-4 w-4 mr-2" />
              Cancel
            </Button>
          ) : (
            <Button
              onClick={handleSend}
              disabled={!inputValue.trim()}
              title="Send message"
            >
              <Send className="h-4 w-4 mr-2" />
              Send
            </Button>
          )}
        </div>
      </div>
    </div>
  );
}

// Message bubble component
function MessageBubble({ message }: { message: ChatMessage }) {
  const isUser = message.role === "user";

  return (
    <div
      className={cn("flex gap-3", isUser ? "flex-row-reverse" : "flex-row")}
    >
      {/* Avatar */}
      <div
        className={cn(
          "flex h-8 w-8 shrink-0 items-center justify-center rounded-full",
          isUser
            ? "bg-primary text-primary-foreground"
            : "bg-gradient-to-br from-violet-500 to-purple-600 text-white"
        )}
      >
        {isUser ? <User className="h-4 w-4" /> : <Bot className="h-4 w-4" />}
      </div>

      {/* Message content */}
      <div
        className={cn(
          "flex max-w-[80%] flex-col gap-1",
          isUser ? "items-end" : "items-start"
        )}
      >
        <div
          className={cn(
            "rounded-2xl px-4 py-2",
            isUser
              ? "bg-primary text-primary-foreground rounded-br-md"
              : "bg-secondary text-secondary-foreground rounded-bl-md"
          )}
        >
          {message.isStreaming ? (
            <div className="flex items-center gap-2">
              <Loader2 className="h-4 w-4 animate-spin" />
              <span className="text-sm">Thinking...</span>
            </div>
          ) : (
            <div className="whitespace-pre-wrap break-words text-sm">
              {message.content}
            </div>
          )}
        </div>

        {/* Metadata */}
        <div className="flex items-center gap-2 px-1">
          <span className="text-xs text-muted-foreground">
            {message.timestamp.toLocaleTimeString()}
          </span>

          {message.metadata && !isUser && (
            <>
              {message.metadata.durationMs && (
                <Badge variant="outline" className="text-xs gap-1 h-5">
                  <Clock className="h-3 w-3" />
                  {message.metadata.durationMs}ms
                </Badge>
              )}
              {message.metadata.contextQueries !== undefined &&
                message.metadata.contextQueries > 0 && (
                  <Badge variant="outline" className="text-xs gap-1 h-5">
                    <Zap className="h-3 w-3" />
                    {message.metadata.contextQueries} queries
                  </Badge>
                )}
              {message.metadata.tokenUsage && (
                <Badge variant="outline" className="text-xs gap-1 h-5">
                  {message.metadata.tokenUsage.total} tokens
                </Badge>
              )}
            </>
          )}
        </div>
      </div>
    </div>
  );
}

export default CompanionChat;
