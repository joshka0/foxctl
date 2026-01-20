import { useState } from "react";
import { useCodemaps, useCodemap, useWorkspaces } from "@/api/hooks";
import { deleteCodemap } from "@/api/client";
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
import {
  Accordion,
  AccordionContent,
  AccordionItem,
  AccordionTrigger,
} from "@/components/ui/accordion";
import { WorkspaceFilter } from "@/components/WorkspaceFilter";
import {
  RefreshCw,
  Map,
  Search,
  ChevronLeft,
  ChevronRight,
  Trash2,
  FileCode,
  Hash,
  FolderTree,
  Tag,
  Code,
} from "lucide-react";

export function CodemapsPage() {
  const [page, setPage] = useState(0);
  const [selectedCodemapId, setSelectedCodemapId] = useState<string | null>(null);
  const [searchQuery, setSearchQuery] = useState("");

  const limit = 20;

  const { data: workspacesData } = useWorkspaces();
  const currentWorkspace =
    workspacesData?.current ||
    workspacesData?.workspaces?.find((ws) => ws.is_active)?.path ||
    workspacesData?.workspaces?.[0]?.path;

  const { data: codemapsData, isLoading, refetch, isFetching } = useCodemaps({
    limit,
    workspace: currentWorkspace,
  });

  const { data: codemapDetail, isLoading: isLoadingDetail } = useCodemap(
    selectedCodemapId || "",
    currentWorkspace
  );

  const formatDate = (dateStr?: string) => {
    if (!dateStr) return "-";
    const date = new Date(dateStr);
    return date.toLocaleDateString() + " " + date.toLocaleTimeString();
  };

  const handleDelete = async (id: string) => {
    if (!confirm("Delete this codemap?")) return;
    try {
      await deleteCodemap(id);
      refetch();
      if (selectedCodemapId === id) {
        setSelectedCodemapId(null);
      }
    } catch (err) {
      alert("Failed to delete: " + (err as Error).message);
    }
  };

  // Filter codemaps by search query (client-side)
  const filteredCodemaps = codemapsData?.codemaps.filter((cm) => {
    if (!searchQuery.trim()) return true;
    const query = searchQuery.toLowerCase();
    return (
      cm.title.toLowerCase().includes(query) ||
      cm.query.toLowerCase().includes(query) ||
      cm.id.toLowerCase().includes(query)
    );
  }) || [];

  // Pagination (client-side for now)
  const totalCodemaps = filteredCodemaps.length;
  const pagedCodemaps = filteredCodemaps.slice(page * limit, (page + 1) * limit);
  const totalPages = Math.ceil(totalCodemaps / limit);

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
          <h1 className="text-2xl font-bold">Codemaps</h1>
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
      <div className="grid grid-cols-1 md:grid-cols-3 gap-4">
        <Card>
          <CardHeader className="flex flex-row items-center justify-between space-y-0 pb-2">
            <CardTitle className="text-sm font-medium">Total Codemaps</CardTitle>
            <Map className="h-4 w-4 text-muted-foreground" />
          </CardHeader>
          <CardContent>
            <div className="text-2xl font-bold">{codemapsData?.codemaps.length || 0}</div>
          </CardContent>
        </Card>
        <Card>
          <CardHeader className="flex flex-row items-center justify-between space-y-0 pb-2">
            <CardTitle className="text-sm font-medium">Total Files</CardTitle>
            <FileCode className="h-4 w-4 text-muted-foreground" />
          </CardHeader>
          <CardContent>
            <div className="text-2xl font-bold">
              {codemapsData?.codemaps.reduce((sum, cm) => sum + cm.file_count, 0) || 0}
            </div>
          </CardContent>
        </Card>
        <Card>
          <CardHeader className="flex flex-row items-center justify-between space-y-0 pb-2">
            <CardTitle className="text-sm font-medium">Total Symbols</CardTitle>
            <Hash className="h-4 w-4 text-muted-foreground" />
          </CardHeader>
          <CardContent>
            <div className="text-2xl font-bold">
              {codemapsData?.codemaps.reduce((sum, cm) => sum + cm.symbol_count, 0) || 0}
            </div>
          </CardContent>
        </Card>
      </div>

      {/* Search */}
      <div className="flex items-center gap-2">
        <Search className="h-4 w-4 text-muted-foreground" />
        <Input
          placeholder="Filter codemaps by title or query..."
          value={searchQuery}
          onChange={(e) => {
            setSearchQuery(e.target.value);
            setPage(0);
          }}
          className="max-w-sm"
        />
      </div>

      {/* Codemaps list */}
      <Card>
        <CardHeader>
          <CardTitle className="flex items-center justify-between">
            <span>Codemaps ({totalCodemaps})</span>
            {totalPages > 1 && (
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
                  Page {page + 1} of {totalPages}
                </span>
                <Button
                  variant="outline"
                  size="sm"
                  disabled={page >= totalPages - 1}
                  onClick={() => setPage((p) => p + 1)}
                >
                  <ChevronRight className="h-4 w-4" />
                </Button>
              </div>
            )}
          </CardTitle>
        </CardHeader>
        <CardContent>
          {pagedCodemaps.length === 0 ? (
            <div className="text-center py-8 text-muted-foreground">
              {searchQuery ? "No codemaps match your filter" : "No codemaps found"}
            </div>
          ) : (
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>Title</TableHead>
                  <TableHead>Query</TableHead>
                  <TableHead>Files</TableHead>
                  <TableHead>Symbols</TableHead>
                  <TableHead>Created</TableHead>
                  <TableHead>Actions</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {pagedCodemaps.map((codemap) => (
                  <TableRow
                    key={codemap.id}
                    className="cursor-pointer hover:bg-muted/50"
                    onClick={() => setSelectedCodemapId(codemap.id)}
                  >
                    <TableCell className="font-medium max-w-xs truncate">
                      {codemap.title}
                    </TableCell>
                    <TableCell className="max-w-sm truncate text-muted-foreground">
                      {codemap.query}
                    </TableCell>
                    <TableCell>
                      <div className="flex items-center gap-1">
                        <FileCode className="h-4 w-4 text-muted-foreground" />
                        {codemap.file_count}
                      </div>
                    </TableCell>
                    <TableCell>
                      <div className="flex items-center gap-1">
                        <Hash className="h-4 w-4 text-muted-foreground" />
                        {codemap.symbol_count}
                      </div>
                    </TableCell>
                    <TableCell className="whitespace-nowrap text-sm">
                      {formatDate(codemap.created_at)}
                    </TableCell>
                    <TableCell>
                      <div className="flex items-center gap-1">
                        <Button
                          variant="ghost"
                          size="sm"
                          onClick={(e) => {
                            e.stopPropagation();
                            setSelectedCodemapId(codemap.id);
                          }}
                        >
                          View
                        </Button>
                        <Button
                          variant="ghost"
                          size="sm"
                          onClick={(e) => {
                            e.stopPropagation();
                            handleDelete(codemap.id);
                          }}
                        >
                          <Trash2 className="h-4 w-4 text-destructive" />
                        </Button>
                      </div>
                    </TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          )}
        </CardContent>
      </Card>

      {/* Codemap detail dialog */}
      <Dialog open={!!selectedCodemapId} onOpenChange={() => setSelectedCodemapId(null)}>
        <DialogContent className="max-w-4xl max-h-[90vh] overflow-hidden flex flex-col">
          <DialogHeader>
            <DialogTitle className="flex items-center gap-2">
              <Map className="h-5 w-5" />
              {isLoadingDetail ? "Loading..." : codemapDetail?.title || "Codemap"}
            </DialogTitle>
            {codemapDetail && (
              <div className="flex items-center gap-4 text-sm text-muted-foreground">
                <span>{formatDate(codemapDetail.created_at)}</span>
                <Badge variant="outline">
                  <FileCode className="h-3 w-3 mr-1" />
                  {codemapDetail.file_count} files
                </Badge>
                <Badge variant="outline">
                  <Hash className="h-3 w-3 mr-1" />
                  {codemapDetail.symbol_count} symbols
                </Badge>
              </div>
            )}
          </DialogHeader>

          {isLoadingDetail ? (
            <div className="flex items-center justify-center py-8">
              <RefreshCw className="h-8 w-8 animate-spin text-muted-foreground" />
            </div>
          ) : codemapDetail ? (
            <div className="flex-1 overflow-y-auto space-y-4 pr-2">
              {/* Description */}
              <Card>
                <CardHeader className="py-3">
                  <CardTitle className="text-sm">Description</CardTitle>
                </CardHeader>
                <CardContent className="py-2">
                  <p className="text-sm">{codemapDetail.description}</p>
                </CardContent>
              </Card>

              {/* Query */}
              <Card>
                <CardHeader className="py-3">
                  <CardTitle className="text-sm flex items-center gap-2">
                    <Search className="h-4 w-4" />
                    Query
                  </CardTitle>
                </CardHeader>
                <CardContent className="py-2">
                  <p className="text-sm font-mono bg-muted px-3 py-2 rounded">
                    {codemapDetail.query}
                  </p>
                </CardContent>
              </Card>

              {/* Traces */}
              <Card>
                <CardHeader className="py-3">
                  <CardTitle className="text-sm flex items-center gap-2">
                    <FolderTree className="h-4 w-4" />
                    Traces ({codemapDetail.traces?.length || 0})
                  </CardTitle>
                </CardHeader>
                <CardContent className="py-2">
                  {codemapDetail.traces?.length ? (
                    <Accordion type="multiple" className="w-full">
                      {codemapDetail.traces.map((trace, idx) => (
                        <AccordionItem key={idx} value={`trace-${idx}`}>
                          <AccordionTrigger className="text-sm">
                            <div className="flex items-center gap-3">
                              <Badge variant="outline">#{trace.number}</Badge>
                              <span className="font-medium">{trace.title}</span>
                            </div>
                          </AccordionTrigger>
                          <AccordionContent className="space-y-3">
                            {/* Summary */}
                            <div>
                              <p className="text-sm text-muted-foreground mb-2">
                                {trace.summary}
                              </p>
                            </div>

                            {/* Tree */}
                            {trace.tree && (
                              <div>
                                <h5 className="text-xs font-medium text-muted-foreground mb-1 flex items-center gap-1">
                                  <Code className="h-3 w-3" />
                                  File Tree
                                </h5>
                                <pre className="text-xs font-mono bg-muted p-3 rounded overflow-x-auto">
                                  {trace.tree}
                                </pre>
                              </div>
                            )}

                            {/* Annotations */}
                            {trace.annotations?.length > 0 && (
                              <div>
                                <h5 className="text-xs font-medium text-muted-foreground mb-2 flex items-center gap-1">
                                  <Tag className="h-3 w-3" />
                                  Annotations ({trace.annotations.length})
                                </h5>
                                <div className="space-y-2">
                                  {trace.annotations.map((ann, annIdx) => (
                                    <div
                                      key={annIdx}
                                      className="border rounded p-3 text-sm"
                                    >
                                      <div className="flex items-center gap-2 mb-1">
                                        <Badge variant="secondary" className="text-xs">
                                          {ann.label}
                                        </Badge>
                                        <span className="font-medium">{ann.title}</span>
                                      </div>
                                      <p className="text-muted-foreground text-xs mb-1">
                                        {ann.description}
                                      </p>
                                      <code className="text-xs bg-muted px-2 py-0.5 rounded">
                                        {ann.path}
                                      </code>
                                    </div>
                                  ))}
                                </div>
                              </div>
                            )}
                          </AccordionContent>
                        </AccordionItem>
                      ))}
                    </Accordion>
                  ) : (
                    <p className="text-sm text-muted-foreground">No traces available</p>
                  )}
                </CardContent>
              </Card>

              {/* Metadata */}
              <Card>
                <CardHeader className="py-3">
                  <CardTitle className="text-sm">Metadata</CardTitle>
                </CardHeader>
                <CardContent className="py-2">
                  <div className="grid grid-cols-2 gap-4 text-sm">
                    <div>
                      <span className="text-muted-foreground">ID:</span>
                      <code className="ml-2 text-xs bg-muted px-2 py-0.5 rounded">
                        {codemapDetail.id}
                      </code>
                    </div>
                    <div>
                      <span className="text-muted-foreground">Workspace:</span>
                      <code className="ml-2 text-xs bg-muted px-2 py-0.5 rounded">
                        {codemapDetail.workspace}
                      </code>
                    </div>
                  </div>
                </CardContent>
              </Card>
            </div>
          ) : (
            <div className="text-center py-8 text-muted-foreground">
              Codemap not found
            </div>
          )}
        </DialogContent>
      </Dialog>
    </div>
  );
}
