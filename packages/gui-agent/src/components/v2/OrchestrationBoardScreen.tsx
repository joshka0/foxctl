import { useEffect, useMemo, useState } from 'react'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { listWorkspaces, switchWorkspace } from '@/api/client'
import { RuntimeSummaryPanel } from '@/components/v2/RuntimeSummaryPanel'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { ScrollArea } from '@/components/ui/scroll-area'
import { LayoutGrid, RefreshCw } from 'lucide-react'

function workspaceBadgeLabel(workspace: string): string {
  const trimmed = workspace.trim()
  if (!trimmed) return 'unscoped'
  if (trimmed === '/') return '/'
  const parts = trimmed.split('/').filter(Boolean)
  return parts[parts.length - 1] || trimmed
}

export function OrchestrationBoardScreen() {
  const queryClient = useQueryClient()
  const { data: workspaceData } = useQuery({
    queryKey: ['workspaces'],
    queryFn: listWorkspaces,
    staleTime: 15_000,
  })

  const workspaceID = useMemo(() => {
    const current = workspaceData?.current
    if (typeof current !== 'string') return undefined
    const trimmed = current.trim()
    return trimmed.length > 0 ? trimmed : undefined
  }, [workspaceData?.current])

  const [activeWorkspace, setActiveWorkspace] = useState('')
  const [switchingWorkspace, setSwitchingWorkspace] = useState(false)
  const [switchError, setSwitchError] = useState<string | null>(null)

  useEffect(() => {
    if (activeWorkspace.trim().length > 0) return
    setActiveWorkspace(workspaceID ?? '')
  }, [activeWorkspace, workspaceID])

  const resolvedWorkspaceID = activeWorkspace.trim() || workspaceID || undefined
  const workspaceLabel = useMemo(
    () => workspaceBadgeLabel(resolvedWorkspaceID ?? ''),
    [resolvedWorkspaceID],
  )

  const workspaceOptions = useMemo(() => {
    const seen = new Set<string>()
    const ordered: string[] = []
    const current = (activeWorkspace.trim() || workspaceID || '').trim()
    if (current) {
      seen.add(current)
      ordered.push(current)
    }
    for (const ws of workspaceData?.workspaces ?? []) {
      const path = (ws.path ?? '').trim()
      if (!path || seen.has(path)) continue
      seen.add(path)
      ordered.push(path)
    }
    return ordered
  }, [activeWorkspace, workspaceData?.workspaces, workspaceID])

  const handleWorkspaceSwitch = async (nextWorkspace: string) => {
    if (!nextWorkspace || nextWorkspace === resolvedWorkspaceID) return
    setActiveWorkspace(nextWorkspace)
    setSwitchError(null)
    setSwitchingWorkspace(true)
    try {
      await switchWorkspace(nextWorkspace)
      await queryClient.invalidateQueries({ queryKey: ['workspaces'] })
    } catch (error) {
      setSwitchError(error instanceof Error ? error.message : 'Failed to switch workspace')
      setActiveWorkspace(workspaceID ?? '')
    } finally {
      setSwitchingWorkspace(false)
    }
  }

  return (
    <div className="flex flex-col h-full">
      <div className="p-4 border-b border-border flex items-center justify-between gap-3">
        <div className="flex items-center gap-2 min-w-0">
          <LayoutGrid className="h-5 w-5" />
          <h2 className="text-lg font-semibold text-foreground">Orchestration</h2>
        </div>
        <div className="flex items-center gap-2 min-w-0">
          {workspaceOptions.length > 0 && (
            <select
              value={resolvedWorkspaceID ?? ''}
              onChange={(event) => void handleWorkspaceSwitch(event.target.value)}
              className="h-8 rounded-md border border-input bg-background px-2 text-xs font-mono max-w-[28rem]"
              disabled={switchingWorkspace}
              title={resolvedWorkspaceID || 'unscoped'}
            >
              {workspaceOptions.map((path) => (
                <option key={path} value={path}>
                  {workspaceBadgeLabel(path)} — {path}
                </option>
              ))}
            </select>
          )}
          <Button
            variant="outline"
            size="sm"
            className="h-8"
            onClick={() => void queryClient.invalidateQueries({ queryKey: ['workspaces'] })}
            disabled={switchingWorkspace}
            title="Reload workspace list"
          >
            <RefreshCw className={`h-3.5 w-3.5 ${switchingWorkspace ? 'animate-spin' : ''}`} />
          </Button>
          <Badge variant="outline" className="max-w-[50%] truncate" title={resolvedWorkspaceID || 'unscoped'}>
            workspace: {workspaceLabel}
          </Badge>
        </div>
      </div>
      {switchError && (
        <div className="px-4 py-2 text-xs text-red-300 border-b border-red-500/30 bg-red-500/5">
          {switchError}
        </div>
      )}
      <ScrollArea className="flex-1">
        <div className="p-4">
          <RuntimeSummaryPanel workspaceID={resolvedWorkspaceID} sourceSurface="orchestration" />
        </div>
      </ScrollArea>
    </div>
  )
}
