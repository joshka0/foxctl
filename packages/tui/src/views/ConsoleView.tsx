// ConsoleView - Interactive agent console for sending prompts and viewing responses
// Supports SSE streaming, 1-5 rating feedback, and keyboard shortcuts

import { useState, useCallback, useMemo, useEffect } from "react";
import { useKeyboard } from "@opentui/react";
import {
  useConsoles,
  useConsoleEvents,
  useConsoleMutations,
  type ConsoleHistoryEvent,
} from "../hooks/useData";

interface ConsoleViewProps {
  height?: number;
  onExit?: () => void;
}

// Format timestamp for display
function formatTime(date: Date): string {
  return date.toLocaleTimeString("en-US", {
    hour: "2-digit",
    minute: "2-digit",
    second: "2-digit",
    hour12: false,
  });
}

// Get color based on event type/kind
function getEventColor(event: ConsoleHistoryEvent): string {
  if (event.type === "user") return "#00ffff";
  if (event.type === "agent") {
    if (event.status === "error") return "#ff0000";
    if (event.status === "cancelled") return "#ffff00";
    return "#00ff00";
  }
  // Event types
  switch (event.kind) {
    case "thought":
      return "#ff00ff";
    case "tool_call":
      return "#0088ff";
    case "tool_result":
      return "#ffaa00";
    case "progress":
      return "#888888";
    default:
      return "#ffffff";
  }
}

// Get label for event type
function getEventLabel(event: ConsoleHistoryEvent): string {
  if (event.type === "user") return "USER";
  if (event.type === "agent") return "AGENT";
  switch (event.kind) {
    case "thought":
      return "THINK";
    case "tool_call":
      return event.toolName ? `CALL:${event.toolName}` : "CALL";
    case "tool_result":
      return "RESULT";
    case "progress":
      return "...";
    default:
      return "EVENT";
  }
}

export function ConsoleView({ height = 24, onExit }: ConsoleViewProps) {
  // State
  const [selectedConsoleId, setSelectedConsoleId] = useState<string | undefined>(undefined);
  const [inputValue, setInputValue] = useState("");
  const [isProcessing, setIsProcessing] = useState(false);
  const [currentAskId, setCurrentAskId] = useState<string | null>(null);
  const [lastRating, setLastRating] = useState<number | null>(null);
  const [showConsoleList, setShowConsoleList] = useState(true);
  const [cursor, setCursor] = useState(0);
  const [historyScrollOffset, setHistoryScrollOffset] = useState(0);
  const [inputMode, setInputMode] = useState(false);
  const [feedbackMode, setFeedbackMode] = useState(false);
  const [feedbackText, setFeedbackText] = useState("");
  const [showRatingPrompt, setShowRatingPrompt] = useState(false);

  // Data hooks
  const { data: consolesData, isLoading: consolesLoading, refetch: refetchConsoles } = useConsoles({ limit: 50 });
  const { connected, error: sseError, history, addUserMessage, clearHistory } = useConsoleEvents(
    selectedConsoleId,
    (event) => {
      // Handle processing state based on events
      if (event.type === "console.reply") {
        setIsProcessing(false);
        setLastRating(null); // Reset rating for new response
        setShowRatingPrompt(true); // Show rating prompt after response
      }
    }
  );
  const mutations = useConsoleMutations();

  const consoles = consolesData?.consoles || [];

  // Calculate visible area
  const historyHeight = Math.max(1, height - 8);

  // Handle creating a new console session
  const handleCreateConsole = useCallback(async () => {
    try {
      const session = await mutations.createSession("tui-user");
      setSelectedConsoleId(session.id);
      setShowConsoleList(false);
      clearHistory();
      refetchConsoles();
    } catch (err) {
      // Silently fail - user can try again
    }
  }, [mutations, clearHistory, refetchConsoles]);

  // Handle sending a message
  const handleSend = useCallback(async () => {
    if (!selectedConsoleId || !inputValue.trim() || isProcessing) return;

    const prompt = inputValue.trim();
    setInputValue("");
    setInputMode(false);
    setIsProcessing(true);

    try {
      const { askId } = await mutations.sendMessage(selectedConsoleId, prompt);
      setCurrentAskId(askId);
      addUserMessage(prompt, askId);
    } catch (err) {
      setIsProcessing(false);
    }
  }, [selectedConsoleId, inputValue, isProcessing, mutations, addUserMessage]);

  // Handle canceling current request
  const handleCancel = useCallback(async () => {
    if (!selectedConsoleId || !isProcessing) return;

    try {
      await mutations.cancel(selectedConsoleId, currentAskId || undefined);
      setIsProcessing(false);
    } catch (err) {
      // Silently fail
    }
  }, [selectedConsoleId, isProcessing, currentAskId, mutations]);

  // Handle rating feedback
  const handleRating = useCallback(
    async (rating: number) => {
      if (!selectedConsoleId || !currentAskId) return;

      try {
        await mutations.submitFeedback(selectedConsoleId, rating, undefined, currentAskId);
        setLastRating(rating);
        setShowRatingPrompt(false);
      } catch (err) {
        // Silently fail
      }
    },
    [selectedConsoleId, currentAskId, mutations]
  );

  // Handle text feedback submission
  const handleTextFeedback = useCallback(async () => {
    if (!selectedConsoleId || !currentAskId || !feedbackText.trim()) return;

    try {
      await mutations.submitFeedback(selectedConsoleId, lastRating ?? 3, undefined, currentAskId, feedbackText.trim());
      setFeedbackMode(false);
      setFeedbackText("");
      setShowRatingPrompt(false);
    } catch (err) {
      // Silently fail
    }
  }, [selectedConsoleId, currentAskId, feedbackText, lastRating, mutations]);

  // Auto-scroll history to bottom on new messages
  useEffect(() => {
    if (history.length > historyHeight) {
      setHistoryScrollOffset(history.length - historyHeight);
    }
  }, [history.length, historyHeight]);

  // Keyboard navigation
  useKeyboard((e) => {
    if (showConsoleList) {
      // Console list navigation
      switch (e.name) {
        case "escape":
          // Exit console view back to main menu
          if (onExit) {
            onExit();
          }
          break;
        case "up":
        case "k":
          setCursor((prev) => Math.max(0, prev - 1));
          break;
        case "down":
        case "j":
          setCursor((prev) => Math.min(Math.max(0, consoles.length - 1), prev + 1));
          break;
        case "return":
          if (consoles[cursor]) {
            setSelectedConsoleId(consoles[cursor].id);
            setShowConsoleList(false);
            clearHistory();
          }
          break;
        case "n":
          handleCreateConsole();
          break;
        case "r":
          refetchConsoles();
          break;
      }
    } else if (feedbackMode) {
      // Feedback text input mode
      if (e.name === "return") {
        if (feedbackText.length > 0) {
          handleTextFeedback();
        }
        return;
      }
      if (e.name === "backspace") {
        setFeedbackText((v) => v.slice(0, -1));
        return;
      }
      if (e.name === "escape") {
        setFeedbackMode(false);
        setFeedbackText("");
        return;
      }
      // Handle printable characters
      if (e.raw && e.raw.length === 1 && !e.ctrl && !e.meta) {
        setFeedbackText((v) => v + e.raw);
        return;
      }
      if (e.name === "space") {
        setFeedbackText((v) => v + " ");
        return;
      }
    } else if (inputMode) {
      // Input mode - build message string
      if (e.name === "return") {
        if (inputValue.length > 0) {
          handleSend();
        }
        return;
      }
      if (e.name === "backspace") {
        setInputValue((v) => v.slice(0, -1));
        return;
      }
      if (e.name === "escape") {
        setInputMode(false);
        setInputValue("");
        return;
      }
      // Handle printable characters
      if (e.raw && e.raw.length === 1 && !e.ctrl && !e.meta) {
        setInputValue((v) => v + e.raw);
        return;
      }
      if (e.name === "space") {
        setInputValue((v) => v + " ");
        return;
      }
    } else {
      // Console session view - navigation mode
      switch (e.name) {
        case "escape":
          if (showRatingPrompt) {
            setShowRatingPrompt(false);
          } else if (isProcessing) {
            handleCancel();
          } else {
            setShowConsoleList(true);
            setSelectedConsoleId(undefined);
          }
          break;
        case "up":
        case "k":
          setHistoryScrollOffset((prev) => Math.max(0, prev - 1));
          break;
        case "down":
        case "j":
          setHistoryScrollOffset((prev) => Math.min(Math.max(0, history.length - historyHeight), prev + 1));
          break;
        case "i":
        case "/":
          if (!isProcessing && !feedbackMode) {
            setInputMode(true);
          }
          break;
        case "f":
          // Open text feedback modal
          if (!isProcessing && currentAskId && !inputMode) {
            setFeedbackMode(true);
          }
          break;
        case "1":
        case "2":
        case "3":
        case "4":
        case "5":
          if (!isProcessing && currentAskId) {
            handleRating(parseInt(e.name, 10));
          }
          break;
        case "c":
          if (isProcessing) {
            handleCancel();
          }
          break;
        case "h":
          clearHistory();
          break;
      }
    }
  });

  // Visible history for session view
  const visibleHistory = useMemo(() => {
    return history.slice(historyScrollOffset, historyScrollOffset + historyHeight);
  }, [history, historyScrollOffset, historyHeight]);

  // Render console list view
  if (showConsoleList) {
    return (
      <box flexDirection="column" width="100%" height="100%">
        {/* Header */}
        <box height={2} paddingLeft={1} paddingTop={1}>
          <text>
            <b fg="#0088ff">CONSOLE SESSIONS</b>
            {"  "}
            <span fg="#888888">
              {consoles.length} session{consoles.length !== 1 ? "s" : ""}
            </span>
            {consolesLoading && <span fg="#ffff00"> Loading...</span>}
          </text>
        </box>

        {/* Console list */}
        <box flexDirection="column" flexGrow={1} overflow="hidden" paddingLeft={1}>
          {consoles.length === 0 ? (
            <box paddingTop={1}>
              <text fg="#888888">No console sessions. Press [n] to create one.</text>
            </box>
          ) : (
            consoles.map((console, idx) => {
              const isSelected = idx === cursor;
              const bg = isSelected ? "#444444" : undefined;
              return (
                <box key={console.id} height={1} backgroundColor={bg}>
                  <text fg={isSelected ? "#ffffff" : "#00ffff"}>
                    {isSelected ? "> " : "  "}
                    {console.id.slice(0, 8)}
                  </text>
                  <text fg="#888888">
                    {"  "}actor: {console.actor_id}
                    {"  "}
                    {new Date(console.created_at).toLocaleString()}
                  </text>
                </box>
              );
            })
          )}
        </box>

        {/* Footer */}
        <box height={1} paddingLeft={1} paddingBottom={1}>
          <text fg="#666666">
            [j/k]navigate [Enter]select [n]new [r]refresh [Esc]back
          </text>
        </box>
      </box>
    );
  }

  // Render console session view
  const selectedConsole = consoles.find((c) => c.id === selectedConsoleId);
  const ratingStars = lastRating ? "★".repeat(lastRating) + "☆".repeat(5 - lastRating) : "";

  // Render feedback modal overlay
  if (feedbackMode) {
    return (
      <box flexDirection="column" width="100%" height="100%">
        <box height={3} paddingLeft={1} paddingTop={1}>
          <text>
            <b fg="#ff00ff">FEEDBACK</b>
            {"  "}
            <span fg="#888888">for response {currentAskId?.slice(0, 8)}</span>
            {lastRating && <span fg="#00ffff"> [rating: {ratingStars}]</span>}
          </text>
        </box>
        <box
          flexDirection="column"
          flexGrow={1}
          borderStyle="single"
          borderColor="#ff00ff"
          paddingLeft={1}
          paddingTop={1}
        >
          <box height={1}>
            <text fg="#888888">Enter your feedback (notes, suggestions, etc.):</text>
          </box>
          <box height={3} paddingTop={1}>
            <text>
              <span fg="#00ffff">&gt; </span>
              <span fg="#ffffff">{feedbackText}</span>
              <span fg="#00ff00">_</span>
            </text>
          </box>
          <box paddingTop={2}>
            <text fg="#666666">
              Tip: Press 1-5 first to rate, then f for text feedback
            </text>
          </box>
        </box>
        <box height={1} paddingLeft={1} paddingBottom={1}>
          <text fg="#666666">[Enter]submit [Esc]cancel</text>
        </box>
      </box>
    );
  }

  return (
    <box flexDirection="column" width="100%" height="100%">
      {/* Header */}
      <box height={2} paddingLeft={1} paddingTop={1}>
        <text>
          <b fg="#0088ff">CONSOLE:</b>
          {"  "}
          <span fg="#00ffff">{selectedConsole?.actor_id || "unknown"}</span>
          {"  "}
          <span fg={connected ? "#00ff00" : "#ffff00"}>
            [{connected ? "connected" : "connecting..."}]
          </span>
          {isProcessing && <span fg="#ff00ff"> [processing...]</span>}
          {lastRating && <span fg="#00ffff"> [rated: {ratingStars}]</span>}
          {sseError && <span fg="#ff0000"> [error: {sseError.message}]</span>}
        </text>
      </box>

      {/* History */}
      <box
        flexDirection="column"
        flexGrow={1}
        borderStyle="single"
        borderColor="#444444"
        overflow="hidden"
      >
        <box height={1} paddingLeft={1}>
          <text>
            <b fg="#888888">HISTORY</b>
            {"  "}
            <span fg="#666666">
              {history.length} message{history.length !== 1 ? "s" : ""}
            </span>
          </text>
        </box>
        {history.length === 0 ? (
          <box paddingLeft={1}>
            <text fg="#888888">No messages yet. Press [i] to type a prompt.</text>
          </box>
        ) : (
          <box flexDirection="column" overflow="hidden" paddingLeft={1}>
            {visibleHistory.map((event) => (
              <box key={event.id} height={1}>
                <text fg="#666666">[{formatTime(event.timestamp)}]</text>
                <text>
                  {" "}
                  <b fg={getEventColor(event)}>{getEventLabel(event)}:</b>
                  {" "}
                  <span fg="#ffffff">
                    {event.content.slice(0, 80)}{event.content.length > 80 ? "..." : ""}
                  </span>
                </text>
              </box>
            ))}
          </box>
        )}
      </box>

      {/* Rating Prompt - shown after response */}
      {showRatingPrompt && !isProcessing && currentAskId && (
        <box height={2} paddingLeft={1} borderStyle="single" borderColor="#ffaa00" backgroundColor="#332200">
          <text>
            <span fg="#ffaa00">Rate response: </span>
            <span fg="#ffffff">[1]</span><span fg="#888888">Poor </span>
            <span fg="#ffffff">[2]</span><span fg="#888888">Fair </span>
            <span fg="#ffffff">[3]</span><span fg="#888888">OK </span>
            <span fg="#ffffff">[4]</span><span fg="#888888">Good </span>
            <span fg="#ffffff">[5]</span><span fg="#888888">Great </span>
            <span fg="#666666">| [f]feedback [Esc]skip</span>
          </text>
        </box>
      )}

      {/* Input */}
      <box height={2} paddingLeft={1} borderStyle="single" borderColor={inputMode ? "#00ffff" : "#444444"}>
        <text>
          <span fg={inputMode ? "#00ffff" : "#888888"}>&gt; </span>
          <span fg={isProcessing ? "#888888" : inputMode ? "#ffffff" : "#666666"}>
            {isProcessing
              ? "Processing..."
              : inputValue || (inputMode ? "" : "(press i or / to type)")}
          </span>
          {inputMode && <span fg="#00ff00">_</span>}
        </text>
      </box>

      {/* Footer */}
      <box height={1} paddingLeft={1} paddingBottom={1}>
        <text fg="#666666">
          {inputMode
            ? "[Enter]send [Esc]cancel"
            : isProcessing
              ? "[c]cancel [Esc]back"
              : showRatingPrompt
                ? "[1-5]rate [f]feedback [Esc]skip"
                : `[i]input [1-5]rate [f]feedback [h]clear [j/k]scroll [Esc]back`}
        </text>
      </box>
    </box>
  );
}

export default ConsoleView;
