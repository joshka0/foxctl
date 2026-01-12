<script lang="ts">
  import { RefreshCw } from "lucide-svelte";
  import Layout from "@/lib/components/layout/Layout.svelte";
  import { useAgents } from "@/lib/api/hooks";
  import { Badge, Button, Select, Card, CardContent, CardHeader, CardTitle } from "@/lib/components/ui";
  import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/lib/components/ui";
  import { formatRelativeTime } from "@/lib/utils/time";
  import { truncate } from "@/lib/utils/format";
  import type { AgentState } from "@agentctl/data";

  let stateFilter: AgentState | "" = "";
  let limitStr = "50";

  $: limit = parseInt(limitStr, 10);
  $: agentsQuery = useAgents({
    state: stateFilter || undefined,
    limit,
  });
  $: agents = $agentsQuery.data?.agents || [];
  $: total = $agentsQuery.data?.total ?? agents.length;

  type StateVariant = "default" | "success" | "warning" | "destructive" | "info" | "muted";

  const stateColors: Record<string, StateVariant> = {
    running: "info",
    starting: "warning",
    stopped: "muted",
    error: "destructive",
  };

  function handleRefresh() {
    $agentsQuery.refetch();
  }
</script>

<Layout>
  <div class="space-y-6">
    <div class="flex items-center justify-between">
      <h1 class="text-2xl font-bold">Agents</h1>
      <Button variant="outline" size="sm" on:click={handleRefresh} disabled={$agentsQuery.isFetching}>
        <RefreshCw class="h-4 w-4 mr-2 {$agentsQuery.isFetching ? 'animate-spin' : ''}" />
        Refresh
      </Button>
    </div>

    <!-- Filters -->
    <Card>
      <CardContent class="pt-6">
        <div class="flex gap-4">
          <div class="w-40">
            <label for="agent-state" class="text-sm font-medium mb-1 block">State</label>
            <Select id="agent-state" bind:value={stateFilter}>
              <option value="">All states</option>
              <option value="running">Running</option>
              <option value="starting">Starting</option>
              <option value="stopped">Stopped</option>
              <option value="error">Error</option>
            </Select>
          </div>
          <div class="w-32">
            <label for="agent-limit" class="text-sm font-medium mb-1 block">Limit</label>
            <Select id="agent-limit" bind:value={limitStr}>
              <option value="25">25</option>
              <option value="50">50</option>
              <option value="100">100</option>
              <option value="200">200</option>
            </Select>
          </div>
        </div>
      </CardContent>
    </Card>

    <!-- Agents list -->
    <Card>
      <CardHeader>
        <CardTitle class="text-lg">
          {total} agent{total !== 1 ? "s" : ""}
        </CardTitle>
      </CardHeader>
      <CardContent>
        {#if $agentsQuery.isLoading}
          <div class="flex items-center justify-center py-8">
            <RefreshCw class="h-6 w-6 animate-spin text-muted-foreground" />
          </div>
        {:else if agents.length === 0}
          <div class="text-center py-8 text-muted-foreground">
            No agents found
          </div>
        {:else}
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>ID</TableHead>
                <TableHead>Namespace</TableHead>
                <TableHead>Role</TableHead>
                <TableHead>State</TableHead>
                <TableHead>Heartbeat</TableHead>
                <TableHead>Created</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {#each agents as agent (agent.id)}
                <TableRow class="hover:bg-muted/50">
                  <TableCell class="font-mono text-xs">
                    {truncate(agent.id, 12)}
                  </TableCell>
                  <TableCell class="font-mono text-xs" title={agent.ns}>
                    {truncate(agent.ns || "", 28)}
                  </TableCell>
                  <TableCell class="text-sm">
                    {agent.role || "-"}
                  </TableCell>
                  <TableCell>
                    <Badge variant={stateColors[agent.state] || "default"}>
                      {agent.state}
                    </Badge>
                  </TableCell>
                  <TableCell class="text-muted-foreground">
                    {#if agent.heartbeat_at}
                      {formatRelativeTime(agent.heartbeat_at)}
                    {:else}
                      -
                    {/if}
                  </TableCell>
                  <TableCell class="text-muted-foreground">
                    {#if agent.created_at}
                      {formatRelativeTime(agent.created_at)}
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
