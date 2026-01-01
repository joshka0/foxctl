import { useState, useEffect, useRef } from "react";
import { useSQLiteDatabases, useSQLiteTables, useSQLiteData, useSQLiteSchema, useSQLiteIndexes } from "@/api/hooks";
import { executeSQLiteQuery } from "@/api/client";
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
import { formatBytes, truncate } from "@/lib/utils";
import {
  RefreshCw,
  Database,
  TableIcon,
  ArrowLeft,
  ChevronRight,
  ChevronLeft,
  Code,
  Key,
  Terminal,
  Play,
  AlertCircle,
  Copy,
  Check,
  Cloud,
  HardDrive,
} from "lucide-react";
import type { SQLiteQueryResult } from "@/types";

type TabType = "data" | "schema" | "indexes" | "query";

const PAGE_SIZE = 50;

function CopyButton({ text }: { text: string }) {
  const [copied, setCopied] = useState(false);
  const timeoutRef = useRef<ReturnType<typeof setTimeout> | null>(null);

  // Cleanup timeout on unmount to prevent memory leak / state update on unmounted component
  useEffect(() => {
    return () => {
      if (timeoutRef.current) {
        clearTimeout(timeoutRef.current);
      }
    };
  }, []);

  const handleCopy = async () => {
    await navigator.clipboard.writeText(text);
    setCopied(true);
    // Clear any existing timeout before setting a new one
    if (timeoutRef.current) {
      clearTimeout(timeoutRef.current);
    }
    timeoutRef.current = setTimeout(() => setCopied(false), 2000);
  };

  return (
    <Button variant="ghost" size="icon" onClick={handleCopy} className="h-7 w-7">
      {copied ? <Check className="h-3 w-3 text-green-500" /> : <Copy className="h-3 w-3" />}
    </Button>
  );
}

export function SQLitePage() {
  const [selectedDb, setSelectedDb] = useState<string | null>(null);
  const [selectedTable, setSelectedTable] = useState<string | null>(null);
  const [activeTab, setActiveTab] = useState<TabType>("data");
  const [offset, setOffset] = useState(0);
  const [query, setQuery] = useState("");
  const [queryResult, setQueryResult] = useState<SQLiteQueryResult | null>(null);
  const [queryLoading, setQueryLoading] = useState(false);

  const { data: dbData, isLoading: dbLoading } = useSQLiteDatabases();
  const { data: tableData, isLoading: tableLoading } = useSQLiteTables(selectedDb || "");
  const { data: rowData, isLoading: rowLoading } = useSQLiteData(
    selectedDb || "",
    selectedTable || "",
    PAGE_SIZE,
    offset
  );
  const { data: schemaData, isLoading: schemaLoading } = useSQLiteSchema(
    selectedDb || "",
    selectedTable || ""
  );
  const { data: indexData, isLoading: indexLoading } = useSQLiteIndexes(
    selectedDb || "",
    selectedTable || undefined
  );

  const databases = dbData?.databases || [];
  const tables = tableData?.tables || [];
  const columns = rowData?.columns || [];
  const rows = rowData?.rows || [];
  const totalCount = rowData?.total_count || 0;
  const schema = schemaData?.schema || "";
  const columnInfo = schemaData?.columns || [];
  const indexes = indexData?.indexes || [];

  // Get selected database info for header display
  const selectedDbInfo = databases.find((db) => db.name === selectedDb);
  const isTursoDb = selectedDbInfo?.driver === "turso";
  const isLibSQLDb = selectedDbInfo?.driver === "libsql";

  // Breadcrumb navigation
  const handleBack = () => {
    if (selectedTable) {
      setSelectedTable(null);
      setActiveTab("data");
      setOffset(0);
      setQuery("");
      setQueryResult(null);
    } else if (selectedDb) {
      setSelectedDb(null);
    }
  };

  const handleTableSelect = (tableName: string) => {
    setSelectedTable(tableName);
    setOffset(0);
    setActiveTab("data");
    setQuery(`SELECT * FROM "${tableName}" LIMIT 50`);
    setQueryResult(null);
  };

  const handleExecuteQuery = async () => {
    if (!selectedDb || !query.trim()) return;
    setQueryLoading(true);
    try {
      const result = await executeSQLiteQuery(selectedDb, query, 100);
      setQueryResult(result);
    } catch (err) {
      setQueryResult({
        columns: [],
        rows: [],
        rows_affected: 0,
        error: err instanceof Error ? err.message : "Query failed",
      });
    } finally {
      setQueryLoading(false);
    }
  };

  const totalPages = Math.ceil(totalCount / PAGE_SIZE);
  const currentPage = Math.floor(offset / PAGE_SIZE) + 1;

  return (
    <div className="space-y-6">
      {/* Header with breadcrumb */}
      <div className="flex items-center gap-4">
        {(selectedDb || selectedTable) && (
          <Button variant="outline" size="icon" onClick={handleBack}>
            <ArrowLeft className="h-4 w-4" />
          </Button>
        )}
        <div className="flex items-center gap-2">
          <h1 className="text-2xl font-bold">Database Browser</h1>
          {selectedDb && (
            <>
              <ChevronRight className="h-5 w-5 text-muted-foreground" />
              <div className="flex items-center gap-2">
                {isTursoDb && (
                  <Cloud className="h-4 w-4 text-sky-500" />
                )}
                <Badge variant="secondary" className="text-sm">
                  {selectedDb}
                </Badge>
                {isTursoDb && (
                  <Badge variant="default" className="bg-sky-500 hover:bg-sky-600 text-xs">
                    Turso
                  </Badge>
                )}
                {isLibSQLDb && (
                  <Badge variant="secondary" className="bg-indigo-500/20 text-indigo-600 dark:text-indigo-400 text-xs">
                    libSQL
                  </Badge>
                )}
              </div>
            </>
          )}
          {selectedTable && (
            <>
              <ChevronRight className="h-5 w-5 text-muted-foreground" />
              <Badge variant="outline" className="text-sm">
                {selectedTable}
              </Badge>
            </>
          )}
        </div>
      </div>

      {/* Database list */}
      {!selectedDb && (
        <Card>
          <CardHeader>
            <CardTitle className="flex items-center gap-2">
              <Database className="h-5 w-5" />
              Databases
            </CardTitle>
          </CardHeader>
          <CardContent>
            {dbLoading ? (
              <div className="flex items-center justify-center py-8">
                <RefreshCw className="h-6 w-6 animate-spin text-muted-foreground" />
              </div>
            ) : databases.length === 0 ? (
              <div className="text-center py-8 text-muted-foreground">
                No databases found
              </div>
            ) : (
              <div className="grid gap-4 md:grid-cols-2 lg:grid-cols-3">
                {databases.map((db) => {
                  const isTurso = db.driver === "turso";
                  const isLibSQL = db.driver === "libsql";
                  const isCloud = isTurso || isLibSQL;

                  return (
                    <Card
                      key={db.name}
                      className="cursor-pointer hover:bg-accent transition-colors"
                      onClick={() => setSelectedDb(db.name)}
                    >
                      <CardContent className="pt-4">
                        <div className="flex items-start justify-between gap-2">
                          <div className="flex-1 min-w-0">
                            <div className="flex items-center gap-2">
                              {isCloud ? (
                                <Cloud className="h-4 w-4 text-sky-500 flex-shrink-0" />
                              ) : (
                                <HardDrive className="h-4 w-4 text-muted-foreground flex-shrink-0" />
                              )}
                              <p className="font-medium truncate">{db.friendly_name}</p>
                            </div>
                            <p className="text-sm text-muted-foreground mt-1">{db.name}.db</p>
                            {db.turso_url && (
                              <p className="text-xs text-sky-600 dark:text-sky-400 mt-1 truncate" title={db.turso_url}>
                                {db.turso_url}
                              </p>
                            )}
                          </div>
                          <div className="flex flex-col items-end gap-1 flex-shrink-0">
                            {isTurso && (
                              <Badge variant="default" className="bg-sky-500 hover:bg-sky-600">
                                Turso
                              </Badge>
                            )}
                            {isLibSQL && (
                              <Badge variant="secondary" className="bg-indigo-500/20 text-indigo-600 dark:text-indigo-400">
                                libSQL
                              </Badge>
                            )}
                            <Badge variant="secondary">{formatBytes(db.size)}</Badge>
                          </div>
                        </div>
                        {isTurso && db.has_replica && (
                          <p className="text-xs text-muted-foreground mt-2">
                            Local replica available
                          </p>
                        )}
                        {isTurso && !db.has_replica && (
                          <p className="text-xs text-amber-600 dark:text-amber-400 mt-2">
                            Cloud only (no local replica)
                          </p>
                        )}
                      </CardContent>
                    </Card>
                  );
                })}
              </div>
            )}
          </CardContent>
        </Card>
      )}

      {/* Table list */}
      {selectedDb && !selectedTable && (
        <Card>
          <CardHeader>
            <CardTitle className="flex items-center gap-2">
              <TableIcon className="h-5 w-5" />
              Tables in {selectedDb}
            </CardTitle>
          </CardHeader>
          <CardContent>
            {tableLoading ? (
              <div className="flex items-center justify-center py-8">
                <RefreshCw className="h-6 w-6 animate-spin text-muted-foreground" />
              </div>
            ) : tables.length === 0 ? (
              <div className="text-center py-8 text-muted-foreground">
                No tables found
              </div>
            ) : (
              <Table>
                <TableHeader>
                  <TableRow>
                    <TableHead>Table Name</TableHead>
                    <TableHead className="text-right">Row Count</TableHead>
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {tables.map((table) => (
                    <TableRow
                      key={table.name}
                      className="cursor-pointer"
                      onClick={() => handleTableSelect(table.name)}
                    >
                      <TableCell className="font-medium">{table.name}</TableCell>
                      <TableCell className="text-right">
                        {table.row_count >= 0 ? table.row_count.toLocaleString() : "?"}
                      </TableCell>
                    </TableRow>
                  ))}
                </TableBody>
              </Table>
            )}
          </CardContent>
        </Card>
      )}

      {/* Table detail view with tabs */}
      {selectedDb && selectedTable && (
        <div className="space-y-4">
          {/* Tab navigation */}
          <div className="flex gap-2 border-b pb-2">
            <Button
              variant={activeTab === "data" ? "default" : "ghost"}
              size="sm"
              onClick={() => setActiveTab("data")}
            >
              <TableIcon className="h-4 w-4 mr-2" />
              Data
            </Button>
            <Button
              variant={activeTab === "schema" ? "default" : "ghost"}
              size="sm"
              onClick={() => setActiveTab("schema")}
            >
              <Code className="h-4 w-4 mr-2" />
              Schema
            </Button>
            <Button
              variant={activeTab === "indexes" ? "default" : "ghost"}
              size="sm"
              onClick={() => setActiveTab("indexes")}
            >
              <Key className="h-4 w-4 mr-2" />
              Indexes
            </Button>
            <Button
              variant={activeTab === "query" ? "default" : "ghost"}
              size="sm"
              onClick={() => setActiveTab("query")}
            >
              <Terminal className="h-4 w-4 mr-2" />
              Query
            </Button>
          </div>

          {/* Data tab */}
          {activeTab === "data" && (
            <Card>
              <CardHeader className="flex flex-row items-center justify-between">
                <CardTitle>
                  {selectedTable} ({totalCount.toLocaleString()} rows)
                </CardTitle>
                {totalPages > 1 && (
                  <div className="flex items-center gap-2">
                    <Button
                      variant="outline"
                      size="sm"
                      onClick={() => setOffset(Math.max(0, offset - PAGE_SIZE))}
                      disabled={offset === 0}
                    >
                      <ChevronLeft className="h-4 w-4" />
                    </Button>
                    <span className="text-sm text-muted-foreground">
                      Page {currentPage} of {totalPages}
                    </span>
                    <Button
                      variant="outline"
                      size="sm"
                      onClick={() => setOffset(offset + PAGE_SIZE)}
                      disabled={offset + PAGE_SIZE >= totalCount}
                    >
                      <ChevronRight className="h-4 w-4" />
                    </Button>
                  </div>
                )}
              </CardHeader>
              <CardContent>
                {rowLoading ? (
                  <div className="flex items-center justify-center py-8">
                    <RefreshCw className="h-6 w-6 animate-spin text-muted-foreground" />
                  </div>
                ) : rows.length === 0 ? (
                  <div className="text-center py-8 text-muted-foreground">
                    No data found
                  </div>
                ) : (
                  <div className="overflow-auto max-h-[600px]">
                    <Table>
                      <TableHeader>
                        <TableRow>
                          {columns.map((col) => (
                            <TableHead key={col}>{col}</TableHead>
                          ))}
                        </TableRow>
                      </TableHeader>
                      <TableBody>
                        {rows.map((row, i) => (
                          <TableRow key={i}>
                            {columns.map((col) => (
                              <TableCell key={col} className="font-mono text-xs">
                                {formatCellValue(row[col])}
                              </TableCell>
                            ))}
                          </TableRow>
                        ))}
                      </TableBody>
                    </Table>
                  </div>
                )}
              </CardContent>
            </Card>
          )}

          {/* Schema tab */}
          {activeTab === "schema" && (
            <Card>
              <CardHeader>
                <CardTitle className="flex items-center justify-between">
                  <span>Table Schema</span>
                  <CopyButton text={schema} />
                </CardTitle>
              </CardHeader>
              <CardContent>
                {schemaLoading ? (
                  <div className="flex items-center justify-center py-8">
                    <RefreshCw className="h-6 w-6 animate-spin text-muted-foreground" />
                  </div>
                ) : (
                  <div className="space-y-6">
                    {/* CREATE TABLE statement */}
                    <div>
                      <p className="text-sm text-muted-foreground mb-2">CREATE TABLE</p>
                      <pre className="text-xs bg-zinc-900 text-zinc-100 p-4 rounded overflow-x-auto font-mono">
                        {schema || "No schema available"}
                      </pre>
                    </div>

                    {/* Column details */}
                    {columnInfo.length > 0 && (
                      <div>
                        <p className="text-sm text-muted-foreground mb-2">Columns</p>
                        <Table>
                          <TableHeader>
                            <TableRow>
                              <TableHead>Name</TableHead>
                              <TableHead>Type</TableHead>
                              <TableHead>Nullable</TableHead>
                              <TableHead>Default</TableHead>
                              <TableHead>Primary Key</TableHead>
                            </TableRow>
                          </TableHeader>
                          <TableBody>
                            {columnInfo.map((col) => (
                              <TableRow key={col.name}>
                                <TableCell className="font-mono">{col.name}</TableCell>
                                <TableCell>
                                  <Badge variant="outline">{col.type || "ANY"}</Badge>
                                </TableCell>
                                <TableCell>
                                  {col.not_null ? (
                                    <Badge variant="destructive">NOT NULL</Badge>
                                  ) : (
                                    <span className="text-muted-foreground">NULL</span>
                                  )}
                                </TableCell>
                                <TableCell className="font-mono text-xs">
                                  {col.default_value || "—"}
                                </TableCell>
                                <TableCell>
                                  {col.is_pk && <Badge variant="default">PK</Badge>}
                                </TableCell>
                              </TableRow>
                            ))}
                          </TableBody>
                        </Table>
                      </div>
                    )}
                  </div>
                )}
              </CardContent>
            </Card>
          )}

          {/* Indexes tab */}
          {activeTab === "indexes" && (
            <Card>
              <CardHeader>
                <CardTitle>Indexes on {selectedTable}</CardTitle>
              </CardHeader>
              <CardContent>
                {indexLoading ? (
                  <div className="flex items-center justify-center py-8">
                    <RefreshCw className="h-6 w-6 animate-spin text-muted-foreground" />
                  </div>
                ) : indexes.length === 0 ? (
                  <div className="text-center py-8 text-muted-foreground">
                    No indexes found
                  </div>
                ) : (
                  <Table>
                    <TableHeader>
                      <TableRow>
                        <TableHead>Name</TableHead>
                        <TableHead>Columns</TableHead>
                        <TableHead>Unique</TableHead>
                        <TableHead>SQL</TableHead>
                      </TableRow>
                    </TableHeader>
                    <TableBody>
                      {indexes.map((idx) => (
                        <TableRow key={idx.name}>
                          <TableCell className="font-mono">{idx.name}</TableCell>
                          <TableCell>
                            <div className="flex gap-1 flex-wrap">
                              {idx.columns.map((col) => (
                                <Badge key={col} variant="secondary">
                                  {col}
                                </Badge>
                              ))}
                            </div>
                          </TableCell>
                          <TableCell>
                            {idx.unique && <Badge variant="info">UNIQUE</Badge>}
                          </TableCell>
                          <TableCell className="font-mono text-xs max-w-xs">
                            {idx.sql ? truncate(idx.sql, 50) : "—"}
                          </TableCell>
                        </TableRow>
                      ))}
                    </TableBody>
                  </Table>
                )}
              </CardContent>
            </Card>
          )}

          {/* Query tab */}
          {activeTab === "query" && (
            <Card>
              <CardHeader>
                <CardTitle className="flex items-center gap-2">
                  <Terminal className="h-5 w-5" />
                  SQL Query
                </CardTitle>
              </CardHeader>
              <CardContent className="space-y-4">
                <div className="flex items-center gap-2 p-2 bg-amber-500/10 border border-amber-500/20 rounded text-sm">
                  <AlertCircle className="h-4 w-4 text-amber-500" />
                  <span className="text-amber-600 dark:text-amber-400">
                    Read-only queries only. Write operations are blocked.
                  </span>
                </div>

                <div className="space-y-2">
                  <textarea
                    value={query}
                    onChange={(e) => setQuery(e.target.value)}
                    className="w-full h-32 p-3 font-mono text-sm bg-zinc-900 text-zinc-100 rounded border-0 resize-none focus:outline-none focus:ring-2 focus:ring-primary"
                    placeholder="SELECT * FROM table_name LIMIT 50"
                    onKeyDown={(e) => {
                      if (e.key === "Enter" && (e.metaKey || e.ctrlKey)) {
                        handleExecuteQuery();
                      }
                    }}
                  />
                  <div className="flex items-center justify-between">
                    <span className="text-xs text-muted-foreground">
                      Press ⌘+Enter to execute
                    </span>
                    <Button
                      onClick={handleExecuteQuery}
                      disabled={queryLoading || !query.trim()}
                    >
                      {queryLoading ? (
                        <RefreshCw className="h-4 w-4 mr-2 animate-spin" />
                      ) : (
                        <Play className="h-4 w-4 mr-2" />
                      )}
                      Execute
                    </Button>
                  </div>
                </div>

                {/* Query results */}
                {queryResult && (
                  <div className="space-y-2">
                    {queryResult.error ? (
                      <div className="p-4 bg-destructive/10 border border-destructive/20 rounded">
                        <p className="text-sm text-destructive font-mono">
                          Error: {queryResult.error}
                        </p>
                      </div>
                    ) : (
                      <>
                        <div className="flex items-center gap-2 text-sm text-muted-foreground">
                          <Badge variant="secondary">
                            {queryResult.rows.length} rows returned
                          </Badge>
                        </div>
                        {queryResult.rows.length > 0 && (
                          <div className="overflow-auto max-h-[400px] border rounded">
                            <Table>
                              <TableHeader>
                                <TableRow>
                                  {queryResult.columns.map((col) => (
                                    <TableHead key={col}>{col}</TableHead>
                                  ))}
                                </TableRow>
                              </TableHeader>
                              <TableBody>
                                {queryResult.rows.map((row, i) => (
                                  <TableRow key={i}>
                                    {queryResult.columns.map((col) => (
                                      <TableCell key={col} className="font-mono text-xs">
                                        {formatCellValue(row[col])}
                                      </TableCell>
                                    ))}
                                  </TableRow>
                                ))}
                              </TableBody>
                            </Table>
                          </div>
                        )}
                      </>
                    )}
                  </div>
                )}
              </CardContent>
            </Card>
          )}
        </div>
      )}
    </div>
  );
}

function formatCellValue(value: unknown): string {
  if (value === null || value === undefined) return "NULL";
  if (typeof value === "object") return truncate(JSON.stringify(value), 50);
  const str = String(value);
  return truncate(str, 100);
}
