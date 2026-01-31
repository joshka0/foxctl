import { useState, useMemo } from 'react'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { ScrollArea } from '@/components/ui/scroll-area'
import { spawnAgent, companionChat, listSkills, type SpawnAgentParams } from '@/api/client'
import type { AgentSpawnResponse } from '@/api/types'
import {
  X,
  Plus,
  RefreshCw,
  ChevronDown,
  ChevronRight,
  Sparkles,
} from 'lucide-react'
import { RoleSelector } from '@/components/agents/RoleSelector'
import { EXEC_MODES, PROVIDERS, getRoleById, getProviderById } from '@/components/agents/spawnFormConstants'
import type { ViewType } from '@/App'

interface SpawnAgentPanelProps {
  onClose: () => void
  onViewChange: (view: ViewType) => void
}

export function SpawnAgentPanel({ onClose, onViewChange }: SpawnAgentPanelProps) {
  const queryClient = useQueryClient()
  const [showAdvanced, setShowAdvanced] = useState(false)
  const [showSkills, setShowSkills] = useState(false)
  const [isEnhancing, setIsEnhancing] = useState(false)
  const [customModel, setCustomModel] = useState('')
  const [formData, setFormData] = useState<SpawnAgentParams>({
    role: 'coder',
    prompt: '',
    name: '',
    exec_mode: 'reactive',
    llm_provider: '',
    llm_model: '',
    max_iterations: 10,
    max_auto_turns: 1,
    skills_allow: [],
  })

  // Fetch available skills
  const { data: skillsData } = useQuery({
    queryKey: ['skills'],
    queryFn: listSkills,
  })

  const skills = useMemo(() => skillsData?.skills ?? [], [skillsData?.skills])
  // Group skills by toolkit (first tag) for easier selection
  const skillsByToolkit = useMemo(() => {
    const toolkits: Record<string, typeof skills> = {}
    const seen = new Set<string>()
    for (const skill of skills) {
      if (seen.has(skill.name)) continue
      seen.add(skill.name)
      const toolkit = skill.tags?.[0] || skill.name.split('/')[0] || 'other'
      if (!toolkits[toolkit]) toolkits[toolkit] = []
      toolkits[toolkit].push(skill)
    }
    return Object.fromEntries(
      Object.entries(toolkits).sort(([a], [b]) => a.localeCompare(b))
    )
  }, [skills])

  const mutation = useMutation({
    mutationFn: () => {
      const params: SpawnAgentParams = {
        role: formData.role,
        prompt: formData.prompt,
      }
      if (formData.name?.trim()) params.name = formData.name.trim()
      if (formData.exec_mode && formData.exec_mode !== 'reactive') {
        params.exec_mode = formData.exec_mode
      }
      if (formData.llm_provider) params.llm_provider = formData.llm_provider
      if (formData.llm_model) params.llm_model = formData.llm_model
      if (formData.max_iterations && formData.max_iterations !== 10) {
        params.max_iterations = formData.max_iterations
      }
      if (formData.max_auto_turns && formData.max_auto_turns !== 1) {
        params.max_auto_turns = formData.max_auto_turns
      }
      if (formData.skills_allow && formData.skills_allow.length > 0) {
        params.skills_allow = formData.skills_allow
      }
      return spawnAgent(params)
    },
    onSuccess: (data: AgentSpawnResponse) => {
      console.log('[SpawnAgentPanel] Spawn success:', data)
      // Invalidate agents list to show the new agent
      queryClient.invalidateQueries({ queryKey: ['agents'] })
      // Navigate to agents view and select the new agent
      onViewChange('agents')
      onClose()
    },
    onError: (error) => {
      console.error('[SpawnAgentPanel] Spawn failed:', error)
    },
  })

  const handleSubmit = (e: React.FormEvent) => {
    e.preventDefault()
    if (formData.prompt.trim()) {
      mutation.mutate()
    }
  }

  const handleRoleChange = (roleId: string) => {
    const role = getRoleById(roleId)
    const currentRole = getRoleById(formData.role)
    const shouldAutoFill = !formData.prompt.trim() ||
      (currentRole && formData.prompt.trim() === currentRole.defaultPrompt)

    setFormData({
      ...formData,
      role: roleId,
      prompt: shouldAutoFill && role ? role.defaultPrompt : formData.prompt,
    })
  }

  const handleUseDefaultPrompt = () => {
    const role = getRoleById(formData.role)
    if (role) {
      setFormData({ ...formData, prompt: role.defaultPrompt })
    }
  }

  const handleEnhancePrompt = async () => {
    if (!formData.prompt.trim()) return
    setIsEnhancing(true)
    try {
      const result = await companionChat({
        conversation_id: `enhance-${Date.now()}`,
        message: `Improve and expand this agent system prompt for a ${formData.role} role. Make it more specific and actionable while keeping the same intent. Return ONLY the improved prompt text, no explanation:\n\n${formData.prompt}`,
      })
      if (result.response) {
        setFormData({ ...formData, prompt: result.response.trim() })
      }
    } catch (err) {
      console.error('Failed to enhance prompt:', err)
    } finally {
      setIsEnhancing(false)
    }
  }

  const handleProviderChange = (providerId: string) => {
    const provider = getProviderById(providerId)
    const firstModel = provider?.models[0]?.id ?? ''
    setFormData({
      ...formData,
      llm_provider: providerId,
      llm_model: firstModel,
    })
    setCustomModel('')
  }

  const handleSkillToggle = (skillName: string) => {
    const current = formData.skills_allow ?? []
    const updated = current.includes(skillName)
      ? current.filter((s) => s !== skillName)
      : [...current, skillName]
    setFormData({ ...formData, skills_allow: updated })
  }

  const selectedProvider = getProviderById(formData.llm_provider || '')
  const availableModels = selectedProvider?.models ?? []

  return (
    <div className="fixed inset-0 z-50 flex">
      {/* Backdrop */}
      <div
        className="absolute inset-0 bg-background/80 backdrop-blur-sm"
        onClick={onClose}
      />

      {/* Panel */}
      <div className="absolute right-0 top-0 bottom-0 w-full max-w-lg bg-card border-l border-border shadow-xl flex flex-col">
        {/* Header */}
        <div className="h-12 flex items-center justify-between px-4 border-b border-border">
          <h2 className="text-sm font-semibold text-foreground">Spawn New Agent</h2>
          <Button variant="ghost" size="icon" className="h-7 w-7" onClick={onClose}>
            <X className="h-4 w-4" />
          </Button>
        </div>

        {/* Form Content */}
        <ScrollArea className="flex-1">
          <form onSubmit={handleSubmit} className="p-4 space-y-4">
            {/* Name */}
            <div>
              <label className="text-sm font-medium text-foreground">Name</label>
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

            {/* Role Card Grid */}
            <div>
              <label className="text-sm font-medium text-foreground mb-2 block">Role</label>
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
                    title="Enhance prompt with AI"
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
              <label className="text-sm font-medium text-foreground">Execution Mode</label>
              <select
                value={formData.exec_mode}
                onChange={(e) => setFormData({ ...formData, exec_mode: e.target.value as SpawnAgentParams['exec_mode'] })}
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
              {showAdvanced ? <ChevronDown className="h-4 w-4" /> : <ChevronRight className="h-4 w-4" />}
              Advanced Options
            </button>

            {showAdvanced && (
              <div className="space-y-3 pl-2 border-l-2 border-border">
                {/* Provider & Model Selection */}
                <div className="grid grid-cols-2 gap-3">
                  <div>
                    <label className="text-xs font-medium text-muted-foreground">Provider</label>
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
                    <label className="text-xs font-medium text-muted-foreground">Model</label>
                    {selectedProvider?.allowCustom ? (
                      <div className="space-y-1 mt-1">
                        <select
                          value={customModel ? '' : formData.llm_model}
                          onChange={(e) => {
                            setFormData({ ...formData, llm_model: e.target.value })
                            setCustomModel('')
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
                        {(customModel || formData.llm_model === '') && (
                          <Input
                            value={customModel}
                            onChange={(e) => {
                              setCustomModel(e.target.value)
                              setFormData({ ...formData, llm_model: e.target.value })
                            }}
                            placeholder="e.g., anthropic/claude-3-opus"
                            className="h-8 text-sm"
                          />
                        )}
                      </div>
                    ) : (
                      <select
                        value={formData.llm_model}
                        onChange={(e) => setFormData({ ...formData, llm_model: e.target.value })}
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
                    <label className="text-xs font-medium text-muted-foreground">Max Iterations</label>
                    <Input
                      type="number"
                      value={formData.max_iterations}
                      onChange={(e) => setFormData({ ...formData, max_iterations: parseInt(e.target.value) || 10 })}
                      min={1}
                      max={100}
                      className="h-8 text-sm mt-1"
                    />
                    <p className="text-xs text-muted-foreground mt-0.5">Tool calls per turn</p>
                  </div>
                  <div>
                    <label className="text-xs font-medium text-muted-foreground">Max Auto Turns</label>
                    <Input
                      type="number"
                      value={formData.max_auto_turns}
                      onChange={(e) => setFormData({ ...formData, max_auto_turns: parseInt(e.target.value) || 1 })}
                      min={1}
                      max={20}
                      className="h-8 text-sm mt-1"
                    />
                    <p className="text-xs text-muted-foreground mt-0.5">Autonomous continuations</p>
                  </div>
                </div>

                {/* Skills Section */}
                <div>
                  <button
                    type="button"
                    onClick={() => setShowSkills(!showSkills)}
                    className="text-xs text-muted-foreground hover:text-foreground flex items-center gap-1"
                  >
                    {showSkills ? <ChevronDown className="h-3 w-3" /> : <ChevronRight className="h-3 w-3" />}
                    Skills ({formData.skills_allow?.length || 0} selected, empty = all)
                  </button>

                  {showSkills && (
                    <div className="mt-2 space-y-2 max-h-48 overflow-y-auto">
                      <div className="flex gap-2 mb-2">
                        <button
                          type="button"
                          onClick={() => setFormData({ ...formData, skills_allow: skills.map((s) => s.name) })}
                          className="text-xs text-primary hover:underline"
                        >
                          Select All
                        </button>
                        <button
                          type="button"
                          onClick={() => setFormData({ ...formData, skills_allow: [] })}
                          className="text-xs text-primary hover:underline"
                        >
                          Clear All
                        </button>
                      </div>
                      {Object.entries(skillsByToolkit).map(([toolkit, toolkitSkills]) => {
                        const toolkitSkillNames = toolkitSkills.map((s) => s.name)
                        const selectedInToolkit = toolkitSkillNames.filter((name) => formData.skills_allow?.includes(name))
                        const allSelected = selectedInToolkit.length === toolkitSkillNames.length
                        const someSelected = selectedInToolkit.length > 0 && !allSelected

                        const handleToolkitToggle = () => {
                          const current = formData.skills_allow ?? []
                          if (allSelected) {
                            setFormData({ ...formData, skills_allow: current.filter((s) => !toolkitSkillNames.includes(s)) })
                          } else {
                            const newSkills = [...new Set([...current, ...toolkitSkillNames])]
                            setFormData({ ...formData, skills_allow: newSkills })
                          }
                        }

                        return (
                          <div key={toolkit} className="border border-border rounded-md p-2">
                            <label className="flex items-center gap-2 mb-2 cursor-pointer">
                              <input
                                type="checkbox"
                                checked={allSelected}
                                ref={(el) => { if (el) el.indeterminate = someSelected }}
                                onChange={handleToolkitToggle}
                                className="h-3.5 w-3.5"
                              />
                              <span className="text-xs font-medium text-foreground capitalize">{toolkit}</span>
                              <span className="text-xs text-muted-foreground">({selectedInToolkit.length}/{toolkitSkillNames.length})</span>
                            </label>
                            <div className="grid grid-cols-2 gap-1 pl-5">
                              {toolkitSkills.map((skill) => (
                                <label
                                  key={skill.name}
                                  className="flex items-start gap-2 text-xs cursor-pointer hover:bg-accent/50 p-1 rounded"
                                >
                                  <input
                                    type="checkbox"
                                    checked={formData.skills_allow?.includes(skill.name) ?? false}
                                    onChange={() => handleSkillToggle(skill.name)}
                                    className="mt-0.5"
                                  />
                                  <span className="truncate" title={skill.description}>
                                    {skill.name}
                                  </span>
                                </label>
                              ))}
                            </div>
                          </div>
                        )
                      })}
                    </div>
                  )}
                </div>
              </div>
            )}

            {/* Actions */}
            <div className="flex items-center gap-2 pt-2">
              <Button type="submit" disabled={!formData.prompt.trim() || mutation.isPending}>
                {mutation.isPending ? (
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
              <Button type="button" variant="ghost" onClick={onClose}>
                Cancel
              </Button>
            </div>

            {mutation.isError && (
              <p className="text-sm text-red-500">
                Error: {mutation.error instanceof Error ? mutation.error.message : 'Unknown error'}
              </p>
            )}
          </form>
        </ScrollArea>
      </div>
    </div>
  )
}
