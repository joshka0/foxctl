// CAS View - Content Addressable Storage browser with preview pane
import { useState } from "react";
import { useKeyboard } from "@opentui/react";
import { useCASObjects, useCASContent } from "../hooks/useData";
import { WINDOWED_LIST_HEIGHT } from "../constants";
import type { CASObject } from "@foxctl/data";

function formatBytes(bytes: number | undefined): string {
  if (bytes === undefined) return "?";
  if (bytes < 1024) return `${bytes}B`;
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)}KB`;
  return `${(bytes / (1024 * 1024)).toFixed(1)}MB`;
}

function formatDate(isoString: string | undefined): string {
  if (!isoString) return "";
  try {
    const date = new Date(isoString);
    return date.toLocaleDateString("en-US", { month: "short", day: "numeric" });
  } catch {
    return "";
  }
}

function kindColor(kind: string | undefined): string {
  if (!kind) return "#888888";
  if (kind.includes("json")) return "#00ff00";
  if (kind.includes("text")) return "#00ffff";
  if (kind.includes("html")) return "#ff00ff";
  if (kind.includes("xml")) return "#ffff00";
  return "#888888";
}

interface CASRowProps {
  obj: CASObject;
  selected: boolean;
}

function CASRow({ obj, selected }: CASRowProps) {
  const bg = selected ? "#444444" : undefined;
  const cursor = selected ? "> " : "  ";

  // Extract short digest (last 8 chars of hex)
  const shortDigest = obj.digest.replace("sha256:", "").slice(-8);
  const kind = obj.kind?.split("/").pop() || "binary";
  const tags = obj.tags?.slice(0, 2).join(",") || "";

  return (
    <box height={1} backgroundColor={bg} flexDirection="row">
      <text fg="#ffffff">
        {cursor}
        <span fg="#aa77ff">{shortDigest}</span>
        {"  "}
        <span fg={kindColor(obj.kind)}>{kind.padEnd(12)}</span>
        {"  "}
        <span fg="#00ff00">{formatBytes(obj.size_bytes).padStart(8)}</span>
        {"  "}
        <span fg="#666666">{formatDate(obj.created_at).padEnd(8)}</span>
        {"  "}
        <span fg="#888888">{tags.slice(0, 20)}</span>
      </text>
    </box>
  );
}

interface CASPreviewProps {
  obj: CASObject | undefined;
  content: string | undefined;
  page: number;
  totalPages: number;
  isLoading: boolean;
}

function CASPreview({ obj, content, page, totalPages, isLoading }: CASPreviewProps) {
  if (!obj) {
    return (
      <box padding={1}>
        <text fg="#666666">Select an object to view content</text>
      </box>
    );
  }

  return (
    <box flexDirection="column" padding={1}>
      <box flexDirection="row" justifyContent="space-between">
        <text fg="#aa77ff">
          <b>CAS OBJECT</b>
        </text>
        <text fg="#444444">{obj.digest.replace("sha256:", "").slice(0, 16)}...</text>
      </box>
      <text> </text>
      <box flexDirection="row">
        <text>
          <b fg="#666666">Type: </b>
          <span fg={kindColor(obj.kind)}>{obj.kind || "unknown"}</span>
        </text>
        <text fg="#666666">{"  "}|{"  "}</text>
        <text>
          <b fg="#666666">Size: </b>
          <span fg="#00ff00">{formatBytes(obj.size_bytes)}</span>
        </text>
        {obj.tags && obj.tags.length > 0 && (
          <>
            <text fg="#666666">{"  "}|{"  "}</text>
            <text>
              <b fg="#666666">Tags: </b>
              <span fg="#888888">{obj.tags.join(", ")}</span>
            </text>
          </>
        )}
      </box>
      <text> </text>
      <box flexDirection="row" justifyContent="space-between">
        <text fg="#aa77ff">
          <b>Content Preview:</b>
        </text>
        {totalPages > 1 && (
          <text fg="#666666">
            Page {page}/{totalPages} (h/l to navigate)
          </text>
        )}
      </box>

      {isLoading ? (
        <box paddingTop={1}>
          <text fg="#666666">Loading content...</text>
        </box>
      ) : content ? (
        <box paddingLeft={1} paddingTop={1} overflow="hidden">
          <text fg="#cccccc">
            {content.slice(0, 2000)}
            {content.length > 2000 && "\n..."}
          </text>
        </box>
      ) : (
        <box paddingTop={1}>
          <text fg="#666666">No content available</text>
        </box>
      )}
    </box>
  );
}

export function CASView() {
  const [cursor, setCursor] = useState(0);
  const [scrollOffset, setScrollOffset] = useState(0);
  const [page, setPage] = useState(1);
  const { data: objects, isLoading, error, refetch } = useCASObjects();

  const LIST_HEIGHT = WINDOWED_LIST_HEIGHT;

  const selectedObj = objects?.[cursor];
  const { data: contentData, isLoading: contentLoading } = useCASContent({
    digest: selectedObj?.digest,
    page,
    pageSize: 2048,
  });

  const updateCursor = (newCursor: number) => {
    if (!objects) return;
    setCursor(newCursor);
    setPage(1); // Reset page when changing object
    if (newCursor < scrollOffset) {
      setScrollOffset(newCursor);
    } else if (newCursor >= scrollOffset + LIST_HEIGHT) {
      setScrollOffset(newCursor - LIST_HEIGHT + 1);
    }
  };

  useKeyboard((e) => {
    if (!objects) return;

    switch (e.name) {
      case "up":
      case "k":
        updateCursor(Math.max(0, cursor - 1));
        break;
      case "down":
      case "j":
        updateCursor(Math.min(Math.max(0, objects.length - 1), cursor + 1));
        break;
      case "r":
        refetch();
        break;
      case "g":
        updateCursor(0);
        break;
      case "G":
        updateCursor(Math.max(0, objects.length - 1));
        break;
      case "h":
        // Previous page
        if (contentData && page > 1) {
          setPage(page - 1);
        }
        break;
      case "l":
        // Next page
        if (contentData && contentData.next_page) {
          setPage(contentData.next_page);
        }
        break;
    }
  });

  if (isLoading && !objects) {
    return (
      <box padding={1}>
        <text fg="#888888">Loading CAS objects...</text>
      </box>
    );
  }

  if (error) {
    const errorMessage = error instanceof Error ? error.message : String(error);
    return (
      <box padding={1}>
        <text fg="#ff0000">Error loading CAS: {errorMessage}</text>
        <text fg="#666666">Press r to retry</text>
      </box>
    );
  }

  if (!objects || objects.length === 0) {
    return (
      <box padding={1}>
        <text fg="#888888">No CAS objects found</text>
      </box>
    );
  }

  return (
    <box flexDirection="row" width="100%" height="100%">
      {/* Object list */}
      <box width="45%" flexDirection="column" borderStyle="single" borderColor="#333333" border={["right"]}>
        <box height={3} paddingLeft={1} paddingTop={1} borderStyle="single" borderColor="#333333" border={["bottom"]}>
          <text fg="#aa77ff">
            <b>CONTENT ADDRESSABLE STORAGE</b>
            <span fg="#666666"> ({objects.length})</span>
          </text>
          <text fg="#666666">
            DIGEST    TYPE          SIZE      DATE      TAGS
          </text>
        </box>
        <box flexGrow={1} flexDirection="column" overflow="hidden">
          {objects.slice(scrollOffset, scrollOffset + LIST_HEIGHT).map((obj, i) => (
            <CASRow
              key={obj.digest}
              obj={obj}
              selected={i + scrollOffset === cursor}
            />
          ))}
        </box>
      </box>

      {/* Preview pane */}
      <box
        flexGrow={1}
        borderStyle="single"
        borderColor="#444444"
        border={["left"]}
      >
        <CASPreview
          obj={selectedObj}
          content={contentData?.content}
          page={contentData?.page ?? 1}
          totalPages={contentData?.total_pages ?? 1}
          isLoading={contentLoading}
        />
      </box>
    </box>
  );
}
