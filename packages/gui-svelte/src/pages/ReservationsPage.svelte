<script lang="ts">
  import { RefreshCw, Lock, Unlock, Clock, User } from "lucide-svelte";
  import Layout from "@/lib/components/layout/Layout.svelte";
  import { useReservations, useWorkspaces } from "@/lib/api/hooks";
  import {
    Badge,
    Button,
    Card,
    CardContent,
    CardHeader,
    CardTitle,
    Table,
    TableHeader,
    TableBody,
    TableRow,
    TableHead,
    TableCell,
    Select,
  } from "@/lib/components/ui";

  let selectedWorkspace = "";

  const workspacesQuery = useWorkspaces();

  $: workspaces = $workspacesQuery.data?.workspaces || [];
  $: defaultWorkspace = resolveWorkspace($workspacesQuery.data?.current, workspaces);

  $: if (!selectedWorkspace && defaultWorkspace) {
    selectedWorkspace = defaultWorkspace;
  }

  $: reservationsQuery = useReservations({ workspace: selectedWorkspace || undefined });

  $: reservations = $reservationsQuery.data?.reservations || [];

  function handleRefresh() {
    $reservationsQuery.refetch();
  }

  function formatPath(path: string): string {
    // Show just the filename or last 50 chars
    if (path.length <= 50) return path;
    return "..." + path.slice(-47);
  }

  function formatExpiry(expiresAt: string): string {
    const expires = new Date(expiresAt);
    const now = new Date();
    const diffMs = expires.getTime() - now.getTime();

    if (diffMs < 0) return "Expired";

    const diffMinutes = Math.floor(diffMs / 60000);
    if (diffMinutes < 60) return `${diffMinutes}m`;

    const diffHours = Math.floor(diffMinutes / 60);
    return `${diffHours}h ${diffMinutes % 60}m`;
  }

  function isExpired(expiresAt: string): boolean {
    return new Date(expiresAt) < new Date();
  }

  function getModeVariant(mode: string): "default" | "secondary" | "warning" {
    switch (mode) {
      case "exclusive":
        return "warning";
      case "shared":
        return "secondary";
      default:
        return "default";
    }
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
      <h1 class="text-2xl font-bold">File Reservations</h1>
      <Button
        variant="outline"
        size="sm"
        on:click={handleRefresh}
        disabled={$reservationsQuery.isFetching}
      >
        <RefreshCw class="h-4 w-4 mr-2 {$reservationsQuery.isFetching ? 'animate-spin' : ''}" />
        Refresh
      </Button>
    </div>

    <Card>
      <CardContent class="pt-6">
        <div class="flex gap-4 flex-wrap">
          <div class="w-72">
            <label for="reservations-workspace" class="text-sm font-medium mb-1 block">Workspace</label>
            <Select id="reservations-workspace" bind:value={selectedWorkspace} disabled={workspaces.length === 0}>
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

    {#if $reservationsQuery.isLoading}
      <div class="flex items-center justify-center py-16">
        <RefreshCw class="h-8 w-8 animate-spin text-muted-foreground" />
      </div>
    {:else if reservations.length === 0}
      <Card>
        <CardContent class="py-16 text-center">
          <Unlock class="h-12 w-12 mx-auto mb-4 text-muted-foreground" />
          <h3 class="text-lg font-semibold mb-2">No Active Reservations</h3>
          <p class="text-muted-foreground">
            No files are currently locked by any agent.
          </p>
        </CardContent>
      </Card>
    {:else}
      <Card>
        <CardHeader>
          <CardTitle class="flex items-center gap-2">
            <Lock class="h-5 w-5" />
            Active Reservations ({reservations.length})
          </CardTitle>
        </CardHeader>
        <CardContent>
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>File Path</TableHead>
                <TableHead>Holder</TableHead>
                <TableHead>Mode</TableHead>
                <TableHead>Expires</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {#each reservations as reservation (reservation.id)}
                <TableRow class={isExpired(reservation.expires_at) ? "opacity-50" : ""}>
                  <TableCell>
                    <div class="flex items-center gap-2">
                      <Lock class="h-4 w-4 text-muted-foreground shrink-0" />
                      <span class="font-mono text-sm truncate" title={reservation.path}>
                        {formatPath(reservation.path)}
                      </span>
                    </div>
                  </TableCell>
                  <TableCell>
                    <div class="flex items-center gap-2">
                      <User class="h-4 w-4 text-muted-foreground" />
                      <span class="text-sm">{reservation.holder}</span>
                    </div>
                  </TableCell>
                  <TableCell>
                    <Badge variant={getModeVariant(reservation.mode)}>
                      {reservation.mode}
                    </Badge>
                  </TableCell>
                  <TableCell>
                    <div class="flex items-center gap-2">
                      <Clock class="h-4 w-4 text-muted-foreground" />
                      <span class="text-sm {isExpired(reservation.expires_at) ? 'text-destructive' : ''}">
                        {formatExpiry(reservation.expires_at)}
                      </span>
                    </div>
                  </TableCell>
                </TableRow>
              {/each}
            </TableBody>
          </Table>
        </CardContent>
      </Card>
    {/if}
  </div>
</Layout>
