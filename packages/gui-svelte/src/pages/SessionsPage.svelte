<script lang="ts">
  import { RefreshCw, MessageSquare, ChevronLeft, ChevronRight } from "lucide-svelte";
  import Layout from "@/lib/components/layout/Layout.svelte";
  import { useSessions } from "@/lib/api/hooks";
  import { Badge, Button, Card, CardContent, CardHeader, CardTitle } from "@/lib/components/ui";
  import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/lib/components/ui";
  import { formatRelativeTime } from "@/lib/utils/time";
  import { truncate } from "@/lib/utils/format";

  let page = 0;
  const limit = 20;

  $: sessionsQuery = useSessions({ limit, offset: page * limit });
  $: sessions = $sessionsQuery.data?.sessions || [];
  $: total = $sessionsQuery.data?.total || 0;
  $: totalPages = Math.ceil(total / limit);

  type StatusVariant = "default" | "success" | "destructive" | "secondary";

  function getStatusVariant(status: string | undefined): StatusVariant {
    switch (status) {
      case "ok":
        return "success";
      case "error":
        return "destructive";
      default:
        return "secondary";
    }
  }

  function handleRefresh() {
    $sessionsQuery.refetch();
  }

  function handlePrevPage() {
    if (page > 0) page--;
  }

  function handleNextPage() {
    if ((page + 1) * limit < total) page++;
  }

  function formatDate(dateStr: string | undefined): string {
    if (!dateStr) return "-";
    try {
      const d = new Date(dateStr);
      return d.toLocaleDateString() + " " + d.toLocaleTimeString();
    } catch {
      return dateStr;
    }
  }
</script>

<Layout>
  <div class="space-y-6">
    <div class="flex items-center justify-between">
      <h1 class="text-2xl font-bold">Sessions</h1>
      <Button variant="outline" size="sm" on:click={handleRefresh} disabled={$sessionsQuery.isFetching}>
        <RefreshCw class="h-4 w-4 mr-2 {$sessionsQuery.isFetching ? 'animate-spin' : ''}" />
        Refresh
      </Button>
    </div>

    <Card>
      <CardHeader>
        <CardTitle class="flex items-center justify-between">
          <span>Sessions ({total})</span>
          <div class="flex items-center gap-2">
            <Button variant="outline" size="sm" disabled={page === 0} on:click={handlePrevPage}>
              <ChevronLeft class="h-4 w-4" />
            </Button>
            <span class="text-sm text-muted-foreground">
              Page {page + 1} of {totalPages || 1}
            </span>
            <Button variant="outline" size="sm" disabled={page + 1 >= totalPages} on:click={handleNextPage}>
              <ChevronRight class="h-4 w-4" />
            </Button>
          </div>
        </CardTitle>
      </CardHeader>
      <CardContent>
        {#if $sessionsQuery.isLoading}
          <div class="flex items-center justify-center py-8">
            <RefreshCw class="h-6 w-6 animate-spin text-muted-foreground" />
          </div>
        {:else if sessions.length === 0}
          <div class="text-center py-8 text-muted-foreground">
            No sessions found
          </div>
        {:else}
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>Started</TableHead>
                <TableHead>Summary</TableHead>
                <TableHead>Messages</TableHead>
                <TableHead>Status</TableHead>
                <TableHead>Ended</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {#each sessions as session (session.id)}
                <TableRow class="hover:bg-muted/50">
                  <TableCell class="whitespace-nowrap">
                    <div class="flex flex-col">
                      <span class="text-sm">{formatDate(session.started_at)}</span>
                      <span class="text-xs text-muted-foreground">{formatRelativeTime(session.started_at)}</span>
                    </div>
                  </TableCell>
                  <TableCell class="max-w-md">
                    <div class="flex flex-col">
                      <span class="font-medium">{truncate(session.summary || session.id, 50)}</span>
                      {#if session.workspace_path}
                        <span class="text-xs text-muted-foreground font-mono">{truncate(session.workspace_path, 40)}</span>
                      {/if}
                    </div>
                  </TableCell>
                  <TableCell>
                    <div class="flex items-center gap-1">
                      <MessageSquare class="h-4 w-4 text-muted-foreground" />
                      <span>{session.message_count || 0}</span>
                    </div>
                  </TableCell>
                  <TableCell>
                    <Badge variant={getStatusVariant(session.status)}>
                      {session.status || "active"}
                    </Badge>
                  </TableCell>
                  <TableCell class="text-muted-foreground text-sm">
                    {#if session.ended_at}
                      {formatRelativeTime(session.ended_at)}
                    {:else}
                      -
                    {/if}
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
