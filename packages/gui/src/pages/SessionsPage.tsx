import { useState, useMemo } from "react";
import { useSessions, useSessionMessages } from "@/api/hooks";
import { updateSessionMessage, getSessionMessages } from "@/api/client";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Input } from "@/components/ui/input";
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
import { WorkspaceFilter } from "@/components/WorkspaceFilter";
import {
  RefreshCw,
  MessageSquare,
  Search,
  ChevronLeft,
  ChevronRight,
  Edit2,
  Save,
  X,
  User,
  Bot,
  FileText,
  Wrench,
  List,
  Code,
  Maximize2,
  Download,
  Eye,
} from "lucide-react";
import type { Session, SessionMessage } from "@/types";

type ViewMode = "compact" | "detailed" | "json";

export function SessionsPage() {
  const [page, setPage] = useState(0);
  const [selectedSession, setSelectedSession] = useState<Session | null>(null);
  const [messageOffset, setMessageOffset] = useState(0);
  const [editingMessage, setEditingMessage] = useState<SessionMessage | null>(null);
  const [editContent, setEditContent] = useState("");
  const [viewMode, setViewMode] = useState<ViewMode>("compact");
  const [singleMessageIndex, setSingleMessageIndex] = useState<number | null>(null);
  const [allMessages, setAllMessages] = useState<SessionMessage[] | null>(null);
  const [loadingAll, setLoadingAll] = useState(false);
  const [searchPattern, setSearchPattern] = useState("");

  const limit = 20;
  const messageLimit = 50;

  const { data: sessionsData, isLoading, refetch, isFetching } = useSessions({
    limit,
    offset: page * limit,
  });

  const { data: messagesData, refetch: refetchMessages } = useSessionMessages(
    selectedSession?.id || "",
    { limit: messageLimit, offset: messageOffset }
  );

  const formatDate = (dateStr?: string) => {
    if (!dateStr) return "-";
    const date = new Date(dateStr);
    return date.toLocaleDateString() + " " + date.toLocaleTimeString();
  };

  const handleEditMessage = (msg: SessionMessage) => {
    setEditingMessage(msg);
    setEditContent(JSON.stringify(msg, null, 2));
  };

  const handleSaveMessage = async () => {
    if (!selectedSession || !editingMessage) return;

    try {
      const parsed = JSON.parse(editContent);
      await updateSessionMessage(selectedSession.id, editingMessage.index, parsed);
      setEditingMessage(null);
      refetchMessages();
      if (allMessages) {
        // Refresh all messages if we were viewing all
        loadAllMessages();
      }
    } catch (err) {
      alert("Invalid JSON: " + (err as Error).message);
    }
  };

  const loadAllMessages = async () => {
    if (!selectedSession) return;
    setLoadingAll(true);
    try {
      const result = await getSessionMessages(selectedSession.id, { limit: 10000, offset: 0 });
      setAllMessages(result.messages);
    } catch (err) {
      alert("Failed to load all messages: " + (err as Error).message);
    } finally {
      setLoadingAll(false);
    }
  };

  const openSession = (session: Session, startIndex = 0) => {
    setSelectedSession(session);
    setMessageOffset(Math.floor(startIndex / messageLimit) * messageLimit);
    setSingleMessageIndex(null);
    setAllMessages(null);
    setViewMode("compact");
    setSearchPattern("");
  };

  const getMessageContent = (msg: SessionMessage): string => {
    if (msg.summary) return msg.summary;
    if (msg.message?.content) {
      const content = msg.message.content;
      // Handle string content
      if (typeof content === "string") return content;
      // Handle array content
      if (Array.isArray(content)) {
        return content
          .map((c) => {
            if (typeof c === "string") return c;
            if (c.text) return c.text;
            if (c.name) return `[Tool: ${c.name}]`;
            return JSON.stringify(c);
          })
          .join("\n\n");
      }
      // Handle object content
      return JSON.stringify(content);
    }
    if (msg.error) return `Error: ${msg.error}`;
    if (msg.raw) return msg.raw;
    return "(no content)";
  };

  const getMessagePreview = (msg: SessionMessage) => {
    const content = getMessageContent(msg);
    return content.length > 300 ? content.slice(0, 300) + "..." : content;
  };

  const getMessageIcon = (msg: SessionMessage) => {
    if (msg.type === "user") return <User className="h-4 w-4 text-blue-500" />;
    if (msg.type === "assistant") return <Bot className="h-4 w-4 text-green-500" />;
    if (msg.type === "summary") return <FileText className="h-4 w-4 text-purple-500" />;
    if (msg.type === "tool_result") return <Wrench className="h-4 w-4 text-orange-500" />;
    return <MessageSquare className="h-4 w-4 text-muted-foreground" />;
  };

  const totalMessages = allMessages ? allMessages.length : messagesData?.total || 0;

  // Filter messages by search pattern (client-side regex)
  const currentMessages = useMemo(() => {
    const raw = allMessages || messagesData?.messages || [];
    if (!searchPattern.trim()) return raw;
    try {
      const regex = new RegExp(searchPattern, "i");
      return raw.filter((msg) => {
        const content = getMessageContent(msg);
        return regex.test(content) || regex.test(msg.type) || regex.test(String(msg.index));
      });
    } catch {
      // Invalid regex - return all
      return raw;
    }
  }, [allMessages, messagesData?.messages, searchPattern]);

  // Get single message for detailed one-at-a-time view
  const singleMessage = singleMessageIndex !== null
    ? currentMessages.find(m => m.index === singleMessageIndex)
    : null;

  if (isLoading) {
    return (
      <div className="flex items-center justify-center py-16">
        <RefreshCw className="h-8 w-8 animate-spin text-muted-foreground" />
      </div>
    );
  }

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <div className="flex items-center gap-4">
          <h1 className="text-2xl font-bold">Sessions</h1>
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

      {/* Sessions list */}
      <Card>
        <CardHeader>
          <CardTitle className="flex items-center justify-between">
            <span>Sessions ({sessionsData?.total || 0})</span>
            <div className="flex items-center gap-2">
              <Button
                variant="outline"
                size="sm"
                disabled={page === 0}
                onClick={() => setPage((p) => p - 1)}
              >
                <ChevronLeft className="h-4 w-4" />
              </Button>
              <span className="text-sm text-muted-foreground">
                Page {page + 1} of {Math.ceil((sessionsData?.total || 0) / limit)}
              </span>
              <Button
                variant="outline"
                size="sm"
                disabled={(page + 1) * limit >= (sessionsData?.total || 0)}
                onClick={() => setPage((p) => p + 1)}
              >
                <ChevronRight className="h-4 w-4" />
              </Button>
            </div>
          </CardTitle>
        </CardHeader>
        <CardContent>
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>Started</TableHead>
                <TableHead>Summary</TableHead>
                <TableHead>Messages</TableHead>
                <TableHead>Status</TableHead>
                <TableHead>Actions</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {sessionsData?.sessions.map((session) => (
                <TableRow key={session.id}>
                  <TableCell className="whitespace-nowrap">
                    {formatDate(session.started_at)}
                  </TableCell>
                  <TableCell className="max-w-md truncate">
                    {session.summary || session.id.slice(0, 12)}
                  </TableCell>
                  <TableCell>
                    <div className="flex items-center gap-1">
                      <MessageSquare className="h-4 w-4" />
                      {session.message_count}
                    </div>
                  </TableCell>
                  <TableCell>
                    <Badge
                      variant={
                        session.status === "ok"
                          ? "success"
                          : session.status === "error"
                          ? "destructive"
                          : "secondary"
                      }
                    >
                      {session.status}
                    </Badge>
                  </TableCell>
                  <TableCell>
                    <Button
                      variant="ghost"
                      size="sm"
                      onClick={() => openSession(session)}
                    >
                      View
                    </Button>
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        </CardContent>
      </Card>

      {/* Session viewer dialog */}
      <Dialog open={!!selectedSession} onOpenChange={() => setSelectedSession(null)}>
        <DialogContent className="max-w-5xl max-h-[90vh] overflow-hidden flex flex-col">
          <DialogHeader>
            <DialogTitle className="flex items-center gap-2">
              <MessageSquare className="h-5 w-5" />
              {selectedSession?.summary || selectedSession?.id.slice(0, 12)}
            </DialogTitle>
            <div className="flex items-center justify-between">
              <span className="text-sm text-muted-foreground">
                {formatDate(selectedSession?.started_at)} | {totalMessages} messages
                {allMessages && " (all loaded)"}
                {searchPattern && ` | ${currentMessages.length} matches`}
              </span>

              {/* View mode toggle */}
              <div className="flex items-center gap-1 border rounded-lg p-1">
                <Button
                  variant={viewMode === "compact" ? "secondary" : "ghost"}
                  size="sm"
                  onClick={() => { setViewMode("compact"); setSingleMessageIndex(null); }}
                  title="Compact view"
                >
                  <List className="h-4 w-4" />
                </Button>
                <Button
                  variant={viewMode === "detailed" ? "secondary" : "ghost"}
                  size="sm"
                  onClick={() => {
                    setViewMode("detailed");
                    if (singleMessageIndex === null && currentMessages.length > 0) {
                      setSingleMessageIndex(currentMessages[0].index);
                    }
                  }}
                  title="One at a time"
                >
                  <Eye className="h-4 w-4" />
                </Button>
                <Button
                  variant={viewMode === "json" ? "secondary" : "ghost"}
                  size="sm"
                  onClick={() => { setViewMode("json"); setSingleMessageIndex(null); }}
                  title="Full JSON"
                >
                  <Code className="h-4 w-4" />
                </Button>
                <div className="w-px h-6 bg-border mx-1" />
                <Button
                  variant="ghost"
                  size="sm"
                  onClick={loadAllMessages}
                  disabled={loadingAll || !!allMessages}
                  title="Load all messages"
                >
                  {loadingAll ? (
                    <RefreshCw className="h-4 w-4 animate-spin" />
                  ) : (
                    <Download className="h-4 w-4" />
                  )}
                </Button>
              </div>
            </div>

            {/* Search within session */}
            <div className="flex items-center gap-2 mt-2">
              <Search className="h-4 w-4 text-muted-foreground" />
              <Input
                placeholder="Regex search within session..."
                value={searchPattern}
                onChange={(e) => setSearchPattern(e.target.value)}
                className="h-8"
              />
              {searchPattern && (
                <Button
                  variant="ghost"
                  size="sm"
                  onClick={() => setSearchPattern("")}
                  className="h-8 w-8 p-0"
                >
                  <X className="h-4 w-4" />
                </Button>
              )}
            </div>
          </DialogHeader>

          {/* COMPACT VIEW */}
          {viewMode === "compact" && (
            <>
              <div className="flex-1 overflow-y-auto space-y-2 pr-2">
                {currentMessages.map((msg) => (
                  <div
                    key={msg.index}
                    className={`p-3 rounded-lg border cursor-pointer hover:ring-2 hover:ring-primary/50 ${
                      msg.type === "user"
                        ? "bg-blue-50 dark:bg-blue-950 border-blue-200 dark:border-blue-800"
                        : msg.type === "assistant"
                        ? "bg-green-50 dark:bg-green-950 border-green-200 dark:border-green-800"
                        : "bg-muted"
                    }`}
                    onClick={() => {
                      setSingleMessageIndex(msg.index);
                      setViewMode("detailed");
                    }}
                  >
                    <div className="flex items-center justify-between mb-1">
                      <div className="flex items-center gap-2">
                        {getMessageIcon(msg)}
                        <span className="text-sm font-medium capitalize">{msg.type}</span>
                        <span className="text-xs text-muted-foreground">#{msg.index}</span>
                        {msg.timestamp && (
                          <span className="text-xs text-muted-foreground">
                            {new Date(msg.timestamp).toLocaleTimeString()}
                          </span>
                        )}
                      </div>
                      <div className="flex gap-1">
                        <Button
                          variant="ghost"
                          size="sm"
                          onClick={(e) => {
                            e.stopPropagation();
                            setSingleMessageIndex(msg.index);
                            setViewMode("detailed");
                          }}
                        >
                          <Maximize2 className="h-3 w-3" />
                        </Button>
                        <Button
                          variant="ghost"
                          size="sm"
                          onClick={(e) => {
                            e.stopPropagation();
                            handleEditMessage(msg);
                          }}
                        >
                          <Edit2 className="h-3 w-3" />
                        </Button>
                      </div>
                    </div>
                    <pre className="text-sm whitespace-pre-wrap font-mono max-h-32 overflow-hidden">
                      {getMessagePreview(msg)}
                    </pre>
                  </div>
                ))}
              </div>

              {/* Pagination (only when not viewing all) */}
              {!allMessages && (
                <div className="flex items-center justify-between pt-4 border-t">
                  <Button
                    variant="outline"
                    size="sm"
                    disabled={messageOffset === 0}
                    onClick={() => setMessageOffset((o) => Math.max(0, o - messageLimit))}
                  >
                    <ChevronLeft className="h-4 w-4 mr-1" />
                    Previous
                  </Button>
                  <span className="text-sm text-muted-foreground">
                    {messageOffset + 1} - {Math.min(messageOffset + messageLimit, totalMessages)} of {totalMessages}
                  </span>
                  <Button
                    variant="outline"
                    size="sm"
                    disabled={messageOffset + messageLimit >= totalMessages}
                    onClick={() => setMessageOffset((o) => o + messageLimit)}
                  >
                    Next
                    <ChevronRight className="h-4 w-4 ml-1" />
                  </Button>
                </div>
              )}
            </>
          )}

          {/* DETAILED VIEW (one at a time) */}
          {viewMode === "detailed" && singleMessage && (
            <>
              <div className="flex-1 overflow-y-auto">
                <div
                  className={`p-4 rounded-lg border ${
                    singleMessage.type === "user"
                      ? "bg-blue-50 dark:bg-blue-950 border-blue-200 dark:border-blue-800"
                      : singleMessage.type === "assistant"
                      ? "bg-green-50 dark:bg-green-950 border-green-200 dark:border-green-800"
                      : "bg-muted"
                  }`}
                >
                  <div className="flex items-center justify-between mb-3">
                    <div className="flex items-center gap-3">
                      {getMessageIcon(singleMessage)}
                      <span className="text-lg font-medium capitalize">{singleMessage.type}</span>
                      <Badge variant="outline">#{singleMessage.index}</Badge>
                      {singleMessage.uuid && (
                        <span className="text-xs text-muted-foreground font-mono">
                          {singleMessage.uuid.slice(0, 8)}
                        </span>
                      )}
                    </div>
                    <div className="flex gap-2">
                      <Button
                        variant="outline"
                        size="sm"
                        onClick={() => handleEditMessage(singleMessage)}
                      >
                        <Edit2 className="h-4 w-4 mr-1" />
                        Edit
                      </Button>
                    </div>
                  </div>

                  {singleMessage.timestamp && (
                    <p className="text-sm text-muted-foreground mb-3">
                      {formatDate(singleMessage.timestamp)}
                    </p>
                  )}

                  <pre className="text-sm whitespace-pre-wrap font-mono bg-background/50 p-4 rounded border max-h-[50vh] overflow-y-auto">
                    {getMessageContent(singleMessage)}
                  </pre>

                  {/* Show tool calls if present */}
                  {singleMessage.message?.content?.some(c => c.name) && (
                    <div className="mt-4">
                      <h4 className="text-sm font-medium mb-2">Tool Calls:</h4>
                      <div className="space-y-2">
                        {singleMessage.message.content
                          .filter(c => c.name)
                          .map((tool, i) => (
                            <div key={i} className="p-2 bg-background/50 rounded border">
                              <div className="flex items-center gap-2">
                                <Wrench className="h-4 w-4 text-orange-500" />
                                <span className="font-mono text-sm">{tool.name}</span>
                              </div>
                              {tool.input != null && (
                                <pre className="text-xs mt-2 overflow-x-auto">
                                  {JSON.stringify(tool.input, null, 2)}
                                </pre>
                              )}
                            </div>
                          ))}
                      </div>
                    </div>
                  )}
                </div>
              </div>

              {/* Navigation */}
              <div className="flex items-center justify-between pt-4 border-t">
                <Button
                  variant="outline"
                  size="sm"
                  disabled={singleMessageIndex === 0}
                  onClick={() => {
                    const prevIdx = currentMessages.findIndex(m => m.index === singleMessageIndex) - 1;
                    if (prevIdx >= 0) {
                      setSingleMessageIndex(currentMessages[prevIdx].index);
                    } else if (!allMessages && messageOffset > 0) {
                      setMessageOffset(o => o - messageLimit);
                    }
                  }}
                >
                  <ChevronLeft className="h-4 w-4 mr-1" />
                  Previous
                </Button>
                <div className="flex items-center gap-4">
                  <span className="text-sm text-muted-foreground">
                    Message {singleMessageIndex! + 1} of {totalMessages}
                  </span>
                  <Input
                    type="number"
                    className="w-20"
                    min={0}
                    max={totalMessages - 1}
                    value={singleMessageIndex ?? 0}
                    onChange={(e) => {
                      const idx = parseInt(e.target.value);
                      if (idx >= 0 && idx < totalMessages) {
                        setSingleMessageIndex(idx);
                        if (!allMessages) {
                          setMessageOffset(Math.floor(idx / messageLimit) * messageLimit);
                        }
                      }
                    }}
                  />
                </div>
                <Button
                  variant="outline"
                  size="sm"
                  disabled={singleMessageIndex === totalMessages - 1}
                  onClick={() => {
                    const nextIdx = currentMessages.findIndex(m => m.index === singleMessageIndex) + 1;
                    if (nextIdx < currentMessages.length) {
                      setSingleMessageIndex(currentMessages[nextIdx].index);
                    } else if (!allMessages && messageOffset + messageLimit < totalMessages) {
                      setMessageOffset(o => o + messageLimit);
                    }
                  }}
                >
                  Next
                  <ChevronRight className="h-4 w-4 ml-1" />
                </Button>
              </div>
            </>
          )}

          {/* JSON VIEW (full dump) */}
          {viewMode === "json" && (
            <>
              <div className="flex-1 overflow-y-auto">
                <pre className="text-xs font-mono bg-muted p-4 rounded overflow-x-auto">
                  {JSON.stringify(currentMessages, null, 2)}
                </pre>
              </div>
              {!allMessages && (
                <p className="text-sm text-muted-foreground pt-2">
                  Showing {currentMessages.length} of {totalMessages} messages.
                  Click <Download className="h-4 w-4 inline" /> to load all.
                </p>
              )}
            </>
          )}
        </DialogContent>
      </Dialog>

      {/* Edit message dialog */}
      <Dialog open={!!editingMessage} onOpenChange={() => setEditingMessage(null)}>
        <DialogContent className="max-w-4xl max-h-[85vh] overflow-hidden flex flex-col">
          <DialogHeader>
            <DialogTitle>Edit Message #{editingMessage?.index}</DialogTitle>
          </DialogHeader>
          <textarea
            className="flex-1 min-h-[500px] p-3 font-mono text-sm border rounded resize-none bg-background"
            value={editContent}
            onChange={(e) => setEditContent(e.target.value)}
            spellCheck={false}
          />
          <div className="flex justify-between items-center pt-4">
            <span className="text-xs text-muted-foreground">
              Edit the JSON directly. Changes will be saved to the JSONL file.
            </span>
            <div className="flex gap-2">
              <Button variant="outline" onClick={() => setEditingMessage(null)}>
                <X className="h-4 w-4 mr-1" />
                Cancel
              </Button>
              <Button onClick={handleSaveMessage}>
                <Save className="h-4 w-4 mr-1" />
                Save
              </Button>
            </div>
          </div>
        </DialogContent>
      </Dialog>
    </div>
  );
}
