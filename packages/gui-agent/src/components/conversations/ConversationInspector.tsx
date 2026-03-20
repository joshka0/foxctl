import React from "react";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import { Card } from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import { Textarea } from "@/components/ui/textarea";
import { Slider } from "@/components/ui/slider";
import { ScrollArea } from "@/components/ui/scroll-area";
import { CollapsibleSection } from "@/components/ui/collapsible-section";
import { HelpTooltip, Tooltip } from "@/components/ui/tooltip";
import { ToolAllowlistEditor } from "@/components/conversations/ToolAllowlistEditor";
import type {
  CompanionCompressionResult,
  CompanionMemoryStats,
  ConsoleMessage,
  ConversationSettings,
  ConversationSettingsPatch,
  PersonalityInfo,
} from "@/api/client";
import type { AgentSession } from "@/api/types";
import type { Conversation } from "@/lib/conversation-list-models";
import type { ContextInfo, ExecMode } from "@/components/conversations/types";
import { useAgentOperations } from "@/hooks/useAgentOperations";
import { cn, formatRelativeTime } from "@/lib/utils";
import { getAgentDisplayName, getRoleIcon } from "@/lib/agent-utils";
import {
  PROVIDERS,
  getModelsForProvider,
  COMPANION_TOOL_MODELS,
  COMPANION_RESPONSE_MODELS,
} from "@/components/agents/spawnFormConstants";
import {
  Activity,
  Brain,
  Bug,
  Clock,
  Coins,
  Cpu,
  Folder,
  MessageCircle,
  Pencil,
  Play,
  RotateCcw,
  Save,
  Search,
  Settings2,
  Sliders,
  Sparkles,
  Square,
  Trash2,
  X,
  Zap,
} from "lucide-react";

interface ConversationInspectorProps {
  selectedConversation: Conversation;
  onClose: () => void;
  agentSectionOpen: boolean;
  onAgentSectionOpenChange: (open: boolean) => void;
  agentOps: ReturnType<typeof useAgentOperations>;
  chatModelDisplay: string;
  conversationSettings: ConversationSettings | null;
  defaultProvider: string;
  linkedAgent: {
    llm_provider?: string;
    llm_model?: string;
    exec_mode?: string;
  } | null;
  providerAvailability: Map<string, boolean>;
  selectedProvider: string;
  setSelectedProvider: React.Dispatch<React.SetStateAction<string>>;
  maxHistoryTurns: number;
  setMaxHistoryTurns: React.Dispatch<React.SetStateAction<number>>;
  selectedModel: string;
  setSelectedModel: React.Dispatch<React.SetStateAction<string>>;
  customModelEnabled: boolean;
  setCustomModelEnabled: React.Dispatch<React.SetStateAction<boolean>>;
  customModel: string;
  setCustomModel: React.Dispatch<React.SetStateAction<string>>;
  customModelValue: string;
  isCustomSaved: boolean;
  isCustomDirty: boolean;
  saveCustomModel: () => void;
  compressionProvider: string;
  setCompressionProvider: React.Dispatch<React.SetStateAction<string>>;
  compressionModel: string;
  setCompressionModel: React.Dispatch<React.SetStateAction<string>>;
  isCompressing: boolean;
  onCompressMemory: () => void;
  lastCompression: CompanionCompressionResult | null;
  effectiveExecMode: string;
  execModeOverride: ExecMode;
  setExecModeOverride: React.Dispatch<React.SetStateAction<ExecMode>>;
  toolModel: string;
  setToolModel: React.Dispatch<React.SetStateAction<string>>;
  responseModel: string;
  setResponseModel: React.Dispatch<React.SetStateAction<string>>;
  patchConversationSettings: (
    patch: ConversationSettingsPatch,
  ) => Promise<void>;
  toolsAllowDraft: string[];
  setToolsAllowDraft: React.Dispatch<React.SetStateAction<string[]>>;
  settingsError: string | null;
  resetConversationSettings: () => Promise<void>;
  memoryStats: CompanionMemoryStats | null;
  showMemoryContext: boolean;
  memoryContext: string | null;
  onToggleMemoryContext: () => void;
  contextInfo: ContextInfo;
  messages: ConsoleMessage[];
  editingSystemPrompt: boolean;
  setEditingSystemPrompt: React.Dispatch<React.SetStateAction<boolean>>;
  systemPromptDraft: string;
  setSystemPromptDraft: React.Dispatch<React.SetStateAction<string>>;
  setContextInfo: React.Dispatch<React.SetStateAction<ContextInfo>>;
  personalityInfo: PersonalityInfo | null;
  onUpdatePersonalityDimension: (name: string, value: number) => void;
  selectedMessage: ConsoleMessage | null;
  setSelectedMessage: React.Dispatch<React.SetStateAction<ConsoleMessage | null>>;
}

export function ConversationInspector({
  selectedConversation,
  onClose,
  agentSectionOpen,
  onAgentSectionOpenChange,
  agentOps,
  chatModelDisplay,
  conversationSettings,
  defaultProvider,
  linkedAgent,
  providerAvailability,
  selectedProvider,
  setSelectedProvider,
  maxHistoryTurns,
  setMaxHistoryTurns,
  selectedModel,
  setSelectedModel,
  customModelEnabled,
  setCustomModelEnabled,
  customModel,
  setCustomModel,
  customModelValue,
  isCustomSaved,
  isCustomDirty,
  saveCustomModel,
  compressionProvider,
  setCompressionProvider,
  compressionModel,
  setCompressionModel,
  isCompressing,
  onCompressMemory,
  lastCompression,
  effectiveExecMode,
  execModeOverride,
  setExecModeOverride,
  toolModel,
  setToolModel,
  responseModel,
  setResponseModel,
  patchConversationSettings,
  toolsAllowDraft,
  setToolsAllowDraft,
  settingsError,
  resetConversationSettings,
  memoryStats,
  showMemoryContext,
  memoryContext,
  onToggleMemoryContext,
  contextInfo,
  messages,
  editingSystemPrompt,
  setEditingSystemPrompt,
  systemPromptDraft,
  setSystemPromptDraft,
  setContextInfo,
  personalityInfo,
  onUpdatePersonalityDimension,
  selectedMessage,
  setSelectedMessage,
}: ConversationInspectorProps) {
  const inspectorRoleIcon = getRoleIcon(agentOps.targetAgent?.role);

  return (
    <div className="w-80 border-l border-border flex flex-col bg-muted/20">
      <div className="h-12 border-b border-border flex items-center justify-between px-4">
        <div className="flex items-center gap-2">
          <Settings2 className="h-4 w-4" />
          <span className="text-sm font-medium">Inspector</span>
          <HelpTooltip
            side="bottom"
            content="Inspector explains the current conversation, linked agent, model settings, memory controls, and debug state."
          />
        </div>
        <Tooltip content="Close the inspector and return to the main conversation view.">
          <Button
            variant="ghost"
            size="icon"
            className="h-7 w-7"
            onClick={onClose}
          >
            <X className="h-4 w-4" />
          </Button>
        </Tooltip>
      </div>

      <ScrollArea className="flex-1">
        <CollapsibleSection
          title="Agent"
          icon={React.createElement(inspectorRoleIcon, {
            className: "h-3.5 w-3.5 text-green-500",
          })}
          open={agentSectionOpen}
          onToggle={onAgentSectionOpenChange}
          badge={agentOps.targetAgent ? agentOps.targetAgent.state : undefined}
        >
          {agentOps.targetAgent ? (
            <div className="space-y-3">
              <div className="flex items-center gap-3">
                <div
                  className={cn(
                    "h-10 w-10 rounded-lg flex items-center justify-center",
                    agentOps.targetAgent.state === "running"
                      ? "bg-green-500/10"
                      : "bg-muted",
                  )}
                >
                  {React.createElement(inspectorRoleIcon, {
                    className: cn(
                      "h-5 w-5",
                      agentOps.targetAgent.state === "running"
                        ? "text-green-500"
                        : "text-muted-foreground",
                    ),
                  })}
                </div>
                <div className="flex-1 min-w-0">
                  <div className="flex items-center gap-2">
                    <span className="text-sm font-semibold truncate">
                      {getAgentDisplayName(agentOps.targetAgent)}
                    </span>
                    <Badge
                      variant={
                        agentOps.targetAgent.state === "running"
                          ? "default"
                          : "outline"
                      }
                      className="text-xs"
                    >
                      {agentOps.targetAgent.state}
                    </Badge>
                  </div>
                  <p className="text-xs text-muted-foreground font-mono truncate">
                    {agentOps.targetAgent.id.slice(0, 16)}...
                  </p>
                </div>
              </div>

              <div className="flex gap-2">
                {agentOps.targetAgent.state === "running" ? (
                  <Tooltip content="Stop the linked agent. Use this when you want to halt active work without deleting the agent.">
                    <Button
                      variant="destructive"
                      size="sm"
                      className="flex-1 gap-1"
                      onClick={() => {
                        if (
                          window.confirm(
                            `Stop "${getAgentDisplayName(agentOps.targetAgent!)}"?`,
                          )
                        ) {
                          agentOps.killAgent.mutate(agentOps.targetAgent!.id);
                        }
                      }}
                      disabled={agentOps.killAgent.isPending}
                    >
                      <Square className="h-3 w-3" />
                      {agentOps.killAgent.isPending ? "Stopping..." : "Stop"}
                    </Button>
                  </Tooltip>
                ) : (
                  <Tooltip content="Start the linked agent and return it to active work.">
                    <Button
                      variant="default"
                      size="sm"
                      className="flex-1 gap-1"
                      onClick={() =>
                        agentOps.startAgent.mutate(agentOps.targetAgent!.id)
                      }
                      disabled={agentOps.startAgent.isPending}
                    >
                      <Play className="h-3 w-3" />
                      {agentOps.startAgent.isPending ? "Starting..." : "Start"}
                    </Button>
                  </Tooltip>
                )}
                <Tooltip
                  content={
                    agentOps.targetAgent.state === "running"
                      ? "Stop the agent before removing it from the runtime list."
                      : "Remove this agent from the runtime list."
                  }
                >
                  <Button
                    variant="outline"
                    size="sm"
                    className="gap-1"
                    onClick={() => {
                      if (
                        window.confirm(
                          `Remove "${getAgentDisplayName(agentOps.targetAgent!)}"? This cannot be undone.`,
                        )
                      ) {
                        agentOps.trashAgent.mutate(agentOps.targetAgent!.id);
                      }
                    }}
                    disabled={
                      agentOps.trashAgent.isPending ||
                      agentOps.targetAgent.state === "running"
                    }
                  >
                    <Trash2 className="h-3 w-3" />
                  </Button>
                </Tooltip>
              </div>

              {agentOps.targetAgent.ns && (
                <div className="flex items-center gap-2 p-2 rounded-md bg-muted/30">
                  <Folder className="h-3.5 w-3.5 text-muted-foreground flex-shrink-0" />
                  <div className="min-w-0">
                    <span className="inline-flex items-center gap-1 text-[10px] text-muted-foreground">
                      <span>Workspace</span>
                      <HelpTooltip
                        content="The workspace path linked to this conversation or agent."
                        side="top"
                      />
                    </span>
                    <p
                      className="text-xs font-mono truncate"
                      title={agentOps.targetAgent.ns}
                    >
                      {agentOps.targetAgent.ns}
                    </p>
                  </div>
                </div>
              )}

              <div className="grid grid-cols-2 gap-2">
                <div className="bg-muted/30 rounded-md p-2">
                  <div className="flex items-center gap-1.5 text-muted-foreground mb-0.5">
                    <Cpu className="h-3 w-3" />
                    <span className="text-[10px]">Chat Model</span>
                  </div>
                  <p
                    className="text-xs font-medium truncate"
                    title={`Agent model: ${agentOps.targetAgent.llm_model || "default"}`}
                  >
                    {chatModelDisplay}
                  </p>
                </div>
                <div className="bg-muted/30 rounded-md p-2">
                  <div className="flex items-center gap-1.5 text-muted-foreground mb-0.5">
                    <Zap className="h-3 w-3" />
                    <span className="text-[10px]">Role</span>
                  </div>
                  <p className="text-xs font-medium truncate">
                    {agentOps.targetAgent.role || "agent"}
                  </p>
                </div>
                <div className="bg-muted/30 rounded-md p-2">
                  <div className="flex items-center gap-1.5 text-muted-foreground mb-0.5">
                    <Activity className="h-3 w-3" />
                    <span className="text-[10px]">Sessions</span>
                  </div>
                  <p className="text-xs font-medium">{agentOps.sessions.length}</p>
                </div>
                <div className="bg-muted/30 rounded-md p-2">
                  <div className="flex items-center gap-1.5 text-muted-foreground mb-0.5">
                    <Clock className="h-3 w-3" />
                    <span className="text-[10px]">Created</span>
                  </div>
                  <p className="text-xs font-medium truncate">
                    {agentOps.targetAgent.created_at
                      ? formatRelativeTime(agentOps.targetAgent.created_at)
                      : "-"}
                  </p>
                </div>
              </div>

              {agentOps.sessions.length > 0 && (
                <div className="space-y-1.5">
                  <span className="text-[10px] font-medium text-muted-foreground uppercase tracking-wider">
                    Active Sessions
                  </span>
                  {agentOps.sessions.slice(0, 5).map((session: AgentSession) => (
                    <Card key={session.session_id} className="p-2 bg-muted/30">
                      <div className="flex items-center justify-between mb-0.5">
                        <span className="text-[10px] font-mono text-muted-foreground">
                          {session.session_id.slice(0, 12)}...
                        </span>
                        <Badge
                          variant={
                            session.status === "running" ? "default" : "outline"
                          }
                          className="text-[9px]"
                        >
                          {session.status}
                        </Badge>
                      </div>
                      <div className="flex items-center gap-3 text-[10px] text-muted-foreground">
                        <span>Iters: {session.iterations || 0}</span>
                        <span>{session.role}</span>
                      </div>
                    </Card>
                  ))}
                </div>
              )}
            </div>
          ) : (
            <div className="text-xs text-muted-foreground italic text-center py-2">
              No agent linked to this conversation
            </div>
          )}
        </CollapsibleSection>

        <CollapsibleSection
          title="Models"
          icon={<Cpu className="h-3.5 w-3.5 text-blue-500" />}
          defaultOpen
        >
          {conversationSettings?.updated_at && (
            <div className="text-[10px] text-muted-foreground mb-2">
              Settings saved {formatRelativeTime(conversationSettings.updated_at)}
            </div>
          )}
          <div className="text-[10px] font-medium text-muted-foreground uppercase tracking-wider mb-1">
            Chat
          </div>
          <div className="flex items-center justify-between">
            <span className="inline-flex items-center gap-1 text-xs font-medium">
              <span>Provider</span>
              <HelpTooltip
                side="top"
                content="The model provider used for this conversation's main responses."
              />
            </span>
            <select
              value={selectedProvider}
              onChange={(e) => {
                const nextProvider = e.target.value;
                setSelectedProvider(nextProvider);
                setCustomModelEnabled(false);
                setCustomModel("");

                const nextModel =
                  nextProvider === "openrouter"
                    ? "google/gemini-3.1-flash-lite-preview"
                    : "";
                setSelectedModel(nextModel);
                void patchConversationSettings({
                  llm_provider: nextProvider,
                  llm_model: nextModel,
                });
              }}
              className="text-xs bg-muted border border-border rounded-md px-2 py-1 font-mono focus:outline-none focus:ring-1 focus:ring-ring max-w-[160px]"
            >
              {PROVIDERS.map((provider) => (
                <option key={provider.id} value={provider.id}>
                  {provider.id === ""
                    ? `Default (${defaultProvider || linkedAgent?.llm_provider || "openai"})`
                    : `${provider.name}${providerAvailability.has(provider.id) && !providerAvailability.get(provider.id) ? " \u26A0" : ""}`}
                </option>
              ))}
            </select>
          </div>
          {selectedProvider !== "" &&
            providerAvailability.has(selectedProvider) &&
            !providerAvailability.get(selectedProvider) && (
              <div className="text-[10px] text-destructive">
                No API key configured for this provider
              </div>
            )}
          <div className="flex items-center justify-between">
            <span className="inline-flex items-center gap-1 text-xs font-medium">
              <span>Model</span>
              <HelpTooltip
                side="top"
                content="The specific model used for chat responses. Custom models let you enter an exact provider/model string."
              />
            </span>
            {PROVIDERS.find((provider) => provider.id === selectedProvider)
              ?.allowCustom ? (
              <div className="space-y-1">
                <select
                  value={customModelEnabled ? "__custom__" : selectedModel}
                  onChange={(e) => {
                    const nextModel = e.target.value;
                    if (nextModel === "__custom__") {
                      setCustomModelEnabled(true);
                      setSelectedModel("");
                      return;
                    }
                    setCustomModelEnabled(false);
                    setCustomModel("");
                    setSelectedModel(nextModel);
                    void patchConversationSettings({ llm_model: nextModel });
                  }}
                  className="text-xs bg-muted border border-border rounded-md px-2 py-1 font-mono focus:outline-none focus:ring-1 focus:ring-ring max-w-[160px]"
                >
                  <option value="">Default</option>
                  {getModelsForProvider(selectedProvider).map((model) => (
                    <option key={model.id} value={model.id}>
                      {model.name}
                    </option>
                  ))}
                  <option value="__custom__">
                    {customModelValue ? `Custom (${customModelValue})` : "Custom..."}
                  </option>
                </select>
                {customModelEnabled && (
                  <div className="flex items-center gap-2">
                    <Input
                      value={customModel}
                      onChange={(e) => setCustomModel(e.target.value)}
                      onKeyDown={(e) => {
                        if (e.key === "Enter") {
                          e.preventDefault();
                          saveCustomModel();
                        }
                      }}
                      placeholder="e.g., openrouter/deepseek-r1"
                      className="h-7 text-xs font-mono flex-1"
                    />
                    <Tooltip content={isCustomSaved ? "This custom model is already saved." : "Save this custom model value for the current conversation."}>
                      <Button
                        type="button"
                        variant="outline"
                        size="sm"
                        className="h-7 px-2 text-[10px]"
                        disabled={!customModelValue || isCustomSaved}
                        onClick={saveCustomModel}
                      >
                        Save
                      </Button>
                    </Tooltip>
                    {customModelEnabled &&
                      (customModelValue ? (
                        <Badge
                          variant={
                            isCustomSaved
                              ? "success"
                              : isCustomDirty
                                ? "warning"
                                : "secondary"
                          }
                          className="text-[10px]"
                        >
                          {isCustomSaved ? "Saved" : "Unsaved"}
                        </Badge>
                      ) : (
                        <Badge variant="secondary" className="text-[10px]">
                          Enter model
                        </Badge>
                      ))}
                  </div>
                )}
              </div>
            ) : (
              <select
                value={selectedModel}
                onChange={(e) => {
                  const nextModel = e.target.value;
                  setSelectedModel(nextModel);
                  void patchConversationSettings({ llm_model: nextModel });
                }}
                className="text-xs bg-muted border border-border rounded-md px-2 py-1 font-mono focus:outline-none focus:ring-1 focus:ring-ring max-w-[160px]"
              >
                {getModelsForProvider(selectedProvider).map((model) => (
                  <option key={model.id} value={model.id}>
                    {model.id === ""
                      ? `Default (${linkedAgent?.llm_model || "gpt-4o-mini"})`
                      : model.name}
                  </option>
                ))}
              </select>
            )}
          </div>
          <div className="flex items-center justify-between">
            <span className="inline-flex items-center gap-1 text-xs font-medium">
              <span>History Turns</span>
              <HelpTooltip
                side="top"
                content="How many prior turns should be kept in the active chat context. Higher values preserve more history but use more context budget."
              />
            </span>
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

          <div className="pt-2 border-t border-border space-y-2">
            <div className="text-[10px] font-medium text-muted-foreground uppercase tracking-wider">
              Compression
            </div>
            <div className="flex items-center justify-between">
              <span className="inline-flex items-center gap-1 text-xs font-medium">
                <span>Provider</span>
                <HelpTooltip
                  side="top"
                  content="Optional provider override for memory compression runs."
                />
              </span>
              <select
                value={compressionProvider}
                onChange={(e) => {
                  setCompressionProvider(e.target.value);
                  setCompressionModel("");
                }}
                className="text-xs bg-muted border border-border rounded-md px-2 py-1 font-mono focus:outline-none focus:ring-1 focus:ring-ring max-w-[160px]"
              >
                <option value="">Same as Chat</option>
                {PROVIDERS.filter((provider) => provider.id !== "").map(
                  (provider) => (
                    <option key={provider.id} value={provider.id}>
                      {`${provider.name}${providerAvailability.has(provider.id) && !providerAvailability.get(provider.id) ? " \u26A0" : ""}`}
                    </option>
                  ),
                )}
              </select>
            </div>
            {compressionProvider !== "" && (
              <div className="flex items-center justify-between">
                <span className="inline-flex items-center gap-1 text-xs font-medium">
                  <span>Model</span>
                  <HelpTooltip
                    side="top"
                    content="Optional model override for memory compression."
                  />
                </span>
                <select
                  value={compressionModel}
                  onChange={(e) => setCompressionModel(e.target.value)}
                  className="text-xs bg-muted border border-border rounded-md px-2 py-1 font-mono focus:outline-none focus:ring-1 focus:ring-ring max-w-[160px]"
                >
                  <option value="">Default</option>
                  {getModelsForProvider(compressionProvider).map((model) => (
                    <option key={model.id} value={model.id}>
                      {model.name}
                    </option>
                  ))}
                </select>
              </div>
            )}
            <div className="flex items-center justify-between">
              <span className="inline-flex items-center gap-1 text-xs font-medium">
                <span>Compress</span>
                <HelpTooltip
                  side="top"
                  content="Run memory compression to summarize older material and reduce context load."
                />
              </span>
              <Tooltip content="Start a compression pass over this conversation's memory.">
                <Button
                  type="button"
                  variant="outline"
                  size="sm"
                  className="h-7 text-xs"
                  disabled={isCompressing}
                  onClick={onCompressMemory}
                >
                  {isCompressing ? "Running..." : "Run"}
                </Button>
              </Tooltip>
            </div>
            {lastCompression && (
              <div className="text-[10px] text-muted-foreground">
                {lastCompression.summarized} summarized, {lastCompression.skipped} skipped
                {lastCompression.distilled ? ", distilled" : ""}
              </div>
            )}
          </div>

          {effectiveExecMode === "story" && (
            <div className="pt-2 border-t border-border space-y-2">
              <div className="text-[10px] font-medium text-muted-foreground uppercase tracking-wider">
                Story Pipeline
              </div>
              {providerAvailability.size > 0 &&
                !providerAvailability.get("openrouter") && (
                  <div className="text-[10px] text-destructive">
                    OpenRouter key not configured
                  </div>
                )}
              <div className="flex items-center justify-between">
                <div className="flex items-center gap-2">
                  <Search className="h-4 w-4 text-orange-500" />
                  <span className="inline-flex items-center gap-1 text-xs font-medium">
                    <span>Gather Model</span>
                    <HelpTooltip
                      side="top"
                      content="Model used for the structured gathering stage in story mode."
                    />
                  </span>
                </div>
                <select
                  value={toolModel}
                  onChange={(e) => {
                    const nextModel = e.target.value;
                    setToolModel(nextModel);
                    void patchConversationSettings({
                      story_gather_model: nextModel,
                    });
                  }}
                  className="text-xs bg-muted border border-border rounded-md px-2 py-1 font-mono focus:outline-none focus:ring-1 focus:ring-ring max-w-[140px]"
                >
                  <option value="">Default</option>
                  {toolModel &&
                    !COMPANION_TOOL_MODELS.some((model) => model.id === toolModel) && (
                      <option value={toolModel}>{toolModel}</option>
                    )}
                  {COMPANION_TOOL_MODELS.map((model) => (
                    <option key={model.id} value={model.id}>
                      {model.name}
                    </option>
                  ))}
                </select>
              </div>
              <div className="flex items-center justify-between">
                <div className="flex items-center gap-2">
                  <MessageCircle className="h-4 w-4 text-purple-500" />
                  <span className="inline-flex items-center gap-1 text-xs font-medium">
                    <span>Dialogue Model</span>
                    <HelpTooltip
                      side="top"
                      content="Model used for the narrative or dialogue stage in story mode."
                    />
                  </span>
                </div>
                <select
                  value={responseModel}
                  onChange={(e) => {
                    const nextModel = e.target.value;
                    setResponseModel(nextModel);
                    void patchConversationSettings({
                      story_dialogue_model: nextModel,
                    });
                  }}
                  className="text-xs bg-muted border border-border rounded-md px-2 py-1 font-mono focus:outline-none focus:ring-1 focus:ring-ring max-w-[140px]"
                >
                  <option value="">Default</option>
                  {responseModel &&
                    !COMPANION_RESPONSE_MODELS.some(
                      (model) => model.id === responseModel,
                    ) && <option value={responseModel}>{responseModel}</option>}
                  {COMPANION_RESPONSE_MODELS.map((model) => (
                    <option key={model.id} value={model.id}>
                      {model.name}
                    </option>
                  ))}
                </select>
              </div>
            </div>
          )}

          <div className="pt-2 border-t border-border space-y-1.5">
            <div className="flex items-center justify-between">
              <span className="inline-flex items-center gap-1 text-xs font-medium">
                <span>Exec Mode</span>
                <HelpTooltip
                  side="top"
                  content="How the linked agent behaves between messages: single-turn, self-directed, scheduled, or story-oriented."
                />
              </span>
              <select
                value={execModeOverride}
                onChange={(e) => {
                  const nextMode = e.target.value as ExecMode;
                  setExecModeOverride(nextMode);
                  void patchConversationSettings({ exec_mode: nextMode });
                }}
                className="text-xs bg-muted border border-border rounded-md px-2 py-1 font-mono focus:outline-none focus:ring-1 focus:ring-ring max-w-[160px]"
              >
                <option value="">
                  Default ({linkedAgent?.exec_mode || "reactive"})
                </option>
                <option value="reactive">Reactive</option>
                <option value="autonomous">Autonomous</option>
                <option value="proactive">Proactive</option>
                <option value="tick">Tick</option>
                <option value="story">Story</option>
              </select>
            </div>
            <p className="text-[10px] text-muted-foreground">
              {effectiveExecMode === "reactive" &&
                "Single-turn: responds to each message independently"}
              {effectiveExecMode === "autonomous" &&
                "Multi-turn: continues working until task complete"}
              {effectiveExecMode === "proactive" &&
                "Self-directed: initiates work via think cycles"}
              {effectiveExecMode === "story" &&
                "Two-stage: gather + dialogue with structured outputs"}
            </p>
          </div>

          <div className="pt-2 border-t border-border">
            <ToolAllowlistEditor
              value={toolsAllowDraft}
              onChange={setToolsAllowDraft}
              onSave={() =>
                void patchConversationSettings({ tools_allow: toolsAllowDraft })
              }
              onClear={() => void patchConversationSettings({ tools_allow: [] })}
              error={settingsError}
            />
          </div>

          <div className="pt-2 border-t border-border">
            <Button
              type="button"
              variant="outline"
              size="sm"
              className="h-7 text-xs w-full"
              onClick={() => {
                if (
                  window.confirm("Reset all conversation settings to defaults?")
                ) {
                  void resetConversationSettings();
                }
              }}
            >
              Reset Conversation Settings
            </Button>
          </div>
        </CollapsibleSection>

        <CollapsibleSection
          title="Memory"
          icon={<Brain className="h-3.5 w-3.5 text-purple-500" />}
          badge={memoryStats ? `${memoryStats.day_summaries} days` : undefined}
        >
          {memoryStats ? (
            <div className="space-y-2">
              <div className="text-[10px] text-muted-foreground space-y-0.5">
                <div>
                  {memoryStats.total_turns} turns, {memoryStats.day_summaries} day summaries
                  {memoryStats.has_distilled_history ? ", distilled" : ""}
                </div>
                {memoryStats.last_summarized_date && (
                  <div>Last summarized: {memoryStats.last_summarized_date}</div>
                )}
              </div>
              {memoryStats.day_summaries > 0 && (
                <Button
                  variant="ghost"
                  size="sm"
                  className="h-6 text-[10px] px-2"
                  onClick={onToggleMemoryContext}
                >
                  {showMemoryContext ? "Hide memory context" : "Show memory context"}
                </Button>
              )}
              {showMemoryContext && memoryContext && (
                <Card className="p-2 max-h-[120px] overflow-y-auto">
                  <pre className="text-[10px] text-muted-foreground whitespace-pre-wrap">
                    {memoryContext}
                  </pre>
                </Card>
              )}
            </div>
          ) : (
            <div className="text-xs text-muted-foreground italic">
              No memory data available
            </div>
          )}
        </CollapsibleSection>

        <CollapsibleSection
          title="Conversation"
          icon={<MessageCircle className="h-3.5 w-3.5 text-yellow-500" />}
          badge={`${messages.length} msgs`}
        >
          <div className="space-y-2">
            <div className="flex items-center gap-2">
              <Coins className="h-3.5 w-3.5 text-yellow-500" />
              <span className="text-xs font-medium">Token Usage</span>
            </div>
            <div className="grid grid-cols-3 gap-2 text-center">
              <div className="bg-muted/50 rounded-md p-2">
                <div className="text-[10px] text-muted-foreground">Input</div>
                <div className="text-xs font-mono font-medium">
                  {Math.ceil(
                    messages
                      .filter((message) => message.role === "user")
                      .reduce((sum, message) => sum + message.content.length, 0) /
                      4,
                  ).toLocaleString()}
                </div>
              </div>
              <div className="bg-muted/50 rounded-md p-2">
                <div className="text-[10px] text-muted-foreground">Output</div>
                <div className="text-xs font-mono font-medium">
                  {Math.ceil(
                    messages
                      .filter((message) => message.role !== "user")
                      .reduce((sum, message) => sum + message.content.length, 0) /
                      4,
                  ).toLocaleString()}
                </div>
              </div>
              <div className="bg-primary/10 rounded-md p-2">
                <div className="text-[10px] text-muted-foreground">Total</div>
                <div className="text-xs font-mono font-medium text-primary">
                  {Math.ceil(
                    messages.reduce(
                      (sum, message) => sum + message.content.length,
                      0,
                    ) / 4,
                  ).toLocaleString()}
                </div>
              </div>
            </div>
          </div>

          <Card className="p-3 space-y-2 text-xs">
            <div className="flex items-center justify-between">
              <span className="text-muted-foreground">Profile</span>
              <Badge variant="secondary" className="text-[10px]">
                {contextInfo.profile || "companion"}
              </Badge>
            </div>
            <div className="flex items-center justify-between">
              <span className="text-muted-foreground">Workspace</span>
              <span
                className="text-[10px] font-mono truncate max-w-[140px]"
                title={contextInfo.workspace}
              >
                {contextInfo.workspace || "/"}
              </span>
            </div>
            {contextInfo.createdAt && (
              <div className="flex items-center justify-between">
                <span className="text-muted-foreground">Created</span>
                <span className="text-[10px] flex items-center gap-1">
                  <Clock className="h-3 w-3" />
                  {formatRelativeTime(contextInfo.createdAt)}
                </span>
              </div>
            )}
          </Card>

          <Card className="p-3 space-y-2 text-xs">
            <div className="flex justify-between">
              <span className="text-muted-foreground">ID</span>
              <span
                className="font-mono truncate max-w-[140px]"
                title={selectedConversation.id}
              >
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
        </CollapsibleSection>

        <CollapsibleSection
          title="Personality"
          icon={<Sparkles className="h-3.5 w-3.5 text-purple-500" />}
        >
          {personalityInfo?.profile?.dimensions &&
          personalityInfo.profile.dimensions.length > 0 ? (
            <div className="space-y-3">
              {personalityInfo.profile.dimensions.map((dimension) => (
                <div key={dimension.name} className="space-y-1.5">
                  <div className="flex justify-between text-xs">
                    <span className="capitalize text-muted-foreground">
                      {dimension.name}
                    </span>
                    <span className="font-mono text-primary">
                      {(dimension.value * 100).toFixed(0)}%
                    </span>
                  </div>
                  <Slider
                    value={dimension.value}
                    min={0}
                    max={1}
                    step={0.05}
                    onChange={(value) =>
                      onUpdatePersonalityDimension(dimension.name, value)
                    }
                  />
                  <div className="flex justify-between text-[10px] text-muted-foreground">
                    <span>{dimension.min_label}</span>
                    <span>{dimension.max_label}</span>
                  </div>
                </div>
              ))}
            </div>
          ) : (
            <div className="text-xs text-muted-foreground italic">
              No personality data
            </div>
          )}
        </CollapsibleSection>

        <CollapsibleSection
          title="Prompt"
          icon={<Sliders className="h-3.5 w-3.5 text-blue-500" />}
        >
          <div className="space-y-2">
            <div className="flex items-center justify-between">
              <span className="inline-flex items-center gap-1 text-xs font-medium">
                <span>System Prompt</span>
                <HelpTooltip
                  content="The system prompt sets the assistant's instructions, boundaries, and behavior for this conversation."
                  side="top"
                />
              </span>
              {!editingSystemPrompt ? (
                <Button
                  variant="ghost"
                  size="sm"
                  className="h-6 text-xs"
                  onClick={() => {
                    setEditingSystemPrompt(true);
                    setSystemPromptDraft(contextInfo.systemPrompt || "");
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
                      setEditingSystemPrompt(false);
                      setContextInfo((prev) => ({
                        ...prev,
                        systemPrompt: systemPromptDraft,
                      }));
                    }}
                  >
                    <Save className="h-3 w-3 text-green-500" />
                  </Button>
                  <Button
                    variant="ghost"
                    size="sm"
                    className="h-6 w-6 p-0"
                    onClick={() => {
                      setEditingSystemPrompt(false);
                      setSystemPromptDraft("");
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
                onChange={(e: React.ChangeEvent<HTMLTextAreaElement>) =>
                  setSystemPromptDraft(e.target.value)
                }
                className="text-xs min-h-[100px] max-h-[150px] font-mono"
                placeholder="Enter system prompt..."
              />
            ) : contextInfo.systemPrompt ? (
              <Card className="p-2 max-h-[100px] overflow-y-auto">
                <pre className="text-[11px] text-muted-foreground whitespace-pre-wrap">
                  {contextInfo.systemPrompt.slice(0, 300)}
                  {contextInfo.systemPrompt.length > 300 && "..."}
                </pre>
              </Card>
            ) : (
              <div className="text-xs text-muted-foreground italic">
                No system prompt configured
              </div>
            )}
          </div>
        </CollapsibleSection>

        <CollapsibleSection
          title="Debug"
          icon={<Bug className="h-3.5 w-3.5 text-red-500" />}
          badge={selectedMessage ? "1 selected" : undefined}
        >
          {selectedMessage ? (
            <div className="space-y-2">
              <div className="flex items-center justify-between">
                <span className="text-xs font-medium">Selected Message</span>
                <Tooltip content="Clear the currently selected debug message.">
                  <Button
                    variant="ghost"
                    size="sm"
                    className="h-6 text-xs"
                    onClick={() => setSelectedMessage(null)}
                  >
                    Clear
                  </Button>
                </Tooltip>
              </div>
              <Card className="p-3 space-y-3">
                <div className="flex items-center justify-between text-xs">
                  <span className="text-muted-foreground">Role</span>
                  <Badge
                    variant={
                      selectedMessage.role === "assistant" ? "default" : "secondary"
                    }
                  >
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
                    <span className="text-xs font-medium text-muted-foreground">
                      Content Preview
                    </span>
                    <p className="text-xs text-foreground mt-1 line-clamp-3">
                      {selectedMessage.content.slice(0, 200)}
                      {selectedMessage.content.length > 200 && "..."}
                    </p>
                  </div>
                )}
              </Card>
            </div>
          ) : (
            <div className="text-xs text-muted-foreground italic">
              Click a message to inspect it
            </div>
          )}
        </CollapsibleSection>
      </ScrollArea>
    </div>
  );
}
