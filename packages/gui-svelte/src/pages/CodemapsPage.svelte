<script lang="ts">
  import {
    RefreshCw,
    Map as MapIcon,
    FileCode,
    Code2,
    ChevronRight,
    ChevronDown,
    Search,
    Folder,
    Tag,
  } from "lucide-svelte";
  import Layout from "@/lib/components/layout/Layout.svelte";
  import { useCodemaps, useCodemap, useWorkspaces } from "@/lib/api/hooks";
  import {
    Badge,
    Button,
    Card,
    CardContent,
    CardHeader,
    CardTitle,
    Input,
    CopyButton,
    Select,
  } from "@/lib/components/ui";

  let searchQuery = "";
  let selectedId = "";
  let expandedTraces: Set<number> = new Set();
  let selectedWorkspace = "";
  let lastWorkspace = "";

  const workspacesQuery = useWorkspaces();

  $: workspaces = $workspacesQuery.data?.workspaces || [];
  $: defaultWorkspace = resolveWorkspace($workspacesQuery.data?.current, workspaces);

  $: if (!selectedWorkspace && defaultWorkspace) {
    selectedWorkspace = defaultWorkspace;
  }

  $: if (selectedWorkspace !== lastWorkspace) {
    lastWorkspace = selectedWorkspace;
    selectedId = "";
    expandedTraces = new Set();
  }

  $: codemapsQuery = useCodemaps({ limit: 100, workspace: selectedWorkspace || undefined });

  $: codemaps = $codemapsQuery.data?.codemaps || [];
  $: filteredCodemaps = searchQuery
    ? codemaps.filter(
        (c) =>
          c.title.toLowerCase().includes(searchQuery.toLowerCase()) ||
          c.query.toLowerCase().includes(searchQuery.toLowerCase())
      )
    : codemaps;

  $: codemapDetailQuery = useCodemap(selectedId, selectedWorkspace || undefined);
  $: selectedCodemap = $codemapDetailQuery.data;

  function handleRefresh() {
    $codemapsQuery.refetch();
    if (selectedId) {
      $codemapDetailQuery.refetch();
    }
  }

  function selectCodemap(id: string) {
    selectedId = id;
    expandedTraces = new Set();
  }

  function toggleTrace(traceNum: number) {
    if (expandedTraces.has(traceNum)) {
      expandedTraces.delete(traceNum);
    } else {
      expandedTraces.add(traceNum);
    }
    expandedTraces = new Set(expandedTraces);
  }

  function formatDate(dateStr: string): string {
    return new Date(dateStr).toLocaleDateString(undefined, {
      month: "short",
      day: "numeric",
      year: "numeric",
    });
  }

  type WorkspaceOption = { path: string; is_active?: boolean };

  function resolveWorkspace(current: string | undefined, list: WorkspaceOption[]): string {
    if (current && list.some((ws) => ws.path === current)) {
      return current;
    }
    return list.find((ws) => ws.is_active)?.path || list[0]?.path || "";
  }

</script>

<Layout>
  <div class="h-[calc(100vh-4rem)] flex">
    <!-- Codemaps list sidebar -->
    <div class="w-80 border-r flex flex-col bg-muted/30">
      <div class="p-4 border-b space-y-3">
        <div class="flex items-center justify-between">
          <h2 class="font-semibold">Codemaps</h2>
          <Button
            variant="ghost"
            size="sm"
            on:click={handleRefresh}
            disabled={$codemapsQuery.isFetching}
          >
            <RefreshCw class="h-4 w-4 {$codemapsQuery.isFetching ? 'animate-spin' : ''}" />
          </Button>
        </div>
        <div class="relative">
          <Search class="absolute left-2.5 top-2.5 h-4 w-4 text-muted-foreground" />
          <Input
            placeholder="Filter codemaps..."
            bind:value={searchQuery}
            class="pl-8 h-9"
          />
        </div>
        <div>
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
      </div>

      <div class="flex-1 overflow-y-auto p-2 space-y-1">
        {#if $codemapsQuery.isLoading}
          <div class="flex items-center justify-center py-8">
            <RefreshCw class="h-5 w-5 animate-spin text-muted-foreground" />
          </div>
        {:else if filteredCodemaps.length === 0}
          <div class="text-center py-8 text-muted-foreground text-sm">
            {searchQuery ? "No matching codemaps" : "No codemaps found"}
          </div>
        {:else}
          {#each filteredCodemaps as codemap (codemap.id)}
            <button
              type="button"
              class="w-full p-3 rounded-md text-left hover:bg-muted transition-colors {selectedId === codemap.id ? 'bg-muted border border-primary/50' : ''}"
              on:click={() => selectCodemap(codemap.id)}
            >
              <div class="flex items-start gap-2">
                <MapIcon class="h-4 w-4 mt-0.5 shrink-0 text-primary" />
                <div class="flex-1 min-w-0">
                  <p class="font-medium text-sm truncate">{codemap.title}</p>
                  <p class="text-xs text-muted-foreground truncate mt-0.5">
                    {codemap.query}
                  </p>
                  <div class="flex items-center gap-2 mt-2">
                    <Badge variant="secondary" class="text-xs gap-1">
                      <FileCode class="h-3 w-3" />
                      {codemap.file_count} files
                    </Badge>
                    <Badge variant="outline" class="text-xs gap-1">
                      <Code2 class="h-3 w-3" />
                      {codemap.symbol_count} symbols
                    </Badge>
                  </div>
                </div>
              </div>
            </button>
          {/each}
        {/if}
      </div>

      <div class="border-t p-3">
        <p class="text-xs text-muted-foreground">
          {filteredCodemaps.length} codemap{filteredCodemaps.length !== 1 ? "s" : ""}
        </p>
      </div>
    </div>

    <!-- Detail view -->
    <div class="flex-1 overflow-y-auto">
      {#if !selectedId}
        <div class="flex items-center justify-center h-full">
          <div class="text-center">
            <MapIcon class="h-12 w-12 mx-auto mb-4 text-muted-foreground" />
            <h2 class="text-lg font-semibold mb-2">Codemaps</h2>
            <p class="text-muted-foreground">
              Select a codemap to view its traces and annotations
            </p>
          </div>
        </div>
      {:else if $codemapDetailQuery.isLoading}
        <div class="flex items-center justify-center h-full">
          <RefreshCw class="h-8 w-8 animate-spin text-muted-foreground" />
        </div>
      {:else if selectedCodemap}
        <div class="p-6 space-y-6">
          <!-- Header -->
          <div>
            <div class="flex items-center gap-2 mb-2">
              <MapIcon class="h-6 w-6 text-primary" />
              <h1 class="text-2xl font-bold">{selectedCodemap.title}</h1>
            </div>
            {#if selectedCodemap.description}
              <p class="text-muted-foreground">{selectedCodemap.description}</p>
            {/if}
          </div>

          <!-- Metadata -->
          <Card>
            <CardContent class="pt-6">
              <div class="grid grid-cols-2 md:grid-cols-4 gap-4">
                <div>
                  <p class="text-sm text-muted-foreground">Query</p>
                  <p class="font-medium">{selectedCodemap.query}</p>
                </div>
                <div>
                  <p class="text-sm text-muted-foreground">Files</p>
                  <p class="font-medium">{selectedCodemap.file_count}</p>
                </div>
                <div>
                  <p class="text-sm text-muted-foreground">Symbols</p>
                  <p class="font-medium">{selectedCodemap.symbol_count}</p>
                </div>
                <div>
                  <p class="text-sm text-muted-foreground">Created</p>
                  <p class="font-medium">{formatDate(selectedCodemap.created_at)}</p>
                </div>
              </div>
              {#if selectedCodemap.workspace}
                <div class="mt-4 pt-4 border-t">
                  <p class="text-sm text-muted-foreground">Workspace</p>
                  <div class="flex items-center gap-2 mt-1">
                    <Folder class="h-4 w-4 text-muted-foreground" />
                    <code class="text-sm font-mono">{selectedCodemap.workspace}</code>
                    <CopyButton text={selectedCodemap.workspace} />
                  </div>
                </div>
              {/if}
            </CardContent>
          </Card>

          <!-- Traces -->
          {#if selectedCodemap.traces && selectedCodemap.traces.length > 0}
            <div class="space-y-4">
              <h2 class="text-lg font-semibold">Traces ({selectedCodemap.traces.length})</h2>

              {#each selectedCodemap.traces as trace (trace.number)}
                <Card>
                  <CardHeader class="cursor-pointer" on:click={() => toggleTrace(trace.number)}>
                    <CardTitle class="flex items-center gap-2 text-base">
                      {#if expandedTraces.has(trace.number)}
                        <ChevronDown class="h-4 w-4" />
                      {:else}
                        <ChevronRight class="h-4 w-4" />
                      {/if}
                      <Badge variant="outline" class="text-xs">#{trace.number}</Badge>
                      {trace.title}
                    </CardTitle>
                  </CardHeader>

                  {#if expandedTraces.has(trace.number)}
                    <CardContent class="space-y-4">
                      {#if trace.summary}
                        <div>
                          <p class="text-sm text-muted-foreground mb-1">Summary</p>
                          <p class="text-sm">{trace.summary}</p>
                        </div>
                      {/if}

                      {#if trace.tree}
                        <div>
                          <p class="text-sm text-muted-foreground mb-1">Tree</p>
                          <pre class="text-xs bg-muted p-3 rounded-md overflow-x-auto font-mono">{trace.tree}</pre>
                        </div>
                      {/if}

                      {#if trace.annotations && trace.annotations.length > 0}
                        <div>
                          <p class="text-sm text-muted-foreground mb-2">
                            Annotations ({trace.annotations.length})
                          </p>
                          <div class="space-y-2">
                            {#each trace.annotations as annotation}
                              <div class="p-3 bg-muted/50 rounded-md">
                                <div class="flex items-center gap-2 mb-1">
                                  <Badge variant="secondary" class="text-xs gap-1">
                                    <Tag class="h-3 w-3" />
                                    {annotation.label}
                                  </Badge>
                                  <span class="font-medium text-sm">{annotation.title}</span>
                                </div>
                                {#if annotation.description}
                                  <p class="text-sm text-muted-foreground mb-2">
                                    {annotation.description}
                                  </p>
                                {/if}
                                {#if annotation.path}
                                  <div class="flex items-center gap-2">
                                    <FileCode class="h-3 w-3 text-muted-foreground" />
                                    <code class="text-xs font-mono text-muted-foreground">
                                      {annotation.path}
                                    </code>
                                    <CopyButton text={annotation.path} />
                                  </div>
                                {/if}
                              </div>
                            {/each}
                          </div>
                        </div>
                      {/if}
                    </CardContent>
                  {/if}
                </Card>
              {/each}
            </div>
          {:else}
            <Card>
              <CardContent class="py-8 text-center text-muted-foreground">
                No traces found for this codemap
              </CardContent>
            </Card>
          {/if}

          <!-- ID footer -->
          <div class="pt-4 border-t">
            <div class="flex items-center gap-2 text-xs text-muted-foreground">
              <span>ID:</span>
              <code class="font-mono">{selectedCodemap.id}</code>
              <CopyButton text={selectedCodemap.id} />
            </div>
          </div>
        </div>
      {/if}
    </div>
  </div>
</Layout>
