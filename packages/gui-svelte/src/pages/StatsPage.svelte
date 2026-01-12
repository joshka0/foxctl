<script lang="ts">
  import { RefreshCw, TrendingUp, Clock, CheckCircle2, XCircle } from "lucide-svelte";
  import Layout from "@/lib/components/layout/Layout.svelte";
  import { useStats } from "@/lib/api/hooks";
  import { Card, CardContent, CardHeader, CardTitle } from "@/lib/components/ui";

  const statsQuery = useStats();
  $: stats = $statsQuery.data;

  const stateOrder = ["completed", "running", "pending", "failed"];

  $: sortedStates = Object.entries(stats?.by_state || {}).sort(
    ([a], [b]) => stateOrder.indexOf(a) - stateOrder.indexOf(b)
  );

  $: topCommands = Object.entries(stats?.by_command || {})
    .sort(([, a], [, b]) => (b as number) - (a as number))
    .slice(0, 10);

  const stateColors: Record<string, string> = {
    completed: "bg-green-500",
    running: "bg-blue-500",
    pending: "bg-yellow-500",
    failed: "bg-red-500",
  };

  function getPercentage(count: number): number {
    if (!stats?.total) return 0;
    return Math.round((count / stats.total) * 100);
  }

  function handleRefresh() {
    $statsQuery.refetch();
  }
</script>

<Layout>
  <div class="space-y-6">
    <div class="flex items-center justify-between">
      <h1 class="text-2xl font-bold">Statistics</h1>
      <button
        class="inline-flex items-center justify-center rounded-md text-sm font-medium border border-input bg-background hover:bg-accent hover:text-accent-foreground h-9 px-3 disabled:opacity-50"
        on:click={handleRefresh}
        disabled={$statsQuery.isFetching}
      >
        <RefreshCw class="h-4 w-4 mr-2 {$statsQuery.isFetching ? 'animate-spin' : ''}" />
        Refresh
      </button>
    </div>

    {#if $statsQuery.isLoading}
      <div class="flex items-center justify-center py-16">
        <RefreshCw class="h-8 w-8 animate-spin text-muted-foreground" />
      </div>
    {:else}
      <!-- Overview cards -->
      <div class="grid gap-4 md:grid-cols-4">
        <Card>
          <CardContent class="pt-6">
            <div class="flex items-center justify-between">
              <div>
                <p class="text-sm text-muted-foreground">Total Jobs</p>
                <div class="text-3xl font-bold">{stats?.total || 0}</div>
              </div>
              <TrendingUp class="h-8 w-8 text-muted-foreground" />
            </div>
          </CardContent>
        </Card>
        <Card>
          <CardContent class="pt-6">
            <div class="flex items-center justify-between">
              <div>
                <p class="text-sm text-muted-foreground">Last Hour</p>
                <div class="text-3xl font-bold">{stats?.recent?.last_hour || 0}</div>
              </div>
              <Clock class="h-8 w-8 text-blue-500" />
            </div>
          </CardContent>
        </Card>
        <Card>
          <CardContent class="pt-6">
            <div class="flex items-center justify-between">
              <div>
                <p class="text-sm text-muted-foreground">Last Day</p>
                <div class="text-3xl font-bold">{stats?.recent?.last_day || 0}</div>
              </div>
              <CheckCircle2 class="h-8 w-8 text-green-500" />
            </div>
          </CardContent>
        </Card>
        <Card>
          <CardContent class="pt-6">
            <div class="flex items-center justify-between">
              <div>
                <p class="text-sm text-muted-foreground">Failed</p>
                <div class="text-3xl font-bold text-destructive">
                  {stats?.by_state?.failed || 0}
                </div>
              </div>
              <XCircle class="h-8 w-8 text-destructive" />
            </div>
          </CardContent>
        </Card>
      </div>

      <div class="grid gap-6 md:grid-cols-2">
        <!-- Jobs by State -->
        <Card>
          <CardHeader>
            <CardTitle>Jobs by State</CardTitle>
          </CardHeader>
          <CardContent>
            <div class="space-y-4">
              {#each sortedStates as [state, count]}
                {@const percentage = getPercentage(Number(count))}
                <div class="space-y-1">
                  <div class="flex justify-between text-sm">
                    <span class="capitalize">{state}</span>
                    <span class="text-muted-foreground">{count} ({percentage}%)</span>
                  </div>
                  <div class="h-2 rounded-full bg-muted">
                    <div
                      class="h-full rounded-full {stateColors[state] || 'bg-primary'}"
                      style="width: {percentage}%"
                    />
                  </div>
                </div>
              {/each}
              {#if sortedStates.length === 0}
                <p class="text-muted-foreground text-sm">No data available</p>
              {/if}
            </div>
          </CardContent>
        </Card>

        <!-- Top Commands -->
        <Card>
          <CardHeader>
            <CardTitle>Top Commands</CardTitle>
          </CardHeader>
          <CardContent>
            <div class="space-y-2">
              {#each topCommands as [command, count]}
                <div class="flex justify-between items-center py-2 border-b last:border-0">
                  <span class="font-mono text-sm truncate max-w-[200px]">{command}</span>
                  <span class="text-muted-foreground">{count}</span>
                </div>
              {/each}
              {#if topCommands.length === 0}
                <p class="text-muted-foreground text-sm">No commands recorded</p>
              {/if}
            </div>
          </CardContent>
        </Card>
      </div>
    {/if}
  </div>
</Layout>
