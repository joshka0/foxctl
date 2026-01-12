import { useState, useRef, useEffect } from "react";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Badge } from "@/components/ui/badge";
import { Select } from "@/components/ui/select";
import {
  useConsoleWebSocket,
  useConsoles,
  useCreateConsole,
  useDeleteConsole,
  type ConsoleMessage,
} from "@/api/hooks";
import { cn } from "@/lib/utils";
import {
  Send,
  Square,
  Plus,
  Trash2,
  Wifi,
  WifiOff,
  Loader2,
  RefreshCw,
  User,
  Bot,
  AlertCircle,
} from "lucide-react";

interface ConsoleSession {
  id: string;
  workspace?: string;
  profile?: string;
  created?: string;
  message_count?: number;
  client_count?: number;
}

export function ConsolePage() {
  const [selectedSessionId, setSelectedSessionId] = useState<string | null>(null);
  const [inputValue, setInputValue] = useState("");
  const messagesEndRef = useRef<HTMLDivElement>(null);

  // Fetch available console sessions
  const { data: consolesData, isLoading: isLoadingConsoles } = useConsoles({ limit: 50 });
  const createConsole = useCreateConsole();
  const deleteConsole = useDeleteConsole();

  // WebSocket connection
  const {
    connected,
    connecting,
    error,
    messages,
    inflight,
    sendMessage,
    cancel,
    clearMessages,
    reconnect,
  } = useConsoleWebSocket(selectedSessionId);

  // Auto-scroll to bottom when new messages arrive
  useEffect(() => {
    messagesEndRef.current?.scrollIntoView({ behavior: "smooth" });
  }, [messages]);

  // Handle send message
  const handleSend = () => {
    if (!inputValue.trim() || !connected) return;
    sendMessage(inputValue.trim());
    setInputValue("");
  };

  // Handle key press
  const handleKeyPress = (e: React.KeyboardEvent) => {
    if (e.key === "Enter" && !e.shiftKey) {
      e.preventDefault();
      handleSend();
    }
  };

  // Create new session
  const handleCreateSession = async () => {
    try {
      const result = await createConsole.mutateAsync({
        workspace: window.location.pathname,
        profile: "explorer",
      });
      if (result?.id) {
        setSelectedSessionId(result.id);
      }
    } catch (err) {
      console.error("Failed to create console session:", err);
    }
  };

  // Delete session
  const handleDeleteSession = async () => {
    if (!selectedSessionId) return;
    try {
      await deleteConsole.mutateAsync(selectedSessionId);
      setSelectedSessionId(null);
    } catch (err) {
      console.error("Failed to delete console session:", err);
    }
  };

  const sessions: ConsoleSession[] = consolesData?.sessions || [];

  return (
    <div className="h-full flex flex-col gap-4 p-4">
      {/* Header with session selection */}
      <div className="flex items-center justify-between gap-4">
        <div className="flex items-center gap-2">
          <Select
            value={selectedSessionId || ""}
            onChange={(e) => setSelectedSessionId(e.target.value || null)}
            className="w-64"
          >
            <option value="">
              {isLoadingConsoles ? "Loading sessions..." : "Select a session..."}
            </option>
            {sessions.map((session) => (
              <option key={session.id} value={session.id}>
                {session.id.slice(0, 8)}... ({session.workspace || "default"})
              </option>
            ))}
          </Select>

          <Button variant="outline" size="icon" onClick={handleCreateSession} title="Create new session">
            <Plus className="h-4 w-4" />
          </Button>

          {selectedSessionId && (
            <Button
              variant="outline"
              size="icon"
              onClick={handleDeleteSession}
              title="Delete session"
              className="text-destructive hover:text-destructive"
            >
              <Trash2 className="h-4 w-4" />
            </Button>
          )}
        </div>

        {/* Connection status */}
        <div className="flex items-center gap-2">
          {connecting ? (
            <Badge variant="outline" className="gap-1">
              <Loader2 className="h-3 w-3 animate-spin" />
              Connecting...
            </Badge>
          ) : connected ? (
            <Badge variant="default" className="gap-1 bg-green-600">
              <Wifi className="h-3 w-3" />
              Connected
            </Badge>
          ) : (
            <Badge variant="destructive" className="gap-1">
              <WifiOff className="h-3 w-3" />
              Disconnected
            </Badge>
          )}

          {!connected && selectedSessionId && (
            <Button variant="ghost" size="sm" onClick={reconnect} title="Reconnect">
              <RefreshCw className="h-4 w-4" />
            </Button>
          )}
        </div>
      </div>

      {/* Error display */}
      {error && (
        <div className="flex items-center gap-2 rounded-md bg-destructive/10 p-3 text-destructive">
          <AlertCircle className="h-4 w-4" />
          <span className="text-sm">{error}</span>
        </div>
      )}

      {/* Chat area */}
      <Card className="flex-1 flex flex-col overflow-hidden">
        <CardHeader className="flex-shrink-0 flex flex-row items-center justify-between py-3">
          <CardTitle className="text-lg">Console</CardTitle>
          {messages.length > 0 && (
            <Button variant="ghost" size="sm" onClick={clearMessages}>
              Clear
            </Button>
          )}
        </CardHeader>
        <CardContent className="flex-1 overflow-y-auto p-4">
          {!selectedSessionId ? (
            <div className="flex h-full items-center justify-center text-muted-foreground">
              Select or create a session to start chatting
            </div>
          ) : messages.length === 0 ? (
            <div className="flex h-full items-center justify-center text-muted-foreground">
              No messages yet. Send a message to start.
            </div>
          ) : (
            <div className="space-y-4">
              {messages.map((message) => (
                <MessageBubble key={message.id} message={message} />
              ))}
              <div ref={messagesEndRef} />
            </div>
          )}
        </CardContent>
      </Card>

      {/* Input area */}
      <div className="flex items-center gap-2">
        <Input
          value={inputValue}
          onChange={(e) => setInputValue(e.target.value)}
          onKeyDown={handleKeyPress}
          placeholder={
            !selectedSessionId
              ? "Select a session first..."
              : !connected
              ? "Waiting for connection..."
              : "Type a message..."
          }
          disabled={!selectedSessionId || !connected}
          className="flex-1"
        />

        {inflight ? (
          <Button variant="destructive" onClick={cancel} title="Cancel request">
            <Square className="h-4 w-4 mr-2" />
            Cancel
          </Button>
        ) : (
          <Button
            onClick={handleSend}
            disabled={!inputValue.trim() || !connected || !selectedSessionId}
            title="Send message"
          >
            <Send className="h-4 w-4 mr-2" />
            Send
          </Button>
        )}
      </div>
    </div>
  );
}

// Message bubble component
function MessageBubble({ message }: { message: ConsoleMessage }) {
  const isUser = message.role === "user";
  const isSystem = message.role === "system";

  return (
    <div
      className={cn(
        "flex gap-3",
        isUser ? "flex-row-reverse" : "flex-row"
      )}
    >
      {/* Avatar */}
      <div
        className={cn(
          "flex h-8 w-8 shrink-0 items-center justify-center rounded-full",
          isUser
            ? "bg-primary text-primary-foreground"
            : isSystem
            ? "bg-muted text-muted-foreground"
            : "bg-secondary text-secondary-foreground"
        )}
      >
        {isUser ? (
          <User className="h-4 w-4" />
        ) : (
          <Bot className="h-4 w-4" />
        )}
      </div>

      {/* Message content */}
      <div
        className={cn(
          "flex max-w-[80%] flex-col gap-1 rounded-lg px-4 py-2",
          isUser
            ? "bg-primary text-primary-foreground"
            : isSystem
            ? "bg-muted text-muted-foreground"
            : "bg-secondary text-secondary-foreground"
        )}
      >
        <div className="whitespace-pre-wrap break-words text-sm">
          {message.content}
          {message.isStreaming && (
            <span className="ml-1 inline-block animate-pulse">...</span>
          )}
        </div>
        <div
          className={cn(
            "text-xs",
            isUser ? "text-primary-foreground/70" : "text-muted-foreground"
          )}
        >
          {new Date(message.timestamp).toLocaleTimeString()}
        </div>
      </div>
    </div>
  );
}
