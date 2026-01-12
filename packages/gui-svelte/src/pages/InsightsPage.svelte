<script lang="ts">
  import { RefreshCw, TrendingUp, GitBranch, AlertTriangle } from "lucide-svelte";
  import Layout from "@/lib/components/layout/Layout.svelte";
  import { useInsights, useTasks } from "@/lib/api/hooks";
  import {
    Badge,
    Button,
    Card,
    CardContent,
    CardHeader,
    CardTitle,
    Table,
    TableHeader,
    TableBody,
    TableRow,
    TableHead,
    TableCell,
  } from "@/lib/components/ui";

  const insightsQuery = useInsights();
  const tasksQuery = useTasks({ limit: 200 });

  $: insights = $insightsQuery.data;
  $: tasks = $tasksQuery.data?.tasks || [];

  // Create a map of task IDs to titles
  $: taskTitles = new Map(tasks.map((t) => [t.id, t.title]));

  $: nodes = insights?.nodes || [];
  $: cycles = insights?.cycles || [];
  $: topoOrder = insights?.topological_order || [];

  // Sort nodes by pagerank descending
  $: sortedByPagerank = [...nodes].sort((a, b) => b.pagerank - a.pagerank);

  // Sort nodes by critical path score descending
  $: sortedByCritical = [...nodes]
    .filter((n) => n.critical_path_score > 0)
    .sort((a, b) => b.critical_path_score - a.critical_path_score);

  // Get high-degree nodes (potential bottlenecks)
  $: bottlenecks = [...nodes]
    .filter((n) => n.in_degree >= 2 || n.out_degree >= 2)
    .sort((a, b) => (b.in_degree + b.out_degree) - (a.in_degree + a.out_degree));

  function handleRefresh() {
    $insightsQuery.refetch();
  }

  function getTaskTitle(taskId: string): string {
    return taskTitles.get(taskId) || "Unknown";
  }

  function formatPagerank(pagerank: number): string {
    return (pagerank * 100).toFixed(2) + "%";
  }

  function truncateId(id: string): string {
    return id.slice(0, 12) + "...";
  }
</script>

<Layout>
  <div class="space-y-6">
    <div class="flex items-center justify-between">
      <h1 class="text-2xl font-bold">Task Graph Insights</h1>
      <Button
        variant="outline"
        size="sm"
        on:click={handleRefresh}
        disabled={$insightsQuery.isFetching}
      >
        <RefreshCw class="h-4 w-4 mr-2 {$insightsQuery.isFetching ? 'animate-spin' : ''}" />
        Refresh
      </Button>
    </div>

    {#if $insightsQuery.isLoading}
      <div class="flex items-center justify-center py-16">
        <RefreshCw class="h-8 w-8 animate-spin text-muted-foreground" />
      </div>
    {:else if $insightsQuery.isError}
      <Card class="border-destructive">
        <CardContent class="py-8 text-center">
          <AlertTriangle class="h-8 w-8 mx-auto mb-2 text-destructive" />
          <p class="text-destructive font-medium">Failed to load insights</p>
          <p class="text-sm text-muted-foreground mt-1">{$insightsQuery.error?.message || "Unknown error"}</p>
          <Button variant="outline" size="sm" class="mt-4" on:click={handleRefresh}>
            Retry
          </Button>
        </CardContent>
      </Card>
    {:else}
      <!-- Summary cards -->
      <div class="grid gap-4 md:grid-cols-4">
        <Card>
          <CardContent class="pt-6">
            <div class="flex items-center justify-between">
              <div>
                <p class="text-sm text-muted-foreground">Total Nodes</p>
                <div class="text-3xl font-bold">{nodes.length}</div>
              </div>
              <GitBranch class="h-8 w-8 text-muted-foreground" />
            </div>
          </CardContent>
        </Card>
        <Card>
          <CardContent class="pt-6">
            <div class="flex items-center justify-between">
              <div>
                <p class="text-sm text-muted-foreground">Critical Path Tasks</p>
                <div class="text-3xl font-bold">{sortedByCritical.length}</div>
              </div>
              <TrendingUp class="h-8 w-8 text-blue-500" />
            </div>
          </CardContent>
        </Card>
        <Card>
          <CardContent class="pt-6">
            <div class="flex items-center justify-between">
              <div>
                <p class="text-sm text-muted-foreground">Bottleneck Tasks</p>
                <div class="text-3xl font-bold">{bottlenecks.length}</div>
              </div>
              <AlertTriangle class="h-8 w-8 text-yellow-500" />
            </div>
          </CardContent>
        </Card>
        <Card>
          <CardContent class="pt-6">
            <div class="flex items-center justify-between">
              <div>
                <p class="text-sm text-muted-foreground">Cycles Detected</p>
                <div class="text-3xl font-bold {cycles.length > 0 ? 'text-destructive' : 'text-green-600'}">
                  {cycles.length}
                </div>
              </div>
              {#if cycles.length > 0}
                <AlertTriangle class="h-8 w-8 text-destructive" />
              {:else}
                <Badge variant="success" class="text-xs">OK</Badge>
              {/if}
            </div>
          </CardContent>
        </Card>
      </div>

      <div class="grid gap-6 md:grid-cols-2">
        <!-- Top PageRank tasks -->
        <Card>
          <CardHeader>
            <CardTitle class="flex items-center gap-2">
              <TrendingUp class="h-5 w-5" />
              Highest PageRank Tasks
            </CardTitle>
          </CardHeader>
          <CardContent>
            {#if sortedByPagerank.length === 0}
              <p class="text-muted-foreground text-sm">No tasks with dependencies</p>
            {:else}
              <Table>
                <TableHeader>
                  <TableRow>
                    <TableHead>Task</TableHead>
                    <TableHead class="text-right">PageRank</TableHead>
                    <TableHead class="text-right">In/Out</TableHead>
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {#each sortedByPagerank.slice(0, 10) as node (node.task_id)}
                    <TableRow>
                      <TableCell>
                        <div class="flex flex-col">
                          <span class="font-medium text-sm">
                            {getTaskTitle(node.task_id)}
                          </span>
                          <span class="text-xs text-muted-foreground font-mono">
                            {truncateId(node.task_id)}
                          </span>
                        </div>
                      </TableCell>
                      <TableCell class="text-right font-mono text-sm">
                        {formatPagerank(node.pagerank)}
                      </TableCell>
                      <TableCell class="text-right text-sm">
                        <span class="text-green-600">{node.in_degree}</span>
                        {" / "}
                        <span class="text-blue-600">{node.out_degree}</span>
                      </TableCell>
                    </TableRow>
                  {/each}
                </TableBody>
              </Table>
            {/if}
          </CardContent>
        </Card>

        <!-- Critical path tasks -->
        <Card>
          <CardHeader>
            <CardTitle class="flex items-center gap-2">
              <GitBranch class="h-5 w-5" />
              Critical Path Tasks
            </CardTitle>
          </CardHeader>
          <CardContent>
            {#if sortedByCritical.length === 0}
              <p class="text-muted-foreground text-sm">No tasks on critical path</p>
            {:else}
              <Table>
                <TableHeader>
                  <TableRow>
                    <TableHead>Task</TableHead>
                    <TableHead class="text-right">CP Score</TableHead>
                    <TableHead class="text-right">PageRank</TableHead>
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {#each sortedByCritical.slice(0, 10) as node (node.task_id)}
                    <TableRow>
                      <TableCell>
                        <div class="flex flex-col">
                          <span class="font-medium text-sm">
                            {getTaskTitle(node.task_id)}
                          </span>
                          <span class="text-xs text-muted-foreground font-mono">
                            {truncateId(node.task_id)}
                          </span>
                        </div>
                      </TableCell>
                      <TableCell class="text-right">
                        <Badge variant="warning">{node.critical_path_score}</Badge>
                      </TableCell>
                      <TableCell class="text-right font-mono text-sm">
                        {formatPagerank(node.pagerank)}
                      </TableCell>
                    </TableRow>
                  {/each}
                </TableBody>
              </Table>
            {/if}
          </CardContent>
        </Card>
      </div>

      <!-- Cycles warning -->
      {#if cycles.length > 0}
        <Card class="border-destructive">
          <CardHeader>
            <CardTitle class="flex items-center gap-2 text-destructive">
              <AlertTriangle class="h-5 w-5" />
              Dependency Cycles Detected
            </CardTitle>
          </CardHeader>
          <CardContent>
            <div class="space-y-2">
              {#each cycles as cycle, i (i)}
                <div class="p-3 bg-destructive/10 rounded-md">
                  <p class="text-sm font-medium">Cycle {i + 1}:</p>
                  <div class="flex flex-wrap gap-1 mt-1">
                    {#each cycle as taskId, j (taskId)}
                      <span>
                        <Badge variant="outline" class="font-mono text-xs">
                          {getTaskTitle(taskId) !== "Unknown" ? getTaskTitle(taskId) : taskId.slice(0, 8)}
                        </Badge>
                        {#if j < cycle.length - 1}
                          <span class="mx-1 text-muted-foreground">→</span>
                        {/if}
                      </span>
                    {/each}
                  </div>
                </div>
              {/each}
            </div>
          </CardContent>
        </Card>
      {/if}

      <!-- Topological order -->
      {#if topoOrder.length > 0}
        <Card>
          <CardHeader>
            <CardTitle>Execution Order (Topological)</CardTitle>
          </CardHeader>
          <CardContent>
            <div class="flex flex-wrap gap-1">
              {#each topoOrder.slice(0, 30) as taskId, i (taskId)}
                <Badge variant="secondary" class="text-xs">
                  {i + 1}. {getTaskTitle(taskId) !== "Unknown" ? getTaskTitle(taskId) : taskId.slice(0, 8)}
                </Badge>
              {/each}
              {#if topoOrder.length > 30}
                <Badge variant="outline" class="text-xs">
                  +{topoOrder.length - 30} more
                </Badge>
              {/if}
            </div>
          </CardContent>
        </Card>
      {/if}
    {/if}
  </div>
</Layout>
