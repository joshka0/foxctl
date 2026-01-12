<script lang="ts">
  import { link } from "svelte-spa-router";
  import { RefreshCw, CheckCircle2, Clock, PlayCircle, ExternalLink } from "lucide-svelte";
  import Layout from "@/lib/components/layout/Layout.svelte";
  import { useTasks } from "@/lib/api/hooks";
  import { Badge, Button, Card, CardContent, CardHeader, CardTitle } from "@/lib/components/ui";
  import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/lib/components/ui";
  import { formatRelativeTime } from "@/lib/utils/time";
  import { truncate } from "@/lib/utils/format";

  const tasksQuery = useTasks({ limit: 100 });
  $: tasks = $tasksQuery.data?.tasks || [];
  $: stats = $tasksQuery.data?.stats || { total: 0, pending: 0, in_progress: 0, completed: 0 };

  type StatusVariant = "default" | "success" | "warning" | "info";

  const statusConfig: Record<string, { variant: StatusVariant }> = {
    pending: { variant: "warning" },
    in_progress: { variant: "info" },
    completed: { variant: "success" },
  };

  function handleRefresh() {
    $tasksQuery.refetch();
  }

  function getStatusIcon(status: string) {
    switch (status) {
      case "pending":
        return Clock;
      case "in_progress":
        return PlayCircle;
      case "completed":
        return CheckCircle2;
      default:
        return Clock;
    }
  }
</script>

<Layout>
  <div class="space-y-6">
    <div class="flex items-center justify-between">
      <div class="flex items-center gap-4">
        <h1 class="text-2xl font-bold">Tasks</h1>
      </div>
      <Button variant="outline" size="sm" on:click={handleRefresh} disabled={$tasksQuery.isFetching}>
        <RefreshCw class="h-4 w-4 mr-2 {$tasksQuery.isFetching ? 'animate-spin' : ''}" />
        Refresh
      </Button>
    </div>

    <!-- Stats cards -->
    <div class="grid gap-4 md:grid-cols-4">
      <Card>
        <CardContent class="pt-6">
          <div class="text-2xl font-bold">{stats.total}</div>
          <p class="text-sm text-muted-foreground">Total Tasks</p>
        </CardContent>
      </Card>
      <Card>
        <CardContent class="pt-6">
          <div class="text-2xl font-bold text-yellow-600">{stats.pending}</div>
          <p class="text-sm text-muted-foreground">Pending</p>
        </CardContent>
      </Card>
      <Card>
        <CardContent class="pt-6">
          <div class="text-2xl font-bold text-blue-600">{stats.in_progress}</div>
          <p class="text-sm text-muted-foreground">In Progress</p>
        </CardContent>
      </Card>
      <Card>
        <CardContent class="pt-6">
          <div class="text-2xl font-bold text-green-600">{stats.completed}</div>
          <p class="text-sm text-muted-foreground">Completed</p>
        </CardContent>
      </Card>
    </div>

    <!-- Tasks table -->
    <Card>
      <CardHeader>
        <CardTitle class="text-lg">All Tasks</CardTitle>
      </CardHeader>
      <CardContent>
        {#if $tasksQuery.isLoading}
          <div class="flex items-center justify-center py-8">
            <RefreshCw class="h-6 w-6 animate-spin text-muted-foreground" />
          </div>
        {:else if tasks.length === 0}
          <div class="text-center py-8 text-muted-foreground">
            No tasks found
          </div>
        {:else}
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>Title</TableHead>
                <TableHead>Status</TableHead>
                <TableHead>Priority</TableHead>
                <TableHead>PageRank</TableHead>
                <TableHead>Created</TableHead>
                <TableHead class="w-12"></TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {#each tasks as task (task.id)}
                {@const config = statusConfig[task.status || "pending"] || statusConfig.pending}
                {@const StatusIcon = getStatusIcon(task.status || "pending")}
                <TableRow class="cursor-pointer hover:bg-muted/50">
                  <TableCell>
                    <a href="/tasks/{task.id}" use:link class="flex flex-col">
                      <span class="font-medium hover:underline">{task.title}</span>
                      {#if task.description}
                        <span class="text-xs text-muted-foreground">
                          {truncate(task.description, 60)}
                        </span>
                      {/if}
                    </a>
                  </TableCell>
                  <TableCell>
                    <Badge variant={config.variant} class="gap-1">
                      <svelte:component this={StatusIcon} class="h-3 w-3" />
                      {task.status || "pending"}
                    </Badge>
                  </TableCell>
                  <TableCell>{task.priority || 0}</TableCell>
                  <TableCell>
                    {#if task.pagerank}
                      <span class="font-mono text-xs">
                        {(task.pagerank * 100).toFixed(2)}%
                      </span>
                    {:else}
                      -
                    {/if}
                  </TableCell>
                  <TableCell class="text-muted-foreground">
                    {formatRelativeTime(task.created_at)}
                  </TableCell>
                  <TableCell>
                    <a href="/tasks/{task.id}" use:link>
                      <Button variant="ghost" size="icon" class="h-8 w-8">
                        <ExternalLink class="h-4 w-4" />
                      </Button>
                    </a>
                  </TableCell>
                </TableRow>
              {/each}
            </TableBody>
          </Table>
        {/if}
      </CardContent>
    </Card>
  </div>
</Layout>
