import { useParams, Link } from "react-router-dom";
import { useTasks, useWorkspaces } from "@/api/hooks";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { formatDate, formatRelativeTime } from "@/lib/utils";
import { ArrowLeft, RefreshCw, CheckCircle2, Clock, PlayCircle, GitBranch, TrendingUp } from "lucide-react";

const statusConfig: Record<string, { variant: "default" | "success" | "warning" | "info"; icon: React.ReactNode; label: string }> = {
  pending: { variant: "warning", icon: <Clock className="h-4 w-4" />, label: "Pending" },
  in_progress: { variant: "info", icon: <PlayCircle className="h-4 w-4" />, label: "In Progress" },
  completed: { variant: "success", icon: <CheckCircle2 className="h-4 w-4" />, label: "Completed" },
};

export function TaskDetailPage() {
  const { id } = useParams<{ id: string }>();
  const { data: workspacesData } = useWorkspaces();
  const currentWorkspace =
    workspacesData?.current ||
    workspacesData?.workspaces?.find((ws) => ws.is_active)?.path ||
    workspacesData?.workspaces?.[0]?.path;

  // TODO: Add dedicated single-task API endpoint (GET /api/tasks/:id) to avoid
  // fetching all tasks. Currently we need all tasks for dependency resolution,
  // but the main task lookup could be optimized with a dedicated endpoint.
  const { data, isLoading, refetch, isFetching } = useTasks({
    workspace: currentWorkspace,
  });

  const task = data?.tasks?.find((t) => t.id === id);

  // Get dependent tasks (tasks that this task depends on)
  const dependentTasks = task?.depends_on?.map((depId) =>
    data?.tasks?.find((t) => t.id === depId)
  ).filter(Boolean) || [];

  // Get tasks that depend on this task
  const dependingTasks = data?.tasks?.filter((t) =>
    t.depends_on?.includes(id || "")
  ) || [];

  if (isLoading) {
    return (
      <div className="flex items-center justify-center py-16">
        <RefreshCw className="h-8 w-8 animate-spin text-muted-foreground" />
      </div>
    );
  }

  if (!task) {
    return (
      <div className="text-center py-16">
        <p className="text-muted-foreground">Task not found</p>
        <Link to="/tasks" className="mt-4 inline-block">
          <Button variant="outline">
            <ArrowLeft className="h-4 w-4 mr-2" />
            Back to Tasks
          </Button>
        </Link>
      </div>
    );
  }

  const config = statusConfig[task.status || "pending"] || statusConfig.pending;

  return (
    <div className="space-y-6">
      {/* Header */}
      <div className="flex items-center justify-between">
        <div className="flex items-center gap-4">
          <Link to="/tasks">
            <Button variant="outline" size="icon">
              <ArrowLeft className="h-4 w-4" />
            </Button>
          </Link>
          <div>
            <h1 className="text-2xl font-bold">{task.title}</h1>
            <p className="text-sm text-muted-foreground font-mono">{task.id}</p>
          </div>
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

      <div className="grid gap-6 md:grid-cols-2">
        {/* Status & Details */}
        <Card>
          <CardHeader>
            <CardTitle>Task Details</CardTitle>
          </CardHeader>
          <CardContent className="space-y-4">
            <div className="grid grid-cols-2 gap-4">
              <div>
                <p className="text-sm text-muted-foreground">Status</p>
                <Badge variant={config.variant} className="mt-1 gap-1">
                  {config.icon}
                  {config.label}
                </Badge>
              </div>
              <div>
                <p className="text-sm text-muted-foreground">Priority</p>
                <p className="font-medium text-lg">{task.priority || 0}</p>
              </div>
              <div>
                <p className="text-sm text-muted-foreground">Created</p>
                <p className="font-medium">{formatDate(task.created_at)}</p>
                <p className="text-xs text-muted-foreground">{formatRelativeTime(task.created_at)}</p>
              </div>
              {task.completed_at && (
                <div>
                  <p className="text-sm text-muted-foreground">Completed</p>
                  <p className="font-medium">{formatDate(task.completed_at)}</p>
                  <p className="text-xs text-muted-foreground">{formatRelativeTime(task.completed_at)}</p>
                </div>
              )}
            </div>

            {task.description && (
              <div>
                <p className="text-sm text-muted-foreground mb-2">Description</p>
                <p className="text-sm bg-muted p-3 rounded-md whitespace-pre-wrap">
                  {task.description}
                </p>
              </div>
            )}

            {task.notes && (
              <div>
                <p className="text-sm text-muted-foreground mb-2">Notes</p>
                <p className="text-sm bg-muted p-3 rounded-md whitespace-pre-wrap">
                  {task.notes}
                </p>
              </div>
            )}
          </CardContent>
        </Card>

        {/* Graph Metrics */}
        <Card>
          <CardHeader>
            <CardTitle className="flex items-center gap-2">
              <TrendingUp className="h-5 w-5" />
              Graph Metrics
            </CardTitle>
          </CardHeader>
          <CardContent>
            <div className="grid grid-cols-2 gap-4">
              <div className="p-4 bg-muted rounded-lg text-center">
                <p className="text-2xl font-bold text-primary">
                  {task.pagerank ? (task.pagerank * 100).toFixed(2) : "0.00"}%
                </p>
                <p className="text-sm text-muted-foreground">PageRank</p>
              </div>
              <div className="p-4 bg-muted rounded-lg text-center">
                <p className="text-2xl font-bold text-blue-600">
                  {task.critical_path_score || 0}
                </p>
                <p className="text-sm text-muted-foreground">Critical Path Score</p>
              </div>
              <div className="p-4 bg-muted rounded-lg text-center">
                <p className="text-2xl font-bold text-green-600">
                  {task.in_degree || 0}
                </p>
                <p className="text-sm text-muted-foreground">Dependencies</p>
              </div>
              <div className="p-4 bg-muted rounded-lg text-center">
                <p className="text-2xl font-bold text-orange-600">
                  {task.out_degree || 0}
                </p>
                <p className="text-sm text-muted-foreground">Dependents</p>
              </div>
            </div>
            <div className="mt-4 p-3 bg-blue-50 dark:bg-blue-950 rounded-md">
              <p className="text-sm font-medium text-blue-800 dark:text-blue-200">Score</p>
              <p className="text-lg font-bold text-blue-600">{task.score?.toFixed(2) || "0.00"}</p>
            </div>
          </CardContent>
        </Card>
      </div>

      {/* Dependencies */}
      {dependentTasks.length > 0 && (
        <Card>
          <CardHeader>
            <CardTitle className="flex items-center gap-2">
              <GitBranch className="h-5 w-5" />
              This task depends on ({dependentTasks.length})
            </CardTitle>
          </CardHeader>
          <CardContent>
            <div className="space-y-2">
              {dependentTasks.map((dep) => dep && (
                <Link
                  key={dep.id}
                  to={`/tasks/${dep.id}`}
                  className="flex items-center justify-between p-3 bg-muted rounded-md hover:bg-muted/80 transition-colors"
                >
                  <div>
                    <p className="font-medium">{dep.title}</p>
                    <p className="text-xs text-muted-foreground font-mono">{dep.id}</p>
                  </div>
                  <Badge variant={statusConfig[dep.status || "pending"]?.variant || "default"}>
                    {dep.status || "pending"}
                  </Badge>
                </Link>
              ))}
            </div>
          </CardContent>
        </Card>
      )}

      {/* Dependent tasks */}
      {dependingTasks.length > 0 && (
        <Card>
          <CardHeader>
            <CardTitle className="flex items-center gap-2">
              <GitBranch className="h-5 w-5 rotate-180" />
              Tasks depending on this ({dependingTasks.length})
            </CardTitle>
          </CardHeader>
          <CardContent>
            <div className="space-y-2">
              {dependingTasks.map((dep) => (
                <Link
                  key={dep.id}
                  to={`/tasks/${dep.id}`}
                  className="flex items-center justify-between p-3 bg-muted rounded-md hover:bg-muted/80 transition-colors"
                >
                  <div>
                    <p className="font-medium">{dep.title}</p>
                    <p className="text-xs text-muted-foreground font-mono">{dep.id}</p>
                  </div>
                  <Badge variant={statusConfig[dep.status || "pending"]?.variant || "default"}>
                    {dep.status || "pending"}
                  </Badge>
                </Link>
              ))}
            </div>
          </CardContent>
        </Card>
      )}
    </div>
  );
}
