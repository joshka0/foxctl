import { useState } from "react";
import { useNavigate } from "react-router-dom";
import { useJobs } from "@/api/hooks";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Select } from "@/components/ui/select";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { WorkspaceFilter } from "@/components/WorkspaceFilter";
import { formatRelativeTime, truncate } from "@/lib/utils";
import { RefreshCw } from "lucide-react";

const stateColors: Record<string, "default" | "success" | "warning" | "destructive" | "info" | "muted"> = {
  completed: "success",
  running: "info",
  pending: "warning",
  failed: "destructive",
  cancelled: "muted",
};

export function JobsPage() {
  const navigate = useNavigate();
  const [stateFilter, setStateFilter] = useState<string>("");
  const [limit, setLimit] = useState(50);
  const { data, isLoading, refetch, isFetching } = useJobs({ state: stateFilter, limit });

  const jobs = data?.jobs || [];

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <div className="flex items-center gap-4">
          <h1 className="text-2xl font-bold">Jobs</h1>
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

      {/* Filters */}
      <Card>
        <CardContent className="pt-6">
          <div className="flex gap-4">
            <div className="w-40">
              <label className="text-sm font-medium mb-1 block">State</label>
              <Select
                value={stateFilter}
                onChange={(e) => setStateFilter(e.target.value)}
              >
                <option value="">All states</option>
                <option value="completed">Completed</option>
                <option value="running">Running</option>
                <option value="pending">Pending</option>
                <option value="failed">Failed</option>
              </Select>
            </div>
            <div className="w-32">
              <label className="text-sm font-medium mb-1 block">Limit</label>
              <Select
                value={String(limit)}
                onChange={(e) => setLimit(Number(e.target.value))}
              >
                <option value="25">25</option>
                <option value="50">50</option>
                <option value="100">100</option>
                <option value="200">200</option>
              </Select>
            </div>
          </div>
        </CardContent>
      </Card>

      {/* Jobs table */}
      <Card>
        <CardHeader>
          <CardTitle className="text-lg">
            {jobs.length} job{jobs.length !== 1 ? "s" : ""}
          </CardTitle>
        </CardHeader>
        <CardContent>
          {isLoading ? (
            <div className="flex items-center justify-center py-8">
              <RefreshCw className="h-6 w-6 animate-spin text-muted-foreground" />
            </div>
          ) : jobs.length === 0 ? (
            <div className="text-center py-8 text-muted-foreground">
              No jobs found
            </div>
          ) : (
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>ID</TableHead>
                  <TableHead>Command</TableHead>
                  <TableHead>State</TableHead>
                  <TableHead>Created</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {jobs.map((job) => (
                  <TableRow
                    key={job.id}
                    className="cursor-pointer hover:bg-muted/50"
                    onClick={() => navigate(`/jobs/${job.id}`)}
                  >
                    <TableCell className="font-mono text-xs">
                      {truncate(job.id, 12)}
                    </TableCell>
                    <TableCell>
                      <div className="flex flex-col">
                        <span className="font-medium">{job.skill || job.command}</span>
                        {job.category && (
                          <span className="text-xs text-muted-foreground">
                            {job.type}:{job.category}
                          </span>
                        )}
                      </div>
                    </TableCell>
                    <TableCell>
                      <Badge variant={stateColors[job.state] || "default"}>
                        {job.state}
                      </Badge>
                    </TableCell>
                    <TableCell className="text-muted-foreground">
                      {formatRelativeTime(job.created_at)}
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
