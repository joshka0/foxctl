// Jobs View - List with preview pane
import { useState } from "react";
import { useKeyboard } from "@opentui/react";
import { useJobs, useJobDetail } from "../hooks/useData";
import { WINDOWED_LIST_HEIGHT } from "../constants";
import type { JobSummary } from "@foxctl/data";

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

function formatTime(isoString: string): string {
  try {
    const date = new Date(isoString);
    return date.toLocaleTimeString("en-US", { hour12: false });
  } catch {
    return isoString.slice(11, 19);
  }
}

interface JobRowProps {
  job: JobSummary;
  selected: boolean;
}

function JobRow({ job, selected }: JobRowProps) {
  const bg = selected ? "#444444" : undefined;
  const cursor = selected ? "> " : "  ";

  const commandDisplay = job.command.length > 50
    ? job.command.slice(0, 47) + "..."
    : job.command;

  return (
    <box height={1} backgroundColor={bg} flexDirection="row">
      <text fg="#ffffff">
        {cursor}
        <span fg={stateColor(job.state)}>{job.state.padEnd(8)}</span>
        {"  "}
        <span fg={selected ? "#ffffff" : "#cccccc"}>{commandDisplay.padEnd(50)}</span>
        {"  "}
        <span fg="#666666">{formatTime(job.created_at)}</span>
        {"  "}
        <span fg="#444444">{job.id.slice(-6)}</span>
      </text>
    </box>
  );
}

interface JobPreviewProps {
  job: JobSummary | undefined;
  detail: any;
  isLoading: boolean;
}

function JobPreview({ job, detail, isLoading }: JobPreviewProps) {
  if (!job) {
    return (
      <box padding={1}>
        <text fg="#666666">Select a job to view details</text>
      </box>
    );
  }

  return (
    <box flexDirection="column" padding={1}>
      <box flexDirection="row" justifyContent="space-between">
        <text fg="#aa77ff">
          <b>Job Preview</b>
        </text>
        <text fg="#444444">{job.id}</text>
      </box>
      <text> </text>
      <box flexDirection="row">
        <text>
          <b fg="#666666">Status: </b>
          <span fg={stateColor(job.state)}>{job.state.toUpperCase()}</span>
        </text>
        <text fg="#666666">{"  "}|{"  "}</text>
        <text>
          <b fg="#666666">Created: </b>
          <span fg="#ffffff">{new Date(job.created_at).toLocaleString()}</span>
        </text>
      </box>
      <text> </text>
      <text>
        <b fg="#aa77ff">Command:</b>
      </text>
      <box paddingLeft={2} paddingTop={1}>
        <text fg="#ffffff">{job.command}</text>
      </box>

      {job.error && (
        <>
          <text> </text>
          <text fg="#ff0000">
            <b>Error:</b>
          </text>
          <box paddingLeft={2} paddingTop={1}>
            <text fg="#ff6666">{job.error}</text>
          </box>
        </>
      )}

      {isLoading && (
        <box paddingTop={1}>
          <text fg="#666666">Loading details...</text>
        </box>
      )}

      {detail?.result_data && (
        <>
          <text> </text>
          <text fg="#00ff00">
            <b>Result Data:</b>
          </text>
          <box paddingLeft={2} paddingTop={1}>
            <text fg="#cccccc">
              {(() => {
                const json = JSON.stringify(detail.result_data, null, 2);
                return (
                  <>
                    {json.slice(0, 5000)}
                    {json.length > 5000 && "..."}
                  </>
                );
              })()}
            </text>
          </box>
        </>
      )}
    </box>
  );
}

export function JobsView() {
  const [cursor, setCursor] = useState(0);
  const [scrollOffset, setScrollOffset] = useState(0);
  const { data: jobs, isLoading, error, refetch } = useJobs({ limit: 50 });

  const LIST_HEIGHT = WINDOWED_LIST_HEIGHT; // Number of items to show at once

  const selectedJob = jobs?.[cursor];
  const { data: detail, isLoading: detailLoading } = useJobDetail(selectedJob?.id);

  const updateCursor = (newCursor: number) => {
    if (!jobs) return;
    setCursor(newCursor);
    // Scroll window to keep cursor visible
    if (newCursor < scrollOffset) {
      setScrollOffset(newCursor);
    } else if (newCursor >= scrollOffset + LIST_HEIGHT) {
      setScrollOffset(newCursor - LIST_HEIGHT + 1);
    }
  };

  useKeyboard((e) => {
    if (!jobs) return;

    switch (e.name) {
      case "up":
      case "k":
        updateCursor(Math.max(0, cursor - 1));
        break;
      case "down":
      case "j":
        updateCursor(Math.min(Math.max(0, jobs.length - 1), cursor + 1));
        break;
      case "r":
        refetch();
        break;
      case "g":
        updateCursor(0);
        break;
      case "G":
        updateCursor(Math.max(0, jobs.length - 1));
        break;
    }
  });

  if (isLoading && !jobs) {
    return (
      <box padding={1}>
        <text fg="#888888">Loading jobs...</text>
      </box>
    );
  }

  if (error) {
    return (
      <box padding={1}>
        <text fg="#ff0000">Error loading jobs: {error.message}</text>
        <text fg="#666666">Press r to retry</text>
      </box>
    );
  }

  if (!jobs || jobs.length === 0) {
    return (
      <box padding={1}>
        <text fg="#888888">No jobs found</text>
      </box>
    );
  }

  return (
    <box flexDirection="row" width="100%" height="100%">
      {/* Job list */}
      <box width="50%" flexDirection="column" borderStyle="single" borderColor="#333333" border={["right"]}>
        <box height={3} paddingLeft={1} paddingTop={1} borderStyle="single" borderColor="#333333" border={["bottom"]}>
          <text fg="#aa77ff">
            <b>JOB HISTORY</b>
            <span fg="#666666"> ({jobs.length})</span>
          </text>
          <text fg="#666666">
            STATE     COMMAND                                           TIME
          </text>
        </box>
        <box flexGrow={1} flexDirection="column" overflow="hidden">
          {jobs.slice(scrollOffset, scrollOffset + LIST_HEIGHT).map((job, i) => (
            <JobRow
              key={job.id}
              job={job}
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
        <JobPreview
          job={selectedJob}
          detail={detail}
          isLoading={detailLoading}
        />
      </box>
    </box>
  );
}
