<script lang="ts">
  import { push } from "svelte-spa-router";
  import { ArrowLeft, RefreshCw, FileText, AlertCircle, Terminal } from "lucide-svelte";
  import Layout from "@/lib/components/layout/Layout.svelte";
  import { useJobDetail } from "@/lib/api/hooks";
  import { Badge, Button, Card, CardContent, CardHeader, CardTitle, CopyButton } from "@/lib/components/ui";
  import { formatRelativeTime } from "@/lib/utils/time";

  export let params: { id: string };

  $: jobQuery = useJobDetail(params.id);
  $: job = $jobQuery.data;

  let showRawResult = false;

  type StateVariant = "default" | "success" | "warning" | "destructive" | "info" | "muted";

  const stateConfig: Record<string, { variant: StateVariant; label: string }> = {
    queued: { variant: "muted", label: "Queued" },
    running: { variant: "info", label: "Running" },
    ok: { variant: "success", label: "Completed" },
    completed: { variant: "success", label: "Completed" },
    error: { variant: "destructive", label: "Error" },
    failed: { variant: "destructive", label: "Failed" },
    canceled: { variant: "warning", label: "Canceled" },
    cancelled: { variant: "warning", label: "Cancelled" },
  };

  function handleBack() {
    push("/jobs");
  }

  function handleRefresh() {
    $jobQuery.refetch();
  }

  function formatResultValue(value: unknown): string {
    if (value === null || value === undefined) return "-";
    if (typeof value === "string") return value;
    if (typeof value === "number" || typeof value === "boolean") return String(value);
    return JSON.stringify(value, null, 2);
  }

  function isComplexValue(value: unknown): boolean {
    return typeof value === "object" && value !== null;
  }
</script>

<Layout>
  <div class="space-y-6">
    <!-- Header -->
    <div class="flex items-center justify-between">
      <div class="flex items-center gap-4">
        <Button variant="ghost" size="icon" on:click={handleBack}>
          <ArrowLeft class="h-4 w-4" />
        </Button>
        <div>
          <h1 class="text-2xl font-bold">
            {#if job}
              {job.skill || job.command || "Job"}
            {:else}
              Job Detail
            {/if}
          </h1>
          <p class="text-sm text-muted-foreground font-mono">{params.id}</p>
        </div>
      </div>
      <div class="flex items-center gap-2">
        {#if job}
          {@const config = stateConfig[job.state] || stateConfig.queued}
          <Badge variant={config.variant}>{config.label}</Badge>
        {/if}
        <Button variant="outline" size="sm" on:click={handleRefresh} disabled={$jobQuery.isFetching}>
          <RefreshCw class="h-4 w-4 mr-2 {$jobQuery.isFetching ? 'animate-spin' : ''}" />
          Refresh
        </Button>
      </div>
    </div>

    {#if $jobQuery.isLoading}
      <div class="flex items-center justify-center py-12">
        <RefreshCw class="h-8 w-8 animate-spin text-muted-foreground" />
      </div>
    {:else if $jobQuery.error}
      <Card>
        <CardContent class="pt-6">
          <div class="flex items-center gap-2 text-destructive">
            <AlertCircle class="h-5 w-5" />
            <span>Failed to load job details</span>
          </div>
        </CardContent>
      </Card>
    {:else if job}
      <!-- Info Cards -->
      <div class="grid gap-4 md:grid-cols-4">
        <Card>
          <CardContent class="pt-6">
            <div class="text-sm text-muted-foreground">Type</div>
            <div class="text-lg font-medium">{job.type || "-"}</div>
          </CardContent>
        </Card>
        <Card>
          <CardContent class="pt-6">
            <div class="text-sm text-muted-foreground">Category</div>
            <div class="text-lg font-medium">{job.category || "-"}</div>
          </CardContent>
        </Card>
        <Card>
          <CardContent class="pt-6">
            <div class="text-sm text-muted-foreground">Skill</div>
            <div class="text-lg font-medium">{job.skill || "-"}</div>
          </CardContent>
        </Card>
        <Card>
          <CardContent class="pt-6">
            <div class="text-sm text-muted-foreground">Created</div>
            <div class="text-lg font-medium">{formatRelativeTime(job.created_at)}</div>
          </CardContent>
        </Card>
      </div>

      <!-- Error display -->
      {#if job.error}
        <Card class="border-destructive">
          <CardHeader>
            <CardTitle class="flex items-center gap-2 text-destructive">
              <AlertCircle class="h-5 w-5" />
              Error
            </CardTitle>
          </CardHeader>
          <CardContent>
            <pre class="text-sm bg-destructive/10 p-4 rounded-md overflow-x-auto whitespace-pre-wrap">{job.error}</pre>
          </CardContent>
        </Card>
      {/if}

      <!-- Command -->
      {#if job.command}
        <Card>
          <CardHeader>
            <CardTitle class="flex items-center gap-2">
              <Terminal class="h-5 w-5" />
              Command
            </CardTitle>
          </CardHeader>
          <CardContent>
            <div class="flex items-start gap-2">
              <pre class="flex-1 text-sm bg-muted p-4 rounded-md overflow-x-auto font-mono">{job.command}</pre>
              <CopyButton text={job.command} />
            </div>
          </CardContent>
        </Card>
      {/if}

      <!-- Artifacts -->
      {#if job.artifacts && job.artifacts.length > 0}
        <Card>
          <CardHeader>
            <CardTitle class="flex items-center gap-2">
              <FileText class="h-5 w-5" />
              Artifacts ({job.artifacts.length})
            </CardTitle>
          </CardHeader>
          <CardContent>
            <div class="space-y-2">
              {#each job.artifacts as artifact}
                <div class="flex items-center gap-2 p-2 bg-muted rounded-md">
                  <FileText class="h-4 w-4 text-muted-foreground" />
                  <span class="font-mono text-sm">{artifact}</span>
                  <CopyButton text={artifact} />
                </div>
              {/each}
            </div>
          </CardContent>
        </Card>
      {/if}

      <!-- Result -->
      {#if job.result_data !== undefined && job.result_data !== null}
        <Card>
          <CardHeader>
            <div class="flex items-center justify-between">
              <CardTitle>Result</CardTitle>
              <Button variant="outline" size="sm" on:click={() => (showRawResult = !showRawResult)}>
                {showRawResult ? "Formatted" : "Raw JSON"}
              </Button>
            </div>
          </CardHeader>
          <CardContent>
            {#if showRawResult}
              <div class="relative">
                <pre class="text-sm bg-muted p-4 rounded-md overflow-x-auto max-h-96">{JSON.stringify(job.result_data, null, 2)}</pre>
                <div class="absolute top-2 right-2">
                  <CopyButton text={JSON.stringify(job.result_data, null, 2)} />
                </div>
              </div>
            {:else}
              <div class="space-y-3">
                {#if typeof job.result_data === "object" && job.result_data !== null && !Array.isArray(job.result_data)}
                  {#each Object.entries(job.result_data) as [key, value]}
                    <div class="border-b border-border pb-3 last:border-0 last:pb-0">
                      <div class="text-sm font-medium text-muted-foreground mb-1">{key}</div>
                      {#if isComplexValue(value)}
                        <pre class="text-sm bg-muted p-3 rounded-md overflow-x-auto">{formatResultValue(value)}</pre>
                      {:else}
                        <div class="text-sm">{formatResultValue(value)}</div>
                      {/if}
                    </div>
                  {/each}
                {:else}
                  <pre class="text-sm bg-muted p-4 rounded-md overflow-x-auto">{formatResultValue(job.result_data)}</pre>
                {/if}
              </div>
            {/if}
          </CardContent>
        </Card>
      {/if}

      <!-- Stderr -->
      {#if job.stderr}
        <Card>
          <CardHeader>
            <CardTitle class="flex items-center gap-2">
              <Terminal class="h-5 w-5" />
              Stderr
            </CardTitle>
          </CardHeader>
          <CardContent>
            <pre class="text-sm bg-muted p-4 rounded-md overflow-x-auto max-h-64 text-orange-600 dark:text-orange-400">{job.stderr}</pre>
          </CardContent>
        </Card>
      {/if}
    {:else}
      <Card>
        <CardContent class="pt-6">
          <div class="text-center text-muted-foreground">Job not found</div>
        </CardContent>
      </Card>
    {/if}
  </div>
</Layout>
