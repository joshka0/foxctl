<script lang="ts">
  import { link } from "svelte-spa-router";
  import { push } from "svelte-spa-router";
  import { ArrowLeft, RefreshCw, CheckCircle2, Clock, PlayCircle, GitBranch, TrendingUp } from "lucide-svelte";
  import Layout from "@/lib/components/layout/Layout.svelte";
  import { useTasks } from "@/lib/api/hooks";
  import { Badge, Button, Card, CardContent, CardHeader, CardTitle } from "@/lib/components/ui";
  import { formatRelativeTime } from "@/lib/utils/time";
  import type { TaskSummary } from "@agentctl/data";

  export let params: { id: string };

  const tasksQuery = useTasks({ limit: 500 });

  $: tasks = $tasksQuery.data?.tasks || [];
  $: task = tasks.find((t) => t.id === params.id);

  const isTaskSummary = (dep: TaskSummary | undefined): dep is TaskSummary => Boolean(dep);

  // Tasks this task depends on
  $: dependentTasks = (task?.depends_on || [])
    .map((depId: string) => tasks.find((t) => t.id === depId))
    .filter(isTaskSummary);

  // Tasks that depend on this task
  $: dependingTasks = tasks.filter((t) => t.depends_on?.includes(params.id));

  type StatusVariant = "default" | "success" | "warning" | "info";

  const statusConfig: Record<string, { variant: StatusVariant; label: string }> = {
    pending: { variant: "warning", label: "Pending" },
    in_progress: { variant: "info", label: "In Progress" },
    completed: { variant: "success", label: "Completed" },
  };

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

  function handleBack() {
    push("/tasks");
  }

  function handleRefresh() {
    $tasksQuery.refetch();
  }

  function formatDate(dateStr: string | undefined): string {
    if (!dateStr) return "-";
    try {
      return new Date(dateStr).toLocaleString();
    } catch {
      return dateStr;
    }
  }
</script>

<Layout>
  <div class="space-y-6">
    {#if $tasksQuery.isLoading}
      <div class="flex items-center justify-center py-16">
        <RefreshCw class="h-8 w-8 animate-spin text-muted-foreground" />
      </div>
    {:else if !task}
      <div class="text-center py-16">
        <p class="text-muted-foreground">Task not found</p>
        <div class="mt-4">
          <Button variant="outline" on:click={handleBack}>
            <ArrowLeft class="h-4 w-4 mr-2" />
            Back to Tasks
          </Button>
        </div>
      </div>
    {:else}
      {@const config = statusConfig[task.status || "pending"] || statusConfig.pending}
      {@const StatusIcon = getStatusIcon(task.status || "pending")}

      <!-- Header -->
      <div class="flex items-center justify-between">
        <div class="flex items-center gap-4">
          <Button variant="outline" size="icon" on:click={handleBack}>
            <ArrowLeft class="h-4 w-4" />
          </Button>
          <div>
            <h1 class="text-2xl font-bold">{task.title}</h1>
            <p class="text-sm text-muted-foreground font-mono">{task.id}</p>
          </div>
        </div>
        <Button variant="outline" size="sm" on:click={handleRefresh} disabled={$tasksQuery.isFetching}>
          <RefreshCw class="h-4 w-4 mr-2 {$tasksQuery.isFetching ? 'animate-spin' : ''}" />
          Refresh
        </Button>
      </div>

      <div class="grid gap-6 md:grid-cols-2">
        <!-- Task Details -->
        <Card>
          <CardHeader>
            <CardTitle>Task Details</CardTitle>
          </CardHeader>
          <CardContent class="space-y-4">
            <div class="grid grid-cols-2 gap-4">
              <div>
                <p class="text-sm text-muted-foreground">Status</p>
                <Badge variant={config.variant} class="mt-1 gap-1">
                  <svelte:component this={StatusIcon} class="h-3 w-3" />
                  {config.label}
                </Badge>
              </div>
              <div>
                <p class="text-sm text-muted-foreground">Priority</p>
                <p class="font-medium text-lg">{task.priority || 0}</p>
              </div>
              <div>
                <p class="text-sm text-muted-foreground">Created</p>
                <p class="font-medium">{formatDate(task.created_at)}</p>
                <p class="text-xs text-muted-foreground">{formatRelativeTime(task.created_at)}</p>
              </div>
              {#if task.completed_at}
                <div>
                  <p class="text-sm text-muted-foreground">Completed</p>
                  <p class="font-medium">{formatDate(task.completed_at)}</p>
                  <p class="text-xs text-muted-foreground">{formatRelativeTime(task.completed_at)}</p>
                </div>
              {/if}
            </div>

            {#if task.description}
              <div>
                <p class="text-sm text-muted-foreground mb-2">Description</p>
                <p class="text-sm bg-muted p-3 rounded-md whitespace-pre-wrap">{task.description}</p>
              </div>
            {/if}

            {#if task.notes}
              <div>
                <p class="text-sm text-muted-foreground mb-2">Notes</p>
                <p class="text-sm bg-muted p-3 rounded-md whitespace-pre-wrap">{task.notes}</p>
              </div>
            {/if}
          </CardContent>
        </Card>

        <!-- Graph Metrics -->
        <Card>
          <CardHeader>
            <CardTitle class="flex items-center gap-2">
              <TrendingUp class="h-5 w-5" />
              Graph Metrics
            </CardTitle>
          </CardHeader>
          <CardContent>
            <div class="grid grid-cols-2 gap-4">
              <div class="p-4 bg-muted rounded-lg text-center">
                <p class="text-2xl font-bold text-primary">
                  {task.pagerank ? (task.pagerank * 100).toFixed(2) : "0.00"}%
                </p>
                <p class="text-sm text-muted-foreground">PageRank</p>
              </div>
              <div class="p-4 bg-muted rounded-lg text-center">
                <p class="text-2xl font-bold text-blue-600">
                  {task.critical_path_score || 0}
                </p>
                <p class="text-sm text-muted-foreground">Critical Path Score</p>
              </div>
              <div class="p-4 bg-muted rounded-lg text-center">
                <p class="text-2xl font-bold text-green-600">
                  {task.in_degree || 0}
                </p>
                <p class="text-sm text-muted-foreground">Dependencies</p>
              </div>
              <div class="p-4 bg-muted rounded-lg text-center">
                <p class="text-2xl font-bold text-orange-600">
                  {task.out_degree || 0}
                </p>
                <p class="text-sm text-muted-foreground">Dependents</p>
              </div>
            </div>
            <div class="mt-4 p-3 bg-blue-50 dark:bg-blue-950 rounded-md">
              <p class="text-sm font-medium text-blue-800 dark:text-blue-200">Score</p>
              <p class="text-lg font-bold text-blue-600">{task.score?.toFixed(2) || "0.00"}</p>
            </div>
          </CardContent>
        </Card>
      </div>

      <!-- Dependencies: tasks this task depends on -->
      {#if dependentTasks.length > 0}
        <Card>
          <CardHeader>
            <CardTitle class="flex items-center gap-2">
              <GitBranch class="h-5 w-5" />
              This task depends on ({dependentTasks.length})
            </CardTitle>
          </CardHeader>
          <CardContent>
            <div class="space-y-2">
              {#each dependentTasks as dep (dep.id)}
                {@const depConfig = statusConfig[dep.status || "pending"] || statusConfig.pending}
                <a
                  href="/tasks/{dep.id}"
                  use:link
                  class="flex items-center justify-between p-3 bg-muted rounded-md hover:bg-muted/80 transition-colors"
                >
                  <div>
                    <p class="font-medium">{dep.title}</p>
                    <p class="text-xs text-muted-foreground font-mono">{dep.id}</p>
                  </div>
                  <Badge variant={depConfig.variant}>
                    {dep.status || "pending"}
                  </Badge>
                </a>
              {/each}
            </div>
          </CardContent>
        </Card>
      {/if}

      <!-- Dependents: tasks that depend on this task -->
      {#if dependingTasks.length > 0}
        <Card>
          <CardHeader>
            <CardTitle class="flex items-center gap-2">
              <GitBranch class="h-5 w-5 rotate-180" />
              Tasks depending on this ({dependingTasks.length})
            </CardTitle>
          </CardHeader>
          <CardContent>
            <div class="space-y-2">
              {#each dependingTasks as dep (dep.id)}
                {@const depConfig = statusConfig[dep.status || "pending"] || statusConfig.pending}
                <a
                  href="/tasks/{dep.id}"
                  use:link
                  class="flex items-center justify-between p-3 bg-muted rounded-md hover:bg-muted/80 transition-colors"
                >
                  <div>
                    <p class="font-medium">{dep.title}</p>
                    <p class="text-xs text-muted-foreground font-mono">{dep.id}</p>
                  </div>
                  <Badge variant={depConfig.variant}>
                    {dep.status || "pending"}
                  </Badge>
                </a>
              {/each}
            </div>
          </CardContent>
        </Card>
      {/if}
    {/if}
  </div>
</Layout>
