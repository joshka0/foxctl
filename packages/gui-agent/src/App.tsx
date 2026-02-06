import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { AppShell } from '@/components/layout/AppShell'
import { ActivityFeed } from '@/components/activity/ActivityFeed'

import { LogsViewer, SkillRunner } from '@/components/actions'
import { AgentList } from '@/components/agents'
import { ConversationsList } from '@/components/conversations'
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
    case 'activity':
      return <ActivityFeed />
    case 'agents':
      return <AgentList />
    case 'conversations':
      return <ConversationsList />
    case 'search':
      return <PlaceholderView title="Search" description="Search code, memory, and sessions" />
    case 'logs':
      return <LogsViewer />
    case 'skills':
      return <SkillRunner />
    case 'mailbox':
      return <PlaceholderView title="Mailbox" description="Inter-agent messages" />
    case 'blackboard':
      return <PlaceholderView title="Blackboard" description="Shared context records" />
    case 'settings':
      return <PlaceholderView title="Settings" description="Configure the agent system" />
    default:
      return <AgentList />
  }
}

/**
 * Renders a centered placeholder view with a title, description, and a "Coming soon..." note.
 *
 * @param title - Heading text displayed prominently at the top of the placeholder.
 * @param description - Supporting descriptive text shown beneath the title.
 * @returns The placeholder view element.
 */
function PlaceholderView({ title, description }: { title: string; description: string }) {
  return (
    <div className="flex items-center justify-center h-full">
      <div className="text-center">
        <h2 className="text-xl font-semibold text-foreground mb-2">{title}</h2>
        <p className="text-muted-foreground">{description}</p>
        <p className="text-sm text-muted-foreground mt-4">Coming soon...</p>
      </div>
    </div>
  )
}
