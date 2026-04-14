# Phase 12: Svelte SPA Migration - Implementation Plan

> Generated from comprehensive analysis of existing React GUI and Phase 12 requirements.

## Executive Summary

Migrate the foxctl React GUI (`packages/gui/`) to a Svelte SPA (`packages/gui-svelte/`). The migration follows a phased approach, starting with scaffolding and simple pages, progressing to complex interactive pages like Console.

**Estimated Effort**: 4-5 weeks (following port order from simplest to most complex)

---

## 1. Package Setup

### 1.1 Directory Structure

```
packages/gui-svelte/
├── index.html
├── package.json              # @foxctl/gui-svelte
├── tsconfig.json
├── vite.config.ts
├── postcss.config.cjs
├── tailwind.config.cjs
├── src/
│   ├── main.ts               # Entry point with theme init
│   ├── app.css               # CSS variables (copy from React)
│   ├── Root.svelte           # QueryClientProvider wrapper
│   ├── App.svelte            # Router + Layout
│   ├── routes.ts             # Hash-based route definitions
│   │
│   ├── lib/
│   │   ├── api/
│   │   │   ├── queryClient.ts      # TanStack Query client
│   │   │   ├── sse.ts              # SSE invalidation
│   │   │   ├── client.ts           # Re-export from @foxctl/data
│   │   │   ├── consoleApi.ts       # Console-specific endpoints
│   │   │   └── consoleTypes.ts     # Console type definitions
│   │   │
│   │   ├── components/
│   │   │   ├── layout/
│   │   │   │   ├── Layout.svelte   # Main layout with SSE lifecycle
│   │   │   │   ├── Sidebar.svelte  # Navigation with lucide-svelte
│   │   │   │   └── Header.svelte   # Theme toggle + workspace
│   │   │   ├── ui/
│   │   │   │   ├── Badge.svelte
│   │   │   │   ├── Button.svelte
│   │   │   │   ├── Card.svelte
│   │   │   │   ├── Table.svelte
│   │   │   │   ├── Dialog.svelte
│   │   │   │   ├── Accordion.svelte
│   │   │   │   ├── Input.svelte
│   │   │   │   ├── Select.svelte
│   │   │   │   └── Tabs.svelte
│   │   │   └── WorkspaceFilter.svelte
│   │   │
│   │   ├── stores/
│   │   │   └── workspace.ts
│   │   │
│   │   └── utils/
│   │       ├── time.ts             # formatRelativeTime, formatDate
│   │       └── format.ts           # cn, formatBytes, truncate
│   │
│   └── pages/
│       ├── JobsPage.svelte
│       ├── JobDetailPage.svelte
│       ├── TasksPage.svelte
│       ├── TaskDetailPage.svelte
│       ├── StatsPage.svelte
│       ├── InsightsPage.svelte
│       ├── MailboxPage.svelte
│       ├── SessionsPage.svelte
│       ├── AgentsPage.svelte
│       ├── SQLitePage.svelte
│       ├── SearchPage.svelte
│       ├── CodemapsPage.svelte
│       ├── ConsolePage.svelte
│       └── PlaceholderPage.svelte
```

### 1.2 Dependencies

```json
{
  "name": "@foxctl/gui-svelte",
  "private": true,
  "version": "0.1.0",
  "type": "module",
  "scripts": {
    "dev": "vite --port 5174",
    "build": "vite build",
    "preview": "vite preview",
    "check": "svelte-check --tsconfig ./tsconfig.json"
  },
  "dependencies": {
    "@foxctl/data": "workspace:*",
    "@tanstack/svelte-query": "^5.64.0",
    "lucide-svelte": "^0.500.0",
    "svelte": "^4.2.19",
    "svelte-spa-router": "^4.0.1"
  },
  "devDependencies": {
    "@sveltejs/vite-plugin-svelte": "^3.1.2",
    "autoprefixer": "^10.4.20",
    "postcss": "^8.4.49",
    "svelte-check": "^4.1.1",
    "tailwindcss": "^3.4.17",
    "typescript": "^5.7.2",
    "vite": "^6.0.6"
  }
}
```

### 1.3 Vite Configuration

```typescript
// vite.config.ts
import { defineConfig } from 'vite';
import { svelte } from '@sveltejs/vite-plugin-svelte';
import path from 'path';

export default defineConfig({
  plugins: [svelte()],
  resolve: {
    alias: {
      '@': path.resolve(__dirname, './src'),
    },
  },
  server: {
    port: 5174,
    proxy: {
      '/api': {
        target: 'http://localhost:8090',
        changeOrigin: true,
      },
      '/ws': {
        target: 'ws://localhost:8090',
        ws: true,
      },
    },
  },
});
```

---

## 2. React to Svelte Pattern Mapping

| React Pattern | Svelte Equivalent |
|--------------|-------------------|
| `useState(value)` | `let value = initialValue` |
| `useEffect(() => {}, [deps])` | `$: { ... }` reactive block |
| `useQuery({queryKey, queryFn})` | `createQuery({queryKey: [...], queryFn})` |
| `useMemo(() => val, [deps])` | `$: val = expression` |
| `useRef()` | `let element: HTMLElement` (bind:this) |
| `{condition && <Component>}` | `{#if condition}<Component />{/if}` |
| `items.map(item => <Item />)` | `{#each items as item}<Item />{/each}` |
| `className={cn(...)}` | `class={cn(...)}` |
| `onClick={handler}` | `on:click={handler}` |
| `children` prop | `<slot />` |

---

## 3. Phase-by-Phase Implementation

### Phase A: Foundation + Simple Pages (Week 1)

**Goal**: Scaffold, build infrastructure, implement 3 simple table pages

#### A.1 Scaffolding
- [ ] Create `packages/gui-svelte/` directory
- [ ] Add `package.json` with dependencies
- [ ] Add `vite.config.ts` with proxy to `:8090`
- [ ] Add `tsconfig.json` with path aliases
- [ ] Add `tailwind.config.cjs` (copy CSS variables)
- [ ] Add `postcss.config.cjs`
- [ ] Add `index.html`
- [ ] Create `src/main.ts` with theme initialization
- [ ] Create `src/app.css` (copy from React)
- [ ] Verify `bun install && bun run dev` works

#### A.2 Core Infrastructure
- [ ] Create `src/lib/api/queryClient.ts`
- [ ] Create `src/lib/api/client.ts` (re-export from `@foxctl/data`)
- [ ] Create `src/lib/api/sse.ts` (invalidate on events)
- [ ] Create `src/lib/utils/time.ts` (formatRelativeTime)
- [ ] Create `src/lib/utils/format.ts` (cn, formatBytes)

#### A.3 UI Components (Core)
- [ ] Create `Button.svelte` (variants: default, outline, ghost, destructive)
- [ ] Create `Badge.svelte` (all variants from React)
- [ ] Create `Card.svelte` (with slots)
- [ ] Create `Table.svelte` (all sub-components)
- [ ] Create `Input.svelte`
- [ ] Create `Select.svelte`

#### A.4 Layout Components
- [ ] Create `Layout.svelte` (with SSE lifecycle)
- [ ] Create `Sidebar.svelte` (navigation)
- [ ] Create `Header.svelte` (theme toggle)
- [ ] Create `WorkspaceFilter.svelte`

#### A.5 Routing
- [ ] Create `src/routes.ts`
- [ ] Create `src/Root.svelte` (QueryClientProvider)
- [ ] Create `src/App.svelte` (Router + Layout)
- [ ] Create `PlaceholderPage.svelte`

#### A.6 Simple Pages
- [ ] **JobsPage.svelte** - state filter, table with badges
- [ ] **TasksPage.svelte** - limit select, table with status
- [ ] **MailboxPage.svelte** - actor filter, messages

**Acceptance**: Dev server runs, pages load data, SSE triggers refetch

---

### Phase B: Detail Pages + More Tables (Week 2)

#### B.1 Detail Pages
- [ ] **JobDetailPage.svelte** - job metadata + result
- [ ] **TaskDetailPage.svelte** - task details + deps

#### B.2 Route Parameters
- [ ] Update `routes.ts` for `/jobs/:id` and `/tasks/:id`
- [ ] Implement route param extraction

#### B.3 Additional Pages
- [ ] **StatsPage.svelte** - summary cards + stats
- [ ] **AgentsPage.svelte** - agents table + start daemon

#### B.4 Actions
- [ ] Implement `startAgentDaemon` mutation
- [ ] Add loading states and feedback

**Acceptance**: Detail pages work via route params, actions complete

---

### Phase C: Complex Pages (Week 3)

#### C.1 Additional Components
- [ ] **Dialog.svelte** - modal overlay
- [ ] **Accordion.svelte** - expandable sections

#### C.2 InsightsPage.svelte
- [ ] Summary cards
- [ ] PageRank + critical path tables
- [ ] Cycles display
- [ ] Topological order

#### C.3 SearchPage.svelte
- [ ] Search form with options
- [ ] Stats badges
- [ ] Expandable results

#### C.4 CodemapsPage.svelte
- [ ] Codemaps table
- [ ] Detail dialog with traces accordion
- [ ] Search + pagination
- [ ] Delete action

**Acceptance**: Insights/Search/Codemaps fully functional

---

### Phase D: Sessions + SQLite (Week 4)

#### D.1 Tabs Component
- [ ] **Tabs.svelte** - tab navigation

#### D.2 SessionsPage.svelte
- [ ] Sessions list with pagination
- [ ] Detail dialog with 3 view modes (Compact/Detailed/JSON)
- [ ] Search within session
- [ ] Message editing

#### D.3 SQLitePage.svelte
- [ ] Database card grid
- [ ] Table list + breadcrumbs
- [ ] 4 tabs: Data, Schema, Indexes, Query
- [ ] SQL query execution (Cmd+Enter)
- [ ] Results display

**Acceptance**: Sessions view modes work, SQLite query execution works

---

### Phase E: Console + Polish (Week 5)

#### E.1 ConsolePage.svelte
- [ ] Console session list
- [ ] Create new session
- [ ] WebSocket connection
- [ ] Timeline display (user/assistant/tool)
- [ ] Send message with correlation ID
- [ ] Cancel active request
- [ ] Streaming updates
- [ ] Tool call display

#### E.2 Polish
- [ ] Loading skeletons
- [ ] Error handling
- [ ] Keyboard shortcuts
- [ ] Mobile responsiveness
- [ ] Performance optimization

**Acceptance**: Console streams in real-time, all pages polished

---

## 4. Coexistence Strategy

Both GUIs run in parallel during migration:

| GUI | URL | Port |
|-----|-----|------|
| React (existing) | http://localhost:5173 | 5173 |
| Svelte (new) | http://localhost:5174 | 5174 |

Both proxy to the same Go backend at `:8090`.

### Feature Parity Tracking

| Page | React | Svelte | Notes |
|------|:-----:|:------:|-------|
| Jobs | ✅ | ⬜ | |
| JobDetail | ✅ | ⬜ | |
| Tasks | ✅ | ⬜ | |
| TaskDetail | ✅ | ⬜ | |
| Stats | ✅ | ⬜ | |
| Insights | ✅ | ⬜ | |
| Mailbox | ✅ | ⬜ | |
| Sessions | ✅ | ⬜ | |
| Agents | ✅ | ⬜ | |
| SQLite | ✅ | ⬜ | |
| Search | ✅ | ⬜ | |
| Codemaps | ✅ | ⬜ | |
| Console | ✅ | ⬜ | |

---

## 5. Key Files Reference

| Purpose | React File | Notes |
|---------|-----------|-------|
| Types | `packages/data/src/types.ts` | Import directly |
| API Client | `packages/data/src/client.ts` | Can re-export |
| Query Hooks | `packages/gui/src/api/hooks.ts` | Convert to Svelte Query |
| Jobs Page | `packages/gui/src/pages/JobsPage.tsx` | Simple table pattern |
| Sessions Page | `packages/gui/src/pages/SessionsPage.tsx` | Complex (648 lines) |
| SQLite Page | `packages/gui/src/pages/SQLitePage.tsx` | Most complex (661 lines) |
| Console Page | `packages/gui/src/pages/ConsolePage.tsx` | WebSocket streaming |

---

## 6. Success Metrics

1. **Build Success**: Production bundle builds without errors
2. **Feature Parity**: All 13 pages work identically to React
3. **Performance**: Initial load < 2s, transitions < 200ms
4. **Bundle Size**: Target 30-50% smaller than React
5. **DX**: Clear patterns, minimal boilerplate
