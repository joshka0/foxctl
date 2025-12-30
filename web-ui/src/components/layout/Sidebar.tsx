import { NavLink } from "react-router-dom";
import { cn } from "@/lib/utils";
import {
  Briefcase,
  ListTodo,
  BarChart3,
  Lightbulb,
  Mail,
  Lock,
  Clipboard,
  Database,
  Search,
  Settings,
  MessageSquare,
} from "lucide-react";

const navigation = [
  { name: "Jobs", href: "/jobs", icon: Briefcase },
  { name: "Tasks", href: "/tasks", icon: ListTodo },
  { name: "Sessions", href: "/sessions", icon: MessageSquare },
  { name: "Stats", href: "/stats", icon: BarChart3 },
  { name: "Insights", href: "/insights", icon: Lightbulb },
  { name: "Mailbox", href: "/mailbox", icon: Mail },
  { name: "Reservations", href: "/reservations", icon: Lock },
  { name: "Blackboard", href: "/blackboard", icon: Clipboard },
  { name: "SQLite", href: "/sqlite", icon: Database },
  { name: "Search", href: "/search", icon: Search },
];

export function Sidebar() {
  return (
    <div className="flex h-full w-64 flex-col bg-card border-r">
      {/* Logo */}
      <div className="flex h-16 items-center border-b px-6">
        <Settings className="h-6 w-6 mr-2 text-primary" />
        <span className="text-lg font-semibold">agentctl</span>
      </div>

      {/* Navigation */}
      <nav className="flex-1 space-y-1 px-3 py-4">
        {navigation.map((item) => (
          <NavLink
            key={item.name}
            to={item.href}
            className={({ isActive }) =>
              cn(
                "flex items-center gap-3 rounded-lg px-3 py-2 text-sm font-medium transition-colors",
                isActive
                  ? "bg-primary text-primary-foreground"
                  : "text-muted-foreground hover:bg-accent hover:text-accent-foreground"
              )
            }
          >
            <item.icon className="h-4 w-4" />
            {item.name}
          </NavLink>
        ))}
      </nav>

      {/* Footer */}
      <div className="border-t p-4">
        <p className="text-xs text-muted-foreground">
          agentctl web v0.1.0
        </p>
      </div>
    </div>
  );
}
