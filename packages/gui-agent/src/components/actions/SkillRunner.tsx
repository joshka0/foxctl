import { useState, useMemo } from 'react'
import { ScrollArea } from '@/components/ui/scroll-area'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Badge } from '@/components/ui/badge'
import { Label } from '@/components/ui/label'
import { Switch } from '@/components/ui/switch'
import { cn } from '@/lib/utils'
import {
  Play,
  Search,
  Zap,
  ChevronRight,
  Loader2,
  CheckCircle,
  XCircle,
  Code,
  Folder,
} from 'lucide-react'
import { runSkill } from '@/api/client'

// Skill parameter definitions
interface SkillParam {
  name: string
  type: 'string' | 'number' | 'boolean'
  description: string
  default?: string | number | boolean
  required?: boolean
}

interface SkillDefinition {
  name: string
  description: string
  params: SkillParam[]
}

// Skill definitions with their parameters
const SKILL_DEFINITIONS: SkillDefinition[] = [
  {
    name: 'code/semantic_search',
    description: 'Search code by concept',
    params: [
      {
        name: 'query',
        type: 'string',
        description: 'What to search for',
        required: true,
      },
      {
        name: 'limit',
        type: 'number',
        description: 'Max results',
        default: 10,
      },
      {
        name: 'format',
        type: 'string',
        description: 'Output format (tree or list)',
        default: 'tree',
      },
    ],
  },
  {
    name: 'memory/query',
    description: 'Query memory store',
    params: [
      {
        name: 'query',
        type: 'string',
        description: 'Search query',
        required: true,
      },
      {
        name: 'type',
        type: 'string',
        description: 'Memory type (gotcha, insight, etc)',
      },
      { name: 'limit', type: 'number', description: 'Max results', default: 10 },
    ],
  },
  {
    name: 'todo/manage',
    description: 'Manage tasks',
    params: [
      {
        name: 'action',
        type: 'string',
        description: 'Action: list, add, complete, delete',
        default: 'list',
        required: true,
      },
      { name: 'id', type: 'string', description: 'Task ID (for complete/delete)' },
      { name: 'title', type: 'string', description: 'Task title (for add)' },
      { name: 'description', type: 'string', description: 'Task description' },
    ],
  },
  {
    name: 'obs/logs',
    description: 'View observability logs',
    params: [
      { name: 'limit', type: 'number', description: 'Max entries', default: 50 },
      {
        name: 'since',
        type: 'string',
        description: 'Time range (e.g., 1h, 30m)',
        default: '1h',
      },
      {
        name: 'errors_only',
        type: 'boolean',
        description: 'Only show errors',
        default: false,
      },
      { name: 'component', type: 'string', description: 'Filter by component' },
    ],
  },
  {
    name: 'code/smart_search',
    description: 'Smart code search + extract',
    params: [
      {
        name: 'query',
        type: 'string',
        description: 'What to search for',
        required: true,
      },
      { name: 'limit', type: 'number', description: 'Max results', default: 5 },
      {
        name: 'extract',
        type: 'boolean',
        description: 'Extract code snippets',
        default: true,
      },
    ],
  },
  {
    name: 'codemap/generate',
    description: 'Generate AI code relationships',
    params: [
      {
        name: 'prompt',
        type: 'string',
        description: 'What relationships to trace',
        required: true,
      },
      {
        name: 'max_files',
        type: 'number',
        description: 'Max files to analyze',
        default: 20,
      },
    ],
  },
]

interface SkillResult {
  status: 'success' | 'error'
  output?: string
  error?: string
  duration_ms?: number
}

/**
 * Interactive UI to search, configure, and execute predefined skills.
 *
 * Renders a two-pane interface with a searchable list of skills on the left and
 * a configuration / output panel on the right. Users can select a skill,
 * edit its parameters via form fields or raw JSON, optionally specify a
 * workspace path, and run the skill. Execution results (success or error),
 * formatted output, and execution duration are displayed in the output panel.
 *
 * The component manages its own selection, form state, raw JSON view, running
 * state, and result state, and it POSTs the chosen skill and input to
 * /api/skills/run when the user triggers a run.
 *
 * @returns A React element rendering the skill runner user interface.
 */
export function SkillRunner() {
  const [selectedSkill, setSelectedSkill] = useState<string>('')
  const [searchQuery, setSearchQuery] = useState('')
  const [isRunning, setIsRunning] = useState(false)
  const [result, setResult] = useState<SkillResult | null>(null)
  const [showRawJson, setShowRawJson] = useState(false)
  const [rawJson, setRawJson] = useState('{}')
  const [workspace, setWorkspace] = useState('')  // Workspace path for scoped operations

  // Form values for the selected skill
  const [formValues, setFormValues] = useState<Record<string, string | number | boolean>>({})

  const filteredSkills = searchQuery
    ? SKILL_DEFINITIONS.filter(
        (s) =>
          s.name.toLowerCase().includes(searchQuery.toLowerCase()) ||
          s.description.toLowerCase().includes(searchQuery.toLowerCase())
      )
    : SKILL_DEFINITIONS

  const currentSkill = SKILL_DEFINITIONS.find((s) => s.name === selectedSkill)

  // Build input JSON from form values
  const inputJson = useMemo(() => {
    if (showRawJson) return rawJson

    const input: Record<string, unknown> = {}
    if (currentSkill) {
      for (const param of currentSkill.params) {
        const value = formValues[param.name]
        if (value !== undefined && value !== '' && value !== param.default) {
          if (param.type === 'number' && typeof value === 'string') {
            const num = parseFloat(value)
            if (!isNaN(num)) input[param.name] = num
          } else if (param.type === 'boolean') {
            input[param.name] = value === true || value === 'true'
          } else {
            input[param.name] = value
          }
        } else if (value !== undefined && value !== '') {
          // Include even if it matches default (user explicitly set it)
          if (param.type === 'number' && typeof value === 'string') {
            const num = parseFloat(value)
            if (!isNaN(num)) input[param.name] = num
          } else if (param.type === 'boolean') {
            input[param.name] = value === true || value === 'true'
          } else {
            input[param.name] = value
          }
        }
      }
    }
    return JSON.stringify(input, null, 2)
  }, [formValues, currentSkill, showRawJson, rawJson])

  const handleSelectSkill = (skillName: string) => {
    setSelectedSkill(skillName)
    setResult(null)
    // Reset form values with defaults
    const skill = SKILL_DEFINITIONS.find((s) => s.name === skillName)
    if (skill) {
      const defaults: Record<string, string | number | boolean> = {}
      for (const param of skill.params) {
        if (param.default !== undefined) {
          defaults[param.name] = param.default
        }
      }
      setFormValues(defaults)
      setRawJson(JSON.stringify(defaults, null, 2))
    }
  }

  const handleParamChange = (name: string, value: string | number | boolean) => {
    setFormValues((prev) => ({ ...prev, [name]: value }))
  }

  const handleRun = async () => {
    if (!selectedSkill) return

    setIsRunning(true)
    setResult(null)

    try {
      // Parse input JSON
      let input: Record<string, unknown> = {}
      try {
        input = JSON.parse(inputJson)
      } catch {
        setResult({ status: 'error', error: 'Invalid JSON input' })
        setIsRunning(false)
        return
      }

      // Add workspace to input if specified (for skills that support it)
      if (workspace.trim()) {
        input.workspace = workspace.trim()
      }

      const data = await runSkill(selectedSkill, input)
      if (!data.ok) {
        setResult({ status: 'error', error: data.error || 'Skill returned error' })
      } else {
        setResult({
          status: 'success',
          output: JSON.stringify(data, null, 2),
          duration_ms: data.duration_ms,
        })
      }
    } catch (err) {
      setResult({
        status: 'error',
        error: err instanceof Error ? err.message : 'Unknown error',
      })
    } finally {
      setIsRunning(false)
    }
  }

  return (
    <div className="flex flex-col h-full">
      {/* Header */}
      <div className="p-4 border-b border-border">
        <h2 className="text-lg font-semibold text-foreground mb-3">Run Skill</h2>
        <div className="relative">
          <Search className="absolute left-2.5 top-2.5 h-4 w-4 text-muted-foreground" />
          <Input
            placeholder="Search skills..."
            value={searchQuery}
            onChange={(e) => setSearchQuery(e.target.value)}
            className="pl-9"
          />
        </div>
      </div>

      <div className="flex-1 flex overflow-hidden">
        {/* Skill List */}
        <div className="w-64 border-r border-border">
          <ScrollArea className="h-full">
            <div className="p-2 space-y-1">
              {filteredSkills.map((skill) => (
                <button
                  key={skill.name}
                  onClick={() => handleSelectSkill(skill.name)}
                  className={cn(
                    'w-full text-left px-3 py-2 rounded-md text-sm transition-colors',
                    selectedSkill === skill.name
                      ? 'bg-primary text-primary-foreground'
                      : 'hover:bg-accent'
                  )}
                >
                  <div className="flex items-center gap-2">
                    <Zap className="h-4 w-4 flex-shrink-0" />
                    <div className="min-w-0">
                      <div className="font-medium truncate">{skill.name}</div>
                      <div
                        className={cn(
                          'text-xs truncate',
                          selectedSkill === skill.name
                            ? 'text-primary-foreground/70'
                            : 'text-muted-foreground'
                        )}
                      >
                        {skill.description}
                      </div>
                    </div>
                  </div>
                </button>
              ))}
            </div>
          </ScrollArea>
        </div>

        {/* Skill Config & Output */}
        <div className="flex-1 flex flex-col">
          {currentSkill ? (
            <>
              {/* Input Form */}
              <div className="p-4 border-b border-border">
                <div className="flex items-center justify-between mb-3">
                  <div className="flex items-center gap-2">
                    <Badge variant="outline">{currentSkill.name}</Badge>
                  </div>
                  <div className="flex items-center gap-2">
                    <Label htmlFor="raw-json" className="text-xs text-muted-foreground">
                      Raw JSON
                    </Label>
                    <Switch
                      id="raw-json"
                      checked={showRawJson}
                      onCheckedChange={setShowRawJson}
                    />
                  </div>
                </div>

                {showRawJson ? (
                  <div className="space-y-2">
                    <textarea
                      value={rawJson}
                      onChange={(e) => setRawJson(e.target.value)}
                      className="w-full h-32 rounded-md border border-input bg-background px-3 py-2 text-sm font-mono resize-none focus:outline-none focus:ring-2 focus:ring-ring"
                      placeholder='{"query": "example"}'
                    />
                  </div>
                ) : (
                  <div className="space-y-3 max-h-48 overflow-y-auto">
                    {currentSkill.params.map((param) => (
                      <div key={param.name} className="space-y-1">
                        <div className="flex items-center gap-2">
                          <Label
                            htmlFor={param.name}
                            className="text-sm font-medium"
                          >
                            {param.name}
                            {param.required && (
                              <span className="text-red-500 ml-1">*</span>
                            )}
                          </Label>
                          <span className="text-xs text-muted-foreground">
                            {param.description}
                          </span>
                        </div>
                        {param.type === 'boolean' ? (
                          <Switch
                            id={param.name}
                            checked={
                              formValues[param.name] === true ||
                              formValues[param.name] === 'true'
                            }
                            onCheckedChange={(checked) =>
                              handleParamChange(param.name, checked)
                            }
                          />
                        ) : (
                          <Input
                            id={param.name}
                            type={param.type === 'number' ? 'number' : 'text'}
                            value={String(formValues[param.name] ?? '')}
                            onChange={(e) =>
                              handleParamChange(param.name, e.target.value)
                            }
                            placeholder={
                              param.default !== undefined
                                ? `Default: ${param.default}`
                                : undefined
                            }
                            className="h-8"
                          />
                        )}
                      </div>
                    ))}
                  </div>
                )}

                {/* Workspace override */}
                <div className="space-y-1 mt-3">
                  <div className="flex items-center gap-2">
                    <Label htmlFor="workspace" className="text-sm font-medium">
                      Workspace
                    </Label>
                    <span className="text-xs text-muted-foreground">
                      Optional path for scoped operations
                    </span>
                  </div>
                  <div className="relative">
                    <Folder className="absolute left-2.5 top-2 h-4 w-4 text-muted-foreground" />
                    <Input
                      id="workspace"
                      value={workspace}
                      onChange={(e) => setWorkspace(e.target.value)}
                      placeholder="/path/to/project"
                      className="h-8 pl-9"
                    />
                  </div>
                </div>

                <div className="flex items-center gap-2 mt-3">
                  <Button onClick={handleRun} disabled={isRunning}>
                    {isRunning ? (
                      <>
                        <Loader2 className="h-4 w-4 mr-2 animate-spin" />
                        Running...
                      </>
                    ) : (
                      <>
                        <Play className="h-4 w-4 mr-2" />
                        Run
                      </>
                    )}
                  </Button>
                  {!showRawJson && (
                    <Button
                      variant="outline"
                      size="sm"
                      onClick={() => {
                        setRawJson(inputJson)
                        setShowRawJson(true)
                      }}
                    >
                      <Code className="h-3 w-3 mr-1" />
                      View JSON
                    </Button>
                  )}
                </div>
              </div>

              {/* Output */}
              <ScrollArea className="flex-1">
                <div className="p-4">
                  {result ? (
                    <div>
                      <div className="flex items-center gap-2 mb-3">
                        {result.status === 'success' ? (
                          <CheckCircle className="h-5 w-5 text-green-500" />
                        ) : (
                          <XCircle className="h-5 w-5 text-red-500" />
                        )}
                        <span
                          className={cn(
                            'font-medium',
                            result.status === 'success'
                              ? 'text-green-500'
                              : 'text-red-500'
                          )}
                        >
                          {result.status === 'success' ? 'Success' : 'Error'}
                        </span>
                        {result.duration_ms && (
                          <Badge variant="secondary">{result.duration_ms}ms</Badge>
                        )}
                      </div>
                      <pre className="text-sm bg-muted rounded-md p-3 overflow-x-auto whitespace-pre-wrap">
                        {result.output || result.error}
                      </pre>
                    </div>
                  ) : (
                    <div className="text-center py-12 text-muted-foreground">
                      <ChevronRight className="h-8 w-8 mx-auto mb-2 opacity-50" />
                      <p>Run a skill to see output</p>
                    </div>
                  )}
                </div>
              </ScrollArea>
            </>
          ) : (
            <div className="flex-1 flex items-center justify-center text-muted-foreground">
              <div className="text-center">
                <Zap className="h-12 w-12 mx-auto mb-3 opacity-30" />
                <p>Select a skill to run</p>
              </div>
            </div>
          )}
        </div>
      </div>
    </div>
  )
}
