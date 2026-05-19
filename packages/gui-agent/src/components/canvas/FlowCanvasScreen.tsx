import { useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { ScrollArea } from '@/components/ui/scroll-area'
import { Badge } from '@/components/ui/badge'
import { cn } from '@/lib/utils'
import { listFlows, createFlow, deleteFlow } from '@/api/client'
import { FlowCanvas } from './FlowCanvas'
import { GitBranch, Plus, Trash2, Loader2 } from 'lucide-react'
import type { Flow } from '@/api/types'

export function FlowCanvasScreen() {
  const [selectedFlowId, setSelectedFlowId] = useState<string | null>(null)
  const [newFlowName, setNewFlowName] = useState('')
  const [isCreating, setIsCreating] = useState(false)

  const { data: flowsData, isLoading, refetch } = useQuery({
    queryKey: ['flows', '.'],
    queryFn: () => listFlows('.'),
  })

  const flows = flowsData?.flows ?? []

  const handleCreate = async () => {
    const name = newFlowName.trim()
    if (!name) return
    setIsCreating(true)
    try {
      const result = await createFlow({ name, workspace: '.' })
      setNewFlowName('')
      await refetch()
      if (result.flow) {
        setSelectedFlowId(result.flow.id)
      }
    } finally {
      setIsCreating(false)
    }
  }

  const handleDelete = async (flowId: string) => {
    if (!window.confirm('Delete this flow and all its nodes/edges?')) return
    await deleteFlow(flowId)
    if (selectedFlowId === flowId) setSelectedFlowId(null)
    await refetch()
  }

  if (selectedFlowId) {
    const selectedFlow = flows.find((f) => f.id === selectedFlowId)
    return (
      <FlowCanvas
        flowId={selectedFlowId}
        flowName={selectedFlow?.name ?? selectedFlowId}
        onBack={() => setSelectedFlowId(null)}
      />
    )
  }

  return (
    <div className="flex h-full">
      {/* Flow List Sidebar */}
      <aside className="w-72 border-r bg-muted/5 flex flex-col shrink-0">
        <div className="px-4 py-3 border-b">
          <div className="flex items-center gap-2 mb-3">
            <GitBranch className="w-4 h-4 text-primary" />
            <h2 className="text-sm font-black uppercase tracking-tight">Flows</h2>
          </div>
          <div className="flex gap-2">
            <Input
              value={newFlowName}
              onChange={(e) => setNewFlowName(e.target.value)}
              placeholder="New flow name…"
              className="h-8 text-xs"
              onKeyDown={(e) => e.key === 'Enter' && handleCreate()}
            />
            <Button
              size="sm"
              className="h-8 px-2"
              disabled={!newFlowName.trim() || isCreating}
              onClick={handleCreate}
            >
              {isCreating ? <Loader2 className="w-3.5 h-3.5 animate-spin" /> : <Plus className="w-3.5 h-3.5" />}
            </Button>
          </div>
        </div>
        <ScrollArea className="flex-1">
          {isLoading ? (
            <div className="p-4 text-xs text-muted-foreground animate-pulse">Loading flows…</div>
          ) : flows.length === 0 ? (
            <div className="p-4 text-xs text-muted-foreground">
              No flows yet. Create one to start wiring nodes.
            </div>
          ) : (
            <div className="divide-y divide-border/50">
              {flows.map((flow) => (
                <FlowListItem
                  key={flow.id}
                  flow={flow}
                  onSelect={() => setSelectedFlowId(flow.id)}
                  onDelete={() => handleDelete(flow.id)}
                />
              ))}
            </div>
          )}
        </ScrollArea>
      </aside>

      {/* Empty State */}
      <div className="flex-1 flex items-center justify-center">
        <div className="text-center space-y-3 opacity-50">
          <GitBranch className="w-12 h-12 mx-auto text-muted-foreground" />
          <p className="text-sm font-medium text-muted-foreground">
            Select a flow from the sidebar to open the canvas.
          </p>
          <p className="text-xs text-muted-foreground max-w-xs mx-auto">
            The canvas lets you wire terminals, agents, and skills into directed graphs
            that execute via the flow engine.
          </p>
        </div>
      </div>
    </div>
  )
}

function FlowListItem({
  flow,
  onSelect,
  onDelete,
}: {
  flow: Flow
  onSelect: () => void
  onDelete: () => void
}) {
  return (
    <div className="group flex items-center gap-2 px-4 py-2.5 hover:bg-muted/40 cursor-pointer transition-colors">
      <button
        onClick={onSelect}
        className="flex-1 min-w-0 text-left"
      >
        <div className="flex items-center gap-2">
          <span className="text-xs font-semibold truncate">{flow.name}</span>
          <FlowStateBadge state={flow.state} />
        </div>
        <div className="text-[10px] text-muted-foreground font-mono mt-0.5">
          {flow.id.slice(0, 8)} · {flow.workspace}
        </div>
      </button>
      <Button
        variant="ghost"
        size="xs"
        className="opacity-0 group-hover:opacity-100 h-6 w-6 p-0 text-red-500 hover:text-red-600 hover:bg-red-500/10"
        onClick={(e) => {
          e.stopPropagation()
          onDelete()
        }}
      >
        <Trash2 className="w-3 h-3" />
      </Button>
    </div>
  )
}

function FlowStateBadge({ state }: { state: string }) {
  const colors: Record<string, string> = {
    draft: 'bg-slate-500/10 text-slate-500 border-slate-500/20',
    running: 'bg-green-500/10 text-green-500 border-green-500/20',
    paused: 'bg-amber-500/10 text-amber-500 border-amber-500/20',
    stopped: 'bg-muted text-muted-foreground',
    errored: 'bg-red-500/10 text-red-500 border-red-500/20',
  }
  return (
    <Badge variant="outline" className={cn('text-[9px] h-4 px-1.5 font-bold', colors[state] ?? colors.draft)}>
      {state}
    </Badge>
  )
}
