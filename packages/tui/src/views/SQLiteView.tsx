// SQLite Browser View - 3-pane database explorer
import { useState } from "react";
import { useKeyboard } from "@opentui/react";
import { useSQLiteDatabases, useSQLiteTables, useSQLiteData } from "../hooks/useData";

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
  const [tableCursor, setTableCursor] = useState(0);
  const [dataCursor, setDataCursor] = useState(0);

  const { data: databases, isLoading: dbLoading, error: dbError } = useSQLiteDatabases();
  const selectedDb = databases?.[dbCursor];

  const { data: tables, isLoading: tablesLoading } = useSQLiteTables(selectedDb?.name);
  const selectedTable = tables?.[tableCursor];

  const { data: tableData, isLoading: dataLoading } = useSQLiteData(
    selectedDb?.name,
    selectedTable?.name,
    50
  );

  useKeyboard((e) => {
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

    // Cursor navigation within active pane
    const moveCursor = (
      current: number,
      max: number,
      setCursor: (fn: (n: number) => number) => void,
      direction: "up" | "down"
    ) => {
      if (direction === "up") {
        setCursor((c) => Math.max(0, c - 1));
      } else {
        setCursor((c) => Math.min(Math.max(0, max - 1), c + 1));
      }
    };

    switch (e.name) {
      case "up":
      case "k":
        if (activePane === "databases" && databases) {
          moveCursor(dbCursor, databases.length, setDbCursor, "up");
          setTableCursor(0);
        } else if (activePane === "tables" && tables) {
          moveCursor(tableCursor, tables.length, setTableCursor, "up");
        } else if (activePane === "data" && tableData?.rows) {
          moveCursor(dataCursor, tableData.rows.length, setDataCursor, "up");
        }
        break;
      case "down":
      case "j":
        if (activePane === "databases" && databases) {
          moveCursor(dbCursor, databases.length, setDbCursor, "down");
          setTableCursor(0);
        } else if (activePane === "tables" && tables) {
          moveCursor(tableCursor, tables.length, setTableCursor, "down");
        } else if (activePane === "data" && tableData?.rows) {
          moveCursor(dataCursor, tableData.rows.length, setDataCursor, "down");
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
            {databases.map((db, i) => (
              <box
                key={db.name}
                height={1}
                backgroundColor={i === dbCursor ? "#333333" : undefined}
              >
                <text fg={i === dbCursor ? "#ffffff" : "#888888"}>
                  {i === dbCursor ? "> " : "  "}
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
          ) : !tables || tables.length === 0 ? (
            <box padding={1}>
              <text fg="#666666">No tables</text>
            </box>
          ) : (
            <box flexGrow={1} flexDirection="column" overflow="hidden">
              {tables.map((table, i) => (
                <box
                  key={table.name}
                  height={1}
                  backgroundColor={i === tableCursor ? "#333333" : undefined}
                >
                  <text fg={i === tableCursor ? "#ffffff" : "#888888"}>
                    {i === tableCursor ? "> " : "  "}
                    {table.name}
                    <span fg="#666666"> ({table.row_count})</span>
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
                {tableData.rows.slice(0, 20).map((row, i) => (
                  <box
                    key={i}
                    height={1}
                    backgroundColor={i === dataCursor ? "#333333" : undefined}
                    paddingLeft={1}
                  >
                    <text fg={i === dataCursor ? "#ffffff" : "#888888"}>
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
                {tableData.rows.length > 20 && (
                  <box height={1} paddingLeft={1}>
                    <text fg="#666666">
                      ... and {tableData.rows.length - 20} more rows
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
