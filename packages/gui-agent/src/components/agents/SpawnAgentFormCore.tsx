import { useState, useMemo, useEffect } from "react";
import { useQuery } from "@tanstack/react-query";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { HelpTooltip, Tooltip } from "@/components/ui/tooltip";
import {
  companionChat,
  listSkills,
  listWorkspaces,
  type SpawnAgentParams,
} from "@/api/client";
import { Folder } from "lucide-react";
import {
  Plus,
  RefreshCw,
  ChevronDown,
  ChevronRight,
  Sparkles,
  Globe,
} from "lucide-react";
import { RoleSelector } from "./RoleSelector";
import {
  EXEC_MODES,
  PROVIDERS,
  getRoleById,
  getProviderById,
} from "./spawnFormConstants";
import { humanReadableWorkspacePath } from "@/lib/room-utils";
import { useViewStore } from "@/stores/viewStore";

function recommendedMemoryScopeForRetention(
  retention: NonNullable<SpawnAgentParams["memory_retention"]>,
): NonNullable<SpawnAgentParams["memory_scope"]> {
  return retention === "task" || retention === "ephemeral"
    ? "session"
    : "agent";
}

export interface SpawnAgentFormCoreProps {
  onSubmit: (params: SpawnAgentParams) => void;
  onCancel: () => void;
  isPending: boolean;
  error?: Error | null;
}

export function SpawnAgentFormCore({
  onSubmit,
  onCancel,
  isPending,
  error,
}: SpawnAgentFormCoreProps) {
  const spawnRoomID = useViewStore((s) => s.spawnRoomID);
  const spawnRoomWorkspaceID = useViewStore((s) => s.spawnRoomWorkspaceID);
  const spawnRoomRole = useViewStore((s) => s.spawnRoomRole);
  const [showAdvanced, setShowAdvanced] = useState(false);
  const [showSkills, setShowSkills] = useState(false);
  const [isEnhancing, setIsEnhancing] = useState(false);
  const [customModel, setCustomModel] = useState("");
  const [formData, setFormData] = useState<SpawnAgentParams>({
    role: "coder",
    prompt: "",
    name: "",
    workspace_id: spawnRoomWorkspaceID || "",
    workspace_source: "local",
    sandbox_provider: "opensandbox",
    repo_url: "",
    repo_ref: "main",
    sandbox_timeout_s: 3600,
    allow_egress: [],
    memory_scope: "agent",
    memory_retention: "durable",
    room_id: spawnRoomID || undefined,
    room_role: spawnRoomRole || undefined,
    exec_mode: "reactive",
    llm_provider: "",
    llm_model: "",
    max_iterations: 10,
    max_auto_turns: 1,
    think_interval: 60,
    skills_allow: [],
  });

  // Fetch available workspaces
  const { data: workspacesData } = useQuery({
    queryKey: ["workspaces"],
    queryFn: listWorkspaces,
  });

  const workspaces = useMemo(
    () => workspacesData?.workspaces ?? [],
    [workspacesData?.workspaces],
  );
  const currentWorkspace = workspacesData?.current ?? "";

  // Auto-select current workspace on first load
  useEffect(() => {
    if (currentWorkspace && !formData.workspace_id) {
      setFormData((prev) => ({ ...prev, workspace_id: currentWorkspace }));
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [currentWorkspace]);

  useEffect(() => {
    if (spawnRoomWorkspaceID && !formData.workspace_id) {
      setFormData((prev) => ({ ...prev, workspace_id: spawnRoomWorkspaceID }));
    }
    if (spawnRoomID) {
      setFormData((prev) => ({
        ...prev,
        room_id: prev.room_id || spawnRoomID,
        room_role: prev.room_role || spawnRoomRole || prev.role,
      }));
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [spawnRoomID, spawnRoomRole, spawnRoomWorkspaceID]);

  // Fetch available skills
  const { data: skillsData } = useQuery({
    queryKey: ["skills"],
    queryFn: listSkills,
  });

  const skills = useMemo(() => skillsData?.skills ?? [], [skillsData?.skills]);
  // Group skills by toolkit (first tag) for easier selection
  const skillsByToolkit = useMemo(() => {
    const toolkits: Record<string, (typeof skills)[number][]> = {};
    const seen = new Set<string>();
    for (const skill of skills) {
      // Deduplicate by skill name
      if (seen.has(skill.name)) continue;
      seen.add(skill.name);
      // Use first tag as toolkit, fall back to name prefix
      const toolkit = skill.tags?.[0] || skill.name.split("/")[0] || "other";
      if (!toolkits[toolkit]) toolkits[toolkit] = [];
      toolkits[toolkit].push(skill);
    }
    // Sort toolkits alphabetically
    return Object.fromEntries(
      Object.entries(toolkits).sort(([a], [b]) => a.localeCompare(b)),
    );
  }, [skills]);

  const handleSubmit = (e: React.FormEvent) => {
    e.preventDefault();
    if (!formData.prompt.trim()) return;
    const params: SpawnAgentParams = {
      role: formData.role,
      prompt: formData.prompt,
    };
    if (formData.workspace_id?.trim())
      params.workspace_id = formData.workspace_id.trim();
    if (formData.workspace_root?.trim())
      params.workspace_root = formData.workspace_root.trim();
    if (formData.workspace_source) params.workspace_source = formData.workspace_source;
    if (formData.workspace_source === "sandbox") {
      if (formData.sandbox_provider) params.sandbox_provider = formData.sandbox_provider;
      if (formData.repo_url?.trim()) params.repo_url = formData.repo_url.trim();
      if (formData.repo_ref?.trim()) params.repo_ref = formData.repo_ref.trim();
      if (formData.sandbox_image?.trim()) params.sandbox_image = formData.sandbox_image.trim();
      if (formData.sandbox_timeout_s && formData.sandbox_timeout_s > 0) {
        params.sandbox_timeout_s = formData.sandbox_timeout_s;
      }
      if (formData.allow_egress && formData.allow_egress.length > 0) {
        params.allow_egress = formData.allow_egress;
      }
    }
    if (formData.name?.trim()) params.name = formData.name.trim();
    if (formData.memory_scope) params.memory_scope = formData.memory_scope;
    if (formData.memory_retention)
      params.memory_retention = formData.memory_retention;
    if (formData.room_id?.trim()) params.room_id = formData.room_id.trim();
    if (formData.room_role?.trim())
      params.room_role = formData.room_role.trim();
    if (formData.exec_mode && formData.exec_mode !== "reactive") {
      params.exec_mode = formData.exec_mode;
    }
    if (formData.llm_provider) params.llm_provider = formData.llm_provider;
    if (formData.llm_model) params.llm_model = formData.llm_model;
    if (formData.max_iterations && formData.max_iterations !== 10) {
      params.max_iterations = formData.max_iterations;
    }
    if (formData.max_auto_turns && formData.max_auto_turns !== 1) {
      params.max_auto_turns = formData.max_auto_turns;
    }
    if (formData.think_interval && formData.think_interval !== 60) {
      params.think_interval = formData.think_interval;
    }
    if (formData.skills_allow && formData.skills_allow.length > 0) {
      params.skills_allow = formData.skills_allow;
    }
    onSubmit(params);
  };

  // Handle role change - auto-fill default prompt if prompt is empty
  const handleRoleChange = (roleId: string) => {
    const role = getRoleById(roleId);
    const currentRole = getRoleById(formData.role);
    const nextRetention =
      roleId === "companion"
        ? "companion"
        : formData.memory_retention || "durable";

    // Auto-fill if prompt is empty or matches previous role's default
    const shouldAutoFill =
      !formData.prompt.trim() ||
      (currentRole && formData.prompt.trim() === currentRole.defaultPrompt);

    setFormData({
      ...formData,
      role: roleId,
      memory_retention: nextRetention,
      memory_scope: recommendedMemoryScopeForRetention(nextRetention),
      prompt: shouldAutoFill && role ? role.defaultPrompt : formData.prompt,
    });
  };

  // Use default prompt for current role
  const handleUseDefaultPrompt = () => {
    const role = getRoleById(formData.role);
    if (role) {
      setFormData({ ...formData, prompt: role.defaultPrompt });
    }
  };

  // Enhance prompt with AI
  const handleEnhancePrompt = async () => {
    if (!formData.prompt.trim()) return;

    setIsEnhancing(true);
    try {
      const result = await companionChat({
        conversation_id: `enhance-${Date.now()}`,
        message: `Improve and expand this agent system prompt for a ${formData.role} role. Make it more specific and actionable while keeping the same intent. Return ONLY the improved prompt text, no explanation:\n\n${formData.prompt}`,
      });
      if (result.response) {
        setFormData({ ...formData, prompt: result.response.trim() });
      }
    } catch (err) {
      console.error("Failed to enhance prompt:", err);
    } finally {
      setIsEnhancing(false);
    }
  };

  // Handle provider change - reset model
  const handleProviderChange = (providerId: string) => {
    const provider = getProviderById(providerId);
    const firstModel = provider?.models[0]?.id ?? "";
    setFormData({
      ...formData,
      llm_provider: providerId,
      llm_model: firstModel,
    });
    setCustomModel("");
  };

  // Handle skill toggle
  const handleSkillToggle = (skillName: string) => {
    const current = formData.skills_allow ?? [];
    const updated = current.includes(skillName)
      ? current.filter((s) => s !== skillName)
      : [...current, skillName];
    setFormData({ ...formData, skills_allow: updated });
  };

  const selectedProvider = getProviderById(formData.llm_provider || "");
  const availableModels = selectedProvider?.models ?? [];

  return (
    <form onSubmit={handleSubmit} className="space-y-4">
      {/* Name */}
      <div>
        <div className="text-sm font-medium text-foreground inline-flex items-center gap-1.5">
          <span>Name</span>
          <HelpTooltip
            content="Optional human-friendly name for this agent. Leave blank to auto-generate one."
            side="top"
          />
        </div>
        <Input
          value={formData.name}
          onChange={(e) => setFormData({ ...formData, name: e.target.value })}
          placeholder="Auto-generated if empty"
          className="h-9 text-sm mt-1"
        />
        <p className="text-xs text-muted-foreground mt-1">
          Optional - a memorable name for this agent
        </p>
      </div>

      {/* Workspace */}
      <div>
        <div className="text-sm font-medium text-foreground flex items-center gap-1.5">
          <Folder className="h-3.5 w-3.5" />
          <span>Workspace</span>
          <HelpTooltip
            content="The project directory this agent reads from, writes to, and uses as its default tool context."
            side="top"
          />
        </div>
        <select
          value={formData.workspace_id}
          onChange={(e) =>
            setFormData({ ...formData, workspace_id: e.target.value })
          }
          className="w-full mt-1 h-9 rounded-md border border-input bg-background px-3 text-sm font-mono"
        >
          <option value="">Default</option>
          {workspaces.map((ws) => (
            <option key={ws.path} value={ws.path}>
              {ws.name}
              {ws.is_active ? " (active)" : ""} — {humanReadableWorkspacePath(ws.path)}
            </option>
          ))}
        </select>
        <p className="text-xs text-muted-foreground mt-1">
          The repo/directory this agent operates in and calls tools from
        </p>
      </div>

      <div className="rounded-lg border border-border bg-card/60 p-3 space-y-3">
        <div className="flex items-center gap-2">
          <Globe className="h-4 w-4 text-muted-foreground" />
          <div>
            <div className="text-sm font-medium text-foreground inline-flex items-center gap-1.5">
              <span>Execution Workspace</span>
              <HelpTooltip
                content="Choose whether the agent should work in the live local workspace or in an isolated sandbox clone."
                side="top"
              />
            </div>
            <p className="text-xs text-muted-foreground">
              Run this agent against the local runtime workspace or provision an isolated sandbox clone.
            </p>
          </div>
        </div>
        <div className="grid grid-cols-2 gap-2">
          <Tooltip content="Use the live local project directory. Best when you want the agent working directly against your current checkout.">
            <button
              type="button"
              onClick={() =>
                setFormData((prev) => ({
                  ...prev,
                  workspace_source: "local",
                }))
              }
              className={`w-full rounded-md border px-3 py-2 text-sm text-left ${
                formData.workspace_source !== "sandbox"
                  ? "border-primary bg-primary/10 text-foreground"
                  : "border-border bg-background text-muted-foreground"
              }`}
            >
              Local Runtime
            </button>
          </Tooltip>
          <Tooltip content="Create an isolated clone for the agent. Best when you want safer parallel work without touching the local checkout directly.">
            <button
              type="button"
              onClick={() =>
                setFormData((prev) => ({
                  ...prev,
                  workspace_source: "sandbox",
                  sandbox_provider: prev.sandbox_provider || "opensandbox",
                }))
              }
              className={`w-full rounded-md border px-3 py-2 text-sm text-left ${
                formData.workspace_source === "sandbox"
                  ? "border-primary bg-primary/10 text-foreground"
                  : "border-border bg-background text-muted-foreground"
              }`}
            >
              Sandbox Clone
            </button>
          </Tooltip>
        </div>
        {formData.workspace_source === "sandbox" && (
          <div className="grid gap-3">
            <div>
              <label className="text-xs font-medium text-muted-foreground">
                Repository URL
              </label>
              <Input
                value={formData.repo_url || ""}
                onChange={(e) =>
                  setFormData({ ...formData, repo_url: e.target.value })
                }
                placeholder="https://github.com/org/repo.git"
                className="h-9 text-sm mt-1"
              />
            </div>
            <div className="grid grid-cols-2 gap-3">
              <div>
                <label className="text-xs font-medium text-muted-foreground">
                  Ref
                </label>
                <Input
                  value={formData.repo_ref || ""}
                  onChange={(e) =>
                    setFormData({ ...formData, repo_ref: e.target.value })
                  }
                  placeholder="main"
                  className="h-9 text-sm mt-1"
                />
              </div>
              <div>
                <label className="text-xs font-medium text-muted-foreground">
                  Sandbox Timeout (s)
                </label>
                <Input
                  type="number"
                  min={60}
                  value={formData.sandbox_timeout_s || 3600}
                  onChange={(e) =>
                    setFormData({
                      ...formData,
                      sandbox_timeout_s: Number.parseInt(e.target.value || "0", 10) || 3600,
                    })
                  }
                  className="h-9 text-sm mt-1"
                />
              </div>
            </div>
            <div>
              <label className="text-xs font-medium text-muted-foreground">
                Allowed Egress Domains
              </label>
              <Input
                value={(formData.allow_egress || []).join(", ")}
                onChange={(e) =>
                  setFormData({
                    ...formData,
                    allow_egress: e.target.value
                      .split(",")
                      .map((value) => value.trim())
                      .filter(Boolean),
                  })
                }
                placeholder="api.github.com, codeload.github.com"
                className="h-9 text-sm mt-1"
              />
              <p className="text-[11px] text-muted-foreground mt-1">
                Extra FQDNs allowed from the sandbox in addition to the repo host.
              </p>
            </div>
          </div>
        )}
      </div>

      {(formData.room_id || spawnRoomID) && (
        <div className="rounded-lg border border-primary/20 bg-primary/5 p-3 space-y-3">
          <div>
            <label className="text-sm font-medium text-foreground">
              Spawn Into Room
            </label>
            <p className="text-xs text-muted-foreground mt-1">
              This agent will be attached to a room immediately after spawn.
            </p>
          </div>
          <div className="grid gap-3 md:grid-cols-2">
            <div>
              <label className="text-xs font-medium text-muted-foreground">
                Room ID
              </label>
              <Input
                value={formData.room_id || ""}
                onChange={(e) =>
                  setFormData({ ...formData, room_id: e.target.value })
                }
                placeholder="room id"
                className="h-9 text-sm mt-1"
              />
            </div>
            <div>
              <label className="text-xs font-medium text-muted-foreground">
                Room Role
              </label>
              <Input
                value={formData.room_role || ""}
                onChange={(e) =>
                  setFormData({ ...formData, room_role: e.target.value })
                }
                placeholder="researcher, coder, reviewer..."
                className="h-9 text-sm mt-1"
              />
            </div>
          </div>
        </div>
      )}

      <div>
        <label className="text-sm font-medium text-foreground">
          Memory Retention
        </label>
        <select
          value={formData.memory_retention || "durable"}
          onChange={(e) => {
            const retention = (e.target.value || "durable") as NonNullable<
              SpawnAgentParams["memory_retention"]
            >;
            setFormData({
              ...formData,
              memory_retention: retention,
              memory_scope: recommendedMemoryScopeForRetention(retention),
            });
          }}
          className="w-full mt-1 h-9 rounded-md border border-input bg-background px-3 text-sm"
        >
          <option value="companion">companion</option>
          <option value="durable">durable</option>
          <option value="task">task</option>
          <option value="ephemeral">ephemeral</option>
        </select>
        <p className="text-xs text-muted-foreground mt-1">
          `companion` and `durable` default to stable agent memory. `task` and
          `ephemeral` default to detached session memory.
        </p>
      </div>

      <div>
        <div className="text-sm font-medium text-foreground inline-flex items-center gap-1.5">
          <span>Workbench Memory</span>
          <HelpTooltip
            content="Choose whether this workbench keeps a stable agent-level memory thread or a detached session-level memory thread."
            side="top"
          />
        </div>
        <select
          value={formData.memory_scope || "agent"}
          onChange={(e) =>
            setFormData({
              ...formData,
              memory_scope: e.target.value === "session" ? "session" : "agent",
            })
          }
          className="w-full mt-1 h-9 rounded-md border border-input bg-background px-3 text-sm"
        >
          <option value="agent">agent</option>
          <option value="session">session</option>
        </select>
        <p className="text-xs text-muted-foreground mt-1">
          `agent` persists a stable conversation lineage. `session` keeps the
          workbench chat detached.
        </p>
      </div>

      {/* Role Card Grid */}
      <div>
        <label className="text-sm font-medium text-foreground mb-2 block">
          Role
        </label>
        <RoleSelector
          selectedRole={formData.role}
          onSelectRole={handleRoleChange}
        />
      </div>

      {/* Prompt */}
      <div>
        <div className="flex items-center justify-between mb-1">
          <label className="text-sm font-medium text-foreground">Prompt</label>
          <div className="flex items-center gap-2">
            <button
              type="button"
              onClick={handleUseDefaultPrompt}
              className="text-xs text-primary hover:underline"
            >
              Use default
            </button>
            <button
              type="button"
              onClick={handleEnhancePrompt}
              disabled={isEnhancing || !formData.prompt.trim()}
              className="text-xs text-primary hover:underline flex items-center gap-1 disabled:opacity-50"
            >
              {isEnhancing ? (
                <RefreshCw className="h-3 w-3 animate-spin" />
              ) : (
                <Sparkles className="h-3 w-3" />
              )}
              Enhance
            </button>
          </div>
        </div>
        <textarea
          value={formData.prompt}
          onChange={(e) => setFormData({ ...formData, prompt: e.target.value })}
          placeholder="What should this agent do? Be specific about the task..."
          className="w-full h-24 rounded-md border border-input bg-background px-3 py-2 text-sm resize-none"
        />
      </div>

      {/* Execution Mode */}
      <div>
        <div className="text-sm font-medium text-foreground inline-flex items-center gap-1.5">
          <span>Execution Mode</span>
          <HelpTooltip
            content="Controls whether the agent responds once, continues autonomously, or keeps running on a cadence."
            side="top"
          />
        </div>
        <select
          value={formData.exec_mode}
          onChange={(e) =>
            setFormData({
              ...formData,
              exec_mode: e.target.value as SpawnAgentParams["exec_mode"],
            })
          }
          className="w-full mt-1 h-9 rounded-md border border-input bg-background px-3 text-sm"
        >
          {EXEC_MODES.map((mode) => (
            <option key={mode.id} value={mode.id}>
              {mode.name} - {mode.description}
            </option>
          ))}
        </select>
        <p className="text-xs text-muted-foreground mt-1">
          {EXEC_MODES.find((m) => m.id === formData.exec_mode)?.details}
        </p>
      </div>

      {/* Advanced Options Toggle */}
      <button
        type="button"
        onClick={() => setShowAdvanced(!showAdvanced)}
        className="text-sm text-muted-foreground hover:text-foreground flex items-center gap-1"
      >
        {showAdvanced ? (
          <ChevronDown className="h-4 w-4" />
        ) : (
          <ChevronRight className="h-4 w-4" />
        )}
        Advanced Options
      </button>

      {showAdvanced && (
        <div className="space-y-3 pl-2 border-l-2 border-border">
          {/* Provider & Model Selection */}
          <div className="grid grid-cols-2 gap-3">
            <div>
              <label className="text-xs font-medium text-muted-foreground">
                Provider
              </label>
              <select
                value={formData.llm_provider}
                onChange={(e) => handleProviderChange(e.target.value)}
                className="w-full mt-1 h-8 rounded-md border border-input bg-background px-2 text-sm"
              >
                {PROVIDERS.map((provider) => (
                  <option key={provider.id} value={provider.id}>
                    {provider.name}
                  </option>
                ))}
              </select>
            </div>
            <div>
              <label className="text-xs font-medium text-muted-foreground">
                Model
              </label>
              {selectedProvider?.allowCustom ? (
                <div className="space-y-1 mt-1">
                  <select
                    value={customModel ? "" : formData.llm_model}
                    onChange={(e) => {
                      setFormData({ ...formData, llm_model: e.target.value });
                      setCustomModel("");
                    }}
                    className="w-full h-8 rounded-md border border-input bg-background px-2 text-sm"
                  >
                    {availableModels.map((model) => (
                      <option key={model.id} value={model.id}>
                        {model.name}
                      </option>
                    ))}
                    <option value="">Custom...</option>
                  </select>
                  {(customModel || formData.llm_model === "") && (
                    <Input
                      value={customModel}
                      onChange={(e) => {
                        setCustomModel(e.target.value);
                        setFormData({ ...formData, llm_model: e.target.value });
                      }}
                      placeholder="e.g., anthropic/claude-3-opus"
                      className="h-8 text-sm"
                    />
                  )}
                </div>
              ) : (
                <select
                  value={formData.llm_model}
                  onChange={(e) =>
                    setFormData({ ...formData, llm_model: e.target.value })
                  }
                  className="w-full mt-1 h-8 rounded-md border border-input bg-background px-2 text-sm"
                >
                  {availableModels.map((model) => (
                    <option key={model.id} value={model.id}>
                      {model.name}
                    </option>
                  ))}
                </select>
              )}
            </div>
          </div>

          {/* Iteration Limits */}
          <div className="grid grid-cols-2 gap-3">
            <div>
              <label className="text-xs font-medium text-muted-foreground">
                Max Iterations
              </label>
              <Input
                type="number"
                value={formData.max_iterations}
                onChange={(e) =>
                  setFormData({
                    ...formData,
                    max_iterations: parseInt(e.target.value) || 10,
                  })
                }
                min={1}
                max={100}
                className="h-8 text-sm mt-1"
              />
              <p className="text-xs text-muted-foreground mt-0.5">
                Tool calls per turn
              </p>
            </div>
            <div>
              <label className="text-xs font-medium text-muted-foreground">
                Max Auto Turns
              </label>
              <Input
                type="number"
                value={formData.max_auto_turns}
                onChange={(e) =>
                  setFormData({
                    ...formData,
                    max_auto_turns: parseInt(e.target.value) || 1,
                  })
                }
                min={1}
                max={20}
                className="h-8 text-sm mt-1"
              />
              <p className="text-xs text-muted-foreground mt-0.5">
                Autonomous continuations
              </p>
            </div>
          </div>

          {(formData.exec_mode === "proactive" || formData.exec_mode === "tick") && (
            <div>
              <label className="text-xs font-medium text-muted-foreground">
                Tick Interval (seconds)
              </label>
              <Input
                type="number"
                value={formData.think_interval ?? 60}
                onChange={(e) =>
                  setFormData({
                    ...formData,
                    think_interval: parseInt(e.target.value) || 60,
                  })
                }
                min={1}
                max={86400}
                className="h-8 text-sm mt-1"
              />
              <p className="text-xs text-muted-foreground mt-0.5">
                Cadence for proactive or tick-driven runs
              </p>
            </div>
          )}

          {/* Skills Section */}
          <div>
            <button
              type="button"
              onClick={() => setShowSkills(!showSkills)}
              className="text-xs text-muted-foreground hover:text-foreground flex items-center gap-1"
            >
              {showSkills ? (
                <ChevronDown className="h-3 w-3" />
              ) : (
                <ChevronRight className="h-3 w-3" />
              )}
              Skills ({formData.skills_allow?.length || 0} selected, empty =
              all)
            </button>

            {showSkills && (
              <div className="mt-2 space-y-2 max-h-48 overflow-y-auto">
                <div className="flex gap-2 mb-2">
                  <button
                    type="button"
                    onClick={() =>
                      setFormData({
                        ...formData,
                        skills_allow: skills.map((s) => s.name),
                      })
                    }
                    className="text-xs text-primary hover:underline"
                  >
                    Select All
                  </button>
                  <button
                    type="button"
                    onClick={() =>
                      setFormData({ ...formData, skills_allow: [] })
                    }
                    className="text-xs text-primary hover:underline"
                  >
                    Clear All
                  </button>
                </div>
                {Object.entries(skillsByToolkit).map(
                  ([toolkit, toolkitSkills]) => {
                    const toolkitSkillNames = toolkitSkills.map((s) => s.name);
                    const selectedInToolkit = toolkitSkillNames.filter((name) =>
                      formData.skills_allow?.includes(name),
                    );
                    const allSelected =
                      selectedInToolkit.length === toolkitSkillNames.length;
                    const someSelected =
                      selectedInToolkit.length > 0 && !allSelected;

                    const handleToolkitToggle = () => {
                      const current = formData.skills_allow ?? [];
                      if (allSelected) {
                        // Deselect all in this toolkit
                        setFormData({
                          ...formData,
                          skills_allow: current.filter(
                            (s) => !toolkitSkillNames.includes(s),
                          ),
                        });
                      } else {
                        // Select all in this toolkit
                        const newSkills = [
                          ...new Set([...current, ...toolkitSkillNames]),
                        ];
                        setFormData({ ...formData, skills_allow: newSkills });
                      }
                    };

                    return (
                      <div
                        key={toolkit}
                        className="border border-border rounded-md p-2"
                      >
                        <label className="flex items-center gap-2 mb-2 cursor-pointer">
                          <input
                            type="checkbox"
                            checked={allSelected}
                            ref={(el) => {
                              if (el) el.indeterminate = someSelected;
                            }}
                            onChange={handleToolkitToggle}
                            className="h-3.5 w-3.5"
                          />
                          <span className="text-xs font-medium text-foreground capitalize">
                            {toolkit}
                          </span>
                          <span className="text-xs text-muted-foreground">
                            ({selectedInToolkit.length}/
                            {toolkitSkillNames.length})
                          </span>
                        </label>
                        <div className="grid grid-cols-2 gap-1 pl-5">
                          {toolkitSkills.map((skill) => (
                            <label
                              key={skill.name}
                              className="flex items-start gap-2 text-xs cursor-pointer hover:bg-accent/50 p-1 rounded"
                            >
                              <input
                                type="checkbox"
                                checked={
                                  formData.skills_allow?.includes(skill.name) ??
                                  false
                                }
                                onChange={() => handleSkillToggle(skill.name)}
                                className="mt-0.5"
                              />
                              <Tooltip content={skill.description || skill.name}>
                                <span className="truncate">
                                  {skill.name}
                                </span>
                              </Tooltip>
                            </label>
                          ))}
                        </div>
                      </div>
                    );
                  },
                )}
              </div>
            )}
          </div>
        </div>
      )}

      {/* Actions */}
      <div className="flex items-center gap-2 pt-2">
        <Button type="submit" disabled={!formData.prompt.trim() || isPending}>
          {isPending ? (
            <>
              <RefreshCw className="h-4 w-4 mr-1 animate-spin" />
              Spawning...
            </>
          ) : (
            <>
              <Plus className="h-4 w-4 mr-1" />
              Spawn Agent
            </>
          )}
        </Button>
        <Button type="button" variant="ghost" onClick={onCancel}>
          Cancel
        </Button>
      </div>

      {error && (
        <p className="text-sm text-red-500">
          Error: {error.message || "Unknown error"}
        </p>
      )}
    </form>
  );
}
