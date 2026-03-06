import { useState } from "react";
import { useKeyboard } from "@opentui/react";
import {
  JobsView,
  TasksView,
  InsightsView,
  MailboxView,
  ReservationsView,
  StatsView,
  BlackboardView,
  SQLiteView,
  SearchView,
  CASView,
  MemoryView,
  SessionsView,
  AgentView,
  TrajectoryView,
  OrchestrationView,
  ConsoleView,
} from "./views";

// View types for navigation
type View =
  | "cas"
  | "jobs"
  | "tasks"
  | "insights"
  | "mailbox"
  | "reservations"
  | "stats"
  | "blackboard"
  | "sqlite"
  | "search"
  | "agents"
  | "trajectory"
  | "memory"
  | "sessions"
  | "orchestration"
  | "console";

// Category definitions
type Category = "core" | "data" | "history" | "multi-agent" | "find";

interface ViewDef {
  key: View;
  label: string;
  shortcut: string;
}

interface CategoryDef {
  key: Category;
  label: string;
  shortcut: string;
  color: string;
  views: ViewDef[];
}

const CATEGORIES: CategoryDef[] = [
  {
    key: "core",
    label: "Core",
    shortcut: "c",
    color: "#00ff00",
    views: [
      { key: "jobs", label: "Jobs", shortcut: "1" },
      { key: "tasks", label: "Tasks", shortcut: "2" },
      { key: "stats", label: "Stats", shortcut: "3" },
      { key: "insights", label: "Insights", shortcut: "4" },
    ],
  },
  {
    key: "data",
    label: "Data",
    shortcut: "d",
    color: "#00aaff",
    views: [
      { key: "cas", label: "CAS", shortcut: "1" },
      { key: "sqlite", label: "SQL", shortcut: "2" },
      { key: "blackboard", label: "Board", shortcut: "3" },
    ],
  },
  {
    key: "history",
    label: "History",
    shortcut: "h",
    color: "#aa77ff",
    views: [
      { key: "sessions", label: "Sessions", shortcut: "1" },
      { key: "memory", label: "Memory", shortcut: "2" },
      { key: "trajectory", label: "Trajectory", shortcut: "3" },
    ],
  },
  {
    key: "multi-agent",
    label: "Multi-Agent",
    shortcut: "a",
    color: "#ffaa00",
    views: [
      { key: "agents", label: "Agents", shortcut: "1" },
      { key: "orchestration", label: "Orchestration", shortcut: "2" },
      { key: "mailbox", label: "Mailbox", shortcut: "3" },
      { key: "console", label: "Console", shortcut: "4" },
    ],
  },
  {
    key: "find",
    label: "Find",
    shortcut: "f",
    color: "#ff77ff",
    views: [
      { key: "search", label: "Search", shortcut: "1" },
      { key: "reservations", label: "Locks", shortcut: "2" },
    ],
  },
];

// Helper to find category for a view
function getCategoryForView(view: View): CategoryDef {
  for (const cat of CATEGORIES) {
    if (cat.views.some((v) => v.key === view)) {
      return cat;
    }
  }
  if (process.env.NODE_ENV === "development") {
    console.warn(`View "${view}" not found in any category, falling back to core`);
  }
  return CATEGORIES[0]; // fallback to core
}

// Get all views in order for [/] navigation
const ALL_VIEWS: View[] = CATEGORIES.flatMap((cat) => cat.views.map((v) => v.key));

interface HeaderProps {
  currentView: View;
  activeCategory: Category;
}

function Header({ currentView, activeCategory }: HeaderProps) {
  const currentCat = getCategoryForView(currentView);

  return (
    <box height={2} flexDirection="column">
      {/* Row 1: Categories */}
      <box height={1} flexDirection="row" justifyContent="space-between">
        <text fg="#00ff00"><b>agentctl</b></text>
        <box flexDirection="row">
          {CATEGORIES.map((cat) => {
            const isActive = activeCategory === cat.key;
            const isCurrent = currentCat.key === cat.key;
            const label = ` [${cat.shortcut}]${cat.label}`;
            if (isActive) {
              return <text key={cat.key} fg={cat.color}><b>{label}</b></text>;
            } else if (isCurrent) {
              return <text key={cat.key} fg={cat.color}>{label}</text>;
            } else {
              return <text key={cat.key} fg="#555555">{label}</text>;
            }
          })}
        </box>
      </box>
      {/* Row 2: Views in active category */}
      <box height={1} flexDirection="row" justifyContent="space-between">
        <text fg="#666666">{" "}</text>
        <box flexDirection="row">
          {CATEGORIES.find((c) => c.key === activeCategory)?.views.map((v) => {
            const isActive = currentView === v.key;
            const label = ` [${v.shortcut}]${v.label}`;
            const catColor = CATEGORIES.find((c) => c.key === activeCategory)?.color || "#888888";
            if (isActive) {
              return <text key={v.key} fg={catColor}><b>{label}</b></text>;
            } else {
              return <text key={v.key} fg="#666666">{label}</text>;
            }
          })}
        </box>
      </box>
    </box>
  );
}

interface StatusBarProps {
  currentView: View;
  activeCategory: Category;
}

function StatusBar({ currentView, activeCategory }: StatusBarProps) {
  // View-specific hints
  const hints: Record<View, string> = {
    cas: "j/k: navigate | h/l: page content | r: refresh",
    jobs: "j/k: navigate | r: refresh",
    tasks: "j/k: navigate | r: refresh",
    insights: "j/k: navigate | r: refresh",
    mailbox: "j/k: navigate | r: refresh",
    reservations: "j/k: navigate | r: refresh",
    stats: "r: refresh",
    blackboard: "j/k: navigate | n: cycle ns | r: refresh",
    sqlite: "h/l: panes | j/k: navigate | enter: select",
    search: "type to search | enter: submit | /: edit | esc: clear/exit",
    agents: "j/k: navigate | r: refresh",
    trajectory: "j/k: navigate | f: cycle status | h/l: events | r: refresh",
    memory: "j/k: navigate | t: cycle type | /: search | r: refresh",
    sessions: "j/k: navigate | enter: view turns | r: refresh",
    orchestration: "h/l/tab: panels | j/k: navigate | enter: detail | d: dispatch/delegate | t/u/m: card actions | w/W: workspace | r: refresh",
    console: "i: input | 1-5: rate | Esc: back",
  };

  const catColor = CATEGORIES.find((c) => c.key === activeCategory)?.color || "#888888";

  return (
    <box height={1} backgroundColor="#333333">
      <text fg="#888888">
        {" "}
        <span fg={catColor}>[c/d/h/a/f]</span>
        {" category | "}
        <span fg={catColor}>[1-4]</span>
        {" view | [/]: prev/next | q: quit | "}
        {hints[currentView]}
      </text>
    </box>
  );
}

export function App() {
  const [view, setView] = useState<View>("jobs");
  const [activeCategory, setActiveCategory] = useState<Category>("core");

  // Global keyboard handling for view switching and quit
  useKeyboard((e) => {
    if (e.ctrl && e.name === "c") {
      const shutdown = (globalThis as { __agentctl_tui_shutdown?: (code?: number) => void })
        .__agentctl_tui_shutdown;
      if (shutdown) {
        shutdown(0);
      } else {
        process.exit(0);
      }
      return;
    }
    // Don't intercept keys when in search or console input mode
    // These views handle their own input
    // Use raw character (single char that isn't a control key)
    const isChar = e.raw.length === 1 && !e.ctrl && !e.meta;
    if ((view === "search" || view === "console") && isChar) {
      return;
    }

    // Category shortcuts
    const categoryByShortcut: Record<string, Category> = {
      c: "core",
      d: "data",
      h: "history",
      a: "multi-agent",
      f: "find",
    };

    // Check if it's a category switch
    if (categoryByShortcut[e.name]) {
      const newCat = categoryByShortcut[e.name];
      setActiveCategory(newCat);
      // Also switch to first view in that category
      const catDef = CATEGORIES.find((c) => c.key === newCat);
      if (catDef && catDef.views.length > 0) {
        setView(catDef.views[0].key);
      }
      return;
    }

    // Number keys select views within current category
    if (e.name >= "1" && e.name <= "9") {
      const idx = parseInt(e.name) - 1;
      const catDef = CATEGORIES.find((c) => c.key === activeCategory);
      if (catDef && catDef.views[idx]) {
        setView(catDef.views[idx].key);
      }
      return;
    }

    switch (e.name) {
      case "q": {
        const shutdown = (globalThis as { __agentctl_tui_shutdown?: (code?: number) => void })
          .__agentctl_tui_shutdown;
        const canExit = view !== "search" && view !== "console";
        if (e.ctrl || canExit) {
          setTimeout(() => {
            if (shutdown) {
              shutdown(0);
            } else {
              process.exit(0);
            }
          }, 50);
        }
        break;
      }
      case "[":
        // Previous view (across all categories)
        setView((v) => {
          const idx = ALL_VIEWS.indexOf(v);
          const newView = ALL_VIEWS[(idx - 1 + ALL_VIEWS.length) % ALL_VIEWS.length];
          setActiveCategory(getCategoryForView(newView).key);
          return newView;
        });
        break;
      case "]":
      case "tab":
        // Next view (across all categories)
        setView((v) => {
          const idx = ALL_VIEWS.indexOf(v);
          const newView = ALL_VIEWS[(idx + 1) % ALL_VIEWS.length];
          setActiveCategory(getCategoryForView(newView).key);
          return newView;
        });
        break;
    }
  });

  return (
    <box flexDirection="column" width="100%" height="100%">
      <Header currentView={view} activeCategory={activeCategory} />
      <box flexGrow={1} borderStyle="single" borderColor="#444444">
        {view === "cas" && <CASView />}
        {view === "jobs" && <JobsView />}
        {view === "tasks" && <TasksView />}
        {view === "insights" && <InsightsView />}
        {view === "mailbox" && <MailboxView />}
        {view === "reservations" && <ReservationsView />}
        {view === "stats" && <StatsView />}
        {view === "blackboard" && <BlackboardView />}
        {view === "sqlite" && <SQLiteView />}
        {view === "search" && <SearchView onExit={() => setView("jobs")} />}
        {view === "agents" && <AgentView />}
        {view === "trajectory" && <TrajectoryView />}
        {view === "memory" && <MemoryView />}
        {view === "sessions" && <SessionsView />}
        {view === "orchestration" && <OrchestrationView />}
        {view === "console" && <ConsoleView onExit={() => setView("jobs")} />}
      </box>
      <StatusBar currentView={view} activeCategory={activeCategory} />
    </box>
  );
}
