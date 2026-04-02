// Stats View - Job statistics dashboard
import { useKeyboard } from "@opentui/react";
import { useStats } from "../hooks/useData";

function stateColor(state: string): string {
  switch (state) {
    case "ok":
    case "success":
      return "#00ff00";
    case "error":
    case "failed":
      return "#ff0000";
    case "running":
      return "#00ffff";
    case "queued":
      return "#ffff00";
    default:
      return "#888888";
  }
}

export function StatsView() {
  const { data, isLoading, error, refetch } = useStats();

  useKeyboard((e) => {
    if (e.name === "r") {
      refetch();
    }
  });

  if (isLoading && !data) {
    return (
      <box padding={1}>
        <text fg="#888888">Loading stats...</text>
      </box>
    );
  }

  if (error) {
    return (
      <box padding={1}>
        <text fg="#ff0000">Error loading stats: {error.message}</text>
        <text fg="#666666">Press r to retry</text>
      </box>
    );
  }

  if (!data) {
    return (
      <box padding={1}>
        <text fg="#888888">No statistics available</text>
      </box>
    );
  }

  const byState = Object.entries(data.by_state).sort(([, a], [, b]) => b - a);
  const byCommand = Object.entries(data.by_command).sort(([, a], [, b]) => b - a);

  return (
    <box flexDirection="column" width="100%" height="100%" padding={1}>
      {/* Header */}
      <box height={2}>
        <text fg="#aa77ff">
          <b>Job Statistics</b>
        </text>
      </box>

      {/* Overview panel */}
      <box
        height={6}
        borderStyle="single"
        borderColor="#444444"
        flexDirection="column"
        padding={1}
      >
        <text fg="#00ff00">
          <b>Overview</b>
        </text>
        <text> </text>
        <text>
          <b fg="#666666">Total Jobs: </b>
          <span fg="#ffffff">{data.total}</span>
        </text>
        <text>
          <b fg="#666666">Last Hour: </b>
          <span fg="#00ffff">{data.recent.last_hour}</span>
          {"  "}
          <b fg="#666666">Last Day: </b>
          <span fg="#00ffff">{data.recent.last_day}</span>
        </text>
      </box>

      <text> </text>

      {/* Two columns */}
      <box flexGrow={1} flexDirection="row">
        {/* By State */}
        <box width="50%" flexDirection="column">
          <box
            flexGrow={1}
            borderStyle="single"
            borderColor="#444444"
            flexDirection="column"
            padding={1}
          >
            <text fg="#ffaa00">
              <b>By State</b>
            </text>
            <text> </text>
            {byState.map(([state, count]) => (
              <text key={state}>
                <span fg={stateColor(state)}>{state.padEnd(12)}</span>
                <span fg="#ffffff">{count}</span>
              </text>
            ))}
            {byState.length === 0 && (
              <text fg="#666666">No jobs yet</text>
            )}
          </box>
        </box>

        {/* By Command */}
        <box width="50%" flexDirection="column">
          <box
            flexGrow={1}
            borderStyle="single"
            borderColor="#444444"
            flexDirection="column"
            padding={1}
          >
            <text fg="#00ffff">
              <b>By Command</b>
            </text>
            <text> </text>
            {byCommand.slice(0, 15).map(([cmd, count]) => (
              <text key={cmd}>
                <span fg="#888888">{cmd.slice(0, 20).padEnd(22)}</span>
                <span fg="#ffffff">{count}</span>
              </text>
            ))}
            {byCommand.length === 0 && (
              <text fg="#666666">No jobs yet</text>
            )}
            {byCommand.length > 15 && (
              <text fg="#666666">...and {byCommand.length - 15} more</text>
            )}
          </box>
        </box>
      </box>
    </box>
  );
}
