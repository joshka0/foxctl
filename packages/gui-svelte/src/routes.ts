import type { RouteDefinition } from "svelte-spa-router";
import { wrap } from "svelte-spa-router/wrap";

// Lazy load pages for code splitting
const routes: RouteDefinition = {
  "/": wrap({
    asyncComponent: () => import("./pages/JobsPage.svelte"),
  }),
  "/jobs": wrap({
    asyncComponent: () => import("./pages/JobsPage.svelte"),
  }),
  "/jobs/:id": wrap({
    asyncComponent: () => import("./pages/JobDetailPage.svelte"),
  }),
  "/tasks": wrap({
    asyncComponent: () => import("./pages/TasksPage.svelte"),
  }),
  "/tasks/:id": wrap({
    asyncComponent: () => import("./pages/TaskDetailPage.svelte"),
  }),
  "/sessions": wrap({
    asyncComponent: () => import("./pages/SessionsPage.svelte"),
  }),
  "/console": wrap({
    asyncComponent: () => import("./pages/ConsolePage.svelte"),
  }),
  "/agents": wrap({
    asyncComponent: () => import("./pages/AgentsPage.svelte"),
  }),
  "/codemaps": wrap({
    asyncComponent: () => import("./pages/CodemapsPage.svelte"),
  }),
  "/stats": wrap({
    asyncComponent: () => import("./pages/StatsPage.svelte"),
  }),
  "/insights": wrap({
    asyncComponent: () => import("./pages/InsightsPage.svelte"),
  }),
  "/mailbox": wrap({
    asyncComponent: () => import("./pages/MailboxPage.svelte"),
  }),
  "/reservations": wrap({
    asyncComponent: () => import("./pages/ReservationsPage.svelte"),
  }),
  "/blackboard": wrap({
    asyncComponent: () => import("./pages/BlackboardPage.svelte"),
  }),
  "/sqlite": wrap({
    asyncComponent: () => import("./pages/SQLitePage.svelte"),
  }),
  "/search": wrap({
    asyncComponent: () => import("./pages/SearchPage.svelte"),
  }),
  // Catch-all for 404
  "*": wrap({
    asyncComponent: () => import("./pages/PlaceholderPage.svelte"),
  }),
};

export { routes };
