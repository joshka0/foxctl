import { useInsights, useTasks } from "@/api/hooks";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import { WorkspaceFilter } from "@/components/WorkspaceFilter";
import { RefreshCw, TrendingUp, GitBranch, AlertTriangle } from "lucide-react";

export function InsightsPage() {
  const { data: insights, isLoading, refetch, isFetching } = useInsights();
  const { data: tasksData } = useTasks({ limit: 200 });

  // Create a map of task IDs to titles
  const taskTitles = new Map(
    (tasksData?.tasks || []).map((t) => [t.id, t.title])
  );

  if (isLoading) {
    return (
      <div className="flex items-center justify-center py-16">
        <RefreshCw className="h-8 w-8 animate-spin text-muted-foreground" />
      </div>
    );
  }

  const nodes = insights?.nodes || [];
  const cycles = insights?.cycles || [];
  const topoOrder = insights?.topological_order || [];

  // Sort nodes by pagerank descending
  const sortedByPagerank = [...nodes].sort((a, b) => b.pagerank - a.pagerank);

  // Sort nodes by critical path score descending
  const sortedByCritical = [...nodes]
    .filter((n) => n.critical_path_score > 0)
    .sort((a, b) => b.critical_path_score - a.critical_path_score);

  // Get high-degree nodes (potential bottlenecks)
  const bottlenecks = [...nodes]
    .filter((n) => n.in_degree >= 2 || n.out_degree >= 2)
    .sort((a, b) => (b.in_degree + b.out_degree) - (a.in_degree + a.out_degree));

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <div className="flex items-center gap-4">
          <h1 className="text-2xl font-bold">Task Graph Insights</h1>
          <WorkspaceFilter />
        </div>
        <Button
          variant="outline"
          size="sm"
          onClick={() => refetch()}
          disabled={isFetching}
        >
          <RefreshCw className={`h-4 w-4 mr-2 ${isFetching ? "animate-spin" : ""}`} />
          Refresh
        </Button>
      </div>

      {/* Summary cards */}
      <div className="grid gap-4 md:grid-cols-4">
        <Card>
          <CardContent className="pt-6">
            <div className="flex items-center justify-between">
              <div>
                <p className="text-sm text-muted-foreground">Total Nodes</p>
                <div className="text-3xl font-bold">{nodes.length}</div>
              </div>
              <GitBranch className="h-8 w-8 text-muted-foreground" />
            </div>
          </CardContent>
        </Card>
        <Card>
          <CardContent className="pt-6">
            <div className="flex items-center justify-between">
              <div>
                <p className="text-sm text-muted-foreground">Critical Path Tasks</p>
                <div className="text-3xl font-bold">{sortedByCritical.length}</div>
              </div>
              <TrendingUp className="h-8 w-8 text-blue-500" />
            </div>
          </CardContent>
        </Card>
        <Card>
          <CardContent className="pt-6">
            <div className="flex items-center justify-between">
              <div>
                <p className="text-sm text-muted-foreground">Bottleneck Tasks</p>
                <div className="text-3xl font-bold">{bottlenecks.length}</div>
              </div>
              <AlertTriangle className="h-8 w-8 text-yellow-500" />
            </div>
          </CardContent>
        </Card>
        <Card>
          <CardContent className="pt-6">
            <div className="flex items-center justify-between">
              <div>
                <p className="text-sm text-muted-foreground">Cycles Detected</p>
                <div className={`text-3xl font-bold ${cycles.length > 0 ? "text-destructive" : "text-green-600"}`}>
                  {cycles.length}
                </div>
              </div>
              {cycles.length > 0 ? (
                <AlertTriangle className="h-8 w-8 text-destructive" />
              ) : (
                <Badge variant="success" className="text-xs">OK</Badge>
              )}
            </div>
          </CardContent>
        </Card>
      </div>

      <div className="grid gap-6 md:grid-cols-2">
        {/* Top PageRank tasks */}
        <Card>
          <CardHeader>
            <CardTitle className="flex items-center gap-2">
              <TrendingUp className="h-5 w-5" />
              Highest PageRank Tasks
            </CardTitle>
          </CardHeader>
          <CardContent>
            {sortedByPagerank.length === 0 ? (
              <p className="text-muted-foreground text-sm">No tasks with dependencies</p>
            ) : (
              <Table>
                <TableHeader>
                  <TableRow>
                    <TableHead>Task</TableHead>
                    <TableHead className="text-right">PageRank</TableHead>
                    <TableHead className="text-right">In/Out</TableHead>
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {sortedByPagerank.slice(0, 10).map((node) => (
                    <TableRow key={node.task_id}>
                      <TableCell>
                        <div className="flex flex-col">
                          <span className="font-medium text-sm">
                            {taskTitles.get(node.task_id) || "Unknown"}
                          </span>
                          <span className="text-xs text-muted-foreground font-mono">
                            {node.task_id.slice(0, 12)}...
                          </span>
                        </div>
                      </TableCell>
                      <TableCell className="text-right font-mono text-sm">
                        {(node.pagerank * 100).toFixed(2)}%
                      </TableCell>
                      <TableCell className="text-right text-sm">
                        <span className="text-green-600">{node.in_degree}</span>
                        {" / "}
                        <span className="text-blue-600">{node.out_degree}</span>
                      </TableCell>
                    </TableRow>
                  ))}
                </TableBody>
              </Table>
            )}
          </CardContent>
        </Card>

        {/* Critical path tasks */}
        <Card>
          <CardHeader>
            <CardTitle className="flex items-center gap-2">
              <GitBranch className="h-5 w-5" />
              Critical Path Tasks
            </CardTitle>
          </CardHeader>
          <CardContent>
            {sortedByCritical.length === 0 ? (
              <p className="text-muted-foreground text-sm">No tasks on critical path</p>
            ) : (
              <Table>
                <TableHeader>
                  <TableRow>
                    <TableHead>Task</TableHead>
                    <TableHead className="text-right">CP Score</TableHead>
                    <TableHead className="text-right">PageRank</TableHead>
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {sortedByCritical.slice(0, 10).map((node) => (
                    <TableRow key={node.task_id}>
                      <TableCell>
                        <div className="flex flex-col">
                          <span className="font-medium text-sm">
                            {taskTitles.get(node.task_id) || "Unknown"}
                          </span>
                          <span className="text-xs text-muted-foreground font-mono">
                            {node.task_id.slice(0, 12)}...
                          </span>
                        </div>
                      </TableCell>
                      <TableCell className="text-right">
                        <Badge variant="warning">{node.critical_path_score}</Badge>
                      </TableCell>
                      <TableCell className="text-right font-mono text-sm">
                        {(node.pagerank * 100).toFixed(2)}%
                      </TableCell>
                    </TableRow>
                  ))}
                </TableBody>
              </Table>
            )}
          </CardContent>
        </Card>
      </div>

      {/* Cycles warning */}
      {cycles.length > 0 && (
        <Card className="border-destructive">
          <CardHeader>
            <CardTitle className="flex items-center gap-2 text-destructive">
              <AlertTriangle className="h-5 w-5" />
              Dependency Cycles Detected
            </CardTitle>
          </CardHeader>
          <CardContent>
            <div className="space-y-2">
              {cycles.map((cycle, i) => (
                <div key={i} className="p-3 bg-destructive/10 rounded-md">
                  <p className="text-sm font-medium">Cycle {i + 1}:</p>
                  <div className="flex flex-wrap gap-1 mt-1">
                    {cycle.map((taskId, j) => (
                      <span key={taskId}>
                        <Badge variant="outline" className="font-mono text-xs">
                          {taskTitles.get(taskId) || taskId.slice(0, 8)}
                        </Badge>
                        {j < cycle.length - 1 && (
                          <span className="mx-1 text-muted-foreground">→</span>
                        )}
                      </span>
                    ))}
                  </div>
                </div>
              ))}
            </div>
          </CardContent>
        </Card>
      )}

      {/* Topological order */}
      {topoOrder.length > 0 && (
        <Card>
          <CardHeader>
            <CardTitle>Execution Order (Topological)</CardTitle>
          </CardHeader>
          <CardContent>
            <div className="flex flex-wrap gap-1">
              {topoOrder.slice(0, 30).map((taskId, i) => (
                <Badge key={taskId} variant="secondary" className="text-xs">
                  {i + 1}. {taskTitles.get(taskId) || taskId.slice(0, 8)}
                </Badge>
              ))}
              {topoOrder.length > 30 && (
                <Badge variant="outline" className="text-xs">
                  +{topoOrder.length - 30} more
                </Badge>
              )}
            </div>
          </CardContent>
        </Card>
      )}
    </div>
  );
}
