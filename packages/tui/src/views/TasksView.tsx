// Tasks View - Task recommendations with details
import { useState } from "react";
import { useKeyboard } from "@opentui/react";
import { useTasks } from "../hooks/useData";
import type { TaskSummary } from "@agentctl/data";

interface TaskRowProps {
  task: TaskSummary;
  index: number;
  selected: boolean;
}

function TaskRow({ task, index, selected }: TaskRowProps) {
  const bg = selected ? "#444444" : undefined;
  const cursor = selected ? "> " : "  ";

  // Score color gradient from red (low) to green (high)
  const scoreColor =
    task.score >= 0.8
      ? "#00ff00"
      : task.score >= 0.5
        ? "#ffff00"
        : "#ff8800";

  return (
    <box backgroundColor={bg} flexDirection="column" paddingBottom={1}>
      <text fg="#ffffff">
        {cursor}
        <span fg={scoreColor}>[{task.score.toFixed(2)}]</span>
        {"  "}
        {task.title}
      </text>
      <text fg="#666666">{"    "}{task.id.slice(0, 32)}</text>
    </box>
  );
}

interface TaskDetailProps {
  task: TaskSummary | undefined;
}

function TaskDetail({ task }: TaskDetailProps) {
  if (!task) {
    return (
      <box padding={1}>
        <text fg="#666666">Select a task to view details</text>
      </box>
    );
  }

  return (
    <box flexDirection="column" padding={1}>
      <text fg="#aa77ff">
        <b>Task Details</b>
      </text>
      <text> </text>
      <text>
        <b fg="#666666">ID: </b>
        <span fg="#ffffff">{task.id}</span>
      </text>
      <text>
        <b fg="#666666">Title: </b>
        <span fg="#ffffff">{task.title}</span>
      </text>
      <text>
        <b fg="#666666">Score: </b>
        <span fg="#ffaa00">{task.score.toFixed(4)}</span>
      </text>
      {task.status && (
        <text>
          <b fg="#666666">Status: </b>
          <span fg="#ffffff">{task.status}</span>
        </text>
      )}
      {task.description && (
        <>
          <text> </text>
          <text fg="#666666">
            <b>Description:</b>
          </text>
          <text fg="#cccccc">{task.description}</text>
        </>
      )}
    </box>
  );
}

export function TasksView() {
  const [cursor, setCursor] = useState(0);
  const { data, isLoading, error, refetch } = useTasks({ limit: 50 });

  const tasks = data?.tasks || [];
  const stats = data?.stats;
  const selectedTask = tasks[cursor];

  useKeyboard((e) => {
    switch (e.name) {
      case "up":
      case "k":
        setCursor((c) => Math.max(0, c - 1));
        break;
      case "down":
      case "j":
        setCursor((c) => Math.min(Math.max(0, tasks.length - 1), c + 1));
        break;
      case "r":
        refetch();
        break;
      case "g":
        setCursor(0);
        break;
      case "G":
        setCursor(Math.max(0, tasks.length - 1));
        break;
    }
  });

  if (isLoading && tasks.length === 0) {
    return (
      <box padding={1}>
        <text fg="#888888">Loading tasks...</text>
      </box>
    );
  }

  if (error) {
    return (
      <box padding={1}>
        <text fg="#ff0000">Error loading tasks: {error.message}</text>
        <text fg="#666666">Press r to retry</text>
      </box>
    );
  }

  if (tasks.length === 0) {
    return (
      <box padding={1}>
        <text fg="#888888">No tasks found. Add tasks with 'agentctl todo add'!</text>
      </box>
    );
  }

  return (
    <box flexDirection="row" width="100%" height="100%">
      {/* Task list */}
      <box width="50%" flexDirection="column">
        <box height={1} paddingLeft={1}>
          <text fg="#aa77ff">
            <b>Task Recommendations</b>
            <span fg="#666666"> ({tasks.length})</span>
          </text>
        </box>
        {stats && (
          <box height={1} paddingLeft={1}>
            <text fg="#666666">
              Total: {stats.total} | Pending: {stats.pending} | In Progress: {stats.in_progress}
            </text>
          </box>
        )}
        <text> </text>
        <box flexGrow={1} flexDirection="column" overflow="hidden">
          {tasks.map((task, i) => (
            <TaskRow
              key={task.id}
              task={task}
              index={i}
              selected={i === cursor}
            />
          ))}
        </box>
      </box>

      {/* Detail pane */}
      <box
        flexGrow={1}
        borderStyle="single"
        borderColor="#444444"
        border={["left"]}
      >
        <TaskDetail task={selectedTask} />
      </box>
    </box>
  );
}
