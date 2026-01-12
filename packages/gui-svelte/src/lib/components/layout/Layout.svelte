<script lang="ts">
  import { onDestroy, onMount } from "svelte";
  import { startSSE } from "@/lib/api/sse";
  import Sidebar from "./Sidebar.svelte";
  import Header from "./Header.svelte";

  let stopSSE: (() => void) | null = null;

  onMount(() => {
    stopSSE = startSSE();
  });

  onDestroy(() => {
    if (stopSSE) {
      stopSSE();
    }
  });
</script>

<div class="flex h-screen bg-background">
  <Sidebar />
  <div class="flex flex-1 flex-col overflow-hidden">
    <Header />
    <main class="flex-1 overflow-auto p-6">
      <slot />
    </main>
  </div>
</div>
