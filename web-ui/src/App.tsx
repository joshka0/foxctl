import { BrowserRouter, Routes, Route, Navigate } from "react-router-dom";
import { Layout } from "@/components/layout";
import {
  JobsPage,
  JobDetailPage,
  TasksPage,
  TaskDetailPage,
  StatsPage,
  InsightsPage,
  SQLitePage,
  SearchPage,
  MailboxPage,
  SessionsPage,
  CodemapsPage,
} from "@/pages";

function App() {
  return (
    <BrowserRouter>
      <Routes>
        <Route path="/" element={<Layout />}>
          <Route index element={<Navigate to="/jobs" replace />} />
          <Route path="jobs" element={<JobsPage />} />
          <Route path="jobs/:id" element={<JobDetailPage />} />
          <Route path="tasks" element={<TasksPage />} />
          <Route path="tasks/:id" element={<TaskDetailPage />} />
          <Route path="stats" element={<StatsPage />} />
          <Route path="insights" element={<InsightsPage />} />
          <Route path="mailbox" element={<MailboxPage />} />
          <Route path="sessions" element={<SessionsPage />} />
          <Route path="reservations" element={<PlaceholderPage title="Reservations" />} />
          <Route path="blackboard" element={<PlaceholderPage title="Blackboard" />} />
          <Route path="sqlite" element={<SQLitePage />} />
          <Route path="search" element={<SearchPage />} />
          <Route path="codemaps" element={<CodemapsPage />} />
        </Route>
      </Routes>
    </BrowserRouter>
  );
}

// Placeholder for pages not yet implemented
function PlaceholderPage({ title }: { title: string }) {
  return (
    <div className="flex items-center justify-center h-64">
      <p className="text-muted-foreground">{title} page coming soon...</p>
    </div>
  );
}

export default App;
