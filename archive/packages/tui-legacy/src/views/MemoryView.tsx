// Memory View - Browse named memories with semantic search
import { useState, useMemo } from "react";
import { useKeyboard } from "@opentui/react";
import { useMemoryEntry, useMemoryEntries, useMemoryTypes, useSearch, usePinMemory, useDeleteMemory, useSaveMemory } from "../hooks/useData";
import { useTimeoutManager } from "../hooks/useTimeoutManager";
import { WINDOWED_LIST_HEIGHT } from "../constants";
import type { MemoryEntry, MemoryTypeCount, MemoryEntryDetail, SearchResult } from "@agentctl/data";

// Filter out embedding arrays from data for display (they're just walls of numbers)
function filterEmbeddings(data: unknown): unknown {
  if (data === null || data === undefined) return data;
  if (Array.isArray(data)) {
    // If it looks like an embedding array (all numbers), replace with placeholder
    if (data.length > 10 && data.every((v) => typeof v === "number")) {
      return `[embedding: ${data.length} dimensions]`;
    }
    return data.map(filterEmbeddings);
  }
  if (typeof data === "object") {
    const result: Record<string, unknown> = {};
    for (const [key, value] of Object.entries(data as Record<string, unknown>)) {
      // Skip embedding keys entirely or filter their content
      if (key === "embedding" || key === "embeddings") {
        if (Array.isArray(value) && value.length > 0) {
          result[key] = `[${value.length} dimensions]`;
        }
        continue;
      }
      result[key] = filterEmbeddings(value);
    }
    return result;
  }
  return data;
}

function typeColor(type: string): string {
  switch (type) {
    case "session_snapshot":
      return "#00ffff";
    case "codemap":
      return "#ff00ff";
    case "plan_sync_state":
      return "#ffff00";
    case "file_embedding":
      return "#666666";
    case "task_embedding":
      return "#00ff00";
    case "code_symbol":
      return "#0088ff";
    case "edit":
      return "#ffaa00";
    case "gotcha":
      return "#ff4444";
    case "memory":
      return "#44ff44";
    case "decision":
      return "#ff88ff";
    case "pattern":
      return "#88ff88";
    case "context":
      return "#ffff88";
    case "insight":
      return "#88ffff";
    default:
      return "#888888";
  }
}

function sourceColor(source: string): string {
  switch (source) {
    case "symbols":
      return "#0088ff";
    case "memory":
      return "#44ff44";
    case "sessions":
      return "#00ffff";
    case "tasks":
      return "#ffff00";
    case "codemaps":
      return "#ff00ff";
    default:
      return "#888888";
  }
}

function typeLabel(type: string): string {
  switch (type) {
    case "session_snapshot":
      return "Session";
    case "codemap":
      return "Codemap";
    case "plan_sync_state":
      return "Plan";
    case "file_embedding":
      return "File";
    case "task_embedding":
      return "Task";
    case "code_symbol":
      return "Symbol";
    case "edit":
      return "Edit";
    case "gotcha":
      return "Gotcha";
    case "memory":
      return "Memory";
    case "decision":
      return "Decision";
    case "pattern":
      return "Pattern";
    case "context":
      return "Context";
    case "insight":
      return "Insight";
    default:
      return type.slice(0, 10);
  }
}

function formatDate(isoString: string | undefined): string {
  if (!isoString) return "";
  try {
    const date = new Date(isoString);
    return date.toLocaleDateString("en-US", { month: "short", day: "numeric", hour: "2-digit", minute: "2-digit" });
  } catch {
    return "";
  }
}

function truncate(s: string | undefined, len: number): string {
  if (!s) return "";
  return s.length > len ? s.slice(0, len - 3) + "..." : s;
}

function formatSimilarity(score: number): string {
  return (score * 100).toFixed(0) + "%";
}

interface MemoryRowProps {
  entry: MemoryEntry;
  selected: boolean;
}

function MemoryRow({ entry, selected }: MemoryRowProps) {
  const bg = selected ? "#444444" : undefined;
  const cursor = selected ? "> " : "  ";
  const pinIndicator = entry.pinned_at ? "*" : " ";

  return (
    <box height={1} backgroundColor={bg} flexDirection="row">
      <text fg="#ffffff">
        {cursor}
        <span fg="#ffff00">{pinIndicator}</span>
        <span fg={typeColor(entry.type)}>{typeLabel(entry.type).padEnd(10)}</span>
        {"  "}
        <span fg="#888888">{formatDate(entry.updated_at).padEnd(18)}</span>
        {"  "}
        <span fg="#cccccc">{truncate(entry.summary || entry.name, 58)}</span>
      </text>
    </box>
  );
}

// Search result row with similarity score
interface SearchRowProps {
  result: SearchResult;
  selected: boolean;
}

function SearchRow({ result, selected }: SearchRowProps) {
  const bg = selected ? "#444444" : undefined;
  const cursor = selected ? "> " : "  ";
  const similarity = formatSimilarity(result.similarity);

  return (
    <box height={1} backgroundColor={bg} flexDirection="row">
      <text fg="#ffffff">
        {cursor}
        <span fg={sourceColor(result.source)}>{result.source.padEnd(8)}</span>
        {"  "}
        <span fg="#00ff00">{similarity.padEnd(5)}</span>
        {"  "}
        <span fg="#cccccc">{truncate(result.summary || result.name || result.path, 55)}</span>
      </text>
    </box>
  );
}

interface TypeFilterProps {
  types: MemoryTypeCount[];
  selected: string;
  onSelect: (type: string) => void;
}

function TypeFilter({ types, selected }: TypeFilterProps) {
  // Prioritize important types: gotcha, decision, pattern, context
  const priorityTypes = ["gotcha", "decision", "pattern", "context"];
  const sortedTypes = [...types].sort((a, b) => {
    const aIdx = priorityTypes.indexOf(a.type);
    const bIdx = priorityTypes.indexOf(b.type);
    if (aIdx >= 0 && bIdx >= 0) return aIdx - bIdx;
    if (aIdx >= 0) return -1;
    if (bIdx >= 0) return 1;
    return b.count - a.count;
  });

  return (
    <box height={1} flexDirection="row" paddingLeft={1}>
      <text fg="#666666">Type: </text>
      <text fg={!selected ? "#00ff00" : "#666666"}>
        {!selected ? <b>[All]</b> : "[All]"}
      </text>
      {sortedTypes.slice(0, 6).map((t) => (
        <text key={t.type} fg={selected === t.type ? "#00ff00" : "#666666"}>
          {" "}
          {selected === t.type ? <b>[{typeLabel(t.type)}:{t.count}]</b> : `[${typeLabel(t.type)}:${t.count}]`}
        </text>
      ))}
    </box>
  );
}

interface MemoryDetailProps {
  entry: MemoryEntry | undefined;
  detail: ReturnType<typeof useMemoryEntry>["data"];
  isLoading: boolean;
}

function MemoryDetail({ entry, detail, isLoading }: MemoryDetailProps) {
  if (!entry) {
    return (
      <box padding={1}>
        <text fg="#666666">Select a memory entry to view details</text>
      </box>
    );
  }

  return (
    <box flexDirection="column" padding={1}>
      <box flexDirection="row" justifyContent="space-between">
        <text fg={typeColor(entry.type)}>
          <b>{typeLabel(entry.type).toUpperCase()}</b>
        </text>
        <text fg="#444444">{entry.id.slice(0, 8)}...</text>
      </box>
      <text> </text>
      <box flexDirection="row">
        <text>
          <b fg="#666666">Updated: </b>
          <span fg="#888888">{formatDate(entry.updated_at)}</span>
        </text>
        <text fg="#666666">{"  |  "}</text>
        <text>
          <b fg="#666666">Accessed: </b>
          <span fg="#888888">{entry.access_count}x</span>
        </text>
      </box>
      <text> </text>
      <text fg="#888888">
        <b>Name: </b>
        {entry.name}
      </text>
      {entry.session_id && (
        <text fg="#666666">
          <b>Session: </b>
          {entry.session_id.slice(0, 16)}...
        </text>
      )}
      <text> </text>
      <text fg="#aa77ff">
        <b>Summary:</b>
      </text>
      <box paddingLeft={1} paddingTop={1}>
        <text fg="#cccccc">{entry.summary || "(no summary)"}</text>
      </box>

      {isLoading ? (
        <box paddingTop={1}>
          <text fg="#666666">Loading details...</text>
        </box>
      ) : detail?.data ? (
        <>
          <text> </text>
          <text fg="#aa77ff">
            <b>Data Preview:</b>
          </text>
          <box paddingLeft={1} paddingTop={1} overflow="hidden">
            <text fg="#888888">
              {truncate(
                typeof detail.data === "string"
                  ? detail.data
                  : JSON.stringify(filterEmbeddings(detail.data), null, 2),
                1500
              )}
            </text>
          </box>
        </>
      ) : null}
    </box>
  );
}

// Search result detail panel
interface SearchDetailProps {
  result: SearchResult | undefined;
}

function SearchDetail({ result }: SearchDetailProps) {
  if (!result) {
    return (
      <box padding={1}>
        <text fg="#666666">Select a search result to view details</text>
      </box>
    );
  }

  return (
    <box flexDirection="column" padding={1}>
      <box flexDirection="row" justifyContent="space-between">
        <text fg={sourceColor(result.source)}>
          <b>{result.source.toUpperCase()}</b>
        </text>
        <text fg="#00ff00">
          <b>{formatSimilarity(result.similarity)}</b>
        </text>
      </box>
      <text> </text>
      <text fg="#888888">
        <b>Path: </b>
        {result.path}
        {result.line ? `:${result.line}` : ""}
      </text>
      {result.name && (
        <text fg="#888888">
          <b>Name: </b>
          {result.name}
        </text>
      )}
      <text> </text>
      {result.summary && (
        <>
          <text fg="#aa77ff">
            <b>Summary:</b>
          </text>
          <box paddingLeft={1} paddingTop={1}>
            <text fg="#cccccc">{result.summary}</text>
          </box>
        </>
      )}
      {result.snippet && (
        <>
          <text> </text>
          <text fg="#ffff00">
            <b>Snippet:</b>
          </text>
          <box paddingLeft={1} paddingTop={1} overflow="hidden">
            <text fg="#888888">{truncate(result.snippet, 1000)}</text>
          </box>
        </>
      )}
      {result.rerank_score !== undefined && (
        <text fg="#666666">
          <b>Rerank Score: </b>
          {formatSimilarity(result.rerank_score)}
        </text>
      )}
    </box>
  );
}

// Full-screen memory viewer
interface MemoryFullViewProps {
  entry: MemoryEntry;
  detail: MemoryEntryDetail | undefined;
  isLoading: boolean;
  onClose: () => void;
}

function MemoryFullView({ entry, detail, isLoading, onClose }: MemoryFullViewProps) {
  useKeyboard((e) => {
    switch (e.name) {
      case "escape":
      case "q":
        onClose();
        break;
    }
  });

  // Format data for display, filtering out embeddings
  const formatData = (data: unknown): string => {
    if (!data) return "(no data)";
    if (typeof data === "string") return data;
    const filtered = filterEmbeddings(data);
    return JSON.stringify(filtered, null, 2);
  };

  return (
    <box flexDirection="column" width="100%" height="100%">
      {/* Header */}
      <box height={2} paddingLeft={1} borderStyle="single" borderColor="#333333" border={["bottom"]}>
        <text fg={typeColor(entry.type)}>
          <b>{typeLabel(entry.type).toUpperCase()}</b>
          <span fg="#666666"> | {entry.id.slice(0, 24)}...</span>
        </text>
        <text fg="#666666">q/Esc: close</text>
      </box>

      {/* Metadata */}
      <box height={6} paddingLeft={1} paddingTop={1} borderStyle="single" borderColor="#333333" border={["bottom"]}>
        <text fg="#888888">
          <b>Name: </b>
          {entry.name}
        </text>
        <box flexDirection="row">
          <text>
            <b fg="#666666">Updated: </b>
            <span fg="#888888">{formatDate(entry.updated_at)}</span>
          </text>
          <text fg="#666666">{"  |  "}</text>
          <text>
            <b fg="#666666">Accessed: </b>
            <span fg="#888888">{entry.access_count}x</span>
          </text>
          {entry.session_id && (
            <>
              <text fg="#666666">{"  |  "}</text>
              <text>
                <b fg="#666666">Session: </b>
                <span fg="#888888">{entry.session_id.slice(0, 16)}...</span>
              </text>
            </>
          )}
        </box>
        <text> </text>
        <text fg="#aa77ff">
          <b>Summary: </b>
          <span fg="#cccccc">{entry.summary || "(no summary)"}</span>
        </text>
      </box>

      {/* Data content */}
      <box flexGrow={1} flexDirection="column" padding={1} overflow="scroll">
        <text fg="#ffff00">
          <b>Data:</b>
        </text>
        <box paddingTop={1}>
          {isLoading ? (
            <text fg="#666666">Loading...</text>
          ) : (
            <text fg="#cccccc">{formatData(detail?.data)}</text>
          )}
        </box>
      </box>
    </box>
  );
}

export function MemoryView() {
  const [cursor, setCursor] = useState(0);
  const [scrollOffset, setScrollOffset] = useState(0);
  const [typeFilter, setTypeFilter] = useState("");
  const [typeIndex, setTypeIndex] = useState(-1); // -1 = All
  const [showFullView, setShowFullView] = useState(false);

  // Search mode state
  const [searchMode, setSearchMode] = useState(false);
  const [searchInput, setSearchInput] = useState("");
  const [searchQuery, setSearchQuery] = useState("");
  const [searchCursor, setSearchCursor] = useState(0);
  const [searchScroll, setSearchScroll] = useState(0);

  // Add memory mode state
  const [addMode, setAddMode] = useState(false);
  const [addField, setAddField] = useState<"type" | "name" | "summary">("type");
  const [addType, setAddType] = useState("gotcha");
  const [addName, setAddName] = useState("");
  const [addSummary, setAddSummary] = useState("");
  const [addStatus, setAddStatus] = useState<"idle" | "saving" | "success" | "error">("idle");
  const [addError, setAddError] = useState("");

  const { save: saveMemoryFn } = useSaveMemory();
  const { pin: pinMemoryFn } = usePinMemory();
  const { remove: deleteMemoryFn } = useDeleteMemory();

  // Delete confirmation state
  const [confirmDelete, setConfirmDelete] = useState(false);
  const [actionStatus, setActionStatus] = useState<{ type: "pin" | "delete"; status: "success" | "error"; message: string } | null>(null);

  const memoryTypes = ["gotcha", "decision", "pattern", "context", "insight"];
  const addTypeIndex = memoryTypes.indexOf(addType);

  const { data: typesData } = useMemoryTypes();
  const types = typesData || [];

  // Filter out file_embedding by default as there are too many
  const filteredTypes = types.filter((t) => t.type !== "file_embedding" && t.type !== "code_symbol");
  const allTypes = ["", ...filteredTypes.map((t) => t.type)];

  const { data, isLoading, error, refetch } = useMemoryEntries({
    type: typeFilter || undefined,
    limit: 100,
  });

  // Search results
  const { data: searchData, isLoading: searchLoading } = useSearch({
    q: searchQuery,
    limit: 50,
    rerank: true,
  });

  const memories = data?.memories || [];
  const selectedEntry = memories[cursor];
  const searchResults = searchData?.results || [];
  const selectedSearchResult = searchResults[searchCursor];

  // Group search results by source
  const groupedResults = useMemo(() => {
    if (!searchResults.length) return {};
    const groups: Record<string, SearchResult[]> = {};
    for (const r of searchResults) {
      if (!groups[r.source]) groups[r.source] = [];
      groups[r.source].push(r);
    }
    return groups;
  }, [searchResults]);

  const { data: detail, isLoading: detailLoading } = useMemoryEntry(selectedEntry?.id);
  const setManagedTimeout = useTimeoutManager();

  const LIST_HEIGHT = WINDOWED_LIST_HEIGHT;

  const updateCursor = (newCursor: number) => {
    setCursor(newCursor);
    if (newCursor < scrollOffset) {
      setScrollOffset(newCursor);
    } else if (newCursor >= scrollOffset + LIST_HEIGHT) {
      setScrollOffset(newCursor - LIST_HEIGHT + 1);
    }
  };

  const updateSearchCursor = (newCursor: number) => {
    setSearchCursor(newCursor);
    if (newCursor < searchScroll) {
      setSearchScroll(newCursor);
    } else if (newCursor >= searchScroll + LIST_HEIGHT) {
      setSearchScroll(newCursor - LIST_HEIGHT + 1);
    }
  };

  const handleSaveMemory = async () => {
    if (!addName.trim()) {
      setAddError("Name is required");
      return;
    }
    setAddStatus("saving");
    setAddError("");
    try {
      await saveMemoryFn({
        name: addName.trim(),
        type: addType,
        summary: addSummary.trim(),
      });
      setAddStatus("success");
      setAddName("");
      setAddSummary("");
      refetch();
      // Auto-close after success
      setManagedTimeout(() => {
        setAddMode(false);
        setAddStatus("idle");
      }, 1000);
    } catch (err) {
      setAddStatus("error");
      setAddError(err instanceof Error ? err.message : "Failed to save");
    }
  };

  const handlePinMemory = async () => {
    if (!selectedEntry) return;
    try {
      const result = await pinMemoryFn(selectedEntry.id);
      setActionStatus({
        type: "pin",
        status: "success",
        message: result.pinned ? "Pinned" : "Unpinned",
      });
      refetch();
      setManagedTimeout(() => setActionStatus(null), 2000);
    } catch (err) {
      setActionStatus({
        type: "pin",
        status: "error",
        message: err instanceof Error ? err.message : "Failed to pin",
      });
      setManagedTimeout(() => setActionStatus(null), 3000);
    }
  };

  const handleDeleteMemory = async () => {
    if (!selectedEntry) return;
    try {
      await deleteMemoryFn(selectedEntry.id);
      setActionStatus({
        type: "delete",
        status: "success",
        message: "Deleted",
      });
      setConfirmDelete(false);
      // Move cursor if needed
      if (cursor >= memories.length - 1) {
        setCursor(Math.max(0, cursor - 1));
      }
      refetch();
      setManagedTimeout(() => setActionStatus(null), 2000);
    } catch (err) {
      setActionStatus({
        type: "delete",
        status: "error",
        message: err instanceof Error ? err.message : "Failed to delete",
      });
      setConfirmDelete(false);
      setManagedTimeout(() => setActionStatus(null), 3000);
    }
  };

  useKeyboard((e) => {
    if (showFullView) return; // Full view handles its own keys

    // Delete confirmation mode
    if (confirmDelete) {
      switch (e.name) {
        case "y":
          handleDeleteMemory();
          return;
        case "n":
        case "escape":
          setConfirmDelete(false);
          return;
      }
      return; // Ignore other keys during confirmation
    }

    // Add memory mode
    if (addMode) {
      switch (e.name) {
        case "escape":
          setAddMode(false);
          setAddStatus("idle");
          setAddError("");
          return;
        case "tab":
          // Cycle through fields
          if (addField === "type") setAddField("name");
          else if (addField === "name") setAddField("summary");
          else setAddField("type");
          return;
        case "return":
          if (addField === "summary" || (addField === "name" && addName.trim())) {
            handleSaveMemory();
          } else if (addField === "type") {
            setAddField("name");
          } else if (addField === "name") {
            setAddField("summary");
          }
          return;
        case "up":
        case "k":
          if (addField === "type") {
            const newIdx = (addTypeIndex - 1 + memoryTypes.length) % memoryTypes.length;
            setAddType(memoryTypes[newIdx]);
          }
          return;
        case "down":
        case "j":
          if (addField === "type") {
            const newIdx = (addTypeIndex + 1) % memoryTypes.length;
            setAddType(memoryTypes[newIdx]);
          }
          return;
        case "backspace":
          if (addField === "name") {
            setAddName((prev) => prev.slice(0, -1));
          } else if (addField === "summary") {
            setAddSummary((prev) => prev.slice(0, -1));
          }
          return;
        case "space":
          if (addField === "name") {
            setAddName((prev) => prev + " ");
          } else if (addField === "summary") {
            setAddSummary((prev) => prev + " ");
          }
          return;
        default:
          // Handle printable characters
          if (e.raw && e.raw.length === 1 && !e.ctrl && !e.meta) {
            if (addField === "name") {
              setAddName((prev) => prev + e.raw);
            } else if (addField === "summary") {
              setAddSummary((prev) => prev + e.raw);
            }
          }
          return;
      }
    }

    // Search input mode
    if (searchMode && searchInput !== null) {
      switch (e.name) {
        case "escape":
          if (searchInput === "" && searchQuery === "") {
            setSearchMode(false);
          } else {
            setSearchInput("");
            setSearchQuery("");
          }
          return;
        case "return":
          setSearchQuery(searchInput);
          setSearchCursor(0);
          setSearchScroll(0);
          return;
        case "backspace":
          setSearchInput((prev) => prev.slice(0, -1));
          return;
        case "space":
          setSearchInput((prev) => prev + " ");
          return;
        default:
          // Handle printable characters
          if (e.raw && e.raw.length === 1 && !e.ctrl && !e.meta) {
            setSearchInput((prev) => prev + e.raw);
          }
          return;
      }
    }

    // Search results navigation
    if (searchMode && searchQuery) {
      switch (e.name) {
        case "escape":
          setSearchQuery("");
          setSearchInput("");
          return;
        case "up":
        case "k":
          updateSearchCursor(Math.max(0, searchCursor - 1));
          return;
        case "down":
        case "j":
          updateSearchCursor(Math.min(Math.max(0, searchResults.length - 1), searchCursor + 1));
          return;
        case "q":
          setSearchMode(false);
          setSearchQuery("");
          setSearchInput("");
          return;
      }
    }

    // Normal mode
    switch (e.name) {
      case "up":
      case "k":
        updateCursor(Math.max(0, cursor - 1));
        break;
      case "down":
      case "j":
        updateCursor(Math.min(Math.max(0, memories.length - 1), cursor + 1));
        break;
      case "return":
        if (selectedEntry) {
          setShowFullView(true);
        }
        break;
      case "r":
        refetch();
        break;
      case "g":
        updateCursor(0);
        break;
      case "G":
        updateCursor(Math.max(0, memories.length - 1));
        break;
      case "t":
      case "tab": {
        // Cycle through types
        const nextIdx = (typeIndex + 1) % allTypes.length;
        setTypeIndex(nextIdx);
        setTypeFilter(allTypes[nextIdx]);
        setCursor(0);
        setScrollOffset(0);
        break;
      }
      case "T": {
        // Cycle backwards
        const prevIdx = (typeIndex - 1 + allTypes.length) % allTypes.length;
        setTypeIndex(prevIdx);
        setTypeFilter(allTypes[prevIdx]);
        setCursor(0);
        setScrollOffset(0);
        break;
      }
      case "/":
        // Enter search mode
        setSearchMode(true);
        setSearchInput("");
        break;
      case "a":
        // Enter add memory mode
        setAddMode(true);
        setAddField("type");
        setAddName("");
        setAddSummary("");
        setAddStatus("idle");
        setAddError("");
        break;
      case "p":
        // Pin/unpin selected memory
        if (selectedEntry) {
          handlePinMemory();
        }
        break;
      case "d":
        // Delete selected memory (with confirmation)
        if (selectedEntry) {
          setConfirmDelete(true);
        }
        break;
    }
  });

  if (isLoading && !data) {
    return (
      <box padding={1}>
        <text fg="#888888">Loading memories...</text>
      </box>
    );
  }

  if (error) {
    return (
      <box padding={1}>
        <text fg="#ff0000">Error loading memories: {error.message}</text>
        <text fg="#666666">Press r to retry</text>
      </box>
    );
  }

  // Show full view when Enter is pressed
  if (showFullView && selectedEntry) {
    return (
      <MemoryFullView
        entry={selectedEntry}
        detail={detail}
        isLoading={detailLoading}
        onClose={() => setShowFullView(false)}
      />
    );
  }

  // Add memory mode UI
  if (addMode) {
    return (
      <box flexDirection="column" width="100%" height="100%">
        {/* Header */}
        <box height={2} paddingLeft={1} paddingTop={1} borderStyle="single" borderColor="#333333" border={["bottom"]}>
          <text fg="#44ff44">
            <b>ADD MEMORY</b>
            <span fg="#666666"> | Tab: next field | Enter: save | Esc: cancel</span>
          </text>
        </box>

        {/* Form */}
        <box flexDirection="column" padding={2} flexGrow={1}>
          {/* Type selector */}
          <box flexDirection="row" height={1}>
            <text fg={addField === "type" ? "#00ff00" : "#666666"}>
              <b>Type: </b>
            </text>
            {memoryTypes.map((t, i) => (
              <text key={t} fg={addType === t ? "#00ff00" : "#666666"}>
                {addType === t ? <b>[{typeLabel(t)}]</b> : `[${typeLabel(t)}]`}
                {" "}
              </text>
            ))}
            {addField === "type" && <text fg="#666666"> (j/k to change)</text>}
          </box>
          <text> </text>

          {/* Name input */}
          <box flexDirection="row" height={1}>
            <text fg={addField === "name" ? "#00ff00" : "#666666"}>
              <b>Name: </b>
            </text>
            <text fg="#ffffff">{addName}</text>
            {addField === "name" && <text fg="#00ff00">_</text>}
          </box>
          <text> </text>

          {/* Summary input */}
          <box flexDirection="row" height={1}>
            <text fg={addField === "summary" ? "#00ff00" : "#666666"}>
              <b>Summary: </b>
            </text>
            <text fg="#ffffff">{addSummary}</text>
            {addField === "summary" && <text fg="#00ff00">_</text>}
          </box>
          <text> </text>

          {/* Status messages */}
          {addStatus === "saving" && (
            <text fg="#ffff00">Saving...</text>
          )}
          {addStatus === "success" && (
            <text fg="#00ff00">Memory saved successfully!</text>
          )}
          {addStatus === "error" && (
            <text fg="#ff0000">Error: {addError}</text>
          )}
        </box>

        {/* Footer */}
        <box height={1} paddingLeft={1} borderStyle="single" borderColor="#333333" border={["top"]}>
          <text fg="#666666">
            [Tab]next field [j/k]change type [Enter]save [Esc]cancel
          </text>
        </box>
      </box>
    );
  }

  // Search mode UI
  if (searchMode) {
    return (
      <box flexDirection="column" width="100%" height="100%">
        {/* Search header */}
        <box height={3} paddingLeft={1} paddingTop={1} borderStyle="single" borderColor="#333333" border={["bottom"]}>
          <text fg="#ffff00">
            <b>SEMANTIC SEARCH</b>
            <span fg="#666666"> | Searches: symbols, memory, sessions, tasks, codemaps</span>
          </text>
          <box flexDirection="row" paddingTop={1}>
            <text fg="#00ff00">/ </text>
            <text fg="#ffffff">{searchInput}</text>
            <text fg="#00ff00">_</text>
            {searchQuery && searchQuery !== searchInput && (
              <text fg="#666666"> (searching: "{searchQuery}")</text>
            )}
          </box>
        </box>

        {/* Results area */}
        <box flexDirection="row" flexGrow={1}>
          {/* Results list */}
          <box width="50%" flexDirection="column" borderStyle="single" borderColor="#333333" border={["right"]}>
            <box height={2} paddingLeft={1} borderStyle="single" borderColor="#333333" border={["bottom"]}>
              {searchLoading ? (
                <text fg="#888888">Searching...</text>
              ) : searchQuery ? (
                <text fg="#888888">
                  {searchResults.length} results
                  {searchData?.stats?.latency_ms ? ` (${searchData.stats.latency_ms}ms)` : ""}
                  {searchData?.stats?.reranked ? " [reranked]" : ""}
                </text>
              ) : (
                <text fg="#666666">Type query and press Enter to search</text>
              )}
              <text fg="#666666">
                SOURCE    SCORE  SUMMARY
              </text>
            </box>
            <box flexGrow={1} flexDirection="column" overflow="hidden">
              {searchResults.slice(searchScroll, searchScroll + LIST_HEIGHT).map((result, i) => (
                <SearchRow
                  key={`${result.source}-${result.id}`}
                  result={result}
                  selected={i + searchScroll === searchCursor}
                />
              ))}
              {!searchQuery && !searchLoading && (
                <box padding={1}>
                  <text fg="#666666">Press Enter to search, Esc to cancel</text>
                </box>
              )}
            </box>
          </box>

          {/* Result detail */}
          <box flexGrow={1} borderStyle="single" borderColor="#444444" border={["left"]}>
            <SearchDetail result={selectedSearchResult} />
          </box>
        </box>

        {/* Footer */}
        <box height={1} paddingLeft={1} borderStyle="single" borderColor="#333333" border={["top"]}>
          <text fg="#666666">
            [Enter]search [j/k]navigate [Esc]clear [q]exit search
          </text>
        </box>
      </box>
    );
  }

  // Normal memory list UI
  if (memories.length === 0) {
    return (
      <box padding={1} flexDirection="column">
        <TypeFilter types={filteredTypes} selected={typeFilter} onSelect={setTypeFilter} />
        <text fg="#888888">No memories found{typeFilter ? ` for type: ${typeFilter}` : ""}</text>
        <text fg="#666666">Press t to cycle types, / to search, r to refresh</text>
      </box>
    );
  }

  return (
    <box flexDirection="column" width="100%" height="100%">
      {/* Confirmation/Status bar */}
      {confirmDelete && (
        <box height={1} paddingLeft={1} backgroundColor="#880000">
          <text fg="#ffffff">
            <b>Delete "{truncate(selectedEntry?.name || "", 30)}"? </b>
            <span fg="#ffff00">[y]es [n]o</span>
          </text>
        </box>
      )}
      {actionStatus && !confirmDelete && (
        <box height={1} paddingLeft={1} backgroundColor={actionStatus.status === "success" ? "#006600" : "#660000"}>
          <text fg="#ffffff">
            {actionStatus.message}
          </text>
        </box>
      )}

      <box flexDirection="row" flexGrow={1}>
        {/* Memory list */}
        <box width="50%" flexDirection="column" borderStyle="single" borderColor="#333333" border={["right"]}>
          <box height={4} paddingLeft={1} paddingTop={1} borderStyle="single" borderColor="#333333" border={["bottom"]}>
            <text fg="#aa77ff">
              <b>MEMORY STORE</b>
              <span fg="#666666"> ({data?.total || 0}) | /: search | p: pin | d: delete</span>
            </text>
            <TypeFilter types={filteredTypes} selected={typeFilter} onSelect={setTypeFilter} />
            <text fg="#666666">
              {"  "}TYPE          UPDATED              SUMMARY
            </text>
          </box>
          <box flexGrow={1} flexDirection="column" overflow="hidden">
            {memories.slice(scrollOffset, scrollOffset + LIST_HEIGHT).map((entry, i) => (
              <MemoryRow
                key={entry.id}
                entry={entry}
                selected={i + scrollOffset === cursor}
              />
            ))}
          </box>
        </box>

        {/* Detail pane */}
        <box flexGrow={1} borderStyle="single" borderColor="#444444" border={["left"]}>
          <MemoryDetail entry={selectedEntry} detail={detail} isLoading={detailLoading} />
        </box>
      </box>
    </box>
  );
}
