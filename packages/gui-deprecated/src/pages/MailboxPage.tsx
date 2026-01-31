import { useState } from "react";
import { useMailbox, useWorkspaces } from "@/api/hooks";
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
import { RefreshCw, Mail } from "lucide-react";
import type { MailboxMessage } from "@/types";

export function MailboxPage() {
  const [actorFilter, setActorFilter] = useState("");
  const [selectedMessage, setSelectedMessage] = useState<MailboxMessage | null>(null);
  const { data: workspacesData } = useWorkspaces();
  const currentWorkspace =
    workspacesData?.current ||
    workspacesData?.workspaces?.find((ws) => ws.is_active)?.path ||
    workspacesData?.workspaces?.[0]?.path;
  const { data, isLoading, refetch, isFetching } = useMailbox({
    actor: actorFilter || undefined,
    all: actorFilter === "",
    workspace: currentWorkspace,
    limit: 100,
  });

  const messages = data?.messages || [];

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <h1 className="text-2xl font-bold">Mailbox</h1>
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

      {/* Filter */}
      <Card>
        <CardContent className="pt-6">
          <div className="flex gap-4">
            <div className="w-64">
              <label className="text-sm font-medium mb-1 block">Filter by Actor</label>
              <Input
                placeholder="Leave empty for all, or: user, agent-123"
                value={actorFilter}
                onChange={(e) => setActorFilter(e.target.value)}
              />
            </div>
          </div>
        </CardContent>
      </Card>

      {/* Messages */}
      <Card>
        <CardHeader>
          <CardTitle className="flex items-center gap-2">
            <Mail className="h-5 w-5" />
            Messages ({messages.length})
          </CardTitle>
        </CardHeader>
        <CardContent>
          {isLoading ? (
            <div className="flex items-center justify-center py-8">
              <RefreshCw className="h-6 w-6 animate-spin text-muted-foreground" />
            </div>
          ) : messages.length === 0 ? (
            <div className="text-center py-8 text-muted-foreground">
              No messages found
            </div>
          ) : (
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
                {messages.map((msg) => (
                  <TableRow
                    key={msg.id}
                    className="cursor-pointer hover:bg-muted/50"
                    onClick={() => setSelectedMessage(msg)}
                  >
                    <TableCell>
                      <div className="flex flex-col">
                        <span className="font-medium">{msg.subject}</span>
                        {msg.body && (
                          <span className="text-xs text-muted-foreground">
                            {truncate(msg.body, 60)}
                          </span>
                        )}
                      </div>
                    </TableCell>
                    <TableCell className="font-mono text-xs">
                      {truncate(msg.sender, 30)}
                    </TableCell>
                    <TableCell>
                      <Badge variant="outline">{msg.kind}</Badge>
                    </TableCell>
                    <TableCell>{msg.priority}</TableCell>
                    <TableCell>
                      <Badge
                        variant={msg.status === "read" ? "secondary" : "default"}
                      >
                        {msg.status}
                      </Badge>
                    </TableCell>
                    <TableCell className="text-muted-foreground">
                      {formatRelativeTime(msg.created_at)}
                    </TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          )}
        </CardContent>
      </Card>

      {/* Message Detail Modal */}
      <Dialog open={!!selectedMessage} onOpenChange={(open) => !open && setSelectedMessage(null)}>
        <DialogContent className="max-w-2xl max-h-[80vh] overflow-y-auto">
          <DialogHeader>
            <DialogTitle className="flex items-center gap-2">
              <Mail className="h-5 w-5" />
              {selectedMessage?.subject}
            </DialogTitle>
          </DialogHeader>
          {selectedMessage && (
            <div className="space-y-4">
              {/* Metadata */}
              <div className="grid grid-cols-2 gap-4 text-sm">
                <div>
                  <span className="text-muted-foreground">From:</span>
                  <span className="ml-2 font-mono">{selectedMessage.sender}</span>
                </div>
                <div>
                  <span className="text-muted-foreground">To:</span>
                  <span className="ml-2 font-mono">{selectedMessage.recipient}</span>
                </div>
                <div>
                  <span className="text-muted-foreground">Kind:</span>
                  <Badge variant="outline" className="ml-2">{selectedMessage.kind}</Badge>
                </div>
                <div>
                  <span className="text-muted-foreground">Priority:</span>
                  <span className="ml-2">{selectedMessage.priority}</span>
                </div>
                <div>
                  <span className="text-muted-foreground">Status:</span>
                  <Badge
                    variant={selectedMessage.status === "read" ? "secondary" : "default"}
                    className="ml-2"
                  >
                    {selectedMessage.status}
                  </Badge>
                </div>
                <div>
                  <span className="text-muted-foreground">Created:</span>
                  <span className="ml-2">{formatRelativeTime(selectedMessage.created_at)}</span>
                </div>
                {selectedMessage.stream && (
                  <div>
                    <span className="text-muted-foreground">Stream:</span>
                    <span className="ml-2 font-mono">{selectedMessage.stream}</span>
                  </div>
                )}
                {selectedMessage.task_id && (
                  <div>
                    <span className="text-muted-foreground">Task ID:</span>
                    <span className="ml-2 font-mono text-xs">{selectedMessage.task_id}</span>
                  </div>
                )}
              </div>

              {/* Message Body */}
              <div className="border-t pt-4">
                <h4 className="text-sm font-medium text-muted-foreground mb-2">Message</h4>
                <div className="bg-muted/50 rounded-md p-4 whitespace-pre-wrap font-mono text-sm">
                  {selectedMessage.body || "(No message body)"}
                </div>
              </div>

              {/* Message ID */}
              <div className="border-t pt-4 text-xs text-muted-foreground">
                <span>ID: </span>
                <span className="font-mono">{selectedMessage.id}</span>
              </div>
            </div>
          )}
        </DialogContent>
      </Dialog>
    </div>
  );
}
