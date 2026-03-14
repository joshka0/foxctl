import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { Suspense, lazy, startTransition } from 'react'
import { AppShell } from '@/components/layout/AppShell'
import { AgentList } from '@/components/agents'
import { useViewStore, type ViewType } from '@/stores/viewStore'

const RoomsView = lazy(() =>
  import('@/components/rooms/RoomsView').then((module) => ({
    default: module.RoomsView,
  })),
)
const ConversationsList = lazy(() =>
  import('@/components/conversations').then((module) => ({
    default: module.ConversationsList,
  })),
)
const LogsViewer = lazy(() =>
  import('@/components/actions').then((module) => ({
    default: module.LogsViewer,
  })),
)
const OrchestrationBoardScreen = lazy(() =>
  import('@/components/v2/OrchestrationBoardScreen').then((module) => ({
    default: module.OrchestrationBoardScreen,
  })),
)
const TurnsExplorer = lazy(() =>
  import('@/components/v2/V2Explorers').then((module) => ({
    default: module.TurnsExplorer,
  })),
)
const ContextExplorer = lazy(() =>
  import('@/components/v2/V2Explorers').then((module) => ({
    default: module.ContextExplorer,
  })),
)
const ArtifactsExplorer = lazy(() =>
  import('@/components/v2/V2Explorers').then((module) => ({
    default: module.ArtifactsExplorer,
  })),
)

const queryClient = new QueryClient({
  defaultOptions: {
    queries: {
      staleTime: 5000,
      refetchOnWindowFocus: false,
    },
  },
})

/**
 * The root React component for the GUI application.
 *
 * Provides React Query context and renders the application shell tied to the centralized view state.
 *
 * @returns A JSX element that supplies the configured QueryClientProvider and renders AppShell with the current active view
 */
export function App() {
  const { activeView, setActiveView } = useViewStore()
  const handleViewChange = (nextView: ViewType) => {
    startTransition(() => {
      setActiveView(nextView)
    })
  }

  return (
    <QueryClientProvider client={queryClient}>
      <AppShell activeView={activeView} onViewChange={handleViewChange}>
        <Suspense fallback={<ViewLoadingFallback view={activeView} />}>
          <MainContent view={activeView} />
        </Suspense>
      </AppShell>
    </QueryClientProvider>
  )
}

function ViewLoadingFallback({ view }: { view: ViewType }) {
  return (
    <div className="flex h-full min-h-[320px] items-center justify-center px-6 text-center">
      <div className="space-y-1">
        <div className="text-sm font-medium text-foreground">Loading {view}</div>
        <div className="text-xs text-muted-foreground">
          Preparing the next control-plane surface.
        </div>
      </div>
    </div>
  )
}

/**
 * Render the main content area for the specified view.
 *
 * @param view - The active view to display; determines which screen component is rendered.
 * @returns The React element corresponding to the active view.
 */
function MainContent({ view }: { view: ViewType }) {
  switch (view) {
    case 'runtime':
      return <AgentList />
    case 'rooms':
      return <RoomsView />
    case 'orchestration':
      return <OrchestrationBoardScreen />
    case 'turns':
      return <TurnsExplorer />
    case 'context':
      return <ContextExplorer />
    case 'artifacts':
      return <ArtifactsExplorer />
    case 'events':
      return <LogsViewer />
    case 'companion':
      return <ConversationsList />
    default:
      return <AgentList />
  }
}
