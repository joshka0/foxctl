// Orchestration View - Agent hierarchy + mailbox activity + pending questions (human-in-loop)
import { useState, useCallback, useMemo } from "react";
import { useKeyboard } from "@opentui/react";
import {
  useAgents,
  useMailbox,
  useSendMailboxMessage,
  useAcknowledgeMessage,
  useSpawnAgent,
  useStopAgent,
} from "../hooks/useData";
import type { Agent, MailboxMessage } from "@agentctl/data";

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
        <span fg="#444444">{indent}{prefix}</span>
        <span fg={stateColor(agent.state)}>[{stateIcon(agent.state)}]</span>
        {" "}
        <span fg="#aa77ff">{roleIcon(agent.role)}</span>
        {" "}
        <span fg="#ffffff">{truncate(agent.role || "agent", 12).padEnd(13)}</span>
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

function HierarchyPanel({ agents, cursor, scrollOffset, listHeight }: HierarchyPanelProps) {
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
        <text fg="#555555">
          ST  R  ROLE           NAMESPACE               ID
        </text>
      </box>
      {flatList.slice(scrollOffset, scrollOffset + listHeight).map((node, i) => (
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

function ActivityPanel({ messages, cursor, scrollOffset, listHeight }: ActivityPanelProps) {
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
        <text fg="#555555">
          TIME          FROM           P   KIND         SUBJECT
        </text>
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

function PendingQuestionsPanel({ questions, cursor, scrollOffset, listHeight }: PendingQuestionsPanelProps) {
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
        <text fg="#555555">
          [a]nswer  [d]elegate  [v]iew context
        </text>
      </box>
      {questions.slice(scrollOffset, scrollOffset + listHeight).map((msg, i) => (
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
      <box height={2} paddingLeft={1} borderStyle="single" borderColor="#333333" border={["bottom"]}>
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
            <span fg="#888888">{agent.llm_provider}{agent.llm_model ? `/${agent.llm_model}` : ""}</span>
          </text>
        )}
        {agent.skills_allow && (
          <>
            <text> </text>
            <text fg="#00ff00"><b>Skills:</b></text>
            <text fg="#888888">{agent.skills_allow}</text>
          </>
        )}
        {agent.policy && (
          <>
            <text> </text>
            <text fg="#ffff00"><b>Policy:</b></text>
            <text fg="#888888">{agent.policy}</text>
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
      <box height={2} paddingLeft={1} borderStyle="single" borderColor="#333333" border={["bottom"]}>
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
            <span fg={priorityColor(message.priority)}>P{message.priority}</span>
          </text>
          <text fg="#666666">{"  |  "}</text>
          <text>
            <b fg="#666666">Kind: </b>
            <span fg="#888888">{message.kind}</span>
          </text>
          <text fg="#666666">{"  |  "}</text>
          <text>
            <b fg="#666666">Status: </b>
            <span fg={message.status === "unread" ? "#00ff00" : "#888888"}>{message.status}</span>
          </text>
        </box>
        <text>
          <b fg="#666666">Created: </b>
          <span fg="#888888">{message.created_at}</span>
        </text>
        <text> </text>
        <text fg="#aa77ff"><b>Body:</b></text>
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

export function OrchestrationView() {
  const [panel, setPanel] = useState<"hierarchy" | "questions" | "activity">("hierarchy");
  const [hierarchyCursor, setHierarchyCursor] = useState(0);
  const [hierarchyScroll, setHierarchyScroll] = useState(0);
  const [activityCursor, setActivityCursor] = useState(0);
  const [activityScroll, setActivityScroll] = useState(0);
  const [questionsCursor, setQuestionsCursor] = useState(0);
  const [questionsScroll, setQuestionsScroll] = useState(0);
  const [showAgentDetail, setShowAgentDetail] = useState(false);
  const [showMessageDetail, setShowMessageDetail] = useState(false);
  const [answerMode, setAnswerMode] = useState(false);
  const [answerText, setAnswerText] = useState("");

  const HIERARCHY_HEIGHT = 8;
  const QUESTIONS_HEIGHT = 4;
  const ACTIVITY_HEIGHT = 6;

  const { data: agentsData, isLoading: agentsLoading, error: agentsError, refetch: refetchAgents } = useAgents({
    limit: 100,
  });

  const { data: messages, isLoading: messagesLoading, refetch: refetchMessages } = useMailbox({
    limit: 50,
  });

  // Mutation hooks for agent orchestration
  const { send: sendMessage, isLoading: sendingMessage } = useSendMailboxMessage();
  const { acknowledge, isLoading: acknowledging } = useAcknowledgeMessage();
  const { spawn: spawnNewAgent, isLoading: spawning } = useSpawnAgent();
  const { stop: stopSelectedAgent, isLoading: stopping } = useStopAgent();

  // Status message for user feedback
  const [statusMessage, setStatusMessage] = useState<string>("");

  const agents = agentsData?.agents || [];
  const tree = buildAgentTree(agents);
  const flatAgents = flattenTree(tree);

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
      setTimeout(() => setStatusMessage(""), 3000);
    } catch (err) {
      setStatusMessage(`Error: ${err instanceof Error ? err.message : "Failed to send"}`);
      setTimeout(() => setStatusMessage(""), 5000);
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
      setTimeout(() => setStatusMessage(""), 3000);
    } catch (err) {
      setStatusMessage(`Error: ${err instanceof Error ? err.message : "Failed to delegate"}`);
      setTimeout(() => setStatusMessage(""), 5000);
    }
    refetchMessages();
  }, [selectedQuestion, acknowledge, refetchMessages]);

  const selectedAgent = flatAgents[hierarchyCursor]?.agent;
  const selectedMessage = messages?.[activityCursor];

  // Handle stopping an agent
  const handleStopAgent = useCallback(async () => {
    if (!selectedAgent) return;
    try {
      await stopSelectedAgent(selectedAgent.id);
      setStatusMessage(`Stopped agent: ${selectedAgent.ns}`);
      setTimeout(() => setStatusMessage(""), 3000);
      refetchAgents();
    } catch (err) {
      setStatusMessage(`Error: ${err instanceof Error ? err.message : "Failed to stop"}`);
      setTimeout(() => setStatusMessage(""), 5000);
    }
  }, [selectedAgent, stopSelectedAgent, refetchAgents]);

  useKeyboard((e) => {
    if (showAgentDetail || showMessageDetail) return; // Detail views handle their own keys

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
        } else if (panel === "questions") {
          updateQuestionsCursor(questionsCursor + 1);
        } else {
          updateActivityCursor(activityCursor + 1);
        }
        break;
      case "return":
        if (panel === "hierarchy" && selectedAgent) {
          setShowAgentDetail(true);
        } else if (panel === "questions" && selectedQuestion) {
          setShowMessageDetail(true);
        } else if (panel === "activity" && selectedMessage) {
          setShowMessageDetail(true);
        }
        break;
      case "h":
        // Previous panel: activity -> questions -> hierarchy
        setPanel((p) => {
          if (p === "activity") return "questions";
          if (p === "questions") return "hierarchy";
          return "activity";
        });
        break;
      case "l":
      case "tab":
        // Next panel: hierarchy -> questions -> activity
        setPanel((p) => {
          if (p === "hierarchy") return "questions";
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
        // Delegate question
        if (panel === "questions" && selectedQuestion) {
          handleDelegate();
        }
        break;
      case "v":
        // View question context (show full message detail)
        if (panel === "questions" && selectedQuestion) {
          setShowMessageDetail(true);
        }
        break;
      case "r":
        refetchAgents();
        refetchMessages();
        break;
      case "g":
        if (panel === "hierarchy") {
          updateHierarchyCursor(0);
        } else if (panel === "questions") {
          updateQuestionsCursor(0);
        } else {
          updateActivityCursor(0);
        }
        break;
      case "G":
        if (panel === "hierarchy") {
          updateHierarchyCursor(flatAgents.length - 1);
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
    }
  });

  if ((agentsLoading && !agentsData) || (messagesLoading && !messages)) {
    return (
      <box padding={1}>
        <text fg="#888888">Loading orchestration data...</text>
      </box>
    );
  }

  if (agentsError) {
    return (
      <box padding={1}>
        <text fg="#ff0000">Error loading agents: {agentsError.message}</text>
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
  const messageToShow = panel === "questions" ? selectedQuestion : selectedMessage;
  if (showMessageDetail && messageToShow) {
    return (
      <OrchMessageDetail
        message={messageToShow}
        onClose={() => setShowMessageDetail(false)}
      />
    );
  }

  return (
    <box flexDirection="column" width="100%" height="100%">
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
              {" "}({pendingQuestions.length})
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
          <box height={2} paddingLeft={1} borderStyle="single" borderColor="#ffaa00" backgroundColor="#332200">
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
          h/l/tab: switch panel | j/k: navigate | r: refresh | x: stop agent
          {panel === "questions" && pendingQuestions.length > 0 && " | a: answer, d: delegate"}
        </text>
      </box>
    </box>
  );
}
