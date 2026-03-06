import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { AppShell } from '@/components/layout/AppShell'
import { LogsViewer } from '@/components/actions'
import { AgentList } from '@/components/agents'
import { ConversationsList } from '@/components/conversations'
import { RoomsView } from '@/components/rooms/RoomsView'
import { ArtifactsExplorer, ContextExplorer, TurnsExplorer } from '@/components/v2/V2Explorers'
import { OrchestrationBoardScreen } from '@/components/v2/OrchestrationBoardScreen'
import { useViewStore, type ViewType } from '@/stores/viewStore'

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

  return (
    <QueryClientProvider client={queryClient}>
      <AppShell activeView={activeView} onViewChange={setActiveView}>
        <MainContent view={activeView} />
      </AppShell>
    </QueryClientProvider>
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
