import { Link } from "react-router-dom";
import { useTasks, useWorkspaces } from "@/api/hooks";
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
import { formatRelativeTime, truncate } from "@/lib/utils";
import { RefreshCw, CheckCircle2, Clock, PlayCircle, ExternalLink } from "lucide-react";

const statusConfig: Record<string, { variant: "default" | "success" | "warning" | "info"; icon: React.ReactNode }> = {
  pending: { variant: "warning", icon: <Clock className="h-3 w-3" /> },
  in_progress: { variant: "info", icon: <PlayCircle className="h-3 w-3" /> },
  completed: { variant: "success", icon: <CheckCircle2 className="h-3 w-3" /> },
};

export function TasksPage() {
  const { data: workspacesData } = useWorkspaces();
  const currentWorkspace =
    workspacesData?.current ||
    workspacesData?.workspaces?.find((ws) => ws.is_active)?.path ||
    workspacesData?.workspaces?.[0]?.path;

  const { data, isLoading, refetch, isFetching } = useTasks({
    limit: 100,
    workspace: currentWorkspace,
  });

  const tasks = data?.tasks || [];
  const stats = data?.stats || { total: 0, pending: 0, in_progress: 0, completed: 0 };

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <div className="flex items-center gap-4">
          <h1 className="text-2xl font-bold">Tasks</h1>
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

      {/* Stats cards */}
      <div className="grid gap-4 md:grid-cols-4">
        <Card>
          <CardContent className="pt-6">
            <div className="text-2xl font-bold">{stats.total}</div>
            <p className="text-sm text-muted-foreground">Total Tasks</p>
          </CardContent>
        </Card>
        <Card>
          <CardContent className="pt-6">
            <div className="text-2xl font-bold text-yellow-600">{stats.pending}</div>
            <p className="text-sm text-muted-foreground">Pending</p>
          </CardContent>
        </Card>
        <Card>
          <CardContent className="pt-6">
            <div className="text-2xl font-bold text-blue-600">{stats.in_progress}</div>
            <p className="text-sm text-muted-foreground">In Progress</p>
          </CardContent>
        </Card>
        <Card>
          <CardContent className="pt-6">
            <div className="text-2xl font-bold text-green-600">{stats.completed}</div>
            <p className="text-sm text-muted-foreground">Completed</p>
          </CardContent>
        </Card>
      </div>

      {/* Tasks table */}
      <Card>
        <CardHeader>
          <CardTitle className="text-lg">All Tasks</CardTitle>
        </CardHeader>
        <CardContent>
          {isLoading ? (
            <div className="flex items-center justify-center py-8">
              <RefreshCw className="h-6 w-6 animate-spin text-muted-foreground" />
            </div>
          ) : tasks.length === 0 ? (
            <div className="text-center py-8 text-muted-foreground">
              No tasks found
            </div>
          ) : (
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>Title</TableHead>
                  <TableHead>Status</TableHead>
                  <TableHead>Priority</TableHead>
                  <TableHead>PageRank</TableHead>
                  <TableHead>Created</TableHead>
                  <TableHead className="w-12"></TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {tasks.map((task) => {
                  const config = statusConfig[task.status || "pending"] || statusConfig.pending;
                  return (
                    <TableRow key={task.id} className="cursor-pointer hover:bg-muted/50">
                      <TableCell>
                        <Link to={`/tasks/${task.id}`} className="flex flex-col">
                          <span className="font-medium hover:underline">{task.title}</span>
                          {task.description && (
                            <span className="text-xs text-muted-foreground">
                              {truncate(task.description, 60)}
                            </span>
                          )}
                        </Link>
                      </TableCell>
                      <TableCell>
                        <Badge variant={config.variant} className="gap-1">
                          {config.icon}
                          {task.status || "pending"}
                        </Badge>
                      </TableCell>
                      <TableCell>{task.priority || 0}</TableCell>
                      <TableCell>
                        {task.pagerank ? (
                          <span className="font-mono text-xs">
                            {(task.pagerank * 100).toFixed(2)}%
                          </span>
                        ) : "-"}
                      </TableCell>
                      <TableCell className="text-muted-foreground">
                        {formatRelativeTime(task.created_at)}
                      </TableCell>
                      <TableCell>
                        <Link to={`/tasks/${task.id}`}>
                          <Button variant="ghost" size="icon" className="h-8 w-8">
                            <ExternalLink className="h-4 w-4" />
                          </Button>
                        </Link>
                      </TableCell>
                    </TableRow>
                  );
                })}
              </TableBody>
            </Table>
          )}
        </CardContent>
      </Card>
    </div>
  );
}
