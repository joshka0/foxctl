// Blackboard View - Key-value store browser
import { useState } from "react";
import { useKeyboard } from "@opentui/react";
import { useBlackboard } from "../hooks/useData";
import { WINDOWED_LIST_HEIGHT } from "../constants";
import type { BlackboardRecord } from "@agentctl/data";

interface RecordRowProps {
  record: BlackboardRecord;
  selected: boolean;
}

function RecordRow({ record, selected }: RecordRowProps) {
  const bg = selected ? "#444444" : undefined;
  const cursor = selected ? "> " : "  ";

  const topicDisplay =
    record.topic.length > 30 ? record.topic.slice(0, 27) + "..." : record.topic;

  // Check if expired
  const now = Date.now() / 1000;
  const expiresAt = record.ts + record.ttl_sec;
  const isExpired = expiresAt < now;

  return (
    <box height={1} backgroundColor={bg}>
      <text fg={isExpired ? "#666666" : "#ffffff"}>
        {cursor}
        <span fg="#00ffff">{record.ns.padEnd(10)}</span>
        {"  "}
        <span fg={isExpired ? "#666666" : "#ffaa00"}>{topicDisplay}</span>
      </text>
    </box>
  );
}

interface RecordDetailProps {
  record: BlackboardRecord | undefined;
}

function RecordDetail({ record }: RecordDetailProps) {
  if (!record) {
    return (
      <box padding={1}>
        <text fg="#666666">Select a record to view details</text>
      </box>
    );
  }

  // Format timestamp
  const date = new Date(record.ts * 1000);
  const timestamp = date.toISOString().replace("T", " ").slice(0, 19);

  // Check if expired
  const now = Date.now() / 1000;
  const expiresAt = record.ts + record.ttl_sec;
  const isExpired = expiresAt < now;
  const expiresDate = new Date(expiresAt * 1000);
  const expiresStr = expiresDate.toISOString().replace("T", " ").slice(0, 19);

  // Try to parse payload as JSON for pretty printing
  let payloadDisplay = record.payload;
  try {
    const parsed = JSON.parse(record.payload);
    payloadDisplay = JSON.stringify(parsed, null, 2);
  } catch {
    // Keep as-is if not JSON
  }

  return (
    <box flexDirection="column" padding={1}>
      <text fg="#aa77ff">
        <b>Blackboard Record</b>
      </text>
      <text> </text>
      <text>
        <b fg="#666666">ID: </b>
        <span fg="#ffffff">{record.id}</span>
      </text>
      <text>
        <b fg="#666666">Namespace: </b>
        <span fg="#00ffff">{record.ns}</span>
      </text>
      <text>
        <b fg="#666666">Topic: </b>
        <span fg="#ffaa00">{record.topic}</span>
      </text>
      <text>
        <b fg="#666666">Created: </b>
        <span fg="#ffffff">{timestamp}</span>
      </text>
      <text>
        <b fg="#666666">TTL: </b>
        <span fg="#ffffff">{record.ttl_sec}s</span>
      </text>
      <text>
        <b fg="#666666">Expires: </b>
        <span fg={isExpired ? "#ff0000" : "#00ff00"}>
          {expiresStr} {isExpired ? "(EXPIRED)" : ""}
        </span>
      </text>
      <text> </text>
      <text fg="#666666">
        <b>Payload:</b>
      </text>
      <text fg="#cccccc">
        {payloadDisplay.length > 500
          ? payloadDisplay.slice(0, 500) + "..."
          : payloadDisplay}
      </text>
    </box>
  );
}

export function BlackboardView() {
  const [cursor, setCursor] = useState(0);
  const [scrollOffset, setScrollOffset] = useState(0);
  const [namespace, setNamespace] = useState("default");

  const LIST_HEIGHT = WINDOWED_LIST_HEIGHT;

  const { data: records, isLoading, error, refetch } = useBlackboard({
    ns: namespace,
    limit: 100,
  });

  const selectedRecord = records?.[cursor];

  // Get unique namespaces from records
  const namespaces = Array.from(new Set(records?.map((r) => r.ns) || []));
  if (!namespaces.includes("default")) {
    namespaces.unshift("default");
  }

  const updateCursor = (newCursor: number) => {
    if (!records) return;
    setCursor(newCursor);
    if (newCursor < scrollOffset) {
      setScrollOffset(newCursor);
    } else if (newCursor >= scrollOffset + LIST_HEIGHT) {
      setScrollOffset(newCursor - LIST_HEIGHT + 1);
    }
  };

  useKeyboard((e) => {
    if (!records) return;
    switch (e.name) {
      case "up":
      case "k":
        updateCursor(Math.max(0, cursor - 1));
        break;
      case "down":
      case "j":
        updateCursor(Math.min(Math.max(0, records.length - 1), cursor + 1));
        break;
      case "n": {
        // Cycle namespace
        const idx = namespaces.indexOf(namespace);
        setNamespace(namespaces[(idx + 1) % namespaces.length] || "default");
        setCursor(0);
        setScrollOffset(0);
        break;
      }
      case "r":
        refetch();
        break;
      case "g":
        updateCursor(0);
        break;
      case "G":
        updateCursor(Math.max(0, records.length - 1));
        break;
    }
  });

  if (isLoading && !records) {
    return (
      <box padding={1}>
        <text fg="#888888">Loading blackboard...</text>
      </box>
    );
  }

  if (error) {
    return (
      <box padding={1}>
        <text fg="#ff0000">Error loading blackboard: {error.message}</text>
        <text fg="#666666">Press r to retry</text>
      </box>
    );
  }

  return (
    <box flexDirection="row" width="100%" height="100%">
      {/* Record list */}
      <box width="50%" flexDirection="column">
        <box height={2} paddingLeft={1}>
          <text fg="#aa77ff">
            <b>Blackboard</b>
            <span fg="#666666"> ({records?.length || 0} records)</span>
          </text>
          <text fg="#666666">
            {"  "}Namespace: <span fg="#00ffff">{namespace}</span>
            {"  "}(press 'n' to cycle)
          </text>
        </box>
        <box height={1} paddingLeft={1}>
          <text fg="#666666">
            {"  "}NS{"        "}TOPIC
          </text>
        </box>
        <box flexGrow={1} flexDirection="column" overflow="hidden" paddingLeft={1}>
          {!records || records.length === 0 ? (
            <box padding={1}>
              <text fg="#888888">No records in namespace '{namespace}'</text>
            </box>
          ) : (
            records.slice(scrollOffset, scrollOffset + LIST_HEIGHT).map((rec, i) => (
              <RecordRow
                key={rec.id}
                record={rec}
                selected={i + scrollOffset === cursor}
              />
            ))
          )}
        </box>
      </box>

      {/* Detail pane */}
      <box
        flexGrow={1}
        borderStyle="single"
        borderColor="#444444"
        border={["left"]}
      >
        <RecordDetail record={selectedRecord} />
      </box>
    </box>
  );
}
