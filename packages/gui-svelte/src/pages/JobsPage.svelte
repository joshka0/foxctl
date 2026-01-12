<script lang="ts">
  import { push } from "svelte-spa-router";
  import { RefreshCw } from "lucide-svelte";
  import Layout from "@/lib/components/layout/Layout.svelte";
  import { useJobs } from "@/lib/api/hooks";
  import { Badge, Button, Select, Card, CardContent, CardHeader, CardTitle } from "@/lib/components/ui";
  import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/lib/components/ui";
  import { formatRelativeTime } from "@/lib/utils/time";
  import { truncate } from "@/lib/utils/format";

  let stateFilter = "";
  let limitStr = "50";

  $: limit = parseInt(limitStr, 10);
  $: jobsQuery = useJobs({ state: stateFilter || undefined, limit });
  $: jobs = $jobsQuery.data?.jobs || [];

  const stateColors: Record<string, "default" | "success" | "warning" | "destructive" | "info" | "muted"> = {
    queued: "muted",
    running: "info",
    ok: "success",
    error: "destructive",
    canceled: "warning",
    completed: "success",
    pending: "warning",
    failed: "destructive",
    cancelled: "warning",
  };

  function handleRowClick(id: string) {
    push(`/jobs/${id}`);
  }

  function handleRefresh() {
    $jobsQuery.refetch();
  }
</script>

<Layout>
  <div class="space-y-6">
    <div class="flex items-center justify-between">
      <div class="flex items-center gap-4">
        <h1 class="text-2xl font-bold">Jobs</h1>
      </div>
      <Button variant="outline" size="sm" on:click={handleRefresh} disabled={$jobsQuery.isFetching}>
        <RefreshCw class="h-4 w-4 mr-2 {$jobsQuery.isFetching ? 'animate-spin' : ''}" />
        Refresh
      </Button>
    </div>

    <!-- Filters -->
    <Card>
      <CardContent class="pt-6">
        <div class="flex gap-4">
          <div class="w-40">
            <label for="state-filter" class="text-sm font-medium mb-1 block">State</label>
            <Select id="state-filter" bind:value={stateFilter}>
              <option value="">All states</option>
              <option value="queued">Queued</option>
              <option value="running">Running</option>
              <option value="ok">Completed</option>
              <option value="error">Error</option>
              <option value="canceled">Canceled</option>
            </Select>
          </div>
          <div class="w-32">
            <label for="limit-filter" class="text-sm font-medium mb-1 block">Limit</label>
            <Select id="limit-filter" bind:value={limitStr}>
              <option value="25">25</option>
              <option value="50">50</option>
              <option value="100">100</option>
              <option value="200">200</option>
            </Select>
          </div>
        </div>
      </CardContent>
    </Card>

    <!-- Jobs table -->
    <Card>
      <CardHeader>
        <CardTitle class="text-lg">
          {jobs.length} job{jobs.length !== 1 ? "s" : ""}
        </CardTitle>
      </CardHeader>
      <CardContent>
        {#if $jobsQuery.isLoading}
          <div class="flex items-center justify-center py-8">
            <RefreshCw class="h-6 w-6 animate-spin text-muted-foreground" />
          </div>
        {:else if jobs.length === 0}
          <div class="text-center py-8 text-muted-foreground">
            No jobs found
          </div>
        {:else}
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>ID</TableHead>
                <TableHead>Command</TableHead>
                <TableHead>State</TableHead>
                <TableHead>Created</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {#each jobs as job (job.id)}
                <TableRow
                  class="cursor-pointer hover:bg-muted/50"
                  on:click={() => handleRowClick(job.id)}
                >
                  <TableCell class="font-mono text-xs">
                    {truncate(job.id, 12)}
                  </TableCell>
                  <TableCell>
                    <div class="flex flex-col">
                      <span class="font-medium">{job.skill || job.command}</span>
                      {#if job.category}
                        <span class="text-xs text-muted-foreground">
                          {job.type}:{job.category}
                        </span>
                      {/if}
                    </div>
                  </TableCell>
                  <TableCell>
                    <Badge variant={stateColors[job.state] || "default"}>
                      {job.state}
                    </Badge>
                  </TableCell>
                  <TableCell class="text-muted-foreground">
                    {formatRelativeTime(job.created_at)}
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
