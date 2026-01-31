import { useStats } from "@/api/hooks";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Button } from "@/components/ui/button";
import { WorkspaceFilter } from "@/components/WorkspaceFilter";
import { RefreshCw, TrendingUp, Clock, CheckCircle2, XCircle } from "lucide-react";

export function StatsPage() {
  const { data: stats, isLoading, refetch, isFetching } = useStats();

  if (isLoading) {
    return (
      <div className="flex items-center justify-center py-16">
        <RefreshCw className="h-8 w-8 animate-spin text-muted-foreground" />
      </div>
    );
  }

  const stateOrder = ["completed", "running", "pending", "failed"];
  const sortedStates = Object.entries(stats?.by_state || {}).sort(
    ([a], [b]) => stateOrder.indexOf(a) - stateOrder.indexOf(b)
  );

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <div className="flex items-center gap-4">
          <h1 className="text-2xl font-bold">Statistics</h1>
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

      {/* Overview cards */}
      <div className="grid gap-4 md:grid-cols-4">
        <Card>
          <CardContent className="pt-6">
            <div className="flex items-center justify-between">
              <div>
                <p className="text-sm text-muted-foreground">Total Jobs</p>
                <div className="text-3xl font-bold">{stats?.total || 0}</div>
              </div>
              <TrendingUp className="h-8 w-8 text-muted-foreground" />
            </div>
          </CardContent>
        </Card>
        <Card>
          <CardContent className="pt-6">
            <div className="flex items-center justify-between">
              <div>
                <p className="text-sm text-muted-foreground">Last Hour</p>
                <div className="text-3xl font-bold">{stats?.recent?.last_hour || 0}</div>
              </div>
              <Clock className="h-8 w-8 text-blue-500" />
            </div>
          </CardContent>
        </Card>
        <Card>
          <CardContent className="pt-6">
            <div className="flex items-center justify-between">
              <div>
                <p className="text-sm text-muted-foreground">Last Day</p>
                <div className="text-3xl font-bold">{stats?.recent?.last_day || 0}</div>
              </div>
              <CheckCircle2 className="h-8 w-8 text-green-500" />
            </div>
          </CardContent>
        </Card>
        <Card>
          <CardContent className="pt-6">
            <div className="flex items-center justify-between">
              <div>
                <p className="text-sm text-muted-foreground">Failed</p>
                <div className="text-3xl font-bold text-destructive">
                  {stats?.by_state?.failed || 0}
                </div>
              </div>
              <XCircle className="h-8 w-8 text-destructive" />
            </div>
          </CardContent>
        </Card>
      </div>

      <div className="grid gap-6 md:grid-cols-2">
        {/* By state */}
        <Card>
          <CardHeader>
            <CardTitle>Jobs by State</CardTitle>
          </CardHeader>
          <CardContent>
            <div className="space-y-4">
              {sortedStates.map(([state, count]) => {
                const percentage = stats?.total ? Math.round((count / stats.total) * 100) : 0;
                const colors: Record<string, string> = {
                  completed: "bg-green-500",
                  running: "bg-blue-500",
                  pending: "bg-yellow-500",
                  failed: "bg-red-500",
                };
                return (
                  <div key={state} className="space-y-1">
                    <div className="flex justify-between text-sm">
                      <span className="capitalize">{state}</span>
                      <span className="text-muted-foreground">{count} ({percentage}%)</span>
                    </div>
                    <div className="h-2 rounded-full bg-muted">
                      <div
                        className={`h-full rounded-full ${colors[state] || "bg-primary"}`}
                        style={{ width: `${percentage}%` }}
                      />
                    </div>
                  </div>
                );
              })}
            </div>
          </CardContent>
        </Card>

        {/* Top commands */}
        <Card>
          <CardHeader>
            <CardTitle>Top Commands</CardTitle>
          </CardHeader>
          <CardContent>
            <div className="space-y-2">
              {Object.entries(stats?.by_command || {})
                .sort(([, a], [, b]) => b - a)
                .slice(0, 10)
                .map(([command, count]) => (
                  <div key={command} className="flex justify-between items-center py-2 border-b last:border-0">
                    <span className="font-mono text-sm truncate max-w-[200px]">{command}</span>
                    <span className="text-muted-foreground">{count}</span>
                  </div>
                ))}
            </div>
          </CardContent>
        </Card>
      </div>
    </div>
  );
}
