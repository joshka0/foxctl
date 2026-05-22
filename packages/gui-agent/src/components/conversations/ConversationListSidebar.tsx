import React from "react";
import { ScrollArea } from "@/components/ui/scroll-area";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import { Input } from "@/components/ui/input";
import { CollapsibleSection } from "@/components/ui/collapsible-section";
import { HelpTooltip, Tooltip } from "@/components/ui/tooltip";
import { cn, formatRelativeTime } from "@/lib/utils";
import type { Agent } from '@foxctl/data/types';
import type { PersistedSession } from "@/api/client";
import {
  getAgentDisplayName,
  getPromptSummaryOrSubtitle,
  getRoleIcon,
} from "@/lib/agent-utils";
import {
  agentOptionLabel,
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

const ACTIVE_CONVERSATION_WINDOW_MS = 15 * 60 * 1000;
const RECENT_CONVERSATION_WINDOW_MS = 2 * 60 * 60 * 1000;

type ConversationActivityState = "active" | "recent" | "quiet";

function conversationTimestamp(conversation: Conversation): number {
  return (
    Date.parse(conversation.updated_at || "") ||
    Date.parse(conversation.created_at || "") ||
    0
  );
}

function getConversationActivityState(
  conversation?: Conversation | null,
): ConversationActivityState {
  if (!conversation) return "quiet";

  const ageMs = Date.now() - conversationTimestamp(conversation);
  if (ageMs <= ACTIVE_CONVERSATION_WINDOW_MS) return "active";
  if (ageMs <= RECENT_CONVERSATION_WINDOW_MS) return "recent";
  return "quiet";
}

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
  const latestConversation = conversations[0];
  const latestConversationLabel = latestConversation
    ? latestConversation.name ||
      latestConversation.title ||
      latestConversation.id.slice(0, 12)
    : null;
  const latestConversationAge = latestConversation
    ? formatRelativeTime(
        latestConversation.updated_at || latestConversation.created_at,
      )
    : null;
  const latestConversationActivity = getConversationActivityState(
    latestConversation,
  );
  const hasFreshConversation = latestConversationActivity !== "quiet";

  return (
    <div>
      <div
        className={cn(
          "flex items-center gap-2 px-2 py-1.5 rounded-lg cursor-pointer transition-colors group",
          "hover:bg-accent/50",
          hasFreshConversation &&
            !hasSelectedConversation &&
            latestConversationActivity === "active" &&
            "bg-emerald-500/5",
          hasFreshConversation &&
            !hasSelectedConversation &&
            latestConversationActivity === "recent" &&
            "bg-amber-500/5",
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
              variant="secondary"
              className="text-[9px] px-1 py-0 flex-shrink-0"
            >
              {conversations.length}
            </Badge>
            {latestConversationActivity !== "quiet" && (
              <Badge
                variant="outline"
                className={cn(
                  "text-[9px] px-1 py-0 flex-shrink-0",
                  latestConversationActivity === "active" &&
                    "border-emerald-500/30 bg-emerald-500/10 text-emerald-700 dark:text-emerald-400",
                  latestConversationActivity === "recent" &&
                    "border-amber-500/30 bg-amber-500/10 text-amber-700 dark:text-amber-400",
                )}
              >
                {latestConversationActivity === "active" ? "Active" : "Recent"}
              </Badge>
            )}
            {agent.state === "running" && (
              <span className="h-1.5 w-1.5 rounded-full bg-green-500 animate-pulse" />
            )}
          </div>
          <div className="text-[10px] text-muted-foreground truncate">
            {latestConversationLabel
              ? `Latest: ${latestConversationLabel}`
              : getPromptSummaryOrSubtitle(agent)}
          </div>
        </div>

        {latestConversationAge && (
          <div
            className={cn(
              "text-[10px] flex-shrink-0",
              latestConversationActivity === "active" &&
                "text-emerald-700 dark:text-emerald-400",
              latestConversationActivity === "recent" &&
                "text-amber-700 dark:text-amber-400",
              latestConversationActivity === "quiet" && "text-muted-foreground",
            )}
          >
            {latestConversationAge}
          </div>
        )}

        <Tooltip content="Start a new chat thread linked to this agent.">
          <Button
            variant="ghost"
            size="icon"
            className="h-6 w-6 opacity-0 group-hover:opacity-100 transition-opacity flex-shrink-0"
            onClick={(e) => {
              e.stopPropagation();
              onNewConversationWithAgent(agent);
            }}
          >
            <Plus className="h-3.5 w-3.5" />
          </Button>
        </Tooltip>
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
          {conversations.map((conversation, index) => {
            const conversationActivity = getConversationActivityState(
              conversation,
            );
            const isLatestConversation = index === 0;

            return (
              <div
                key={conversation.id}
                className={cn(
                  "flex items-center gap-2 px-2 py-1 rounded-md cursor-pointer transition-colors group",
                  "hover:bg-accent/50",
                  selectedConversationId === conversation.id &&
                    "bg-accent border-l-2 border-primary -ml-0.5 pl-2.5",
                )}
                onClick={() => onSelectConversation(conversation)}
              >
                <span
                  className={cn(
                    "h-1.5 w-1.5 rounded-full flex-shrink-0",
                    isLatestConversation &&
                      conversationActivity === "active" &&
                      "bg-emerald-500",
                    isLatestConversation &&
                      conversationActivity === "recent" &&
                      "bg-amber-500",
                    (!isLatestConversation ||
                      conversationActivity === "quiet") &&
                      "bg-border",
                  )}
                />
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
                          if (e.key === "Enter")
                            onSaveRename(e, conversation.id);
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
                    <div className="min-w-0 flex items-center gap-2">
                      <span
                        className={cn(
                          "text-xs truncate flex-1 min-w-0",
                          isLatestConversation && "font-medium",
                        )}
                      >
                        {conversation.name ||
                          conversation.title ||
                          conversation.id.slice(0, 12)}
                      </span>
                      <div className="flex items-center gap-1.5 text-[10px] flex-shrink-0">
                        <span
                          className={cn(
                            conversationActivity === "active" &&
                              "text-emerald-700 dark:text-emerald-400",
                            conversationActivity === "recent" &&
                              "text-amber-700 dark:text-amber-400",
                            conversationActivity === "quiet" &&
                              "text-muted-foreground",
                          )}
                        >
                          {formatRelativeTime(
                            conversation.updated_at || conversation.created_at,
                          )}
                        </span>
                        <Badge
                          variant="secondary"
                          className="text-[9px] px-1 py-0 flex-shrink-0"
                        >
                          {conversation.message_count}
                        </Badge>
                      </div>
                    </div>
                  )}
                </div>
                {editingConversationId !== conversation.id && (
                  <div className="flex items-center gap-0.5 opacity-0 group-hover:opacity-100 transition-opacity flex-shrink-0">
                    <Tooltip content="Rename this conversation or change its linked agent.">
                      <Button
                        variant="ghost"
                        size="icon"
                        className="h-5 w-5"
                        onClick={(e) => onStartRename(e, conversation)}
                      >
                        <Pencil className="h-2.5 w-2.5 text-muted-foreground" />
                      </Button>
                    </Tooltip>
                    <Tooltip content="Delete this conversation from the sidebar list.">
                      <Button
                        variant="ghost"
                        size="icon"
                        className="h-5 w-5"
                        onClick={(e) => onDeleteConversation(e, conversation.id)}
                      >
                        <Trash2 className="h-2.5 w-2.5 text-muted-foreground" />
                      </Button>
                    </Tooltip>
                  </div>
                )}
              </div>
            );
          })}
        </div>
      )}
    </div>
  );
}

function CompanionFeedRow({
  conversation,
  selected,
  onClick,
}: {
  conversation: Conversation;
  selected: boolean;
  onClick: () => void;
}) {
  return (
    <button
      type="button"
      className={cn(
        "w-full text-left flex items-center gap-2 px-2 py-1.5 rounded-md transition-colors",
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
          No agent link •{" "}
          {formatRelativeTime(conversation.updated_at || conversation.created_at)}
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
        "w-full text-left flex items-center gap-2 px-2 py-1.5 rounded-md transition-colors",
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
  const agentGroupsByID = new Map(
    groupedConversations.agentGroups.map((group) => [group.agent.id, group]),
  );
  const activeAgentGroups = agentSections.active
    .map((agent) => agentGroupsByID.get(agent.id))
    .filter(
      (group): group is AgentConversationGroupData =>
        Boolean(group && group.conversations.length > 0),
    );
  const erroredAgentGroups = agentSections.errored
    .map((agent) => agentGroupsByID.get(agent.id))
    .filter(
      (group): group is AgentConversationGroupData =>
        Boolean(group && group.conversations.length > 0),
    );
  const groupedConversationIDs = new Set(
    groupedConversations.agentGroups.flatMap((group) =>
      group.conversations.map((conversation) => conversation.id),
    ),
  );
  const unassignedCompanionItems = feedItems.filter(
    (item): item is Extract<FeedItem, { kind: "companion" }> =>
      item.kind === "companion" &&
      !groupedConversationIDs.has(item.conversation.id),
  );
  const historicalSessionItems = feedItems.filter(
    (item): item is Extract<FeedItem, { kind: "session" }> =>
      item.kind === "session",
  );
  const hiddenAgentCount =
    agentSections.active.length +
    agentSections.errored.length -
    activeAgentGroups.length -
    erroredAgentGroups.length;

  return (
    <div className="w-80 border-r border-border flex flex-col">
      <div className="p-2.5 border-b border-border space-y-2">
        <div className="flex items-center justify-between">
          <div className="flex items-center gap-2">
            <MessagesSquare className="h-4 w-4" />
            <div className="min-w-0">
              <div className="flex items-center gap-1.5">
                <h2 className="text-sm font-semibold text-foreground">
                  Companion
                </h2>
                <HelpTooltip
                  side="bottom"
                  content="Companion groups conversations by agent so you can jump between active chats, unassigned threads, and historical sessions."
                />
              </div>
              <p className="text-[10px] text-muted-foreground">
                Grouped by agent
              </p>
            </div>
          </div>
          <div className="flex items-center gap-1">
            <Tooltip content="Start a new companion chat and link it to an agent later if needed.">
              <Button
                variant="ghost"
                size="icon"
                onClick={onNewConversation}
                className="h-7 w-7"
              >
                <Plus className="h-4 w-4" />
              </Button>
            </Tooltip>
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
            placeholder="Search agents or chats..."
            value={searchQuery}
            onChange={(e) => onSearchQueryChange(e.target.value)}
            className="pl-7 h-8 text-sm"
          />
        </div>
        <div className="flex flex-wrap gap-1.5">
          <Badge variant="secondary" className="text-[10px]">
            {groupedConversations.agentGroups.length} agent groups
          </Badge>
          {unassignedCompanionItems.length > 0 && (
            <Badge variant="outline" className="text-[10px]">
              {unassignedCompanionItems.length} without agent link
            </Badge>
          )}
          {historicalSessionItems.length > 0 && (
            <Badge variant="outline" className="text-[10px]">
              {historicalSessionItems.length} historical
            </Badge>
          )}
          {hiddenAgentCount > 0 && (
            <Badge variant="outline" className="text-[10px]">
              {hiddenAgentCount} agents without visible chats
            </Badge>
          )}
        </div>
      </div>

      <ScrollArea className="flex-1">
        <div className="p-2 space-y-0.5">
          {isLoading ? (
            <div className="text-center py-8 text-muted-foreground">
              <RefreshCw className="h-5 w-5 mx-auto mb-2 animate-spin" />
              <p className="text-xs">Loading...</p>
            </div>
          ) : activeAgentGroups.length === 0 &&
            erroredAgentGroups.length === 0 &&
            feedItems.length === 0 ? (
            <div className="text-center py-8 text-muted-foreground">
              <Bot className="h-8 w-8 mx-auto mb-2 opacity-40" />
              <p className="text-sm">
                {searchQuery ? "No agent groups in this view" : "No agent chats yet"}
              </p>
              <Button
                variant="outline"
                size="sm"
                className="mt-2"
                onClick={onNewConversation}
              >
                <Plus className="h-3 w-3 mr-1" />
                New Agent Chat
              </Button>
            </div>
          ) : (
            <>
              {activeAgentGroups.length > 0 && (
                <CollapsibleSection
                  title="Active Agent Groups"
                  icon={
                    <Tooltip content="Agents with chats that are currently active or recently active.">
                      <Play className="h-3.5 w-3.5" />
                    </Tooltip>
                  }
                  defaultOpen
                  badge={String(activeAgentGroups.length)}
                >
                  <div className="space-y-0.5">
                    {activeAgentGroups.map((group) => (
                      <AgentConversationGroup
                        key={group.agent.id}
                        agent={group.agent}
                        conversations={group.conversations}
                        isExpanded={expandedAgents.has(group.agent.id)}
                        hasSelectedConversation={group.conversations.some(
                          (conversation) =>
                            conversation.id === selectedConversationId,
                        )}
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

              {erroredAgentGroups.length > 0 && (
                <CollapsibleSection
                  title="Error-State Agent Groups"
                  icon={
                    <Tooltip content="Agents whose linked chats are associated with an agent currently in an error state.">
                      <Bug className="h-3.5 w-3.5" />
                    </Tooltip>
                  }
                  defaultOpen
                  badge={String(erroredAgentGroups.length)}
                >
                  <div className="space-y-0.5">
                    {erroredAgentGroups.map((group) => (
                      <AgentConversationGroup
                        key={group.agent.id}
                        agent={group.agent}
                        conversations={group.conversations}
                        isExpanded={expandedAgents.has(group.agent.id)}
                        hasSelectedConversation={group.conversations.some(
                          (conversation) =>
                            conversation.id === selectedConversationId,
                        )}
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
                title="Unassigned / No Agent Link"
                icon={
                  <Tooltip content="Chats that exist but are not currently linked to a specific agent.">
                    <MessagesSquare className="h-3.5 w-3.5" />
                  </Tooltip>
                }
                defaultOpen={unassignedCompanionItems.length > 0}
                badge={String(unassignedCompanionItems.length)}
              >
                <div className="space-y-0.5">
                  {unassignedCompanionItems.length === 0 ? (
                    <div className="px-2 py-2 text-xs text-muted-foreground">
                      {searchQuery
                        ? "No unassigned chats in this view"
                        : "No chats without an agent link in this view"}
                    </div>
                  ) : (
                    unassignedCompanionItems.map((item) => (
                      <CompanionFeedRow
                        key={`companion-${item.conversation.id}`}
                        conversation={item.conversation}
                        selected={selectedConversationId === item.conversation.id}
                        onClick={() => onSelectConversation(item.conversation)}
                      />
                    ))
                  )}
                </div>
              </CollapsibleSection>

              <CollapsibleSection
                title="Historical Sessions"
                icon={
                  <Tooltip content="Older persisted sessions that are not part of the current active companion chat list.">
                    <FileText className="h-3.5 w-3.5" />
                  </Tooltip>
                }
                defaultOpen={historicalSessionItems.length > 0}
                badge={String(historicalSessionItems.length)}
              >
                <div className="space-y-0.5">
                  {historicalSessionItems.length === 0 ? (
                    <div className="px-2 py-2 text-xs text-muted-foreground">
                      {searchQuery
                        ? "No matching historical sessions"
                        : "No historical sessions in this view"}
                    </div>
                  ) : (
                    historicalSessionItems.map((item) => (
                      <SessionFeedRow
                        key={`session-${item.session.id}`}
                        session={item.session}
                        selected={selectedPersistedSessionId === item.session.id}
                        onClick={() => onSelectSession(item.session)}
                      />
                    ))
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
