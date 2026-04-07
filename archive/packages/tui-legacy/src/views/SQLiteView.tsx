// SQLite Browser View - 3-pane database explorer
import { useState } from "react";
import { useKeyboard } from "@opentui/react";
import { useSQLiteDatabases, useSQLiteTables, useSQLiteData } from "../hooks/useData";
import { WINDOWED_LIST_HEIGHT } from "../constants";

type Pane = "databases" | "tables" | "data";

function formatBytes(bytes: number): string {
  if (bytes === 0) return "0 B";
  const k = 1024;
  const sizes = ["B", "KB", "MB", "GB"];
  const i = Math.floor(Math.log(bytes) / Math.log(k));
  return parseFloat((bytes / Math.pow(k, i)).toFixed(1)) + " " + sizes[i];
}

export function SQLiteView() {
  const [activePane, setActivePane] = useState<Pane>("databases");
  const [dbCursor, setDbCursor] = useState(0);
  const [dbScroll, setDbScroll] = useState(0);
  const [tableCursor, setTableCursor] = useState(0);
  const [tableScroll, setTableScroll] = useState(0);
  const [dataCursor, setDataCursor] = useState(0);
  const [dataScroll, setDataScroll] = useState(0);

  const LIST_HEIGHT = WINDOWED_LIST_HEIGHT;

  const formatError = (error: any): string => {
    if (!error) return "";
    try {
      if (typeof error.message === "string" && error.message.startsWith("{")) {
        const parsed = JSON.parse(error.message);
        return parsed.error || parsed.message || error.message;
      }
      return error.message;
    } catch {
      return error.message;
    }
  };

  const {
    data: databases,
    isLoading: dbLoading,
    error: dbError,
    refetch: refetchDbs,
  } = useSQLiteDatabases();
  const selectedDb = databases?.[dbCursor];

  const {
    data: tables,
    isLoading: tablesLoading,
    error: tablesError,
    refetch: refetchTables,
  } = useSQLiteTables(selectedDb?.name);
  const selectedTable = tables?.[tableCursor];

  const { data: tableData, isLoading: dataLoading } = useSQLiteData(
    selectedDb?.name,
    selectedTable?.name,
    50
  );

  useKeyboard((e) => {
    if (e.name === "r") {
      refetchDbs();
      refetchTables();
      return;
    }
    // Pane navigation
    if (e.name === "h" || e.name === "left") {
      setActivePane((p) =>
        p === "data" ? "tables" : p === "tables" ? "databases" : "databases"
      );
      return;
    }
    if (e.name === "l" || e.name === "right" || e.name === "return") {
      if (activePane === "databases" && databases?.length) {
        setActivePane("tables");
        setTableCursor(0);
      } else if (activePane === "tables" && tables?.length) {
        setActivePane("data");
        setDataCursor(0);
      }
      return;
    }

    // Cursor navigation with scrolling
    const updateCursor = (
      newCursor: number,
      setCursor: (n: number) => void,
      scrollOffset: number,
      setScrollOffset: (n: number) => void
    ) => {
      setCursor(newCursor);
      if (newCursor < scrollOffset) {
        setScrollOffset(newCursor);
      } else if (newCursor >= scrollOffset + LIST_HEIGHT) {
        setScrollOffset(newCursor - LIST_HEIGHT + 1);
      }
    };

    switch (e.name) {
      case "up":
      case "k":
        if (activePane === "databases" && databases) {
          const next = Math.max(0, dbCursor - 1);
          updateCursor(next, setDbCursor, dbScroll, setDbScroll);
          setTableCursor(0);
          setTableScroll(0);
          setDataCursor(0);
          setDataScroll(0);
        } else if (activePane === "tables" && tables) {
          const next = Math.max(0, tableCursor - 1);
          updateCursor(next, setTableCursor, tableScroll, setTableScroll);
        } else if (activePane === "data" && tableData?.rows) {
          const next = Math.max(0, dataCursor - 1);
          updateCursor(next, setDataCursor, dataScroll, setDataScroll);
        }
        break;
      case "down":
      case "j":
        if (activePane === "databases" && databases) {
          const next = Math.min(Math.max(0, databases.length - 1), dbCursor + 1);
          updateCursor(next, setDbCursor, dbScroll, setDbScroll);
          setTableCursor(0);
          setTableScroll(0);
          setDataCursor(0);
          setDataScroll(0);
        } else if (activePane === "tables" && tables) {
          const next = Math.min(Math.max(0, tables.length - 1), tableCursor + 1);
          updateCursor(next, setTableCursor, tableScroll, setTableScroll);
        } else if (activePane === "data" && tableData?.rows) {
          const next = Math.min(Math.max(0, tableData.rows.length - 1), dataCursor + 1);
          updateCursor(next, setDataCursor, dataScroll, setDataScroll);
        }
        break;
      case "escape":
        setActivePane((p) =>
          p === "data" ? "tables" : p === "tables" ? "databases" : "databases"
        );
        break;
    }
  });

  if (dbLoading) {
    return (
      <box padding={1}>
        <text fg="#888888">Loading databases...</text>
      </box>
    );
  }

  if (dbError) {
    return (
      <box padding={1}>
        <text fg="#ff0000">Error loading databases: {dbError.message}</text>
      </box>
    );
  }

  if (!databases || databases.length === 0) {
    return (
      <box padding={1}>
        <text fg="#888888">No SQLite databases found in ~/.agentctl</text>
      </box>
    );
  }

  const paneLabel = (pane: Pane, label: string) => {
    const isActive = activePane === pane;
    return isActive ? `[${label}]` : label;
  };

  return (
    <box flexDirection="column" width="100%" height="100%">
      {/* Header with pane navigation */}
      <box height={1} paddingLeft={1}>
        <text>
          <b fg="#aa77ff">SQLite Browser</b>
          {"  "}
          <span fg={activePane === "databases" ? "#ffaa00" : "#666666"}>
            {paneLabel("databases", "Databases")}
          </span>
          <span fg="#666666"> {">"} </span>
          <span fg={activePane === "tables" ? "#ffaa00" : "#666666"}>
            {paneLabel("tables", "Tables")}
          </span>
          <span fg="#666666"> {">"} </span>
          <span fg={activePane === "data" ? "#ffaa00" : "#666666"}>
            {paneLabel("data", "Data")}
          </span>
        </text>
      </box>

      {/* 3-pane layout */}
      <box flexGrow={1} flexDirection="row">
        {/* Databases pane */}
        <box
          width="20%"
          borderStyle="single"
          borderColor={activePane === "databases" ? "#ffaa00" : "#444444"}
          flexDirection="column"
        >
          <box height={1} paddingLeft={1}>
            <text fg="#666666">
              <b>Databases</b> ({databases.length})
            </text>
          </box>
          <box flexGrow={1} flexDirection="column" overflow="hidden">
            {databases.slice(dbScroll, dbScroll + LIST_HEIGHT).map((db, i) => (
              <box
                key={db.name}
                height={1}
                backgroundColor={i + dbScroll === dbCursor ? "#333333" : undefined}
              >
                <text fg={i + dbScroll === dbCursor ? "#ffffff" : "#888888"}>
                  {i + dbScroll === dbCursor ? "> " : "  "}
                  {db.name}
                  <span fg="#666666"> {formatBytes(db.size)}</span>
                </text>
              </box>
            ))}
          </box>
        </box>

        {/* Tables pane */}
        <box
          width="25%"
          borderStyle="single"
          borderColor={activePane === "tables" ? "#ffaa00" : "#444444"}
          flexDirection="column"
        >
          <box height={1} paddingLeft={1}>
            <text fg="#666666">
              <b>Tables</b>
              {tables ? ` (${tables.length})` : ""}
            </text>
          </box>
          {tablesLoading ? (
            <box padding={1}>
              <text fg="#666666">Loading...</text>
            </box>
          ) : tablesError ? (
            <box padding={1}>
              <text fg="#ff0000">Error: {formatError(tablesError)}</text>
            </box>
          ) : !tables || tables.length === 0 ? (
            <box padding={1}>
              <text fg="#666666">No tables found</text>
              <text fg="#444444">in {selectedDb?.name}</text>
            </box>
          ) : (
            <box flexGrow={1} flexDirection="column" overflow="hidden">
              {tables.slice(tableScroll, tableScroll + LIST_HEIGHT).map((table, i) => (
                <box
                  key={table.name}
                  height={1}
                  backgroundColor={i + tableScroll === tableCursor ? "#333333" : undefined}
                >
                  <text fg={i + tableScroll === tableCursor ? "#ffffff" : "#888888"}>
                    {i + tableScroll === tableCursor ? "> " : "  "}
                    {table.name}
                    <span fg="#444444"> ({table.row_count})</span>
                  </text>
                </box>
              ))}
            </box>
          )}
        </box>

        {/* Data pane */}
        <box
          flexGrow={1}
          borderStyle="single"
          borderColor={activePane === "data" ? "#ffaa00" : "#444444"}
          flexDirection="column"
        >
          <box height={1} paddingLeft={1}>
            <text fg="#ffaa00">
              {selectedDb?.name}
              {selectedTable && (
                <>
                  <span fg="#666666"> {">"} </span>
                  {selectedTable.name}
                </>
              )}
            </text>
          </box>
          {dataLoading ? (
            <box padding={1}>
              <text fg="#666666">Loading data...</text>
            </box>
          ) : !tableData || !tableData.rows || tableData.rows.length === 0 ? (
            <box padding={1}>
              <text fg="#666666">
                {selectedTable ? "No data" : "Select a table"}
              </text>
            </box>
          ) : (
            <box flexDirection="column" overflow="hidden">
              {/* Column headers */}
              <box height={1} paddingLeft={1}>
                <text fg="#aa77ff">
                  {tableData.columns
                    .slice(0, 5)
                    .map((col) => col.padEnd(15).slice(0, 15))
                    .join(" | ")}
                </text>
              </box>
              <box height={1}>
                <text fg="#444444">
                  {"─".repeat(80)}
                </text>
              </box>
              {/* Data rows */}
              <box flexGrow={1} flexDirection="column" overflow="hidden">
                {tableData.rows.slice(dataScroll, dataScroll + LIST_HEIGHT).map((row, i) => (
                  <box
                    key={i}
                    height={1}
                    backgroundColor={i + dataScroll === dataCursor ? "#333333" : undefined}
                    paddingLeft={1}
                  >
                    <text fg={i + dataScroll === dataCursor ? "#ffffff" : "#888888"}>
                      {tableData.columns
                        .slice(0, 5)
                        .map((col) => {
                          const val = row[col];
                          const str =
                            val === null
                              ? "NULL"
                              : typeof val === "object"
                                ? JSON.stringify(val)
                                : String(val);
                          return str.padEnd(15).slice(0, 15);
                        })
                        .join(" | ")}
                    </text>
                  </box>
                ))}
                {tableData.rows.length > LIST_HEIGHT && (
                  <box height={1} paddingLeft={1}>
                    <text fg="#666666">
                      ... Row {dataCursor + 1} of {tableData.rows.length}
                    </text>
                  </box>
                )}
              </box>
            </box>
          )}
        </box>
      </box>
    </box>
  );
}
