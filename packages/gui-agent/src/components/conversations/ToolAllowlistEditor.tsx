import { useMemo, useState } from 'react'
import { Button } from '@/components/ui/button'
import { Badge } from '@/components/ui/badge'
import { Input } from '@/components/ui/input'
import { Textarea } from '@/components/ui/textarea'
import { Plus, Save, RotateCcw, X } from 'lucide-react'

export interface ToolCatalogEntry {
  name: string
  label: string
  description?: string
  group: string
}

const TOOL_CATALOG: ToolCatalogEntry[] = [
  {
    name: 'rlm_context_query',
    label: 'Context Query',
    description: 'Retrieve stored context / memories',
    group: 'Companion',
  },
  {
    name: 'rlm_context_put',
    label: 'Context Put',
    description: 'Store context / memories',
    group: 'Companion',
  },
  {
    name: 'rlm_context_list',
    label: 'Context List',
    description: 'List available context keys',
    group: 'Companion',
  },
  {
    name: 'rlm_personality_adjust',
    label: 'Personality Adjust',
    description: 'Adjust communication style',
    group: 'Companion',
  },

  { name: 'fs_read_file', label: 'Read File', group: 'Workspace' },
  { name: 'fs_list_dir', label: 'List Directory', group: 'Workspace' },
  { name: 'fs_write_file', label: 'Write File', group: 'Workspace' },

  { name: 'code_search', label: 'Code Search', group: 'Code' },
  { name: 'context_grep', label: 'Context Grep', group: 'Code' },
  { name: 'context_search', label: 'Context Search', group: 'Code' },
  { name: 'smart_search', label: 'Smart Search', group: 'Code' },

  { name: 'repo_index_search', label: 'Repo Index Search', group: 'Repo Index' },
  { name: 'repo_index_expand', label: 'Repo Index Expand', group: 'Repo Index' },
  { name: 'repo_index_open', label: 'Repo Index Open', group: 'Repo Index' },
  { name: 'repo_index_dag_grep', label: 'Repo Index DAG Grep', group: 'Repo Index' },

  { name: 'agent_spawn', label: 'Spawn Agent', group: 'Agents' },
  { name: 'agent_list', label: 'List Agents', group: 'Agents' },
  { name: 'agent_status', label: 'Agent Status', group: 'Agents' },
  { name: 'agent_kill', label: 'Kill Agent', group: 'Agents' },
  { name: 'agent_hierarchy', label: 'Agent Hierarchy', group: 'Agents' },
  { name: 'agent_wait', label: 'Wait For Agent', group: 'Agents' },

  { name: 'mail_inbox', label: 'Mailbox Inbox', group: 'Mailbox' },
  { name: 'mail_send', label: 'Mailbox Send', group: 'Mailbox' },
  { name: 'mail_ack', label: 'Mailbox Ack', group: 'Mailbox' },

  { name: 'bb_inbox', label: 'Blackboard Inbox', group: 'Blackboard' },
  { name: 'bb_post', label: 'Blackboard Post', group: 'Blackboard' },
  { name: 'bb_mark_read', label: 'Blackboard Mark Read', group: 'Blackboard' },
]

function normalizeTools(inTools: string[]): string[] {
  const seen = new Set<string>()
  const out: string[] = []
  for (const raw of inTools) {
    const name = raw.trim()
    if (!name) continue
    if (seen.has(name)) continue
    seen.add(name)
    out.push(name)
  }
  out.sort()
  return out
}

function parseTools(text: string): string[] {
  return normalizeTools(
    text
      .split(/\r?\n|,/)
      .map((s) => s.trim())
      .filter(Boolean)
  )
}

export interface ToolAllowlistEditorProps {
  value: string[]
  onChange: (next: string[]) => void
  onSave: () => void
  onClear: () => void
  error?: string | null
}

export function ToolAllowlistEditor({
  value,
  onChange,
  onSave,
  onClear,
  error,
}: ToolAllowlistEditorProps) {
  const [toolToAdd, setToolToAdd] = useState('')
  const [showAdvanced, setShowAdvanced] = useState(false)
  const [search, setSearch] = useState('')

  const normalizedValue = useMemo(() => normalizeTools(value), [value])

  const groupedCatalog = useMemo(() => {
    const q = search.trim().toLowerCase()
    const filtered = q
      ? TOOL_CATALOG.filter((t) => t.name.toLowerCase().includes(q) || t.label.toLowerCase().includes(q))
      : TOOL_CATALOG

    const groups = new Map<string, ToolCatalogEntry[]>()
    for (const t of filtered) {
      if (!groups.has(t.group)) groups.set(t.group, [])
      groups.get(t.group)!.push(t)
    }

    return Array.from(groups.entries())
      .sort((a, b) => a[0].localeCompare(b[0]))
      .map(([group, tools]) => ({
        group,
        tools: tools.slice().sort((a, b) => a.label.localeCompare(b.label)),
      }))
  }, [search])

  const addTool = (name: string) => {
    const next = normalizeTools([...normalizedValue, name])
    onChange(next)
    setToolToAdd('')
  }

  const removeTool = (name: string) => {
    onChange(normalizeTools(normalizedValue.filter((t) => t !== name)))
  }

  const allowAll = normalizedValue.length === 0
  const missingContextQuery = !allowAll && !normalizedValue.includes('rlm_context_query')

  return (
    <div className="space-y-2">
      <div className="flex items-center justify-between">
        <span className="text-xs font-medium">Tools Access</span>
        <div className="flex items-center gap-1">
          <Button
            type="button"
            variant="ghost"
            size="sm"
            className="h-6 px-2 text-[10px]"
            onClick={onSave}
            title="Save tool access settings"
          >
            <Save className="h-3 w-3 mr-1" />
            Save
          </Button>
          <Button
            type="button"
            variant="ghost"
            size="sm"
            className="h-6 px-2 text-[10px]"
            onClick={() => {
              onChange([])
              onClear()
            }}
            title="Allow all tools"
          >
            <RotateCcw className="h-3 w-3 mr-1" />
            Allow All
          </Button>
        </div>
      </div>

      {allowAll ? (
        <div className="text-[10px] text-muted-foreground">
          Tool access is unrestricted (all available tools are allowed).
        </div>
      ) : (
        <div className="flex flex-wrap gap-1">
          {normalizedValue.map((t) => (
            <div
              key={t}
              className="inline-flex items-center gap-1 rounded-md border bg-muted/40 px-2 py-0.5"
              title={t}
            >
              <span className="text-[10px] font-mono">{t}</span>
              <button
                type="button"
                className="text-muted-foreground hover:text-foreground"
                onClick={() => removeTool(t)}
                aria-label={`Remove ${t}`}
              >
                <X className="h-3 w-3" />
              </button>
            </div>
          ))}
        </div>
      )}

      {missingContextQuery && (
        <Badge variant="warning" className="text-[10px]">
          Note: rlm_context_query is not allowed, so the assistant cannot query saved context.
        </Badge>
      )}

      <div className="flex items-center gap-2">
        <Input
          value={toolToAdd}
          onChange={(e) => setToolToAdd(e.target.value)}
          onKeyDown={(e) => {
            if (e.key === 'Enter') {
              e.preventDefault()
              const name = toolToAdd.trim()
              if (!name) return
              addTool(name)
            }
          }}
          placeholder="Add tool by name (press Enter)"
          className="h-8 text-xs font-mono"
        />
        <Button
          type="button"
          variant="outline"
          size="sm"
          className="h-8 text-xs"
          onClick={() => {
            const name = toolToAdd.trim()
            if (!name) return
            addTool(name)
          }}
        >
          <Plus className="h-3 w-3 mr-1" />
          Add
        </Button>
      </div>

      <details className="rounded-md border border-border bg-muted/20 p-2">
        <summary className="cursor-pointer text-[10px] font-medium text-muted-foreground">
          Browse common tools
        </summary>
        <div className="mt-2 space-y-2">
          <Input
            value={search}
            onChange={(e) => setSearch(e.target.value)}
            placeholder="Filter tools..."
            className="h-7 text-[10px]"
          />
          <div className="max-h-40 overflow-y-auto space-y-2 pr-1">
            {groupedCatalog.map(({ group, tools }) => (
              <div key={group} className="space-y-1">
                <div className="text-[10px] font-medium text-muted-foreground uppercase tracking-wider">
                  {group}
                </div>
                <div className="flex flex-wrap gap-1">
                  {tools.map((t) => {
                    const selected = normalizedValue.includes(t.name)
                    return (
                      <Button
                        key={t.name}
                        type="button"
                        variant={selected ? 'secondary' : 'outline'}
                        size="sm"
                        className="h-7 px-2 text-[10px]"
                        onClick={() => (selected ? removeTool(t.name) : addTool(t.name))}
                        title={t.description ? `${t.name}: ${t.description}` : t.name}
                      >
                        {t.label}
                      </Button>
                    )
                  })}
                </div>
              </div>
            ))}
          </div>
        </div>
      </details>

      <div className="flex items-center justify-between">
        <button
          type="button"
          className="text-[10px] text-muted-foreground hover:text-foreground underline underline-offset-2"
          onClick={() => setShowAdvanced((v) => !v)}
        >
          {showAdvanced ? 'Hide advanced' : 'Advanced (paste list)'}
        </button>
        {!allowAll && (
          <button
            type="button"
            className="text-[10px] text-muted-foreground hover:text-foreground underline underline-offset-2"
            onClick={() => onChange(['rlm_context_query', 'rlm_context_put', 'rlm_context_list', 'rlm_personality_adjust'])}
            title="Replace selection with recommended companion tools"
          >
            Use recommended
          </button>
        )}
      </div>

      {showAdvanced && (
        <Textarea
          value={normalizedValue.join('\n')}
          onChange={(e) => onChange(parseTools(e.target.value))}
          placeholder="One tool per line"
          className="min-h-[84px] text-[10px] font-mono"
        />
      )}

      {error && <div className="text-[10px] text-destructive">{error}</div>}
    </div>
  )
}

