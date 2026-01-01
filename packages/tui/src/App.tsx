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
} from "./views";

// View types for navigation (matching Go viewer)
// 1=Jobs, 2=Tasks, 3=Insights, 4=Mailbox, 5=Reservations, 6=Stats, 7=Blackboard, 8=SQLite, 9=Search
type View =
  | "jobs"
  | "tasks"
  | "insights"
  | "mailbox"
  | "reservations"
  | "stats"
  | "blackboard"
  | "sqlite"
  | "search";

const ALL_VIEWS: View[] = [
  "jobs",
  "tasks",
  "insights",
  "mailbox",
  "reservations",
  "stats",
  "blackboard",
  "sqlite",
  "search",
];

interface HeaderProps {
  currentView: View;
}

function Header({ currentView }: HeaderProps) {
  const views: { key: View; label: string; shortcut: string }[] = [
    { key: "jobs", label: "Jobs", shortcut: "1" },
    { key: "tasks", label: "Tasks", shortcut: "2" },
    { key: "insights", label: "Insights", shortcut: "3" },
    { key: "mailbox", label: "Mail", shortcut: "4" },
    { key: "reservations", label: "Locks", shortcut: "5" },
    { key: "stats", label: "Stats", shortcut: "6" },
    { key: "blackboard", label: "BB", shortcut: "7" },
    { key: "sqlite", label: "SQL", shortcut: "8" },
    { key: "search", label: "Search", shortcut: "9" },
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
    jobs: "j/k: navigate | r: refresh",
    tasks: "j/k: navigate | r: refresh",
    insights: "j/k: navigate | r: refresh",
    mailbox: "j/k: navigate | a: cycle actor | r: refresh",
    reservations: "j/k: navigate | r: refresh",
    stats: "r: refresh",
    blackboard: "j/k: navigate | n: cycle ns | r: refresh",
    sqlite: "h/l: panes | j/k: navigate | enter: select",
    search: "type to search | enter: submit | /: edit",
  };

  return (
    <box height={1} backgroundColor="#333333">
      <text fg="#888888">
        {" "}
        1-9: views | [/]: prev/next | q: quit | {hints[currentView]}
      </text>
    </box>
  );
}

export function App() {
  const [view, setView] = useState<View>("jobs");

  // Global keyboard handling for view switching and quit
  useKeyboard((e) => {
    // Don't intercept keys when in search input mode
    // The search view handles its own input
    // Use raw character (single char that isn't a control key)
    const isChar = e.raw.length === 1 && !e.ctrl && !e.meta;
    if (view === "search" && isChar) {
      return;
    }

    switch (e.name) {
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
      case "q":
        if (e.ctrl) {
          process.exit(0);
        }
        // Regular q only quits from non-search views
        if (view !== "search") {
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
        {view === "jobs" && <JobsView />}
        {view === "tasks" && <TasksView />}
        {view === "insights" && <InsightsView />}
        {view === "mailbox" && <MailboxView />}
        {view === "reservations" && <ReservationsView />}
        {view === "stats" && <StatsView />}
        {view === "blackboard" && <BlackboardView />}
        {view === "sqlite" && <SQLiteView />}
        {view === "search" && <SearchView />}
      </box>
      <StatusBar currentView={view} />
    </box>
  );
}
