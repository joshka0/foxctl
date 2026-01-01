# agentctl Viewer Applications

agentctl includes TypeScript-based viewer applications for monitoring and
inspecting job execution, tasks, sessions, and other data stored by agentctl.

## Architecture

```
┌─────────────────────────────────────────────────────────────────┐
│                         packages/                                │
├─────────────────┬─────────────────┬─────────────────────────────┤
│   @agentctl/tui │  @agentctl/gui  │       @agentctl/data        │
│   (Terminal UI) │    (Web GUI)    │    (Shared API Client)      │
│    OpenTUI      │  React + Vite   │   Types + Fetch Wrapper     │
└────────┬────────┴────────┬────────┴──────────────┬──────────────┘
         │                 │                       │
         └─────────────────┼───────────────────────┘
                           │
                           ▼
              ┌────────────────────────┐
              │      API Server        │
              │  packages/gui/server/  │
              │   Express (port 8090)  │
              └───────────┬────────────┘
                          │
                          ▼
              ┌────────────────────────┐
              │   agentctl SQLite      │
              │  ~/.agentctl/storage/  │
              └────────────────────────┘
```

## Packages

### @agentctl/data

Shared TypeScript package containing:

- **API client**: Fetch wrapper with error handling
- **Types**: TypeScript interfaces for all agentctl data structures
- **Hooks**: React Query hooks for data fetching (used by both GUI and TUI)

### @agentctl/gui

Web-based dashboard built with:

- **React 19** + **Vite**
- **Tailwind CSS** for styling
- **TanStack Query** for data fetching
- **React Router** for navigation

Also includes the **API server** (`server/index.js`):

- Express.js on port 8090
- Reads directly from agentctl SQLite databases
- Provides REST endpoints for all data types

### @agentctl/tui

Terminal UI built with:

- **OpenTUI** (@opentui/react, @opentui/core)
- **React** (same component model as GUI)
- **Bun** runtime

## Quick Start

```bash
# From repository root
bun install

# Start API server + Web GUI (recommended)
make ts-dev-gui

# Or use npm scripts
bun run dev:all      # Server + GUI concurrent
bun run dev:server   # Server only (port 8090)
bun run dev:gui      # GUI only (http://localhost:5173)

# Terminal UI (requires server running)
AGENTCTL_API_URL=http://localhost:8090 bun run dev:tui
```

## TUI Views

The terminal UI provides 9 views accessible via number keys:

| Key | View         | Description                                    |
| --- | ------------ | ---------------------------------------------- |
| 1   | Jobs         | Job queue with status, timing, command         |
| 2   | Tasks        | Task list with dependencies and status         |
| 3   | Insights     | Task graph analysis (PageRank, critical path)  |
| 4   | Mailbox      | Actor messages with priority indicators        |
| 5   | Reservations | File locks (exclusive/shared mode)             |
| 6   | Stats        | Job statistics dashboard                       |
| 7   | Blackboard   | Key-value store browser by namespace           |
| 8   | SQLite       | Direct SQL query interface                     |
| 9   | Search       | Full-text search across data                   |

### Keyboard Shortcuts

**Global:**

- `1-9`: Switch to view
- `[` / `]`: Previous / next view
- `Tab`: Next view
- `q`: Quit (Ctrl+Q in search view)

**List Navigation:**

- `j` / `k`: Move down / up
- `g` / `G`: Go to top / bottom
- `r`: Refresh data

**View-specific:**

- Mailbox: `a` cycles actor filter (admin, overseer, engineer, tester)
- Blackboard: `n` cycles namespace filter
- SQLite: `h` / `l` switches panes, `Enter` executes query

## API Endpoints

The API server exposes these endpoints:

| Endpoint                | Description              |
| ----------------------- | ------------------------ |
| `GET /api/jobs`         | List all jobs            |
| `GET /api/tasks`        | List all tasks           |
| `GET /api/stats`        | Job statistics           |
| `GET /api/insights`     | Task graph insights      |
| `GET /api/mailbox`      | Actor messages           |
| `GET /api/reservations` | File reservations        |
| `GET /api/blackboard`   | Key-value records        |
| `GET /api/sqlite/query` | Execute SQL query        |
| `GET /api/search`       | Full-text search         |
| `GET /api/sessions/*`   | Session management       |
| `GET /api/codemaps/*`   | Code map data            |

## Development

### Adding a New View

1. Create view component in `packages/tui/src/views/NewView.tsx`
2. Add data hook in `packages/tui/src/hooks/useData.ts`
3. Export from `packages/tui/src/views/index.ts`
4. Add to App.tsx view type, ALL_VIEWS array, Header, and render switch

### TypeScript Development

```bash
# Type check all packages
bun run typecheck

# Build all packages
bun run build

# Lint
cd packages/gui && bun run lint
```

### Makefile Targets

```makefile
ts-install      # Install bun dependencies
ts-dev-tui      # Start TUI development
ts-dev-gui      # Start GUI + server development
ts-build        # Build all TypeScript packages
ts-typecheck    # Type check all packages
```

## Environment Variables

| Variable           | Default                 | Description                |
| ------------------ | ----------------------- | -------------------------- |
| `AGENTCTL_API_URL` | `http://localhost:8090` | API server URL for TUI     |
| `PORT`             | `8090`                  | API server port            |
| `AGENTCTL_HOME`    | `~/.agentctl`           | agentctl storage directory |

## Migration Notes

The TUI was migrated from Go/Bubble Tea to TypeScript/OpenTUI in December 2025.
Key changes:

- **Same functionality**: All 9 views preserved with identical keyboard shortcuts
- **Shared data layer**: GUI and TUI now share `@agentctl/data` package
- **Unified API server**: Single Express server serves both applications
- **Monorepo structure**: Bun workspaces in `packages/` directory
