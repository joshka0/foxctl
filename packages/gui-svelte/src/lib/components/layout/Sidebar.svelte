<script lang="ts">
  import { link } from "svelte-spa-router";
  import { location } from "svelte-spa-router";
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
    Map as MapIcon,
    Bot,
    Terminal,
  } from "lucide-svelte";

  const navigation = [
    { name: "Jobs", href: "/jobs", icon: Briefcase },
    { name: "Tasks", href: "/tasks", icon: ListTodo },
    { name: "Sessions", href: "/sessions", icon: MessageSquare },
    { name: "Console", href: "/console", icon: Terminal },
    { name: "Agents", href: "/agents", icon: Bot },
    { name: "Codemaps", href: "/codemaps", icon: MapIcon },
    { name: "Stats", href: "/stats", icon: BarChart3 },
    { name: "Insights", href: "/insights", icon: Lightbulb },
    { name: "Mailbox", href: "/mailbox", icon: Mail },
    { name: "Reservations", href: "/reservations", icon: Lock },
    { name: "Blackboard", href: "/blackboard", icon: Clipboard },
    { name: "SQLite", href: "/sqlite", icon: Database },
    { name: "Search", href: "/search", icon: Search },
  ];

  function isActive(path: string, currentLocation: string): boolean {
    if (path === "/") return currentLocation === "/" || currentLocation === "/jobs";
    return currentLocation.startsWith(path);
  }
</script>

<div class="flex h-full w-64 flex-col bg-card border-r">
  <!-- Logo -->
  <div class="flex h-16 items-center border-b px-6">
    <Settings class="h-6 w-6 mr-2 text-primary" />
    <span class="text-lg font-semibold">agentctl</span>
  </div>

  <!-- Navigation -->
  <nav class="flex-1 space-y-1 px-3 py-4">
    {#each navigation as item}
      <a
        href={item.href}
        use:link
        class="flex items-center gap-3 rounded-lg px-3 py-2 text-sm font-medium transition-colors
          {isActive(item.href, $location)
            ? 'bg-primary text-primary-foreground'
            : 'text-muted-foreground hover:bg-accent hover:text-accent-foreground'}"
      >
        <svelte:component this={item.icon} class="h-4 w-4" />
        {item.name}
      </a>
    {/each}
  </nav>

  <!-- Footer -->
  <div class="border-t p-4">
    <p class="text-xs text-muted-foreground">
      agentctl svelte v0.1.0
    </p>
  </div>
</div>
