<script lang="ts">
  import {
    RefreshCw,
    Database,
    Table as TableIcon,
    ArrowLeft,
    ChevronRight,
    ChevronLeft,
    Code,
    Key,
    Terminal,
    Play,
    AlertCircle,
    Cloud,
    HardDrive,
  } from "lucide-svelte";
  import Layout from "@/lib/components/layout/Layout.svelte";
  import {
    useSQLiteDatabases,
    useSQLiteTables,
    useSQLiteData,
    useSQLiteSchema,
    useSQLiteIndexes,
  } from "@/lib/api/hooks";
  import { executeSQLiteQuery } from "@agentctl/data";
  import {
    Badge,
    Button,
    Card,
    CardContent,
    CardHeader,
    CardTitle,
    Table,
    TableHeader,
    TableBody,
    TableRow,
    TableHead,
    TableCell,
    CopyButton,
  } from "@/lib/components/ui";
  import { formatBytes, truncate } from "@/lib/utils/format";
  import type { SQLiteQueryResult } from "@agentctl/data";

  type TabType = "data" | "schema" | "indexes" | "query";
  const PAGE_SIZE = 50;

  let selectedDb: string | null = null;
  let selectedTable: string | null = null;
  let activeTab: TabType = "data";
  let offset = 0;
  let query = "";
  let queryResult: SQLiteQueryResult | null = null;
  let queryLoading = false;

  // Queries
  const dbQuery = useSQLiteDatabases();
  $: tableQuery = useSQLiteTables(selectedDb || "");
  $: dataQuery = useSQLiteData(selectedDb || "", selectedTable || "", PAGE_SIZE, offset);
  $: schemaQuery = useSQLiteSchema(selectedDb || "", selectedTable || "");
  $: indexQuery = useSQLiteIndexes(selectedDb || "", selectedTable || undefined);

  // Derived data
  $: databases = $dbQuery.data?.databases || [];
  $: tables = $tableQuery.data?.tables || [];
  $: columns = $dataQuery.data?.columns || [];
  $: rows = $dataQuery.data?.rows || [];
  $: totalCount = $dataQuery.data?.total_count || 0;
  $: schema = $schemaQuery.data?.schema || "";
  $: columnInfo = $schemaQuery.data?.columns || [];
  $: indexes = $indexQuery.data?.indexes || [];

  // Selected database info
  $: selectedDbInfo = databases.find((db) => db.name === selectedDb);
  $: isTursoDb = selectedDbInfo?.driver === "turso";
  $: isLibSQLDb = selectedDbInfo?.driver === "libsql";

  // Pagination
  $: totalPages = Math.ceil(totalCount / PAGE_SIZE);
  $: currentPage = Math.floor(offset / PAGE_SIZE) + 1;

  function handleBack() {
    if (selectedTable) {
      selectedTable = null;
      activeTab = "data";
      offset = 0;
      query = "";
      queryResult = null;
    } else if (selectedDb) {
      selectedDb = null;
    }
  }

  function handleTableSelect(tableName: string) {
    selectedTable = tableName;
    offset = 0;
    activeTab = "data";
    query = `SELECT * FROM "${tableName}" LIMIT 50`;
    queryResult = null;
  }

  async function handleExecuteQuery() {
    if (!selectedDb || !query.trim()) return;
    queryLoading = true;
    try {
      const result = await executeSQLiteQuery(selectedDb, query, 100);
      queryResult = result;
    } catch (err) {
      queryResult = {
        columns: [],
        rows: [],
        rows_affected: 0,
        error: err instanceof Error ? err.message : "Query failed",
      };
    } finally {
      queryLoading = false;
    }
  }

  function handleKeyDown(e: KeyboardEvent) {
    if (e.key === "Enter" && (e.metaKey || e.ctrlKey)) {
      handleExecuteQuery();
    }
  }

  function formatCellValue(value: unknown): string {
    if (value === null || value === undefined) return "NULL";
    if (typeof value === "object") return truncate(JSON.stringify(value), 50);
    const str = String(value);
    return truncate(str, 100);
  }
</script>

<Layout>
  <div class="space-y-6">
    <!-- Header with breadcrumb -->
    <div class="flex items-center gap-4">
      {#if selectedDb || selectedTable}
        <Button variant="outline" size="icon" on:click={handleBack}>
          <ArrowLeft class="h-4 w-4" />
        </Button>
      {/if}
      <div class="flex items-center gap-2">
        <h1 class="text-2xl font-bold">Database Browser</h1>
        {#if selectedDb}
          <ChevronRight class="h-5 w-5 text-muted-foreground" />
          <div class="flex items-center gap-2">
            {#if isTursoDb}
              <Cloud class="h-4 w-4 text-sky-500" />
            {/if}
            <Badge variant="secondary" class="text-sm">
              {selectedDb}
            </Badge>
            {#if isTursoDb}
              <Badge variant="default" class="bg-sky-500 hover:bg-sky-600 text-xs">
                Turso
              </Badge>
            {/if}
            {#if isLibSQLDb}
              <Badge variant="secondary" class="bg-indigo-500/20 text-indigo-600 dark:text-indigo-400 text-xs">
                libSQL
              </Badge>
            {/if}
          </div>
        {/if}
        {#if selectedTable}
          <ChevronRight class="h-5 w-5 text-muted-foreground" />
          <Badge variant="outline" class="text-sm">
            {selectedTable}
          </Badge>
        {/if}
      </div>
    </div>

    <!-- Database list -->
    {#if !selectedDb}
      <Card>
        <CardHeader>
          <CardTitle class="flex items-center gap-2">
            <Database class="h-5 w-5" />
            Databases
          </CardTitle>
        </CardHeader>
        <CardContent>
          {#if $dbQuery.isLoading}
            <div class="flex items-center justify-center py-8">
              <RefreshCw class="h-6 w-6 animate-spin text-muted-foreground" />
            </div>
          {:else if databases.length === 0}
            <div class="text-center py-8 text-muted-foreground">
              No databases found
            </div>
          {:else}
            <div class="grid gap-4 md:grid-cols-2 lg:grid-cols-3">
              {#each databases as db (db.name)}
                {@const isTurso = db.driver === "turso"}
                {@const isLibSQL = db.driver === "libsql"}
                {@const isCloud = isTurso || isLibSQL}
                <Card
                  class="cursor-pointer hover:bg-accent transition-colors"
                >
                  <button
                    type="button"
                    class="w-full text-left"
                    on:click={() => (selectedDb = db.name)}
                  >
                    <CardContent class="pt-4">
                      <div class="flex items-start justify-between gap-2">
                        <div class="flex-1 min-w-0">
                          <div class="flex items-center gap-2">
                            {#if isCloud}
                              <Cloud class="h-4 w-4 text-sky-500 flex-shrink-0" />
                            {:else}
                              <HardDrive class="h-4 w-4 text-muted-foreground flex-shrink-0" />
                            {/if}
                            <p class="font-medium truncate">{db.friendly_name}</p>
                          </div>
                          <p class="text-sm text-muted-foreground mt-1">{db.name}.db</p>
                          {#if db.turso_url}
                            <p class="text-xs text-sky-600 dark:text-sky-400 mt-1 truncate" title={db.turso_url}>
                              {db.turso_url}
                            </p>
                          {/if}
                        </div>
                        <div class="flex flex-col items-end gap-1 flex-shrink-0">
                          {#if isTurso}
                            <Badge variant="default" class="bg-sky-500 hover:bg-sky-600">
                              Turso
                            </Badge>
                          {/if}
                          {#if isLibSQL}
                            <Badge variant="secondary" class="bg-indigo-500/20 text-indigo-600 dark:text-indigo-400">
                              libSQL
                            </Badge>
                          {/if}
                          <Badge variant="secondary">{formatBytes(db.size)}</Badge>
                        </div>
                      </div>
                      {#if isTurso && db.has_replica}
                        <p class="text-xs text-muted-foreground mt-2">
                          Local replica available
                        </p>
                      {/if}
                      {#if isTurso && !db.has_replica}
                        <p class="text-xs text-amber-600 dark:text-amber-400 mt-2">
                          Cloud only (no local replica)
                        </p>
                      {/if}
                    </CardContent>
                  </button>
                </Card>
              {/each}
            </div>
          {/if}
        </CardContent>
      </Card>
    {/if}

    <!-- Table list -->
    {#if selectedDb && !selectedTable}
      <Card>
        <CardHeader>
          <CardTitle class="flex items-center gap-2">
            <TableIcon class="h-5 w-5" />
            Tables in {selectedDb}
          </CardTitle>
        </CardHeader>
        <CardContent>
          {#if $tableQuery.isLoading}
            <div class="flex items-center justify-center py-8">
              <RefreshCw class="h-6 w-6 animate-spin text-muted-foreground" />
            </div>
          {:else if tables.length === 0}
            <div class="text-center py-8 text-muted-foreground">
              No tables found
            </div>
          {:else}
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>Table Name</TableHead>
                  <TableHead class="text-right">Row Count</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {#each tables as table (table.name)}
                  <TableRow
                    class="cursor-pointer"
                  >
                    <TableCell>
                      <button
                        type="button"
                        class="w-full text-left font-medium"
                        on:click={() => handleTableSelect(table.name)}
                      >
                        {table.name}
                      </button>
                    </TableCell>
                    <TableCell class="text-right">
                      {table.row_count >= 0 ? table.row_count.toLocaleString() : "?"}
                    </TableCell>
                  </TableRow>
                {/each}
              </TableBody>
            </Table>
          {/if}
        </CardContent>
      </Card>
    {/if}

    <!-- Table detail view with tabs -->
    {#if selectedDb && selectedTable}
      <div class="space-y-4">
        <!-- Tab navigation -->
        <div class="flex gap-2 border-b pb-2">
          <Button
            variant={activeTab === "data" ? "default" : "ghost"}
            size="sm"
            on:click={() => (activeTab = "data")}
          >
            <TableIcon class="h-4 w-4 mr-2" />
            Data
          </Button>
          <Button
            variant={activeTab === "schema" ? "default" : "ghost"}
            size="sm"
            on:click={() => (activeTab = "schema")}
          >
            <Code class="h-4 w-4 mr-2" />
            Schema
          </Button>
          <Button
            variant={activeTab === "indexes" ? "default" : "ghost"}
            size="sm"
            on:click={() => (activeTab = "indexes")}
          >
            <Key class="h-4 w-4 mr-2" />
            Indexes
          </Button>
          <Button
            variant={activeTab === "query" ? "default" : "ghost"}
            size="sm"
            on:click={() => (activeTab = "query")}
          >
            <Terminal class="h-4 w-4 mr-2" />
            Query
          </Button>
        </div>

        <!-- Data tab -->
        {#if activeTab === "data"}
          <Card>
            <CardHeader class="flex flex-row items-center justify-between">
              <CardTitle>
                {selectedTable} ({totalCount.toLocaleString()} rows)
              </CardTitle>
              {#if totalPages > 1}
                <div class="flex items-center gap-2">
                  <Button
                    variant="outline"
                    size="sm"
                    on:click={() => (offset = Math.max(0, offset - PAGE_SIZE))}
                    disabled={offset === 0}
                  >
                    <ChevronLeft class="h-4 w-4" />
                  </Button>
                  <span class="text-sm text-muted-foreground">
                    Page {currentPage} of {totalPages}
                  </span>
                  <Button
                    variant="outline"
                    size="sm"
                    on:click={() => (offset = offset + PAGE_SIZE)}
                    disabled={offset + PAGE_SIZE >= totalCount}
                  >
                    <ChevronRight class="h-4 w-4" />
                  </Button>
                </div>
              {/if}
            </CardHeader>
            <CardContent>
              {#if $dataQuery.isLoading}
                <div class="flex items-center justify-center py-8">
                  <RefreshCw class="h-6 w-6 animate-spin text-muted-foreground" />
                </div>
              {:else if rows.length === 0}
                <div class="text-center py-8 text-muted-foreground">
                  No data found
                </div>
              {:else}
                <div class="overflow-auto max-h-[600px]">
                  <Table>
                    <TableHeader>
                      <TableRow>
                        {#each columns as col}
                          <TableHead>{col}</TableHead>
                        {/each}
                      </TableRow>
                    </TableHeader>
                    <TableBody>
                      {#each rows as row, i (i)}
                        <TableRow>
                          {#each columns as col}
                            <TableCell class="font-mono text-xs">
                              {formatCellValue(row[col])}
                            </TableCell>
                          {/each}
                        </TableRow>
                      {/each}
                    </TableBody>
                  </Table>
                </div>
              {/if}
            </CardContent>
          </Card>
        {/if}

        <!-- Schema tab -->
        {#if activeTab === "schema"}
          <Card>
            <CardHeader>
              <CardTitle class="flex items-center justify-between">
                <span>Table Schema</span>
                <CopyButton text={schema} />
              </CardTitle>
            </CardHeader>
            <CardContent>
              {#if $schemaQuery.isLoading}
                <div class="flex items-center justify-center py-8">
                  <RefreshCw class="h-6 w-6 animate-spin text-muted-foreground" />
                </div>
              {:else}
                <div class="space-y-6">
                  <!-- CREATE TABLE statement -->
                  <div>
                    <p class="text-sm text-muted-foreground mb-2">CREATE TABLE</p>
                    <pre class="text-xs bg-zinc-900 text-zinc-100 p-4 rounded overflow-x-auto font-mono">{schema || "No schema available"}</pre>
                  </div>

                  <!-- Column details -->
                  {#if columnInfo.length > 0}
                    <div>
                      <p class="text-sm text-muted-foreground mb-2">Columns</p>
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
                          {#each columnInfo as col (col.name)}
                            <TableRow>
                              <TableCell class="font-mono">{col.name}</TableCell>
                              <TableCell>
                                <Badge variant="outline">{col.type || "ANY"}</Badge>
                              </TableCell>
                              <TableCell>
                                {#if col.not_null}
                                  <Badge variant="destructive">NOT NULL</Badge>
                                {:else}
                                  <span class="text-muted-foreground">NULL</span>
                                {/if}
                              </TableCell>
                              <TableCell class="font-mono text-xs">
                                {col.default_value || "—"}
                              </TableCell>
                              <TableCell>
                                {#if col.is_pk}
                                  <Badge variant="default">PK</Badge>
                                {/if}
                              </TableCell>
                            </TableRow>
                          {/each}
                        </TableBody>
                      </Table>
                    </div>
                  {/if}
                </div>
              {/if}
            </CardContent>
          </Card>
        {/if}

        <!-- Indexes tab -->
        {#if activeTab === "indexes"}
          <Card>
            <CardHeader>
              <CardTitle>Indexes on {selectedTable}</CardTitle>
            </CardHeader>
            <CardContent>
              {#if $indexQuery.isLoading}
                <div class="flex items-center justify-center py-8">
                  <RefreshCw class="h-6 w-6 animate-spin text-muted-foreground" />
                </div>
              {:else if indexes.length === 0}
                <div class="text-center py-8 text-muted-foreground">
                  No indexes found
                </div>
              {:else}
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
                    {#each indexes as idx (idx.name)}
                      <TableRow>
                        <TableCell class="font-mono">{idx.name}</TableCell>
                        <TableCell>
                          <div class="flex gap-1 flex-wrap">
                            {#each idx.columns as col}
                              <Badge variant="secondary">
                                {col}
                              </Badge>
                            {/each}
                          </div>
                        </TableCell>
                        <TableCell>
                          {#if idx.unique}
                            <Badge variant="info">UNIQUE</Badge>
                          {/if}
                        </TableCell>
                        <TableCell class="font-mono text-xs max-w-xs">
                          {idx.sql ? truncate(idx.sql, 50) : "—"}
                        </TableCell>
                      </TableRow>
                    {/each}
                  </TableBody>
                </Table>
              {/if}
            </CardContent>
          </Card>
        {/if}

        <!-- Query tab -->
        {#if activeTab === "query"}
          <Card>
            <CardHeader>
              <CardTitle class="flex items-center gap-2">
                <Terminal class="h-5 w-5" />
                SQL Query
              </CardTitle>
            </CardHeader>
            <CardContent class="space-y-4">
              <div class="flex items-center gap-2 p-2 bg-amber-500/10 border border-amber-500/20 rounded text-sm">
                <AlertCircle class="h-4 w-4 text-amber-500" />
                <span class="text-amber-600 dark:text-amber-400">
                  Read-only queries only. Write operations are blocked.
                </span>
              </div>

              <div class="space-y-2">
                <textarea
                  bind:value={query}
                  class="w-full h-32 p-3 font-mono text-sm bg-zinc-900 text-zinc-100 rounded border-0 resize-none focus:outline-none focus:ring-2 focus:ring-primary"
                  placeholder="SELECT * FROM table_name LIMIT 50"
                  on:keydown={handleKeyDown}
                ></textarea>
                <div class="flex items-center justify-between">
                  <span class="text-xs text-muted-foreground">
                    Press ⌘+Enter to execute
                  </span>
                  <Button
                    on:click={handleExecuteQuery}
                    disabled={queryLoading || !query.trim()}
                  >
                    {#if queryLoading}
                      <RefreshCw class="h-4 w-4 mr-2 animate-spin" />
                    {:else}
                      <Play class="h-4 w-4 mr-2" />
                    {/if}
                    Execute
                  </Button>
                </div>
              </div>

              <!-- Query results -->
              {#if queryResult}
                <div class="space-y-2">
                  {#if queryResult.error}
                    <div class="p-4 bg-destructive/10 border border-destructive/20 rounded">
                      <p class="text-sm text-destructive font-mono">
                        Error: {queryResult.error}
                      </p>
                    </div>
                  {:else}
                    <div class="flex items-center gap-2 text-sm text-muted-foreground">
                      <Badge variant="secondary">
                        {queryResult.rows.length} rows returned
                      </Badge>
                    </div>
                    {#if queryResult.rows.length > 0}
                      <div class="overflow-auto max-h-[400px] border rounded">
                        <Table>
                          <TableHeader>
                            <TableRow>
                              {#each queryResult.columns as col}
                                <TableHead>{col}</TableHead>
                              {/each}
                            </TableRow>
                          </TableHeader>
                          <TableBody>
                            {#each queryResult.rows as row, i (i)}
                              <TableRow>
                                {#each queryResult.columns as col}
                                  <TableCell class="font-mono text-xs">
                                    {formatCellValue(row[col])}
                                  </TableCell>
                                {/each}
                              </TableRow>
                            {/each}
                          </TableBody>
                        </Table>
                      </div>
                    {/if}
                  {/if}
                </div>
              {/if}
            </CardContent>
          </Card>
        {/if}
      </div>
    {/if}
  </div>
</Layout>
