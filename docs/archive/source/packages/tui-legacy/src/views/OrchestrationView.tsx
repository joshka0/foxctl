// Orchestration View - Agent hierarchy + mailbox activity + pending questions (human-in-loop)
import { useState, useCallback, useMemo } from "react";
import { useKeyboard } from "@opentui/react";
import { useTimeoutManager } from "../hooks/useTimeoutManager";
import {
  useAgents,
  useMailbox,
  useSendMailboxMessage,
  useAcknowledgeMessage,
  useSpawnAgent,
  useStopAgent,
  useOrchestrationBoard,
  useOrchestrationCardRuntime,
  useApplyOrchestrationCardAction,
  useRefreshOrchestration,
  useDispatchOrchestrationIssue,
  useWorkspaces,
  useSwitchWorkspace,
} from "../hooks/useData";
import type {
  Agent,
  MailboxMessage,
  OrchestrationBoard,
  OrchestrationBoardArtifactRef,
  OrchestrationCard,
  OrchestrationCardAction,
  OrchestrationRuntimeTreeNode,
} from "@foxctl/data";

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

function stateIcon(state: string): string {
  switch (state) {
    case "running":
      return ">";
    case "starting":
      return "~";
    case "stopped":
      return "-";
    case "error":
      return "!";
    default:
      return "?";
  }
}

function roleIcon(role: string | undefined): string {
  switch (role?.toLowerCase()) {
    case "overseer":
      return "O";
    case "coder":
    case "engineer":
      return "C";
    case "planner":
      return "P";
    case "reviewer":
      return "R";
    case "tester":
      return "T";
    default:
      return "A";
  }
}

function priorityColor(priority: number): string {
  if (priority <= 1) return "#ff0000";
  if (priority <= 2) return "#ffaa00";
  if (priority <= 3) return "#ffff00";
  return "#888888";
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

function formatAgentSkills(skills: Agent["skills_allow"]): string {
  if (!skills) return "";
  return Array.isArray(skills) ? skills.join(", ") : skills;
}

function formatAgentPolicy(policy: Agent["policy"]): string {
  if (!policy) return "";
  if (typeof policy === "string") return policy;
  try {
    return JSON.stringify(policy);
  } catch {
    return String(policy);
  }
}

function workspaceID(): string | undefined {
  if (typeof process !== "undefined" && typeof process.cwd === "function") {
    return process.cwd();
  }
  return undefined;
}

function workspaceDisplayName(workspace: string | undefined): string {
  if (!workspace) return "(unset)";
  const normalized = workspace.replace(/\\/g, "/");
  const parts = normalized.split("/").filter(Boolean);
  return parts[parts.length - 1] || workspace;
}

function cardTitle(card: OrchestrationCard): string {
  const issue = card.issue_identifier || card.issue_id;
  const title = (card.title || "").trim();
  if (!title) return issue;
  return `${issue}: ${title}`;
}

function laneColor(lane: string | undefined): string {
  switch (lane) {
    case "Running":
      return "#00ff00";
    case "RetryQueued":
      return "#ffaa00";
    case "Blocked":
      return "#ff4444";
    case "Review":
      return "#00ffff";
    case "Done":
      return "#888888";
    case "Claimed":
      return "#ffff00";
    default:
      return "#aa77ff";
  }
}

function canRetryNow(card: OrchestrationCard | undefined): boolean {
  return card?.lane === "Blocked" || card?.lane === "RetryQueued";
}

function canRelease(card: OrchestrationCard | undefined): boolean {
  return (
    !!card &&
    card.state !== "Running" &&
    card.state !== "Claimed" &&
    card.lane !== "Todo"
  );
}

function canMarkDone(card: OrchestrationCard | undefined): boolean {
  return (
    !!card &&
    card.state !== "Running" &&
    card.state !== "Claimed" &&
    card.lane !== "Done"
  );
}

function canDispatch(card: OrchestrationCard | undefined): boolean {
  if (!card) return false;
  return (
    card.lane === "Todo" ||
    card.lane === "RetryQueued" ||
    card.lane === "Blocked"
  );
}

interface BoardRowData {
  lane: string;
  card: OrchestrationCard;
}

function flattenBoard(
  board: OrchestrationBoard | null | undefined,
): BoardRowData[] {
  if (!board?.lanes) return [];
  const rows: BoardRowData[] = [];
  for (const lane of board.lanes) {
    for (const card of lane.cards || []) {
      rows.push({ lane: lane.id, card });
    }
  }
  return rows;
}

function formatRuntimeLines(
  node: OrchestrationRuntimeTreeNode | undefined,
  depth = 0,
): string[] {
  if (!node) return [];
  const indent = "  ".repeat(depth);
  const lines = [
    `${indent}${node.tag || node.agent_id || "node"} [${node.status || "unknown"}]`,
  ];
  if (node.agent_id) {
    lines.push(`${indent}  agent: ${node.agent_id}`);
  }
  if (node.error) {
    lines.push(`${indent}  error: ${node.error}`);
  }
  if (node.state !== undefined && node.state !== null) {
    try {
      lines.push(`${indent}  state: ${JSON.stringify(node.state)}`);
    } catch {
      lines.push(`${indent}  state: ${String(node.state)}`);
    }
  }
  for (const child of node.children || []) {
    lines.push(...formatRuntimeLines(child, depth + 1));
  }
  return lines;
}

// Build tree structure from flat agent list
interface AgentNode {
  agent: Agent;
  children: AgentNode[];
  depth: number;
}

function buildAgentTree(agents: Agent[]): AgentNode[] {
  const nodeMap = new Map<string, AgentNode>();
  const roots: AgentNode[] = [];

  // Create nodes for all agents
  for (const agent of agents) {
    nodeMap.set(agent.id, { agent, children: [], depth: 0 });
  }

  // Build parent-child relationships
  for (const agent of agents) {
    const node = nodeMap.get(agent.id)!;
    if (agent.parent_id && nodeMap.has(agent.parent_id)) {
      const parent = nodeMap.get(agent.parent_id)!;
      parent.children.push(node);
    } else {
      roots.push(node);
    }
  }

  // Set depths
  function setDepths(nodes: AgentNode[], depth: number) {
    for (const node of nodes) {
      node.depth = depth;
      setDepths(node.children, depth + 1);
    }
  }
  setDepths(roots, 0);

  return roots;
}

// Flatten tree for display
function flattenTree(roots: AgentNode[]): AgentNode[] {
  const result: AgentNode[] = [];
  function traverse(nodes: AgentNode[]) {
    for (const node of nodes) {
      result.push(node);
      traverse(node.children);
    }
  }
  traverse(roots);
  return result;
}

interface AgentRowProps {
  node: AgentNode;
  selected: boolean;
}

function AgentRow({ node, selected }: AgentRowProps) {
  const { agent, depth } = node;
  const bg = selected ? "#444444" : undefined;
  const cursor = selected ? "> " : "  ";
  const indent = "  ".repeat(depth);
  const prefix = depth > 0 ? "|- " : "";

  return (
    <box height={1} backgroundColor={bg} flexDirection="row">
      <text fg="#ffffff">
        {cursor}
        <span fg="#444444">
          {indent}
          {prefix}
        </span>
        <span fg={stateColor(agent.state)}>[{stateIcon(agent.state)}]</span>{" "}
        <span fg="#aa77ff">{roleIcon(agent.role)}</span>{" "}
        <span fg="#ffffff">
          {truncate(agent.role || "agent", 12).padEnd(13)}
        </span>
        {"  "}
        <span fg="#888888">{truncate(agent.ns, 20).padEnd(21)}</span>
        {"  "}
        <span fg="#666666">{truncate(agent.id, 12)}</span>
      </text>
    </box>
  );
}

interface MessageRowProps {
  message: MailboxMessage;
  selected: boolean;
}

function MessageRow({ message, selected }: MessageRowProps) {
  const bg = selected ? "#333333" : undefined;

  return (
    <box height={1} backgroundColor={bg} flexDirection="row">
      <text fg="#ffffff">
        <span fg="#666666">{formatTime(message.created_at).padEnd(12)}</span>
        {"  "}
        <span fg="#00ffff">{truncate(message.sender, 12).padEnd(13)}</span>
        {"  "}
        <span fg={priorityColor(message.priority)}>P{message.priority}</span>
        {"  "}
        <span fg="#888888">{truncate(message.kind, 10).padEnd(11)}</span>
        {"  "}
        <span fg="#cccccc">{truncate(message.subject, 35)}</span>
      </text>
    </box>
  );
}

interface HierarchyPanelProps {
  agents: Agent[];
  cursor: number;
  scrollOffset: number;
  listHeight: number;
}

function HierarchyPanel({
  agents,
  cursor,
  scrollOffset,
  listHeight,
}: HierarchyPanelProps) {
  const tree = buildAgentTree(agents);
  const flatList = flattenTree(tree);

  if (flatList.length === 0) {
    return (
      <box padding={1}>
        <text fg="#888888">No agents found</text>
        <text fg="#666666">Press r to refresh</text>
      </box>
    );
  }

  return (
    <box flexDirection="column" overflow="hidden">
      <box height={1} paddingLeft={1} paddingBottom={1}>
        <text fg="#555555">ST R ROLE NAMESPACE ID</text>
      </box>
      {flatList
        .slice(scrollOffset, scrollOffset + listHeight)
        .map((node, i) => (
          <AgentRow
            key={node.agent.id}
            node={node}
            selected={i + scrollOffset === cursor}
          />
        ))}
    </box>
  );
}

interface ActivityPanelProps {
  messages: MailboxMessage[] | undefined;
  cursor: number;
  scrollOffset: number;
  listHeight: number;
}

function ActivityPanel({
  messages,
  cursor,
  scrollOffset,
  listHeight,
}: ActivityPanelProps) {
  if (!messages || messages.length === 0) {
    return (
      <box padding={1}>
        <text fg="#888888">No recent mailbox activity</text>
      </box>
    );
  }

  return (
    <box flexDirection="column" overflow="hidden">
      <box height={1} paddingLeft={1} paddingBottom={1}>
        <text fg="#555555">TIME FROM P KIND SUBJECT</text>
      </box>
      {messages.slice(scrollOffset, scrollOffset + listHeight).map((msg, i) => (
        <MessageRow
          key={msg.id}
          message={msg}
          selected={i + scrollOffset === cursor}
        />
      ))}
    </box>
  );
}

interface BoardPanelProps {
  rows: BoardRowData[];
  cursor: number;
  scrollOffset: number;
  listHeight: number;
  artifact: OrchestrationBoardArtifactRef | null;
}

function BoardPanel({
  rows,
  cursor,
  scrollOffset,
  listHeight,
  artifact,
}: BoardPanelProps) {
  if (artifact) {
    return (
      <box padding={1} flexDirection="column">
        <text fg="#ffaa00">Board payload was moved to CAS</text>
        <text fg="#cccccc">{artifact.summary}</text>
        <text fg="#888888">{artifact.artifact}</text>
        {artifact.hint && <text fg="#666666">{artifact.hint}</text>}
      </box>
    );
  }

  if (rows.length === 0) {
    return (
      <box padding={1}>
        <text fg="#888888">No orchestration cards projected</text>
      </box>
    );
  }

  return (
    <box flexDirection="column" overflow="hidden">
      <box height={1} paddingLeft={1} paddingBottom={1}>
        <text fg="#555555">LANE ISSUE POLICY OUTCOME TITLE</text>
      </box>
      {rows.slice(scrollOffset, scrollOffset + listHeight).map((row, i) => {
        const selected = i + scrollOffset === cursor;
        const bg = selected ? "#223344" : undefined;
        return (
          <box
            key={row.card.issue_id}
            height={1}
            backgroundColor={bg}
            flexDirection="row"
          >
            <text fg="#ffffff">
              {selected ? "> " : "  "}
              <span fg={laneColor(row.lane)}>
                {truncate(row.lane, 11).padEnd(12)}
              </span>
              <span fg="#00ffff">
                {truncate(
                  row.card.issue_identifier || row.card.issue_id,
                  23,
                ).padEnd(24)}
              </span>
              <span fg="#888888">
                {truncate(row.card.policy_status || "-", 11).padEnd(12)}
              </span>
              <span fg="#ffaa00">
                {truncate(row.card.last_outcome || "-", 13).padEnd(14)}
              </span>
              <span fg="#cccccc">{truncate(row.card.title || "", 28)}</span>
            </text>
          </box>
        );
      })}
    </box>
  );
}

// Pending Questions Panel - shows agent.ask messages awaiting human response
interface PendingQuestionRowProps {
  message: MailboxMessage;
  selected: boolean;
}

function PendingQuestionRow({ message, selected }: PendingQuestionRowProps) {
  const bg = selected ? "#442200" : undefined;

  return (
    <box height={2} backgroundColor={bg} flexDirection="column" paddingLeft={1}>
      <text fg="#ffffff">
        {selected ? "> " : "  "}
        <span fg={priorityColor(message.priority)}>[P{message.priority}]</span>
        {"  "}
        <span fg="#00ffff">{truncate(message.sender, 15)}</span>
        <span fg="#888888"> asks: </span>
        <span fg="#ffffff">{truncate(message.subject, 50)}</span>
      </text>
      <text fg="#666666">
        {"     "}
        {truncate(message.body, 70)}
      </text>
    </box>
  );
}

interface PendingQuestionsPanelProps {
  questions: MailboxMessage[];
  cursor: number;
  scrollOffset: number;
  listHeight: number;
}

function PendingQuestionsPanel({
  questions,
  cursor,
  scrollOffset,
  listHeight,
}: PendingQuestionsPanelProps) {
  if (questions.length === 0) {
    return (
      <box padding={1}>
        <text fg="#888888">No pending questions from agents</text>
        <text fg="#666666">
          {"\n"}
          When agents send questions to the overseer, they'll appear here
        </text>
      </box>
    );
  }

  return (
    <box flexDirection="column" overflow="hidden">
      <box height={1} paddingLeft={1}>
        <text fg="#555555">[a]nswer [d]elegate [v]iew context</text>
      </box>
      {questions
        .slice(scrollOffset, scrollOffset + listHeight)
        .map((msg, i) => (
          <PendingQuestionRow
            key={msg.id}
            message={msg}
            selected={i + scrollOffset === cursor}
          />
        ))}
    </box>
  );
}

// Full-screen agent detail (reusing from AgentView patterns)
interface OrchAgentDetailProps {
  agent: Agent;
  onClose: () => void;
}

function OrchAgentDetail({ agent, onClose }: OrchAgentDetailProps) {
  useKeyboard((e) => {
    if (e.name === "escape" || e.name === "q") {
      onClose();
    }
  });

  return (
    <box flexDirection="column" width="100%" height="100%">
      <box
        height={2}
        paddingLeft={1}
        borderStyle="single"
        borderColor="#333333"
        border={["bottom"]}
      >
        <text fg="#aa77ff">
          <b>AGENT DETAIL</b>
          <span fg="#666666"> | {agent.id.slice(0, 24)}...</span>
        </text>
        <text fg="#666666">q/Esc: close</text>
      </box>
      <box flexGrow={1} padding={1} flexDirection="column">
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
        <text> </text>
        <text>
          <b fg="#666666">ID: </b>
          <span fg="#888888">{agent.id}</span>
        </text>
        <text>
          <b fg="#666666">Namespace: </b>
          <span fg="#888888">{agent.ns}</span>
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
            <span fg="#888888">
              {agent.llm_provider}
              {agent.llm_model ? `/${agent.llm_model}` : ""}
            </span>
          </text>
        )}
        {agent.skills_allow && (
          <>
            <text> </text>
            <text fg="#00ff00">
              <b>Skills:</b>
            </text>
            <text fg="#888888">{formatAgentSkills(agent.skills_allow)}</text>
          </>
        )}
        {agent.policy && (
          <>
            <text> </text>
            <text fg="#ffff00">
              <b>Policy:</b>
            </text>
            <text fg="#888888">{formatAgentPolicy(agent.policy)}</text>
          </>
        )}
      </box>
    </box>
  );
}

// Full-screen message detail
interface OrchMessageDetailProps {
  message: MailboxMessage;
  onClose: () => void;
}

function OrchMessageDetail({ message, onClose }: OrchMessageDetailProps) {
  useKeyboard((e) => {
    if (e.name === "escape" || e.name === "q") {
      onClose();
    }
  });

  return (
    <box flexDirection="column" width="100%" height="100%">
      <box
        height={2}
        paddingLeft={1}
        borderStyle="single"
        borderColor="#333333"
        border={["bottom"]}
      >
        <text fg="#ffff00">
          <b>MESSAGE</b>
          <span fg="#666666"> | {message.id.slice(0, 24)}...</span>
        </text>
        <text fg="#666666">q/Esc: close</text>
      </box>
      <box padding={1} flexDirection="column">
        <box flexDirection="row">
          <text>
            <b fg="#666666">From: </b>
            <span fg="#00ffff">{message.sender}</span>
          </text>
          <text fg="#666666">{"  |  "}</text>
          <text>
            <b fg="#666666">To: </b>
            <span fg="#888888">{message.recipient}</span>
          </text>
        </box>
        <text>
          <b fg="#666666">Subject: </b>
          <span fg="#ffffff">{message.subject}</span>
        </text>
        <box flexDirection="row">
          <text>
            <b fg="#666666">Priority: </b>
            <span fg={priorityColor(message.priority)}>
              P{message.priority}
            </span>
          </text>
          <text fg="#666666">{"  |  "}</text>
          <text>
            <b fg="#666666">Kind: </b>
            <span fg="#888888">{message.kind}</span>
          </text>
          <text fg="#666666">{"  |  "}</text>
          <text>
            <b fg="#666666">Status: </b>
            <span fg={message.status === "unread" ? "#00ff00" : "#888888"}>
              {message.status}
            </span>
          </text>
        </box>
        <text>
          <b fg="#666666">Created: </b>
          <span fg="#888888">{message.created_at}</span>
        </text>
        <text> </text>
        <text fg="#aa77ff">
          <b>Body:</b>
        </text>
        <box paddingTop={1} overflow="scroll">
          {message.body ? (
            <text fg="#cccccc">{message.body}</text>
          ) : (
            <text fg="#666666">(no body)</text>
          )}
        </box>
      </box>
    </box>
  );
}

interface OrchBoardCardDetailProps {
  card: OrchestrationCard;
  runtimeLines: string[];
  runtimeError?: string;
  busy: boolean;
  onClose: () => void;
  onRefresh: () => void;
  onAction: (action: OrchestrationCardAction) => void;
  onDispatch: () => void;
}

function OrchBoardCardDetail({
  card,
  runtimeLines,
  runtimeError,
  busy,
  onClose,
  onRefresh,
  onAction,
  onDispatch,
}: OrchBoardCardDetailProps) {
  useKeyboard((e) => {
    if (e.name === "escape" || e.name === "q") {
      onClose();
      return;
    }
    if (e.name === "r") {
      onRefresh();
      return;
    }
    if (e.name === "d" && canDispatch(card)) {
      onDispatch();
      return;
    }
    if (e.name === "t" && canRetryNow(card)) {
      onAction("retry-now");
      return;
    }
    if (e.name === "u" && canRelease(card)) {
      onAction("release");
      return;
    }
    if (e.name === "m" && canMarkDone(card)) {
      onAction("mark-done");
    }
  });

  return (
    <box flexDirection="column" width="100%" height="100%">
      <box
        height={2}
        paddingLeft={1}
        borderStyle="single"
        borderColor="#333333"
        border={["bottom"]}
      >
        <text fg="#00aaff">
          <b>BOARD CARD</b>
          <span fg="#666666"> | {card.issue_identifier || card.issue_id}</span>
        </text>
        <text fg="#666666">
          q/Esc close | r refresh | d dispatch | t retry | u release | m done
        </text>
      </box>
      <box padding={1} flexDirection="column">
        <text fg="#ffffff">
          <b>{cardTitle(card)}</b>
        </text>
        <text>
          <b fg="#666666">Lane: </b>
          <span fg={laneColor(card.lane)}>{card.lane || "-"}</span>
          <span fg="#666666"> | </span>
          <b fg="#666666">State: </b>
          <span fg="#cccccc">{card.state}</span>
        </text>
        <text>
          <b fg="#666666">Policy: </b>
          <span fg="#cccccc">{card.policy_status || "-"}</span>
          <span fg="#666666"> | </span>
          <b fg="#666666">Outcome: </b>
          <span fg="#ffaa00">{card.last_outcome || "-"}</span>
        </text>
        <text>
          <b fg="#666666">Run: </b>
          <span fg="#888888">{card.run_id || "-"}</span>
        </text>
        <text>
          <b fg="#666666">Agent: </b>
          <span fg="#888888">{card.agent_id || "-"}</span>
        </text>
        {canDispatch(card) && (
          <text fg="#00ff88">Dispatchable from board detail with key d</text>
        )}
        {card.denial_reason && (
          <text>
            <b fg="#666666">Reason: </b>
            <span fg="#ffcc66">{card.denial_reason}</span>
          </text>
        )}
        {card.suggestion && (
          <text>
            <b fg="#666666">Suggestion: </b>
            <span fg="#cccccc">{card.suggestion}</span>
          </text>
        )}
        <text> </text>
        <text fg="#00ffcc">
          <b>Runtime Tree</b>
        </text>
        {runtimeError && <text fg="#ff8800">{runtimeError}</text>}
        {runtimeLines.length > 0 ? (
          runtimeLines.slice(0, 24).map((line, index) => (
            <text key={`${line}-${index}`} fg="#888888">
              {line}
            </text>
          ))
        ) : (
          <text fg="#666666">
            {card.agent_id
              ? "Runtime tree unavailable"
              : "No runtime agent attached"}
          </text>
        )}
        {busy && <text fg="#00ff00">Working…</text>}
      </box>
    </box>
  );
}

export function OrchestrationView() {
  const [panel, setPanel] = useState<
    "hierarchy" | "board" | "questions" | "activity"
  >("hierarchy");
  const [hierarchyCursor, setHierarchyCursor] = useState(0);
  const [hierarchyScroll, setHierarchyScroll] = useState(0);
  const [boardCursor, setBoardCursor] = useState(0);
  const [boardScroll, setBoardScroll] = useState(0);
  const [activityCursor, setActivityCursor] = useState(0);
  const [activityScroll, setActivityScroll] = useState(0);
  const [questionsCursor, setQuestionsCursor] = useState(0);
  const [questionsScroll, setQuestionsScroll] = useState(0);
  const [showAgentDetail, setShowAgentDetail] = useState(false);
  const [showMessageDetail, setShowMessageDetail] = useState(false);
  const [showBoardCardDetail, setShowBoardCardDetail] = useState(false);
  const [answerMode, setAnswerMode] = useState(false);
  const [answerText, setAnswerText] = useState("");
  const [activeWorkspace, setActiveWorkspace] = useState("");

  const HIERARCHY_HEIGHT = 8;
  const BOARD_HEIGHT = 7;
  const QUESTIONS_HEIGHT = 4;
  const ACTIVITY_HEIGHT = 6;
  const currentWorkspace = workspaceID();

  const {
    data: agentsData,
    isLoading: agentsLoading,
    error: agentsError,
    refetch: refetchAgents,
  } = useAgents({
    limit: 100,
  });
  const { data: workspaceData, refetch: refetchWorkspaces } = useWorkspaces();

  const workspaceOptions = useMemo(() => {
    const values = new Set<string>();
    const current = (workspaceData?.current || "").trim();
    if (current) values.add(current);
    for (const ws of workspaceData?.workspaces || []) {
      const path = (ws.path || "").trim();
      if (path) values.add(path);
    }
    if (currentWorkspace) values.add(currentWorkspace);
    return Array.from(values);
  }, [currentWorkspace, workspaceData?.current, workspaceData?.workspaces]);

  const resolvedWorkspace = useMemo(() => {
    const preferred = activeWorkspace.trim();
    if (preferred) return preferred;
    const current = (workspaceData?.current || "").trim();
    if (current) return current;
    return currentWorkspace;
  }, [activeWorkspace, currentWorkspace, workspaceData?.current]);

  const {
    data: messages,
    isLoading: messagesLoading,
    refetch: refetchMessages,
  } = useMailbox({
    limit: 50,
    workspace: resolvedWorkspace,
  });

  const {
    data: boardData,
    isLoading: boardLoading,
    error: boardError,
    refetch: refetchBoard,
  } = useOrchestrationBoard({
    workspace: resolvedWorkspace,
    limit: 50,
  });

  // Mutation hooks for agent orchestration
  const { send: sendMessage, isLoading: sendingMessage } =
    useSendMailboxMessage();
  const { acknowledge, isLoading: acknowledging } = useAcknowledgeMessage();
  const { spawn: spawnNewAgent, isLoading: spawning } = useSpawnAgent();
  const { stop: stopSelectedAgent, isLoading: stopping } = useStopAgent();
  const { apply: applyCardAction, isLoading: applyingCardAction } =
    useApplyOrchestrationCardAction();
  const { refresh: refreshBoardProjection, isLoading: refreshingBoard } =
    useRefreshOrchestration();
  const { dispatch: dispatchIssue, isLoading: dispatchingIssue } =
    useDispatchOrchestrationIssue();
  const { switchTo: switchWorkspaceTo, isLoading: switchingWorkspace } =
    useSwitchWorkspace();

  // Status message for user feedback
  const [statusMessage, setStatusMessage] = useState<string>("");

  const agents = agentsData?.agents || [];
  const tree = buildAgentTree(agents);
  const flatAgents = flattenTree(tree);
  const board = boardData?.board || null;
  const boardArtifact = boardData?.artifact || null;
  const boardRows = useMemo(() => flattenBoard(board), [board]);
  const selectedBoardRow = boardRows[boardCursor];
  const selectedBoardCard = selectedBoardRow?.card;

  const {
    data: boardCardRuntime,
    isLoading: boardCardRuntimeLoading,
    error: boardCardRuntimeError,
    refetch: refetchBoardCardRuntime,
  } = useOrchestrationCardRuntime(selectedBoardCard?.issue_id, {
    workspace: resolvedWorkspace,
    depth: 3,
  });

  // Filter pending questions (agent.ask messages to overseer that are unread/unacked)
  const pendingQuestions = useMemo(() => {
    if (!messages) return [];
    return messages.filter((msg) => {
      const isQuestion = msg.kind?.includes("ask") || msg.kind === "agent.ask";
      const toOverseer = msg.recipient === "overseer" || msg.recipient === "*";
      const isPending = msg.status === "unread" || msg.status === "pending";
      return isQuestion && toOverseer && isPending;
    });
  }, [messages]);

  const selectedQuestion = pendingQuestions[questionsCursor];

  const updateHierarchyCursor = (newCursor: number) => {
    const maxCursor = Math.max(0, flatAgents.length - 1);
    const clampedCursor = Math.min(Math.max(0, newCursor), maxCursor);
    setHierarchyCursor(clampedCursor);
    if (clampedCursor < hierarchyScroll) {
      setHierarchyScroll(clampedCursor);
    } else if (clampedCursor >= hierarchyScroll + HIERARCHY_HEIGHT) {
      setHierarchyScroll(clampedCursor - HIERARCHY_HEIGHT + 1);
    }
  };

  const updateBoardCursor = (newCursor: number) => {
    const maxCursor = Math.max(0, boardRows.length - 1);
    const clampedCursor = Math.min(Math.max(0, newCursor), maxCursor);
    setBoardCursor(clampedCursor);
    if (clampedCursor < boardScroll) {
      setBoardScroll(clampedCursor);
    } else if (clampedCursor >= boardScroll + BOARD_HEIGHT) {
      setBoardScroll(clampedCursor - BOARD_HEIGHT + 1);
    }
  };

  const updateActivityCursor = (newCursor: number) => {
    const maxCursor = Math.max(0, (messages?.length || 0) - 1);
    const clampedCursor = Math.min(Math.max(0, newCursor), maxCursor);
    setActivityCursor(clampedCursor);
    if (clampedCursor < activityScroll) {
      setActivityScroll(clampedCursor);
    } else if (clampedCursor >= activityScroll + ACTIVITY_HEIGHT) {
      setActivityScroll(clampedCursor - ACTIVITY_HEIGHT + 1);
    }
  };

  const updateQuestionsCursor = (newCursor: number) => {
    const maxCursor = Math.max(0, pendingQuestions.length - 1);
    const clampedCursor = Math.min(Math.max(0, newCursor), maxCursor);
    setQuestionsCursor(clampedCursor);
    if (clampedCursor < questionsScroll) {
      setQuestionsScroll(clampedCursor);
    } else if (clampedCursor >= questionsScroll + QUESTIONS_HEIGHT) {
      setQuestionsScroll(clampedCursor - QUESTIONS_HEIGHT + 1);
    }
  };

  const setManagedTimeout = useTimeoutManager();

  // Handle answering a question - send agent.reply message
  const handleAnswerSubmit = useCallback(async () => {
    if (!selectedQuestion || !answerText.trim()) return;
    try {
      // Send the reply to the original sender
      await sendMessage({
        recipient: selectedQuestion.sender,
        subject: `Re: ${selectedQuestion.subject}`,
        body: answerText,
        kind: "agent.reply",
        priority: selectedQuestion.priority,
        sender: "tui-user",
      });
      // Acknowledge the original question
      await acknowledge(selectedQuestion.id);
      setStatusMessage(`Answered: ${selectedQuestion.sender}`);
      setManagedTimeout(() => setStatusMessage(""), 3000);
    } catch (err) {
      setStatusMessage(
        `Error: ${err instanceof Error ? err.message : "Failed to send"}`,
      );
      setManagedTimeout(() => setStatusMessage(""), 5000);
    }
    setAnswerMode(false);
    setAnswerText("");
    refetchMessages();
  }, [selectedQuestion, answerText, sendMessage, acknowledge, refetchMessages]);

  // Handle delegating a question - mark as read and let overseer handle
  const handleDelegate = useCallback(async () => {
    if (!selectedQuestion) return;
    try {
      // Acknowledge the message (marks as handled, overseer will auto-respond)
      await acknowledge(selectedQuestion.id, "delegated");
      setStatusMessage(`Delegated to overseer: ${selectedQuestion.subject}`);
      setManagedTimeout(() => setStatusMessage(""), 3000);
    } catch (err) {
      setStatusMessage(
        `Error: ${err instanceof Error ? err.message : "Failed to delegate"}`,
      );
      setManagedTimeout(() => setStatusMessage(""), 5000);
    }
    refetchMessages();
  }, [selectedQuestion, acknowledge, refetchMessages]);

  const handleRefreshAll = useCallback(async () => {
    try {
      await refreshBoardProjection(resolvedWorkspace);
      refetchBoard();
      refetchAgents();
      refetchMessages();
      if (selectedBoardCard?.issue_id) {
        refetchBoardCardRuntime();
      }
      setStatusMessage("Refreshed orchestration surfaces");
      setManagedTimeout(() => setStatusMessage(""), 3000);
    } catch (err) {
      setStatusMessage(
        `Error: ${err instanceof Error ? err.message : "Failed to refresh"}`,
      );
      setManagedTimeout(() => setStatusMessage(""), 5000);
    }
  }, [
    refetchAgents,
    refetchBoard,
    refetchBoardCardRuntime,
    refetchMessages,
    refreshBoardProjection,
    resolvedWorkspace,
    selectedBoardCard?.issue_id,
    setManagedTimeout,
  ]);

  const handleBoardCardAction = useCallback(
    async (action: OrchestrationCardAction) => {
      if (!selectedBoardCard) return;
      try {
        await applyCardAction({
          workspace: resolvedWorkspace,
          issueID: selectedBoardCard.issue_id,
          action,
        });
        refetchBoard();
        refetchBoardCardRuntime();
        setStatusMessage(
          `${action} applied to ${selectedBoardCard.issue_identifier || selectedBoardCard.issue_id}`,
        );
        setManagedTimeout(() => setStatusMessage(""), 3000);
      } catch (err) {
        setStatusMessage(
          `Error: ${err instanceof Error ? err.message : "Failed to apply card action"}`,
        );
        setManagedTimeout(() => setStatusMessage(""), 5000);
      }
    },
    [
      applyCardAction,
      refetchBoard,
      refetchBoardCardRuntime,
      resolvedWorkspace,
      selectedBoardCard,
      setManagedTimeout,
    ],
  );

  const handleDispatchBoardCard = useCallback(async () => {
    if (!selectedBoardCard || !canDispatch(selectedBoardCard)) return;
    try {
      const result = await dispatchIssue({
        workspace: resolvedWorkspace,
        card: selectedBoardCard,
      });
      refetchBoard();
      refetchBoardCardRuntime();
      const target =
        selectedBoardCard.issue_identifier || selectedBoardCard.issue_id;
      const summary =
        [result.policy_status, result.last_outcome]
          .filter(Boolean)
          .join(" / ") || result.status;
      setStatusMessage(`Dispatch ${target}: ${summary}`);
      setManagedTimeout(() => setStatusMessage(""), 3000);
    } catch (err) {
      setStatusMessage(
        `Error: ${err instanceof Error ? err.message : "Failed to dispatch card"}`,
      );
      setManagedTimeout(() => setStatusMessage(""), 5000);
    }
  }, [
    dispatchIssue,
    refetchBoard,
    refetchBoardCardRuntime,
    resolvedWorkspace,
    selectedBoardCard,
    setManagedTimeout,
  ]);

  const handleWorkspaceCycle = useCallback(
    async (direction: 1 | -1) => {
      if (workspaceOptions.length === 0) return;
      const currentIndex = Math.max(
        0,
        workspaceOptions.indexOf(resolvedWorkspace || ""),
      );
      const nextIndex =
        (currentIndex + direction + workspaceOptions.length) %
        workspaceOptions.length;
      const nextWorkspace = workspaceOptions[nextIndex];
      if (!nextWorkspace) return;

      setActiveWorkspace(nextWorkspace);
      setBoardCursor(0);
      setBoardScroll(0);
      setQuestionsCursor(0);
      setQuestionsScroll(0);
      setActivityCursor(0);
      setActivityScroll(0);

      let switched = true;
      try {
        await switchWorkspaceTo(nextWorkspace);
      } catch {
        switched = false;
      }

      refetchWorkspaces();
      refetchBoard();
      refetchMessages();

      const name = workspaceDisplayName(nextWorkspace);
      setStatusMessage(
        switched
          ? `Workspace: ${name}`
          : `Workspace: ${name} (local view only)`,
      );
      setManagedTimeout(() => setStatusMessage(""), switched ? 2500 : 4000);
    },
    [
      refetchBoard,
      refetchMessages,
      refetchWorkspaces,
      resolvedWorkspace,
      setManagedTimeout,
      switchWorkspaceTo,
      workspaceOptions,
    ],
  );

  const selectedAgent = flatAgents[hierarchyCursor]?.agent;
  const selectedMessage = messages?.[activityCursor];

  // Handle stopping an agent
  const handleStopAgent = useCallback(async () => {
    if (!selectedAgent) return;
    try {
      await stopSelectedAgent(selectedAgent.id);
      setStatusMessage(`Stopped agent: ${selectedAgent.ns}`);
      setManagedTimeout(() => setStatusMessage(""), 3000);
      refetchAgents();
    } catch (err) {
      setStatusMessage(
        `Error: ${err instanceof Error ? err.message : "Failed to stop"}`,
      );
      setManagedTimeout(() => setStatusMessage(""), 5000);
    }
  }, [selectedAgent, stopSelectedAgent, refetchAgents]);

  useKeyboard((e) => {
    if (showAgentDetail || showMessageDetail || showBoardCardDetail) return; // Detail views handle their own keys

    // Handle answer mode input
    if (answerMode) {
      if (e.name === "escape") {
        setAnswerMode(false);
        setAnswerText("");
        return;
      }
      if (e.name === "return") {
        handleAnswerSubmit();
        return;
      }
      if (e.name === "backspace") {
        setAnswerText((t) => t.slice(0, -1));
        return;
      }
      // Handle regular characters
      if (e.raw.length === 1 && !e.ctrl && !e.meta) {
        setAnswerText((t) => t + e.raw);
        return;
      }
      return;
    }

    switch (e.name) {
      case "up":
      case "k":
        if (panel === "hierarchy") {
          updateHierarchyCursor(hierarchyCursor - 1);
        } else if (panel === "board") {
          updateBoardCursor(boardCursor - 1);
        } else if (panel === "questions") {
          updateQuestionsCursor(questionsCursor - 1);
        } else {
          updateActivityCursor(activityCursor - 1);
        }
        break;
      case "down":
      case "j":
        if (panel === "hierarchy") {
          updateHierarchyCursor(hierarchyCursor + 1);
        } else if (panel === "board") {
          updateBoardCursor(boardCursor + 1);
        } else if (panel === "questions") {
          updateQuestionsCursor(questionsCursor + 1);
        } else {
          updateActivityCursor(activityCursor + 1);
        }
        break;
      case "return":
        if (panel === "hierarchy" && selectedAgent) {
          setShowAgentDetail(true);
        } else if (panel === "board" && selectedBoardCard) {
          setShowBoardCardDetail(true);
        } else if (panel === "questions" && selectedQuestion) {
          setShowMessageDetail(true);
        } else if (panel === "activity" && selectedMessage) {
          setShowMessageDetail(true);
        }
        break;
      case "h":
        // Previous panel: activity -> questions -> board -> hierarchy
        setPanel((p) => {
          if (p === "activity") return "questions";
          if (p === "questions") return "board";
          if (p === "board") return "hierarchy";
          return "activity";
        });
        break;
      case "l":
      case "tab":
        // Next panel: hierarchy -> board -> questions -> activity
        setPanel((p) => {
          if (p === "hierarchy") return "board";
          if (p === "board") return "questions";
          if (p === "questions") return "activity";
          return "hierarchy";
        });
        break;
      case "a":
        // Answer mode (only in questions panel with a selected question)
        if (panel === "questions" && selectedQuestion) {
          setAnswerMode(true);
          setAnswerText("");
        }
        break;
      case "d":
        if (panel === "questions" && selectedQuestion) {
          handleDelegate();
        } else if (panel === "board" && canDispatch(selectedBoardCard)) {
          void handleDispatchBoardCard();
        }
        break;
      case "w":
        void handleWorkspaceCycle(1);
        break;
      case "W":
        void handleWorkspaceCycle(-1);
        break;
      case "v":
        // View question context (show full message detail)
        if (panel === "questions" && selectedQuestion) {
          setShowMessageDetail(true);
        }
        break;
      case "r":
        void handleRefreshAll();
        break;
      case "g":
        if (panel === "hierarchy") {
          updateHierarchyCursor(0);
        } else if (panel === "board") {
          updateBoardCursor(0);
        } else if (panel === "questions") {
          updateQuestionsCursor(0);
        } else {
          updateActivityCursor(0);
        }
        break;
      case "G":
        if (panel === "hierarchy") {
          updateHierarchyCursor(flatAgents.length - 1);
        } else if (panel === "board") {
          updateBoardCursor(boardRows.length - 1);
        } else if (panel === "questions") {
          updateQuestionsCursor(pendingQuestions.length - 1);
        } else {
          updateActivityCursor((messages?.length || 1) - 1);
        }
        break;
      case "x":
        // Kill/stop the selected agent (only in hierarchy panel)
        if (panel === "hierarchy" && selectedAgent) {
          handleStopAgent();
        }
        break;
      case "t":
        if (panel === "board" && canRetryNow(selectedBoardCard)) {
          void handleBoardCardAction("retry-now");
        }
        break;
      case "u":
        if (panel === "board" && canRelease(selectedBoardCard)) {
          void handleBoardCardAction("release");
        }
        break;
      case "m":
        if (panel === "board" && canMarkDone(selectedBoardCard)) {
          void handleBoardCardAction("mark-done");
        }
        break;
    }
  });

  if (
    (agentsLoading && !agentsData) ||
    (messagesLoading && !messages) ||
    (boardLoading && !boardData)
  ) {
    return (
      <box padding={1}>
        <text fg="#888888">Loading orchestration data...</text>
      </box>
    );
  }

  if (agentsError || boardError) {
    return (
      <box padding={1}>
        <text fg="#ff0000">
          Error loading orchestration data:{" "}
          {(agentsError || boardError)?.message}
        </text>
        <text fg="#666666">Press r to retry</text>
      </box>
    );
  }

  // Show agent detail when Enter is pressed on hierarchy panel
  if (showAgentDetail && selectedAgent) {
    return (
      <OrchAgentDetail
        agent={selectedAgent}
        onClose={() => setShowAgentDetail(false)}
      />
    );
  }

  // Show message detail when Enter/v is pressed (works for both activity and questions panel)
  const messageToShow =
    panel === "questions" ? selectedQuestion : selectedMessage;
  if (showMessageDetail && messageToShow) {
    return (
      <OrchMessageDetail
        message={messageToShow}
        onClose={() => setShowMessageDetail(false)}
      />
    );
  }

  if (showBoardCardDetail && selectedBoardCard) {
    return (
      <OrchBoardCardDetail
        card={boardCardRuntime?.card || selectedBoardCard}
        runtimeLines={formatRuntimeLines(boardCardRuntime?.runtime?.root)}
        runtimeError={
          boardCardRuntime?.runtime?.error || boardCardRuntimeError?.message
        }
        busy={
          applyingCardAction ||
          boardCardRuntimeLoading ||
          refreshingBoard ||
          dispatchingIssue
        }
        onClose={() => setShowBoardCardDetail(false)}
        onRefresh={() => void handleRefreshAll()}
        onAction={(action) => void handleBoardCardAction(action)}
        onDispatch={() => void handleDispatchBoardCard()}
      />
    );
  }

  return (
    <box flexDirection="column" width="100%" height="100%">
      <box height={1} paddingLeft={1}>
        <text fg="#666666">
          workspace: <span fg="#cccccc">{truncate(resolvedWorkspace, 64)}</span>
          <span fg="#666666"> ({workspaceDisplayName(resolvedWorkspace)})</span>
          <span fg="#666666">
            {" "}
            | options: {workspaceOptions.length} | w/W cycle
            {switchingWorkspace && " | switching..."}
          </span>
        </text>
      </box>

      {/* Agent Hierarchy Panel */}
      <box
        flexGrow={1}
        flexDirection="column"
        borderStyle="single"
        borderColor={panel === "hierarchy" ? "#00ff00" : "#333333"}
        border={["bottom"]}
      >
        <box height={2} paddingLeft={1} paddingTop={1}>
          <text fg={panel === "hierarchy" ? "#00ff00" : "#aa77ff"}>
            <b>AGENT HIERARCHY</b>
            <span fg="#666666"> ({agents.length} agents) | Enter: detail</span>
          </text>
        </box>
        <HierarchyPanel
          agents={agents}
          cursor={hierarchyCursor}
          scrollOffset={hierarchyScroll}
          listHeight={HIERARCHY_HEIGHT}
        />
      </box>

      {/* Board Panel */}
      <box
        height={10}
        flexDirection="column"
        borderStyle="single"
        borderColor={panel === "board" ? "#00aaff" : "#333333"}
        border={["bottom"]}
      >
        <box height={2} paddingLeft={1} paddingTop={1}>
          <text fg={panel === "board" ? "#00aaff" : "#00cccc"}>
            <b>BOARD CONTROL PLANE</b>
            <span fg="#666666">
              {" "}
              ({boardRows.length} cards)
              {selectedBoardCard &&
                " | Enter detail | d dispatch | t retry | u release | m done"}
            </span>
          </text>
        </box>
        <BoardPanel
          rows={boardRows}
          cursor={boardCursor}
          scrollOffset={boardScroll}
          listHeight={BOARD_HEIGHT}
          artifact={boardArtifact}
        />
      </box>

      {/* Pending Questions Panel */}
      <box
        height={pendingQuestions.length > 0 ? 12 : 5}
        flexDirection="column"
        borderStyle="single"
        borderColor={panel === "questions" ? "#ffaa00" : "#333333"}
        border={["bottom"]}
      >
        <box height={2} paddingLeft={1} paddingTop={1}>
          <text fg={panel === "questions" ? "#ffaa00" : "#ff8800"}>
            <b>PENDING QUESTIONS</b>
            <span fg="#666666">
              {" "}
              ({pendingQuestions.length})
              {pendingQuestions.length > 0 && " | [a]nswer [d]elegate [v]iew"}
            </span>
          </text>
        </box>
        <PendingQuestionsPanel
          questions={pendingQuestions}
          cursor={questionsCursor}
          scrollOffset={questionsScroll}
          listHeight={QUESTIONS_HEIGHT}
        />
        {/* Answer input bar */}
        {answerMode && (
          <box
            height={2}
            paddingLeft={1}
            borderStyle="single"
            borderColor="#ffaa00"
            backgroundColor="#332200"
          >
            <text fg="#ffffff">
              <span fg="#ffaa00">Answer: </span>
              {answerText}
              <span fg="#ffaa00">_</span>
            </text>
            <text fg="#666666">Enter: send | Esc: cancel</text>
          </box>
        )}
      </box>

      {/* Mailbox Activity Panel */}
      <box
        flexGrow={1}
        flexDirection="column"
        borderStyle="single"
        borderColor={panel === "activity" ? "#00ff00" : "#333333"}
        border={["top"]}
      >
        <box height={2} paddingLeft={1} paddingTop={1}>
          <text fg={panel === "activity" ? "#00ff00" : "#ffff00"}>
            <b>MAILBOX ACTIVITY</b>
            <span fg="#666666"> ({messages?.length || 0} messages)</span>
          </text>
        </box>
        <ActivityPanel
          messages={messages}
          cursor={activityCursor}
          scrollOffset={activityScroll}
          listHeight={ACTIVITY_HEIGHT}
        />
      </box>

      {/* Status message */}
      {statusMessage && (
        <box height={1} paddingLeft={1}>
          <text fg={statusMessage.startsWith("Error") ? "#ff0000" : "#00ff00"}>
            {statusMessage}
          </text>
        </box>
      )}

      {/* Controls hint */}
      <box height={1} paddingLeft={1}>
        <text fg="#666666">
          h/l/tab: switch panel | j/k: navigate | r: refresh | w/W: workspace
          {panel === "hierarchy" && " | x: stop agent"}
          {panel === "board" &&
            " | Enter: detail | d: dispatch | t: retry | u: release | m: done"}
          {panel === "questions" &&
            pendingQuestions.length > 0 &&
            " | a: answer | d: delegate"}
        </text>
      </box>
    </box>
  );
}
