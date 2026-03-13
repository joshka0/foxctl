import React from "react";
import { ScrollArea } from "@/components/ui/scroll-area";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import { ChatInput } from "@/components/chat/ChatInput";
import {
  MessageBubble,
  TypingIndicator,
} from "@/components/chat/MessageBubble";
import type { Agent } from "@/api/types";
import type {
  ConsoleMessage,
  PersistedSession,
} from "@/api/client";
import type { Conversation } from "@/lib/conversation-list-models";
import { agentOptionLabel } from "@/lib/conversation-list-models";
import { getAgentDisplayName, getRoleIcon } from "@/lib/agent-utils";
import {
  Bot,
  Cpu,
  FileText,
  Folder,
  Hash,
  MessagesSquare,
  PanelRightClose,
  PanelRightOpen,
  Play,
  RefreshCw,
  Settings2,
  Sparkles,
} from "lucide-react";

interface ConversationWorkspaceProps {
  selectedConversation: Conversation | null;
  selectedPersistedSession: PersistedSession | null;
  linkedAgent: Agent | null;
  messages: ConsoleMessage[];
  inflight: boolean;
  isLoadingMessages: boolean;
  selectedMessage: ConsoleMessage | null;
  showContextPanel: boolean;
  sessionId: string | null;
  selectedAgentForNew: string;
  linkableAgents: Agent[];
  scrollRef: React.RefObject<HTMLDivElement | null>;
  onSelectMessage: (message: ConsoleMessage) => void;
  onDeleteMessage: (message: ConsoleMessage) => void;
  onToggleContextPanel: () => void;
  onStartHistoricalFollowUp: () => void;
  onSend: (message: string) => void;
  onCancel: () => void;
  onSelectedAgentForNewChange: (value: string) => void;
  onNewConversation: () => void;
  onNewConversationWithAgent: (agent: Agent) => void;
  onOpenRuntime: (agent?: Agent) => void;
}

export function ConversationWorkspace({
  selectedConversation,
  selectedPersistedSession,
  linkedAgent,
  messages,
  inflight,
  isLoadingMessages,
  selectedMessage,
  showContextPanel,
  sessionId,
  selectedAgentForNew,
  linkableAgents,
  scrollRef,
  onSelectMessage,
  onDeleteMessage,
  onToggleContextPanel,
  onStartHistoricalFollowUp,
  onSend,
  onCancel,
  onSelectedAgentForNewChange,
  onNewConversation,
  onNewConversationWithAgent,
  onOpenRuntime,
}: ConversationWorkspaceProps) {
  const isHistoricalSessionView = selectedPersistedSession !== null;
  const linkedRoleIcon = linkedAgent ? getRoleIcon(linkedAgent.role) : Bot;

  return (
    <div className="flex-1 flex flex-col min-w-0">
      {selectedConversation || selectedPersistedSession ? (
        <>
          <div className="border-b border-border">
            <div className="h-12 flex items-center justify-between px-4">
              <div className="flex items-center gap-3 min-w-0">
                {selectedPersistedSession ? (
                  <>
                    <div className="h-8 w-8 rounded-lg bg-blue-500/10 flex items-center justify-center flex-shrink-0">
                      <FileText className="h-4 w-4 text-blue-500" />
                    </div>
                    <div className="min-w-0">
                      <div className="flex items-center gap-2">
                        <span className="text-sm font-medium truncate">
                          {selectedPersistedSession.project_name ||
                            selectedPersistedSession.workspace_path
                              .split("/")
                              .pop() ||
                            "Historical Session"}
                        </span>
                        <Badge variant="outline" className="text-[10px]">
                          Historical Session
                        </Badge>
                      </div>
                      <div className="text-[10px] text-muted-foreground flex items-center gap-2">
                        <span>
                          {selectedPersistedSession.message_count} messages
                        </span>
                        <span>{selectedPersistedSession.status}</span>
                      </div>
                    </div>
                  </>
                ) : linkedAgent ? (
                  <>
                    <div className="h-8 w-8 rounded-lg bg-primary/10 flex items-center justify-center flex-shrink-0">
                      {React.createElement(linkedRoleIcon, {
                        className: "h-4 w-4 text-primary",
                      })}
                    </div>
                    <div className="min-w-0">
                      <div className="flex items-center gap-2">
                        <span className="text-sm font-medium truncate">
                          {getAgentDisplayName(linkedAgent)}
                        </span>
                        <Badge variant="outline" className="text-[10px]">
                          {linkedAgent.role || "agent"}
                        </Badge>
                      </div>
                      <div className="text-[10px] text-muted-foreground flex items-center gap-2">
                        <span className="flex items-center gap-0.5">
                          <Cpu className="h-2.5 w-2.5" />
                          {linkedAgent.llm_model || "default"}
                        </span>
                        {linkedAgent.ns && (
                          <span className="flex items-center gap-0.5">
                            <Folder className="h-2.5 w-2.5" />
                            {linkedAgent.ns.split("/").pop()}
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
                        {selectedConversation?.title ||
                          selectedConversation?.id.slice(0, 20)}
                      </span>
                      <div className="flex items-center gap-2 text-[10px] text-muted-foreground">
                        <span>{messages.length} messages</span>
                        {linkedAgent !== null && (
                          <Badge
                            variant="secondary"
                            className="text-[9px] bg-primary/10 text-primary"
                          >
                            {React.createElement(linkedRoleIcon, {
                              className: "h-2.5 w-2.5 mr-0.5",
                            })}
                            {getAgentDisplayName(linkedAgent as Agent)}
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
                {isHistoricalSessionView && (
                  <Button
                    variant="outline"
                    size="sm"
                    className="h-8 text-xs gap-1.5"
                    onClick={onStartHistoricalFollowUp}
                  >
                    <Play className="h-3.5 w-3.5" />
                    Start Follow-up
                  </Button>
                )}
                {!isHistoricalSessionView && (
                  <Button
                    variant="ghost"
                    size="icon"
                    className="h-8 w-8"
                    onClick={onToggleContextPanel}
                    title={showContextPanel ? "Hide context" : "Show context"}
                  >
                    {showContextPanel ? (
                      <PanelRightClose className="h-4 w-4" />
                    ) : (
                      <PanelRightOpen className="h-4 w-4" />
                    )}
                  </Button>
                )}
              </div>
            </div>

            {linkedAgent && !selectedPersistedSession && (
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

          <ScrollArea className="flex-1 p-4" ref={scrollRef}>
            {isLoadingMessages ? (
              <div className="flex items-center justify-center h-full">
                <RefreshCw className="h-6 w-6 animate-spin text-muted-foreground" />
              </div>
            ) : messages.length === 0 ? (
              <div className="flex flex-col items-center justify-center h-full text-muted-foreground">
                <Bot className="h-12 w-12 mb-3 opacity-30" />
                <p className="text-sm">
                  {isHistoricalSessionView
                    ? "No session messages found"
                    : "Start the conversation"}
                </p>
              </div>
            ) : (
              <div className="space-y-4">
                {messages.map((message, index) => (
                  <MessageBubble
                    key={message.id || index}
                    message={message}
                    isSelected={selectedMessage === message}
                    onSelect={(selected) => onSelectMessage(selected)}
                    onDelete={onDeleteMessage}
                  />
                ))}
                {inflight &&
                  messages[messages.length - 1]?.role !== "assistant" && (
                    <TypingIndicator />
                  )}
              </div>
            )}
          </ScrollArea>

          <div className="p-4 border-t border-border">
            <ChatInput
              onSend={onSend}
              onCancel={onCancel}
              disabled={isHistoricalSessionView || (!sessionId && !selectedConversation)}
              inflight={inflight}
            />
            {isHistoricalSessionView && (
              <p className="mt-2 text-[11px] text-muted-foreground">
                Historical sessions are read-only. Use “Start Follow-up” to continue in a new companion thread.
              </p>
            )}
          </div>
        </>
      ) : (
        <div className="flex-1 flex flex-col items-center justify-center text-muted-foreground">
          <div className="h-20 w-20 rounded-2xl bg-muted flex items-center justify-center mb-4">
            <MessagesSquare className="h-10 w-10 opacity-40" />
          </div>
          <h3 className="text-lg font-medium text-foreground mb-1">
            No conversation selected
          </h3>
          <p className="text-sm mb-4">
            Select a conversation or start a new one
          </p>

          <div className="w-64 mb-4">
            <label className="text-xs text-muted-foreground mb-1 block">
              Link to agent (optional)
            </label>
            <select
              value={selectedAgentForNew}
              onChange={(e) => onSelectedAgentForNewChange(e.target.value)}
              className="w-full h-9 rounded-md border border-input bg-background px-3 text-sm"
            >
              <option value="">No agent</option>
              {linkableAgents.map((agent) => (
                <option key={agent.id} value={agent.id}>
                  {agentOptionLabel(agent)}
                </option>
              ))}
            </select>
          </div>

          <div className="flex items-center gap-2">
            <Button
              onClick={() => {
                if (selectedAgentForNew) {
                  const agent = linkableAgents.find(
                    (candidate) => candidate.id === selectedAgentForNew,
                  );
                  if (agent) {
                    onNewConversationWithAgent(agent);
                    return;
                  }
                }
                onNewConversation();
              }}
            >
              <Plus className="h-4 w-4 mr-2" />
              New Conversation
            </Button>
            <Button
              variant="outline"
              onClick={() => {
                if (selectedAgentForNew) {
                  const agent = linkableAgents.find(
                    (candidate) => candidate.id === selectedAgentForNew,
                  );
                  onOpenRuntime(agent);
                  return;
                }
                onOpenRuntime();
              }}
            >
              Open Runtime
            </Button>
          </div>
        </div>
      )}
    </div>
  );
}
