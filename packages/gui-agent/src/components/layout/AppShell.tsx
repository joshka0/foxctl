import { type ReactNode } from 'react'
import { AgentSidebar } from './AgentSidebar'
import { SpawnAgentPanel } from './SpawnAgentPanel'
import { useActivityStream } from '@/hooks/useActivityStream'
import { useViewStore } from '@/stores/viewStore'
import type { ViewType } from '@/stores/viewStore'

interface AppShellProps {
  children: ReactNode
  activeView: ViewType
  onViewChange: (view: ViewType) => void
}

export function AppShell({ children, activeView, onViewChange }: AppShellProps) {
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
        </header>

        {/* Content Area */}
        <div className="flex-1 flex overflow-hidden">
          {/* Main Panel */}
          <main className="flex-1 overflow-hidden">
            {children}
          </main>

          {/* Global Spawn Agent Panel */}
          {spawnAgentOpen && (
            <SpawnAgentPanel
              onClose={() => setSpawnAgentOpen(false)}
              onViewChange={onViewChange}
            />
          )}
        </div>
      </div>
    </div>
  )
}
