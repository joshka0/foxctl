<script lang="ts">
  import { Moon, Sun } from "lucide-svelte";

  let isDark = typeof window !== "undefined" && document.documentElement.classList.contains("dark");

  function toggleTheme() {
    isDark = !isDark;
    if (isDark) {
      document.documentElement.classList.add("dark");
      localStorage.setItem("theme", "dark");
    } else {
      document.documentElement.classList.remove("dark");
      localStorage.setItem("theme", "light");
    }
  }
</script>

<header class="flex h-16 items-center justify-between border-b bg-card px-6">
  <div class="flex items-center gap-4">
    <!-- Workspace selector placeholder -->
    <span class="text-sm text-muted-foreground">Workspace: default</span>
  </div>

  <div class="flex items-center gap-4">
    <!-- Theme toggle -->
    <button
      on:click={toggleTheme}
      class="rounded-md p-2 hover:bg-accent transition-colors"
      title={isDark ? "Switch to light mode" : "Switch to dark mode"}
    >
      {#if isDark}
        <Sun class="h-5 w-5" />
      {:else}
        <Moon class="h-5 w-5" />
      {/if}
    </button>
  </div>
</header>
