# Svelte SPA (gui-v2)

## Purpose

This document tracks the Svelte SPA migration for gui-v2. It summarizes
structure, how to run it, and the current state against the implementation plan.

## Goals

- Replace the React GUI (`packages/gui/`) with a Svelte SPA (`packages/gui-svelte/`).
- Preserve the dashboard information architecture (jobs/tasks/sessions/etc).
- Use the Go backend (`cmd/agentctl_web`) for REST + SSE + console streaming.
- Keep the UI lightweight and fast, with server-backed data only.

## Status (current)

- Foundation + core UI components: complete
- Pages ported:
  - Jobs, Job detail, Tasks, Task detail, Sessions, Agents, Stats
  - Mailbox, Reservations, Blackboard
  - Search, SQLite, Codemaps, Insights
  - Console (wired to `/api/console/sessions/*`)
- Console streaming: implemented via SSE using payload format
- Remaining work:
  - Console polish (tool result rendering, metrics, retry UX)
  - Workspace selection in header (global vs per-page)

## Layout and Structure

```
packages/gui-svelte/
  index.html
  vite.config.ts
  src/
    main.ts
    app.css
    Root.svelte
    App.svelte
    routes.ts
    lib/
      api/
        hooks.ts
        queryClient.ts
        sse.ts
        console.ts
      components/
        layout/
          Layout.svelte
          Sidebar.svelte
          Header.svelte
        ui/...
      utils/
        time.ts
        format.ts
    pages/
      JobsPage.svelte
      JobDetailPage.svelte
      TasksPage.svelte
      TaskDetailPage.svelte
      SessionsPage.svelte
      ConsolePage.svelte
      AgentsPage.svelte
      CodemapsPage.svelte
      StatsPage.svelte
      InsightsPage.svelte
      MailboxPage.svelte
      ReservationsPage.svelte
      BlackboardPage.svelte
      SQLitePage.svelte
      SearchPage.svelte
      PlaceholderPage.svelte
```

## Local Development

From repo root:

```bash
bun run --cwd packages/gui-svelte dev
```

Typechecking:

```bash
bun run --cwd packages/gui-svelte check
```

Fast Svelte-only checks (skips TS project files):

```bash
bun run --cwd packages/gui-svelte check:fast
```

Server proxy targets:

- `http://localhost:8090` (Go web server)
- `/api/*` and `/ws/*` are proxied by Vite

## Data Flow

- REST data uses `@agentctl/data` client methods.
- SSE invalidation uses `/api/events` and invalidates query keys.
- Console page uses `/api/console/sessions` and
  `/api/console/sessions/:id/events?format=payload`.

## Known Constraints

- `/api/mailbox` and `/api/reservations` require `workspace_id`.
- `/api/search` returns only normalized source types for UI consistency.
- `/api/workspaces` returns `current` and `workspaces` with `is_active`.
- `bun run --cwd packages/gui-svelte check` can take ~20s on a cold run; use `check:fast` for quicker Svelte/CSS diagnostics.

## Related Docs

- `docs/plans/gui-v2/svelte-spa-impl-plan.md`
- `docs/plans/gui-v2/IMPLEMENTATION_PLAN.md`
