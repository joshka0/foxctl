import React from "react";
import { ScrollArea } from "@/components/ui/scroll-area";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import { Input } from "@/components/ui/input";
import { CollapsibleSection } from "@/components/ui/collapsible-section";
import { cn, formatRelativeTime } from "@/lib/utils";
import type { Agent } from "@/api/types";
import type { PersistedSession } from "@/api/client";
import {
  getAgentDisplayName,
  getPromptSummaryOrSubtitle,
  getRoleIcon,
} from "@/lib/agent-utils";
import {
  agentOptionLabel,
  shortAgentID,
  type AgentConversationGroupData,
  type AgentSections,
  type Conversation,
  type FeedItem,
} from "@/lib/conversation-list-models";
import {
  Bot,
  Bug,
  Check,
  ChevronDown,
  ChevronRight,
  FileText,
  MessageCircle,
  MessagesSquare,
  Pencil,
  Play,
  Plus,
  RefreshCw,
  Search,
  Trash2,
  X,
} from "lucide-react";

interface AgentConversationGroupProps {
  agent: Agent;
  conversations: Conversation[];
  isExpanded: boolean;
  hasSelectedConversation: boolean;
  selectedConversationId?: string;
  agents: Agent[];
  editingConversationId: string | null;
  editTitle: string;
  onEditTitleChange: (value: string) => void;
  editLinkedAgentId: string;
  onEditLinkedAgentIdChange: (value: string) => void;
  onToggleExpanded: (agentId: string) => void;
  onSelectAgent: (agent: Agent) => void;
  onNewConversationWithAgent: (agent: Agent) => void;
  onSelectConversation: (conversation: Conversation) => void;
  onSaveRename: (
    e: React.MouseEvent | React.KeyboardEvent,
    conversationId: string,
  ) => void;
  onCancelRename: (e: React.MouseEvent | React.KeyboardEvent) => void;
  onStartRename: (e: React.MouseEvent, conversation: Conversation) => void;
  onDeleteConversation: (e: React.MouseEvent, conversationId: string) => void;
}

function AgentConversationGroup({
  agent,
  conversations,
  isExpanded,
  hasSelectedConversation,
  selectedConversationId,
  agents,
  editingConversationId,
  editTitle,
  onEditTitleChange,
  editLinkedAgentId,
  onEditLinkedAgentIdChange,
  onToggleExpanded,
  onSelectAgent,
  onNewConversationWithAgent,
  onSelectConversation,
  onSaveRename,
  onCancelRename,
  onStartRename,
  onDeleteConversation,
}: AgentConversationGroupProps) {
  return (
    <div>
      <div
        className={cn(
          "flex items-center gap-2 px-2 py-2 rounded-lg cursor-pointer transition-colors group",
          "hover:bg-accent/50",
          hasSelectedConversation && "bg-accent/30",
        )}
        onClick={() => {
          onToggleExpanded(agent.id);
          onSelectAgent(agent);
        }}
      >
        <div className="flex-shrink-0 w-4 h-4 flex items-center justify-center">
          {isExpanded ? (
            <ChevronDown className="h-3.5 w-3.5 text-muted-foreground" />
          ) : (
            <ChevronRight className="h-3.5 w-3.5 text-muted-foreground" />
          )}
        </div>

        <div
          className={cn(
            "h-7 w-7 rounded-lg flex items-center justify-center flex-shrink-0",
            agent.state === "running" ? "bg-green-500/20" : "bg-primary/10",
          )}
        >
          {React.createElement(getRoleIcon(agent.role), {
            className: cn(
              "h-3.5 w-3.5",
              agent.state === "running" ? "text-green-500" : "text-primary",
            ),
          })}
        </div>

        <div className="flex-1 min-w-0">
          <div className="flex items-center gap-1.5">
            <span className="text-sm font-medium truncate">
              {getAgentDisplayName(agent)}
            </span>
            <Badge
              variant="outline"
              className="text-[9px] px-1 py-0 font-mono flex-shrink-0"
            >
              {shortAgentID(agent.id)}
            </Badge>
            <Badge
              variant="secondary"
              className="text-[9px] px-1 py-0 flex-shrink-0"
            >
              {conversations.length}
            </Badge>
            {agent.state === "running" && (
              <span className="h-1.5 w-1.5 rounded-full bg-green-500 animate-pulse" />
            )}
          </div>
          <div className="text-[10px] text-muted-foreground truncate">
            {getPromptSummaryOrSubtitle(agent)}
          </div>
        </div>

        <Button
          variant="ghost"
          size="icon"
          className="h-6 w-6 opacity-0 group-hover:opacity-100 transition-opacity flex-shrink-0"
          onClick={(e) => {
            e.stopPropagation();
            onNewConversationWithAgent(agent);
          }}
          title="New chat with this agent"
        >
          <Plus className="h-3.5 w-3.5" />
        </Button>
      </div>

      {isExpanded && (
        <div className="ml-6 pl-2 border-l border-border/50 space-y-0.5 mt-0.5">
          {conversations.length === 0 && (
            <div
              className="flex items-center gap-2 px-2 py-1.5 rounded-md cursor-pointer text-muted-foreground hover:text-foreground hover:bg-accent/50 transition-colors"
              onClick={(e) => {
                e.stopPropagation();
                onNewConversationWithAgent(agent);
              }}
            >
              <Plus className="h-3 w-3" />
              <span className="text-xs">Start a conversation</span>
            </div>
          )}
          {conversations.map((conversation) => (
            <div
              key={conversation.id}
              className={cn(
                "flex items-center gap-2 px-2 py-1.5 rounded-md cursor-pointer transition-colors group",
                "hover:bg-accent/50",
                selectedConversationId === conversation.id &&
                  "bg-accent border-l-2 border-primary -ml-0.5 pl-2.5",
              )}
              onClick={() => onSelectConversation(conversation)}
            >
              <MessageCircle className="h-3 w-3 text-muted-foreground flex-shrink-0" />
              <div className="flex-1 min-w-0">
                {editingConversationId === conversation.id ? (
                  <div
                    className="space-y-1"
                    onClick={(e) => e.stopPropagation()}
                  >
                    <Input
                      value={editTitle}
                      onChange={(e) => onEditTitleChange(e.target.value)}
                      onKeyDown={(e) => {
                        if (e.key === "Enter") onSaveRename(e, conversation.id);
                        if (e.key === "Escape") onCancelRename(e);
                      }}
                      className="h-5 text-xs py-0 px-1"
                      placeholder="Title..."
                      autoFocus
                    />
                    <div className="flex items-center gap-1">
                      <select
                        value={editLinkedAgentId}
                        onChange={(e) =>
                          onEditLinkedAgentIdChange(e.target.value)
                        }
                        className="flex-1 h-5 text-[10px] bg-muted border border-border rounded px-1"
                      >
                        <option value="">No agent</option>
                        {agents.map((candidate) => (
                          <option key={candidate.id} value={candidate.id}>
                            {agentOptionLabel(candidate)}
                          </option>
                        ))}
                      </select>
                      <Button
                        variant="ghost"
                        size="icon"
                        className="h-5 w-5"
                        onClick={(e) => onSaveRename(e, conversation.id)}
                      >
                        <Check className="h-3 w-3 text-green-500" />
                      </Button>
                      <Button
                        variant="ghost"
                        size="icon"
                        className="h-5 w-5"
                        onClick={onCancelRename}
                      >
                        <X className="h-3 w-3" />
                      </Button>
                    </div>
                  </div>
                ) : (
                  <div className="flex items-center gap-1.5">
                    <span className="text-xs truncate">
                      {conversation.name || conversation.id.slice(0, 12)}
                    </span>
                    <Badge
                      variant="secondary"
                      className="text-[9px] px-1 py-0 flex-shrink-0"
                    >
                      {conversation.message_count}
                    </Badge>
                  </div>
                )}
              </div>
              {editingConversationId !== conversation.id && (
                <div className="flex items-center gap-0.5 opacity-0 group-hover:opacity-100 transition-opacity flex-shrink-0">
                  <Button
                    variant="ghost"
                    size="icon"
                    className="h-5 w-5"
                    onClick={(e) => onStartRename(e, conversation)}
                    title="Rename"
                  >
                    <Pencil className="h-2.5 w-2.5 text-muted-foreground" />
                  </Button>
                  <Button
                    variant="ghost"
                    size="icon"
                    className="h-5 w-5"
                    onClick={(e) => onDeleteConversation(e, conversation.id)}
                    title="Delete"
                  >
                    <Trash2 className="h-2.5 w-2.5 text-muted-foreground" />
                  </Button>
                </div>
              )}
            </div>
          ))}
        </div>
      )}
    </div>
  );
}

function CompanionFeedRow({
  conversation,
  agent,
  selected,
  onClick,
}: {
  conversation: Conversation;
  agent?: Agent;
  selected: boolean;
  onClick: () => void;
}) {
  return (
    <button
      type="button"
      className={cn(
        "w-full text-left flex items-center gap-2 px-2 py-2 rounded-md transition-colors",
        "hover:bg-accent/50",
        selected && "bg-accent border-l-2 border-primary",
      )}
      onClick={onClick}
    >
      <MessageCircle className="h-3.5 w-3.5 text-muted-foreground flex-shrink-0" />
      <div className="flex-1 min-w-0">
        <div className="flex items-center gap-1.5">
          <span className="text-xs font-medium truncate">
            {conversation.name ||
              conversation.title ||
              conversation.id.slice(0, 16)}
          </span>
          <Badge
            variant="secondary"
            className="text-[9px] px-1 py-0 flex-shrink-0"
          >
            {conversation.message_count}
          </Badge>
        </div>
        <div className="text-[10px] text-muted-foreground truncate">
          {agent
            ? `${getAgentDisplayName(agent)} · #${shortAgentID(agent.id)} · ${
                agent.state
              }`
            : "Companion chat"}{" "}
          •{" "}
          {formatRelativeTime(conversation.updated_at)}
        </div>
      </div>
    </button>
  );
}

function SessionFeedRow({
  session,
  selected,
  onClick,
}: {
  session: PersistedSession;
  selected: boolean;
  onClick: () => void;
}) {
  const title =
    session.project_name ||
    session.workspace_path.split("/").pop() ||
    "Session";
  return (
    <button
      type="button"
      className={cn(
        "w-full text-left flex items-center gap-2 px-2 py-2 rounded-md transition-colors",
        "hover:bg-accent/50",
        selected && "bg-accent border-l-2 border-primary",
      )}
      onClick={onClick}
    >
      <FileText className="h-3.5 w-3.5 text-blue-500 flex-shrink-0" />
      <div className="flex-1 min-w-0">
        <div className="flex items-center gap-1.5">
          <span className="text-xs font-medium truncate">{title}</span>
          <Badge
            variant="secondary"
            className="text-[9px] px-1 py-0 flex-shrink-0"
          >
            {session.message_count}
          </Badge>
        </div>
        <div className="text-[10px] text-muted-foreground truncate">
          {session.summary || session.status || "Historical session"} •{" "}
          {formatRelativeTime(session.started_at)}
        </div>
      </div>
    </button>
  );
}

interface ConversationListSidebarProps {
  isLoading: boolean;
  isFetching: boolean;
  searchQuery: string;
  onSearchQueryChange: (value: string) => void;
  onNewConversation: () => void;
  onRefresh: () => void;
  agentSections: AgentSections;
  groupedConversations: { agentGroups: AgentConversationGroupData[] };
  expandedAgents: Set<string>;
  selectedConversationId?: string;
  linkableAgents: Agent[];
  editingConversationId: string | null;
  editTitle: string;
  onEditTitleChange: (value: string) => void;
  editLinkedAgentId: string;
  onEditLinkedAgentIdChange: (value: string) => void;
  onToggleExpanded: (agentId: string) => void;
  onSelectAgent: (agent: Agent) => void;
  onNewConversationWithAgent: (agent: Agent) => void;
  onSelectConversation: (conversation: Conversation) => void;
  onSaveRename: (
    e: React.MouseEvent | React.KeyboardEvent,
    conversationId: string,
  ) => void;
  onCancelRename: (e: React.MouseEvent | React.KeyboardEvent) => void;
  onStartRename: (e: React.MouseEvent, conversation: Conversation) => void;
  onDeleteConversation: (e: React.MouseEvent, conversationId: string) => void;
  feedItems: FeedItem[];
  selectedPersistedSessionId?: string;
  onSelectSession: (session: PersistedSession) => void;
}

export function ConversationListSidebar({
  isLoading,
  isFetching,
  searchQuery,
  onSearchQueryChange,
  onNewConversation,
  onRefresh,
  agentSections,
  groupedConversations,
  expandedAgents,
  selectedConversationId,
  linkableAgents,
  editingConversationId,
  editTitle,
  onEditTitleChange,
  editLinkedAgentId,
  onEditLinkedAgentIdChange,
  onToggleExpanded,
  onSelectAgent,
  onNewConversationWithAgent,
  onSelectConversation,
  onSaveRename,
  onCancelRename,
  onStartRename,
  onDeleteConversation,
  feedItems,
  selectedPersistedSessionId,
  onSelectSession,
}: ConversationListSidebarProps) {
  return (
    <div className="w-80 border-r border-border flex flex-col">
      <div className="p-3 border-b border-border space-y-2">
        <div className="flex items-center justify-between">
          <div className="flex items-center gap-2">
            <MessagesSquare className="h-4 w-4" />
            <h2 className="text-sm font-semibold text-foreground">
              Conversations
            </h2>
          </div>
          <div className="flex items-center gap-1">
            <Button
              variant="ghost"
              size="icon"
              onClick={onNewConversation}
              className="h-7 w-7"
              title="New conversation"
            >
              <Plus className="h-4 w-4" />
            </Button>
            <Button
              variant="ghost"
              size="icon"
              onClick={onRefresh}
              disabled={isFetching}
              className="h-7 w-7"
            >
              <RefreshCw
                className={cn("h-4 w-4", isFetching && "animate-spin")}
              />
            </Button>
          </div>
        </div>

        <div className="relative">
          <Search className="absolute left-2 top-2 h-3.5 w-3.5 text-muted-foreground" />
          <Input
            placeholder="Search..."
            value={searchQuery}
            onChange={(e) => onSearchQueryChange(e.target.value)}
            className="pl-7 h-8 text-sm"
          />
        </div>
      </div>

      <ScrollArea className="flex-1">
        <div className="p-2 space-y-1">
          {isLoading ? (
            <div className="text-center py-8 text-muted-foreground">
              <RefreshCw className="h-5 w-5 mx-auto mb-2 animate-spin" />
              <p className="text-xs">Loading...</p>
            </div>
          ) : agentSections.active.length === 0 &&
            agentSections.errored.length === 0 &&
            feedItems.length === 0 ? (
            <div className="text-center py-8 text-muted-foreground">
              <Bot className="h-8 w-8 mx-auto mb-2 opacity-40" />
              <p className="text-sm">
                {searchQuery ? "No matches" : "No agents yet"}
              </p>
              <Button
                variant="outline"
                size="sm"
                className="mt-2"
                onClick={onNewConversation}
              >
                <Plus className="h-3 w-3 mr-1" />
                New Chat
              </Button>
            </div>
          ) : (
            <>
              {agentSections.active.length > 0 && (
                <CollapsibleSection
                  title="Active"
                  icon={<Play className="h-3.5 w-3.5" />}
                  defaultOpen
                  badge={String(agentSections.active.length)}
                >
                  <div className="space-y-1">
                    {agentSections.active.map((agent) => (
                      <AgentConversationGroup
                        key={agent.id}
                        agent={agent}
                        conversations={
                          groupedConversations.agentGroups.find(
                            (group) => group.agent.id === agent.id,
                          )?.conversations || []
                        }
                        isExpanded={expandedAgents.has(agent.id)}
                        hasSelectedConversation={(
                          groupedConversations.agentGroups.find(
                            (group) => group.agent.id === agent.id,
                          )?.conversations || []
                        ).some((conversation) => conversation.id === selectedConversationId)}
                        selectedConversationId={selectedConversationId}
                        agents={linkableAgents}
                        editingConversationId={editingConversationId}
                        editTitle={editTitle}
                        onEditTitleChange={onEditTitleChange}
                        editLinkedAgentId={editLinkedAgentId}
                        onEditLinkedAgentIdChange={onEditLinkedAgentIdChange}
                        onToggleExpanded={onToggleExpanded}
                        onSelectAgent={onSelectAgent}
                        onNewConversationWithAgent={onNewConversationWithAgent}
                        onSelectConversation={onSelectConversation}
                        onSaveRename={onSaveRename}
                        onCancelRename={onCancelRename}
                        onStartRename={onStartRename}
                        onDeleteConversation={onDeleteConversation}
                      />
                    ))}
                  </div>
                </CollapsibleSection>
              )}

              {agentSections.errored.length > 0 && (
                <CollapsibleSection
                  title="Errors"
                  icon={<Bug className="h-3.5 w-3.5" />}
                  defaultOpen
                  badge={String(agentSections.errored.length)}
                >
                  <div className="space-y-1">
                    {agentSections.errored.map((agent) => (
                      <AgentConversationGroup
                        key={agent.id}
                        agent={agent}
                        conversations={
                          groupedConversations.agentGroups.find(
                            (group) => group.agent.id === agent.id,
                          )?.conversations || []
                        }
                        isExpanded={expandedAgents.has(agent.id)}
                        hasSelectedConversation={(
                          groupedConversations.agentGroups.find(
                            (group) => group.agent.id === agent.id,
                          )?.conversations || []
                        ).some((conversation) => conversation.id === selectedConversationId)}
                        selectedConversationId={selectedConversationId}
                        agents={linkableAgents}
                        editingConversationId={editingConversationId}
                        editTitle={editTitle}
                        onEditTitleChange={onEditTitleChange}
                        editLinkedAgentId={editLinkedAgentId}
                        onEditLinkedAgentIdChange={onEditLinkedAgentIdChange}
                        onToggleExpanded={onToggleExpanded}
                        onSelectAgent={onSelectAgent}
                        onNewConversationWithAgent={onNewConversationWithAgent}
                        onSelectConversation={onSelectConversation}
                        onSaveRename={onSaveRename}
                        onCancelRename={onCancelRename}
                        onStartRename={onStartRename}
                        onDeleteConversation={onDeleteConversation}
                      />
                    ))}
                  </div>
                </CollapsibleSection>
              )}

              <CollapsibleSection
                title="All Conversations"
                icon={<MessagesSquare className="h-3.5 w-3.5" />}
                defaultOpen
                badge={String(feedItems.length)}
              >
                <div className="space-y-1">
                  {feedItems.length === 0 ? (
                    <div className="px-2 py-2 text-xs text-muted-foreground">
                      {searchQuery
                        ? "No matching conversations"
                        : "No conversations with messages yet"}
                    </div>
                  ) : (
                    feedItems.map((item) =>
                      item.kind === "companion" ? (
                        <CompanionFeedRow
                          key={`companion-${item.conversation.id}`}
                          conversation={item.conversation}
                          agent={item.agent}
                          selected={selectedConversationId === item.conversation.id}
                          onClick={() => onSelectConversation(item.conversation)}
                        />
                      ) : (
                        <SessionFeedRow
                          key={`session-${item.session.id}`}
                          session={item.session}
                          selected={selectedPersistedSessionId === item.session.id}
                          onClick={() => onSelectSession(item.session)}
                        />
                      ),
                    )
                  )}
                </div>
              </CollapsibleSection>
            </>
          )}
        </div>
      </ScrollArea>
    </div>
  );
}
