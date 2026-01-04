// Agent View - Browse agents and their activity streams
import { useState } from "react";
import { useKeyboard } from "@opentui/react";
import { useAgents } from "../hooks/useData";
import { useAgent, useTrajectoryEvents } from "../hooks/useData";
import { WINDOWED_LIST_HEIGHT } from "../constants";
import type { Agent, TrajectoryEvent } from "@agentctl/data";

function stateColor(state: string): string {
  switch (state) {
    case "running":
      return "#00ff00";
    case "starting":
      return "#00ffff";
    case "stopped":
      return "#888888";
    case "error":
      return "#ff0000";
    default:
      return "#666666";
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

interface AgentRowProps {
  agent: Agent;
  selected: boolean;
}

function AgentRow({ agent, selected }: AgentRowProps) {
  const bg = selected ? "#444444" : undefined;
  const cursor = selected ? "> " : "  ";

  return (
    <box height={1} backgroundColor={bg} flexDirection="row">
      <text fg="#ffffff">
        {cursor}
        <span fg={stateColor(agent.state)}>{(agent.state || "?").slice(0, 8).padEnd(9)}</span>
        {"  "}
        <span fg="#aa77ff">{(agent.role || "agent").slice(0, 12).padEnd(13)}</span>
        {"  "}
        <span fg="#888888">{formatDate(agent.created_at).padEnd(14)}</span>
        {"  "}
        <span fg="#666666">{truncate(agent.ns, 30)}</span>
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
      preview = data.summary || data.message || JSON.stringify(data).slice(0, 50);
    } catch {
      preview = event.data_inline_json.slice(0, 50);
    }
  }

  return (
    <box height={1} backgroundColor={bg} flexDirection="row">
      <text fg="#ffffff">
        <span fg="#666666">{formatTime(event.ts).padEnd(12)}</span>
        {"  "}
        <span fg={eventKindColor(event.kind)}>{event.kind.slice(0, 15).padEnd(16)}</span>
        {"  "}
        <span fg="#888888">{truncate(preview, 45)}</span>
      </text>
    </box>
  );
}

interface AgentDetailProps {
  agent: Agent | undefined;
  detail: Agent | undefined;
  events: TrajectoryEvent[] | undefined;
  eventsLoading: boolean;
  eventCursor: number;
}

function AgentDetail({ agent, detail, events, eventsLoading, eventCursor }: AgentDetailProps) {
  if (!agent) {
    return (
      <box padding={1}>
        <text fg="#666666">Select an agent to view details</text>
      </box>
    );
  }

  const agentData = detail || agent;

  return (
    <box flexDirection="column" padding={1}>
      <box flexDirection="row" justifyContent="space-between">
        <text fg={stateColor(agentData.state)}>
          <b>{(agentData.state || "unknown").toUpperCase()}</b>
        </text>
        <text fg="#444444">{agentData.id.slice(0, 16)}...</text>
      </box>
      <text> </text>
      <box flexDirection="row">
        <text>
          <b fg="#666666">Role: </b>
          <span fg="#aa77ff">{agentData.role || "agent"}</span>
        </text>
        <text fg="#666666">{"  |  "}</text>
        <text>
          <b fg="#666666">Created: </b>
          <span fg="#888888">{formatDate(agentData.created_at)}</span>
        </text>
      </box>
      <text> </text>
      <box flexDirection="row">
        <text>
          <b fg="#666666">NS: </b>
          <span fg="#888888">{agentData.ns}</span>
        </text>
      </box>
      {agentData.parent_id && (
        <text fg="#666666">
          <b>Parent: </b>
          {agentData.parent_id.slice(0, 16)}...
        </text>
      )}
      <text> </text>
      <box flexDirection="row">
        {agentData.llm_provider && (
          <text>
            <b fg="#666666">LLM: </b>
            <span fg="#888888">
              {agentData.llm_provider}
              {agentData.llm_model ? `/${agentData.llm_model}` : ""}
            </span>
          </text>
        )}
      </box>
      {agentData.skills_allow && (
        <>
          <text> </text>
          <text fg="#00ff00">
            <b>Skills:</b>
          </text>
          <box paddingLeft={1}>
            <text fg="#888888">{truncate(agentData.skills_allow, 60)}</text>
          </box>
        </>
      )}
      {agentData.policy && (
        <>
          <text> </text>
          <text fg="#ffff00">
            <b>Policy:</b>
          </text>
          <box paddingLeft={1}>
            <text fg="#888888">{truncate(agentData.policy, 60)}</text>
          </box>
        </>
      )}
      <text> </text>
      <text fg="#aa77ff">
        <b>Recent Activity:</b>
      </text>
      <box flexGrow={1} flexDirection="column" paddingTop={1} overflow="hidden">
        {eventsLoading ? (
          <text fg="#666666">Loading events...</text>
        ) : events && events.length > 0 ? (
          events.slice(0, 8).map((event, i) => (
            <EventRow key={event.id} event={event} selected={i === eventCursor} />
          ))
        ) : (
          <text fg="#666666">No recent activity</text>
        )}
      </box>
    </box>
  );
}

// Full-screen agent detail viewer
interface AgentFullDetailProps {
  agent: Agent;
  onClose: () => void;
}

function AgentFullDetail({ agent, onClose }: AgentFullDetailProps) {
  const [eventCursor, setEventCursor] = useState(0);
  const [eventScrollOffset, setEventScrollOffset] = useState(0);
  const EVENT_LIST_HEIGHT = WINDOWED_LIST_HEIGHT;

  const { data: events, isLoading, refetch } = useTrajectoryEvents(agent.id, {
    limit: 100,
  });

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

  return (
    <box flexDirection="column" width="100%" height="100%">
      {/* Header */}
      <box height={2} paddingLeft={1} borderStyle="single" borderColor="#333333" border={["bottom"]}>
        <text fg="#aa77ff">
          <b>AGENT DETAIL</b>
          <span fg="#666666"> | {agent.id.slice(0, 24)}...</span>
        </text>
        <text fg="#666666">j/k: navigate events | q/Esc: close</text>
      </box>

      {/* Agent Info */}
      <box height={8} paddingLeft={1} paddingTop={1} borderStyle="single" borderColor="#333333" border={["bottom"]}>
        <box flexDirection="row">
          <text fg={stateColor(agent.state)}>
            <b>{(agent.state || "unknown").toUpperCase()}</b>
          </text>
          <text fg="#666666">{"  |  "}</text>
          <text>
            <b fg="#666666">Role: </b>
            <span fg="#aa77ff">{agent.role || "agent"}</span>
          </text>
        </box>
        <text>
          <b fg="#666666">Namespace: </b>
          <span fg="#888888">{agent.ns}</span>
        </text>
        <text>
          <b fg="#666666">Created: </b>
          <span fg="#888888">{formatDate(agent.created_at)}</span>
        </text>
        {agent.parent_id && (
          <text>
            <b fg="#666666">Parent: </b>
            <span fg="#888888">{agent.parent_id}</span>
          </text>
        )}
        {agent.llm_provider && (
          <text>
            <b fg="#666666">LLM: </b>
            <span fg="#888888">{agent.llm_provider}{agent.llm_model ? `/${agent.llm_model}` : ""}</span>
          </text>
        )}
        {agent.skills_allow && (
          <text>
            <b fg="#666666">Skills: </b>
            <span fg="#888888">{truncate(agent.skills_allow, 60)}</span>
          </text>
        )}
      </box>

      {/* Events list */}
      <box flexGrow={1} flexDirection="column" paddingLeft={1}>
        <box height={2} paddingTop={1}>
          <text fg="#ffff00">
            <b>ACTIVITY</b>
            <span fg="#666666"> ({events?.length || 0} events)</span>
          </text>
        </box>
        {isLoading ? (
          <text fg="#666666">Loading events...</text>
        ) : events && events.length > 0 ? (
          <box flexDirection="column" overflow="hidden">
            <box height={1}>
              <text fg="#555555">
                TIME          KIND             PREVIEW
              </text>
            </box>
            {events.slice(eventScrollOffset, eventScrollOffset + EVENT_LIST_HEIGHT).map((event, i) => (
              <EventRow key={event.id} event={event} selected={i + eventScrollOffset === eventCursor} />
            ))}
          </box>
        ) : (
          <text fg="#666666">No activity recorded</text>
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

export function AgentView() {
  const [cursor, setCursor] = useState(0);
  const [scrollOffset, setScrollOffset] = useState(0);
  const [stateFilter, setStateFilter] = useState<string>("");
  const [eventCursor, setEventCursor] = useState(0);
  const [showFullDetail, setShowFullDetail] = useState(false);

  const stateFilters = ["", "running", "starting", "stopped", "error"];
  const [stateIndex, setStateIndex] = useState(0);

  const { data, isLoading, error, refetch } = useAgents({
    state: (stateFilter || undefined) as "running" | "starting" | "stopped" | "error" | undefined,
    limit: 50,
  });

  const agents = data?.agents || [];
  const selectedAgent = agents[cursor];

  const { data: agentDetail } = useAgent(selectedAgent?.id);

  // Get trajectory events for the selected agent (using agent id as trajectory id for now)
  const { data: events, isLoading: eventsLoading } = useTrajectoryEvents(
    selectedAgent?.id,
    { limit: 20 }
  );

  const LIST_HEIGHT = WINDOWED_LIST_HEIGHT;

  const updateCursor = (newCursor: number) => {
    setCursor(newCursor);
    if (newCursor < scrollOffset) {
      setScrollOffset(newCursor);
    } else if (newCursor >= scrollOffset + LIST_HEIGHT) {
      setScrollOffset(newCursor - LIST_HEIGHT + 1);
    }
  };

  useKeyboard((e) => {
    if (showFullDetail) return; // Full detail view handles its own keys
    switch (e.name) {
      case "up":
      case "k":
        updateCursor(Math.max(0, cursor - 1));
        break;
      case "down":
      case "j":
        updateCursor(Math.min(Math.max(0, agents.length - 1), cursor + 1));
        break;
      case "return":
        if (selectedAgent) {
          setShowFullDetail(true);
        }
        break;
      case "r":
        refetch();
        break;
      case "g":
        updateCursor(0);
        break;
      case "G":
        updateCursor(Math.max(0, agents.length - 1));
        break;
      case "f":
      case "tab":
        // Cycle through state filters
        const nextIdx = (stateIndex + 1) % stateFilters.length;
        setStateIndex(nextIdx);
        setStateFilter(stateFilters[nextIdx]);
        setCursor(0);
        setScrollOffset(0);
        break;
      case "F":
        // Cycle backwards
        const prevIdx = (stateIndex - 1 + stateFilters.length) % stateFilters.length;
        setStateIndex(prevIdx);
        setStateFilter(stateFilters[prevIdx]);
        setCursor(0);
        setScrollOffset(0);
        break;
      case "h":
        // Navigate events up
        setEventCursor((c) => Math.max(0, c - 1));
        break;
      case "l":
        // Navigate events down
        setEventCursor((c) => Math.min(7, c + 1));
        break;
    }
  });

  if (isLoading && !data) {
    return (
      <box padding={1}>
        <text fg="#888888">Loading agents...</text>
      </box>
    );
  }

  if (error) {
    return (
      <box padding={1}>
        <text fg="#ff0000">Error loading agents: {error.message}</text>
        <text fg="#666666">Press r to retry</text>
      </box>
    );
  }

  if (agents.length === 0) {
    return (
      <box padding={1} flexDirection="column">
        <box height={1} flexDirection="row" paddingLeft={1}>
          <text fg="#666666">Filter: </text>
          <text fg={!stateFilter ? "#00ff00" : "#666666"}>
            {!stateFilter ? <b>[All]</b> : "[All]"}
          </text>
          {stateFilters.slice(1).map((s) => (
            <text key={s} fg={stateFilter === s ? "#00ff00" : "#666666"}>
              {" "}
              {stateFilter === s ? <b>[{s}]</b> : `[${s}]`}
            </text>
          ))}
        </box>
        <text fg="#888888">No agents found{stateFilter ? ` with state: ${stateFilter}` : ""}</text>
        <text fg="#666666">Press f to cycle filters, r to refresh</text>
      </box>
    );
  }

  // Show full detail view when Enter is pressed
  if (showFullDetail && selectedAgent) {
    return (
      <AgentFullDetail
        agent={selectedAgent}
        onClose={() => setShowFullDetail(false)}
      />
    );
  }

  return (
    <box flexDirection="row" width="100%" height="100%">
      {/* Agents list */}
      <box width="50%" flexDirection="column" borderStyle="single" borderColor="#333333" border={["right"]}>
        <box height={4} paddingLeft={1} paddingTop={1} borderStyle="single" borderColor="#333333" border={["bottom"]}>
          <text fg="#aa77ff">
            <b>AGENTS</b>
            <span fg="#666666"> ({data?.total || 0}) | Enter: full detail</span>
          </text>
          <box height={1} flexDirection="row">
            <text fg="#666666">Filter: </text>
            <text fg={!stateFilter ? "#00ff00" : "#666666"}>
              {!stateFilter ? <b>[All]</b> : "[All]"}
            </text>
            {stateFilters.slice(1).map((s) => (
              <text key={s} fg={stateFilter === s ? "#00ff00" : "#666666"}>
                {" "}
                {stateFilter === s ? <b>[{s}]</b> : `[${s}]`}
              </text>
            ))}
          </box>
          <text fg="#666666">
            STATE      ROLE           CREATED          NAMESPACE
          </text>
        </box>
        <box flexGrow={1} flexDirection="column" overflow="hidden">
          {agents.slice(scrollOffset, scrollOffset + LIST_HEIGHT).map((agent, i) => (
            <AgentRow
              key={agent.id}
              agent={agent}
              selected={i + scrollOffset === cursor}
            />
          ))}
        </box>
      </box>

      {/* Detail pane */}
      <box flexGrow={1} borderStyle="single" borderColor="#444444" border={["left"]}>
        <AgentDetail
          agent={selectedAgent}
          detail={agentDetail}
          events={events}
          eventsLoading={eventsLoading}
          eventCursor={eventCursor}
        />
      </box>
    </box>
  );
}
