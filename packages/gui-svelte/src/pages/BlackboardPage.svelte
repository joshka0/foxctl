<script lang="ts">
  import { RefreshCw, Clipboard, Clock, Tag, FileJson } from "lucide-svelte";
  import Layout from "@/lib/components/layout/Layout.svelte";
  import { useBlackboard } from "@/lib/api/hooks";
  import {
    Badge,
    Button,
    Card,
    CardContent,
    CardHeader,
    CardTitle,
    Input,
    Select,
    Table,
    TableHeader,
    TableBody,
    TableRow,
    TableHead,
    TableCell,
    CopyButton,
  } from "@/lib/components/ui";

  let nsFilter = "";
  let topicFilter = "";
  let limitStr = "50";

  $: limit = parseInt(limitStr, 10);

  $: blackboardQuery = useBlackboard({
    ns: nsFilter || undefined,
    topic: topicFilter || undefined,
    limit,
  });

  $: records = $blackboardQuery.data?.records || [];

  function handleRefresh() {
    $blackboardQuery.refetch();
  }

  function formatTimestamp(ts: number): string {
    return new Date(ts * 1000).toLocaleString();
  }

  function formatTTL(ttlSec: number): string {
    if (ttlSec <= 0) return "No expiry";
    if (ttlSec < 60) return `${ttlSec}s`;
    if (ttlSec < 3600) return `${Math.floor(ttlSec / 60)}m`;
    if (ttlSec < 86400) return `${Math.floor(ttlSec / 3600)}h`;
    return `${Math.floor(ttlSec / 86400)}d`;
  }

  function truncatePayload(payload: string, maxLen = 100): string {
    if (payload.length <= maxLen) return payload;
    return payload.slice(0, maxLen) + "...";
  }

  function isValidJson(str: string): boolean {
    try {
      JSON.parse(str);
      return true;
    } catch {
      return false;
    }
  }

  function formatJson(str: string): string {
    try {
      return JSON.stringify(JSON.parse(str), null, 2);
    } catch {
      return str;
    }
  }

  let expandedId: string | null = null;

  function toggleExpand(id: string) {
    expandedId = expandedId === id ? null : id;
  }
</script>

<Layout>
  <div class="space-y-6">
    <div class="flex items-center justify-between">
      <h1 class="text-2xl font-bold">Blackboard</h1>
      <Button
        variant="outline"
        size="sm"
        on:click={handleRefresh}
        disabled={$blackboardQuery.isFetching}
      >
        <RefreshCw class="h-4 w-4 mr-2 {$blackboardQuery.isFetching ? 'animate-spin' : ''}" />
        Refresh
      </Button>
    </div>

    <!-- Filters -->
    <Card>
      <CardContent class="pt-6">
        <div class="flex gap-4 flex-wrap">
          <div class="w-48">
            <label for="ns-filter" class="text-sm font-medium mb-1 block">Namespace</label>
            <Input
              id="ns-filter"
              placeholder="Filter by namespace..."
              bind:value={nsFilter}
            />
          </div>
          <div class="w-48">
            <label for="topic-filter" class="text-sm font-medium mb-1 block">Topic</label>
            <Input
              id="topic-filter"
              placeholder="Filter by topic..."
              bind:value={topicFilter}
            />
          </div>
          <div class="w-32">
            <label for="limit-select" class="text-sm font-medium mb-1 block">Limit</label>
            <Select id="limit-select" bind:value={limitStr}>
              <option value="25">25</option>
              <option value="50">50</option>
              <option value="100">100</option>
              <option value="200">200</option>
            </Select>
          </div>
        </div>
      </CardContent>
    </Card>

    {#if $blackboardQuery.isLoading}
      <div class="flex items-center justify-center py-16">
        <RefreshCw class="h-8 w-8 animate-spin text-muted-foreground" />
      </div>
    {:else if records.length === 0}
      <Card>
        <CardContent class="py-16 text-center">
          <Clipboard class="h-12 w-12 mx-auto mb-4 text-muted-foreground" />
          <h3 class="text-lg font-semibold mb-2">No Records Found</h3>
          <p class="text-muted-foreground">
            {nsFilter || topicFilter
              ? "No records match your filters."
              : "The blackboard is empty."}
          </p>
        </CardContent>
      </Card>
    {:else}
      <Card>
        <CardHeader>
          <CardTitle class="flex items-center gap-2">
            <Clipboard class="h-5 w-5" />
            Records ({records.length})
          </CardTitle>
        </CardHeader>
        <CardContent>
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>Namespace</TableHead>
                <TableHead>Topic</TableHead>
                <TableHead>Payload</TableHead>
                <TableHead>TTL</TableHead>
                <TableHead>Timestamp</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {#each records as record (record.id)}
                <TableRow
                  class="cursor-pointer hover:bg-muted/50"
                  on:click={() => toggleExpand(record.id)}
                >
                  <TableCell>
                    <Badge variant="outline">{record.ns}</Badge>
                  </TableCell>
                  <TableCell>
                    <div class="flex items-center gap-1">
                      <Tag class="h-3 w-3 text-muted-foreground" />
                      <span class="text-sm">{record.topic}</span>
                    </div>
                  </TableCell>
                  <TableCell>
                    <div class="flex items-center gap-2">
                      {#if isValidJson(record.payload)}
                        <FileJson class="h-4 w-4 text-blue-500 shrink-0" />
                      {/if}
                      <span class="font-mono text-xs truncate max-w-[200px]">
                        {truncatePayload(record.payload)}
                      </span>
                    </div>
                  </TableCell>
                  <TableCell>
                    <div class="flex items-center gap-1">
                      <Clock class="h-3 w-3 text-muted-foreground" />
                      <span class="text-sm">{formatTTL(record.ttl_sec)}</span>
                    </div>
                  </TableCell>
                  <TableCell>
                    <span class="text-sm text-muted-foreground">
                      {formatTimestamp(record.ts)}
                    </span>
                  </TableCell>
                </TableRow>
                {#if expandedId === record.id}
                  <TableRow>
                    <TableCell colspan={5}>
                      <div class="p-4 bg-muted/50 rounded-md space-y-3">
                        <div class="flex items-center justify-between">
                          <span class="text-sm font-medium">Full Payload</span>
                          <CopyButton text={record.payload} />
                        </div>
                        <pre class="text-xs font-mono bg-zinc-900 text-zinc-100 p-3 rounded overflow-x-auto max-h-64">{isValidJson(record.payload) ? formatJson(record.payload) : record.payload}</pre>
                        <div class="grid grid-cols-2 gap-4 text-sm">
                          <div>
                            <span class="text-muted-foreground">ID:</span>
                            <span class="font-mono ml-2">{record.id}</span>
                          </div>
                          <div>
                            <span class="text-muted-foreground">Namespace:</span>
                            <span class="ml-2">{record.ns}</span>
                          </div>
                          <div>
                            <span class="text-muted-foreground">Topic:</span>
                            <span class="ml-2">{record.topic}</span>
                          </div>
                          <div>
                            <span class="text-muted-foreground">TTL:</span>
                            <span class="ml-2">{record.ttl_sec}s ({formatTTL(record.ttl_sec)})</span>
                          </div>
                        </div>
                      </div>
                    </TableCell>
                  </TableRow>
                {/if}
              {/each}
            </TableBody>
          </Table>
        </CardContent>
      </Card>
    {/if}
  </div>
</Layout>
