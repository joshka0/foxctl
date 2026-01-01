// Search View - Semantic search with input
import { useState } from "react";
import { useKeyboard } from "@opentui/react";
import { useSearch } from "../hooks/useData";

function sourceColor(source: string): string {
  switch (source) {
    case "symbols":
    case "symbol":
      return "#00ff00";
    case "sessions":
    case "session":
      return "#00ffff";
    case "memories":
    case "memory":
      return "#ff00ff";
    case "tasks":
    case "task":
      return "#ffaa00";
    default:
      return "#888888";
  }
}

export function SearchView() {
  const [query, setQuery] = useState("");
  const [inputMode, setInputMode] = useState(true);
  const [cursor, setCursor] = useState(0);

  const { data, isLoading, error } = useSearch({
    q: query,
    limit: 20,
    rerank: false,
  });

  const results = data?.results || [];
  const stats = data?.stats;

  useKeyboard((e) => {
    if (inputMode) {
      // Input mode - build query string
      if (e.name === "return") {
        if (query.length > 0) {
          setInputMode(false);
          setCursor(0);
        }
        return;
      }
      if (e.name === "backspace") {
        setQuery((q) => q.slice(0, -1));
        return;
      }
      if (e.name === "escape") {
        setQuery("");
        return;
      }
      // Handle printable characters (raw is the actual character typed)
      if (e.raw && e.raw.length === 1 && !e.ctrl && !e.meta) {
        setQuery((q) => q + e.raw);
        return;
      }
      if (e.name === "space") {
        setQuery((q) => q + " ");
        return;
      }
    } else {
      // Results mode - navigate results
      switch (e.name) {
        case "up":
        case "k":
          setCursor((c) => Math.max(0, c - 1));
          break;
        case "down":
        case "j":
          setCursor((c) => Math.min(Math.max(0, results.length - 1), c + 1));
          break;
        case "escape":
        case "/":
          setInputMode(true);
          break;
        case "g":
          setCursor(0);
          break;
        case "G":
          setCursor(Math.max(0, results.length - 1));
          break;
      }
    }
  });

  const selectedResult = results[cursor];

  return (
    <box flexDirection="column" width="100%" height="100%">
      {/* Search input */}
      <box height={3} paddingLeft={1} paddingTop={1}>
        <text>
          <b fg="#aa77ff">Search: </b>
          <span fg={inputMode ? "#ffffff" : "#888888"}>
            {query}
            {inputMode && <span fg="#00ff00">_</span>}
          </span>
        </text>
        {!inputMode && (
          <text fg="#666666">{"  "}(press / to edit query)</text>
        )}
      </box>

      {/* Stats line */}
      {stats && (
        <box height={1} paddingLeft={1}>
          <text fg="#666666">
            {stats.total_results} results | {stats.latency_ms}ms
          </text>
        </box>
      )}

      {/* Main content area */}
      <box flexGrow={1} flexDirection="row">
        {/* Results list */}
        <box width="50%" flexDirection="column">
          {isLoading ? (
            <box padding={1}>
              <text fg="#888888">Searching...</text>
            </box>
          ) : error ? (
            <box padding={1}>
              <text fg="#ff0000">Error: {error.message}</text>
            </box>
          ) : query.length === 0 ? (
            <box padding={1}>
              <text fg="#888888">Type a query and press Enter to search</text>
              <text> </text>
              <text fg="#666666">
                Searches across: symbols, sessions, memories, tasks
              </text>
            </box>
          ) : results.length === 0 ? (
            <box padding={1}>
              <text fg="#888888">No results found</text>
            </box>
          ) : (
            <box flexDirection="column" overflow="hidden" paddingLeft={1}>
              {results.map((result, i) => {
                const isSelected = i === cursor && !inputMode;
                const bg = isSelected ? "#444444" : undefined;
                const scoreStr = `${Math.round(result.similarity * 100)}%`;
                const pathDisplay =
                  result.name && result.name !== result.path
                    ? result.name
                    : result.path.length > 40
                      ? "..." + result.path.slice(-37)
                      : result.path;

                return (
                  <box
                    key={`${result.source}-${result.id}-${i}`}
                    height={2}
                    backgroundColor={bg}
                    flexDirection="column"
                  >
                    <text fg={isSelected ? "#ffffff" : "#cccccc"}>
                      {isSelected ? "> " : "  "}
                      <span fg={sourceColor(result.source)}>
                        [{result.source}]
                      </span>
                      {"  "}
                      <span fg="#ffaa00">{scoreStr}</span>
                      {"  "}
                      {pathDisplay}
                    </text>
                  </box>
                );
              })}
            </box>
          )}
        </box>

        {/* Result detail */}
        <box
          flexGrow={1}
          borderStyle="single"
          borderColor="#444444"
          border={["left"]}
        >
          {selectedResult ? (
            <box flexDirection="column" padding={1}>
              <text fg="#aa77ff">
                <b>Search Result</b>
                {"  "}
                <span fg={sourceColor(selectedResult.source)}>
                  [{selectedResult.source}]
                </span>
              </text>
              <text> </text>
              <text>
                <b fg="#666666">ID: </b>
                <span fg="#ffffff">{selectedResult.id}</span>
              </text>
              {selectedResult.name && (
                <text>
                  <b fg="#666666">Name: </b>
                  <span fg="#ffffff">{selectedResult.name}</span>
                </text>
              )}
              <text>
                <b fg="#666666">Path: </b>
                <span fg="#ffffff">{selectedResult.path}</span>
              </text>
              <text>
                <b fg="#666666">Similarity: </b>
                <span fg="#ffaa00">
                  {(selectedResult.similarity * 100).toFixed(2)}%
                </span>
              </text>
              {selectedResult.rerank_score !== undefined &&
                selectedResult.rerank_score > 0 && (
                  <text>
                    <b fg="#666666">Rerank Score: </b>
                    <span fg="#00ff00">
                      {selectedResult.rerank_score.toFixed(4)}
                    </span>
                  </text>
                )}
              <text>
                <b fg="#666666">Rank: </b>
                <span fg="#ffffff">#{selectedResult.rank}</span>
              </text>
            </box>
          ) : (
            <box padding={1}>
              <text fg="#666666">Select a result to view details</text>
            </box>
          )}
        </box>
      </box>
    </box>
  );
}
