import { useState } from "react";
import { useAgents } from "@/api/hooks";
import { startAgentDaemon } from "@/api/client";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Select } from "@/components/ui/select";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { WorkspaceFilter } from "@/components/WorkspaceFilter";
import { formatRelativeTime, truncate } from "@/lib/utils";
import { RefreshCw, Play } from "lucide-react";

const stateColors: Record<string, "default" | "success" | "warning" | "destructive" | "info" | "muted"> = {
  running: "info",
  starting: "warning",
  stopped: "muted",
  error: "destructive",
};

export function AgentsPage() {
  const [stateFilter, setStateFilter] = useState<string>("");
  const [limit, setLimit] = useState(50);
  const [actionMessage, setActionMessage] = useState<string | null>(null);
  const [startingId, setStartingId] = useState<string | null>(null);

  const { data, isLoading, refetch, isFetching } = useAgents({ state: stateFilter, limit });

  const agents = data?.agents || [];
  const total = data?.total ?? agents.length;

  async function handleStart(id: string) {
    setStartingId(id);
    setActionMessage(null);

    try {
      const result = await startAgentDaemon(id);
      const status = result.status || "ok";
      setActionMessage(`${result.actor_id}: ${status}`);
      await refetch();
    } catch (err) {
      const message = err instanceof Error ? err.message : "Failed to start daemon";
      setActionMessage(message);
    } finally {
      setStartingId(null);
    }
  }

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <div className="flex items-center gap-4">
          <h1 className="text-2xl font-bold">Agents</h1>
          <WorkspaceFilter />
        </div>
        <Button variant="outline" size="sm" onClick={() => refetch()} disabled={isFetching}>
          <RefreshCw className={`h-4 w-4 mr-2 ${isFetching ? "animate-spin" : ""}`} />
          Refresh
        </Button>
      </div>

      <Card>
        <CardContent className="pt-6">
          <div className="flex gap-4">
            <div className="w-40">
              <label htmlFor="agents-state" className="text-sm font-medium mb-1 block">State</label>
              <Select id="agents-state" value={stateFilter} onChange={(e) => setStateFilter(e.target.value)}>
                <option value="">All states</option>
                <option value="running">Running</option>
                <option value="starting">Starting</option>
                <option value="stopped">Stopped</option>
                <option value="error">Error</option>
              </Select>
            </div>
            <div className="w-32">
              <label htmlFor="agents-limit" className="text-sm font-medium mb-1 block">Limit</label>
              <Select id="agents-limit" value={String(limit)} onChange={(e) => setLimit(Number(e.target.value))}>
                <option value="25">25</option>
                <option value="50">50</option>
                <option value="100">100</option>
                <option value="200">200</option>
              </Select>
            </div>
          </div>
        </CardContent>
      </Card>

      {actionMessage && (
        <div className="rounded-lg border bg-card px-4 py-3 text-sm">
          {actionMessage}
        </div>
      )}

      <Card>
        <CardHeader>
          <CardTitle className="text-lg">
            {total} agent{total !== 1 ? "s" : ""}
          </CardTitle>
        </CardHeader>
        <CardContent>
          {isLoading ? (
            <div className="flex items-center justify-center py-8">
              <RefreshCw className="h-6 w-6 animate-spin text-muted-foreground" />
            </div>
          ) : agents.length === 0 ? (
            <div className="text-center py-8 text-muted-foreground">No agents found</div>
          ) : (
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>ID</TableHead>
                  <TableHead>Namespace</TableHead>
                  <TableHead>Role</TableHead>
                  <TableHead>State</TableHead>
                  <TableHead>Heartbeat</TableHead>
                  <TableHead>Created</TableHead>
                  <TableHead className="text-right">Actions</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {agents.map((agent) => (
                  <TableRow key={agent.id}>
                    <TableCell className="font-mono text-xs">{truncate(agent.id, 12)}</TableCell>
                    <TableCell className="font-mono text-xs" title={agent.ns}>
                      {truncate(agent.ns, 28)}
                    </TableCell>
                    <TableCell className="text-sm">{agent.role || "-"}</TableCell>
                    <TableCell>
                      <Badge variant={stateColors[agent.state] || "default"}>{agent.state}</Badge>
                    </TableCell>
                    <TableCell className="text-muted-foreground">
                      {agent.heartbeat_at ? formatRelativeTime(agent.heartbeat_at) : "-"}
                    </TableCell>
                    <TableCell className="text-muted-foreground">
                      {agent.created_at ? formatRelativeTime(agent.created_at) : "-"}
                    </TableCell>
                    <TableCell className="text-right">
                      <Button
                        variant="outline"
                        size="sm"
                        onClick={() => handleStart(agent.id)}
                        disabled={startingId === agent.id}
                      >
                        <Play className="h-4 w-4 mr-2" />
                        Start daemon
                      </Button>
                    </TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          )}
        </CardContent>
      </Card>
    </div>
  );
}
