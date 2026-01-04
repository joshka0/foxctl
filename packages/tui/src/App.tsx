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

// View types for navigation (matching Go viewer)
// 0=CAS, 1=Jobs, 2=Tasks, 3=Insights, 4=Mailbox, 5=Reservations, 6=Stats, 7=Blackboard, 8=SQLite, 9=Search, a=Agents, t=Trajectory, m=Memory, s=Sessions, o=Orchestration
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

const ALL_VIEWS: View[] = [
  "cas",
  "jobs",
  "tasks",
  "insights",
  "mailbox",
  "reservations",
  "stats",
  "blackboard",
  "sqlite",
  "search",
  "agents",
  "trajectory",
  "memory",
  "sessions",
  "orchestration",
  "console",
];

interface HeaderProps {
  currentView: View;
}

function Header({ currentView }: HeaderProps) {
  const views: { key: View; label: string; shortcut: string }[] = [
    { key: "cas", label: "CAS", shortcut: "0" },
    { key: "jobs", label: "Jobs", shortcut: "1" },
    { key: "tasks", label: "Tasks", shortcut: "2" },
    { key: "insights", label: "Insights", shortcut: "3" },
    { key: "mailbox", label: "Mail", shortcut: "4" },
    { key: "reservations", label: "Locks", shortcut: "5" },
    { key: "stats", label: "Stats", shortcut: "6" },
    { key: "blackboard", label: "BB", shortcut: "7" },
    { key: "sqlite", label: "SQL", shortcut: "8" },
    { key: "search", label: "Search", shortcut: "9" },
    { key: "agents", label: "Agent", shortcut: "a" },
    { key: "trajectory", label: "Traj", shortcut: "t" },
    { key: "memory", label: "Mem", shortcut: "m" },
    { key: "sessions", label: "Sess", shortcut: "s" },
    { key: "orchestration", label: "Orch", shortcut: "o" },
    { key: "console", label: "Con", shortcut: "c" },
  ];

  return (
    <box height={1} flexDirection="row" justifyContent="space-between">
      <text fg="#00ff00">
        <b>agentctl-viewer</b>
      </text>
      <box flexDirection="row">
        {views.map((v) => (
          <text key={v.key} fg={currentView === v.key ? "#00ff00" : "#666666"}>
            {currentView === v.key ? (
              <b>
                {" "}
                [{v.shortcut}]{v.label}
              </b>
            ) : (
              <>
                {" "}
                [{v.shortcut}]{v.label}
              </>
            )}
          </text>
        ))}
      </box>
    </box>
  );
}

interface StatusBarProps {
  currentView: View;
}

function StatusBar({ currentView }: StatusBarProps) {
  // View-specific hints
  const hints: Record<View, string> = {
    cas: "j/k: navigate | h/l: page content | r: refresh",
    jobs: "j/k: navigate | r: refresh",
    tasks: "j/k: navigate | r: refresh",
    insights: "j/k: navigate | r: refresh",
    mailbox: "j/k: navigate | a: cycle actor | r: refresh",
    reservations: "j/k: navigate | r: refresh",
    stats: "r: refresh",
    blackboard: "j/k: navigate | n: cycle ns | r: refresh",
    sqlite: "h/l: panes | j/k: navigate | enter: select",
    search: "type to search | enter: submit | /: edit | esc: clear/exit",
    agents: "j/k: navigate | f: cycle state | h/l: events | r: refresh",
    trajectory: "j/k: navigate | f: cycle status | h/l: events | r: refresh",
    memory: "j/k: navigate | t: cycle type | /: search | a: add | r: refresh",
    sessions: "j/k: navigate | r: refresh",
    orchestration: "h/l/tab: panels | j/k: navigate | a: answer | d: delegate | r: refresh",
    console: "i: input | 1-5: rate | f: feedback | c: cancel | Esc: back",
  };

  return (
    <box height={1} backgroundColor="#333333">
      <text fg="#888888">
        {" "}
        0-9: views | [/]: prev/next | q: quit | {hints[currentView]}
      </text>
    </box>
  );
}

export function App() {
  const [view, setView] = useState<View>("jobs");

  // Global keyboard handling for view switching and quit
  useKeyboard((e) => {
    // Don't intercept keys when in search or console input mode
    // These views handle their own input
    // Use raw character (single char that isn't a control key)
    const isChar = e.raw.length === 1 && !e.ctrl && !e.meta;
    if ((view === "search" || view === "console") && isChar) {
      return;
    }

    switch (e.name) {
      case "0":
        setView("cas");
        break;
      case "1":
        setView("jobs");
        break;
      case "2":
        setView("tasks");
        break;
      case "3":
        setView("insights");
        break;
      case "4":
        setView("mailbox");
        break;
      case "5":
        setView("reservations");
        break;
      case "6":
        setView("stats");
        break;
      case "7":
        setView("blackboard");
        break;
      case "8":
        setView("sqlite");
        break;
      case "9":
        setView("search");
        break;
      case "a":
        setView("agents");
        break;
      case "t":
        setView("trajectory");
        break;
      case "m":
        setView("memory");
        break;
      case "s":
        setView("sessions");
        break;
      case "o":
        setView("orchestration");
        break;
      case "c":
        setView("console");
        break;
      case "q":
        if (e.ctrl) {
          process.exit(0);
        }
        // Regular q only quits from non-input views
        if (view !== "search" && view !== "console") {
          process.exit(0);
        }
        break;
      case "[":
        // Previous view
        setView((v) => {
          const idx = ALL_VIEWS.indexOf(v);
          return ALL_VIEWS[(idx - 1 + ALL_VIEWS.length) % ALL_VIEWS.length];
        });
        break;
      case "]":
      case "tab":
        // Next view
        setView((v) => {
          const idx = ALL_VIEWS.indexOf(v);
          return ALL_VIEWS[(idx + 1) % ALL_VIEWS.length];
        });
        break;
    }
  });

  return (
    <box flexDirection="column" width="100%" height="100%">
      <Header currentView={view} />
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
      <StatusBar currentView={view} />
    </box>
  );
}
