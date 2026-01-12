<script lang="ts">
  import {
    Search,
    RefreshCw,
    ChevronDown,
    ChevronUp,
    FileCode,
    MessageSquare,
    Brain,
    ListTodo,
  } from "lucide-svelte";
  import Layout from "@/lib/components/layout/Layout.svelte";
  import { useSearch, useWorkspaces } from "@/lib/api/hooks";
  import {
    Badge,
    Button,
    Card,
    CardContent,
    Input,
    Select,
    CopyButton,
  } from "@/lib/components/ui";
  import { truncate } from "@/lib/utils/format";
  import type { SearchResult } from "@agentctl/data";

  type SourceVariant = "default" | "secondary" | "info" | "success" | "warning";

  interface SourceConfig {
    variant: SourceVariant;
    label: string;
  }

  const sourceConfig: Record<string, SourceConfig> = {
    symbol: { variant: "default", label: "Code" },
    session: { variant: "info", label: "Session" },
    memory: { variant: "success", label: "Memory" },
    task: { variant: "warning", label: "Task" },
  };

  let query = "";
  let submittedQuery = "";
  let rerank = false;
  let scope = "";
  let limitStr = "50";
  let selectedWorkspace = "";
  let expandedIndex: number | null = null;

  $: limit = parseInt(limitStr, 10);

  $: searchQuery = useSearch({
    q: submittedQuery,
    limit,
    rerank,
    scope: scope || undefined,
    workspace: selectedWorkspace || undefined,
  });

  $: results = $searchQuery.data?.results || [];
  $: stats = $searchQuery.data?.stats;

  const workspacesQuery = useWorkspaces();

  $: workspaces = $workspacesQuery.data?.workspaces || [];
  $: defaultWorkspace = resolveWorkspace($workspacesQuery.data?.current, workspaces);

  $: if (!selectedWorkspace && defaultWorkspace) {
    selectedWorkspace = defaultWorkspace;
  }

  type WorkspaceOption = { path: string; is_active?: boolean };

  function resolveWorkspace(current: string | undefined, list: WorkspaceOption[]): string {
    if (current && list.some((ws) => ws.path === current)) {
      return current;
    }
    return list.find((ws) => ws.is_active)?.path || list[0]?.path || "";
  }

  function handleSubmit(e: Event) {
    e.preventDefault();
    submittedQuery = query;
    expandedIndex = null;
  }

  function toggleExpand(index: number) {
    expandedIndex = expandedIndex === index ? null : index;
  }

  function getConfig(source: string): SourceConfig {
    return sourceConfig[source] || sourceConfig.symbol;
  }

  function getScore(result: SearchResult): string {
    return result.final_score?.toFixed(4) || result.similarity?.toFixed(4) || "—";
  }
</script>

<Layout>
  <div class="space-y-6">
    <h1 class="text-2xl font-bold">Semantic Search</h1>

    <!-- Search form -->
    <Card>
      <CardContent class="pt-6">
        <form on:submit={handleSubmit} class="space-y-4">
          <div class="flex gap-4">
            <div class="flex-1">
              <Input
                placeholder="Search code, sessions, memories..."
                bind:value={query}
                class="h-11"
              />
            </div>
            <Button type="submit" disabled={!query || !selectedWorkspace || $searchQuery.isFetching} class="h-11">
              <Search class="h-4 w-4 mr-2" />
              Search
            </Button>
          </div>
          <div class="flex gap-4 items-center flex-wrap">
            <div class="w-40">
              <Select bind:value={scope}>
                <option value="">All sources</option>
                <option value="symbols">Code (symbols)</option>
                <option value="sessions">Sessions</option>
                <option value="memories">Memories</option>
                <option value="tasks">Tasks</option>
              </Select>
            </div>
            <div class="w-72">
              <Select bind:value={selectedWorkspace} disabled={workspaces.length === 0}>
                {#if workspaces.length === 0}
                  <option value="">No workspaces found</option>
                {:else}
                  {#each workspaces as ws (ws.path)}
                    <option value={ws.path}>{ws.name}</option>
                  {/each}
                {/if}
              </Select>
            </div>
            <div class="w-32">
              <Select bind:value={limitStr}>
                <option value="25">25 results</option>
                <option value="50">50 results</option>
                <option value="100">100 results</option>
              </Select>
            </div>
            <label class="flex items-center gap-2 cursor-pointer">
              <input
                type="checkbox"
                bind:checked={rerank}
                class="rounded"
              />
              <span class="text-sm">Enable reranking</span>
            </label>
          </div>
        </form>
      </CardContent>
    </Card>

    <!-- Stats -->
    {#if stats && submittedQuery}
      <div class="flex gap-3 flex-wrap">
        <Badge variant="outline" class="text-sm py-1">
          {stats.total_results} results
        </Badge>
        <Badge variant="outline" class="text-sm py-1">
          {stats.latency_ms}ms
        </Badge>
        {#if stats.embedding_dimensions > 0}
          <Badge variant="outline" class="text-sm py-1">
            {stats.embedding_dimensions}d vectors
          </Badge>
        {/if}
        {#if stats.reranked}
          <Badge variant="info" class="text-sm py-1">
            Reranked
          </Badge>
        {/if}
        {#each Object.entries(stats.source_counts || {}) as [source, count]}
          {@const config = getConfig(source)}
          <Badge variant={config.variant} class="text-sm py-1 gap-1">
            {#if source === "symbol"}
              <FileCode class="h-3 w-3" />
            {:else if source === "session"}
              <MessageSquare class="h-3 w-3" />
            {:else if source === "memory"}
              <Brain class="h-3 w-3" />
            {:else if source === "task"}
              <ListTodo class="h-3 w-3" />
            {/if}
            {source}: {count}
          </Badge>
        {/each}
      </div>
    {/if}

    <!-- Results -->
    {#if submittedQuery}
      <div>
        <div class="flex items-center justify-between mb-4">
          <h2 class="text-lg font-semibold">
            Results for "{truncate(submittedQuery, 40)}"
          </h2>
          {#if results.length > 0}
            <Button
              variant="ghost"
              size="sm"
              on:click={() => (expandedIndex = expandedIndex === null ? 0 : null)}
            >
              {expandedIndex !== null ? "Collapse All" : "Expand First"}
            </Button>
          {/if}
        </div>

        {#if $searchQuery.isLoading}
          <div class="flex items-center justify-center py-16">
            <RefreshCw class="h-8 w-8 animate-spin text-muted-foreground" />
          </div>
        {:else if results.length === 0}
          <Card>
            <CardContent class="py-16 text-center text-muted-foreground">
              No results found for "{submittedQuery}"
            </CardContent>
          </Card>
        {:else}
          <div class="space-y-3">
            {#each results as result, i (result.source + "-" + result.id)}
              {@const config = getConfig(result.source)}
              <Card class={expandedIndex === i ? "ring-2 ring-primary" : ""}>
                <CardContent class="p-4">
                  <!-- Result header -->
                  <button
                    type="button"
                    class="flex items-start justify-between w-full text-left cursor-pointer"
                    on:click={() => toggleExpand(i)}
                  >
                    <div class="flex items-start gap-3 flex-1">
                      <div class="flex items-center justify-center w-8 h-8 rounded-full bg-muted text-sm font-medium">
                        {i + 1}
                      </div>
                      <div class="flex-1 min-w-0">
                        <div class="flex items-center gap-2 mb-1">
                          <Badge variant={config.variant} class="gap-1">
                            {#if result.source === "symbol"}
                              <FileCode class="h-4 w-4" />
                            {:else if result.source === "session"}
                              <MessageSquare class="h-4 w-4" />
                            {:else if result.source === "memory"}
                              <Brain class="h-4 w-4" />
                            {:else if result.source === "task"}
                              <ListTodo class="h-4 w-4" />
                            {/if}
                            {config.label}
                          </Badge>
                          <span class="text-xs text-muted-foreground">
                            Score: {getScore(result)}
                          </span>
                        </div>
                        <p class="font-medium truncate">
                          {result.name || result.path || result.id}
                        </p>
                        {#if result.path && result.name}
                          <p class="text-sm text-muted-foreground truncate font-mono">
                            {result.path}
                          </p>
                        {/if}
                      </div>
                    </div>
                    <div class="h-8 w-8 shrink-0 flex items-center justify-center">
                      {#if expandedIndex === i}
                        <ChevronUp class="h-4 w-4" />
                      {:else}
                        <ChevronDown class="h-4 w-4" />
                      {/if}
                    </div>
                  </button>

                  <!-- Expanded details -->
                  {#if expandedIndex === i}
                    <div class="mt-4 pt-4 border-t space-y-3">
                      <div class="grid grid-cols-2 md:grid-cols-4 gap-4 text-sm">
                        <div>
                          <p class="text-muted-foreground">Source</p>
                          <p class="font-medium">{result.source}</p>
                        </div>
                        <div>
                          <p class="text-muted-foreground">Similarity</p>
                          <p class="font-mono">{result.similarity?.toFixed(4) || "—"}</p>
                        </div>
                        {#if result.rerank_score !== undefined && result.rerank_score > 0}
                          <div>
                            <p class="text-muted-foreground">Rerank Score</p>
                            <p class="font-mono">{result.rerank_score.toFixed(4)}</p>
                          </div>
                        {/if}
                        <div>
                          <p class="text-muted-foreground">Final Score</p>
                          <p class="font-mono font-semibold">{result.final_score?.toFixed(4) || "—"}</p>
                        </div>
                        <div>
                          <p class="text-muted-foreground">Global Rank</p>
                          <p class="font-medium">#{result.rank}</p>
                        </div>
                        <div>
                          <p class="text-muted-foreground">Source Rank</p>
                          <p class="font-medium">#{result.source_rank}</p>
                        </div>
                      </div>

                      <div>
                        <div class="flex items-center justify-between mb-1">
                          <p class="text-sm text-muted-foreground">ID</p>
                          <CopyButton text={result.id} />
                        </div>
                        <code class="text-xs bg-muted p-2 rounded block font-mono break-all">
                          {result.id}
                        </code>
                      </div>

                      {#if result.path}
                        <div>
                          <div class="flex items-center justify-between mb-1">
                            <p class="text-sm text-muted-foreground">
                              Path{result.line ? `:${result.line}` : ""}
                            </p>
                            <CopyButton text={result.path} />
                          </div>
                          <code class="text-xs bg-muted p-2 rounded block font-mono break-all">
                            {result.path}{result.line ? `:${result.line}` : ""}
                          </code>
                        </div>
                      {/if}

                      {#if result.snippet}
                        <div>
                          <div class="flex items-center justify-between mb-1">
                            <p class="text-sm text-muted-foreground">Code Snippet</p>
                            <CopyButton text={result.snippet} />
                          </div>
                          <pre class="text-xs bg-zinc-900 text-zinc-100 p-3 rounded overflow-x-auto max-h-48 font-mono">{result.snippet}</pre>
                        </div>
                      {/if}

                      {#if result.summary}
                        <div>
                          <div class="flex items-center justify-between mb-1">
                            <p class="text-sm text-muted-foreground">
                              {#if result.source === "memory"}
                                Memory
                              {:else if result.source === "session"}
                                Session Summary
                              {:else}
                                Summary
                              {/if}
                            </p>
                            <CopyButton text={result.summary} />
                          </div>
                          <div class="text-sm bg-muted/50 p-3 rounded whitespace-pre-wrap">
                            {result.summary}
                          </div>
                        </div>
                      {/if}
                    </div>
                  {/if}
                </CardContent>
              </Card>
            {/each}
          </div>
        {/if}
      </div>
    {/if}
  </div>
</Layout>
