import { useState } from "react";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Badge } from "@/components/ui/badge";
import { cn } from "@/lib/utils";
import { useCompanionConversations } from "@/api/hooks";
import { CompanionChat, MemoryStats, ContextViewer } from "@/components/companion";
import {
  Plus,
  MessageSquare,
  PanelRightClose,
  PanelRight,
  Search,
  Sparkles,
  Loader2,
} from "lucide-react";

export function ConsolePage() {
  const [selectedConversationId, setSelectedConversationId] = useState<string | null>(null);
  const [showSidebar, setShowSidebar] = useState(true);
  const [searchQuery, setSearchQuery] = useState("");

  // Fetch conversations
  const { data: conversationsData, isLoading: isLoadingConversations } = useCompanionConversations({
    limit: 50,
  });

  const conversations = conversationsData?.conversations ?? [];

  // Filter conversations by search
  const filteredConversations = conversations.filter((conv) => {
    if (!searchQuery) return true;
    const query = searchQuery.toLowerCase();
    return (
      conv.id.toLowerCase().includes(query) ||
      conv.name?.toLowerCase().includes(query) ||
      conv.last_message?.toLowerCase().includes(query)
    );
  });

  // Create new conversation
  const handleNewConversation = () => {
    // Generate a new conversation ID
    const newId = crypto.randomUUID();
    setSelectedConversationId(newId);
  };

  return (
    <div className="h-full flex">
      {/* Left Panel: Conversations List */}
      <div className="w-72 border-r flex flex-col bg-muted/30">
        {/* Header */}
        <div className="p-4 border-b space-y-3">
          <div className="flex items-center justify-between">
            <h2 className="font-semibold flex items-center gap-2">
              <Sparkles className="h-4 w-4 text-purple-500" />
              Companion
            </h2>
            <Button variant="ghost" size="icon" onClick={handleNewConversation} title="New conversation">
              <Plus className="h-4 w-4" />
            </Button>
          </div>

          {/* Search */}
          <div className="relative">
            <Search className="absolute left-2 top-1/2 -translate-y-1/2 h-4 w-4 text-muted-foreground" />
            <Input
              value={searchQuery}
              onChange={(e) => setSearchQuery(e.target.value)}
              placeholder="Search conversations..."
              className="pl-8 h-8 text-sm"
            />
          </div>
        </div>

        {/* Conversations List */}
        <div className="flex-1 overflow-y-auto p-2 space-y-1">
          {isLoadingConversations ? (
            <div className="flex items-center justify-center py-8">
              <Loader2 className="h-5 w-5 animate-spin text-muted-foreground" />
            </div>
          ) : filteredConversations.length === 0 ? (
            <div className="text-center py-8 px-4">
              <MessageSquare className="h-8 w-8 mx-auto text-muted-foreground/50 mb-2" />
              <p className="text-sm text-muted-foreground">
                {searchQuery ? "No matching conversations" : "No conversations yet"}
              </p>
              <Button variant="link" size="sm" onClick={handleNewConversation} className="mt-2">
                Start a new conversation
              </Button>
            </div>
          ) : (
            filteredConversations.map((conv) => (
              <ConversationItem
                key={conv.id}
                conversation={conv}
                isSelected={selectedConversationId === conv.id}
                onClick={() => setSelectedConversationId(conv.id)}
              />
            ))
          )}
        </div>
      </div>

      {/* Main Chat Area */}
      <div className="flex-1 flex flex-col min-w-0">
        {selectedConversationId ? (
          <>
            {/* Chat Header */}
            <div className="h-14 border-b px-4 flex items-center justify-between shrink-0">
              <div className="flex items-center gap-2">
                <div className="h-8 w-8 rounded-full bg-gradient-to-br from-violet-500 to-purple-600 flex items-center justify-center">
                  <Sparkles className="h-4 w-4 text-white" />
                </div>
                <div>
                  <h3 className="font-medium text-sm">Companion Chat</h3>
                  <p className="text-xs text-muted-foreground font-mono">
                    {selectedConversationId.slice(0, 8)}...
                  </p>
                </div>
              </div>

              <Button
                variant="ghost"
                size="icon"
                onClick={() => setShowSidebar(!showSidebar)}
                title={showSidebar ? "Hide sidebar" : "Show sidebar"}
              >
                {showSidebar ? (
                  <PanelRightClose className="h-4 w-4" />
                ) : (
                  <PanelRight className="h-4 w-4" />
                )}
              </Button>
            </div>

            {/* Chat + Sidebar */}
            <div className="flex-1 flex min-h-0">
              {/* Chat */}
              <div className="flex-1 min-w-0">
                <CompanionChat
                  conversationId={selectedConversationId}
                  className="h-full"
                />
              </div>

              {/* Right Sidebar */}
              {showSidebar && (
                <div className="w-80 border-l overflow-y-auto p-4 space-y-4 bg-muted/20">
                  <MemoryStats conversationId={selectedConversationId} />
                  <ContextViewer conversationId={selectedConversationId} />
                </div>
              )}
            </div>
          </>
        ) : (
          // Empty state
          <div className="flex-1 flex items-center justify-center">
            <div className="text-center space-y-4 max-w-md px-4">
              <div className="h-16 w-16 mx-auto rounded-full bg-gradient-to-br from-violet-500/20 to-purple-600/20 flex items-center justify-center">
                <Sparkles className="h-8 w-8 text-purple-500" />
              </div>
              <div>
                <h2 className="text-xl font-semibold mb-2">Welcome to Companion</h2>
                <p className="text-muted-foreground">
                  Your AI companion remembers context across conversations using a tiered memory system.
                  Start a new conversation or select an existing one to continue.
                </p>
              </div>
              <div className="flex flex-col gap-2 items-center">
                <Button onClick={handleNewConversation}>
                  <Plus className="h-4 w-4 mr-2" />
                  New Conversation
                </Button>
                <p className="text-xs text-muted-foreground">
                  or select a conversation from the sidebar
                </p>
              </div>

              {/* Feature highlights */}
              <div className="grid grid-cols-3 gap-4 pt-4 border-t">
                <FeatureCard
                  title="L0: Vivid"
                  description="Recent conversation turns"
                  color="green"
                />
                <FeatureCard
                  title="L1: Summaries"
                  description="Daily compressed context"
                  color="blue"
                />
                <FeatureCard
                  title="L2: History"
                  description="Long-term relationship"
                  color="purple"
                />
              </div>
            </div>
          </div>
        )}
      </div>
    </div>
  );
}

// Conversation list item
interface ConversationItemProps {
  conversation: {
    id: string;
    name?: string;
    created_at: string;
    updated_at: string;
    message_count: number;
    last_message?: string;
  };
  isSelected: boolean;
  onClick: () => void;
}

function ConversationItem({ conversation, isSelected, onClick }: ConversationItemProps) {
  const displayName = conversation.name || conversation.id.slice(0, 8);
  const updatedAt = new Date(conversation.updated_at);
  const isToday = updatedAt.toDateString() === new Date().toDateString();
  const timeString = isToday
    ? updatedAt.toLocaleTimeString([], { hour: "2-digit", minute: "2-digit" })
    : updatedAt.toLocaleDateString([], { month: "short", day: "numeric" });

  return (
    <button
      onClick={onClick}
      aria-label={`Open conversation ${conversation.name || 'untitled'}`}
      className={cn(
        "w-full p-3 rounded-lg text-left transition-colors",
        isSelected
          ? "bg-primary text-primary-foreground"
          : "hover:bg-muted/50"
      )}
    >
      <div className="flex items-start justify-between gap-2">
        <div className="min-w-0 flex-1">
          <div className="flex items-center gap-2">
            <span className="font-medium text-sm truncate">{displayName}</span>
            {conversation.message_count > 0 && (
              <Badge
                variant={isSelected ? "secondary" : "outline"}
                className="text-xs h-4 shrink-0"
              >
                {conversation.message_count}
              </Badge>
            )}
          </div>
          {conversation.last_message && (
            <p
              className={cn(
                "text-xs truncate mt-0.5",
                isSelected ? "text-primary-foreground/70" : "text-muted-foreground"
              )}
            >
              {conversation.last_message}
            </p>
          )}
        </div>
        <span
          className={cn(
            "text-xs shrink-0",
            isSelected ? "text-primary-foreground/70" : "text-muted-foreground"
          )}
        >
          {timeString}
        </span>
      </div>
    </button>
  );
}

// Feature card for empty state
function FeatureCard({
  title,
  description,
  color,
}: {
  title: string;
  description: string;
  color: "green" | "blue" | "purple";
}) {
  const colorClasses = {
    green: "text-green-600",
    blue: "text-blue-600",
    purple: "text-purple-600",
  };

  return (
    <div className="text-center">
      <h4 className={cn("text-xs font-semibold", colorClasses[color])}>{title}</h4>
      <p className="text-xs text-muted-foreground mt-1">{description}</p>
    </div>
  );
}

export default ConsolePage;
