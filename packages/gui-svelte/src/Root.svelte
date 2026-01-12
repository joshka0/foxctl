<script lang="ts">
  import { QueryClient, QueryClientProvider } from "@tanstack/svelte-query";
  import App from "./App.svelte";

  const queryClient = new QueryClient({
    defaultOptions: {
      queries: {
        staleTime: 1000 * 30, // 30 seconds
        refetchOnWindowFocus: true,
      },
    },
  });
</script>

<QueryClientProvider client={queryClient}>
  <App />
</QueryClientProvider>
