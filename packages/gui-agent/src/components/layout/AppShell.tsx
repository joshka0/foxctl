import { type ReactNode, useState } from 'react'
import { cn } from '@/lib/utils'
import { AgentSidebar } from './AgentSidebar'
import { AgentHUD } from './AgentHUD'
import { SpawnAgentPanel } from './SpawnAgentPanel'
import { useActivityStream } from '@/hooks/useActivityStream'
import { useViewStore } from '@/stores/viewStore'
import { PanelRightClose, PanelRightOpen } from 'lucide-react'
import { Button } from '@/components/ui/button'
import type { ViewType } from '@/App'

interface AppShellProps {
  children: ReactNode
  activeView: ViewType
  onViewChange: (view: ViewType) => void
}

export function AppShell({ children, activeView, onViewChange }: AppShellProps) {
  const [rightPanelOpen, setRightPanelOpen] = useState(true)
  const { spawnAgentOpen, setSpawnAgentOpen } = useViewStore()

  // Initialize SSE connection
  useActivityStream()

  return (
    <div className="flex h-screen w-screen bg-background dark">
      {/* Left Sidebar - Agent Panel */}
      <AgentSidebar activeView={activeView} onViewChange={onViewChange} />

      {/* Main Content */}
      <div className="flex-1 flex flex-col overflow-hidden">
        {/* Top Bar */}
        <header className="h-12 border-b border-border flex items-center justify-between px-4">
          <h1 className="text-sm font-medium text-foreground">Agent Operations Center</h1>
          <div className="flex items-center gap-2">
            <Button
              variant="ghost"
              size="icon"
              onClick={() => setRightPanelOpen(!rightPanelOpen)}
              title={rightPanelOpen ? 'Hide communication panel' : 'Show communication panel'}
            >
              {rightPanelOpen ? (
                <PanelRightClose className="h-4 w-4" />
              ) : (
                <PanelRightOpen className="h-4 w-4" />
              )}
            </Button>
          </div>
        </header>

        {/* Content Area */}
        <div className="flex-1 flex overflow-hidden">
          {/* Main Panel */}
          <main className={cn(
            "flex-1 overflow-hidden",
            rightPanelOpen && "mr-0"
          )}>
            {children}
          </main>

          {/* Global Spawn Agent Panel */}
          {spawnAgentOpen && (
            <SpawnAgentPanel
              onClose={() => setSpawnAgentOpen(false)}
              onViewChange={onViewChange}
            />
          )}

          {/* Right Panel - Communication */}
          {rightPanelOpen && (
            <aside className="w-80 border-l border-border bg-card">
              <AgentHUD />
            </aside>
          )}
        </div>
      </div>
    </div>
  )
}
