// Mailbox View - Actor messages
import { useState } from "react";
import { useKeyboard } from "@opentui/react";
import { useMailbox } from "../hooks/useData";
import type { MailboxMessage } from "@agentctl/data";

const ACTORS = ["admin", "overseer", "engineer", "tester"];

function priorityColor(priority: number): string {
  if (priority <= 1) return "#ff0000";
  if (priority <= 2) return "#ffaa00";
  if (priority <= 3) return "#ffff00";
  return "#888888";
}

function statusIcon(status: string): string {
  return status === "unread" ? "*" : "o";
}

interface MessageRowProps {
  message: MailboxMessage;
  selected: boolean;
}

function MessageRow({ message, selected }: MessageRowProps) {
  const bg = selected ? "#444444" : undefined;
  const cursor = selected ? "> " : "  ";
  const icon = statusIcon(message.status);

  return (
    <box height={2} backgroundColor={bg} flexDirection="column">
      <text fg="#ffffff">
        {cursor}
        <span fg={message.status === "unread" ? "#00ff00" : "#666666"}>
          {icon}
        </span>
        {"  "}
        <span fg={priorityColor(message.priority)}>P{message.priority}</span>
        {"  "}
        <span fg="#00ffff">{message.sender.padEnd(12)}</span>
        {"  "}
        {message.subject.slice(0, 40)}
      </text>
      {message.body && (
        <text fg="#666666">
          {"     "}
          {message.body.slice(0, 50)}
          {message.body.length > 50 ? "..." : ""}
        </text>
      )}
    </box>
  );
}

interface MessageDetailProps {
  message: MailboxMessage | undefined;
}

function MessageDetail({ message }: MessageDetailProps) {
  if (!message) {
    return (
      <box padding={1}>
        <text fg="#666666">Select a message to view details</text>
      </box>
    );
  }

  return (
    <box flexDirection="column" padding={1}>
      <text fg="#aa77ff">
        <b>Message Detail</b>
      </text>
      <text> </text>
      <text>
        <b fg="#666666">ID: </b>
        <span fg="#ffffff">{message.id}</span>
      </text>
      <text>
        <b fg="#666666">From: </b>
        <span fg="#00ffff">{message.sender}</span>
      </text>
      <text>
        <b fg="#666666">Subject: </b>
        <span fg="#ffffff">{message.subject}</span>
      </text>
      <text>
        <b fg="#666666">Priority: </b>
        <span fg={priorityColor(message.priority)}>P{message.priority}</span>
      </text>
      <text>
        <b fg="#666666">Kind: </b>
        <span fg="#ffffff">{message.kind}</span>
      </text>
      <text>
        <b fg="#666666">Status: </b>
        <span fg={message.status === "unread" ? "#00ff00" : "#888888"}>
          {message.status}
        </span>
      </text>
      <text>
        <b fg="#666666">Created: </b>
        <span fg="#ffffff">{message.created_at}</span>
      </text>
      {message.body && (
        <>
          <text> </text>
          <text fg="#666666">
            <b>Body:</b>
          </text>
          <text fg="#cccccc">{message.body}</text>
        </>
      )}
    </box>
  );
}

// Full-screen message viewer
interface MessageFullViewProps {
  message: MailboxMessage;
  onClose: () => void;
}

function MessageFullView({ message, onClose }: MessageFullViewProps) {
  useKeyboard((e) => {
    switch (e.name) {
      case "escape":
      case "q":
        onClose();
        break;
    }
  });

  return (
    <box flexDirection="column" width="100%" height="100%">
      {/* Header */}
      <box height={2} paddingLeft={1} borderStyle="single" borderColor="#333333" border={["bottom"]}>
        <text fg="#aa77ff">
          <b>MESSAGE</b>
          <span fg="#666666"> | {message.id.slice(0, 24)}...</span>
        </text>
        <text fg="#666666">q/Esc: close</text>
      </box>

      {/* Message metadata */}
      <box height={7} paddingLeft={1} paddingTop={1} borderStyle="single" borderColor="#333333" border={["bottom"]}>
        <box flexDirection="row">
          <text>
            <b fg="#666666">From: </b>
            <span fg="#00ffff">{message.sender}</span>
          </text>
          <text fg="#666666">{"  |  "}</text>
          <text>
            <b fg="#666666">To: </b>
            <span fg="#888888">{message.recipient}</span>
          </text>
        </box>
        <text>
          <b fg="#666666">Subject: </b>
          <span fg="#ffffff">{message.subject}</span>
        </text>
        <box flexDirection="row">
          <text>
            <b fg="#666666">Priority: </b>
            <span fg={priorityColor(message.priority)}>P{message.priority}</span>
          </text>
          <text fg="#666666">{"  |  "}</text>
          <text>
            <b fg="#666666">Kind: </b>
            <span fg="#888888">{message.kind}</span>
          </text>
          <text fg="#666666">{"  |  "}</text>
          <text>
            <b fg="#666666">Status: </b>
            <span fg={message.status === "unread" ? "#00ff00" : "#888888"}>{message.status}</span>
          </text>
        </box>
        <text>
          <b fg="#666666">Created: </b>
          <span fg="#888888">{message.created_at}</span>
        </text>
      </box>

      {/* Body */}
      <box flexGrow={1} flexDirection="column" padding={1} overflow="scroll">
        <text fg="#ffff00">
          <b>Body:</b>
        </text>
        <box paddingTop={1}>
          {message.body ? (
            <text fg="#cccccc">{message.body}</text>
          ) : (
            <text fg="#666666">(no body)</text>
          )}
        </box>
      </box>
    </box>
  );
}

export function MailboxView() {
  const [actorIdx, setActorIdx] = useState(0);
  const [cursor, setCursor] = useState(0);
  const [scrollOffset, setScrollOffset] = useState(0);
  const [showFullView, setShowFullView] = useState(false);

  const LIST_HEIGHT = 8; // Each row is 2 lines high

  const actor = ACTORS[actorIdx];
  const { data: messages, isLoading, error, refetch } = useMailbox({
    actor,
    limit: 50,
  });

  const selectedMessage = messages?.[cursor];

  const updateCursor = (newCursor: number) => {
    if (!messages) return;
    setCursor(newCursor);
    if (newCursor < scrollOffset) {
      setScrollOffset(newCursor);
    } else if (newCursor >= scrollOffset + LIST_HEIGHT) {
      setScrollOffset(newCursor - LIST_HEIGHT + 1);
    }
  };

  useKeyboard((e) => {
    if (showFullView) return; // Full view handles its own keys
    if (!messages) return;

    switch (e.name) {
      case "up":
      case "k":
        updateCursor(Math.max(0, cursor - 1));
        break;
      case "down":
      case "j":
        updateCursor(Math.min(Math.max(0, messages.length - 1), cursor + 1));
        break;
      case "return":
        if (selectedMessage) {
          setShowFullView(true);
        }
        break;
      case "a":
        // Cycle actor
        setActorIdx((i) => (i + 1) % ACTORS.length);
        setCursor(0);
        setScrollOffset(0);
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

  if (isLoading && !messages) {
    return (
      <box padding={1}>
        <text fg="#888888">Loading mailbox...</text>
      </box>
    );
  }

  if (error) {
    return (
      <box padding={1}>
        <text fg="#ff0000">Error loading mailbox: {error.message}</text>
        <text fg="#666666">Press r to retry</text>
      </box>
    );
  }

  // Show full view when Enter is pressed
  if (showFullView && selectedMessage) {
    return (
      <MessageFullView
        message={selectedMessage}
        onClose={() => setShowFullView(false)}
      />
    );
  }

  return (
    <box flexDirection="row" width="100%" height="100%">
      {/* Message list */}
      <box width="55%" flexDirection="column">
        <box height={2} paddingLeft={1}>
          <text fg="#aa77ff">
            <b>Mailbox: {actor}</b>
            <span fg="#666666"> ({messages?.length || 0} messages) | Enter: full view</span>
          </text>
          <text fg="#666666">{"  "}Press 'a' to cycle actors</text>
        </box>
        <box flexGrow={1} flexDirection="column" overflow="hidden" paddingLeft={1}>
          {!messages || messages.length === 0 ? (
            <box padding={1}>
              <text fg="#888888">No messages for {actor}</text>
            </box>
          ) : (
            messages.slice(scrollOffset, scrollOffset + LIST_HEIGHT).map((msg, i) => (
              <MessageRow
                key={msg.id}
                message={msg}
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
        <MessageDetail message={selectedMessage} />
      </box>
    </box>
  );
}
