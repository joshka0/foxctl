import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { Suspense, lazy, startTransition, type ReactNode } from 'react'
import { AppShell } from '@/components/layout/AppShell'
import { AgentList } from '@/components/agents'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { useAuthSession, useAuthSignOut } from '@/hooks/useAuthSession'
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
const FlowCanvasScreen = lazy(() =>
  import('@/components/canvas').then((module) => ({
    default: module.FlowCanvasScreen,
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
  return (
    <QueryClientProvider client={queryClient}>
      <AuthenticatedApp />
    </QueryClientProvider>
  )
}

function AuthenticatedApp() {
  const { activeView, setActiveView } = useViewStore()
  const authSession = useAuthSession()
  const signOut = useAuthSignOut()

  const handleViewChange = (nextView: ViewType) => {
    startTransition(() => {
      setActiveView(nextView)
    })
  }

  if (authSession.isLoading) {
    return (
      <SessionStateScreen
        eyebrow="Session"
        title="Checking access"
        body="Validating your gui-agent session before the control plane starts."
      />
    )
  }

  if (authSession.error) {
    return (
      <SessionStateScreen
        eyebrow="Session Error"
        title="Could not verify your sign-in"
        body={
          authSession.error instanceof Error
            ? authSession.error.message
            : "The gateway did not return a valid auth session."
        }
        actions={
          <>
            <Button variant="outline" onClick={() => authSession.refetch()}>
              Retry
            </Button>
            <Button onClick={() => window.location.assign('/login')}>
              Return to login
            </Button>
          </>
        }
      />
    )
  }

  if (!authSession.data) {
    return (
      <SessionStateScreen
        eyebrow="Signed Out"
        title="Your gui-agent session is not active"
        body="This public control plane requires an active Better Auth session. Use the magic-link flow to sign in again."
        actions={
          <Button onClick={() => window.location.assign('/login')}>
            Sign in
          </Button>
        }
      />
    )
  }

  return (
    <AppShell
      activeView={activeView}
      onViewChange={handleViewChange}
      authSession={authSession.data}
      onSignOut={() => signOut.mutate()}
      signingOut={signOut.isPending}
    >
      <Suspense fallback={<ViewLoadingFallback view={activeView} />}>
        <MainContent view={activeView} />
      </Suspense>
    </AppShell>
  )
}

function SessionStateScreen({
  eyebrow,
  title,
  body,
  actions,
}: {
  eyebrow: string
  title: string
  body: string
  actions?: ReactNode
}) {
  return (
    <div className="min-h-screen w-screen bg-background text-foreground">
      <div className="mx-auto flex min-h-screen w-full max-w-5xl items-center px-6 py-12">
        <div className="grid gap-6 rounded-3xl border border-border bg-card/90 p-8 shadow-2xl shadow-black/20 md:grid-cols-[1.4fr_0.8fr] md:p-10">
          <div className="space-y-4">
            <Badge variant="outline" className="text-[10px] uppercase tracking-[0.18em]">
              {eyebrow}
            </Badge>
            <div className="space-y-2">
              <h1 className="text-3xl font-semibold tracking-tight md:text-4xl">
                {title}
              </h1>
              <p className="max-w-2xl text-sm leading-6 text-muted-foreground md:text-base">
                {body}
              </p>
            </div>
            {actions && <div className="flex flex-wrap items-center gap-3">{actions}</div>}
          </div>
          <div className="rounded-2xl border border-border bg-background/70 p-5">
            <p className="text-xs uppercase tracking-[0.18em] text-muted-foreground">
              Public GUI
            </p>
            <div className="mt-4 space-y-3 text-sm text-muted-foreground">
              <p>Authentication is handled by the public Better Auth gateway.</p>
              <p>
                After sign-in, the gateway proxies authenticated <code>/api</code> and{' '}
                <code>/ws</code> traffic to the private <code>foxctl</code> service.
              </p>
              <p>
                If this screen appears unexpectedly, your session likely expired or the
                gateway could not verify it.
              </p>
            </div>
          </div>
        </div>
      </div>
    </div>
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
    case 'canvas':
      return <FlowCanvasScreen />
    default:
      return <AgentList />
  }
}
