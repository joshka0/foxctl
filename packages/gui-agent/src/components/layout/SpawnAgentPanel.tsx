import { useEffect } from 'react'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { Button } from '@/components/ui/button'
import { ScrollArea } from '@/components/ui/scroll-area'
import { spawnAgent, type SpawnAgentParams } from '@/api/client'
import type { AgentSpawnResponse } from '@/api/types'
import { X } from 'lucide-react'
import { SpawnAgentFormCore } from '@/components/agents/SpawnAgentFormCore'
import type { ViewType } from '@/stores/viewStore'

interface SpawnAgentPanelProps {
  onClose: () => void
  onViewChange: (view: ViewType) => void
}

export function SpawnAgentPanel({ onClose, onViewChange }: SpawnAgentPanelProps) {
  const queryClient = useQueryClient()

  useEffect(() => {
    const handleKeyDown = (e: KeyboardEvent) => {
      if (e.key === 'Escape') onClose()
    }
    document.addEventListener('keydown', handleKeyDown)
    return () => document.removeEventListener('keydown', handleKeyDown)
  }, [onClose])

  const mutation = useMutation({
    mutationFn: (params: SpawnAgentParams) => spawnAgent(params),
    onSuccess: (_data: AgentSpawnResponse) => {
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

  return (
    <div className="fixed inset-0 z-50 flex" role="dialog" aria-modal="true">
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
          <Button variant="ghost" size="icon" className="h-7 w-7" onClick={onClose} aria-label="Close">
            <X className="h-4 w-4" />
          </Button>
        </div>

        {/* Form Content */}
        <ScrollArea className="flex-1">
          <div className="p-4">
            <SpawnAgentFormCore
              onSubmit={(params) => mutation.mutate(params)}
              onCancel={onClose}
              isPending={mutation.isPending}
              error={mutation.error instanceof Error ? mutation.error : null}
            />
          </div>
        </ScrollArea>
      </div>
    </div>
  )
}
