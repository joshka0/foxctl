// Tasks View - Task recommendations with details
import { useState } from "react";
import { useKeyboard } from "@opentui/react";
import { useTasks } from "../hooks/useData";
import { WINDOWED_LIST_HEIGHT } from "../constants";
import type { TaskSummary } from "@foxctl/data";

interface TaskRowProps {
  task: TaskSummary;
  index: number;
  selected: boolean;
}

function TaskRow({ task, selected }: TaskRowProps) {
  const bg = selected ? "#444444" : undefined;
  const cursor = selected ? "> " : "  ";

  // Score color gradient from red (low) to green (high)
  const score = task.score ?? 0;
  const scoreColor =
    score >= 0.8
      ? "#00ff00"
      : score >= 0.5
        ? "#ffff00"
        : "#ff8800";

  return (
    <box backgroundColor={bg} flexDirection="column" paddingBottom={1}>
      <text fg={selected ? "#ffffff" : "#cccccc"}>
        {cursor}
        <span fg={scoreColor}>[{score.toFixed(2)}]</span>
        {"  "}
        {task.title}
      </text>
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
        <span fg="#ffaa00">{(task.score ?? 0).toFixed(4)}</span>
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
  const [scrollOffset, setScrollOffset] = useState(0);
  const { data, isLoading, error, refetch } = useTasks({ limit: 50 });

  const LIST_HEIGHT = WINDOWED_LIST_HEIGHT;

  const tasks = data?.tasks || [];
  const stats = data?.stats;
  const selectedTask = tasks[cursor];

  const updateCursor = (newCursor: number) => {
    setCursor(newCursor);
    if (newCursor < scrollOffset) {
      setScrollOffset(newCursor);
    } else if (newCursor >= scrollOffset + LIST_HEIGHT) {
      setScrollOffset(newCursor - LIST_HEIGHT + 1);
    }
  };

  useKeyboard((e) => {
    switch (e.name) {
      case "up":
      case "k":
        updateCursor(Math.max(0, cursor - 1));
        break;
      case "down":
      case "j":
        updateCursor(Math.min(Math.max(0, tasks.length - 1), cursor + 1));
        break;
      case "r":
        refetch();
        break;
      case "g":
        updateCursor(0);
        break;
      case "G":
        updateCursor(Math.max(0, tasks.length - 1));
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
        <text fg="#888888">No tasks found. Add tasks with 'foxctl todo add'!</text>
      </box>
    );
  }

  return (
    <box flexDirection="row" width="100%" height="100%">
      {/* Task list */}
      <box width="45%" flexDirection="column" borderStyle="single" borderColor="#333333" border={["right"]}>
        <box height={3} paddingLeft={1} paddingTop={1} borderStyle="single" borderColor="#333333" border={["bottom"]}>
          <text fg="#aa77ff">
            <b>TASK LIST</b>
            <span fg="#666666"> ({tasks.length})</span>
          </text>
          {stats && (
            <text fg="#666666">
              P:{stats.pending} | IP:{stats.in_progress} | C:{stats.completed}
            </text>
          )}
        </box>
        <box flexGrow={1} flexDirection="column" overflow="hidden">
          {tasks.slice(scrollOffset, scrollOffset + LIST_HEIGHT).map((task, i) => (
            <TaskRow
              key={task.id}
              task={task}
              index={i + scrollOffset}
              selected={i + scrollOffset === cursor}
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
