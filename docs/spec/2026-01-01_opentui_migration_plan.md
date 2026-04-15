# OpenTUI Migration Plan for foxctl-viewer

**Date:** 2026-01-01
**Status:** RFC
**Author:** Claude Code

## Executive Summary

Migrate the terminal UI component of foxctl-viewer from Go/Bubble Tea to
TypeScript/OpenTUI, enabling code sharing with the existing React web-ui and
providing a more maintainable unified codebase.

## Current Architecture

```
┌─────────────────────────────────────────────────────────────────┐
│                     foxctl-viewer (Go)                        │
│  cmd/foxctl_viewer/ (~5K lines)                               │
│  ├── Bubble Tea framework                                       │
│  ├── Lipgloss styling                                           │
│  └── 9 views: Jobs, Tasks, Insights, Mailbox, Reservations,    │
│               Stats, Blackboard, SQLite, Search                 │
└─────────────────────────┬───────────────────────────────────────┘
                          │ Direct access
                          ▼
┌─────────────────────────────────────────────────────────────────┐
│                      Data Sources                                │
│  ├── ~/.foxctl/jobs/{ULID}/result.json (filesystem)           │
│  ├── ~/.foxctl/storage/*.db (SQLite)                          │
│  └── foxctl run <skill> (CLI execution)                       │
└─────────────────────────────────────────────────────────────────┘
                          ▲
                          │ HTTP API
┌─────────────────────────┴───────────────────────────────────────┐
│                     web-ui (TypeScript)                          │
│  ├── Express.js backend (server/index.js)                       │
│  ├── React frontend (src/)                                      │
│  └── API client (src/api/client.ts)                             │
└─────────────────────────────────────────────────────────────────┘
```

## Target Architecture

```
┌─────────────────────────────────────────────────────────────────┐
│                     Shared UI Components                         │
│  packages/ui/ (React components)                                 │
│  ├── JobsList, JobDetail                                        │
│  ├── TasksList, TaskDetail                                      │
│  ├── SQLiteBrowser                                              │
│  ├── SemanticSearch                                             │
│  └── etc.                                                       │
└──────────────┬────────────────────────────┬─────────────────────┘
               │                            │
      ┌────────▼────────┐          ┌────────▼────────┐
      │   OpenTUI App   │          │   Web App       │
      │   (Bun + TUI)   │          │   (Vite + DOM)  │
      │ @opentui/react  │          │ react-dom       │
      └────────┬────────┘          └────────┬────────┘
               │                            │
               └──────────┬─────────────────┘
                          │
               ┌──────────▼──────────┐
               │    Data Layer       │
               │  (Unified Client)   │
               └──────────┬──────────┘
                          │
          ┌───────────────┼───────────────┐
          ▼               ▼               ▼
    ┌──────────┐   ┌──────────┐   ┌──────────┐
    │ HTTP API │   │ Bun SQL  │   │ foxctl │
    │ (remote) │   │ (local)  │   │   CLI    │
    └──────────┘   └──────────┘   └──────────┘
```

---

## Data Layer Strategy

### Option A: HTTP-First (Recommended)

Reuse the existing Express.js backend from web-ui.

**Pros:**
- Zero data layer work - already complete
- Same API for TUI and web
- Security already implemented (path validation, SQL injection prevention)

**Cons:**
- Requires running backend server
- HTTP overhead for local operations

**Implementation:**
```typescript
// packages/data/src/client.ts
import { hc } from 'hono/client'  // or fetch wrapper

export const api = {
  jobs: {
    list: (opts) => fetch(`${BASE_URL}/api/jobs?${qs(opts)}`),
    get: (id) => fetch(`${BASE_URL}/api/jobs/${id}`),
  },
  tasks: {
    list: (opts) => fetch(`${BASE_URL}/api/tasks?${qs(opts)}`),
  },
  sqlite: {
    databases: () => fetch(`${BASE_URL}/api/sqlite`),
    tables: (db) => fetch(`${BASE_URL}/api/sqlite/${db}`),
    query: (db, sql) => fetch(`${BASE_URL}/api/sqlite/${db}/query`, {
      method: 'POST',
      body: JSON.stringify({ query: sql }),
    }),
  },
  // ... etc
}
```

### Option B: Bun-Native (Performance)

Access data directly using Bun's built-in SQLite and filesystem APIs.

**Pros:**
- No server required
- Lower latency
- Single binary distribution

**Cons:**
- Duplicates security logic from web-ui backend
- Tighter coupling to storage format

**Implementation:**
```typescript
// packages/data/src/native.ts
import { Database } from 'bun:sqlite'
import { readdir, readFile } from 'fs/promises'
import { join } from 'path'
import { homedir } from 'os'

const FOXCTL_HOME = process.env.FOXCTL_HOME || join(homedir(), '.foxctl')

export const native = {
  jobs: {
    async list(opts: { limit?: number; state?: string }) {
      const jobsDir = join(FOXCTL_HOME, 'jobs')
      const entries = await readdir(jobsDir)
      const jobs = []

      for (const id of entries.sort().reverse().slice(0, opts.limit || 50)) {
        const resultPath = join(jobsDir, id, 'result.json')
        try {
          const result = JSON.parse(await readFile(resultPath, 'utf-8'))
          if (opts.state && result.status !== opts.state) continue
          jobs.push({
            id,
            command: result.command,
            state: result.status,
            created_at: result.meta?.ts,
            error: result.error?.message,
          })
        } catch {}
      }
      return jobs
    },
  },

  sqlite: {
    query(dbName: string, sql: string) {
      const dbPath = join(FOXCTL_HOME, 'storage', `${dbName}.db`)
      // Validate path is under FOXCTL_HOME
      if (!dbPath.startsWith(FOXCTL_HOME)) {
        throw new Error('Invalid database path')
      }
      const db = new Database(dbPath, { readonly: true })
      try {
        return db.query(sql).all()
      } finally {
        db.close()
      }
    },
  },
}
```

### Option C: Hybrid (Recommended for Production)

Use HTTP API by default, with optional Bun-native fallback for local-only mode.

```typescript
// packages/data/src/index.ts
import { api } from './client'
import { native } from './native'

export const data = process.env.FOXCTL_LOCAL_MODE === '1' ? native : api
```

---

## Component Migration Plan

### Phase 1: Foundation (Week 1-2)

#### 1.1 Project Setup

```bash
# Create monorepo structure
packages/
├── ui/           # Shared React components
├── data/         # Data access layer
├── tui/          # OpenTUI application
└── web/          # Web application (move from web-ui/)
```

```bash
# Initialize OpenTUI
cd packages/tui
bun init
bun add @opentui/core @opentui/react
```

#### 1.2 Minimal TUI Shell

```typescript
// packages/tui/src/App.tsx
import { useState } from 'react'
import { useKeyboard, useTerminalDimensions } from '@opentui/react'

type View = 'jobs' | 'tasks' | 'sqlite' | 'search'

export function App() {
  const [view, setView] = useState<View>('jobs')
  const { width, height } = useTerminalDimensions()

  useKeyboard((key) => {
    if (key.name === '1') setView('jobs')
    if (key.name === '2') setView('tasks')
    if (key.name === '3') setView('sqlite')
    if (key.name === '4') setView('search')
    if (key.name === 'q' && key.ctrl) process.exit(0)
  })

  return (
    <box width={width} height={height} flexDirection="column">
      <Header currentView={view} />
      <box flexGrow={1}>
        {view === 'jobs' && <JobsView />}
        {view === 'tasks' && <TasksView />}
        {view === 'sqlite' && <SQLiteView />}
        {view === 'search' && <SearchView />}
      </box>
      <StatusBar />
    </box>
  )
}
```

#### 1.3 Data Layer Integration

```typescript
// packages/data/src/hooks.ts
import { useQuery, useMutation } from '@tanstack/react-query'
import { api } from './client'

export function useJobs(opts: { state?: string; limit?: number }) {
  return useQuery({
    queryKey: ['jobs', opts],
    queryFn: () => api.jobs.list(opts),
    refetchInterval: 5000,
  })
}

export function useTasks(opts: { limit?: number }) {
  return useQuery({
    queryKey: ['tasks', opts],
    queryFn: () => api.tasks.list(opts),
  })
}
```

### Phase 2: Core Views (Week 3-4)

#### 2.1 Jobs View

Port the jobs list with split-pane layout.

```typescript
// packages/ui/src/JobsList.tsx
import { useState } from 'react'
import { useJobs } from '@foxctl/data'

interface Props {
  onSelect: (jobId: string) => void
  selectedId?: string
}

export function JobsList({ onSelect, selectedId }: Props) {
  const { data: jobs, isLoading } = useJobs({ limit: 50 })

  if (isLoading) return <text>Loading...</text>

  return (
    <scrollbox height="100%">
      {jobs?.map((job, i) => (
        <JobRow
          key={job.id}
          job={job}
          selected={job.id === selectedId}
          onSelect={() => onSelect(job.id)}
        />
      ))}
    </scrollbox>
  )
}
```

```typescript
// packages/tui/src/views/JobsView.tsx
import { useState } from 'react'
import { useKeyboard } from '@opentui/react'
import { JobsList, JobDetail } from '@foxctl/ui'
import { useJobs, useJobDetail } from '@foxctl/data'

export function JobsView() {
  const [selectedId, setSelectedId] = useState<string>()
  const [cursor, setCursor] = useState(0)
  const { data: jobs } = useJobs({ limit: 50 })
  const { data: detail } = useJobDetail(selectedId)

  useKeyboard((key) => {
    if (!jobs) return
    if (key.name === 'up') setCursor(c => Math.max(0, c - 1))
    if (key.name === 'down') setCursor(c => Math.min(jobs.length - 1, c + 1))
    if (key.name === 'return') setSelectedId(jobs[cursor].id)
    if (key.name === 'escape') setSelectedId(undefined)
  })

  return (
    <box flexDirection="row" width="100%" height="100%">
      <box width="40%">
        <JobsList
          jobs={jobs}
          cursor={cursor}
          selectedId={selectedId}
        />
      </box>
      <box width="60%" borderStyle="single" borderLeft>
        {selectedId ? (
          <JobDetail job={detail} />
        ) : (
          <text fg="#666">Select a job to view details</text>
        )}
      </box>
    </box>
  )
}
```

#### 2.2 SQLite Browser

Three-pane database explorer using OpenTUI's built-in components.

```typescript
// packages/tui/src/views/SQLiteView.tsx
import { useState } from 'react'
import { useKeyboard } from '@opentui/react'
import { useDatabases, useTables, useTableData } from '@foxctl/data'

export function SQLiteView() {
  const [selectedDb, setSelectedDb] = useState<string>()
  const [selectedTable, setSelectedTable] = useState<string>()
  const [pane, setPane] = useState<'dbs' | 'tables' | 'data'>('dbs')

  const { data: databases } = useDatabases()
  const { data: tables } = useTables(selectedDb)
  const { data: tableData } = useTableData(selectedDb, selectedTable)

  useKeyboard((key) => {
    if (key.name === 'tab') {
      setPane(p => p === 'dbs' ? 'tables' : p === 'tables' ? 'data' : 'dbs')
    }
    if (key.name === 'i') {
      // Show schema modal
    }
  })

  return (
    <box flexDirection="row" height="100%">
      <box width="20%" borderStyle={pane === 'dbs' ? 'double' : 'single'}>
        <text bold>Databases</text>
        <select
          options={databases?.map(d => ({ label: d.name, value: d.name }))}
          onChange={setSelectedDb}
        />
      </box>
      <box width="25%" borderStyle={pane === 'tables' ? 'double' : 'single'}>
        <text bold>Tables</text>
        <select
          options={tables?.map(t => ({ label: `${t.name} (${t.row_count})`, value: t.name }))}
          onChange={setSelectedTable}
        />
      </box>
      <box flexGrow={1} borderStyle={pane === 'data' ? 'double' : 'single'}>
        <DataTable columns={tableData?.columns} rows={tableData?.rows} />
      </box>
    </box>
  )
}
```

#### 2.3 Semantic Search

```typescript
// packages/tui/src/views/SearchView.tsx
import { useState } from 'react'
import { useSearch } from '@foxctl/data'

export function SearchView() {
  const [query, setQuery] = useState('')
  const [scope, setScope] = useState<string[]>(['symbols', 'memories'])
  const { data: results, isLoading } = useSearch({ query, scope })

  return (
    <box flexDirection="column" height="100%">
      <box height={3}>
        <input
          placeholder="Search..."
          value={query}
          onChange={setQuery}
          autoFocus
        />
        <tab-select
          options={[
            { label: 'Symbols', value: 'symbols' },
            { label: 'Memories', value: 'memories' },
            { label: 'Sessions', value: 'sessions' },
            { label: 'Tasks', value: 'tasks' },
          ]}
          selected={scope}
          onChange={setScope}
        />
      </box>
      <scrollbox flexGrow={1}>
        {isLoading && <text fg="#666">Searching...</text>}
        {results?.map((r, i) => (
          <SearchResult key={i} result={r} />
        ))}
      </scrollbox>
    </box>
  )
}
```

### Phase 3: Advanced Views (Week 5-6)

| View | OpenTUI Components | Notes |
|------|-------------------|-------|
| Tasks | `<select>`, `<scrollbox>` | Action keys: d=done, s=set active |
| Insights | `<box>` with styled text | Graph visualization may need custom |
| Mailbox | `<select>`, `<scrollbox>` | Priority coloring |
| Reservations | `<scrollbox>` | Lease expiry display |
| Blackboard | `<scrollbox>`, JSON viewer | Pretty-print payloads |
| Stats | Styled `<box>` layout | Bar charts via ASCII |

### Phase 4: Robot Mode (Week 7)

Port JSON output mode for AI agent consumption.

```typescript
// packages/tui/src/robot.ts
import { api } from '@foxctl/data'

interface RobotArgs {
  jobs?: boolean
  job?: string
  tasks?: boolean
  search?: string
  // ...
}

export async function robotMode(args: RobotArgs) {
  const envelope = (cmd: string, data: unknown, meta = {}) => ({
    version: 1,
    status: 'ok',
    command: cmd,
    data,
    meta: { ...meta, generated_at: new Date().toISOString() },
  })

  if (args.jobs) {
    const jobs = await api.jobs.list({ limit: 100 })
    console.log(JSON.stringify(envelope('viewer/jobs', { jobs })))
    return
  }

  if (args.job) {
    const job = await api.jobs.get(args.job)
    console.log(JSON.stringify(envelope('viewer/job', job)))
    return
  }

  // ... etc
}
```

### Phase 5: Polish & Testing (Week 8)

1. **Keyboard shortcuts parity** - Match all existing keybindings
2. **Auto-refresh** - 5-second refresh for jobs view
3. **Error handling** - Graceful degradation when API unavailable
4. **Help screen** - Port help.go content
5. **Testing** - Unit tests for components, integration tests for data layer

---

## Migration Checklist

### Prerequisites

- [ ] Bun installed (required for OpenTUI)
- [ ] Zig toolchain (for OpenTUI native layer)
- [ ] Node.js 20+ (for web-ui backend)

### Phase 1: Foundation

- [ ] Create monorepo structure with workspaces
- [ ] Initialize OpenTUI package
- [ ] Create data layer package with HTTP client
- [ ] Minimal TUI app rendering

### Phase 2: Core Views

- [ ] Port Jobs view (list + detail)
- [ ] Port SQLite browser (3-pane)
- [ ] Port Semantic search
- [ ] Port Tasks view

### Phase 3: Advanced Views

- [ ] Port Insights dashboard
- [ ] Port Mailbox view
- [ ] Port Reservations view
- [ ] Port Stats dashboard
- [ ] Port Blackboard view

### Phase 4: Features

- [ ] Robot mode (JSON output)
- [ ] Keyboard shortcuts
- [ ] Auto-refresh
- [ ] Help screen

### Phase 5: Polish

- [ ] Error boundaries
- [ ] Loading states
- [ ] Responsive layout
- [ ] Documentation
- [ ] Tests

---

## Risk Assessment

| Risk | Impact | Mitigation |
|------|--------|------------|
| Bun runtime requirement | Medium | Document requirement; provide Node fallback path |
| OpenTUI stability | Medium | Pin versions; contribute fixes upstream |
| Feature parity gaps | Low | Prioritize core flows; defer edge cases |
| Performance regression | Low | Profile early; use Bun-native for hot paths |

## Decision Points

1. **Monorepo vs Separate Repos**: Recommend monorepo for shared types/components
2. **React Query vs Manual Fetch**: Recommend React Query for caching/refetch
3. **HTTP vs Bun-Native**: Start with HTTP; add Bun-native for local-only mode later
4. **Web-UI Refactor**: Move to packages/web/ or keep separate?

## Open Questions

1. Should we support running without the backend server (Bun-native only)?
2. Do we need to maintain the Go TUI during migration, or can we deprecate?
3. Should robot mode be a separate binary or integrated?

---

## Appendix: OpenTUI Component Reference

### Layout

```tsx
<box flexDirection="row" justifyContent="space-between" alignItems="center">
  <box width="50%" height={10} />
  <box flexGrow={1} />
</box>
```

### Text & Styling

```tsx
<text fg="#00FF00" bold underline>Styled text</text>
<span><b>Bold</b> and <i>italic</i></span>
```

### Input

```tsx
<input
  placeholder="Enter query..."
  value={query}
  onChange={setQuery}
  onEnter={() => search()}
/>
```

### Selection

```tsx
<select
  options={[
    { label: 'Option 1', value: '1' },
    { label: 'Option 2', value: '2' },
  ]}
  value={selected}
  onChange={setSelected}
/>
```

### Scrolling

```tsx
<scrollbox height={20} overflow="scroll">
  {items.map(item => <Item key={item.id} {...item} />)}
</scrollbox>
```

### Code Display

```tsx
<code language="typescript">
{`function hello() {
  console.log('world')
}`}
</code>
```
