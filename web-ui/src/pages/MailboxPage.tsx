import { useState } from "react";
import { useMailbox } from "@/api/hooks";
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
import { formatRelativeTime, truncate } from "@/lib/utils";
import { RefreshCw, Mail } from "lucide-react";

export function MailboxPage() {
  const [actorFilter, setActorFilter] = useState("");
  const { data, isLoading, refetch, isFetching } = useMailbox({
    actor: actorFilter || undefined,
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
                placeholder="actor:claude:main"
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
                  <TableRow key={msg.id}>
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
    </div>
  );
}
