// Insights View - Task graph insights (keystones, bottlenecks, cycles)
import { useState } from "react";
import { useKeyboard } from "@opentui/react";
import { useInsights } from "../hooks/useData";
import type { GraphNode } from "@agentctl/data";

interface NodeRowProps {
  node: GraphNode;
  index: number;
  selected: boolean;
}

function NodeRow({ node, index, selected }: NodeRowProps) {
  const bg = selected ? "#444444" : undefined;
  const cursor = selected ? "> " : "  ";

  // Color based on pagerank (higher = more important)
  const prColor =
    node.pagerank >= 0.1
      ? "#ff0000"
      : node.pagerank >= 0.05
        ? "#ffaa00"
        : "#00ff00";

  const idDisplay = node.task_id.length > 16
    ? node.task_id.slice(0, 16) + "..."
    : node.task_id.padEnd(19);

  return (
    <box height={1} backgroundColor={bg}>
      <text fg="#ffffff">
        {cursor}
        <span fg={prColor}>{node.pagerank.toFixed(3).padStart(6)}</span>
        {"  "}
        <span fg="#00ffff">{String(node.critical_path_score).padStart(3)}</span>
        {"  "}
        <span fg="#888888">{idDisplay}</span>
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
  const { data, isLoading, error, refetch } = useInsights();

  const nodes = data?.nodes || [];
  const cycles = data?.cycles || [];
  const topoOrder = data?.topological_order || [];
  const selectedNode = nodes[cursor];

  useKeyboard((e) => {
    switch (e.name) {
      case "up":
      case "k":
        setCursor((c) => Math.max(0, c - 1));
        break;
      case "down":
      case "j":
        setCursor((c) => Math.min(Math.max(0, nodes.length - 1), c + 1));
        break;
      case "r":
        refetch();
        break;
      case "g":
        setCursor(0);
        break;
      case "G":
        setCursor(Math.max(0, nodes.length - 1));
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
      <box height={3} paddingLeft={1} paddingTop={1}>
        <text fg="#aa77ff">
          <b>Task Graph Insights</b>
        </text>
        <text fg="#666666">
          {"  "}Nodes: {nodes.length} | Cycles: {cycles.length} | Topo Order:{" "}
          {topoOrder.length}
        </text>
        {cycles.length > 0 && (
          <text fg="#ff0000">
            {"  "}Circular dependencies detected!
          </text>
        )}
      </box>

      {/* Main content */}
      <box flexGrow={1} flexDirection="row">
        {/* Node list */}
        <box width="55%" flexDirection="column">
          <box height={1} paddingLeft={1}>
            <text fg="#666666">
              {"  "}PR{"      "}CP{"   "}Task ID
            </text>
          </box>
          <box flexGrow={1} flexDirection="column" overflow="hidden">
            {nodes.length === 0 ? (
              <box padding={1}>
                <text fg="#888888">No graph nodes. Add tasks with dependencies!</text>
              </box>
            ) : (
              nodes.map((node, i) => (
                <NodeRow
                  key={node.task_id}
                  node={node}
                  index={i}
                  selected={i === cursor}
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
