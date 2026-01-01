// Jobs View - List with preview pane
import { useState } from "react";
import { useKeyboard } from "@opentui/react";
import { useJobs, useJobDetail } from "../hooks/useData";
import type { JobSummary } from "@agentctl/data";

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

  return (
    <box height={1} backgroundColor={bg} flexDirection="row">
      <text fg="#ffffff">
        {cursor}
        <span fg={stateColor(job.state)}>{job.state.padEnd(8)}</span>
        {" "}
        {job.id.slice(0, 10).padEnd(12)}
        {" "}
        <span fg="#666666">{formatTime(job.created_at)}</span>
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
      <text fg="#aa77ff">
        <b>Job Preview</b>
      </text>
      <text> </text>
      <text>
        <b fg="#666666">ID: </b>
        <span fg="#ffffff">{job.id}</span>
      </text>
      <text>
        <b fg="#666666">Status: </b>
        <span fg={stateColor(job.state)}>{job.state.toUpperCase()}</span>
      </text>
      <text>
        <b fg="#666666">Created: </b>
        <span fg="#ffffff">{job.created_at}</span>
      </text>
      <text> </text>
      <text>
        <b fg="#666666">Command:</b>
      </text>
      <text fg="#ffffff">{job.command}</text>
      {job.error && (
        <>
          <text> </text>
          <text fg="#ff0000">
            <b>Error:</b>
          </text>
          <text fg="#ff6666">{job.error}</text>
        </>
      )}
      {isLoading && (
        <text fg="#666666">Loading details...</text>
      )}
      {detail?.data && (
        <>
          <text> </text>
          <text fg="#00ff00">
            <b>Result:</b>
          </text>
          <text fg="#cccccc">
            {JSON.stringify(detail.data, null, 2).slice(0, 500)}
            {JSON.stringify(detail.data).length > 500 && "..."}
          </text>
        </>
      )}
    </box>
  );
}

export function JobsView() {
  const [cursor, setCursor] = useState(0);
  const { data: jobs, isLoading, error, refetch } = useJobs({ limit: 50 });

  const selectedJob = jobs?.[cursor];
  const { data: detail, isLoading: detailLoading } = useJobDetail(selectedJob?.id);

  useKeyboard((e) => {
    if (!jobs) return;

    switch (e.name) {
      case "up":
      case "k":
        setCursor((c) => Math.max(0, c - 1));
        break;
      case "down":
      case "j":
        setCursor((c) => Math.min(Math.max(0, jobs.length - 1), c + 1));
        break;
      case "r":
        refetch();
        break;
      case "g":
        setCursor(0);
        break;
      case "G":
        setCursor(Math.max(0, jobs.length - 1));
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
      <box width="45%" flexDirection="column">
        <box height={1} paddingLeft={1}>
          <text fg="#aa77ff">
            <b>Jobs</b>
            <span fg="#666666"> ({jobs.length})</span>
          </text>
        </box>
        <box height={1}>
          <text fg="#666666">
            {"  "}STATE{"    "}ID{"          "}TIME
          </text>
        </box>
        <box flexGrow={1} flexDirection="column" overflow="hidden">
          {jobs.map((job, i) => (
            <JobRow key={job.id} job={job} selected={i === cursor} />
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
