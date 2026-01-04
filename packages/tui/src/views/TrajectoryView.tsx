// Trajectory View - Browse trajectories and their event streams
// Phase 4: DSPy Training Loop - inline rating (1-5 keys) and weight visualization
import { useState } from "react";
import { useKeyboard } from "@opentui/react";
import {
  useTrajectories,
  useTrajectoryEvents,
  useTrajectoryFeedback,
  useSubmitTrajectoryFeedback,
  useScorerWeights,
} from "../hooks/useData";
import { WINDOWED_LIST_HEIGHT } from "../constants";
import type { Trajectory, TrajectoryEvent, ScorerWeights } from "@agentctl/data";

function statusColor(status: string): string {
  switch (status) {
    case "ok":
      return "#00ff00";
    case "running":
      return "#00ffff";
    case "partial":
      return "#ffff00";
    case "error":
      return "#ff0000";
    default:
      return "#888888";
  }
}

function eventKindColor(kind: string): string {
  switch (kind) {
    case "user_request":
      return "#00ffff";
    case "tool_result":
      return "#00ff00";
    case "hook_call":
      return "#ffff00";
    case "hook_result":
      return "#ff8800";
    case "task_transition":
      return "#aa77ff";
    default:
      return "#888888";
  }
}

function eventKindLabel(kind: string): string {
  switch (kind) {
    case "user_request":
      return "USR_REQ";
    case "tool_result":
      return "TOOL";
    case "hook_call":
      return "HOOK";
    case "hook_result":
      return "HOOK_R";
    case "task_transition":
      return "TASK";
    default:
      return kind.slice(0, 8);
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

function formatTime(isoString: string | undefined): string {
  if (!isoString) return "";
  try {
    const date = new Date(isoString);
    return date.toLocaleTimeString("en-US", {
      hour: "2-digit",
      minute: "2-digit",
      second: "2-digit",
    });
  } catch {
    return "";
  }
}

function truncate(s: string | undefined, len: number): string {
  if (!s) return "";
  return s.length > len ? s.slice(0, len - 3) + "..." : s;
}

// Rating display helper
function renderStars(rating: number | undefined): string {
  if (rating === undefined) return "-----";
  const filled = Math.min(5, Math.max(0, rating));
  return "*".repeat(filled) + "-".repeat(5 - filled);
}

function ratingColor(rating: number | undefined): string {
  if (rating === undefined) return "#666666";
  if (rating >= 4) return "#00ff00";
  if (rating >= 3) return "#ffff00";
  return "#ff8800";
}

// Weight bar visualization
function renderWeightBar(value: number, maxWidth: number = 20): string {
  const filled = Math.round(value * maxWidth);
  const bar = "=".repeat(filled) + " ".repeat(maxWidth - filled);
  return `[${bar}]`;
}

// Weights panel component
interface WeightsPanelProps {
  weights: ScorerWeights | undefined;
  isLoading: boolean;
}

function WeightsPanel({ weights, isLoading }: WeightsPanelProps) {
  if (isLoading) {
    return (
      <box padding={1}>
        <text fg="#666666">Loading weights...</text>
      </box>
    );
  }

  if (!weights) {
    return (
      <box padding={1}>
        <text fg="#666666">No weights data</text>
      </box>
    );
  }

  const weightItems = [
    { name: "Critical Path", value: weights.critical_path, color: "#ff8800" },
    { name: "Page Rank", value: weights.page_rank, color: "#00ffff" },
    { name: "Admin Mail", value: weights.admin_mail, color: "#00ff00" },
    { name: "Overseer Mail", value: weights.overseer_mail, color: "#aa77ff" },
    { name: "Recency", value: weights.recency, color: "#ffff00" },
  ];

  return (
    <box flexDirection="column" paddingLeft={1}>
      <text fg="#ffff00">
        <b>SCORER WEIGHTS</b>
        <span fg="#666666"> v{weights.version}</span>
      </text>
      <text> </text>
      {weightItems.map((item) => (
        <box key={item.name} height={1} flexDirection="row">
          <text fg="#888888">{item.name.padEnd(14)}</text>
          <text fg={item.color}>{renderWeightBar(item.value, 15)}</text>
          <text fg="#666666"> {(item.value * 100).toFixed(0)}%</text>
        </box>
      ))}
      {weights.last_updated && (
        <>
          <text> </text>
          <text fg="#666666">Updated: {formatDate(weights.last_updated)}</text>
        </>
      )}
    </box>
  );
}

interface TrajectoryRowProps {
  trajectory: Trajectory;
  selected: boolean;
}

function TrajectoryRow({ trajectory, selected }: TrajectoryRowProps) {
  const bg = selected ? "#444444" : undefined;
  const cursor = selected ? "> " : "  ";

  return (
    <box height={1} backgroundColor={bg} flexDirection="row">
      <text fg="#ffffff">
        {cursor}
        <span fg={statusColor(trajectory.status)}>{(trajectory.status || "?").slice(0, 8).padEnd(9)}</span>
        {"  "}
        <span fg="#aa77ff">{(trajectory.agent_role || "-").slice(0, 12).padEnd(13)}</span>
        {"  "}
        <span fg="#888888">{formatDate(trajectory.created_at).padEnd(14)}</span>
        {"  "}
        <span fg="#666666">{truncate(trajectory.summary || trajectory.id.slice(0, 16), 25)}</span>
      </text>
    </box>
  );
}

interface EventRowProps {
  event: TrajectoryEvent;
  selected: boolean;
}

function EventRow({ event, selected }: EventRowProps) {
  const bg = selected ? "#333333" : undefined;

  // Parse command or data for display
  let preview = event.command || "";
  if (!preview && event.data_inline_json) {
    try {
      const data = JSON.parse(event.data_inline_json);
      preview = data.summary || data.message || data.text || JSON.stringify(data).slice(0, 60);
    } catch {
      preview = event.data_inline_json.slice(0, 60);
    }
  }
  if (!preview && event.status) {
    preview = `status: ${event.status}`;
  }

  return (
    <box height={1} backgroundColor={bg} flexDirection="row">
      <text fg="#ffffff">
        <span fg="#666666">{formatTime(event.ts).padEnd(12)}</span>
        {"  "}
        <span fg={eventKindColor(event.kind)}>{eventKindLabel(event.kind).padEnd(8)}</span>
        {"  "}
        <span fg="#888888">{truncate(event.actor, 12).padEnd(13)}</span>
        {"  "}
        <span fg="#cccccc">{truncate(preview, 40)}</span>
      </text>
    </box>
  );
}

interface TrajectoryDetailProps {
  trajectory: Trajectory | undefined;
  events: TrajectoryEvent[] | undefined;
  eventsLoading: boolean;
  eventCursor: number;
  eventScrollOffset: number;
}

function TrajectoryDetail({
  trajectory,
  events,
  eventsLoading,
  eventCursor,
  eventScrollOffset,
}: TrajectoryDetailProps) {
  if (!trajectory) {
    return (
      <box padding={1}>
        <text fg="#666666">Select a trajectory to view details</text>
      </box>
    );
  }

  const EVENT_LIST_HEIGHT = 12;

  return (
    <box flexDirection="column" padding={1}>
      <box flexDirection="row" justifyContent="space-between">
        <text fg={statusColor(trajectory.status)}>
          <b>{(trajectory.status || "unknown").toUpperCase()}</b>
        </text>
        <text fg="#444444">{trajectory.id.slice(0, 16)}...</text>
      </box>
      <text> </text>
      <box flexDirection="row">
        {trajectory.agent_role && (
          <text>
            <b fg="#666666">Role: </b>
            <span fg="#aa77ff">{trajectory.agent_role}</span>
          </text>
        )}
        {trajectory.agent_role && <text fg="#666666">{"  |  "}</text>}
        <text>
          <b fg="#666666">Created: </b>
          <span fg="#888888">{formatDate(trajectory.created_at)}</span>
        </text>
      </box>
      {trajectory.trace_id && (
        <text fg="#666666">
          <b>Trace: </b>
          {trajectory.trace_id.slice(0, 24)}...
        </text>
      )}
      {trajectory.job_id && (
        <text fg="#666666">
          <b>Job: </b>
          {trajectory.job_id.slice(0, 24)}...
        </text>
      )}
      {trajectory.summary && (
        <>
          <text> </text>
          <text fg="#aa77ff">
            <b>Summary:</b>
          </text>
          <box paddingLeft={1}>
            <text fg="#cccccc">{truncate(trajectory.summary, 80)}</text>
          </box>
        </>
      )}
      <text> </text>
      <box flexDirection="row" justifyContent="space-between">
        <text fg="#ffff00">
          <b>Events:</b>
        </text>
        <text fg="#666666">
          {events?.length || 0} total | h/l: navigate
        </text>
      </box>
      <box flexGrow={1} flexDirection="column" paddingTop={1} overflow="hidden">
        {eventsLoading ? (
          <text fg="#666666">Loading events...</text>
        ) : events && events.length > 0 ? (
          <>
            <box height={1} paddingBottom={1}>
              <text fg="#555555">
                TIME          KIND       ACTOR          PREVIEW
              </text>
            </box>
            {events.slice(eventScrollOffset, eventScrollOffset + EVENT_LIST_HEIGHT).map((event, i) => (
              <EventRow key={event.id} event={event} selected={i + eventScrollOffset === eventCursor} />
            ))}
          </>
        ) : (
          <text fg="#666666">No events recorded</text>
        )}
      </box>
    </box>
  );
}

// Full-screen trajectory event viewer
interface TrajectoryFullViewProps {
  trajectory: Trajectory;
  onClose: () => void;
}

function TrajectoryFullView({ trajectory, onClose }: TrajectoryFullViewProps) {
  const [eventCursor, setEventCursor] = useState(0);
  const [eventScrollOffset, setEventScrollOffset] = useState(0);
  const [showEventDetail, setShowEventDetail] = useState(false);
  const EVENT_LIST_HEIGHT = 18;

  const { data: events, isLoading, refetch } = useTrajectoryEvents(trajectory.id, {
    limit: 200,
  });

  const selectedEvent = events?.[eventCursor];

  const updateEventCursor = (newCursor: number) => {
    const maxCursor = Math.max(0, (events?.length || 0) - 1);
    const clampedCursor = Math.min(Math.max(0, newCursor), maxCursor);
    setEventCursor(clampedCursor);
    if (clampedCursor < eventScrollOffset) {
      setEventScrollOffset(clampedCursor);
    } else if (clampedCursor >= eventScrollOffset + EVENT_LIST_HEIGHT) {
      setEventScrollOffset(clampedCursor - EVENT_LIST_HEIGHT + 1);
    }
  };

  useKeyboard((e) => {
    if (showEventDetail) {
      if (e.name === "escape" || e.name === "q") {
        setShowEventDetail(false);
      }
      return;
    }
    switch (e.name) {
      case "escape":
      case "q":
        onClose();
        break;
      case "up":
      case "k":
        updateEventCursor(eventCursor - 1);
        break;
      case "down":
      case "j":
        updateEventCursor(eventCursor + 1);
        break;
      case "return":
        if (selectedEvent) {
          setShowEventDetail(true);
        }
        break;
      case "r":
        refetch();
        break;
      case "g":
        updateEventCursor(0);
        break;
      case "G":
        updateEventCursor((events?.length || 1) - 1);
        break;
    }
  });

  // Show full event detail
  if (showEventDetail && selectedEvent) {
    let eventData = "";
    if (selectedEvent.data_inline_json) {
      try {
        eventData = JSON.stringify(JSON.parse(selectedEvent.data_inline_json), null, 2);
      } catch {
        eventData = selectedEvent.data_inline_json;
      }
    }

    return (
      <box flexDirection="column" width="100%" height="100%">
        <box height={2} paddingLeft={1} borderStyle="single" borderColor="#333333" border={["bottom"]}>
          <text fg="#ffff00">
            <b>EVENT #{eventCursor}</b>
            <span fg="#666666"> | {selectedEvent.kind} | {formatTime(selectedEvent.ts)}</span>
          </text>
          <text fg="#666666">q/Esc: back to event list</text>
        </box>
        <box flexGrow={1} padding={1} overflow="scroll">
          <text>
            <b fg="#666666">Actor: </b>
            <span fg="#00ffff">{selectedEvent.actor || "-"}</span>
          </text>
          <text>
            <b fg="#666666">Kind: </b>
            <span fg={eventKindColor(selectedEvent.kind)}>{selectedEvent.kind}</span>
          </text>
          {selectedEvent.command && (
            <text>
              <b fg="#666666">Command: </b>
              <span fg="#ffffff">{selectedEvent.command}</span>
            </text>
          )}
          {selectedEvent.status && (
            <text>
              <b fg="#666666">Status: </b>
              <span fg="#888888">{selectedEvent.status}</span>
            </text>
          )}
          <text> </text>
          {eventData && (
            <>
              <text fg="#aa77ff"><b>Data:</b></text>
              <text fg="#888888">{eventData}</text>
            </>
          )}
        </box>
      </box>
    );
  }

  return (
    <box flexDirection="column" width="100%" height="100%">
      {/* Header */}
      <box height={2} paddingLeft={1} borderStyle="single" borderColor="#333333" border={["bottom"]}>
        <text fg="#ffff00">
          <b>TRAJECTORY</b>
          <span fg="#666666"> | {trajectory.id.slice(0, 24)}...</span>
        </text>
        <text fg="#666666">j/k: navigate | Enter: event detail | q/Esc: close</text>
      </box>

      {/* Trajectory info */}
      <box height={5} paddingLeft={1} paddingTop={1} borderStyle="single" borderColor="#333333" border={["bottom"]}>
        <box flexDirection="row">
          <text fg={statusColor(trajectory.status)}>
            <b>{(trajectory.status || "unknown").toUpperCase()}</b>
          </text>
          {trajectory.agent_role && (
            <>
              <text fg="#666666">{"  |  "}</text>
              <text>
                <b fg="#666666">Role: </b>
                <span fg="#aa77ff">{trajectory.agent_role}</span>
              </text>
            </>
          )}
        </box>
        <text>
          <b fg="#666666">Created: </b>
          <span fg="#888888">{formatDate(trajectory.created_at)}</span>
        </text>
        {trajectory.summary && (
          <text>
            <b fg="#666666">Summary: </b>
            <span fg="#cccccc">{truncate(trajectory.summary, 70)}</span>
          </text>
        )}
      </box>

      {/* Events list */}
      <box flexGrow={1} flexDirection="column" paddingLeft={1}>
        <box height={2} paddingTop={1}>
          <text fg="#aa77ff">
            <b>EVENTS</b>
            <span fg="#666666"> ({events?.length || 0})</span>
          </text>
        </box>
        {isLoading ? (
          <text fg="#666666">Loading events...</text>
        ) : events && events.length > 0 ? (
          <box flexDirection="column" overflow="hidden">
            <box height={1}>
              <text fg="#555555">
                TIME          KIND       ACTOR          PREVIEW
              </text>
            </box>
            {events.slice(eventScrollOffset, eventScrollOffset + EVENT_LIST_HEIGHT).map((event, i) => (
              <EventRow key={event.id} event={event} selected={i + eventScrollOffset === eventCursor} />
            ))}
          </box>
        ) : (
          <text fg="#666666">No events recorded</text>
        )}
      </box>

      {/* Footer */}
      <box height={1} paddingLeft={1} borderStyle="single" borderColor="#333333" border={["top"]}>
        <text fg="#666666">
          {eventCursor + 1}/{events?.length || 0} events
        </text>
      </box>
    </box>
  );
}

export function TrajectoryView() {
  const [cursor, setCursor] = useState(0);
  const [scrollOffset, setScrollOffset] = useState(0);
  const [statusFilter, setStatusFilter] = useState<string>("");
  const [eventCursor, setEventCursor] = useState(0);
  const [eventScrollOffset, setEventScrollOffset] = useState(0);
  const [showFullView, setShowFullView] = useState(false);
  const [showWeights, setShowWeights] = useState(false);
  const [ratingStatus, setRatingStatus] = useState<string | null>(null);

  const statusFilters = ["", "ok", "running", "partial", "error"];
  const [statusIndex, setStatusIndex] = useState(0);

  const { data, isLoading, error, refetch } = useTrajectories({
    status: statusFilter || undefined,
    limit: 50,
  });

  const trajectories = data?.trajectories || [];
  const selectedTrajectory = trajectories[cursor];

  const { data: events, isLoading: eventsLoading } = useTrajectoryEvents(
    selectedTrajectory?.id,
    { limit: 100 }
  );

  // Feedback and weights hooks (Phase 4: DSPy Training Loop)
  const { data: feedback, refetch: refetchFeedback } = useTrajectoryFeedback(
    selectedTrajectory?.id
  );
  const { submit: submitFeedback, isLoading: submittingFeedback } =
    useSubmitTrajectoryFeedback();
  const { data: weightsData, isLoading: weightsLoading } = useScorerWeights();

  // Handle rating submission
  const handleRating = async (rating: number) => {
    if (!selectedTrajectory?.id || submittingFeedback) return;
    try {
      setRatingStatus(`Rating ${rating}...`);
      await submitFeedback(selectedTrajectory.id, rating);
      setRatingStatus(`Rated ${rating}/5`);
      refetchFeedback();
      setTimeout(() => setRatingStatus(null), 2000);
    } catch (err) {
      setRatingStatus("Rating failed");
      setTimeout(() => setRatingStatus(null), 2000);
    }
  };

  const LIST_HEIGHT = WINDOWED_LIST_HEIGHT;
  const EVENT_LIST_HEIGHT = 12;

  const updateCursor = (newCursor: number) => {
    setCursor(newCursor);
    if (newCursor < scrollOffset) {
      setScrollOffset(newCursor);
    } else if (newCursor >= scrollOffset + LIST_HEIGHT) {
      setScrollOffset(newCursor - LIST_HEIGHT + 1);
    }
    // Reset event cursor when switching trajectories
    setEventCursor(0);
    setEventScrollOffset(0);
  };

  const updateEventCursor = (newCursor: number) => {
    const maxCursor = Math.max(0, (events?.length || 0) - 1);
    const clampedCursor = Math.min(Math.max(0, newCursor), maxCursor);
    setEventCursor(clampedCursor);
    if (clampedCursor < eventScrollOffset) {
      setEventScrollOffset(clampedCursor);
    } else if (clampedCursor >= eventScrollOffset + EVENT_LIST_HEIGHT) {
      setEventScrollOffset(clampedCursor - EVENT_LIST_HEIGHT + 1);
    }
  };

  useKeyboard((e) => {
    if (showFullView) return; // Full view handles its own keys
    switch (e.name) {
      case "up":
      case "k":
        updateCursor(Math.max(0, cursor - 1));
        break;
      case "down":
      case "j":
        updateCursor(Math.min(Math.max(0, trajectories.length - 1), cursor + 1));
        break;
      case "return":
        if (selectedTrajectory) {
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
        updateCursor(Math.max(0, trajectories.length - 1));
        break;
      case "f":
      case "tab": {
        // Cycle through status filters
        const nextIdx = (statusIndex + 1) % statusFilters.length;
        setStatusIndex(nextIdx);
        setStatusFilter(statusFilters[nextIdx]);
        setCursor(0);
        setScrollOffset(0);
        break;
      }
      case "F": {
        // Cycle backwards
        const prevIdx = (statusIndex - 1 + statusFilters.length) % statusFilters.length;
        setStatusIndex(prevIdx);
        setStatusFilter(statusFilters[prevIdx]);
        setCursor(0);
        setScrollOffset(0);
        break;
      }
      case "h":
        // Navigate events up
        updateEventCursor(eventCursor - 1);
        break;
      case "l":
        // Navigate events down
        updateEventCursor(eventCursor + 1);
        break;
      case "w":
        // Toggle weights panel
        setShowWeights(!showWeights);
        break;
      case "1":
      case "2":
      case "3":
      case "4":
      case "5":
        // Quick rating (1-5)
        if (selectedTrajectory) {
          handleRating(parseInt(e.name));
        }
        break;
    }
  });

  if (isLoading && !data) {
    return (
      <box padding={1}>
        <text fg="#888888">Loading trajectories...</text>
      </box>
    );
  }

  if (error) {
    return (
      <box padding={1}>
        <text fg="#ff0000">Error loading trajectories: {error.message}</text>
        <text fg="#666666">Press r to retry</text>
      </box>
    );
  }

  if (trajectories.length === 0) {
    return (
      <box padding={1} flexDirection="column">
        <box height={1} flexDirection="row" paddingLeft={1}>
          <text fg="#666666">Filter: </text>
          <text fg={!statusFilter ? "#00ff00" : "#666666"}>
            {!statusFilter ? <b>[All]</b> : "[All]"}
          </text>
          {statusFilters.slice(1).map((s) => (
            <text key={s} fg={statusFilter === s ? "#00ff00" : "#666666"}>
              {" "}
              {statusFilter === s ? <b>[{s}]</b> : `[${s}]`}
            </text>
          ))}
        </box>
        <text fg="#888888">No trajectories found{statusFilter ? ` with status: ${statusFilter}` : ""}</text>
        <text fg="#666666">Press f to cycle filters, r to refresh</text>
      </box>
    );
  }

  // Show full view when Enter is pressed
  if (showFullView && selectedTrajectory) {
    return (
      <TrajectoryFullView
        trajectory={selectedTrajectory}
        onClose={() => setShowFullView(false)}
      />
    );
  }

  return (
    <box flexDirection="column" width="100%" height="100%">
      {/* Main content */}
      <box flexDirection="row" flexGrow={1}>
        {/* Trajectories list */}
        <box width={showWeights ? "35%" : "45%"} flexDirection="column" borderStyle="single" borderColor="#333333" border={["right"]}>
          <box height={4} paddingLeft={1} paddingTop={1} borderStyle="single" borderColor="#333333" border={["bottom"]}>
            <text fg="#ffff00">
              <b>TRAJECTORIES</b>
              <span fg="#666666"> ({data?.total || 0})</span>
            </text>
            <box height={1} flexDirection="row">
              <text fg="#666666">Filter: </text>
              <text fg={!statusFilter ? "#00ff00" : "#666666"}>
                {!statusFilter ? <b>[All]</b> : "[All]"}
              </text>
              {statusFilters.slice(1).map((s) => (
                <text key={s} fg={statusFilter === s ? "#00ff00" : "#666666"}>
                  {" "}
                  {statusFilter === s ? <b>[{s}]</b> : `[${s}]`}
                </text>
              ))}
            </box>
            <text fg="#666666">
              STATUS     ROLE           CREATED
            </text>
          </box>
          <box flexGrow={1} flexDirection="column" overflow="hidden">
            {trajectories.slice(scrollOffset, scrollOffset + LIST_HEIGHT).map((trajectory, i) => (
              <TrajectoryRow
                key={trajectory.id}
                trajectory={trajectory}
                selected={i + scrollOffset === cursor}
              />
            ))}
          </box>
        </box>

        {/* Detail pane */}
        <box flexGrow={1} flexDirection="column" borderStyle="single" borderColor="#444444" border={["left"]}>
          {/* Rating bar */}
          {selectedTrajectory && (
            <box height={2} paddingLeft={1} borderStyle="single" borderColor="#333333" border={["bottom"]}>
              <box flexDirection="row" justifyContent="space-between">
                <text>
                  <span fg="#888888">Rating: </span>
                  <span fg={ratingColor(feedback?.rating)}>
                    {renderStars(feedback?.rating)}
                  </span>
                  {feedback?.rating && (
                    <span fg="#666666"> ({feedback.rating}/5)</span>
                  )}
                  {ratingStatus && (
                    <span fg="#00ffff"> {ratingStatus}</span>
                  )}
                </text>
                <text fg="#666666">1-5: rate</text>
              </box>
            </box>
          )}
          <TrajectoryDetail
            trajectory={selectedTrajectory}
            events={events}
            eventsLoading={eventsLoading}
            eventCursor={eventCursor}
            eventScrollOffset={eventScrollOffset}
          />
        </box>

        {/* Weights panel (toggle with 'w') */}
        {showWeights && (
          <box width="25%" flexDirection="column" borderStyle="single" borderColor="#444444" border={["left"]}>
            <WeightsPanel
              weights={weightsData?.weights}
              isLoading={weightsLoading}
            />
          </box>
        )}
      </box>

      {/* Footer with shortcuts */}
      <box height={1} paddingLeft={1} borderStyle="single" borderColor="#333333" border={["top"]}>
        <text fg="#666666">
          j/k: navigate | Enter: detail | f: filter | 1-5: rate | w: {showWeights ? "hide" : "show"} weights | r: refresh
        </text>
      </box>
    </box>
  );
}
