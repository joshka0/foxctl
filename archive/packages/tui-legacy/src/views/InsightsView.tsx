// Insights View - Task graph insights (keystones, bottlenecks, cycles)
import { useState } from "react";
import { useKeyboard } from "@opentui/react";
import { useInsights } from "../hooks/useData";
import { WINDOWED_LIST_HEIGHT } from "../constants";
import type { GraphNode } from "@agentctl/data";

interface NodeRowProps {
  node: GraphNode;
  index: number;
  selected: boolean;
}

function NodeRow({ node, selected }: NodeRowProps) {
  const bg = selected ? "#444444" : undefined;
  const cursor = selected ? "> " : "  ";

  // Color based on pagerank (higher = more important)
  const prColor =
    node.pagerank >= 0.1
      ? "#ff0000"
      : node.pagerank >= 0.05
        ? "#ffaa00"
        : "#00ff00";

  const titleDisplay = node.title || node.task_id;
  const display = titleDisplay.length > 40
    ? titleDisplay.slice(0, 37) + "..."
    : titleDisplay;

  return (
    <box height={1} backgroundColor={bg}>
      <text fg="#ffffff">
        {cursor}
        <span fg={prColor}>{node.pagerank.toFixed(3).padStart(6)}</span>
        {"  "}
        <span fg="#00ffff">{String(node.critical_path_score).padStart(3)}</span>
        {"  "}
        <span fg={node.title ? "#ffffff" : "#666666"}>{display}</span>
      </text>
    </box>
  );
}

interface NodeDetailProps {
  node: GraphNode | undefined;
}

function NodeDetail({ node }: NodeDetailProps) {
  if (!node) {
    return (
      <box padding={1}>
        <text fg="#666666">Select a node to view details</text>
      </box>
    );
  }

  return (
    <box flexDirection="column" padding={1}>
      <text fg="#aa77ff">
        <b>Node Details</b>
      </text>
      <text> </text>
      <text>
        <b fg="#666666">Task ID: </b>
        <span fg="#ffffff">{node.task_id}</span>
      </text>
      {node.title && (
        <text>
          <b fg="#666666">Title: </b>
          <span fg="#ffffff">{node.title}</span>
        </text>
      )}
      <text> </text>
      <text fg="#aa77ff">
        <b>Graph Metrics</b>
      </text>
      <text>
        <b fg="#666666">PageRank: </b>
        <span fg="#ffaa00">{node.pagerank.toFixed(6)}</span>
      </text>
      <text>
        <b fg="#666666">Critical Path: </b>
        <span fg="#00ffff">{node.critical_path_score}</span>
      </text>
      <text>
        <b fg="#666666">In-Degree: </b>
        <span fg="#ffffff">{node.in_degree}</span>
      </text>
      <text>
        <b fg="#666666">Out-Degree: </b>
        <span fg="#ffffff">{node.out_degree}</span>
      </text>
    </box>
  );
}

export function InsightsView() {
  const [cursor, setCursor] = useState(0);
  const [scrollOffset, setScrollOffset] = useState(0);
  const { data, isLoading, error, refetch } = useInsights();

  const LIST_HEIGHT = WINDOWED_LIST_HEIGHT;

  const nodes = data?.nodes || [];
  const cycles = data?.cycles || [];
  const topoOrder = data?.topological_order || [];
  const selectedNode = nodes[cursor];

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
        updateCursor(Math.min(Math.max(0, nodes.length - 1), cursor + 1));
        break;
      case "r":
        refetch();
        break;
      case "g":
        updateCursor(0);
        break;
      case "G":
        updateCursor(Math.max(0, nodes.length - 1));
        break;
    }
  });

  if (isLoading && nodes.length === 0) {
    return (
      <box padding={1}>
        <text fg="#888888">Loading insights...</text>
      </box>
    );
  }

  if (error) {
    return (
      <box padding={1}>
        <text fg="#ff0000">Error loading insights: {error.message}</text>
        <text fg="#666666">Press r to retry</text>
      </box>
    );
  }

  return (
    <box flexDirection="column" width="100%" height="100%">
      {/* Overview stats */}
      <box height={3} paddingLeft={1} paddingTop={1} borderStyle="single" borderColor="#333333" border={["bottom"]}>
        <box flexDirection="row" justifyContent="space-between">
          <text fg="#aa77ff">
            <b>TASK GRAPH INSIGHTS</b>
          </text>
          <text fg="#666666">
            Nodes: {nodes.length} | Cycles: {cycles.length} | Topo: {topoOrder.length} {"  "}
          </text>
        </box>
        {cycles.length > 0 && (
          <text fg="#ff0000">
            Circular dependencies detected!
          </text>
        )}
      </box>

      {/* Main content */}
      <box flexGrow={1} flexDirection="row">
        {/* Node list */}
        <box width="55%" flexDirection="column">
          <box height={1} paddingLeft={1}>
            <text fg="#666666">
              {"  "}PR{"      "}CP{"   "}Task Name
            </text>
          </box>
          <box flexGrow={1} flexDirection="column" overflow="hidden">
            {nodes.length === 0 ? (
              <box padding={1}>
                <text fg="#888888">No graph nodes. Add tasks with dependencies!</text>
              </box>
            ) : (
              nodes.slice(scrollOffset, scrollOffset + LIST_HEIGHT).map((node, i) => (
                <NodeRow
                  key={node.task_id}
                  node={node}
                  index={i + scrollOffset}
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
          <NodeDetail node={selectedNode} />
        </box>
      </box>
    </box>
  );
}
