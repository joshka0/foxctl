<script lang="ts">
  import { RefreshCw, Mail } from "lucide-svelte";
  import Layout from "@/lib/components/layout/Layout.svelte";
  import { useMailbox, useWorkspaces } from "@/lib/api/hooks";
  import { Badge, Button, Input, Card, CardContent, CardHeader, CardTitle, Select } from "@/lib/components/ui";
  import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/lib/components/ui";
  import { formatRelativeTime } from "@/lib/utils/time";
  import { truncate } from "@/lib/utils/format";

  let actorFilter = "";
  let selectedWorkspace = "";

  const workspacesQuery = useWorkspaces();

  $: workspaces = $workspacesQuery.data?.workspaces || [];
  $: defaultWorkspace = resolveWorkspace($workspacesQuery.data?.current, workspaces);

  $: if (!selectedWorkspace && defaultWorkspace) {
    selectedWorkspace = defaultWorkspace;
  }

  $: mailboxQuery = useMailbox({
    actor: actorFilter || undefined,
    workspace: selectedWorkspace || undefined,
    limit: 100,
  });
  $: messages = $mailboxQuery.data?.messages || [];

  function handleRefresh() {
    $mailboxQuery.refetch();
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
  <div class="space-y-6">
    <div class="flex items-center justify-between">
      <h1 class="text-2xl font-bold">Mailbox</h1>
      <Button variant="outline" size="sm" on:click={handleRefresh} disabled={$mailboxQuery.isFetching}>
        <RefreshCw class="h-4 w-4 mr-2 {$mailboxQuery.isFetching ? 'animate-spin' : ''}" />
        Refresh
      </Button>
    </div>

    <!-- Filter -->
    <Card>
      <CardContent class="pt-6">
        <div class="flex gap-4">
          <div class="w-64">
            <label for="mailbox-actor" class="text-sm font-medium mb-1 block">Filter by Actor</label>
            <Input
              id="mailbox-actor"
              placeholder="actor:claude:main"
              bind:value={actorFilter}
            />
          </div>
          <div class="w-72">
            <label for="mailbox-workspace" class="text-sm font-medium mb-1 block">Workspace</label>
            <Select id="mailbox-workspace" bind:value={selectedWorkspace} disabled={workspaces.length === 0}>
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
      </CardContent>
    </Card>

    <!-- Messages -->
    <Card>
      <CardHeader>
        <CardTitle class="flex items-center gap-2">
          <Mail class="h-5 w-5" />
          Messages ({messages.length})
        </CardTitle>
      </CardHeader>
      <CardContent>
        {#if $mailboxQuery.isLoading}
          <div class="flex items-center justify-center py-8">
            <RefreshCw class="h-6 w-6 animate-spin text-muted-foreground" />
          </div>
        {:else if messages.length === 0}
          <div class="text-center py-8 text-muted-foreground">
            No messages found
          </div>
        {:else}
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>Subject</TableHead>
                <TableHead>Sender</TableHead>
                <TableHead>Kind</TableHead>
                <TableHead>Priority</TableHead>
                <TableHead>Status</TableHead>
                <TableHead>Created</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {#each messages as msg (msg.id)}
                <TableRow>
                  <TableCell>
                    <div class="flex flex-col">
                      <span class="font-medium">{msg.subject}</span>
                      {#if msg.body}
                        <span class="text-xs text-muted-foreground">
                          {truncate(msg.body, 60)}
                        </span>
                      {/if}
                    </div>
                  </TableCell>
                  <TableCell class="font-mono text-xs">
                    {truncate(msg.sender, 30)}
                  </TableCell>
                  <TableCell>
                    <Badge variant="outline">{msg.kind}</Badge>
                  </TableCell>
                  <TableCell>{msg.priority}</TableCell>
                  <TableCell>
                    <Badge variant={msg.status === "read" ? "secondary" : "default"}>
                      {msg.status}
                    </Badge>
                  </TableCell>
                  <TableCell class="text-muted-foreground">
                    {formatRelativeTime(msg.created_at)}
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
