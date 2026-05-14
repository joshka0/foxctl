// Sessions View - Browse Claude Code sessions with summaries
import { useState } from "react";
import { useSessions, useSessionMessages, useSessionContextWindows } from "../hooks/useData";
import { WINDOWED_LIST_HEIGHT } from "../constants";
import type { Session, SessionMessage, ContextWindow } from "@foxctl/data";
import { useKeyboardStable } from "../hooks/useKeyboardStable";

function statusColor(status: string): string {
  switch (status) {
    case "ok":
    case "completed":
      return "#00ff00";
    case "running":
    case "active":
      return "#00ffff";
    case "error":
    case "failed":
      return "#ff0000";
    case "canceled":
      return "#ffff00";
    default:
      return "#888888";
  }
}

function formatDate(isoString: string | undefined): string {
  if (!isoString) return "";
  try {
    const date = new Date(isoString);
    return date.toLocaleDateString("en-US", {
      month: "short",
      day: "numeric",
      hour: "2-digit",
      minute: "2-digit",
    });
  } catch {
    return "";
  }
}

function formatDuration(start: string, end: string | undefined): string {
  if (!start) return "";
  try {
    const startTime = new Date(start).getTime();
    const endTime = end ? new Date(end).getTime() : Date.now();
    const diffMs = endTime - startTime;
    const mins = Math.floor(diffMs / 60000);
    if (mins < 60) return `${mins}m`;
    const hours = Math.floor(mins / 60);
    if (hours < 24) return `${hours}h ${mins % 60}m`;
    const days = Math.floor(hours / 24);
    return `${days}d ${hours % 24}h`;
  } catch {
    return "";
  }
}

function truncate(s: string | undefined, len: number): string {
  if (!s) return "";
  return s.length > len ? s.slice(0, len - 3) + "..." : s;
}

function extractBranch(path: string | undefined): string {
  if (!path) return "";
  // Extract last path component
  const parts = path.split("/");
  return parts[parts.length - 1] || "";
}

interface SessionRowProps {
  session: Session;
  selected: boolean;
}

function SessionRow({ session, selected }: SessionRowProps) {
  const bg = selected ? "#444444" : undefined;
  const cursor = selected ? "> " : "  ";

  return (
    <box height={1} backgroundColor={bg} flexDirection="row">
      <text fg="#ffffff">
        {cursor}
        <span fg={statusColor(session.status)}>{(session.status || "?").slice(0, 6).padEnd(7)}</span>
        {"  "}
        <span fg="#888888">{formatDate(session.started_at).padEnd(14)}</span>
        {"  "}
        <span fg="#666666">{formatDuration(session.started_at, session.ended_at).padEnd(8)}</span>
        {"  "}
        <span fg="#aa77ff">{String(session.user_turns || 0).padStart(3)} turns</span>
        {"  "}
        <span fg="#cccccc">{truncate(session.summary || extractBranch(session.workspace_path), 45)}</span>
      </text>
    </box>
  );
}

// Format token count nicely
function formatTokens(tokens: number): string {
  if (tokens >= 1000000) return `${(tokens / 1000000).toFixed(1)}M`;
  if (tokens >= 1000) return `${(tokens / 1000).toFixed(1)}k`;
  return String(tokens);
}

// Trigger color based on type
function triggerColor(trigger: string): string {
  switch (trigger) {
    case "auto":
      return "#00ffff";
    case "manual":
      return "#ffff00";
    default:
      return "#888888";
  }
}

interface ContextWindowsDisplayProps {
  sessionId: string;
}

function ContextWindowsDisplay({ sessionId }: ContextWindowsDisplayProps) {
  const { data, isLoading, error } = useSessionContextWindows(sessionId);

  if (isLoading) {
    return <text fg="#666666">Loading context windows...</text>;
  }

  if (error || !data) {
    return null;
  }

  const windows = data.context_windows || [];
  if (windows.length === 0) {
    return (
      <text fg="#666666">No context windows (single context)</text>
    );
  }

  return (
    <box flexDirection="column">
      <text fg="#00aaff">
        <b>Context Windows ({windows.length}):</b>
      </text>
      <box paddingLeft={1} paddingTop={1} flexDirection="column">
        {windows.map((w: ContextWindow) => (
          <box key={w.id} height={1} flexDirection="row">
            <text fg="#888888">
              <span fg="#666666">[{w.window_index}]</span>
              {" "}
              <span fg={triggerColor(w.trigger)}>{w.trigger.padEnd(6)}</span>
              {" "}
              <span fg="#aa77ff">{formatTokens(w.pre_compact_tokens)} tokens</span>
              {" "}
              <span fg="#666666">{w.message_count} msgs</span>
              {" "}
              <span fg="#444444">chunks {w.chunk_start}-{w.chunk_end}</span>
            </text>
          </box>
        ))}
      </box>
    </box>
  );
}

interface SessionDetailProps {
  session: Session | undefined;
}

function SessionDetail({ session }: SessionDetailProps) {
  if (!session) {
    return (
      <box padding={1}>
        <text fg="#666666">Select a session to view details</text>
      </box>
    );
  }

  return (
    <box flexDirection="column" padding={1}>
      <box flexDirection="row" justifyContent="space-between">
        <text fg={statusColor(session.status)}>
          <b>{(session.status || "unknown").toUpperCase()}</b>
        </text>
        <text fg="#444444">{session.id.slice(0, 16)}...</text>
      </box>
      <text> </text>
      <box flexDirection="row">
        <text>
          <b fg="#666666">Started: </b>
          <span fg="#888888">{formatDate(session.started_at)}</span>
        </text>
        {session.ended_at && (
          <>
            <text fg="#666666">{"  |  "}</text>
            <text>
              <b fg="#666666">Duration: </b>
              <span fg="#888888">{formatDuration(session.started_at, session.ended_at)}</span>
            </text>
          </>
        )}
      </box>
      <text> </text>
      <box flexDirection="row">
        <text>
          <b fg="#666666">Turns: </b>
          <span fg="#aa77ff">{session.user_turns || 0}</span>
        </text>
        <text fg="#666666">{"  |  "}</text>
        <text>
          <b fg="#666666">Messages: </b>
          <span fg="#888888">{session.message_count || 0}</span>
        </text>
        <text fg="#666666">{"  |  "}</text>
        <text>
          <b fg="#666666">Tools: </b>
          <span fg="#888888">{session.tool_invocations || 0}</span>
        </text>
      </box>
      <text> </text>
      {session.project_name && (
        <text fg="#888888">
          <b>Project: </b>
          {session.project_name}
        </text>
      )}
      {session.git_branch && (
        <text fg="#888888">
          <b>Branch: </b>
          {session.git_branch}
        </text>
      )}
      {session.workspace_path && (
        <text fg="#666666">
          <b>Workspace: </b>
          {truncate(session.workspace_path, 60)}
        </text>
      )}
      {session.agent_id && (
        <text fg="#666666">
          <b>Agent: </b>
          {session.agent_id.slice(0, 24)}
        </text>
      )}

      <text> </text>
      <ContextWindowsDisplay sessionId={session.id} />

      <text> </text>
      <text fg="#aa77ff">
        <b>Summary:</b>
      </text>
      <box paddingLeft={1} paddingTop={1}>
        <text fg="#cccccc">{session.summary || "(no summary)"}</text>
      </box>

      {session.accomplished && (
        <>
          <text> </text>
          <text fg="#00ff00">
            <b>Accomplished:</b>
          </text>
          <box paddingLeft={1} paddingTop={1}>
            <text fg="#888888">{truncate(session.accomplished, 500)}</text>
          </box>
        </>
      )}

      {session.decisions && (
        <>
          <text> </text>
          <text fg="#ffff00">
            <b>Decisions:</b>
          </text>
          <box paddingLeft={1} paddingTop={1}>
            <text fg="#888888">{truncate(session.decisions, 500)}</text>
          </box>
        </>
      )}

      {session.gotchas && (
        <>
          <text> </text>
          <text fg="#ff8800">
            <b>Gotchas:</b>
          </text>
          <box paddingLeft={1} paddingTop={1}>
            <text fg="#888888">{truncate(session.gotchas, 500)}</text>
          </box>
        </>
      )}
    </box>
  );
}

// Turn viewer component
interface SessionTurnViewerProps {
  sessionId: string;
  onClose: () => void;
}

function roleColor(role: string | undefined): string {
  switch (role) {
    case "user":
      return "#00ffff";
    case "assistant":
      return "#00ff00";
    case "system":
      return "#ffff00";
    default:
      return "#888888";
  }
}

function typeColor(type: string): string {
  switch (type) {
    case "user":
      return "#00ffff";
    case "assistant":
      return "#00ff00";
    case "summary":
      return "#ffff00";
    case "result":
      return "#aa77ff";
    default:
      return "#888888";
  }
}

function getMessagePreview(msg: SessionMessage): string {
  if (msg.summary) return msg.summary;
  if (msg.error) return `Error: ${msg.error}`;
  if (!msg.message?.content) return "(no content)";

  const content = msg.message.content;
  for (const block of content) {
    if (block.type === "text" && block.text) {
      return truncate(block.text, 120);
    }
    if (block.type === "tool_use" && block.name) {
      return `[Tool: ${block.name}]`;
    }
    if (block.type === "tool_result") {
      return "[Tool Result]";
    }
  }
  return "(unknown content)";
}

function SessionTurnViewer({ sessionId, onClose }: SessionTurnViewerProps) {
  const [cursor, setCursor] = useState(0);
  const [scrollOffset, setScrollOffset] = useState(0);
  const [expandedMessage, setExpandedMessage] = useState<number | null>(null);
  const LIST_HEIGHT = 18;

  const { data, isLoading, error, refetch } = useSessionMessages(sessionId, {
    limit: 200,
  });
  const messages = data?.messages || [];
  const selectedMessage = messages[cursor];

  const updateCursor = (newCursor: number) => {
    setCursor(newCursor);
    if (newCursor < scrollOffset) {
      setScrollOffset(newCursor);
    } else if (newCursor >= scrollOffset + LIST_HEIGHT) {
      setScrollOffset(newCursor - LIST_HEIGHT + 1);
    }
  };

  useKeyboardStable((e) => {
    switch (e.name) {
      case "escape":
      case "q":
        onClose();
        break;
      case "up":
      case "k":
        updateCursor(Math.max(0, cursor - 1));
        break;
      case "down":
      case "j":
        updateCursor(Math.min(Math.max(0, messages.length - 1), cursor + 1));
        break;
      case "return":
        // Toggle expand message
        setExpandedMessage(expandedMessage === cursor ? null : cursor);
        break;
      case "r":
        refetch();
        break;
      case "g":
        updateCursor(0);
        break;
      case "G":
        updateCursor(Math.max(0, messages.length - 1));
        break;
    }
  });

  if (isLoading && !data) {
    return (
      <box padding={1}>
        <text fg="#888888">Loading messages...</text>
      </box>
    );
  }

  if (error) {
    return (
      <box padding={1}>
        <text fg="#ff0000">Error: {error.message}</text>
        <text fg="#666666">Press q to go back, r to retry</text>
      </box>
    );
  }

  // Full screen message view when expanded
  if (expandedMessage !== null && selectedMessage) {
    const content = selectedMessage.message?.content || [];
    return (
      <box flexDirection="column" width="100%" height="100%">
        <box height={2} paddingLeft={1} borderStyle="single" borderColor="#333333" border={["bottom"]}>
          <text fg="#aa77ff">
            <b>MESSAGE #{selectedMessage.index}</b>
            <span fg="#666666"> | {selectedMessage.type} | {selectedMessage.message?.role || "?"}</span>
          </text>
          <text fg="#666666">Press Enter to collapse, q/Esc to close viewer</text>
        </box>
        <box flexGrow={1} flexDirection="column" overflow="scroll" padding={1}>
          {content.map((block, idx) => (
            <box key={idx} flexDirection="column" marginBottom={1}>
              <text fg="#666666">
                [{block.type}]
              </text>
              {block.type === "text" && block.text && (
                <text fg="#cccccc">{block.text}</text>
              )}
              {block.type === "tool_use" && (
                <>
                  <text fg="#aa77ff">Tool: {block.name}</text>
                  <text fg="#888888">{JSON.stringify(block.input, null, 2)}</text>
                </>
              )}
              {block.type === "tool_result" && (
                <text fg="#888888">{JSON.stringify(block, null, 2)}</text>
              )}
            </box>
          ))}
        </box>
      </box>
    );
  }

  return (
    <box flexDirection="column" width="100%" height="100%">
      {/* Header */}
      <box height={2} paddingLeft={1} borderStyle="single" borderColor="#333333" border={["bottom"]}>
        <text fg="#aa77ff">
          <b>TURN VIEWER</b>
          <span fg="#666666"> ({data?.total || 0} messages) | Session: {sessionId.slice(0, 16)}...</span>
        </text>
        <text fg="#666666">j/k: navigate | Enter: expand | q/Esc: close</text>
      </box>

      {/* Messages list */}
      <box flexGrow={1} flexDirection="column" overflow="hidden">
        {messages.slice(scrollOffset, scrollOffset + LIST_HEIGHT).map((msg, i) => {
          const isSelected = i + scrollOffset === cursor;
          const bg = isSelected ? "#444444" : undefined;
          const cursorChar = isSelected ? "> " : "  ";
          const role = msg.message?.role || msg.type;

          return (
            <box key={msg.index} height={1} backgroundColor={bg} flexDirection="row">
              <text fg="#ffffff">
                {cursorChar}
                <span fg="#666666">{String(msg.index).padStart(4)} </span>
                <span fg={typeColor(msg.type)}>{msg.type.slice(0, 9).padEnd(10)}</span>
                <span fg={roleColor(role)}>{(role || "?").slice(0, 9).padEnd(10)}</span>
                <span fg="#cccccc">{getMessagePreview(msg)}</span>
              </text>
            </box>
          );
        })}
      </box>

      {/* Footer with message count */}
      <box height={1} paddingLeft={1} borderStyle="single" borderColor="#333333" border={["top"]}>
        <text fg="#666666">
          {cursor + 1}/{messages.length} messages
          {data?.total && data.total > messages.length && ` (${data.total} total)`}
        </text>
      </box>
    </box>
  );
}

export function SessionsView() {
  const [cursor, setCursor] = useState(0);
  const [scrollOffset, setScrollOffset] = useState(0);
  const [showTurnViewer, setShowTurnViewer] = useState(false);

  const { data, isLoading, error, refetch } = useSessions({ limit: 100 });
  const sessions = data?.sessions || [];
  const selectedSession = sessions[cursor];

  const LIST_HEIGHT = WINDOWED_LIST_HEIGHT;

  const updateCursor = (newCursor: number) => {
    setCursor(newCursor);
    if (newCursor < scrollOffset) {
      setScrollOffset(newCursor);
    } else if (newCursor >= scrollOffset + LIST_HEIGHT) {
      setScrollOffset(newCursor - LIST_HEIGHT + 1);
    }
  };

  useKeyboardStable((e) => {
    switch (e.name) {
      case "up":
      case "k":
        updateCursor(Math.max(0, cursor - 1));
        break;
      case "down":
      case "j":
        updateCursor(Math.min(Math.max(0, sessions.length - 1), cursor + 1));
        break;
      case "return":
        if (selectedSession) {
          setShowTurnViewer(true);
        }
        break;
      case "r":
        refetch();
        break;
      case "g":
        updateCursor(0);
        break;
      case "G":
        updateCursor(Math.max(0, sessions.length - 1));
        break;
    }
  }, !showTurnViewer);

  if (isLoading && !data) {
    return (
      <box padding={1}>
        <text fg="#888888">Loading sessions...</text>
      </box>
    );
  }

  if (error) {
    return (
      <box padding={1}>
        <text fg="#ff0000">Error loading sessions: {error.message}</text>
        <text fg="#666666">Press r to retry</text>
      </box>
    );
  }

  if (sessions.length === 0) {
    return (
      <box padding={1}>
        <text fg="#888888">No sessions found</text>
        <text fg="#666666">Press r to refresh</text>
      </box>
    );
  }

  // Show turn viewer when Enter is pressed on a session
  if (showTurnViewer && selectedSession) {
    return (
      <SessionTurnViewer
        sessionId={selectedSession.id}
        onClose={() => setShowTurnViewer(false)}
      />
    );
  }

  return (
    <box flexDirection="row" width="100%" height="100%">
      {/* Sessions list */}
      <box width="55%" flexDirection="column" borderStyle="single" borderColor="#333333" border={["right"]}>
        <box height={3} paddingLeft={1} paddingTop={1} borderStyle="single" borderColor="#333333" border={["bottom"]}>
          <text fg="#aa77ff">
            <b>SESSIONS</b>
            <span fg="#666666"> ({data?.total || 0}) | Enter: view turns</span>
          </text>
          <text fg="#666666">
            STATUS   DATE            DURATION   TURNS     SUMMARY
          </text>
        </box>
        <box flexGrow={1} flexDirection="column" overflow="hidden">
          {sessions.slice(scrollOffset, scrollOffset + LIST_HEIGHT).map((session, i) => (
            <SessionRow
              key={session.id}
              session={session}
              selected={i + scrollOffset === cursor}
            />
          ))}
        </box>
      </box>

      {/* Detail pane */}
      <box flexGrow={1} borderStyle="single" borderColor="#444444" border={["left"]}>
        <SessionDetail session={selectedSession} />
      </box>
    </box>
  );
}
