Cool — SPA is the cleanest swap because it matches your current React/Vite mental model and keeps **Go backend + `/api/*` + SSE** unchanged.

Below is **copy/paste scaffolding** you (or Codex/Claude) can implement as a new package: `packages/gui-svelte/`, reusing `@agentctl/data`.

---

# 0) Goal

Create a Svelte SPA that:

* uses the existing Go backend (`/api/...`)
* reuses `@agentctl/data` for types + API calls
* uses TanStack Query (Svelte) for caching + invalidation
* uses SSE (`/api/events`) to invalidate queries in real-time
* ports pages gradually (start with Jobs + Tasks)

---

# 1) Add a new package: `packages/gui-svelte/`

## 1.1 File tree (initial)

```
packages/gui-svelte/
  index.html
  package.json
  tsconfig.json
  vite.config.ts
  postcss.config.cjs
  tailwind.config.cjs
  src/
    main.ts
    app.css
    Root.svelte
    App.svelte
    routes.ts

    lib/
      api/
        queryClient.ts
        sse.ts
        client.ts
      components/
        layout/
          Layout.svelte
          Sidebar.svelte
          Header.svelte
        WorkspaceFilter.svelte
      utils/
        time.ts

    pages/
      JobsPage.svelte
      TasksPage.svelte
      PlaceholderPage.svelte
```

---

# 2) Concrete files (paste these)

## `packages/gui-svelte/package.json`

```json
{
  "name": "@agentctl/gui-svelte",
  "private": true,
  "version": "0.0.0",
  "type": "module",
  "scripts": {
    "dev": "vite --port 5174",
    "build": "vite build",
    "preview": "vite preview --port 5174",
    "typecheck": "svelte-check --tsconfig ./tsconfig.json"
  },
  "dependencies": {
    "@agentctl/data": "workspace:*",
    "@tanstack/svelte-query": "^5.0.0",
    "lucide-svelte": "^0.500.0",
    "svelte": "^4.2.0",
    "svelte-spa-router": "^4.0.0"
  },
  "devDependencies": {
    "@sveltejs/vite-plugin-svelte": "^3.0.0",
    "@types/node": "^24.0.0",
    "autoprefixer": "^10.4.0",
    "postcss": "^8.4.0",
    "svelte-check": "^3.0.0",
    "tailwindcss": "^3.4.0",
    "typescript": "^5.9.0",
    "vite": "^7.0.0"
  }
}
```

> If you’re already pinning versions repo-wide, align these with whatever you use in `packages/gui`.

---

## `packages/gui-svelte/vite.config.ts`

```ts
import { defineConfig } from "vite";
import { svelte } from "@sveltejs/vite-plugin-svelte";
import path from "node:path";

export default defineConfig({
  plugins: [svelte()],
  resolve: {
    alias: {
      "@": path.resolve(__dirname, "./src"),
    },
  },
  server: {
    proxy: {
      "/api": {
        target: "http://localhost:8090",
        changeOrigin: true,
      },
    },
  },
});
```

---

## `packages/gui-svelte/tsconfig.json`

```json
{
  "compilerOptions": {
    "target": "ES2022",
    "lib": ["ES2022", "DOM", "DOM.Iterable"],
    "module": "ESNext",
    "moduleResolution": "bundler",
    "strict": true,
    "verbatimModuleSyntax": true,
    "skipLibCheck": true,
    "types": ["vite/client"],
    "baseUrl": ".",
    "paths": {
      "@/*": ["src/*"]
    }
  },
  "include": ["src/**/*.ts", "src/**/*.svelte"]
}
```

---

## `packages/gui-svelte/tailwind.config.cjs`

```js
/** @type {import('tailwindcss').Config} */
module.exports = {
  darkMode: ["class"],
  content: ["./index.html", "./src/**/*.{svelte,ts,js}"],
  theme: {
    extend: {
      colors: {
        border: "hsl(var(--border))",
        input: "hsl(var(--input))",
        ring: "hsl(var(--ring))",
        background: "hsl(var(--background))",
        foreground: "hsl(var(--foreground))",
        card: "hsl(var(--card))",
        "card-foreground": "hsl(var(--card-foreground))",
        muted: "hsl(var(--muted))",
        "muted-foreground": "hsl(var(--muted-foreground))",
        primary: {
          DEFAULT: "hsl(var(--primary))",
          foreground: "hsl(var(--primary-foreground))"
        },
        secondary: {
          DEFAULT: "hsl(var(--secondary))",
          foreground: "hsl(var(--secondary-foreground))"
        },
        destructive: {
          DEFAULT: "hsl(var(--destructive))",
          foreground: "hsl(var(--destructive-foreground))"
        },
        accent: {
          DEFAULT: "hsl(var(--accent))",
          foreground: "hsl(var(--accent-foreground))"
        }
      },
      borderRadius: {
        md: "calc(var(--radius))"
      }
    }
  },
  plugins: []
};
```

---

## `packages/gui-svelte/postcss.config.cjs`

```js
module.exports = {
  plugins: {
    tailwindcss: {},
    autoprefixer: {},
  },
};
```

---

## `packages/gui-svelte/src/app.css`

(copy your React CSS variables so dark mode matches)

```css
@tailwind base;
@tailwind components;
@tailwind utilities;

@layer base {
  :root {
    --background: 0 0% 100%;
    --foreground: 222.2 84% 4.9%;
    --card: 0 0% 100%;
    --card-foreground: 222.2 84% 4.9%;
    --popover: 0 0% 100%;
    --popover-foreground: 222.2 84% 4.9%;
    --primary: 222.2 47.4% 11.2%;
    --primary-foreground: 210 40% 98%;
    --secondary: 210 40% 96.1%;
    --secondary-foreground: 222.2 47.4% 11.2%;
    --muted: 210 40% 96.1%;
    --muted-foreground: 215.4 16.3% 46.9%;
    --accent: 210 40% 96.1%;
    --accent-foreground: 222.2 47.4% 11.2%;
    --destructive: 0 84.2% 60.2%;
    --destructive-foreground: 210 40% 98%;
    --border: 214.3 31.8% 91.4%;
    --input: 214.3 31.8% 91.4%;
    --ring: 222.2 84% 4.9%;
    --radius: 0.5rem;
  }

  .dark {
    --background: 222.2 84% 4.9%;
    --foreground: 210 40% 98%;
    --card: 222.2 84% 4.9%;
    --card-foreground: 210 40% 98%;
    --popover: 222.2 84% 4.9%;
    --popover-foreground: 210 40% 98%;
    --primary: 210 40% 98%;
    --primary-foreground: 222.2 47.4% 11.2%;
    --secondary: 217.2 32.6% 17.5%;
    --secondary-foreground: 210 40% 98%;
    --muted: 217.2 32.6% 17.5%;
    --muted-foreground: 215 20.2% 65.1%;
    --accent: 217.2 32.6% 17.5%;
    --accent-foreground: 210 40% 98%;
    --destructive: 0 62.8% 30.6%;
    --destructive-foreground: 210 40% 98%;
    --border: 217.2 32.6% 17.5%;
    --input: 217.2 32.6% 17.5%;
    --ring: 212.7 26.8% 83.9%;
  }

  body {
    margin: 0;
    min-height: 100vh;
  }
}
```

---

## `packages/gui-svelte/src/main.ts`

```ts
import "./app.css";
import Root from "./Root.svelte";

// Theme init (mirrors your React app)
const savedTheme = localStorage.getItem("theme");
if (
  savedTheme === "dark" ||
  (!savedTheme && window.matchMedia("(prefers-color-scheme: dark)").matches)
) {
  document.documentElement.classList.add("dark");
}

new Root({
  target: document.getElementById("app")!,
});
```

---

## `packages/gui-svelte/src/Root.svelte`

```svelte
<script lang="ts">
  import { QueryClientProvider } from "@tanstack/svelte-query";
  import App from "./App.svelte";
  import { queryClient } from "@/lib/api/queryClient";
</script>

<QueryClientProvider client={queryClient}>
  <App />
</QueryClientProvider>
```

---

## `packages/gui-svelte/src/lib/api/queryClient.ts`

```ts
import { QueryClient } from "@tanstack/svelte-query";

export const queryClient = new QueryClient({
  defaultOptions: {
    queries: {
      staleTime: 10_000,
      refetchOnWindowFocus: false,
    },
  },
});
```

---

## `packages/gui-svelte/src/lib/api/client.ts`

(Reuse your shared data layer)

```ts
export * from "@agentctl/data";
```

---

## `packages/gui-svelte/src/lib/api/sse.ts`

Simple “invalidate on any event” version (works immediately, refine later):

```ts
import { queryClient } from "./queryClient";

export function startSSE(): () => void {
  const es = new EventSource("/api/events", { withCredentials: true });

  es.onmessage = () => {
    // blunt but reliable
    queryClient.invalidateQueries();
  };

  es.onerror = () => {
    // keep it alive; backend restarts happen in dev
    // browser will auto-retry EventSource
  };

  return () => es.close();
}
```

---

## `packages/gui-svelte/src/routes.ts`

```ts
import JobsPage from "@/pages/JobsPage.svelte";
import TasksPage from "@/pages/TasksPage.svelte";
import PlaceholderPage from "@/pages/PlaceholderPage.svelte";

export default {
  "/": JobsPage,
  "/jobs": JobsPage,
  "/tasks": TasksPage,
  "/sessions": PlaceholderPage,
  "/agents": PlaceholderPage,
  "/mailbox": PlaceholderPage,
  "/search": PlaceholderPage,
  "/sqlite": PlaceholderPage,
  "/insights": PlaceholderPage,
} as const;
```

---

## `packages/gui-svelte/src/App.svelte`

```svelte
<script lang="ts">
  import Router from "svelte-spa-router";
  import routes from "./routes";
  import Layout from "@/lib/components/layout/Layout.svelte";
</script>

<Layout>
  <Router {routes} />
</Layout>
```

---

## `packages/gui-svelte/src/lib/components/layout/Layout.svelte`

```svelte
<script lang="ts">
  import { onMount } from "svelte";
  import Sidebar from "./Sidebar.svelte";
  import Header from "./Header.svelte";
  import { startSSE } from "@/lib/api/sse";

  let stop = () => {};

  onMount(() => {
    stop = startSSE();
    return () => stop();
  });
</script>

<div class="flex h-screen bg-background text-foreground">
  <Sidebar />
  <div class="flex flex-1 flex-col overflow-hidden">
    <Header />
    <main class="flex-1 overflow-auto p-6">
      <slot />
    </main>
  </div>
</div>
```

---

## `packages/gui-svelte/src/lib/components/layout/Sidebar.svelte`

```svelte
<script lang="ts">
  const nav = [
    { name: "Jobs", href: "#/jobs" },
    { name: "Tasks", href: "#/tasks" },
    { name: "Sessions", href: "#/sessions" },
    { name: "Agents", href: "#/agents" },
    { name: "Mailbox", href: "#/mailbox" },
    { name: "Search", href: "#/search" },
    { name: "SQLite", href: "#/sqlite" },
    { name: "Insights", href: "#/insights" }
  ];
</script>

<div class="flex h-full w-64 flex-col bg-card border-r">
  <div class="flex h-16 items-center border-b px-6">
    <span class="text-lg font-semibold">agentctl</span>
  </div>

  <nav class="flex-1 space-y-1 px-3 py-4">
    {#each nav as item}
      <a
        class="block rounded-lg px-3 py-2 text-sm font-medium text-muted-foreground hover:bg-accent hover:text-accent-foreground"
        href={item.href}
      >
        {item.name}
      </a>
    {/each}
  </nav>

  <div class="border-t p-4">
    <p class="text-xs text-muted-foreground">agentctl web (svelte)</p>
  </div>
</div>
```

---

## `packages/gui-svelte/src/lib/components/layout/Header.svelte`

```svelte
<script lang="ts">
  import WorkspaceFilter from "@/lib/components/WorkspaceFilter.svelte";

  let isDark = document.documentElement.classList.contains("dark");

  function toggleTheme() {
    isDark = !isDark;
    document.documentElement.classList.toggle("dark", isDark);
    localStorage.setItem("theme", isDark ? "dark" : "light");
  }
</script>

<header class="flex h-16 items-center justify-between border-b bg-card px-6">
  <div class="flex items-center gap-4">
    <h1 class="text-lg font-semibold">agentctl</h1>
    <WorkspaceFilter />
  </div>

  <div class="flex items-center gap-2">
    <button
      class="rounded-md border px-3 py-1 text-sm hover:bg-accent"
      on:click={toggleTheme}
    >
      {isDark ? "Light" : "Dark"}
    </button>
  </div>
</header>
```

---

## `packages/gui-svelte/src/lib/components/WorkspaceFilter.svelte`

This includes a safe “fallback POST path” because your backend path may be either.

```svelte
<script lang="ts">
  import { createQuery } from "@tanstack/svelte-query";
  import { queryClient } from "@/lib/api/queryClient";

  type WorkspaceItem = { name: string; path: string };
  type WorkspacesResponse = { workspaces: WorkspaceItem[]; current?: string };

  async function getWorkspaces(): Promise<WorkspacesResponse> {
    const res = await fetch("/api/workspaces", { credentials: "include" });
    if (!res.ok) throw new Error(await res.text());
    return res.json();
  }

  async function switchWorkspace(workspace: string): Promise<void> {
    // try /api/workspaces/switch first, fallback to /api/workspaces
    const payload = JSON.stringify({ workspace });

    let res = await fetch("/api/workspaces/switch", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      credentials: "include",
      body: payload,
    });

    if (res.status === 404) {
      res = await fetch("/api/workspaces", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        credentials: "include",
        body: payload,
      });
    }

    if (!res.ok) throw new Error(await res.text());
  }

  const q = createQuery({
    queryKey: ["workspaces"],
    queryFn: getWorkspaces,
  });

  let error: string | null = null;

  async function onChange(e: Event) {
    const value = (e.target as HTMLSelectElement).value;
    error = null;
    try {
      await switchWorkspace(value);
      queryClient.invalidateQueries();
    } catch (err) {
      error = err instanceof Error ? err.message : "Failed to switch workspace";
    }
  }
</script>

{#if $q.data?.workspaces?.length}
  <div class="flex items-center gap-2">
    <select
      class="h-9 rounded-md border bg-background px-2 text-sm"
      on:change={onChange}
      value={$q.data.current ?? ""}
    >
      <option value="">All workspaces</option>
      {#each $q.data.workspaces as ws}
        <option value={ws.path} title={ws.path}>{ws.name}</option>
      {/each}
    </select>

    {#if error}
      <span class="text-xs text-destructive" title={error}>Error</span>
    {/if}
  </div>
{/if}
```

---

## `packages/gui-svelte/src/lib/utils/time.ts`

```ts
export function formatRelativeTime(dateString: string): string {
  if (!dateString) return "-";
  const date = new Date(dateString);
  const now = new Date();
  const diffMs = now.getTime() - date.getTime();

  const diffSec = Math.floor(diffMs / 1000);
  const diffMin = Math.floor(diffSec / 60);
  const diffHour = Math.floor(diffMin / 60);
  const diffDay = Math.floor(diffHour / 24);

  if (diffSec < 60) return `${diffSec}s ago`;
  if (diffMin < 60) return `${diffMin}m ago`;
  if (diffHour < 24) return `${diffHour}h ago`;
  return `${diffDay}d ago`;
}
```

---

## `packages/gui-svelte/src/pages/JobsPage.svelte`

Uses `@agentctl/data` (via the re-export in `client.ts`).

```svelte
<script lang="ts">
  import { createQuery } from "@tanstack/svelte-query";
  import { getJobs } from "@/lib/api/client";
  import { formatRelativeTime } from "@/lib/utils/time";

  let state = "";
  let limit = 50;

  const q = createQuery({
    queryKey: ["jobs", () => state, () => limit],
    queryFn: async () => {
      return getJobs({ state: state || undefined, limit });
    },
  });
</script>

<div class="space-y-6">
  <div class="flex items-center justify-between">
    <h1 class="text-2xl font-bold">Jobs</h1>
    <button class="rounded-md border px-3 py-1 text-sm hover:bg-accent" on:click={() => q.refetch()}>
      Refresh
    </button>
  </div>

  <div class="flex gap-4">
    <div class="space-y-1">
      <label class="text-sm font-medium">State</label>
      <select class="h-9 rounded-md border bg-background px-2 text-sm" bind:value={state}>
        <option value="">All</option>
        <option value="completed">Completed</option>
        <option value="running">Running</option>
        <option value="pending">Pending</option>
        <option value="failed">Failed</option>
      </select>
    </div>

    <div class="space-y-1">
      <label class="text-sm font-medium">Limit</label>
      <select class="h-9 rounded-md border bg-background px-2 text-sm" bind:value={limit}>
        <option value={25}>25</option>
        <option value={50}>50</option>
        <option value={100}>100</option>
        <option value={200}>200</option>
      </select>
    </div>
  </div>

  {#if $q.isLoading}
    <div class="text-muted-foreground">Loading…</div>
  {:else if $q.isError}
    <div class="text-destructive">Error: {$q.error?.message}</div>
  {:else}
    <div class="overflow-auto rounded-md border">
      <table class="w-full text-sm">
        <thead class="bg-muted/50 text-left">
          <tr>
            <th class="p-3">ID</th>
            <th class="p-3">Skill</th>
            <th class="p-3">State</th>
            <th class="p-3">Created</th>
          </tr>
        </thead>
        <tbody>
          {#each $q.data?.jobs ?? [] as job}
            <tr class="border-t">
              <td class="p-3 font-mono text-xs">{job.id}</td>
              <td class="p-3">{job.skill || job.command}</td>
              <td class="p-3">{job.state}</td>
              <td class="p-3 text-muted-foreground">{formatRelativeTime(job.created_at)}</td>
            </tr>
          {/each}
        </tbody>
      </table>
    </div>
  {/if}
</div>
```

---

## `packages/gui-svelte/src/pages/TasksPage.svelte`

```svelte
<script lang="ts">
  import { createQuery } from "@tanstack/svelte-query";
  import { getTasks } from "@/lib/api/client";
  import { formatRelativeTime } from "@/lib/utils/time";

  const q = createQuery({
    queryKey: ["tasks", 100],
    queryFn: () => getTasks({ limit: 100 }),
  });
</script>

<div class="space-y-6">
  <div class="flex items-center justify-between">
    <h1 class="text-2xl font-bold">Tasks</h1>
    <button class="rounded-md border px-3 py-1 text-sm hover:bg-accent" on:click={() => q.refetch()}>
      Refresh
    </button>
  </div>

  {#if $q.isLoading}
    <div class="text-muted-foreground">Loading…</div>
  {:else if $q.isError}
    <div class="text-destructive">Error: {$q.error?.message}</div>
  {:else}
    <div class="overflow-auto rounded-md border">
      <table class="w-full text-sm">
        <thead class="bg-muted/50 text-left">
          <tr>
            <th class="p-3">Title</th>
            <th class="p-3">Status</th>
            <th class="p-3">Score</th>
            <th class="p-3">Created</th>
          </tr>
        </thead>
        <tbody>
          {#each $q.data?.tasks ?? [] as t}
            <tr class="border-t">
              <td class="p-3">{t.title}</td>
              <td class="p-3">{t.status ?? "pending"}</td>
              <td class="p-3 font-mono">{t.score?.toFixed?.(2) ?? t.score}</td>
              <td class="p-3 text-muted-foreground">{formatRelativeTime(t.created_at)}</td>
            </tr>
          {/each}
        </tbody>
      </table>
    </div>
  {/if}
</div>
```

---

## `packages/gui-svelte/src/pages/PlaceholderPage.svelte`

```svelte
<div class="flex h-64 items-center justify-center">
  <p class="text-muted-foreground">Coming soon…</p>
</div>
```

---

## `packages/gui-svelte/index.html`

```html
<!doctype html>
<html lang="en">
  <head>
    <meta charset="UTF-8" />
    <meta name="viewport" content="width=device-width, initial-scale=1.0" />
    <title>agentctl</title>
  </head>
  <body>
    <div id="app"></div>
    <script type="module" src="/src/main.ts"></script>
  </body>
</html>
```

---

# 3) How to run

From repo root (whatever you use for the existing workspace):

```bash
cd packages/gui-svelte
bun install
bun run dev
```

Backend should be running on `localhost:8090` (same as your React proxy setup).

---

# 4) Implementation plan (what to give Codex/Claude)

### Phase 1 — Scaffold + parity baseline

* Create `packages/gui-svelte` with Vite + Svelte + TS
* Add Tailwind theme vars (copy from React)
* Wire TanStack Query + SSE invalidation
* Implement pages: `JobsPage`, `TasksPage`
  **Acceptance:** loads data + refresh works + SSE updates trigger UI refresh.

### Phase 2 — Port the rest of the “viewer” pages

Port in order:

1. Sessions
2. Agents
3. Mailbox
4. Search
5. SQLite
6. Insights
   **Acceptance:** feature parity with existing React GUI.

### Phase 3 — “Console” / “Chat” UI (your Claude Code replacement surface)

* A dedicated page to attach to an agent/overseer
* Stream events/toolcalls (SSE or WS)
* Show correlation IDs + allow cancel/retry via console commands
  **Acceptance:** you can chat with overseer + see tool execution timeline.

---

Awesome — here’s a **ConsolePage SPA skeleton** that shows a Claude-Code-like **streaming timeline** (user → events/toolcalls → final reply), with **send + cancel**, backed by **SSE**.

I’m going to assume a minimal backend API (paths below). If your server already has console endpoints with different paths, you only need to change the `BASE` constant in `consoleApi.ts`.

---

# 1) Frontend: add a Console route + nav item

## `packages/gui-svelte/src/routes.ts` (add)

```ts
import ConsolePage from "@/pages/ConsolePage.svelte";
// ...
export default {
  "/": JobsPage,
  "/jobs": JobsPage,
  "/tasks": TasksPage,
  "/console": ConsolePage,
  // ...
} as const;
```

## `packages/gui-svelte/src/lib/components/layout/Sidebar.svelte` (add link)

```svelte
<script lang="ts">
  const nav = [
    { name: "Console", href: "#/console" },
    { name: "Jobs", href: "#/jobs" },
    { name: "Tasks", href: "#/tasks" },
    // ...
  ];
</script>
```

---

# 2) Frontend: Console types + API wrapper

## `packages/gui-svelte/src/lib/api/consoleTypes.ts`

Matches the spirit of `internal/domain/console/types.go`:

```ts
export type ConsolePayloadType = "ask" | "reply" | "event" | "cmd";

export interface ConsoleProgress {
  pct: number;   // 0-100
  phase: string; // "planning" | "tool" | "final" etc
}

export interface ConsoleMetadata {
  mime?: string;        // text/plain | text/markdown | application/json
  partial?: boolean;    // for streaming chunks
  exit_code?: number;
  error?: string;
  progress?: ConsoleProgress;
  tool?: string;        // tool name for tool events
  cas_digest?: string;  // sha256:...
}

export interface ConsoleCommand {
  name: string;               // "cancel"
  correlation_id?: string;
}

export interface ConsolePayload {
  type: ConsolePayloadType;
  actor_id: string;
  console_id: string;
  correlation_id: string;
  content: string;
  metadata?: ConsoleMetadata;
  cmd?: ConsoleCommand;
}

export interface ConsoleSession {
  console_id: string;
  actor_id: string;
  session_id?: string;
  workspace: string;
  created_at: string;
  last_attached_at: string;
  meta?: Record<string, unknown>;
}

export interface CreateConsoleSessionRequest {
  actor_id: string;
  session_id?: string;
  workspace?: string;
  meta?: Record<string, unknown>;
}

export interface SendConsoleRequest {
  content: string;
  correlation_id?: string;
  mime?: string;
}

export interface SendConsoleResponse {
  correlation_id: string;
}

export interface CancelConsoleRequest {
  correlation_id: string;
}

export interface ListConsoleSessionsResponse {
  sessions: ConsoleSession[];
}
```

## `packages/gui-svelte/src/lib/api/consoleApi.ts`

```ts
import type {
  CancelConsoleRequest,
  CreateConsoleSessionRequest,
  ListConsoleSessionsResponse,
  SendConsoleRequest,
  SendConsoleResponse,
  ConsoleSession
} from "./consoleTypes";

const BASE = "/api/console";

async function req<T>(path: string, init?: RequestInit): Promise<T> {
  const res = await fetch(`${BASE}${path}`, {
    ...init,
    headers: {
      "Content-Type": "application/json",
      ...(init?.headers ?? {}),
    },
    credentials: "include",
  });
  if (!res.ok) throw new Error(await res.text());
  return res.json() as Promise<T>;
}

export async function listConsoleSessions(): Promise<ListConsoleSessionsResponse> {
  return req(`/sessions`);
}

export async function createConsoleSession(body: CreateConsoleSessionRequest): Promise<ConsoleSession> {
  return req(`/sessions`, { method: "POST", body: JSON.stringify(body) });
}

export async function sendConsole(consoleId: string, body: SendConsoleRequest): Promise<SendConsoleResponse> {
  return req(`/sessions/${encodeURIComponent(consoleId)}/send`, {
    method: "POST",
    body: JSON.stringify(body),
  });
}

export async function cancelConsole(consoleId: string, body: CancelConsoleRequest): Promise<{ ok: true }> {
  return req(`/sessions/${encodeURIComponent(consoleId)}/cancel`, {
    method: "POST",
    body: JSON.stringify(body),
  });
}

/**
 * SSE endpoint. Server should stream ConsolePayload as JSON per message.
 * Example URL: /api/console/sessions/:id/events
 */
export function consoleEventsURL(consoleId: string): string {
  return `${BASE}/sessions/${encodeURIComponent(consoleId)}/events`;
}
```

---

# 3) Frontend: Console page (timeline + streaming + tool events)

## `packages/gui-svelte/src/pages/ConsolePage.svelte`

```svelte
<script lang="ts">
  import { onDestroy } from "svelte";
  import { createQuery } from "@tanstack/svelte-query";

  import {
    listConsoleSessions,
    createConsoleSession,
    sendConsole,
    cancelConsole,
    consoleEventsURL
  } from "@/lib/api/consoleApi";

  import type { ConsolePayload, ConsoleSession } from "@/lib/api/consoleTypes";

  type TimelineItem =
    | { kind: "user"; correlationId: string; text: string; ts: number }
    | { kind: "assistant"; correlationId: string; text: string; partial: boolean; ts: number; error?: string }
    | { kind: "tool"; correlationId: string; tool: string; phase: "call" | "result"; text: string; ts: number; casDigest?: string; error?: string }
    | { kind: "system"; text: string; ts: number };

  const sessionsQ = createQuery({
    queryKey: ["console", "sessions"],
    queryFn: listConsoleSessions,
  });

  let sessions: ConsoleSession[] = [];
  $: sessions = $sessionsQ.data?.sessions ?? [];

  let selectedConsoleId: string | null = null;
  let selectedActorId: string = "actor:system:overseer";

  let es: EventSource | null = null;
  let timeline: TimelineItem[] = [];
  let inputText = "";
  let activeCorrelationId: string | null = null;

  function nowTs() {
    return Date.now();
  }

  function addSystem(text: string) {
    timeline = [...timeline, { kind: "system", text, ts: nowTs() }];
  }

  function connect(consoleId: string) {
    // close previous
    if (es) es.close();
    es = new EventSource(consoleEventsURL(consoleId), { withCredentials: true });

    es.onopen = () => addSystem(`Connected: ${consoleId}`);
    es.onerror = () => {
      // EventSource auto-retries; keep UI calm
    };

    es.onmessage = (ev) => {
      try {
        const payload = JSON.parse(ev.data) as ConsolePayload;
        handlePayload(payload);
      } catch (e) {
        addSystem(`Bad event JSON: ${String(e)}`);
      }
    };
  }

  function findAssistantIndex(correlationId: string): number {
    return timeline.findIndex((t) => t.kind === "assistant" && t.correlationId === correlationId);
  }

  function ensureAssistant(correlationId: string) {
    const idx = findAssistantIndex(correlationId);
    if (idx >= 0) return;

    timeline = [
      ...timeline,
      { kind: "assistant", correlationId, text: "", partial: true, ts: nowTs() },
    ];
  }

  function appendAssistant(correlationId: string, chunk: string, partial: boolean, error?: string) {
    ensureAssistant(correlationId);
    const idx = findAssistantIndex(correlationId);
    if (idx < 0) return;

    const existing = timeline[idx] as TimelineItem & { kind: "assistant" };
    const next = {
      ...existing,
      text: (existing.text ?? "") + (chunk ?? ""),
      partial,
      error: error ?? existing.error,
      ts: nowTs()
    } as const;

    timeline = [...timeline.slice(0, idx), next, ...timeline.slice(idx + 1)];
  }

  function addToolEvent(correlationId: string, tool: string, phase: "call" | "result", text: string, casDigest?: string, error?: string) {
    timeline = [
      ...timeline,
      { kind: "tool", correlationId, tool, phase, text, ts: nowTs(), casDigest, error }
    ];
  }

  function handlePayload(p: ConsolePayload) {
    // Common pieces
    const cid = p.correlation_id;
    const meta = p.metadata ?? {};

    // Track "active correlation" (single in-flight UX)
    if (!activeCorrelationId && p.type === "event") {
      activeCorrelationId = cid;
    }

    // Tool events: metadata.tool is the marker
    if (p.type === "event" && meta.tool) {
      // Heuristic: if content looks like result-ish, treat as result
      const phase = (meta.error || meta.cas_digest) ? "result" : "call";
      addToolEvent(cid, meta.tool, phase, p.content ?? "", meta.cas_digest, meta.error);
      return;
    }

    // Streaming assistant events
    if (p.type === "event") {
      const partial = meta.partial !== false; // default true for event
      appendAssistant(cid, p.content ?? "", partial, meta.error);
      return;
    }

    // Final reply
    if (p.type === "reply") {
      appendAssistant(cid, p.content ?? "", false, meta.error);
      if (activeCorrelationId === cid) activeCorrelationId = null;
      return;
    }

    // Ask echoes (optional)
    if (p.type === "ask") {
      timeline = [...timeline, { kind: "user", correlationId: cid, text: p.content ?? "", ts: nowTs() }];
      return;
    }
  }

  async function selectSession(consoleId: string) {
    selectedConsoleId = consoleId;
    timeline = [];
    activeCorrelationId = null;
    connect(consoleId);
  }

  async function createNewSession() {
    const session = await createConsoleSession({
      actor_id: selectedActorId,
      // workspace/session_id optional; backend can infer from cookie/env
    });
    await sessionsQ.refetch();
    await selectSession(session.console_id);
  }

  async function send() {
    if (!selectedConsoleId) {
      addSystem("No console selected. Create one first.");
      return;
    }
    if (!inputText.trim()) return;
    if (activeCorrelationId) return; // enforce single in-flight

    const text = inputText.trim();
    inputText = "";

    // client correlation id (backend may return another; we will reconcile)
    const provisional = crypto.randomUUID();
    timeline = [...timeline, { kind: "user", correlationId: provisional, text, ts: nowTs() }];
    ensureAssistant(provisional);
    activeCorrelationId = provisional;

    try {
      const resp = await sendConsole(selectedConsoleId, {
        content: text,
        correlation_id: provisional,
        mime: "text/plain",
      });

      // If backend returns a different correlation_id, rewrite our provisional ids
      if (resp.correlation_id && resp.correlation_id !== provisional) {
        const from = provisional;
        const to = resp.correlation_id;
        timeline = timeline.map((t) => {
          if ("correlationId" in t && t.correlationId === from) return { ...t, correlationId: to } as any;
          return t;
        });
        activeCorrelationId = to;
      }
    } catch (e) {
      const msg = e instanceof Error ? e.message : String(e);
      appendAssistant(provisional, "", false, msg);
      activeCorrelationId = null;
    }
  }

  async function cancelActive() {
    if (!selectedConsoleId || !activeCorrelationId) return;
    const cid = activeCorrelationId;
    try {
      await cancelConsole(selectedConsoleId, { correlation_id: cid });
      addSystem(`Cancel sent: ${cid}`);
    } catch (e) {
      addSystem(`Cancel failed: ${e instanceof Error ? e.message : String(e)}`);
    }
  }

  onDestroy(() => {
    if (es) es.close();
  });
</script>

<div class="space-y-4">
  <div class="flex items-center justify-between">
    <h1 class="text-2xl font-bold">Console</h1>

    <div class="flex items-center gap-2">
      <input
        class="h-9 w-72 rounded-md border bg-background px-2 text-sm"
        placeholder="actor:system:overseer"
        bind:value={selectedActorId}
      />

      <button class="rounded-md border px-3 py-1 text-sm hover:bg-accent" on:click={createNewSession}>
        New session
      </button>
    </div>
  </div>

  <div class="grid gap-4 md:grid-cols-[280px_1fr]">
    <!-- Sessions list -->
    <div class="rounded-md border bg-card">
      <div class="border-b p-3 text-sm font-medium">Sessions</div>

      {#if $sessionsQ.isLoading}
        <div class="p-3 text-sm text-muted-foreground">Loading…</div>
      {:else if $sessionsQ.isError}
        <div class="p-3 text-sm text-destructive">{$sessionsQ.error?.message}</div>
      {:else}
        <div class="max-h-[70vh] overflow-auto">
          {#each sessions as s}
            <button
              class="w-full border-b p-3 text-left text-sm hover:bg-accent"
              class:bg-accent={selectedConsoleId === s.console_id}
              on:click={() => selectSession(s.console_id)}
              title={s.console_id}
            >
              <div class="font-mono text-xs">{s.console_id.slice(0, 10)}…</div>
              <div class="text-xs text-muted-foreground">{s.actor_id}</div>
            </button>
          {/each}

          {#if sessions.length === 0}
            <div class="p-3 text-sm text-muted-foreground">No console sessions yet.</div>
          {/if}
        </div>
      {/if}
    </div>

    <!-- Timeline + input -->
    <div class="rounded-md border bg-card flex flex-col min-h-[70vh]">
      <div class="border-b p-3 text-sm font-medium flex items-center justify-between">
        <div>
          {#if selectedConsoleId}
            <span class="font-mono text-xs">{selectedConsoleId}</span>
          {:else}
            <span class="text-muted-foreground">Select or create a session</span>
          {/if}
        </div>

        <div class="flex items-center gap-2">
          {#if activeCorrelationId}
            <span class="text-xs text-muted-foreground font-mono">in-flight: {activeCorrelationId.slice(0, 8)}…</span>
            <button class="rounded-md border px-3 py-1 text-xs hover:bg-accent" on:click={cancelActive}>
              Cancel
            </button>
          {/if}
        </div>
      </div>

      <div class="flex-1 overflow-auto p-4 space-y-3">
        {#each timeline as item (item.ts)}
          {#if item.kind === "system"}
            <div class="text-xs text-muted-foreground">{item.text}</div>

          {:else if item.kind === "user"}
            <div class="ml-auto max-w-[85%] rounded-md border bg-background p-3">
              <div class="text-xs text-muted-foreground mb-1 font-mono">user • {item.correlationId.slice(0, 8)}…</div>
              <pre class="whitespace-pre-wrap text-sm">{item.text}</pre>
            </div>

          {:else if item.kind === "assistant"}
            <div class="mr-auto max-w-[85%] rounded-md border bg-muted/30 p-3">
              <div class="text-xs text-muted-foreground mb-1 font-mono">
                assistant • {item.correlationId.slice(0, 8)}… {item.partial ? "(streaming…)" : ""}
              </div>

              {#if item.error}
                <div class="text-xs text-destructive mb-2">Error: {item.error}</div>
              {/if}

              <pre class="whitespace-pre-wrap text-sm">{item.text}</pre>
            </div>

          {:else if item.kind === "tool"}
            <div class="rounded-md border bg-background p-3">
              <div class="flex items-center justify-between">
                <div class="text-xs font-mono">
                  tool • {item.tool} • {item.phase} • {item.correlationId.slice(0, 8)}…
                </div>

                {#if item.casDigest}
                  <span class="text-xs text-muted-foreground font-mono">CAS: {item.casDigest.slice(0, 14)}…</span>
                {/if}
              </div>

              {#if item.error}
                <div class="text-xs text-destructive mt-2">Error: {item.error}</div>
              {/if}

              <pre class="whitespace-pre-wrap text-xs mt-2 bg-muted/40 p-2 rounded">{item.text}</pre>
            </div>
          {/if}
        {/each}
      </div>

      <div class="border-t p-3 flex gap-2">
        <input
          class="h-10 flex-1 rounded-md border bg-background px-3 text-sm"
          placeholder={selectedConsoleId ? "Type a message…" : "Create/select a session first"}
          bind:value={inputText}
          disabled={!selectedConsoleId || !!activeCorrelationId}
          on:keydown={(e) => {
            if (e.key === "Enter" && (e.metaKey || e.ctrlKey)) send();
          }}
        />

        <button
          class="h-10 rounded-md border px-4 text-sm hover:bg-accent disabled:opacity-50"
          disabled={!selectedConsoleId || !inputText.trim() || !!activeCorrelationId}
          on:click={send}
        >
          Send
        </button>
      </div>
    </div>
  </div>
</div>
```

---

# 4) Backend API contract (what to hand to Codex/Claude)

If you don’t already have console endpoints, implement these:

### 4.1 Routes

* `GET  /api/console/sessions` → `{ sessions: ConsoleSession[] }`
* `POST /api/console/sessions` → creates a console session
* `POST /api/console/sessions/:id/send` → enqueues an ask message to the actor; returns `{ correlation_id }`
* `POST /api/console/sessions/:id/cancel` → enqueues cancel command to the actor; returns `{ ok:true }`
* `GET  /api/console/sessions/:id/events` → **SSE** stream of `ConsolePayload` JSON

### 4.2 Minimal server behavior

* Persist `ConsoleSession` using `internal/storage/console` (you already have it).
* For `send` / `cancel`, write to `internal/storage/mailbox`:

  * `to_ns = <actor_id>` (target actor)
  * `from_ns = actor:human:web` (or similar)
  * `type = console.ask | console.cmd` (or your existing schema)
  * `payload = ConsolePayload{ type:"ask"/"cmd", console_id, correlation_id, content, ... }`

### 4.3 SSE implementation (simple & reliable)

* SSE handler loops:

  * `Poll(ctx, consumerNS, lease, max)` the mailbox store
  * Stream each message payload (already JSON) as `data: <json>\n\n`
  * `Ack(messageID)` after successfully writing to client
* Consumer namespace: pick a stable inbox per console session, e.g.

  * `consumerNS = "actor:console:" + consoleID`
  * and ensure actors reply to that namespace.

---

# 5) Next step after skeleton: toolcall fidelity

Once this is wired end-to-end, upgrade the actor/overseer so it emits:

* `event` with `metadata.tool=<toolName>` + `content=<tool args>` (tool_call)
* `event` with `metadata.tool=<toolName>` + `metadata.cas_digest` or `metadata.error` + `content=<result preview>` (tool_result)
* `reply` with final assistant answer

That gets you 90% of the “Claude Code experience” in your own app.

---

You’re basically **already 70–80% there**: `packages/gui/server/index.js` already implements `/api/consoles/*` + a console-specific SSE channel + mailbox writes.

What’s missing is mostly **schema/shape alignment** + a couple **correctness fixes** so the UI can rely on it like Claude Code.

Below is a **Codex/Claude-ready patch plan** (with concrete code blocks you can paste/replace), plus an exact mapping from the “generic console API” I suggested earlier to your **existing** server structure.

---

## What you already have (and how it works today)

### Storage

* `agents.db` (in `~/.agentctl/storage/`) holds `console_sessions` (created lazily) and `agents`.
* `mailbox.db` holds mailbox messages:

  * UI → actor daemon: `type = "console.ask"` / `"console.cmd"`, `to_ns = actor_id`
  * actor daemon → UI: expected `type = "console.event"` / `"console.reply"` (not guaranteed yet, but your poll loop looks for them).

### HTTP routes that exist today

* `GET  /api/consoles` → `{ consoles: [...], total }`
* `POST /api/consoles` → creates a session and `ensureAgentDaemon(actor_id, workspace, meta)`
* `POST /api/consoles/:id/send` → inserts `console.ask` into `mailbox.db`
* `POST /api/consoles/:id/cancel` → inserts `console.cmd` into `mailbox.db`
* `GET  /api/consoles/:id/events` → SSE connection (no heartbeat right now)
* background poll loop reads `mailbox.db` and calls `broadcastConsoleEvent(consoleId, "console.event"/"console.reply", data)`

---

## Critical issues to fix (so it behaves like a reliable console)

### 1) **SSE shape mismatch**

Your SSE currently sends:

```json
{ "type": "console.reply", "data": { ... }, "ts": 123 }
```

…but the UI scaffolding I gave expects a `ConsolePayload` shape directly.

✅ Fix options:

* **A (minimal change):** Update the Svelte client to parse your wrapper.
* **B (recommended):** Add `?format=payload` to SSE to emit canonical payloads (UI stays clean).

I’ll show **B** below (keeps backward compatibility).

### 2) **Console SSE has no heartbeat**

You add console SSE `res` to `sseClients`, but only `/api/events` installs a heartbeat interval. `/api/consoles/:id/events` does not.

✅ Add a per-connection heartbeat in the console SSE handler.

### 3) **Console mailbox poll can drop messages**

You use a single global `lastConsoleMessageId` and skip `msg.id <= lastConsoleMessageId`. With your `generateId()` this can drop messages created in the same millisecond (lexicographic ordering isn’t guaranteed for “time + random”).

✅ Fix: stop using `lastConsoleMessageId` and instead **ACK/delete** messages after broadcasting (or track a `(ts,id)` cursor per console). Deleting is simplest + prevents mailbox growth.

### 4) **No ACK/delete of delivered console messages**

Mailbox fills forever; you currently only “stream” them.

✅ Fix: delete the mailbox row after you broadcast it to SSE clients.

---

## Concrete patch plan for `packages/gui/server/index.js`

### Patch 0 — Create `console_sessions` table at startup (optional but clean)

Add this to `runMigrations()`:

```js
function runMigrations() {
  // ... existing migrations ...

  const agentsDbPath = join(AGENTCTL_HOME, "storage", "agents.db");
  const adb = new Database(agentsDbPath);
  try {
    adb.exec(`
      CREATE TABLE IF NOT EXISTS console_sessions (
        console_id       TEXT PRIMARY KEY,
        actor_id         TEXT NOT NULL,
        session_id       TEXT,
        workspace        TEXT NOT NULL,
        created_at       TEXT NOT NULL,
        last_attached_at TEXT NOT NULL,
        meta             TEXT
      );
      CREATE INDEX IF NOT EXISTS idx_console_actor ON console_sessions(actor_id);
      CREATE INDEX IF NOT EXISTS idx_console_workspace ON console_sessions(workspace);
      CREATE INDEX IF NOT EXISTS idx_console_session ON console_sessions(session_id);
    `);
    console.log("Migration: ensured console_sessions table exists");
  } catch (err) {
    console.error("Migration: console_sessions error:", err.message);
  } finally {
    adb.close();
  }
}
```

---

### Patch 1 — Make `/send` accept both `{prompt}` and `{content}`, and support client correlation ids

Replace the start of `POST /api/consoles/:id/send` with:

```js
app.post("/api/consoles/:id/send", (req, res) => {
  const consoleId = req.params.id;

  // Accept either prompt (current) or content (new UI)
  const prompt =
    (typeof req.body.prompt === "string" && req.body.prompt) ||
    (typeof req.body.content === "string" && req.body.content) ||
    "";

  const context = (req.body && typeof req.body.context === "object" && req.body.context) || {};

  // Accept correlation_id from client; fallback to generated
  const askId =
    (typeof req.body.ask_id === "string" && req.body.ask_id) ||
    (typeof req.body.correlation_id === "string" && req.body.correlation_id) ||
    generateId();

  if (!prompt) {
    return res.status(400).json({ error: "prompt (or content) is required" });
  }

  // ... keep rest, but use askId (above) instead of generating new askId ...
```

Then update headers/payload to include both names:

```js
const payload = JSON.stringify({
  status: "ok",
  command: "console.ask",
  data: {
    ask_id: askId,
    correlation_id: askId,
    prompt,
    content: prompt,
    context,
    console_id: consoleId,
  }
});

const headers = JSON.stringify({
  correlation: askId,
  ask_id: askId,
  correlation_id: askId,
  console_id: consoleId,
});
```

And update the response to include both:

```js
res.json({
  message_id: messageId,
  ask_id: askId,
  correlation_id: askId,
  status: "sent",
  daemon_status: daemonResult.status || "unknown",
  daemon_error: daemonResult.error || null,
});
```

---

### Patch 2 — Make `/cancel` accept `{correlation_id}` too

In `POST /api/consoles/:id/cancel` change:

```js
const { ask_id } = req.body;
```

to:

```js
const ask_id =
  (typeof req.body.ask_id === "string" && req.body.ask_id) ||
  (typeof req.body.correlation_id === "string" && req.body.correlation_id) ||
  "";
```

And include `correlation_id` in payload + headers:

```js
const payload = JSON.stringify({
  status: "ok",
  command: "console.cmd",
  data: {
    cmd_id: cmdId,
    action: "cancel",
    ask_id,
    correlation_id: ask_id,
    console_id: consoleId,
  }
});

const headers = JSON.stringify({
  ask_id,
  correlation_id: ask_id,
  console_id: consoleId,
});
```

---

### Patch 3 — Add heartbeat to `/api/consoles/:id/events` and support `format=payload`

In `GET /api/consoles/:id/events`, add:

```js
app.get("/api/consoles/:id/events", (req, res) => {
  const consoleId = req.params.id;
  const format = (req.query.format || "").toString(); // "" | "payload"

  // ... existing verification ...

  res.setHeader("Content-Type", "text/event-stream");
  res.setHeader("Cache-Control", "no-cache");
  res.setHeader("Connection", "keep-alive");
  res.setHeader("X-Accel-Buffering", "no");
  res.flushHeaders();

  // Heartbeat every 30s
  const heartbeat = setInterval(() => {
    if (format === "payload") {
      res.write(`data: ${JSON.stringify({
        type: "event",
        actor_id: "server",
        console_id: consoleId,
        correlation_id: "",
        content: "",
        metadata: { mime: "application/json", partial: true },
      })}\n\n`);
    } else {
      res.write(`data: ${JSON.stringify({ type: "heartbeat", console_id: consoleId, ts: Date.now() })}\n\n`);
    }
  }, 30000);

  // Send initial
  if (format === "payload") {
    res.write(`data: ${JSON.stringify({
      type: "event",
      actor_id: "server",
      console_id: consoleId,
      correlation_id: "",
      content: "[connected]",
      metadata: { mime: "text/plain", partial: true },
    })}\n\n`);
  } else {
    res.write(`data: ${JSON.stringify({ type: "connected", console_id: consoleId, ts: Date.now() })}\n\n`);
  }

  // Track client + store preferred format on res object
  res.__console_format = format;

  if (!consoleSSEClients.has(consoleId)) consoleSSEClients.set(consoleId, new Set());
  consoleSSEClients.get(consoleId).add(res);

  req.on("close", () => {
    clearInterval(heartbeat);
    const clients = consoleSSEClients.get(consoleId);
    if (clients) {
      clients.delete(res);
      if (clients.size === 0) consoleSSEClients.delete(consoleId);
    }
  });
});
```

Then update `broadcastConsoleEvent` to emit canonical payloads when a client requested it:

```js
function broadcastConsoleEvent(consoleId, type, data = {}) {
  const clients = consoleSSEClients.get(consoleId);
  if (!clients || clients.size === 0) return;

  for (const client of clients) {
    const format = client.__console_format || "";

    if (format === "payload") {
      // Convert current wrapper -> ConsolePayload-like
      const correlationId = data.correlation_id || data.ask_id || "";
      const actorId = data.from || "actor";

      const payload = {
        type: type === "console.reply" ? "reply" : "event",
        actor_id: actorId,
        console_id: consoleId,
        correlation_id: correlationId,
        content: data.content || data.prompt || data.response || "",
        metadata: {
          partial: type !== "console.reply",
          tool: data.tool_name || data.tool || undefined,
          error: data.error || undefined,
          cas_digest: data.cas_digest || undefined,
        },
      };

      client.write(`data: ${JSON.stringify(payload)}\n\n`);
    } else {
      const event = JSON.stringify({ type, data: { ...data, console_id: consoleId }, ts: Date.now() });
      client.write(`data: ${event}\n\n`);
    }
  }
}
```

This lets your Svelte client just do:

* `new EventSource("/api/consoles/<id>/events?format=payload")`
  …and it receives *exactly* what the UI expects.

---

### Patch 4 — Replace console mailbox poll loop with “consume + delete”

Replace the **entire** current console mailbox polling `setInterval` with:

```js
setInterval(() => {
  if (consoleSSEClients.size === 0) return;

  const mailboxDB = join(AGENTCTL_HOME, "storage", "mailbox.db");
  if (!existsSync(mailboxDB)) return;

  const now = Math.floor(Date.now() / 1000);

  try {
    const db = new Database(mailboxDB);

    // For each active console, pull messages addressed to that console
    for (const [consoleId] of consoleSSEClients) {
      const rows = db.prepare(`
        SELECT id, from_ns, to_ns, type, payload, ts, headers
        FROM mailbox
        WHERE to_ns = ?
          AND visible_at <= ?
          AND type IN ('console.event', 'console.reply')
        ORDER BY ts ASC
        LIMIT 200
      `).all(consoleId, now);

      for (const msg of rows) {
        let env = null;
        let headers = null;

        try { env = msg.payload ? JSON.parse(msg.payload) : null; } catch {}
        try { headers = msg.headers ? JSON.parse(msg.headers) : null; } catch {}

        const data = env?.data || {};
        const eventType = msg.type === "console.reply" ? "console.reply" : "console.event";

        broadcastConsoleEvent(consoleId, eventType, {
          message_id: msg.id,
          from: msg.from_ns,
          ...data,
          // normalize
          ask_id: data.ask_id || headers?.ask_id || headers?.correlation_id || "",
          correlation_id: data.correlation_id || data.ask_id || headers?.correlation_id || headers?.ask_id || "",
        });

        // ACK by deleting to prevent duplicates + DB growth
        db.prepare(`DELETE FROM mailbox WHERE id = ?`).run(msg.id);
      }
    }

    db.close();
  } catch (err) {
    if (Date.now() % 60000 < 500) {
      console.debug("Console mailbox poll error:", err?.message);
    }
  }
}, 250);
```

**Important note:** this assumes the actor daemon sends console events to `to_ns = consoleId`.
If it currently sends them elsewhere, you should update the actor to do this (it’s the correct mailbox semantics anyway).

---

## Mapping: “generic console API” → your existing server

If you stick with `/api/consoles`, here’s the direct mapping:

| UI operation   | Existing route                  | Notes                                          |
| -------------- | ------------------------------- | ---------------------------------------------- |
| list sessions  | `GET /api/consoles`             | returns `{consoles, total}` not `{sessions}`   |
| create session | `POST /api/consoles`            | body `{actor_id, session_id?, meta?}`          |
| connect SSE    | `GET /api/consoles/:id/events`  | add `?format=payload` (Patch 3)                |
| send           | `POST /api/consoles/:id/send`   | accept `{content, correlation_id}` via Patch 1 |
| cancel         | `POST /api/consoles/:id/cancel` | accept `{correlation_id}` via Patch 2          |

So your Svelte UI should set:

* `BASE = "/api/consoles"`
* `EventSource("/api/consoles/<id>/events?format=payload")`

---

## What’s done particularly well already

* You already have `ensureAgentDaemon()` integrated into console creation and send, which is exactly what you want for “Claude Code alternative” UX.
* You’re routing UI→actor via `mailbox.db` (correct integration point with your Go daemon model).
* You already separated global SSE vs console SSE client sets (good architecture).

---

## Next steps (what to hand to Codex/Claude)

1. Apply **Patch 1–4** above (send/cancel compatibility, SSE heartbeat + `format=payload`, mailbox consume+delete).
2. Update the Svelte client to:

   * use `/api/consoles`
   * connect to `/api/consoles/:id/events?format=payload`
   * send `{ content, correlation_id }` (or `{ prompt }` still works)


---

Below is the **actual, end-to-end console message flow** in *your current code*, from SPA → Express → `mailbox.db` → Go daemon → back into `mailbox.db` → Express SSE.

I’ll also call out the **one critical routing mismatch** that currently prevents `console.event` / `console.reply` from reliably reaching the right SSE stream.

---

## 0) The actors involved

### Browser (SPA)

* Calls `/api/consoles/*`
* Opens SSE stream `GET /api/consoles/:id/events`
* Sends user input via `POST /api/consoles/:id/send`

### Express server (`packages/gui/server/index.js`)

* Stores console session metadata in `~/.agentctl/storage/agents.db` → `console_sessions`
* Writes mailbox messages directly into `~/.agentctl/storage/mailbox.db` → `mailbox`
* Polls mailbox for `console.event` and `console.reply` and broadcasts them via SSE

### Go daemon (`agentctl agent run <agentId>`)

* Runs the actor system (`Supervisor` + `AgentActor`)
* Claims messages from mailbox for its namespace (`to_ns = actor_id`)
* Handles `console.ask` and `console.cmd`
* Emits `console.event` and `console.reply` back into mailbox

---

## 1) Console session creation: how the daemon is started

### HTTP: `POST /api/consoles`

**Writes**: `agents.db` → `console_sessions` row

1. Express reads `actor_id` from body.
2. Calls `ensureAgentDaemon(actor_id, workspace, meta)`:

   * Ensures `agents` table exists in `agents.db`
   * Creates an `agents` row if needed
   * Spawns daemon process:

     ```js
     spawn(AGENTCTL_BIN, ["agent", "run", agentId], { detached: true, ... })
     ```
3. Inserts console session row in `console_sessions`:

   * `console_id` = `generateId()`
   * `actor_id` = provided `actor_id`
   * `workspace`, `meta`, etc.

✅ At this point:

* A daemon process should be running for the actor namespace (which is `agents.ns` in `agents.db`)
* The UI has a `console_id` to use

---

## 2) Client connects to SSE: how `/api/consoles/:id/events` works

### HTTP: `GET /api/consoles/:id/events`

**In-memory only**: adds `res` to `consoleSSEClients.get(consoleId)`

* Express verifies the console exists in `agents.db` (`console_sessions`).
* Sets SSE headers and immediately sends:

  ```json
  { "type": "connected", "console_id": "...", "ts": ... }
  ```
* Adds the response object to:

  * `consoleSSEClients.get(consoleId)` (console-specific)
  * `sseClients` (global set) — **but there is no heartbeat interval here**, only on `/api/events`.

So: SSE is “connected”, but no messages will flow until the poll loop finds mailbox rows.

---

## 3) User sends prompt: how `/api/consoles/:id/send` writes to `mailbox.db`

### HTTP: `POST /api/consoles/:id/send`

**Reads**: `agents.db` → `console_sessions`
**Writes**: `mailbox.db` → `mailbox`

1. Express looks up console session:

   ```sql
   SELECT actor_id, session_id, workspace, meta
   FROM console_sessions
   WHERE console_id = ?
   ```

2. Ensures daemon again: `ensureAgentDaemon(consoleSession.actor_id, ...)`

3. Creates an **ask message** and inserts directly into mailbox:

* `from_ns = consoleId`
* `to_ns = actor_id`  ✅ **this is how the daemon receives it**
* `type = "console.ask"`
* `visible_at = nowSeconds` (so it is immediately claimable)
* `ts = nowSeconds`
* `payload` is an **envelope JSON**:

  ```json
  {
    "status": "ok",
    "command": "console.ask",
    "data": {
      "ask_id": "<askId>",
      "prompt": "<prompt>",
      "context": { ... },
      "console_id": "<consoleId>"
    }
  }
  ```
* headers include:

  ```json
  { "correlation": "<askId>", "ask_id": "<askId>", "console_id": "<consoleId>" }
  ```

✅ Result: There is now a mailbox row:

* `to_ns = actor_id`
* `type = console.ask`

---

## 4) How the daemon receives `console.ask`

### Go: `Supervisor` → `MailboxStore.Poll(...)` → `Actor.OnMailReceived(...)`

1. Supervisor wakes up / polls mailbox for the actor namespace (details depend on Watcher + mailbox Poll implementation).

2. It calls:

   ```go
   msg, _ := s.mailbox.Poll(ctx, wake.Namespace, leaseTimeout)
   ```

   where `wake.Namespace` is the actor’s mailbox namespace.

3. The message is delivered into:

   ```go
   actor.OnMailReceived(ctx, msg)
   ```

4. `BaseActor.OnMailReceived` dispatches by `msg.Subject`:

   * For mailbox rows, the `type` column becomes `msg.Subject` (your actor is registering `"console.ask"`, `"console.cmd"` handlers, so this matches).

✅ Therefore the ask hits:

```go
actor.RegisterHandler("console.ask", actor.handleConsoleAsk)
```

---

## 5) `handleConsoleAsk`: execution + streaming events + final reply

### Go: `AgentActor.handleConsoleAsk`

#### 5.1 Parse the ask envelope

```go
var env struct { Data agentdomain.ConsoleAskData `json:"data"` }
json.Unmarshal(msg.Body, &env)
askData := env.Data
```

* Correlation id is derived from headers:

  ```go
  correlID := msg.Headers["correlation"]
  if correlID == "" { correlID = askData.AskID }
  ```

#### 5.2 Cancellation wiring

* Creates `execCtx, cancel := context.WithTimeout(ctx, timeout)`
* Stores `cancel` in `a.cancelFuncs[askID]`
* `console.cmd cancel` later calls that cancel()

#### 5.3 Emit streaming events (`console.event`)

When it calls:

```go
a.emitConsoleEvent(msg.FromNS, askData.AskID, correlID, 1, 0, "progress", "Starting execution...")
```

**Important:** `msg.FromNS` for the ask is the console id, because Express inserted `from_ns = consoleId`.

So the daemon emits events to **ToNS = consoleId**.

`emitConsoleEvent` builds:

* subject `console.event`
* ToNS = `toNS` argument (consoleId)
* FromNS = actor namespace
* Headers = `{ "correlation": correlID, "ask_id": askID }`
* Envelope payload is `envelope.OK("console.event", ConsoleEventData{...})`

Then it calls:

```go
a.sendReply(context.Background(), eventMsg)
```

#### 5.4 How `sendReply` actually writes to mailbox.db

In `Supervisor.Register`, you inject:

```go
setter.SetReplySender(func(ctx context.Context, msg *Message) error {
  ...
  return s.mailbox.Send(ctx, msg)
})
```

So `emitConsoleEvent(...)` → `sendReply` → `mailbox.Send` → inserts mailbox row with:

* `to_ns = consoleId` ✅
* `type = "console.event"`
* payload = envelope JSON

#### 5.5 Final reply (`console.reply`)

At the end, `handleConsoleAsk` returns a message:

```go
return &Message{
  Subject: "console.reply",
  Body: envelope.OK("console.reply", ConsoleReplyData{ AskID, Response, Status, Metrics }),
  Headers: map[string]string{"correlation": correlID},
}
```

Then `BaseActor.OnMailReceived` sends it via `BaseActor.Reply(...)`.

**Key detail**: `BaseActor.Reply(...)` routes replies back to the original sender:

```go
reply.ToNS = original.FromNS // original.FromNS == consoleId
reply.FromNS = actor.Namespace()
```

So the final `console.reply` is also inserted into mailbox with:

* `to_ns = consoleId` ✅
* `type = "console.reply"`

---

## 6) How Express discovers daemon events and pushes them to the browser

### Express poll loop (current code)

Your poll loop currently does:

```sql
SELECT id, from_ns, to_ns, type, payload, ts, headers
FROM mailbox
WHERE type IN ('console.event', 'console.reply')
  AND ts > ?
ORDER BY ts DESC
LIMIT 50
```

Then it tries to determine the console id like this:

* `headers.console_id`, else
* `envelope.data.console_id`

### ⚠️ Critical mismatch: daemon does NOT include `console_id` in headers or payload

From the Go code:

* `ConsoleEventData` does **not** include `ConsoleID`
* `ConsoleReplyData` does **not** include `ConsoleID`
* `emitConsoleEvent` headers do **not** include `console_id`
* `handleConsoleAsk` reply headers only include `correlation`
* `BaseActor.Reply` will **not copy** the original ask headers unless `reply.Headers == nil` (but reply.Headers is non-nil)

✅ The daemon’s **only reliable routing key** is:

* `to_ns == consoleId`

So the Express poller should route using `msg.to_ns`, not `headers.console_id` nor `payload.data.console_id`.

That’s also why I previously recommended switching the poll loop to:

* either query `WHERE to_ns = consoleId`
* or at minimum `consoleId = msg.to_ns`

---

## 7) Cancellation flow (`console.cmd cancel`)

### HTTP: `POST /api/consoles/:id/cancel`

* Writes mailbox row:

  * `from_ns = consoleId`
  * `to_ns = actor_id`
  * `type = "console.cmd"`
  * payload envelope `console.cmd` with `{ action:"cancel", ask_id }`

### Go: `AgentActor.handleConsoleCmd`

* Extract ask_id
* Finds `a.cancelFuncs[askID]` and calls cancel()
* `handleConsoleAsk` sees `context.Canceled` and returns a `console.reply` with `status = "cancelled"`

---

## Routing + storage cheat sheet

### Mailbox messages created by Express (UI → daemon)

| type          | mailbox.to_ns | mailbox.from_ns | payload.command | handler            |
| ------------- | ------------- | --------------- | --------------- | ------------------ |
| `console.ask` | `actor_id`    | `consoleId`     | `"console.ask"` | `handleConsoleAsk` |
| `console.cmd` | `actor_id`    | `consoleId`     | `"console.cmd"` | `handleConsoleCmd` |

### Mailbox messages created by daemon (daemon → UI)

| type            | mailbox.to_ns | mailbox.from_ns | payload.command   | produced by                                            |
| --------------- | ------------- | --------------- | ----------------- | ------------------------------------------------------ |
| `console.event` | `consoleId`   | `actorNS`       | `"console.event"` | `emitConsoleEvent`                                     |
| `console.reply` | `consoleId`   | `actorNS`       | `"console.reply"` | returned from `handleConsoleAsk` (via BaseActor.Reply) |

---

## Practical fixes (so the flow works end-to-end)

You can fix it in either place:

### Option A (recommended): Express routes by `to_ns`

* In the poll loop, set:

  ```js
  const consoleId = msg.to_ns;
  ```
* Or query per-console:

  ```sql
  WHERE to_ns = ? AND type IN (...)
  ```

### Option B: Daemon includes `console_id` in every event/reply

* Add `ConsoleID` field to `ConsoleEventData` / `ConsoleReplyData`, or
* Add header `console_id` in `emitConsoleEvent`, and ensure replies copy it.

Option A is cleaner: mailbox already *is* the router (`to_ns`).

---
