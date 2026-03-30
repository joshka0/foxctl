import {
  Bot,
  Code,
  Eye,
  Map,
  Wrench,
  CheckCircle,
  Search,
  Heart,
  Users,
  type LucideIcon,
} from 'lucide-react'

export interface RoleConfig {
  id: string
  name: string
  icon: LucideIcon
  description: string
  defaultPrompt: string
}

export const ROLES: RoleConfig[] = [
  {
    id: 'assistant',
    name: 'Assistant',
    icon: Bot,
    description: 'General-purpose helper',
    defaultPrompt:
      'You are a helpful generalist agent. Be clear, concise, and honest about uncertainty. Ask clarifying questions when requirements are unclear.',
  },
  {
    id: 'coder',
    name: 'Coder',
    icon: Code,
    description: 'Write & modify code',
    defaultPrompt:
      'You are a coding agent. Make small, correct changes, explain briefly, and prefer tests. Ask when requirements are unclear.',
  },
  {
    id: 'reviewer',
    name: 'Reviewer',
    icon: Eye,
    description: 'Review code changes',
    defaultPrompt:
      'You are a code reviewer. Focus on bugs, risks, regressions, and missing tests. Prioritize issues by severity.',
  },
  {
    id: 'planner',
    name: 'Planner',
    icon: Map,
    description: 'Create implementation plans',
    defaultPrompt:
      'You are a planning agent. Produce step-by-step implementation plans with risks and dependencies. Avoid writing code unless asked.',
  },
  {
    id: 'fixer',
    name: 'Fixer',
    icon: Wrench,
    description: 'Debug and fix issues',
    defaultPrompt:
      'You are a debugging agent. Reproduce issues, identify root causes, and apply minimal fixes. Add or suggest tests.',
  },
  {
    id: 'verifier',
    name: 'Verifier',
    icon: CheckCircle,
    description: 'Validate changes',
    defaultPrompt:
      'You are a verification agent. Validate changes via tests or reasoning, and report failures clearly. Do not change code unless asked.',
  },
  {
    id: 'researcher',
    name: 'Researcher',
    icon: Search,
    description: 'Gather information',
    defaultPrompt:
      'You are a research agent. Gather evidence, summarize with citations, and note uncertainties or gaps.',
  },
  {
    id: 'companion',
    name: 'Companion',
    icon: Heart,
    description: 'Conversational chat',
    defaultPrompt:
      'You are a friendly conversational companion. Be warm and concise, remember preferences, and ask thoughtful follow-up questions.',
  },
  {
    id: 'overseer',
    name: 'Overseer',
    icon: Users,
    description: 'Coordinate agents',
    defaultPrompt:
      'You are an oversight agent. Coordinate tasks, delegate work, and request human decisions when needed.',
  },
]

export interface ExecModeConfig {
  id: 'reactive' | 'autonomous' | 'proactive' | 'tick' | 'story'
  name: string
  description: string
  details: string
}

export const EXEC_MODES: ExecModeConfig[] = [
  {
    id: 'reactive',
    name: 'Reactive',
    description: 'Responds to input',
    details: 'Agent waits for messages and responds. Best for Q&A, code review requests.',
  },
  {
    id: 'autonomous',
    name: 'Autonomous',
    description: 'Works independently',
    details: 'Agent continues working across multiple turns toward a goal. Best for complex tasks.',
  },
  {
    id: 'proactive',
    name: 'Proactive',
    description: 'Self-initiating',
    details: 'Agent can start work on its own based on triggers. Best for monitoring/automation.',
  },
  {
    id: 'tick',
    name: 'Tick',
    description: 'Runs on interval',
    details: 'Agent wakes up on a fixed cadence and advances work or simulations one tick at a time.',
  },
  {
    id: 'story',
    name: 'Story',
    description: 'Narrative flow',
    details: 'Agent runs a gather-then-dialogue loop. Best for research and exploration.',
  },
]

export interface ModelConfig {
  id: string
  name: string
}

export interface ProviderConfig {
  id: string
  name: string
  models: ModelConfig[]
  allowCustom?: boolean
}

export const PROVIDERS: ProviderConfig[] = [
  {
    id: '',
    name: 'Default (from env)',
    models: [{ id: '', name: 'Default model' }],
  },
  {
    id: 'anthropic',
    name: 'Anthropic',
    models: [
      { id: 'claude-sonnet-4-5-20250514', name: 'Claude Sonnet 4.5' },
      { id: 'claude-haiku-4-5-20250514', name: 'Claude Haiku 4.5' },
      { id: 'claude-opus-4-5-20250514', name: 'Claude Opus 4.5' },
    ],
  },
  {
    id: 'openai',
    name: 'OpenAI',
    models: [
      { id: 'gpt-5.2', name: 'GPT-5.2' },
      { id: 'gpt-5.1', name: 'GPT-5.1' },
      { id: 'gpt-5', name: 'GPT-5' },
      { id: 'gpt-5-mini', name: 'GPT-5 Mini' },
      { id: 'gpt-5-nano', name: 'GPT-5 Nano' },
      { id: 'gpt-4.1', name: 'GPT-4.1' },
      { id: 'gpt-4.1-mini', name: 'GPT-4.1 Mini' },
      { id: 'gpt-4.1-nano', name: 'GPT-4.1 Nano' },
      { id: 'gpt-4o', name: 'GPT-4o' },
      { id: 'gpt-4o-mini', name: 'GPT-4o Mini' },
      { id: 'gpt-realtime', name: 'GPT Realtime' },
      { id: 'gpt-realtime-mini', name: 'GPT Realtime Mini' },
    ],
  },
  {
    id: 'gemini',
    name: 'Google Gemini',
    models: [
      { id: 'gemini-2.5-flash', name: 'Gemini 2.5 Flash' },
      { id: 'gemini-2.5-pro', name: 'Gemini 2.5 Pro' },
    ],
  },
  {
    id: 'groq',
    name: 'Groq',
    models: [
      { id: 'llama-3.3-70b-versatile', name: 'Llama 3.3 70B' },
      { id: 'llama-3.1-8b-instant', name: 'Llama 3.1 8B Instant' },
      { id: 'openai/gpt-oss-120b', name: 'GPT OSS 120B' },
      { id: 'openai/gpt-oss-20b', name: 'GPT OSS 20B' },
      { id: 'meta-llama/llama-4-maverick-17b-128e-instruct', name: 'Llama 4 Maverick 17B' },
      { id: 'meta-llama/llama-4-scout-17b-16e-instruct', name: 'Llama 4 Scout 17B' },
      { id: 'qwen/qwen3-32b', name: 'Qwen 3 32B' },
      { id: 'groq/compound', name: 'Compound' },
      { id: 'groq/compound-mini', name: 'Compound Mini' },
    ],
  },
  {
    id: 'cerebras',
    name: 'Cerebras',
    models: [
      { id: 'llama3.1-8b', name: 'Llama 3.1 8B (~2200 t/s)' },
      { id: 'llama-3.3-70b', name: 'Llama 3.3 70B (~2100 t/s)' },
      { id: 'gpt-oss-120b', name: 'GPT OSS 120B (~3000 t/s)' },
      { id: 'qwen-3-32b', name: 'Qwen 3 32B (~2600 t/s)' },
      { id: 'qwen-3-235b-a22b-instruct-2507', name: 'Qwen 3 235B (~1400 t/s)' },
      { id: 'zai-glm-4.7', name: 'Z.ai GLM 4.7 (~1000 t/s)' },
    ],
  },
  {
    id: 'lmstudio',
    name: 'LM Studio',
    models: [{ id: '', name: 'Default model' }],
    allowCustom: true,
  },
  {
    id: 'openrouter',
    name: 'OpenRouter',
    models: [
      { id: 'google/gemini-3.1-flash-lite-preview', name: 'Gemini 3.1 Flash Lite Preview' },
      { id: 'mistralai/devstral-2512', name: 'Devstral' },
      { id: 'anthropic/claude-3-haiku', name: 'Claude 3 Haiku' },
      { id: 'meta-llama/llama-3-70b', name: 'Llama 3 70B' },
    ],
    allowCustom: true,
  },
]

// Companion 2-stage model configuration
// Stage 1: Tool calling model (handles function calls)
// Stage 2: Response model (generates conversational responses)
export interface CompanionModelConfig {
  id: string
  name: string
  provider: string
}

export const COMPANION_TOOL_MODELS: CompanionModelConfig[] = [
  { id: 'google/gemini-3.1-flash-lite-preview', name: 'Gemini 3.1 Flash Lite Preview', provider: 'openrouter' },
  { id: 'z-ai/glm-4.7-flash', name: 'GLM 4.7 Flash', provider: 'openrouter' },
  { id: 'moonshotai/kimi-k2.5', name: 'Kimi K2.5', provider: 'openrouter' },
  { id: 'minimax/minimax-m2.1', name: 'MiniMax M2.1', provider: 'openrouter' },
  { id: 'mistralai/devstral-2512', name: 'Devstral', provider: 'openrouter' },
]

export const COMPANION_RESPONSE_MODELS: CompanionModelConfig[] = [
  { id: 'google/gemini-3.1-flash-lite-preview', name: 'Gemini 3.1 Flash Lite Preview', provider: 'openrouter' },
  { id: 'minimax/minimax-m2-her', name: 'MiniMax M2 Her', provider: 'openrouter' },
  { id: 'mistralai/mistral-small-creative', name: 'Mistral Small Creative', provider: 'openrouter' },
]

/**
 * Retrieve a role configuration by its id.
 *
 * @param id - The role identifier to look up
 * @returns The `RoleConfig` matching `id`, or `undefined` if no match is found
 */
export function getRoleById(id: string): RoleConfig | undefined {
  return ROLES.find((r) => r.id === id)
}

/**
 * Finds a provider configuration by its identifier.
 *
 * @param id - The provider `id` to look up
 * @returns The matching `ProviderConfig`, or `undefined` if no provider has the given `id`
 */
export function getProviderById(id: string): ProviderConfig | undefined {
  return PROVIDERS.find((p) => p.id === id)
}

/**
 * Retrieve the model list for a given provider identifier.
 *
 * @param providerId - The provider's `id` as listed in the providers catalog
 * @returns The provider's array of `ModelConfig` entries, or an empty array if no provider matches `providerId`
 */
export function getModelsForProvider(providerId: string): ModelConfig[] {
  const provider = getProviderById(providerId)
  return provider?.models ?? []
}

export function mergeModelsForProvider(
  providerId: string,
  discoveredModels: ModelConfig[] = [],
): ModelConfig[] {
  const merged = new globalThis.Map<string, ModelConfig>()
  for (const model of getModelsForProvider(providerId)) {
    merged.set(model.id, model)
  }
  for (const model of discoveredModels) {
    if (!model?.id) continue
    merged.set(model.id, { id: model.id, name: model.name || model.id })
  }
  return Array.from(merged.values()).sort((a, b) => {
    if (a.id === '') return -1
    if (b.id === '') return 1
    return a.name.localeCompare(b.name)
  })
}
