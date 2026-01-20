import { useState } from "react";
import { useBlackboard } from "@/api/hooks";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { formatRelativeTime, truncate } from "@/lib/utils";
import { RefreshCw, Clipboard, Clock, Lock } from "lucide-react";
import type { BlackboardRecord } from "@/types";

export function BlackboardPage() {
  const [nsFilter, setNsFilter] = useState("");
  const [topicFilter, setTopicFilter] = useState("");
  const [selectedRecord, setSelectedRecord] = useState<BlackboardRecord | null>(null);

  const { data, isLoading, refetch, isFetching } = useBlackboard({
    ns: nsFilter || undefined,
    topic: topicFilter || undefined,
    all: nsFilter === "" && topicFilter === "",
    limit: 100,
  });

  const records = data?.records || [];

  // Format timestamp to readable date
  const formatTimestamp = (ts: number) => {
    if (!ts) return "N/A";
    // ts is Unix seconds
    const date = new Date(ts * 1000);
    return date.toLocaleString();
  };

  // Check if record is expired
  const isExpired = (record: BlackboardRecord) => {
    if (!record.ttl_sec || record.ttl_sec <= 0) return false;
    const expiresAt = record.ts + record.ttl_sec;
    return Date.now() / 1000 > expiresAt;
  };

  // Check if record is leased
  const isLeased = (record: BlackboardRecord) => {
    if (!record.lease_by || !record.lease_exp) return false;
    return Date.now() / 1000 < record.lease_exp;
  };

  // Try to parse payload as JSON for pretty display
  const formatPayload = (payload: string) => {
    try {
      const parsed = JSON.parse(payload);
      return JSON.stringify(parsed, null, 2);
    } catch {
      return payload;
    }
  };

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <h1 className="text-2xl font-bold">Blackboard</h1>
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
            <div className="w-48">
              <label className="text-sm font-medium mb-1 block">Namespace</label>
              <Input
                placeholder="Leave empty for all, or: default"
                value={nsFilter}
                onChange={(e) => setNsFilter(e.target.value)}
              />
            </div>
            <div className="w-48">
              <label className="text-sm font-medium mb-1 block">Topic</label>
              <Input
                placeholder="Leave empty for all, or: *"
                value={topicFilter}
                onChange={(e) => setTopicFilter(e.target.value)}
              />
            </div>
          </div>
        </CardContent>
      </Card>

      {/* Records */}
      <Card>
        <CardHeader>
          <CardTitle className="flex items-center gap-2">
            <Clipboard className="h-5 w-5" />
            Records ({records.length})
          </CardTitle>
        </CardHeader>
        <CardContent>
          {isLoading ? (
            <div className="flex items-center justify-center py-8">
              <RefreshCw className="h-6 w-6 animate-spin text-muted-foreground" />
            </div>
          ) : records.length === 0 ? (
            <div className="text-center py-8 text-muted-foreground">
              <Clipboard className="h-12 w-12 mx-auto mb-4 opacity-50" />
              <p>No blackboard records found</p>
              <p className="text-sm mt-2">
                The blackboard is used for agent coordination and shared state.
              </p>
            </div>
          ) : (
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>Topic</TableHead>
                  <TableHead>Namespace</TableHead>
                  <TableHead>Payload</TableHead>
                  <TableHead>TTL</TableHead>
                  <TableHead>Status</TableHead>
                  <TableHead>Created</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {records.map((record) => (
                  <TableRow
                    key={record.id}
                    className="cursor-pointer hover:bg-muted/50"
                    onClick={() => setSelectedRecord(record)}
                  >
                    <TableCell className="font-mono text-sm">
                      {record.topic}
                    </TableCell>
                    <TableCell>
                      <Badge variant="outline">{record.ns}</Badge>
                    </TableCell>
                    <TableCell className="max-w-md">
                      <span className="text-xs text-muted-foreground font-mono">
                        {truncate(record.payload, 80)}
                      </span>
                    </TableCell>
                    <TableCell>
                      {record.ttl_sec > 0 ? (
                        <span className="flex items-center gap-1 text-sm">
                          <Clock className="h-3 w-3" />
                          {record.ttl_sec}s
                        </span>
                      ) : (
                        <span className="text-muted-foreground text-sm">-</span>
                      )}
                    </TableCell>
                    <TableCell>
                      <div className="flex gap-1">
                        {isExpired(record) && (
                          <Badge variant="destructive">Expired</Badge>
                        )}
                        {isLeased(record) && (
                          <Badge variant="secondary" className="flex items-center gap-1">
                            <Lock className="h-3 w-3" />
                            Leased
                          </Badge>
                        )}
                        {!isExpired(record) && !isLeased(record) && (
                          <Badge variant="default">Active</Badge>
                        )}
                      </div>
                    </TableCell>
                    <TableCell className="text-muted-foreground text-sm">
                      {formatRelativeTime(new Date(record.ts * 1000).toISOString())}
                    </TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          )}
        </CardContent>
      </Card>

      {/* Record Detail Modal */}
      <Dialog open={!!selectedRecord} onOpenChange={(open) => !open && setSelectedRecord(null)}>
        <DialogContent className="max-w-2xl max-h-[80vh] overflow-y-auto">
          <DialogHeader>
            <DialogTitle className="flex items-center gap-2">
              <Clipboard className="h-5 w-5" />
              {selectedRecord?.topic}
            </DialogTitle>
          </DialogHeader>
          {selectedRecord && (
            <div className="space-y-4">
              {/* Metadata */}
              <div className="grid grid-cols-2 gap-4 text-sm">
                <div>
                  <span className="text-muted-foreground">Namespace:</span>
                  <Badge variant="outline" className="ml-2">{selectedRecord.ns}</Badge>
                </div>
                <div>
                  <span className="text-muted-foreground">Topic:</span>
                  <span className="ml-2 font-mono">{selectedRecord.topic}</span>
                </div>
                <div>
                  <span className="text-muted-foreground">Created:</span>
                  <span className="ml-2">{formatTimestamp(selectedRecord.ts)}</span>
                </div>
                <div>
                  <span className="text-muted-foreground">TTL:</span>
                  <span className="ml-2">
                    {selectedRecord.ttl_sec > 0 ? `${selectedRecord.ttl_sec} seconds` : "No expiration"}
                  </span>
                </div>
                <div>
                  <span className="text-muted-foreground">Status:</span>
                  <span className="ml-2">
                    {isExpired(selectedRecord) ? (
                      <Badge variant="destructive">Expired</Badge>
                    ) : isLeased(selectedRecord) ? (
                      <Badge variant="secondary">Leased by {selectedRecord.lease_by}</Badge>
                    ) : (
                      <Badge variant="default">Active</Badge>
                    )}
                  </span>
                </div>
                {selectedRecord.cas_ref && (
                  <div>
                    <span className="text-muted-foreground">CAS Ref:</span>
                    <span className="ml-2 font-mono text-xs">{selectedRecord.cas_ref}</span>
                  </div>
                )}
              </div>

              {/* Lease Info */}
              {selectedRecord.lease_by && (
                <div className="border rounded-md p-3 bg-muted/30">
                  <h4 className="text-sm font-medium flex items-center gap-2 mb-2">
                    <Lock className="h-4 w-4" />
                    Lease Information
                  </h4>
                  <div className="grid grid-cols-2 gap-2 text-sm">
                    <div>
                      <span className="text-muted-foreground">Holder:</span>
                      <span className="ml-2 font-mono">{selectedRecord.lease_by}</span>
                    </div>
                    <div>
                      <span className="text-muted-foreground">Expires:</span>
                      <span className="ml-2">
                        {selectedRecord.lease_exp ? formatTimestamp(selectedRecord.lease_exp) : "N/A"}
                      </span>
                    </div>
                  </div>
                </div>
              )}

              {/* Payload */}
              <div className="border-t pt-4">
                <h4 className="text-sm font-medium text-muted-foreground mb-2">Payload</h4>
                <pre className="bg-muted/50 rounded-md p-4 overflow-x-auto text-sm font-mono whitespace-pre-wrap">
                  {formatPayload(selectedRecord.payload) || "(Empty payload)"}
                </pre>
              </div>

              {/* Record ID */}
              <div className="border-t pt-4 text-xs text-muted-foreground">
                <span>ID: </span>
                <span className="font-mono">{selectedRecord.id}</span>
              </div>
            </div>
          )}
        </DialogContent>
      </Dialog>
    </div>
  );
}
