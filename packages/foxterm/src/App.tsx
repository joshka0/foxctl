import { useEffect, useMemo, useState } from "react";
import { useKeyboard, useTerminalDimensions } from "@opentui/react";

import type {
  OrchestrationCard,
  OrchestrationCardAction,
  OrchestrationRuntimeTree,
  OrchestrationRuntimeTreeNode,
  V2RunTranscriptItem,
  V2RuntimeEvent,
} from "@foxctl/data/types";
import {
  ACTOR_ID,
  ATCP_DAEMON_HINT,
  applyOrchestrationCardAction,
  createRoom,
  createRun,
  getAgents,
  getATCPRoomSessions,
  getOrchestrationCardRuntime,
  getOrchestrationCardWork,
  getRoomLoop,
  getRoomMessages,
  getRoomTaskWork,
  getRunTranscript,
  getRuns,
  getV2ModelEndpoint,
  killRun,
  sendRoomMessage,
  sendATCPMessageToRoom,
  seedOrchestrationCard,
  spawnAgent,
  spawnATCPCLIForRoom,
  stopATCPSessionForRoom,
  subscribeToV2Stream,
  WORKSPACE_ID,
  WORKSPACE_ROOT,
  type AgentSummary,
  type ATCPMember,
  type ATCPReadiness,
  type ATCPScreen,
  type ATCPSession,
  type ApplyOrchestrationCardActionInput,
  type GetOrchestrationCardRuntimeInput,
  type OrchestrationCardWorkItem,
  type RoomLoop,
  type RoomMessage,
  type RoomTaskWorkItem,
  type RunListItem,
  type SeedOrchestrationCardInput,
  type V2ModelEndpoint,
} from "./api";
import { HelpOverlay } from "./components/HelpOverlay";
import { Panel, PanelState } from "./components/Panel";
import { AppFrame, MainRegion, SmallTerminalNotice } from "./components/Shell";
import { GroupedWorklist, type WorklistSection } from "./components/Worklist";
import { theme, toneColor, type Tone } from "./theme";
import type {
  ActivityScope,
  FocusRegion,
  LoadState,
  Mode,
  StatusMessage,
} from "./types";

const navItems = [
  { id: "runs", label: "Runs", hint: "v2 runtime" },
  { id: "rooms", label: "Rooms", hint: "control snapshots" },
  { id: "cards", label: "Cards", hint: "orchestration board" },
] as const;

const importantEventTypes = new Set([
  "run.completed",
  "run.failed",
  "stage.failed",
  "artifact.failed",
  "orchestration.updated",
]);

const nonKillableRunStatuses = new Set([
  "completed",
  "failed",
  "killed",
  "succeeded",
  "cancelled",
  "canceled",
]);

interface AppProps {
  onExit: () => void;
}

interface RunWorklistItem {
  id: string;
  run: RunListItem;
}

interface CompactEventLine {
  key: string;
  tone: Tone;
  text: string;
  repeat: number;
}

interface RunOutputSummary {
  summary: string;
  turnID?: string;
  iterations?: number;
  toolCalls?: number;
}

interface CLISpawnDraft {
  agentId: string;
  adapter: string;
  cmd: string[];
}

interface CLIPreset {
  id: string;
  label: string;
  agentId: string;
  adapter: string;
  cmd: string[];
}

interface CLISpawnForm {
  agentId: string;
  adapter: string;
  command: string;
  raw: string;
}

interface ATCPStopTarget {
  roomId: string;
  sessionId: string;
  agentId?: string;
}

interface PendingCardAction {
  issueId: string;
  title: string;
  action: OrchestrationCardAction;
}

interface CommandPaletteAction {
  id: string;
  title: string;
  category: string;
  command: string;
  description: string;
  destructive: boolean;
  disabled?: boolean;
  disabledReason?: string;
  run: () => void;
}

type AgentPaneSize = "small" | "medium" | "large";
type MainPaneSize = "small" | "medium" | "large";
type PanelWidth = number | "100%";

type ComposerTarget =
  | { kind: "new" }
  | { kind: "continue"; runId: string }
  | { kind: "context"; source: string };

const cliPresets: CLIPreset[] = [
  {
    id: "codex",
    label: "Codex",
    agentId: "codex-a",
    adapter: "codex",
    cmd: ["codex", "--no-alt-screen"],
  },
  {
    id: "claude",
    label: "Claude",
    agentId: "claude-a",
    adapter: "claude",
    cmd: ["claude"],
  },
  {
    id: "droid",
    label: "Droid",
    agentId: "droid-a",
    adapter: "droid",
    cmd: ["droid"],
  },
  {
    id: "gemini",
    label: "Gemini",
    agentId: "gemini-a",
    adapter: "gemini",
    cmd: ["gemini"],
  },
  {
    id: "shell",
    label: "Shell",
    agentId: "shell-a",
    adapter: "shell",
    cmd: ["bash"],
  },
  {
    id: "custom",
    label: "Custom",
    agentId: "custom-a",
    adapter: "custom",
    cmd: ["bash"],
  },
];

export function App({ onExit }: AppProps) {
  const { width, height } = useTerminalDimensions();
  const short = height < 28;
  const compact = width < 120 || short;
  const focusOnly = compact && short;
  const tooSmall = width < 60 || height < 18;
  const [mode, setMode] = useState<Mode>("normal");
  const [focus, setFocus] = useState<FocusRegion>("worklist");
  const [paletteText, setPaletteText] = useState("");
  const [paletteIndex, setPaletteIndex] = useState(0);
  const [filterText, setFilterText] = useState("");
  const [activityScope, setActivityScope] =
    useState<ActivityScope>("focused");
  const [scopeCollapsed, setScopeCollapsed] = useState(false);
  const [worklistCollapsed, setWorklistCollapsed] = useState(false);
  const [scopePaneSize, setScopePaneSize] =
    useState<MainPaneSize>("medium");
  const [worklistPaneSize, setWorklistPaneSize] =
    useState<MainPaneSize>("medium");
  const [composerText, setComposerText] = useState("");
  const [composerTarget, setComposerTarget] = useState<ComposerTarget>({
    kind: "new",
  });
  const [composerBusy, setComposerBusy] = useState(false);
  const [roomComposerText, setRoomComposerText] = useState("");
  const [roomCreateBusy, setRoomCreateBusy] = useState(false);
  const [roomMessageText, setRoomMessageText] = useState("");
  const [roomMessageBusy, setRoomMessageBusy] = useState(false);
  const [roomMessageRoomId, setRoomMessageRoomId] = useState<string | null>(
    null,
  );
  const [atcpPromptText, setATCPPromptText] = useState("");
  const [atcpPromptBusy, setATCPPromptBusy] = useState(false);
  const [atcpPromptRoomId, setATCPPromptRoomId] = useState<string | null>(null);
  const [agentSpawnText, setAgentSpawnText] = useState("researcher");
  const [agentSpawnBusy, setAgentSpawnBusy] = useState(false);
  const [agentSpawnRoomId, setAgentSpawnRoomId] = useState<string | null>(null);
  const [cliSpawnStep, setCLISpawnStep] = useState<"preset" | "fields">(
    "preset",
  );
  const [cliPresetIndex, setCLIPresetIndex] = useState(0);
  const [cliSpawnField, setCLISpawnField] =
    useState<keyof CLISpawnForm>("agentId");
  const [cliSpawnForm, setCLISpawnForm] = useState<CLISpawnForm>(
    formFromCLIPreset(cliPresets[0]),
  );
  const [cliSpawnBusy, setCLISpawnBusy] = useState(false);
  const [cliSpawnRoomId, setCLISpawnRoomId] = useState<string | null>(null);
  const [busyTick, setBusyTick] = useState(0);
  const [modelEndpoint, setModelEndpoint] = useState<V2ModelEndpoint | null>(
    null,
  );
  const [pendingKillRunId, setPendingKillRunId] = useState<string | null>(null);
  const [killBusy, setKillBusy] = useState(false);
  const [pendingATCPStop, setPendingATCPStop] =
    useState<ATCPStopTarget | null>(null);
  const [atcpStopBusy, setATCPStopBusy] = useState(false);
  const [agentPaneSize, setAgentPaneSize] =
    useState<AgentPaneSize>("medium");
  const [navIndex, setNavIndex] = useState(0);
  const [selectedRunId, setSelectedRunId] = useState<string | null>(null);
  const [runs, setRuns] = useState<RunListItem[]>([]);
  const [runLoadState, setRunLoadState] = useState<LoadState>("idle");
  const [lastRunLoadAt, setLastRunLoadAt] = useState<string | null>(null);
  const [transcriptItems, setTranscriptItems] = useState<
    V2RunTranscriptItem[]
  >([]);
  const [transcriptLoadState, setTranscriptLoadState] =
    useState<LoadState>("idle");
  const [roomTaskItems, setRoomTaskItems] = useState<RoomTaskWorkItem[]>([]);
  const [roomCount, setRoomCount] = useState(0);
  const [selectedRoomTaskId, setSelectedRoomTaskId] = useState<string | null>(
    null,
  );
  const [roomTaskLoadState, setRoomTaskLoadState] =
    useState<LoadState>("idle");
  const [lastRoomTaskLoadAt, setLastRoomTaskLoadAt] = useState<string | null>(
    null,
  );
  const [cardItems, setCardItems] = useState<OrchestrationCardWorkItem[]>([]);
  const [cardArtifact, setCardArtifact] = useState<string | null>(null);
  const [selectedCardId, setSelectedCardId] = useState<string | null>(null);
  const [cardLoadState, setCardLoadState] = useState<LoadState>("idle");
  const [lastCardLoadAt, setLastCardLoadAt] = useState<string | null>(null);
  const [cardComposerText, setCardComposerText] = useState("");
  const [cardCreateBusy, setCardCreateBusy] = useState(false);
  const [pendingCardAction, setPendingCardAction] =
    useState<PendingCardAction | null>(null);
  const [cardActionBusy, setCardActionBusy] = useState(false);
  const [cardRuntime, setCardRuntime] = useState<OrchestrationRuntimeTree | null>(
    null,
  );
  const [cardRuntimeLoadState, setCardRuntimeLoadState] =
    useState<LoadState>("idle");
  const [cardRuntimeError, setCardRuntimeError] = useState<string | null>(null);
  const [events, setEvents] = useState<V2RuntimeEvent[]>([]);
  const [streamStatus, setStreamStatus] = useState<StatusMessage>({
    tone: "muted",
    text: "idle",
  });
  const [status, setStatus] = useState<StatusMessage>({
    tone: "muted",
    text: "ready",
  });

  const filteredRuns = useMemo(
    () => runs.filter((run) => matchesRunFilter(run, filterText)),
    [runs, filterText],
  );
  const runSections = useMemo(
    () => buildRunSections(filteredRuns),
    [filteredRuns],
  );
  const selectableRuns = useMemo(
    () => runSections.flatMap((section) => section.items),
    [runSections],
  );
  const selectedRunItem =
    selectedRunId === null
      ? undefined
      : selectableRuns.find((item) => item.id === selectedRunId);
  const selectedRun = selectedRunItem?.run;
  const selectedRunMissing =
    selectedRunId !== null && selectedRun === undefined && runs.length > 0;
  const activeNav = navItems[navIndex] ?? navItems[0];
  const activeView = activeNav.id;
  const scopeVisible = !compact && !scopeCollapsed;
  const worklistVisible = !worklistCollapsed;
  const showWorklistRegion = worklistVisible && (!focusOnly || focus === "worklist");
  const showDetailRegion = !focusOnly || focus !== "worklist" || !worklistVisible;
  const scopeWidth = mainScopePaneWidth(scopePaneSize);
  const worklistWidth = mainWorklistPaneWidth(compact, worklistPaneSize);
  const filteredRoomTaskItems = useMemo(
    () =>
      roomTaskItems.filter((item) => matchesRoomTaskFilter(item, filterText)),
    [roomTaskItems, filterText],
  );
  const roomTaskSections = useMemo(
    () => buildRoomTaskSections(filteredRoomTaskItems),
    [filteredRoomTaskItems],
  );
  const selectableRoomTasks = useMemo(
    () => roomTaskSections.flatMap((section) => section.items),
    [roomTaskSections],
  );
  const selectedRoomTaskItem =
    selectedRoomTaskId === null
      ? undefined
      : selectableRoomTasks.find((item) => item.id === selectedRoomTaskId);
  const selectedRoom = selectedRoomTaskItem?.room;
  const selectedRoomTaskMissing =
    selectedRoomTaskId !== null &&
    selectedRoomTaskItem === undefined &&
    roomTaskItems.length > 0;
  const [roomMessages, setRoomMessages] = useState<RoomMessage[]>([]);
  const [roomMessagesLoadState, setRoomMessagesLoadState] =
    useState<LoadState>("idle");
  const [roomLoop, setRoomLoop] = useState<RoomLoop | null>(null);
  const [roomLoopLoadState, setRoomLoopLoadState] =
    useState<LoadState>("idle");
  const [agents, setAgents] = useState<AgentSummary[]>([]);
  const [agentLoadState, setAgentLoadState] = useState<LoadState>("idle");
  const [atcpSessions, setATCPSessions] = useState<ATCPSession[]>([]);
  const [atcpMembers, setATCPMembers] = useState<ATCPMember[]>([]);
  const [atcpReadiness, setATCPReadiness] = useState<
    Record<string, ATCPReadiness>
  >({});
  const [atcpScreens, setATCPScreens] = useState<Record<string, ATCPScreen>>(
    {},
  );
  const [atcpLoadState, setATCPLoadState] = useState<LoadState>("idle");
  const [selectedATCPSessionId, setSelectedATCPSessionId] = useState<
    string | null
  >(null);
  const selectedATCPSession =
    atcpSessions.find((item) => item.id === selectedATCPSessionId) ??
    atcpSessions[0];
  const selectedATCPMember = selectedATCPSession
    ? atcpMembers.find((item) => item.session_id === selectedATCPSession.id)
    : undefined;
  const selectedATCPAgentLabel =
    selectedATCPMember?.agent_id ?? selectedATCPSession?.id ?? null;
  const filteredCardItems = useMemo(
    () => cardItems.filter((item) => matchesCardFilter(item, filterText)),
    [cardItems, filterText],
  );
  const cardSections = useMemo(
    () => buildCardSections(filteredCardItems),
    [filteredCardItems],
  );
  const selectableCards = useMemo(
    () => cardSections.flatMap((section) => section.items),
    [cardSections],
  );
  const selectedCardItem =
    selectedCardId === null
      ? undefined
      : selectableCards.find((item) => item.id === selectedCardId);
  const selectedCardMissing =
    selectedCardId !== null &&
    selectedCardItem === undefined &&
    cardItems.length > 0;

  const refreshRuns = async (preferredRunId?: string) => {
    const hadRuns = runs.length > 0;
    setRunLoadState("loading");
    setStatus({ tone: "focus", text: "refreshing runs" });
    try {
      const result = await getRuns({ limit: 50 });
      setRuns(result.items);
      setSelectedRunId(
        (current) => preferredRunId ?? current ?? result.items[0]?.run_id ?? null,
      );
      setRunLoadState("ready");
      setLastRunLoadAt(new Date().toLocaleTimeString());
      setStatus({
        tone: "success",
        text: result.count === 0 ? "no runs" : `${result.count} runs loaded`,
      });
    } catch (error) {
      setRunLoadState("error");
      setStatus({
        tone: hadRuns ? "warning" : "danger",
        text: error instanceof Error ? error.message : "failed to load runs",
      });
    }
  };

  const submitComposedRun = async () => {
    const prompt = composerText.trim();
    if (prompt === "" || composerBusy) return;
    const target = composerTarget;
    setComposerBusy(true);
    setStatus({
      tone: "focus",
      text: target.kind === "continue" ? "sending follow-up" : "running prompt",
    });
    try {
      const result = await createRun({
        prompt,
        runId: target.kind === "continue" ? target.runId : undefined,
        profile: "worker",
        maxIterations: 1,
        async: true,
      });
      setComposerText("");
      setMode("normal");
      setFocus("detail");
      if (!result.output) {
        const optimisticRun: RunListItem = {
          run_id: result.run_id,
          status: result.status === "started" ? "running" : result.status ?? "running",
          command: "run",
          request_id: result.request_id,
          actor_id: "actor:web",
          updated_at: new Date().toISOString(),
        };
        setRuns((current) =>
          current.some((run) => run.run_id === result.run_id)
            ? current.map((run) =>
                run.run_id === result.run_id
                  ? { ...run, status: "running", updated_at: optimisticRun.updated_at }
                  : run,
              )
            : [optimisticRun, ...current],
        );
        setSelectedRunId(result.run_id);
        setStatus({
          tone: "success",
          text:
            target.kind === "continue"
              ? `follow-up sent to ${result.run_id}`
              : target.kind === "context"
                ? `context run ${result.run_id} started`
              : `run ${result.run_id} started`,
        });
        setTimeout(() => void refreshRuns(result.run_id), 500);
        return;
      }
      await refreshRuns(result.run_id);
      setStatus({
        tone: result.output?.degraded ? "warning" : "success",
        text:
          target.kind === "continue"
            ? `follow-up completed on ${result.run_id}`
            : target.kind === "context"
              ? `context run ${result.run_id} completed`
            : `run ${result.run_id} completed`,
      });
    } catch (error) {
      setStatus({
        tone: "danger",
        text: error instanceof Error ? error.message : "failed to create run",
      });
    } finally {
      setComposerBusy(false);
    }
  };

  const openComposer = (target: ComposerTarget) => {
    if (target.kind === "continue" && !selectedRun) {
      setStatus({ tone: "muted", text: "select a run first" });
      return;
    }
    setComposerTarget(target);
    setMode("compose");
    setFocus("composer");
  };

  const openRoomComposer = () => {
    setMode("createRoom");
    setFocus("composer");
  };

  const openCardComposer = () => {
    setMode("createCard");
    setFocus("composer");
  };

  const submitRoomCreate = async () => {
    const title = roomComposerText.trim();
    if (title === "" || roomCreateBusy) return;
    setRoomCreateBusy(true);
    setStatus({ tone: "focus", text: "creating room" });
    try {
      const result = await createRoom({
        title,
        workspaceId: WORKSPACE_ID,
      });
      setRoomComposerText("");
      setMode("normal");
      setFocus("detail");
      await refreshRoomTasks(`room:${result.room.id}`);
      setStatus({
        tone: "success",
        text: `room ${result.room.id} created`,
      });
    } catch (error) {
      setStatus({
        tone: "danger",
        text: error instanceof Error ? error.message : "failed to create room",
      });
    } finally {
      setRoomCreateBusy(false);
    }
  };

  const submitCardCreate = async () => {
    const title = cardComposerText.trim();
    if (title === "" || cardCreateBusy) return;
    setCardCreateBusy(true);
    setStatus({ tone: "focus", text: "creating card" });
    try {
      const input: SeedOrchestrationCardInput = {
        workspaceId: WORKSPACE_ID,
        title,
      };
      const result = await seedOrchestrationCard(input);
      setCardComposerText("");
      setMode("normal");
      setFocus("worklist");
      await refreshCards();
      setStatus({
        tone: result.created > 0 ? "success" : "warning",
        text:
          result.created > 0
            ? `${result.created} card created`
            : "card was skipped",
      });
    } catch (error) {
      setStatus({
        tone: "danger",
        text: error instanceof Error ? error.message : "failed to create card",
      });
    } finally {
      setCardCreateBusy(false);
    }
  };

  const openRoomMessageComposer = () => {
    const room = selectedRoom ?? roomTaskItems[0]?.room;
    if (!room) {
      setStatus({ tone: "muted", text: "create or select a room first" });
      return;
    }
    setRoomMessageRoomId(room.id);
    setMode("roomMessage");
    setFocus("composer");
  };

  const openAgentSpawnComposer = () => {
    const room = selectedRoom ?? roomTaskItems[0]?.room;
    if (!room) {
      setStatus({ tone: "muted", text: "create or select a room first" });
      return;
    }
    setAgentSpawnRoomId(room.id);
    setAgentSpawnText((current) => current || "researcher");
    setMode("spawnAgent");
    setFocus("composer");
  };

  const openCLISpawnComposer = () => {
    const room = selectedRoom ?? roomTaskItems[0]?.room;
    if (!room) {
      setStatus({ tone: "muted", text: "create or select a room first" });
      return;
    }
    setCLISpawnRoomId(room.id);
    setCLISpawnStep("preset");
    setCLIPresetIndex(0);
    setCLISpawnField("agentId");
    setCLISpawnForm(formFromCLIPreset(cliPresets[0]));
    setMode("spawnCLI");
    setFocus("composer");
  };

  const openATCPPromptComposer = () => {
    const room = selectedRoom ?? roomTaskItems[0]?.room;
    if (!room) {
      setStatus({ tone: "muted", text: "create or select a room first" });
      return;
    }
    if (atcpSessions.length === 0) {
      setStatus({ tone: "muted", text: "no ATCP sessions" });
      return;
    }
    setATCPPromptRoomId(room.id);
    setMode("atcpPrompt");
    setFocus("composer");
  };

  const resetCLISpawnState = () => {
    setCLISpawnStep("preset");
    setCLIPresetIndex(0);
    setCLISpawnField("agentId");
    setCLISpawnForm(formFromCLIPreset(cliPresets[0]));
  };

  const submitAgentSpawn = async () => {
    const roomId = agentSpawnRoomId ?? selectedRoom?.id;
    const draft = parseAgentSpawnDraft(agentSpawnText);
    if (!roomId || draft.role === "" || agentSpawnBusy) return;
    setAgentSpawnBusy(true);
    setStatus({ tone: "focus", text: `spawning ${draft.role}` });
    try {
      const result = await spawnAgent({
        role: draft.role,
        roomRole: draft.role,
        prompt: draft.prompt,
        roomId,
        workspaceId: WORKSPACE_ID,
        workspaceRoot: WORKSPACE_ROOT,
        execMode: "reactive",
        maxIterations: 10,
        maxAutoTurns: 1,
      });
      setAgentSpawnText("researcher");
      setAgentSpawnRoomId(null);
      setMode("normal");
      setFocus("detail");
      await refreshRoomTasks(`room:${roomId}`);
      await refreshRoomMessages(roomId);
      await refreshRoomLoop(roomId);
      await refreshAgents();
      setStatus({
        tone: "success",
        text: `agent ${result.actor_id} ${result.status}`,
      });
    } catch (error) {
      setStatus({
        tone: "danger",
        text: error instanceof Error ? error.message : "failed to spawn agent",
      });
    } finally {
      setAgentSpawnBusy(false);
    }
  };

  const submitCLISpawn = async () => {
    const roomId = cliSpawnRoomId ?? selectedRoom?.id;
    if (!roomId || cliSpawnBusy) return;
    let draft: CLISpawnDraft;
    try {
      draft =
        cliSpawnStep === "preset"
          ? draftFromCLIPreset(cliPresets[cliPresetIndex])
          : draftFromCLIForm(cliSpawnForm);
    } catch (error) {
      setStatus({
        tone: "danger",
        text:
          error instanceof Error ? error.message : "invalid CLI spawn draft",
      });
      return;
    }
    setCLISpawnBusy(true);
    setStatus({ tone: "focus", text: `starting ${draft.agentId}` });
    try {
      const result = await spawnATCPCLIForRoom({
        roomId,
        workspaceId: WORKSPACE_ID,
        agentId: draft.agentId,
        adapter: draft.adapter,
        cmd: draft.cmd,
        cwd: WORKSPACE_ROOT,
        role: draft.adapter,
        canMutate: true,
      });
      resetCLISpawnState();
      setCLISpawnRoomId(null);
      setMode("normal");
      setFocus("detail");
      await refreshATCPRoomSessions(roomId);
      setStatus({
        tone: "success",
        text: `ATCP ${result.member.agent_id} session ${result.session.status}`,
      });
    } catch (error) {
      setStatus({
        tone: "danger",
        text: atcpErrorText(error, "failed to spawn ATCP CLI session"),
      });
    } finally {
      setCLISpawnBusy(false);
    }
  };

  const submitATCPPrompt = async () => {
    const roomId = atcpPromptRoomId ?? selectedRoom?.id;
    const text = atcpPromptText.trim();
    const session =
      atcpSessions.find((item) => item.id === selectedATCPSessionId) ??
      atcpSessions[0];
    const member = session
      ? atcpMembers.find((item) => item.session_id === session.id)
      : undefined;
    if (!roomId || !session || !member || text === "" || atcpPromptBusy) {
      return;
    }
    setATCPPromptBusy(true);
    setStatus({ tone: "focus", text: `sending to ${member.agent_id}` });
    try {
      const result = await sendATCPMessageToRoom({
        roomId,
        workspaceId: WORKSPACE_ID,
        source: ACTOR_ID,
        targetAgentId: member.agent_id,
        text,
        awaitActivityMs: 500,
        awaitReadyMs: 500,
      });
      setATCPPromptText("");
      setATCPPromptRoomId(null);
      setMode("normal");
      setFocus("detail");
      await refreshATCPRoomSessions(roomId);
      setStatus({
        tone: result.message.failed > 0 ? "warning" : "success",
        text: `ATCP delivered ${result.message.delivered}/${result.message.delivered + result.message.failed}`,
      });
    } catch (error) {
      setStatus({
        tone: "danger",
        text: atcpErrorText(error, "failed to send ATCP prompt"),
      });
    } finally {
      setATCPPromptBusy(false);
    }
  };

  const submitRoomMessage = async () => {
    const body = roomMessageText.trim();
    const roomId = roomMessageRoomId ?? selectedRoom?.id;
    if (!roomId || body === "" || roomMessageBusy) return;
    setRoomMessageBusy(true);
    setStatus({ tone: "focus", text: "sending room message" });
    try {
      const result = await sendRoomMessage({
        roomId,
        body,
        workspaceId: WORKSPACE_ID,
        sender: ACTOR_ID,
        subject: selectedRoom?.title || roomId,
        taskId: selectedRoomTaskItem?.task?.id,
      });
      setRoomMessageText("");
      setRoomMessageRoomId(null);
      setMode("normal");
      setFocus("detail");
      await refreshRoomMessages(roomId);
      await refreshRoomTasks(`room:${roomId}`);
      setStatus({
        tone: result.delivery_pending ? "focus" : "success",
        text: `message ${result.status} in ${roomId}`,
      });
    } catch (error) {
      setStatus({
        tone: "danger",
        text: roomMessageErrorText(error, roomId),
      });
    } finally {
      setRoomMessageBusy(false);
    }
  };

  const jumpToRun = (runId: string) => {
    const trimmed = runId.trim();
    if (trimmed === "") return;
    setNavIndex(0);
    setSelectedRunId(trimmed);
    setFocus("detail");
    setStatus({ tone: "focus", text: `selected run ${trimmed}` });
    if (!runs.some((run) => run.run_id === trimmed)) {
      setRuns((current) => [
        {
          run_id: trimmed,
          status: "unknown",
          command: "run",
          updated_at: new Date().toISOString(),
        },
        ...current,
      ]);
      setTimeout(() => void refreshRuns(trimmed), 250);
    }
  };

  const refreshRunTranscript = async (runId: string) => {
    setTranscriptLoadState("loading");
    try {
      const transcript = await getRunTranscript(runId);
      setTranscriptItems(transcript.items);
      setTranscriptLoadState("ready");
    } catch {
      setTranscriptLoadState("error");
    }
  };

  const requestKillSelectedRun = () => {
    if (!selectedRun) {
      setStatus({ tone: "muted", text: "select a run first" });
      return;
    }
    if (!canKillRunStatus(selectedRun.status)) {
      setStatus({ tone: "muted", text: `run is ${selectedRun.status}` });
      return;
    }
    setPendingKillRunId(selectedRun.run_id);
    setMode("confirmKill");
    setFocus("detail");
  };

  const confirmKillRun = async () => {
    const runId = pendingKillRunId;
    if (!runId || killBusy) return;
    setKillBusy(true);
    setStatus({ tone: "warning", text: `killing ${runId}` });
    try {
      const result = await killRun(runId);
      setPendingKillRunId(null);
      setMode("normal");
      await refreshRuns(result.run_id);
      setStatus({
        tone: result.status === "killed" ? "warning" : "muted",
        text: `${result.run_id} ${result.status}`,
      });
    } catch (error) {
      setStatus({
        tone: "danger",
        text: error instanceof Error ? error.message : "failed to kill run",
      });
    } finally {
      setKillBusy(false);
    }
  };

  const requestStopSelectedATCPSession = () => {
    const room = selectedRoom ?? roomTaskItems[0]?.room;
    if (!room) {
      setStatus({ tone: "muted", text: "select a room first" });
      return;
    }
    if (!selectedATCPSession) {
      setStatus({ tone: "muted", text: "no ATCP sessions" });
      return;
    }
    setPendingATCPStop({
      roomId: room.id,
      sessionId: selectedATCPSession.id,
      agentId: selectedATCPMember?.agent_id,
    });
    setMode("confirmATCPStop");
    setFocus("detail");
  };

  const confirmStopATCPSession = async () => {
    const target = pendingATCPStop;
    if (!target || atcpStopBusy) return;
    setATCPStopBusy(true);
    setStatus({
      tone: "warning",
      text: `stopping ${target.agentId ?? target.sessionId}`,
    });
    try {
      const result = await stopATCPSessionForRoom({
        roomId: target.roomId,
        sessionId: target.sessionId,
        workspaceId: WORKSPACE_ID,
      });
      setPendingATCPStop(null);
      setMode("normal");
      await refreshATCPRoomSessions(target.roomId);
      setStatus({
        tone: "warning",
        text: `${result.agent_id ?? result.session_id} ${result.status}`,
      });
    } catch (error) {
      setStatus({
        tone: "danger",
        text: atcpErrorText(error, "failed to stop ATCP session"),
      });
    } finally {
      setATCPStopBusy(false);
    }
  };

  const requestSelectedCardAction = (action: OrchestrationCardAction) => {
    if (!selectedCardItem) {
      setStatus({ tone: "muted", text: "select a card first" });
      return;
    }
    const availability = cardActionAvailability(selectedCardItem.card, action);
    if (!availability.available) {
      setStatus({ tone: "muted", text: availability.reason });
      return;
    }
    setPendingCardAction({
      issueId: selectedCardItem.card.issue_id,
      title:
        selectedCardItem.card.title ||
        selectedCardItem.card.issue_identifier ||
        selectedCardItem.card.issue_id,
      action,
    });
    setMode("confirmCardAction");
    setFocus("detail");
  };

  const confirmCardAction = async () => {
    const target = pendingCardAction;
    if (!target || cardActionBusy) return;
    setCardActionBusy(true);
    setStatus({
      tone: "focus",
      text: `${cardActionLabel(target.action)} ${target.issueId}`,
    });
    try {
      const input: ApplyOrchestrationCardActionInput = {
        workspaceId: WORKSPACE_ID,
        issueId: target.issueId,
        action: target.action,
      };
      const result = await applyOrchestrationCardAction(input);
      setPendingCardAction(null);
      setMode("normal");
      const updatedItem = cardWorkItemFromCard(result.card, selectedCardItem);
      setCardItems((current) =>
        current.some((item) => item.card.issue_id === result.card.issue_id)
          ? current.map((item) =>
              item.card.issue_id === result.card.issue_id ? updatedItem : item,
            )
          : [updatedItem, ...current],
      );
      setSelectedCardId(result.card.issue_id);
      setStatus({
        tone: "success",
        text: `${result.card.issue_id} ${cardActionDoneLabel(result.action)}`,
      });
    } catch (error) {
      setStatus({
        tone: "danger",
        text:
          error instanceof Error
            ? error.message
            : "failed to update card",
      });
    } finally {
      setCardActionBusy(false);
    }
  };

  const refreshRoomTasks = async (preferredItemId?: string) => {
    const hadItems = roomTaskItems.length > 0;
    setRoomTaskLoadState("loading");
    setStatus({ tone: "focus", text: "refreshing room tasks" });
    try {
      const result = await getRoomTaskWork({
        workspaceId: WORKSPACE_ID,
        roomLimit: 25,
        taskLimit: 100,
      });
      setRoomCount(result.rooms.length);
      setRoomTaskItems(result.items);
      setSelectedRoomTaskId(
        (current) => preferredItemId ?? current ?? result.items[0]?.id ?? null,
      );
      setRoomTaskLoadState("ready");
      setLastRoomTaskLoadAt(new Date().toLocaleTimeString());
      setStatus({
        tone: "success",
        text: roomTaskStatus(result.rooms.length, countRoomTasks(result.items)),
      });
    } catch (error) {
      setRoomTaskLoadState("error");
      setStatus({
        tone: hadItems ? "warning" : "danger",
        text:
          error instanceof Error ? error.message : "failed to load room tasks",
      });
    }
  };

  const refreshRoomMessages = async (roomId: string) => {
    const trimmed = roomId.trim();
    if (trimmed === "") {
      setRoomMessages([]);
      setRoomMessagesLoadState("idle");
      return;
    }
    setRoomMessagesLoadState("loading");
    try {
      const result = await getRoomMessages({
        roomId: trimmed,
        workspaceId: WORKSPACE_ID,
        limit: 8,
      });
      setRoomMessages(result.messages);
      setRoomMessagesLoadState("ready");
    } catch {
      setRoomMessages([]);
      setRoomMessagesLoadState("error");
    }
  };

  const refreshRoomLoop = async (roomId: string) => {
    const trimmed = roomId.trim();
    if (trimmed === "") {
      setRoomLoop(null);
      setRoomLoopLoadState("idle");
      return;
    }
    setRoomLoopLoadState("loading");
    try {
      const result = await getRoomLoop({
        roomId: trimmed,
        workspaceId: WORKSPACE_ID,
        actorId: ACTOR_ID,
      });
      setRoomLoop(result.loop);
      setRoomLoopLoadState("ready");
    } catch {
      setRoomLoop(null);
      setRoomLoopLoadState("error");
    }
  };

  const refreshAgents = async () => {
    setAgentLoadState("loading");
    try {
      const result = await getAgents({ limit: 100 });
      setAgents(result.agents);
      setAgentLoadState("ready");
    } catch {
      setAgents([]);
      setAgentLoadState("error");
    }
  };

  const refreshATCPRoomSessions = async (roomId: string) => {
    const trimmed = roomId.trim();
    if (trimmed === "") {
      setATCPSessions([]);
      setATCPMembers([]);
      setATCPReadiness({});
      setATCPScreens({});
      setSelectedATCPSessionId(null);
      setATCPLoadState("idle");
      return;
    }
    setATCPLoadState("loading");
    try {
      const result = await getATCPRoomSessions({
        roomId: trimmed,
        workspaceId: WORKSPACE_ID,
      });
      setATCPSessions(result.sessions);
      setATCPMembers(result.members);
      setATCPReadiness(result.readiness ?? {});
      setATCPScreens(result.screens ?? {});
      setSelectedATCPSessionId((current) =>
        current && result.sessions.some((session) => session.id === current)
          ? current
          : result.sessions[0]?.id ?? null,
      );
      setATCPLoadState("ready");
    } catch {
      setATCPSessions([]);
      setATCPMembers([]);
      setATCPReadiness({});
      setATCPScreens({});
      setSelectedATCPSessionId(null);
      setATCPLoadState("error");
      setStatus({ tone: "warning", text: ATCP_DAEMON_HINT });
    }
  };

  const refreshCards = async () => {
    const hadItems = cardItems.length > 0;
    setCardLoadState("loading");
    setStatus({ tone: "focus", text: "refreshing cards" });
    try {
      const result = await getOrchestrationCardWork({
        workspaceId: WORKSPACE_ID,
        limit: 100,
      });
      setCardItems(result.items);
      setCardArtifact(result.artifact?.artifact ?? null);
      setSelectedCardId((current) => current ?? result.items[0]?.id ?? null);
      setCardLoadState("ready");
      setLastCardLoadAt(new Date().toLocaleTimeString());
      setStatus({
        tone: result.artifact ? "warning" : "success",
        text: cardStatus(result.items.length, result.artifact?.artifact),
      });
    } catch (error) {
      setCardLoadState("error");
      setStatus({
        tone: hadItems ? "warning" : "danger",
        text: error instanceof Error ? error.message : "failed to load cards",
      });
    }
  };

  const cycleSelectedATCPSession = () => {
    if (atcpSessions.length === 0) {
      setStatus({ tone: "muted", text: "no ATCP sessions" });
      return;
    }
    setSelectedATCPSessionId((current) => {
      const index = atcpSessions.findIndex((session) => session.id === current);
      return atcpSessions[(index + 1 + atcpSessions.length) % atcpSessions.length]?.id ?? null;
    });
    setFocus("detail");
  };

  const cycleSelectedATCPSessionBy = (delta: number) => {
    if (atcpSessions.length === 0) {
      setStatus({ tone: "muted", text: "no ATCP sessions" });
      return;
    }
    setSelectedATCPSessionId((current) => {
      const index = atcpSessions.findIndex((session) => session.id === current);
      const safeIndex = index < 0 ? 0 : index;
      return atcpSessions[(safeIndex + delta + atcpSessions.length) % atcpSessions.length]?.id ?? null;
    });
    setFocus("detail");
  };

  const openATCPScreenView = () => {
    if (atcpSessions.length === 0) {
      setStatus({ tone: "muted", text: "no ATCP sessions" });
      return;
    }
    setMode("atcpScreen");
    setFocus("detail");
  };

  const openCardRuntimeView = async () => {
    if (!selectedCardItem) {
      setStatus({ tone: "muted", text: "select a card first" });
      return;
    }
    setMode("cardRuntime");
    setFocus("detail");
    setCardRuntime(null);
    setCardRuntimeError(null);
    setCardRuntimeLoadState("loading");
    try {
      const input: GetOrchestrationCardRuntimeInput = {
        workspaceId: WORKSPACE_ID,
        issueId: selectedCardItem.card.issue_id,
        depth: 3,
      };
      const result = await getOrchestrationCardRuntime(input);
      setCardRuntime(result.runtime ?? null);
      setCardRuntimeLoadState("ready");
      const updatedItem = cardWorkItemFromCard(result.card, selectedCardItem);
      setCardItems((current) =>
        current.map((item) =>
          item.card.issue_id === result.card.issue_id ? updatedItem : item,
        ),
      );
      setSelectedCardId(result.card.issue_id);
      setStatus({
        tone: result.runtime?.enabled ? "success" : "warning",
        text: result.runtime?.enabled ? "runtime loaded" : "no runtime attached",
      });
    } catch (error) {
      const message =
        error instanceof Error ? error.message : "failed to load card runtime";
      setCardRuntime(null);
      setCardRuntimeError(message);
      setCardRuntimeLoadState("error");
      setStatus({ tone: "danger", text: message });
    }
  };

  const adjustAgentPaneSize = (delta: number) => {
    setAgentPaneSize((current) => {
      const next = nextAgentPaneSize(current, delta);
      setStatus({ tone: "focus", text: `agent panes ${next}` });
      return next;
    });
    setFocus("detail");
  };

  const toggleScopePane = () => {
    if (compact) {
      setStatus({ tone: "muted", text: "scope is hidden in compact layout" });
      return;
    }
    setScopeCollapsed((current) => {
      const next = !current;
      setStatus({ tone: "focus", text: next ? "scope hidden" : "scope shown" });
      return next;
    });
    if (focus === "nav") {
      setFocus(worklistCollapsed ? "detail" : "worklist");
    }
  };

  const toggleWorklistPane = () => {
    setWorklistCollapsed((current) => {
      const next = !current;
      setStatus({ tone: "focus", text: next ? "worklist hidden" : "worklist shown" });
      return next;
    });
    if (focus === "worklist") {
      setFocus("detail");
    }
  };

  const resizeFocusedMainPane = (delta: number) => {
    if (focus === "nav" && !compact && !scopeCollapsed) {
      setScopePaneSize((current) => {
        const next = nextMainPaneSize(current, delta);
        setStatus({ tone: "focus", text: `scope ${next}` });
        return next;
      });
      return;
    }
    if (focus === "worklist" && !worklistCollapsed) {
      setWorklistPaneSize((current) => {
        const next = nextMainPaneSize(current, delta);
        setStatus({ tone: "focus", text: `${activeNav.label.toLowerCase()} list ${next}` });
        return next;
      });
      return;
    }
    setStatus({ tone: "muted", text: "focus scope or worklist to resize it" });
  };

  const resetPalette = () => {
    setPaletteText("");
    setPaletteIndex(0);
  };

  const openCommandPalette = () => {
    resetPalette();
    setMode("palette");
    setStatus({ tone: "focus", text: "command palette" });
  };

  const executePaletteAction = (action?: CommandPaletteAction) => {
    if (!action) {
      setStatus({ tone: "muted", text: "no matching command" });
      return;
    }
    if (action.disabled) {
      setStatus({
        tone: "muted",
        text: action.disabledReason ?? "command unavailable",
      });
      return;
    }
    resetPalette();
    setMode("normal");
    action.run();
  };

  const refreshActiveWorklist = () => {
    if (activeView === "rooms") {
      void refreshRoomTasks();
      void refreshAgents();
      if (selectedRoom?.id) {
        void refreshRoomMessages(selectedRoom.id);
        void refreshRoomLoop(selectedRoom.id);
        void refreshATCPRoomSessions(selectedRoom.id);
      }
      return;
    }
    if (activeView === "cards") {
      void refreshCards();
      return;
    }
    void refreshRuns();
  };

  const commandPaletteActions: CommandPaletteAction[] = [
    {
      id: "scope:runs",
      title: "Switch to Runs",
      category: "Scope",
      command: "1",
      description: "Show v2 runtime runs and transcripts.",
      destructive: false,
      run: () => {
        setNavIndex(0);
        setFocus(worklistCollapsed ? "detail" : "worklist");
      },
    },
    {
      id: "scope:rooms",
      title: "Switch to Rooms",
      category: "Scope",
      command: "2",
      description: "Show rooms, room tasks, and attached agents.",
      destructive: false,
      run: () => {
        setNavIndex(1);
        setFocus(worklistCollapsed ? "detail" : "worklist");
      },
    },
    {
      id: "scope:cards",
      title: "Switch to Cards",
      category: "Scope",
      command: "3",
      description: "Show orchestration cards from the board projection.",
      destructive: false,
      run: () => {
        setNavIndex(2);
        setFocus(worklistCollapsed ? "detail" : "worklist");
      },
    },
    {
      id: "run:new",
      title: "Compose New Run",
      category: "Runs",
      command: "n",
      description: "Open the model prompt composer.",
      destructive: false,
      run: () => openComposer({ kind: "new" }),
    },
    {
      id: "run:continue",
      title: "Continue Selected Run",
      category: "Runs",
      command: "c",
      description: "Send a follow-up turn to the selected run stream.",
      destructive: false,
      disabled: !selectedRun?.run_id,
      disabledReason: "select a run first",
      run: () => {
        if (selectedRun?.run_id) {
          openComposer({ kind: "continue", runId: selectedRun.run_id });
        }
      },
    },
    {
      id: "run:kill",
      title: "Kill Selected Run",
      category: "Runs",
      command: "x",
      description: "Request cancellation through the v2 runtime.",
      destructive: true,
      disabled: !selectedRun || !canKillRunStatus(selectedRun.status),
      disabledReason: selectedRun ? `run is ${selectedRun.status}` : "select a run first",
      run: requestKillSelectedRun,
    },
    {
      id: "room:create",
      title: "Create Room",
      category: "Rooms",
      command: "+",
      description: "Create a room in the active workspace.",
      destructive: false,
      run: () => {
        setNavIndex(1);
        openRoomComposer();
      },
    },
    {
      id: "room:message",
      title: "Message Selected Room",
      category: "Rooms",
      command: "m",
      description: "Write a room message as the local actor.",
      destructive: false,
      disabled: !selectedRoom && roomTaskItems.length === 0,
      disabledReason: "create or select a room first",
      run: () => {
        setNavIndex(1);
        openRoomMessageComposer();
      },
    },
    {
      id: "room:context-run",
      title: "Run with Room Context",
      category: "Rooms",
      command: "n",
      description: "Seed a new run from the selected room or task.",
      destructive: false,
      disabled: !selectedRoomTaskItem && roomTaskItems.length === 0,
      disabledReason: "select a room or task first",
      run: () => {
        const fallbackRoom = selectedRoomTaskItem ?? roomTaskItems[0];
        openComposer({
          kind: "context",
          source: contextSourceForRoomTask(fallbackRoom),
        });
        setComposerText(contextPromptForRoomTask(fallbackRoom));
      },
    },
    {
      id: "agent:spawn",
      title: "Spawn foxctl Agent",
      category: "Agents",
      command: "s",
      description: "Attach a room-aware daemon agent to the selected room.",
      destructive: false,
      disabled: !selectedRoom && roomTaskItems.length === 0,
      disabledReason: "create or select a room first",
      run: () => {
        setNavIndex(1);
        openAgentSpawnComposer();
      },
    },
    {
      id: "agent:spawn-cli",
      title: "Spawn CLI Agent",
      category: "Agents",
      command: "S",
      description: "Start Codex, Claude, Droid, Gemini, shell, or custom ATCP CLI.",
      destructive: false,
      disabled: !selectedRoom && roomTaskItems.length === 0,
      disabledReason: "create or select a room first",
      run: () => {
        setNavIndex(1);
        openCLISpawnComposer();
      },
    },
    {
      id: "atcp:screen",
      title: "Open Focused CLI Screen",
      category: "ATCP",
      command: "Enter",
      description: "Inspect the focused ATCP session in a larger overlay.",
      destructive: false,
      disabled: atcpSessions.length === 0,
      disabledReason: "no ATCP sessions",
      run: () => {
        setNavIndex(1);
        openATCPScreenView();
      },
    },
    {
      id: "atcp:prompt",
      title: "Prompt Focused CLI",
      category: "ATCP",
      command: "p",
      description: "Send the composer prompt to the focused CLI participant.",
      destructive: false,
      disabled: atcpSessions.length === 0,
      disabledReason: "no ATCP sessions",
      run: () => {
        setNavIndex(1);
        openATCPPromptComposer();
      },
    },
    {
      id: "atcp:stop",
      title: "Stop Focused CLI",
      category: "ATCP",
      command: "x",
      description: "Terminate the attached CLI process after confirmation.",
      destructive: true,
      disabled: atcpSessions.length === 0,
      disabledReason: "no ATCP sessions",
      run: () => {
        setNavIndex(1);
        requestStopSelectedATCPSession();
      },
    },
    {
      id: "card:create",
      title: "Create Card",
      category: "Cards",
      command: "+",
      description: "Seed a new orchestration card into the active workspace.",
      destructive: false,
      run: () => {
        setNavIndex(2);
        openCardComposer();
      },
    },
    {
      id: "card:context-run",
      title: "Run with Card Context",
      category: "Cards",
      command: "n",
      description: "Seed a new run from the selected orchestration card.",
      destructive: false,
      disabled: !selectedCardItem,
      disabledReason: "select an orchestration card first",
      run: () => {
        openComposer({
          kind: "context",
          source: contextSourceForCard(selectedCardItem),
        });
        setComposerText(contextPromptForCard(selectedCardItem));
      },
    },
    {
      id: "card:linked-run",
      title: "Open Card Linked Run",
      category: "Cards",
      command: "Enter",
      description: "Jump to the run linked from the selected card.",
      destructive: false,
      disabled: !selectedCardItem?.card.run_id,
      disabledReason: "selected card has no linked run",
      run: () => {
        if (selectedCardItem?.card.run_id) jumpToRun(selectedCardItem.card.run_id);
      },
    },
    {
      id: "card:runtime",
      title: "Inspect Card Runtime",
      category: "Cards",
      command: "v",
      description: "Open the selected card worker/runtime tree.",
      destructive: false,
      disabled: !selectedCardItem,
      disabledReason: "select an orchestration card first",
      run: () => {
        void openCardRuntimeView();
      },
    },
    {
      id: "card:mark-done",
      title: "Mark Selected Card Done",
      category: "Cards",
      command: "d",
      description: "Move the selected orchestration card to Done.",
      destructive: false,
      disabled: !selectedCardItem || !cardActionAvailability(selectedCardItem.card, "mark-done").available,
      disabledReason: selectedCardItem
        ? cardActionAvailability(selectedCardItem.card, "mark-done").reason
        : "select an orchestration card first",
      run: () => requestSelectedCardAction("mark-done"),
    },
    {
      id: "card:release",
      title: "Release Selected Card",
      category: "Cards",
      command: "u",
      description: "Release a card back to eligible work.",
      destructive: false,
      disabled: !selectedCardItem || !cardActionAvailability(selectedCardItem.card, "release").available,
      disabledReason: selectedCardItem
        ? cardActionAvailability(selectedCardItem.card, "release").reason
        : "select an orchestration card first",
      run: () => requestSelectedCardAction("release"),
    },
    {
      id: "card:retry-now",
      title: "Retry Selected Card Now",
      category: "Cards",
      command: "t",
      description: "Move a blocked or retry-queued card back to retry queue.",
      destructive: false,
      disabled: !selectedCardItem || !cardActionAvailability(selectedCardItem.card, "retry-now").available,
      disabledReason: selectedCardItem
        ? cardActionAvailability(selectedCardItem.card, "retry-now").reason
        : "select an orchestration card first",
      run: () => requestSelectedCardAction("retry-now"),
    },
    {
      id: "worklist:filter",
      title: "Filter Active Worklist",
      category: "Navigation",
      command: "/",
      description: "Filter the visible worklist by explicit fields.",
      destructive: false,
      run: () => {
        setMode("filter");
        setFocus("worklist");
      },
    },
    {
      id: "worklist:refresh",
      title: "Refresh Active Worklist",
      category: "Navigation",
      command: "r",
      description: "Reload the active scope and related room details.",
      destructive: false,
      run: refreshActiveWorklist,
    },
    {
      id: "layout:scope",
      title: scopeCollapsed ? "Show Scope Pane" : "Hide Scope Pane",
      category: "Layout",
      command: "b",
      description: "Toggle the left Scope pane.",
      destructive: false,
      disabled: compact,
      disabledReason: "scope is hidden in compact layout",
      run: toggleScopePane,
    },
    {
      id: "layout:worklist",
      title: worklistCollapsed ? "Show Worklist Pane" : "Hide Worklist Pane",
      category: "Layout",
      command: "w",
      description: "Toggle the active Runs, Rooms, or Cards list pane.",
      destructive: false,
      run: toggleWorklistPane,
    },
    {
      id: "layout:shrink-main",
      title: "Shrink Focused Side Pane",
      category: "Layout",
      command: ",",
      description: "Shrink Scope or the active worklist, depending on focus.",
      destructive: false,
      run: () => resizeFocusedMainPane(-1),
    },
    {
      id: "layout:grow-main",
      title: "Grow Focused Side Pane",
      category: "Layout",
      command: ".",
      description: "Grow Scope or the active worklist, depending on focus.",
      destructive: false,
      run: () => resizeFocusedMainPane(1),
    },
    {
      id: "layout:shrink-agents",
      title: "Shrink Room Agent Panes",
      category: "Layout",
      command: "-",
      description: "Reduce the room agent pane height.",
      destructive: false,
      disabled: activeView !== "rooms",
      disabledReason: "switch to Rooms first",
      run: () => adjustAgentPaneSize(-1),
    },
    {
      id: "layout:grow-agents",
      title: "Grow Room Agent Panes",
      category: "Layout",
      command: "=",
      description: "Increase the room agent pane height.",
      destructive: false,
      disabled: activeView !== "rooms",
      disabledReason: "switch to Rooms first",
      run: () => adjustAgentPaneSize(1),
    },
    {
      id: "help:open",
      title: "Open Help",
      category: "Help",
      command: "?",
      description: "Show keyboard shortcuts and escape paths.",
      destructive: false,
      run: () => setMode("help"),
    },
  ];
  const filteredPaletteActions = filterCommandPaletteActions(
    commandPaletteActions,
    paletteText,
  );
  const selectedPaletteIndex = clampInt(
    paletteIndex,
    0,
    Math.max(0, filteredPaletteActions.length - 1),
  );
  const selectedPaletteAction = filteredPaletteActions[selectedPaletteIndex];

  useEffect(() => {
    void refreshRuns();
    void refreshModelEndpoint();
  }, []);

  useEffect(() => {
    if (
      !composerBusy &&
      !roomCreateBusy &&
      !roomMessageBusy &&
      !atcpPromptBusy &&
      !agentSpawnBusy &&
      !cliSpawnBusy
    ) {
      setBusyTick(0);
      return;
    }
    const timer = setInterval(() => {
      setBusyTick((current) => current + 1);
    }, 120);
    return () => clearInterval(timer);
  }, [
    composerBusy,
    cardCreateBusy,
    roomCreateBusy,
    roomMessageBusy,
    atcpPromptBusy,
    agentSpawnBusy,
    cliSpawnBusy,
  ]);

  const refreshModelEndpoint = async () => {
    try {
      setModelEndpoint(await getV2ModelEndpoint());
    } catch {
      setModelEndpoint(null);
    }
  };

  useEffect(() => {
    if (activeView === "rooms" && roomTaskLoadState === "idle") {
      void refreshRoomTasks();
    }
    if (
      (activeView === "cards" || activeView === "rooms") &&
      cardLoadState === "idle"
    ) {
      void refreshCards();
    }
  }, [activeView, roomTaskLoadState, cardLoadState]);

  useEffect(() => {
    if ((compact || scopeCollapsed) && focus === "nav") {
      setFocus(worklistCollapsed ? "detail" : "worklist");
    }
    if (worklistCollapsed && focus === "worklist") {
      setFocus("detail");
    }
  }, [compact, focus, scopeCollapsed, worklistCollapsed]);

  useEffect(() => {
    const composerVisible =
      activeView === "runs" ||
      activeView === "rooms" ||
      activeView === "cards" ||
      mode === "compose";
    if (!composerVisible && focus === "composer") {
      setFocus("worklist");
    }
  }, [activeView, focus, mode]);

  useEffect(() => {
    if (activeView !== "rooms" && mode === "createRoom") {
      setRoomComposerText("");
      setMode("normal");
    }
    if (activeView !== "cards" && mode === "createCard") {
      setCardComposerText("");
      setMode("normal");
    }
    if (activeView !== "rooms" && mode === "roomMessage") {
      setRoomMessageText("");
      setRoomMessageRoomId(null);
      setMode("normal");
    }
    if (activeView !== "rooms" && mode === "spawnAgent") {
      setAgentSpawnText("researcher");
      setAgentSpawnRoomId(null);
      setMode("normal");
    }
    if (activeView !== "rooms" && mode === "spawnCLI") {
      resetCLISpawnState();
      setCLISpawnRoomId(null);
      setMode("normal");
    }
    if (activeView !== "rooms" && mode === "atcpPrompt") {
      setATCPPromptText("");
      setATCPPromptRoomId(null);
      setMode("normal");
    }
  }, [activeView, mode]);

  useEffect(() => {
    if (activeView !== "rooms") {
      setRoomMessages([]);
      setRoomMessagesLoadState("idle");
      setRoomLoop(null);
      setRoomLoopLoadState("idle");
      setAgentLoadState("idle");
      setATCPSessions([]);
      setATCPMembers([]);
      setATCPReadiness({});
      setATCPScreens({});
      setSelectedATCPSessionId(null);
      setATCPLoadState("idle");
      return;
    }
    if (!selectedRoom?.id) {
      setRoomMessages([]);
      setRoomMessagesLoadState("idle");
      setRoomLoop(null);
      setRoomLoopLoadState("idle");
      setAgentLoadState("idle");
      setATCPSessions([]);
      setATCPMembers([]);
      setATCPReadiness({});
      setATCPScreens({});
      setSelectedATCPSessionId(null);
      setATCPLoadState("idle");
      return;
    }
    void refreshRoomMessages(selectedRoom.id);
    void refreshRoomLoop(selectedRoom.id);
    void refreshAgents();
    void refreshATCPRoomSessions(selectedRoom.id);
  }, [activeView, selectedRoom?.id]);

  useEffect(() => {
    if (activeView !== "runs") return;
    setSelectedRunId((current) =>
      current && selectableRuns.some((item) => item.id === current)
        ? current
        : selectableRuns[0]?.id ?? null,
    );
  }, [activeView, selectableRuns]);

  useEffect(() => {
    if (activeView !== "rooms") return;
    setSelectedRoomTaskId((current) =>
      current && selectableRoomTasks.some((item) => item.id === current)
        ? current
        : selectableRoomTasks[0]?.id ?? null,
    );
  }, [activeView, selectableRoomTasks]);

  useEffect(() => {
    if (activeView !== "cards") return;
    setSelectedCardId((current) =>
      current && selectableCards.some((item) => item.id === current)
        ? current
        : selectableCards[0]?.id ?? null,
    );
  }, [activeView, selectableCards]);

  useEffect(() => {
    setEvents([]);
    setTranscriptItems([]);
    setTranscriptLoadState("idle");
    if (activeView !== "runs") {
      setStreamStatus({ tone: "muted", text: "run stream paused" });
      return;
    }
    if (selectedRunMissing) {
      setStreamStatus({ tone: "warning", text: "selected run is stale" });
      return;
    }
    if (!selectedRun?.run_id) {
      setStreamStatus({ tone: "muted", text: "no run selected" });
      return;
    }
    void refreshRunTranscript(selectedRun.run_id);
    setStreamStatus({ tone: "focus", text: "connecting" });
    return subscribeToV2Stream({
      streamId: selectedRun.run_id,
      streamType: "run",
      afterVersion: 0,
      onStatus: (message) => {
        setStreamStatus({
          tone: message === "live" ? "success" : "focus",
          text: message,
        });
      },
      onError: (message) => {
        setStreamStatus({ tone: "warning", text: message });
      },
      onEvent: (event) => {
        setEvents((current) => [...current.slice(-199), event]);
        if (
          event.event_type === "run.started" ||
          event.event_type === "tool.invoked" ||
          event.event_type === "tool.responded" ||
          event.event_type === "run.completed" ||
          event.event_type === "run.failed" ||
          event.event_type === "stage.failed" ||
          event.event_type === "turn.recorded"
        ) {
          setTimeout(() => void refreshRunTranscript(event.stream_id), 100);
        }
      },
    });
  }, [activeView, selectedRun?.run_id, selectedRunMissing]);

  useKeyboard((key: {
    name?: string;
    ctrl?: boolean;
    shift?: boolean;
    sequence?: string;
    raw?: string;
  }) => {
    const name = key.name ?? "";
    if (key.ctrl && name.toLowerCase() === "c") {
      onExit();
      return;
    }
    if (name === "escape") {
      if (mode === "palette") {
        resetPalette();
      }
      if (mode === "filter") {
        setFilterText("");
      }
      if (mode === "compose") {
        setComposerText("");
        setFocus("worklist");
      }
      if (mode === "createRoom") {
        setRoomComposerText("");
        setFocus("worklist");
      }
      if (mode === "createCard") {
        setCardComposerText("");
        setFocus("worklist");
      }
      if (mode === "roomMessage") {
        setRoomMessageText("");
        setRoomMessageRoomId(null);
        setFocus("worklist");
      }
      if (mode === "spawnAgent") {
        setAgentSpawnText("researcher");
        setAgentSpawnRoomId(null);
        setFocus("worklist");
      }
      if (mode === "spawnCLI") {
        resetCLISpawnState();
        setCLISpawnRoomId(null);
        setFocus("worklist");
      }
      if (mode === "atcpPrompt") {
        setATCPPromptText("");
        setATCPPromptRoomId(null);
        setFocus("worklist");
      }
      if (mode === "atcpScreen") {
        setFocus("detail");
      }
      if (mode === "cardRuntime") {
        setFocus("detail");
      }
      if (mode === "confirmKill") {
        setPendingKillRunId(null);
      }
      if (mode === "confirmCardAction") {
        setPendingCardAction(null);
      }
      if (mode === "confirmATCPStop") {
        setPendingATCPStop(null);
      }
      setMode("normal");
      return;
    }
    if (mode === "palette") {
      if (isSubmitKey(key)) {
        executePaletteAction(selectedPaletteAction);
        return;
      }
      if (name === "up" || name === "k") {
        setPaletteIndex((current) =>
          clampInt(current - 1, 0, Math.max(0, filteredPaletteActions.length - 1)),
        );
        return;
      }
      if (name === "down" || name === "j" || name === "tab") {
        const delta = name === "tab" && key.shift ? -1 : 1;
        setPaletteIndex((current) =>
          clampInt(current + delta, 0, Math.max(0, filteredPaletteActions.length - 1)),
        );
        return;
      }
      if (name === "backspace" || name === "delete") {
        setPaletteText((current) => current.slice(0, -1));
        setPaletteIndex(0);
        return;
      }
      const char = keyChar(name);
      if (char) {
        setPaletteText((current) => current + char);
        setPaletteIndex(0);
      }
      return;
    }
    if (mode === "confirmKill") {
      if (isSubmitKey(key) || name === "y") {
        void confirmKillRun();
        return;
      }
      if (name === "n") {
        setPendingKillRunId(null);
        setMode("normal");
        return;
      }
      return;
    }
    if (mode === "confirmCardAction") {
      if (isSubmitKey(key) || name === "y") {
        void confirmCardAction();
        return;
      }
      if (name === "n") {
        setPendingCardAction(null);
        setMode("normal");
        return;
      }
      return;
    }
    if (mode === "confirmATCPStop") {
      if (isSubmitKey(key) || name === "y") {
        void confirmStopATCPSession();
        return;
      }
      if (name === "n") {
        setPendingATCPStop(null);
        setMode("normal");
        return;
      }
      return;
    }
    if (mode === "atcpScreen") {
      if (name === "tab" || name === "v" || name === "right" || name === "l") {
        cycleSelectedATCPSessionBy(key.shift ? -1 : 1);
        return;
      }
      if (name === "left" || name === "h") {
        cycleSelectedATCPSessionBy(-1);
        return;
      }
      if (name === "p") {
        openATCPPromptComposer();
        return;
      }
      if (name === "x") {
        requestStopSelectedATCPSession();
        return;
      }
      return;
    }
    if (mode === "cardRuntime") {
      if (name === "r") {
        void openCardRuntimeView();
      }
      return;
    }
    if (mode === "compose") {
      if (isSubmitKey(key)) {
        void submitComposedRun();
        return;
      }
      if (name === "backspace" || name === "delete") {
        setComposerText((current) => current.slice(0, -1));
        return;
      }
      const char = keyChar(name);
      if (char) {
        setComposerText((current) => current + char);
      }
      return;
    }
    if (mode === "createRoom") {
      if (isSubmitKey(key)) {
        void submitRoomCreate();
        return;
      }
      if (name === "backspace" || name === "delete") {
        setRoomComposerText((current) => current.slice(0, -1));
        return;
      }
      const char = keyChar(name);
      if (char) {
        setRoomComposerText((current) => current + char);
      }
      return;
    }
    if (mode === "createCard") {
      if (isSubmitKey(key)) {
        void submitCardCreate();
        return;
      }
      if (name === "backspace" || name === "delete") {
        setCardComposerText((current) => current.slice(0, -1));
        return;
      }
      const char = keyChar(name);
      if (char) {
        setCardComposerText((current) => current + char);
      }
      return;
    }
    if (mode === "roomMessage") {
      if (isSubmitKey(key)) {
        void submitRoomMessage();
        return;
      }
      if (name === "backspace" || name === "delete") {
        setRoomMessageText((current) => current.slice(0, -1));
        return;
      }
      const char = keyChar(name);
      if (char) {
        setRoomMessageText((current) => current + char);
      }
      return;
    }
    if (mode === "spawnAgent") {
      if (isSubmitKey(key)) {
        void submitAgentSpawn();
        return;
      }
      if (name === "backspace" || name === "delete") {
        setAgentSpawnText((current) => current.slice(0, -1));
        return;
      }
      const char = keyChar(name);
      if (char) {
        setAgentSpawnText((current) => current + char);
      }
      return;
    }
    if (mode === "spawnCLI") {
      if (cliSpawnStep === "preset") {
        if (name === "up" || name === "k") {
          movePresetSelection(-1, setCLIPresetIndex);
          return;
        }
        if (name === "down" || name === "j") {
          movePresetSelection(1, setCLIPresetIndex);
          return;
        }
        if (isSubmitKey(key)) {
          const preset = cliPresets[cliPresetIndex] ?? cliPresets[0];
          setCLISpawnForm(formFromCLIPreset(preset));
          setCLISpawnField(preset.id === "custom" ? "raw" : "agentId");
          setCLISpawnStep("fields");
          return;
        }
        return;
      }
      if (isSubmitKey(key)) {
        void submitCLISpawn();
        return;
      }
      if (name === "tab") {
        setCLISpawnField((current) => nextCLISpawnField(current, key.shift));
        return;
      }
      if (name === "backspace" || name === "delete") {
        updateCLISpawnField(setCLISpawnForm, cliSpawnField, (current) =>
          current.slice(0, -1),
        );
        return;
      }
      const char = keyChar(name);
      if (char) {
        updateCLISpawnField(setCLISpawnForm, cliSpawnField, (current) =>
          current + char,
        );
      }
      return;
    }
    if (mode === "atcpPrompt") {
      if (isSubmitKey(key)) {
        void submitATCPPrompt();
        return;
      }
      if (name === "backspace" || name === "delete") {
        setATCPPromptText((current) => current.slice(0, -1));
        return;
      }
      const char = keyChar(name);
      if (char) {
        setATCPPromptText((current) => current + char);
      }
      return;
    }
    if (mode === "filter") {
      if (isSubmitKey(key)) {
        setMode("normal");
        return;
      }
      if (name === "backspace" || name === "delete") {
        setFilterText((current) => current.slice(0, -1));
        return;
      }
      const char = keyChar(name);
      if (char) {
        setFilterText((current) => current + char);
      }
      return;
    }
    if (name === "q") {
      onExit();
      return;
    }
    if (name === "?") {
      setMode((current) => (current === "help" ? "normal" : "help"));
      return;
    }
    if (mode === "help") return;
    if (isCommandPaletteKey(key)) {
      openCommandPalette();
      return;
    }
    if (name === "/") {
      setMode("filter");
      setFocus("worklist");
      return;
    }
    if (name === "1" || name === "2" || name === "3") {
      const nextIndex = Number(name) - 1;
      if (navItems[nextIndex]) {
        setNavIndex(nextIndex);
        setFocus("worklist");
      }
      return;
    }
    if (name === "[" || name === "]") {
      const delta = name === "]" ? 1 : -1;
      setNavIndex((current) =>
        (current + delta + navItems.length) % navItems.length,
      );
      setFocus("worklist");
      return;
    }
    if (activeView === "rooms" && isRoomCreateKey(key)) {
      openRoomComposer();
      return;
    }
    if (activeView === "cards" && isRoomCreateKey(key)) {
      openCardComposer();
      return;
    }
    if (name === "m" && activeView === "rooms") {
      openRoomMessageComposer();
      return;
    }
    if (activeView === "rooms" && isCLISpawnKey(key)) {
      openCLISpawnComposer();
      return;
    }
    if (name === "s" && activeView === "rooms") {
      openAgentSpawnComposer();
      return;
    }
    if (name === "p" && activeView === "rooms") {
      openATCPPromptComposer();
      return;
    }
    if (name === "n" && activeView === "runs") {
      openComposer({ kind: "new" });
      return;
    }
    if (name === "n" && activeView === "rooms") {
      const fallbackRoom = selectedRoomTaskItem ?? roomTaskItems[0];
      openComposer({
        kind: "context",
        source: contextSourceForRoomTask(fallbackRoom),
      });
      setComposerText(contextPromptForRoomTask(fallbackRoom));
      return;
    }
    if (name === "n" && activeView === "cards") {
      openComposer({
        kind: "context",
        source: contextSourceForCard(selectedCardItem),
      });
      setComposerText(contextPromptForCard(selectedCardItem));
      return;
    }
    if (name === "c" && activeView === "runs") {
      if (selectedRun?.run_id) {
        openComposer({ kind: "continue", runId: selectedRun.run_id });
      } else {
        setStatus({ tone: "muted", text: "select a run first" });
      }
      return;
    }
    if (name === "x" && activeView === "runs") {
      requestKillSelectedRun();
      return;
    }
    if (name === "x" && activeView === "rooms") {
      requestStopSelectedATCPSession();
      return;
    }
    if (activeView === "cards" && name === "d") {
      requestSelectedCardAction("mark-done");
      return;
    }
    if (activeView === "cards" && name === "u") {
      requestSelectedCardAction("release");
      return;
    }
    if (activeView === "cards" && name === "t") {
      requestSelectedCardAction("retry-now");
      return;
    }
    if (name === "a") {
      setActivityScope((current) => nextActivityScope(current));
      return;
    }
    if (name === "v" && activeView === "rooms") {
      cycleSelectedATCPSession();
      return;
    }
    if (name === "v" && activeView === "cards") {
      void openCardRuntimeView();
      return;
    }
    if (name === "b") {
      toggleScopePane();
      return;
    }
    if (name === "w") {
      toggleWorklistPane();
      return;
    }
    if ((name === "," || name === ".") && mode === "normal") {
      resizeFocusedMainPane(name === "," ? -1 : 1);
      return;
    }
    if ((name === "-" || name === "=") && activeView === "rooms" && focus === "detail") {
      adjustAgentPaneSize(name === "-" ? -1 : 1);
      return;
    }
    if (name === "r") {
      refreshActiveWorklist();
      return;
    }
    if (name === "tab") {
      setFocus((current) =>
        nextFocus(current, {
          reverse: key.shift,
          showNav: scopeVisible,
          showWorklist: worklistVisible,
          hasComposer:
            activeView === "runs" ||
            activeView === "rooms" ||
            activeView === "cards",
        }),
      );
      return;
    }
    if (isSubmitKey(key) && activeView === "runs" && focus === "worklist") {
      setFocus("detail");
      return;
    }
    if (isSubmitKey(key) && activeView === "rooms" && focus === "detail") {
      openATCPScreenView();
      return;
    }
    if (isSubmitKey(key) && activeView === "cards" && selectedCardItem?.card.run_id) {
      jumpToRun(selectedCardItem.card.run_id);
      return;
    }
    if (name === "left" || name === "h") {
      if (focus === "detail" || focus === "composer") {
        setFocus("worklist");
        return;
      }
    }
    if (name === "right" || name === "l") {
      if (focus === "worklist") {
        setFocus("detail");
        return;
      }
    }
    if (name === "up" || name === "k") {
      if (focus === "nav") setNavIndex((i) => Math.max(0, i - 1));
      if (focus === "detail" && activeView === "rooms") {
        cycleSelectedATCPSessionBy(-1);
        return;
      }
      if (focus === "worklist" && activeView === "runs") {
        moveSelection(-1, selectableRuns, selectedRunId, setSelectedRunId);
      }
      if (focus === "worklist" && activeView === "rooms") {
        moveSelection(
          -1,
          selectableRoomTasks,
          selectedRoomTaskId,
          setSelectedRoomTaskId,
        );
      }
      if (focus === "worklist" && activeView === "cards") {
        moveSelection(-1, selectableCards, selectedCardId, setSelectedCardId);
      }
      return;
    }
    if (name === "down" || name === "j") {
      if (focus === "nav") {
        setNavIndex((i) => Math.min(navItems.length - 1, i + 1));
      }
      if (focus === "detail" && activeView === "rooms") {
        cycleSelectedATCPSessionBy(1);
        return;
      }
      if (focus === "worklist" && activeView === "runs") {
        moveSelection(1, selectableRuns, selectedRunId, setSelectedRunId);
      }
      if (focus === "worklist" && activeView === "rooms") {
        moveSelection(
          1,
          selectableRoomTasks,
          selectedRoomTaskId,
          setSelectedRoomTaskId,
        );
      }
      if (focus === "worklist" && activeView === "cards") {
        moveSelection(1, selectableCards, selectedCardId, setSelectedCardId);
      }
    }
  });

  return (
    <AppFrame
      compact={compact}
      focus={focus}
      mode={mode}
      status={status}
      streamStatus={streamStatus}
      filterText={filterText}
      activityScope={activityScope}
    >
      {tooSmall ? (
        <SmallTerminalNotice width={width} height={height} />
      ) : (
        <>
          <MainRegion compact={compact}>
            {scopeVisible && (
              <Sidebar
                compact={compact}
                focused={focus === "nav"}
                activeIndex={navIndex}
                width={scopeWidth}
              />
            )}
            {activeView === "rooms" ? (
              <>
                {showWorklistRegion && (
                  <RoomTaskListPanel
                    compact={compact}
                    short={short}
                    panelWidth={worklistWidth}
                    focused={focus === "worklist"}
                    sections={roomTaskSections}
                    selectedId={selectedRoomTaskId}
                    status={status}
                    loadState={roomTaskLoadState}
                    lastLoadedAt={lastRoomTaskLoadAt}
                    roomCount={roomCount}
                    sourceCount={roomTaskItems.length}
                    filterText={filterText}
                  />
                )}
                {showDetailRegion && (
                  <RoomTaskDetailPanel
                    compact={compact}
                    terminalWidth={width}
                    focused={focus === "detail"}
                    selectedItem={selectedRoomTaskItem}
                    selectedId={selectedRoomTaskId}
                    selectedMissing={selectedRoomTaskMissing}
                    roomCount={roomCount}
                    messages={roomMessages}
                    messagesLoadState={roomMessagesLoadState}
                    loop={roomLoop}
                    loopLoadState={roomLoopLoadState}
                    agents={agents}
                    agentLoadState={agentLoadState}
                    agentPaneSize={agentPaneSize}
                    atcpSessions={atcpSessions}
                    atcpMembers={atcpMembers}
                    atcpReadiness={atcpReadiness}
                    atcpScreens={atcpScreens}
                    selectedATCPSessionId={selectedATCPSessionId}
                    atcpLoadState={atcpLoadState}
                    cards={cardItems}
                    cardLoadState={cardLoadState}
                  />
                )}
              </>
            ) : activeView === "cards" ? (
              <>
                {showWorklistRegion && (
                  <CardListPanel
                    compact={compact}
                    short={short}
                    panelWidth={worklistWidth}
                    focused={focus === "worklist"}
                    sections={cardSections}
                    selectedId={selectedCardId}
                    status={status}
                    loadState={cardLoadState}
                    lastLoadedAt={lastCardLoadAt}
                    artifact={cardArtifact}
                    sourceCount={cardItems.length}
                    filterText={filterText}
                  />
                )}
                {showDetailRegion && (
                  <CardDetailPanel
                    compact={compact}
                    focused={focus === "detail"}
                    selectedItem={selectedCardItem}
                    selectedId={selectedCardId}
                    selectedMissing={selectedCardMissing}
                    artifact={cardArtifact}
                  />
                )}
              </>
            ) : (
              <>
                {showWorklistRegion && (
                  <RunListPanel
                    compact={compact}
                    short={short}
                    panelWidth={worklistWidth}
                    focused={focus === "worklist"}
                    sections={runSections}
                    selectedId={selectedRunId}
                    status={status}
                    loadState={runLoadState}
                    lastLoadedAt={lastRunLoadAt}
                    sourceCount={runs.length}
                    filterText={filterText}
                  />
                )}
                {showDetailRegion && (
                  <RunDetailPanel
                    compact={compact}
                    short={short}
                    focused={focus === "detail"}
                    selectedRun={selectedRun}
                    selectedRunId={selectedRunId}
                    selectedRunMissing={selectedRunMissing}
                    events={events}
                    transcriptItems={transcriptItems}
                    transcriptLoadState={transcriptLoadState}
                    activityScope={activityScope}
                    activeSection={activeNav.label}
                  />
                )}
              </>
            )}
          </MainRegion>
          {(activeView === "runs" || mode === "compose") && (
            <RunComposer
              compact={compact}
              focused={focus === "composer" || mode === "compose"}
              value={composerText}
              target={composerTarget}
              busy={composerBusy}
              busyTick={busyTick}
              modelEndpoint={modelEndpoint}
            />
          )}
          {activeView === "rooms" && mode !== "compose" && (
            <RoomComposer
              compact={compact}
              focused={
                focus === "composer" ||
                mode === "createRoom" ||
                mode === "roomMessage" ||
                mode === "spawnAgent" ||
                mode === "spawnCLI" ||
                mode === "atcpPrompt"
              }
              value={
                mode === "spawnCLI"
                  ? cliSpawnComposerValue(
                      cliSpawnStep,
                      cliSpawnForm,
                      cliSpawnField,
                    )
                  : mode === "spawnAgent"
                  ? agentSpawnText
                  : mode === "atcpPrompt"
                  ? atcpPromptText
                  : mode === "roomMessage"
                    ? roomMessageText
                    : roomComposerText
              }
              busy={
                mode === "spawnCLI"
                  ? cliSpawnBusy
                  : mode === "spawnAgent"
                  ? agentSpawnBusy
                  : mode === "atcpPrompt"
                  ? atcpPromptBusy
                  : mode === "roomMessage"
                    ? roomMessageBusy
                    : roomCreateBusy
              }
              busyTick={busyTick}
              purpose={
                mode === "spawnCLI"
                  ? "cli"
                  : mode === "spawnAgent"
                  ? "spawn"
                  : mode === "atcpPrompt"
                  ? "prompt"
                  : mode === "roomMessage"
                    ? "message"
                    : "create"
              }
              roomId={
                cliSpawnRoomId ??
                agentSpawnRoomId ??
                atcpPromptRoomId ??
                roomMessageRoomId ??
                selectedRoom?.id ??
                null
              }
              targetLabel={selectedATCPAgentLabel}
            />
          )}
          {activeView === "cards" && mode !== "compose" && (
            <CardComposer
              compact={compact}
              focused={focus === "composer" || mode === "createCard"}
              value={cardComposerText}
              busy={cardCreateBusy}
              busyTick={busyTick}
            />
          )}
          {mode === "spawnCLI" && cliSpawnStep === "preset" && (
            <CLIPresetOverlay
              compact={compact}
              width={width}
              selectedIndex={cliPresetIndex}
            />
          )}
        </>
      )}
      {mode === "help" && <HelpOverlay compact={compact} width={width} />}
      {mode === "palette" && (
        <CommandPaletteOverlay
          compact={compact}
          width={width}
          height={height}
          query={paletteText}
          actions={filteredPaletteActions}
          selectedIndex={selectedPaletteIndex}
        />
      )}
      {mode === "confirmKill" && pendingKillRunId && (
        <KillConfirmOverlay
          compact={compact}
          width={width}
          runId={pendingKillRunId}
          busy={killBusy}
        />
      )}
      {mode === "confirmCardAction" && pendingCardAction && (
        <CardActionConfirmOverlay
          compact={compact}
          width={width}
          target={pendingCardAction}
          busy={cardActionBusy}
        />
      )}
      {mode === "confirmATCPStop" && pendingATCPStop && (
        <ATCPStopConfirmOverlay
          compact={compact}
          width={width}
          target={pendingATCPStop}
          busy={atcpStopBusy}
        />
      )}
      {mode === "atcpScreen" && selectedATCPSession && (
        <ATCPScreenOverlay
          compact={compact}
          width={width}
          height={height}
          session={selectedATCPSession}
          member={selectedATCPMember}
          readiness={atcpReadiness[selectedATCPSession.id]}
          screen={atcpScreens[selectedATCPSession.id]}
          sessions={atcpSessions}
        />
      )}
      {mode === "cardRuntime" && selectedCardItem && (
        <CardRuntimeOverlay
          compact={compact}
          width={width}
          height={height}
          item={selectedCardItem}
          runtime={cardRuntime}
          loadState={cardRuntimeLoadState}
          error={cardRuntimeError}
        />
      )}
    </AppFrame>
  );
}

function CommandPaletteOverlay({
  compact,
  width,
  height,
  query,
  actions,
  selectedIndex,
}: {
  compact: boolean;
  width: number;
  height: number;
  query: string;
  actions: CommandPaletteAction[];
  selectedIndex: number;
}) {
  const overlayWidth = compact
    ? Math.max(52, width - 4)
    : Math.max(76, Math.min(width - 14, 110));
  const overlayHeight = Math.max(14, Math.min(height - 6, compact ? 20 : 24));
  const left = compact ? 2 : Math.max(4, Math.floor((width - overlayWidth) / 2));
  const top = compact ? 3 : 4;
  const bodyWidth = overlayWidth - 6;
  const maxItems = Math.max(4, overlayHeight - 8);
  const start = paletteWindowStart(selectedIndex, actions.length, maxItems);
  const visibleActions = actions.slice(start, start + maxItems);
  const selectedAction = actions[selectedIndex];
  return (
    <box
      style={{
        position: "absolute",
        top,
        left,
        width: overlayWidth,
        height: overlayHeight,
        border: true,
        borderStyle: "rounded",
        borderColor: theme.focus,
        backgroundColor: theme.panel,
        flexDirection: "column",
        padding: 1,
        gap: 1,
      }}
    >
      <box
        style={{
          flexDirection: "row",
          justifyContent: "space-between",
          height: 1,
        }}
      >
        <text fg={theme.focus}>Command palette</text>
        <text fg={theme.muted}>{actions.length} actions</text>
      </box>
      <text fg={query ? theme.text : theme.muted}>
        {truncate(`:${query || "type to filter"}`, bodyWidth)}
      </text>
      <box style={{ flexDirection: "column", flexGrow: 1, gap: 0 }}>
        {visibleActions.length === 0 ? (
          <text fg={theme.muted}>No matching commands.</text>
        ) : (
          visibleActions.map((action, offset) => {
            const index = start + offset;
            const selected = index === selectedIndex;
            const tone = action.disabled
              ? theme.muted
              : action.destructive
                ? theme.warning
                : selected
                  ? theme.focus
                  : theme.text;
            return (
              <box
                key={action.id}
                style={{
                  flexDirection: "column",
                  backgroundColor: selected ? theme.panelAlt : theme.panel,
                }}
              >
                <box
                  style={{
                    flexDirection: "row",
                    justifyContent: "space-between",
                    height: 1,
                  }}
                >
                  <text fg={tone}>
                    {selected ? "> " : "  "}
                    {truncate(action.title, Math.max(12, bodyWidth - 18))}
                  </text>
                  <text fg={theme.muted}>{truncate(action.command, 12)}</text>
                </box>
                <text fg={theme.muted}>
                  {"  "}
                  {truncate(
                    `${action.category}  ${
                      action.disabled
                        ? action.disabledReason ?? "unavailable"
                        : action.description
                    }`,
                    bodyWidth - 2,
                  )}
                </text>
              </box>
            );
          })
        )}
      </box>
      <text fg={selectedAction?.destructive ? theme.warning : theme.muted}>
        {truncate(
          selectedAction
            ? "Enter runs command, Esc closes"
            : "Esc closes",
          bodyWidth,
        )}
      </text>
    </box>
  );
}

function KillConfirmOverlay({
  compact,
  width,
  runId,
  busy,
}: {
  compact: boolean;
  width: number;
  runId: string;
  busy: boolean;
}) {
  const overlayWidth = compact ? Math.max(44, Math.min(width - 4, 62)) : 64;
  const left = compact ? 2 : 10;
  return (
    <box
      style={{
        position: "absolute",
        top: 5,
        left,
        width: overlayWidth,
        height: 11,
        border: true,
        borderStyle: "rounded",
        borderColor: theme.warning,
        backgroundColor: theme.panel,
        flexDirection: "column",
        padding: 1,
        gap: 1,
      }}
    >
      <text fg={theme.warning}>{busy ? "Killing run" : "Kill selected run?"}</text>
      <text fg={theme.text}>{truncate(runId, overlayWidth - 6)}</text>
      <text fg={theme.muted}>This requests cancellation through v2 runtime.</text>
      <text fg={theme.warning}>Enter confirms, Esc cancels.</text>
    </box>
  );
}

function CardActionConfirmOverlay({
  compact,
  width,
  target,
  busy,
}: {
  compact: boolean;
  width: number;
  target: PendingCardAction;
  busy: boolean;
}) {
  const overlayWidth = compact ? Math.max(44, Math.min(width - 4, 66)) : 66;
  const left = compact ? 2 : 10;
  return (
    <box
      style={{
        position: "absolute",
        top: 5,
        left,
        width: overlayWidth,
        height: 12,
        border: true,
        borderStyle: "rounded",
        borderColor: theme.warning,
        backgroundColor: theme.panel,
        flexDirection: "column",
        padding: 1,
        gap: 1,
      }}
    >
      <text fg={theme.warning}>
        {busy
          ? `${cardActionLabel(target.action)} card`
          : `Confirm ${cardActionLabel(target.action)}?`}
      </text>
      <text fg={theme.text}>{truncate(target.title, overlayWidth - 6)}</text>
      <text fg={theme.muted}>issue     {truncate(target.issueId, overlayWidth - 16)}</text>
      <text fg={theme.muted}>
        This appends an orchestration card event.
      </text>
      <text fg={theme.warning}>Enter confirms, Esc cancels.</text>
    </box>
  );
}

function ATCPStopConfirmOverlay({
  compact,
  width,
  target,
  busy,
}: {
  compact: boolean;
  width: number;
  target: ATCPStopTarget;
  busy: boolean;
}) {
  const overlayWidth = compact ? Math.max(44, Math.min(width - 4, 66)) : 66;
  const left = compact ? 2 : 10;
  const label = target.agentId ?? target.sessionId;
  return (
    <box
      style={{
        position: "absolute",
        top: 5,
        left,
        width: overlayWidth,
        height: 12,
        border: true,
        borderStyle: "rounded",
        borderColor: theme.warning,
        backgroundColor: theme.panel,
        flexDirection: "column",
        padding: 1,
        gap: 1,
      }}
    >
      <text fg={theme.warning}>
        {busy ? "Stopping ATCP session" : "Stop focused ATCP session?"}
      </text>
      <text fg={theme.text}>{truncate(label, overlayWidth - 6)}</text>
      <text fg={theme.muted}>
        session   {truncate(target.sessionId, overlayWidth - 16)}
      </text>
      <text fg={theme.muted}>This terminates the attached CLI process.</text>
      <text fg={theme.warning}>Enter confirms, Esc cancels.</text>
    </box>
  );
}

function ATCPScreenOverlay({
  compact,
  width,
  height,
  session,
  member,
  readiness,
  screen,
  sessions,
}: {
  compact: boolean;
  width: number;
  height: number;
  session: ATCPSession;
  member?: ATCPMember;
  readiness?: ATCPReadiness;
  screen?: ATCPScreen;
  sessions: ATCPSession[];
}) {
  const overlayWidth = compact
    ? Math.max(54, width - 4)
    : Math.max(84, Math.min(width - 12, 150));
  const overlayHeight = Math.max(16, Math.min(height - 6, compact ? 24 : 34));
  const left = compact ? 2 : Math.max(4, Math.floor((width - overlayWidth) / 2));
  const top = compact ? 3 : 4;
  const bodyWidth = overlayWidth - 6;
  const lineCount = Math.max(4, overlayHeight - 9);
  const lines = atcpScreenLines(screen, lineCount);
  const title = member?.agent_id ?? session.id;
  const status = `${session.status} ${readiness?.idle ? "idle" : "busy"}`;
  return (
    <box
      style={{
        position: "absolute",
        top,
        left,
        width: overlayWidth,
        height: overlayHeight,
        border: true,
        borderStyle: "single",
        borderColor: theme.focus,
        backgroundColor: theme.panel,
        flexDirection: "column",
        padding: 1,
        gap: 1,
      }}
    >
      <box
        style={{
          flexDirection: "row",
          justifyContent: "space-between",
          height: 1,
        }}
      >
        <text fg={theme.focus}>ATCP screen</text>
        <text fg={atcpSessionTone(session, readiness)}>{truncate(status, 22)}</text>
      </box>
      <text fg={theme.text}>{truncate(title, bodyWidth)}</text>
      <text fg={theme.muted}>
        {truncate(`${session.adapter || "cli"} ${session.cmd?.join(" ") || ""}`, bodyWidth)}
      </text>
      <text fg={theme.subtle}>
        {truncate(
          `${sessions.length} sessions  Tab cycle  p prompt  x stop  Esc close`,
          bodyWidth,
        )}
      </text>
      <box
        style={{
          flexDirection: "column",
          flexGrow: 1,
          backgroundColor: theme.bg,
          paddingLeft: 1,
          paddingRight: 1,
        }}
      >
        {lines.length === 0 ? (
          <text fg={theme.muted}>No rendered screen snapshot yet.</text>
        ) : (
          lines.map((line, index) => (
            <text key={`${session.id}:screen:${index}`} fg={theme.text}>
              {truncate(line, bodyWidth - 2)}
            </text>
          ))
        )}
      </box>
    </box>
  );
}

function CardRuntimeOverlay({
  compact,
  width,
  height,
  item,
  runtime,
  loadState,
  error,
}: {
  compact: boolean;
  width: number;
  height: number;
  item: OrchestrationCardWorkItem;
  runtime: OrchestrationRuntimeTree | null;
  loadState: LoadState;
  error: string | null;
}) {
  const overlayWidth = compact
    ? Math.max(54, width - 4)
    : Math.max(84, Math.min(width - 12, 150));
  const overlayHeight = Math.max(16, Math.min(height - 6, compact ? 24 : 34));
  const left = compact ? 2 : Math.max(4, Math.floor((width - overlayWidth) / 2));
  const top = compact ? 3 : 4;
  const bodyWidth = overlayWidth - 6;
  const lineCount = Math.max(4, overlayHeight - 10);
  const lines = cardRuntimeLines(runtime, lineCount);
  const title = item.card.title || item.card.issue_identifier || item.card.issue_id;
  const status =
    loadState === "loading"
      ? "loading"
      : error
        ? "error"
        : runtime?.enabled
          ? "runtime"
          : "no runtime";
  return (
    <box
      style={{
        position: "absolute",
        top,
        left,
        width: overlayWidth,
        height: overlayHeight,
        border: true,
        borderStyle: "single",
        borderColor: error ? theme.warning : theme.focus,
        backgroundColor: theme.panel,
        flexDirection: "column",
        padding: 1,
        gap: 1,
      }}
    >
      <box
        style={{
          flexDirection: "row",
          justifyContent: "space-between",
          height: 1,
        }}
      >
        <text fg={theme.focus}>Card runtime</text>
        <text fg={error ? theme.warning : runtime?.enabled ? theme.success : theme.muted}>
          {status}
        </text>
      </box>
      <text fg={theme.text}>{truncate(title, bodyWidth)}</text>
      <text fg={theme.muted}>
        {truncate(
          `${item.card.issue_id}  ${item.laneTitle} / ${item.card.state}`,
          bodyWidth,
        )}
      </text>
      <text fg={theme.subtle}>
        {truncate("r refresh  Esc close", bodyWidth)}
      </text>
      <box
        style={{
          flexDirection: "column",
          flexGrow: 1,
          backgroundColor: theme.bg,
          paddingLeft: 1,
          paddingRight: 1,
        }}
      >
        {loadState === "loading" ? (
          <text fg={theme.focus}>Loading card runtime...</text>
        ) : error ? (
          <text fg={theme.warning}>{truncate(error, bodyWidth - 2)}</text>
        ) : runtime?.error ? (
          <text fg={theme.warning}>{truncate(runtime.error, bodyWidth - 2)}</text>
        ) : lines.length === 0 ? (
          <text fg={theme.muted}>No runtime tree attached to this card.</text>
        ) : (
          lines.map((line, index) => (
            <text key={`${item.card.issue_id}:runtime:${index}`} fg={line.tone}>
              {truncate(line.text, bodyWidth - 2)}
            </text>
          ))
        )}
      </box>
    </box>
  );
}

function CLIPresetOverlay({
  compact,
  width,
  selectedIndex,
}: {
  compact: boolean;
  width: number;
  selectedIndex: number;
}) {
  const overlayWidth = compact ? Math.max(48, Math.min(width - 4, 68)) : 68;
  const left = compact ? 2 : 10;
  return (
    <box
      style={{
        position: "absolute",
        top: 5,
        left,
        width: overlayWidth,
        height: 14,
        border: true,
        borderStyle: "rounded",
        borderColor: theme.focus,
        backgroundColor: theme.panel,
        flexDirection: "column",
        padding: 1,
        gap: 1,
      }}
    >
      <text fg={theme.focus}>Spawn CLI</text>
      {cliPresets.map((preset, index) => (
        <box
          key={preset.id}
          style={{
            flexDirection: "column",
            backgroundColor: index === selectedIndex ? theme.panelAlt : theme.panel,
          }}
        >
          <text fg={index === selectedIndex ? theme.focus : theme.text}>
            {index === selectedIndex ? "> " : "  "}
            {preset.label}
          </text>
          <text fg={theme.muted}>
            {"  "}
            {truncate(cliPresetDescription(preset), overlayWidth - 6)}
          </text>
        </box>
      ))}
      <text fg={theme.muted}>Enter edits fields, Esc cancels.</text>
    </box>
  );
}

function Sidebar({
  compact,
  focused,
  activeIndex,
  width,
}: {
  compact: boolean;
  focused: boolean;
  activeIndex: number;
  width: number;
}) {
  if (compact) return null;
  return (
    <box
      style={{
        width,
        height: "100%",
        border: true,
        borderStyle: "single",
        borderColor: focused ? theme.focus : theme.border,
        flexDirection: "column",
        padding: 1,
        gap: 1,
      }}
    >
      <text fg={focused ? theme.focus : theme.text}>Scope</text>
      {navItems.map((item, index) => (
        <box key={item.id} style={{ flexDirection: "column" }}>
          <text fg={index === activeIndex ? theme.focus : theme.text}>
            {index === activeIndex ? "> " : "  "}
            {item.label}
          </text>
          <text fg={theme.muted}>  {item.hint}</text>
        </box>
      ))}
    </box>
  );
}

function RunComposer({
  compact,
  focused,
  value,
  target,
  busy,
  busyTick,
  modelEndpoint,
}: {
  compact: boolean;
  focused: boolean;
  value: string;
  target: ComposerTarget;
  busy: boolean;
  busyTick: number;
  modelEndpoint: V2ModelEndpoint | null;
}) {
  const label =
    target.kind === "continue"
      ? "follow-up"
      : target.kind === "context"
        ? "context"
        : "prompt";
  const spinner = busy ? spinnerFrame(busyTick) : "";
  const endpoint = modelEndpointLabel(modelEndpoint);
  const visibleValue =
    value.trim() === ""
      ? target.kind === "continue"
        ? `c to continue ${truncate(target.runId, compact ? 24 : 40)}`
        : target.kind === "context"
          ? `n to run from ${truncate(target.source, compact ? 26 : 44)}`
        : "n to compose a v2 worker run"
      : truncate(value, compact ? 58 : 120);
  return (
    <box
      style={{
        height: 3,
        marginLeft: 1,
        marginRight: 1,
        marginBottom: 1,
        border: true,
        borderStyle: "single",
        borderColor: focused ? theme.focus : theme.border,
        flexDirection: "row",
        alignItems: "center",
        paddingLeft: 1,
        paddingRight: 1,
        gap: 1,
      }}
    >
      <text fg={focused ? theme.focus : theme.muted}>
        {busy ? `${spinner} ${label}` : label}
      </text>
      <text fg={theme.subtle}>{truncate(endpoint, compact ? 34 : 42)}</text>
      <text fg={value.trim() === "" ? theme.muted : theme.text}>
        {visibleValue}
        {focused && !busy ? "_" : ""}
      </text>
    </box>
  );
}

function RoomComposer({
  compact,
  focused,
  value,
  busy,
  busyTick,
  purpose,
  roomId,
  targetLabel,
}: {
  compact: boolean;
  focused: boolean;
  value: string;
  busy: boolean;
  busyTick: number;
  purpose: "create" | "message" | "spawn" | "cli" | "prompt";
  roomId: string | null;
  targetLabel?: string | null;
}) {
  const spinner = busy ? spinnerFrame(busyTick) : "";
  const label =
    purpose === "cli"
      ? "cli"
      : purpose === "prompt"
        ? "prompt"
        : purpose === "spawn"
          ? "agent"
          : purpose === "message"
            ? "message"
            : "room";
  const target = targetLabel ?? roomId ?? "session";
  const targetDetail =
    purpose === "prompt"
      ? `Enter -> ATCP ${target}`
      : purpose === "message"
        ? `Enter -> room ${roomId ?? "selected room"}`
        : purpose === "spawn"
          ? `Enter -> daemon agent in ${roomId ?? "selected room"}`
          : purpose === "cli"
            ? `Enter -> ATCP CLI in ${roomId ?? "selected room"}`
            : `Enter -> workspace ${WORKSPACE_ID}`;
  const scopedPurpose =
    purpose === "message" ||
    purpose === "spawn" ||
    purpose === "cli" ||
    purpose === "prompt";
  const visibleValue =
    value.trim() === ""
      ? purpose === "cli"
        ? `S to spawn CLI into ${truncate(roomId ?? "room", compact ? 21 : 38)}`
        : purpose === "prompt"
        ? `p to prompt ${truncate(target, compact ? 24 : 42)}`
        : purpose === "spawn"
        ? `s to spawn into ${truncate(roomId ?? "room", compact ? 24 : 40)}`
        : purpose === "message"
        ? `m to message ${truncate(roomId ?? "room", compact ? 26 : 44)}`
        : "+ to create a room"
      : truncate(value, compact ? 66 : 130);
  return (
    <box
      style={{
        height: 4,
        marginLeft: 1,
        marginRight: 1,
        marginBottom: 1,
        border: true,
        borderStyle: "single",
        borderColor: focused ? theme.focus : theme.border,
        flexDirection: "column",
        paddingLeft: 1,
        paddingRight: 1,
      }}
    >
      <box style={{ flexDirection: "row", justifyContent: "space-between", height: 1 }}>
        <text fg={focused ? theme.focus : theme.muted}>
          {busy ? `${spinner} ${label}` : label}
        </text>
        <text fg={theme.subtle}>
          {truncate(targetDetail, compact ? 44 : 86)}
        </text>
      </box>
      <box style={{ flexDirection: "row", height: 1, gap: 1 }}>
        <text fg={theme.subtle}>
          {truncate(scopedPurpose ? roomId ?? WORKSPACE_ID : WORKSPACE_ID, compact ? 22 : 36)}
        </text>
        <text fg={value.trim() === "" ? theme.muted : theme.text}>
          {visibleValue}
          {focused && !busy ? "_" : ""}
        </text>
      </box>
    </box>
  );
}

function CardComposer({
  compact,
  focused,
  value,
  busy,
  busyTick,
}: {
  compact: boolean;
  focused: boolean;
  value: string;
  busy: boolean;
  busyTick: number;
}) {
  const spinner = busy ? spinnerFrame(busyTick) : "";
  const visibleValue =
    value.trim() === ""
      ? "+ to create an orchestration card"
      : truncate(value, compact ? 66 : 130);
  return (
    <box
      style={{
        height: 3,
        marginLeft: 1,
        marginRight: 1,
        marginBottom: 1,
        border: true,
        borderStyle: "single",
        borderColor: focused ? theme.focus : theme.border,
        flexDirection: "row",
        alignItems: "center",
        paddingLeft: 1,
        paddingRight: 1,
        gap: 1,
      }}
    >
      <text fg={focused ? theme.focus : theme.muted}>
        {busy ? `${spinner} card` : "card"}
      </text>
      <text fg={theme.subtle}>{truncate(WORKSPACE_ID, compact ? 28 : 44)}</text>
      <text fg={value.trim() === "" ? theme.muted : theme.text}>
        {visibleValue}
        {focused && !busy ? "_" : ""}
      </text>
    </box>
  );
}

function RunListPanel({
  compact,
  short,
  panelWidth,
  focused,
  sections,
  selectedId,
  status,
  loadState,
  lastLoadedAt,
  sourceCount,
  filterText,
}: {
  compact: boolean;
  short: boolean;
  panelWidth: PanelWidth;
  focused: boolean;
  sections: WorklistSection<RunWorklistItem>[];
  selectedId: string | null;
  status: StatusMessage;
  loadState: LoadState;
  lastLoadedAt: string | null;
  sourceCount: number;
  filterText: string;
}) {
  const runCount = sections.reduce(
    (total, section) => total + section.items.length,
    0,
  );
  const stale = loadState === "error" && runCount > 0;
  return (
    <Panel
      title="Runs"
      subtitle={runCount === 0 ? undefined : `${runCount} total`}
      focused={focused}
      width={panelWidth}
      minWidth={30}
      height={compact ? (short ? 8 : 12) : "100%"}
      footer={
        <text fg={stale ? theme.warning : theme.muted}>
          {runFooter(loadState, lastLoadedAt, stale)}
        </text>
      }
    >
      <GroupedWorklist
        sections={sections}
        selectedId={selectedId}
        emptyState={
          <RunEmptyState
            loadState={loadState}
            status={status}
            compact={compact}
            sourceCount={sourceCount}
            filterText={filterText}
          />
        }
        renderItem={(item, selected) => (
          <RunRow key={item.id} run={item.run} selected={selected} />
        )}
      />
    </Panel>
  );
}

function RunEmptyState({
  loadState,
  status,
  compact,
  sourceCount,
  filterText,
}: {
  loadState: LoadState;
  status: StatusMessage;
  compact: boolean;
  sourceCount: number;
  filterText: string;
}) {
  if (loadState === "loading" || loadState === "idle") {
    return (
      <PanelState
        tone="focus"
        title="Loading v2 runs..."
        detail="Press q to quit while the backend responds."
      />
    );
  }
  if (status.tone === "danger" || status.tone === "warning") {
    return (
      <PanelState
        tone={status.tone}
        title={truncate(status.text, compact ? 54 : 88)}
        detail="Start the API or press r to retry."
      />
    );
  }
  if (filterText.trim() !== "" && sourceCount > 0) {
    return (
      <PanelState
        tone="muted"
        title={`No runs match /${truncate(filterText, compact ? 40 : 72)}`}
        detail="Esc clears the filter."
      />
    );
  }
  return (
    <PanelState
      tone="muted"
      title="No v2 runs found."
      detail="Press r to refresh."
    />
  );
}

function RunRow({ run, selected }: { run: RunListItem; selected: boolean }) {
  const statusTone = statusToneForRun(run.status);
  return (
    <box
      style={{
        flexDirection: "column",
        paddingLeft: selected ? 0 : 2,
        backgroundColor: selected ? theme.panelAlt : theme.bg,
      }}
    >
      <text fg={selected ? theme.focus : theme.text}>
        {selected ? "> " : ""}
        {truncate(run.run_id, 32)}
      </text>
      <text fg={toneColor(statusTone)}>
        {"  "}
        {run.status.padEnd(10)} {truncate(run.command ?? "", 20)}
      </text>
    </box>
  );
}

function RoomTaskListPanel({
  compact,
  short,
  panelWidth,
  focused,
  sections,
  selectedId,
  status,
  loadState,
  lastLoadedAt,
  roomCount,
  sourceCount,
  filterText,
}: {
  compact: boolean;
  short: boolean;
  panelWidth: PanelWidth;
  focused: boolean;
  sections: WorklistSection<RoomTaskWorkItem>[];
  selectedId: string | null;
  status: StatusMessage;
  loadState: LoadState;
  lastLoadedAt: string | null;
  roomCount: number;
  sourceCount: number;
  filterText: string;
}) {
  const itemCount = sections.reduce(
    (total, section) => total + section.items.length,
    0,
  );
  const taskCount = countRoomTasks(sections.flatMap((section) => section.items));
  const stale = loadState === "error" && itemCount > 0;
  return (
    <Panel
      title="Rooms"
      subtitle={`${roomCount} rooms / ${taskCount} tasks`}
      focused={focused}
      width={panelWidth}
      minWidth={30}
      height={compact ? (short ? 8 : 12) : "100%"}
      footer={
        <text fg={stale ? theme.warning : theme.muted}>
          {runFooter(loadState, lastLoadedAt, stale)}
        </text>
      }
    >
      <GroupedWorklist
        sections={sections}
        selectedId={selectedId}
        emptyState={
          <RoomTaskEmptyState
            loadState={loadState}
            status={status}
            compact={compact}
            roomCount={roomCount}
            sourceCount={sourceCount}
            filterText={filterText}
          />
        }
        renderItem={(item, selected) => (
          <RoomTaskRow key={item.id} item={item} selected={selected} />
        )}
      />
    </Panel>
  );
}

function RoomTaskEmptyState({
  loadState,
  status,
  compact,
  roomCount,
  sourceCount,
  filterText,
}: {
  loadState: LoadState;
  status: StatusMessage;
  compact: boolean;
  roomCount: number;
  sourceCount: number;
  filterText: string;
}) {
  if (loadState === "loading" || loadState === "idle") {
    return (
      <PanelState
        tone="focus"
        title="Loading room tasks..."
        detail={`workspace ${WORKSPACE_ID}`}
      />
    );
  }
  if (status.tone === "danger" || status.tone === "warning") {
    return (
      <PanelState
        tone={status.tone}
        title={truncate(status.text, compact ? 54 : 88)}
        detail="Check FOXTERM_WORKSPACE_ID or press r to retry."
      />
    );
  }
  if (roomCount === 0) {
    return (
      <PanelState
        tone="muted"
        title="No rooms found."
        detail={`Press + to create one in ${WORKSPACE_ID}.`}
      />
    );
  }
  if (filterText.trim() !== "" && sourceCount > 0) {
    return (
      <PanelState
        tone="muted"
        title={`No room tasks match /${truncate(filterText, compact ? 34 : 66)}`}
        detail="Esc clears the filter."
      />
    );
  }
  return (
    <PanelState
      tone="muted"
      title="No room tasks found."
      detail="Press n for a context run, or + to create a room."
    />
  );
}

function RoomTaskRow({
  item,
  selected,
}: {
  item: RoomTaskWorkItem;
  selected: boolean;
}) {
  const tone = item.task ? taskToneForStatus(item.task.status) : "muted";
  return (
    <box
      style={{
        flexDirection: "column",
        paddingLeft: selected ? 0 : 2,
        backgroundColor: selected ? theme.panelAlt : theme.bg,
      }}
    >
      <text fg={selected ? theme.focus : theme.text}>
        {selected ? "> " : ""}
        {truncate(item.task?.title || item.room.title || item.room.id, 32)}
      </text>
      <text fg={toneColor(tone)}>
        {"  "}
        {(item.task?.status ?? "room").padEnd(12)} {truncate(item.room.id, 16)}
      </text>
    </box>
  );
}

function RoomTaskDetailPanel({
  compact,
  terminalWidth,
  focused,
  selectedItem,
  selectedId,
  selectedMissing,
  roomCount,
  messages,
  messagesLoadState,
  loop,
  loopLoadState,
  agents,
  agentLoadState,
  agentPaneSize,
  atcpSessions,
  atcpMembers,
  atcpReadiness,
  atcpScreens,
  selectedATCPSessionId,
  atcpLoadState,
  cards,
  cardLoadState,
}: {
  compact: boolean;
  terminalWidth: number;
  focused: boolean;
  selectedItem?: RoomTaskWorkItem;
  selectedId: string | null;
  selectedMissing: boolean;
  roomCount: number;
  messages: RoomMessage[];
  messagesLoadState: LoadState;
  loop: RoomLoop | null;
  loopLoadState: LoadState;
  agents: AgentSummary[];
  agentLoadState: LoadState;
  agentPaneSize: AgentPaneSize;
  atcpSessions: ATCPSession[];
  atcpMembers: ATCPMember[];
  atcpReadiness: Record<string, ATCPReadiness>;
  atcpScreens: Record<string, ATCPScreen>;
  selectedATCPSessionId: string | null;
  atcpLoadState: LoadState;
  cards: OrchestrationCardWorkItem[];
  cardLoadState: LoadState;
}) {
  return (
    <Panel
      title="Room task detail"
      subtitle={selectedItem?.task ? selectedItem.task.status : "room"}
      focused={focused}
      flexGrow={1}
      height={compact ? undefined : "100%"}
    >
      {selectedItem?.task ? (
        <scrollbox style={{ flexGrow: 1, marginTop: 1 }}>
          <box style={{ flexDirection: "column", gap: 1 }}>
            <text fg={theme.text}>room       {selectedItem.room.title}</text>
            <text fg={theme.muted}>room_id    {selectedItem.room.id}</text>
            <text fg={theme.text}>task       {selectedItem.task.title}</text>
            <text fg={theme.muted}>task_id    {selectedItem.task.id}</text>
            <text fg={theme.text}>status     {selectedItem.task.status}</text>
            <text fg={theme.muted}>
              owner      {selectedItem.task.owner_actor_id ?? "-"}
            </text>
            <text fg={theme.muted}>
              assigned   {selectedItem.task.assigned_actor_id ?? "-"}
            </text>
            <text fg={theme.muted}>
              scope      {selectedItem.task.scope_path ?? "-"}
            </text>
            {selectedItem.task.blocked_reason && (
              <text fg={theme.warning}>
                blocked   {truncate(selectedItem.task.blocked_reason, 88)}
              </text>
            )}
            <RoomTextPreview
              title="notes"
              value={
                selectedItem.task.notes ||
                selectedItem.task.description ||
                "No notes recorded."
              }
              compact={compact}
            />
            <RoomMessagesPreview
              messages={messages}
              loadState={messagesLoadState}
            />
            <RoomLoopPreview
              roomId={selectedItem.room.id}
              loop={loop}
              loadState={loopLoadState}
            />
            <RoomOrchestrationStrip
              compact={compact}
              room={selectedItem.room}
              taskId={selectedItem.task.id}
              cards={cards}
              loadState={cardLoadState}
            />
            <RoomAgentPanes
              compact={compact}
              terminalWidth={terminalWidth}
              room={selectedItem.room}
              agents={agents}
              agentLoadState={agentLoadState}
              agentPaneSize={agentPaneSize}
              sessions={atcpSessions}
              members={atcpMembers}
              readiness={atcpReadiness}
              screens={atcpScreens}
              selectedSessionId={selectedATCPSessionId}
              loadState={atcpLoadState}
            />
          </box>
        </scrollbox>
      ) : selectedMissing ? (
        <PanelState
          tone="warning"
          title={truncate(
            `Selected task is not in the current list: ${selectedId}`,
            88,
          )}
          detail="Move the selection or press r to refresh."
        />
      ) : (
        <scrollbox style={{ flexGrow: 1, marginTop: 1 }}>
          <RoomSummaryDetail
            compact={compact}
            terminalWidth={terminalWidth}
            selectedItem={selectedItem}
            roomCount={roomCount}
            messages={messages}
            messagesLoadState={messagesLoadState}
            loop={loop}
            loopLoadState={loopLoadState}
            agents={agents}
            agentLoadState={agentLoadState}
            agentPaneSize={agentPaneSize}
            atcpSessions={atcpSessions}
            atcpMembers={atcpMembers}
            atcpReadiness={atcpReadiness}
            atcpScreens={atcpScreens}
            selectedATCPSessionId={selectedATCPSessionId}
            atcpLoadState={atcpLoadState}
            cards={cards}
            cardLoadState={cardLoadState}
          />
        </scrollbox>
      )}
    </Panel>
  );
}

function RoomSummaryDetail({
  compact,
  terminalWidth,
  selectedItem,
  roomCount,
  messages,
  messagesLoadState,
  loop,
  loopLoadState,
  agents,
  agentLoadState,
  agentPaneSize,
  atcpSessions,
  atcpMembers,
  atcpReadiness,
  atcpScreens,
  selectedATCPSessionId,
  atcpLoadState,
  cards,
  cardLoadState,
}: {
  compact: boolean;
  terminalWidth: number;
  selectedItem?: RoomTaskWorkItem;
  roomCount: number;
  messages: RoomMessage[];
  messagesLoadState: LoadState;
  loop: RoomLoop | null;
  loopLoadState: LoadState;
  agents: AgentSummary[];
  agentLoadState: LoadState;
  agentPaneSize: AgentPaneSize;
  atcpSessions: ATCPSession[];
  atcpMembers: ATCPMember[];
  atcpReadiness: Record<string, ATCPReadiness>;
  atcpScreens: Record<string, ATCPScreen>;
  selectedATCPSessionId: string | null;
  atcpLoadState: LoadState;
  cards: OrchestrationCardWorkItem[];
  cardLoadState: LoadState;
}) {
  if (!selectedItem) {
    return (
      <PanelState
        tone="muted"
        title="Select a room or room task to inspect it."
        detail={`${roomCount} rooms loaded`}
      />
    );
  }
  return (
    <box style={{ flexDirection: "column", gap: 1 }}>
      <text fg={theme.text}>room       {selectedItem.room.title}</text>
      <text fg={theme.muted}>room_id    {selectedItem.room.id}</text>
      <text fg={theme.text}>
        members   {selectedItem.room.members?.length ?? 0}
      </text>
      <text fg={theme.muted}>
        agents    {truncate(roomMemberSummary(selectedItem.room), 88)}
      </text>
      <text fg={theme.text}>messages   {selectedItem.room.message_count}</text>
      <text fg={theme.text}>unread     {selectedItem.room.unread_count}</text>
      <text fg={theme.muted}>
        latest    {truncate(selectedItem.room.latest_subject ?? "-", 88)}
      </text>
      <text fg={theme.muted}>
        sender    {selectedItem.room.latest_sender ?? "-"}
      </text>
      <RoomTextPreview
        title="preview"
        value={
          selectedItem.room.latest_preview ||
          selectedItem.room.description ||
          "No linked tasks in this room yet."
        }
        compact={compact}
      />
      <RoomMessagesPreview messages={messages} loadState={messagesLoadState} />
      <RoomLoopPreview
        roomId={selectedItem.room.id}
        loop={loop}
        loadState={loopLoadState}
      />
      <RoomOrchestrationStrip
        compact={compact}
        room={selectedItem.room}
        cards={cards}
        loadState={cardLoadState}
      />
      <RoomAgentPanes
        compact={compact}
        terminalWidth={terminalWidth}
        room={selectedItem.room}
        agents={agents}
        agentLoadState={agentLoadState}
        agentPaneSize={agentPaneSize}
        sessions={atcpSessions}
        members={atcpMembers}
        readiness={atcpReadiness}
        screens={atcpScreens}
        selectedSessionId={selectedATCPSessionId}
        loadState={atcpLoadState}
      />
    </box>
  );
}

function RoomTextPreview({
  title,
  value,
  compact,
}: {
  title: string;
  value: string;
  compact: boolean;
}) {
  const width = compact ? 74 : 118;
  return (
    <box style={{ flexDirection: "column", height: 4 }}>
      <text fg={theme.focus}>{title}</text>
      <text fg={theme.text}>{truncate(value, width)}</text>
      <text fg={theme.subtle}>{truncate(value.slice(width), width)}</text>
    </box>
  );
}

function RoomOrchestrationStrip({
  compact,
  room,
  taskId,
  cards,
  loadState,
}: {
  compact: boolean;
  room: RoomTaskWorkItem["room"];
  taskId?: string;
  cards: OrchestrationCardWorkItem[];
  loadState: LoadState;
}) {
  const linkedCards = roomLinkedCards(cards, room, taskId);
  const activeCards = cards
    .filter((item) => isActiveCard(item.card))
    .filter((item) => !linkedCards.some((linked) => linked.id === item.id))
    .slice(0, compact ? 2 : 4);
  const visibleCards = [...linkedCards, ...activeCards].slice(0, compact ? 3 : 6);
  const summary = roomBoardSummary(cards);
  return (
    <box style={{ flexDirection: "column", gap: 1 }}>
      <text fg={loadState === "loading" ? theme.focus : theme.focus}>
        room orchestration
      </text>
      <text fg={theme.muted}>
        {loadState === "loading"
          ? "loading board activity"
          : summary || "no cards loaded"}
      </text>
      {visibleCards.length === 0 ? (
        <text fg={theme.muted}>
          {loadState === "error"
            ? "card board unavailable"
            : "+ in Cards creates board work; linked cards appear here."}
        </text>
      ) : (
        visibleCards.map((item) => (
          <box
            key={`room-card:${item.id}`}
            style={{
              flexDirection: "row",
              justifyContent: "space-between",
              height: 1,
            }}
          >
            <text fg={toneColor(cardToneForLane(item.laneId, item.card.state))}>
              {truncate(
                `${item.laneId} ${item.card.issue_identifier || item.card.issue_id}`,
                compact ? 34 : 52,
              )}
            </text>
            <text fg={cardPolicyTone(item.card)}>
              {truncate(cardRoomCardMeta(item), compact ? 28 : 52)}
            </text>
          </box>
        ))
      )}
    </box>
  );
}

interface RoomAgentPane {
  id: string;
  title: string;
  kind: "daemon" | "cli";
  status: string;
  tone: string;
  meta: string;
  detail: string;
  selected?: boolean;
  screenLines?: string[];
}

function RoomAgentPanes({
  compact,
  terminalWidth,
  room,
  agents,
  agentLoadState,
  agentPaneSize,
  sessions,
  members,
  readiness,
  screens,
  selectedSessionId,
  loadState,
}: {
  compact: boolean;
  terminalWidth: number;
  room: RoomTaskWorkItem["room"];
  agents: AgentSummary[];
  agentLoadState: LoadState;
  agentPaneSize: AgentPaneSize;
  sessions: ATCPSession[];
  members: ATCPMember[];
  readiness: Record<string, ATCPReadiness>;
  screens: Record<string, ATCPScreen>;
  selectedSessionId: string | null;
  loadState: LoadState;
}) {
  const availableWidth = detailPanelContentWidth(terminalWidth, compact);
  const columnCount = agentPaneColumnCount(
    availableWidth,
    compact,
    agentPaneSize,
  );
  const paneWidth = Math.floor((availableWidth - (columnCount - 1)) / columnCount);
  const normalPaneHeight = agentPaneHeight(agentPaneSize, false);
  const focusedPaneHeight = agentPaneHeight(agentPaneSize, true);
  const screenLineLimit = agentPaneScreenLineLimit(agentPaneSize, compact);
  const daemonPanes = buildDaemonAgentPanes(room, agents, compact);
  const cliPanes = buildATCPAgentPanes({
    compact,
    sessions,
    members,
    readiness,
    screens,
    selectedSessionId,
    screenLineLimit,
  });
  const panes = [...daemonPanes, ...cliPanes];
  const paneRows = chunkItems(panes, columnCount);
  const title =
    agentLoadState === "loading" || loadState === "loading"
      ? "agent panes loading"
      : agentLoadState === "error" && loadState === "error"
        ? "agent panes unavailable"
        : panes.length === 0
          ? "agent panes empty"
          : "agent panes";
  return (
    <box style={{ flexDirection: "column", gap: 1 }}>
      <text
        fg={
          agentLoadState === "error" || loadState === "error"
            ? theme.warning
            : theme.focus
        }
      >
        {title}
      </text>
      {panes.length === 0 ? (
        <text
          fg={
            agentLoadState === "error" || loadState === "error"
              ? theme.warning
              : theme.muted
          }
        >
          {loadState === "error"
            ? ATCP_DAEMON_HINT
            : "s starts a daemon agent; Shift+s starts a CLI pane."}
        </text>
      ) : (
        paneRows.map((row, rowIndex) => (
          <box
            key={`agent-pane-row-${rowIndex}`}
            style={{
              flexDirection: compact ? "column" : "row",
              gap: 1,
              height: row.some((pane) => pane.selected && pane.screenLines?.length)
                ? focusedPaneHeight
                : normalPaneHeight,
            }}
          >
            {row.map((pane) => (
              <AgentPane
                key={pane.id}
                pane={pane}
                width={paneWidth}
                height={
                  pane.selected && pane.screenLines?.length
                    ? focusedPaneHeight
                    : normalPaneHeight
                }
              />
            ))}
          </box>
        ))
      )}
      {cliPanes.length > 0 && (
        <text fg={theme.muted}>
          {agentPaneSize} panes  - shrink  = grow  j/k focus  Enter screen
        </text>
      )}
    </box>
  );
}

function AgentPane({
  pane,
  width,
  height,
}: {
  pane: RoomAgentPane;
  width: number;
  height: number;
}) {
  const bodyLines = pane.screenLines?.length
    ? pane.screenLines
    : [pane.detail].filter(Boolean);
  return (
    <box
      style={{
        width,
        minWidth: 32,
        flexGrow: 1,
        height,
        border: true,
        borderStyle: "single",
        borderColor: pane.selected ? theme.focus : theme.borderMuted,
        backgroundColor: pane.selected ? theme.panelAlt : theme.bg,
        flexDirection: "column",
        paddingLeft: 1,
        paddingRight: 1,
      }}
    >
      <box
        style={{
          flexDirection: "row",
          justifyContent: "space-between",
          height: 1,
        }}
      >
        <text fg={pane.selected ? theme.focus : theme.text}>
          {truncate(pane.title, width - 18)}
        </text>
        <text fg={pane.tone}>{truncate(pane.status, 14)}</text>
      </box>
      <text fg={theme.muted}>{truncate(pane.meta, width - 4)}</text>
      {bodyLines.map((line, index) => (
        <text
          key={`${pane.id}:body:${index}`}
          fg={pane.screenLines?.length ? theme.text : theme.subtle}
        >
          {truncate(line, width - 4)}
        </text>
      ))}
    </box>
  );
}

function buildDaemonAgentPanes(
  room: RoomTaskWorkItem["room"],
  agents: AgentSummary[],
  compact: boolean,
): RoomAgentPane[] {
  const byID = new Map(agents.map((agent) => [agent.id, agent]));
  return (room.members ?? [])
    .map((member) => ({
      member,
      agent: byID.get(member.actor_id),
    }))
    .filter((row) => row.agent || row.member.actor_id !== ACTOR_ID)
    .slice(0, compact ? 3 : 4)
    .map(({ member, agent }) => ({
      id: `daemon:${member.actor_id}`,
      title: shortActorLabel(member.actor_id),
      kind: "daemon" as const,
      status: agent?.state ?? "member",
      tone: agentStateTone(agent?.state),
      meta: `daemon ${member.role || agent?.role || "-"}`,
      detail: agentModelLabel(agent),
    }));
}

function buildATCPAgentPanes({
  compact,
  sessions,
  members,
  readiness,
  screens,
  selectedSessionId,
  screenLineLimit,
}: {
  compact: boolean;
  sessions: ATCPSession[];
  members: ATCPMember[];
  readiness: Record<string, ATCPReadiness>;
  screens: Record<string, ATCPScreen>;
  selectedSessionId: string | null;
  screenLineLimit: number;
}): RoomAgentPane[] {
  const memberBySession = new Map(
    members.map((member) => [member.session_id, member]),
  );
  const selected =
    sessions.find((session) => session.id === selectedSessionId) ?? sessions[0];
  return sessions.slice(0, compact ? 3 : 4).map((session) => {
    const member = memberBySession.get(session.id);
    const ready = readiness[session.id];
    const selectedPane = session.id === selected?.id;
    const screenLines = atcpScreenLines(
      screens[session.id],
      selectedPane ? screenLineLimit : 1,
    ).map((line) => `| ${line}`);
    const status = `${session.status} ${ready?.idle ? "idle" : "busy"}`;
    return {
      id: `cli:${session.id}`,
      title: member?.agent_id ?? session.id,
      kind: "cli",
      status,
      tone: atcpSessionTone(session, ready),
      meta: `cli ${session.adapter || "terminal"}`,
      detail: session.cmd?.join(" ") || "-",
      selected: selectedPane,
      screenLines: selectedPane ? screenLines : undefined,
    };
  });
}

function chunkItems<T>(items: T[], size: number): T[][] {
  const out: T[][] = [];
  for (let index = 0; index < items.length; index += size) {
    out.push(items.slice(index, index + size));
  }
  return out;
}

function shortActorLabel(actorID: string): string {
  const separator = actorID.lastIndexOf(":");
  if (separator >= 0 && separator < actorID.length - 1) {
    return actorID.slice(separator + 1);
  }
  return actorID;
}

function nextAgentPaneSize(current: AgentPaneSize, delta: number): AgentPaneSize {
  const order: AgentPaneSize[] = ["small", "medium", "large"];
  const index = order.indexOf(current);
  const safeIndex = index < 0 ? 1 : index;
  return order[clampInt(safeIndex + delta, 0, order.length - 1)] ?? "medium";
}

function nextMainPaneSize(current: MainPaneSize, delta: number): MainPaneSize {
  const order: MainPaneSize[] = ["small", "medium", "large"];
  const index = order.indexOf(current);
  const safeIndex = index < 0 ? 1 : index;
  return order[clampInt(safeIndex + delta, 0, order.length - 1)] ?? "medium";
}

function mainScopePaneWidth(size: MainPaneSize): number {
  switch (size) {
    case "small":
      return 18;
    case "large":
      return 30;
    case "medium":
    default:
      return 24;
  }
}

function mainWorklistPaneWidth(compact: boolean, size: MainPaneSize): PanelWidth {
  if (compact) return "100%";
  switch (size) {
    case "small":
      return 32;
    case "large":
      return 54;
    case "medium":
    default:
      return 42;
  }
}

function agentPaneColumnCount(
  availableWidth: number,
  compact: boolean,
  size: AgentPaneSize,
): number {
  if (compact) return 1;
  if (size === "large") return availableWidth >= 112 ? 2 : 1;
  if (size === "small") return availableWidth >= 132 ? 3 : 2;
  return availableWidth >= 132 ? 3 : 2;
}

function agentPaneHeight(size: AgentPaneSize, focused: boolean): number {
  if (size === "large") return focused ? 11 : 8;
  if (size === "small") return focused ? 6 : 5;
  return focused ? 8 : 6;
}

function agentPaneScreenLineLimit(
  size: AgentPaneSize,
  compact: boolean,
): number {
  if (size === "large") return compact ? 4 : 6;
  if (size === "small") return 1;
  return compact ? 2 : 3;
}

function detailPanelContentWidth(terminalWidth: number, compact: boolean): number {
  if (compact) {
    return clampInt(terminalWidth - 8, 42, 80);
  }
  return clampInt(terminalWidth - 86, 80, 160);
}

function clampInt(value: number, min: number, max: number): number {
  return Math.max(min, Math.min(max, value));
}

function RoomLoopPreview({
  roomId,
  loop,
  loadState,
}: {
  roomId: string;
  loop: RoomLoop | null;
  loadState: LoadState;
}) {
  const health = roomLoopHealth(loop, loadState);
  const startCommand = roomLoopStartCommand(roomId);
  return (
    <box style={{ flexDirection: "column", gap: 1 }}>
      <text fg={toneColor(health.tone)}>loop       {health.text}</text>
      {loop?.delivery_owner_id && (
        <text fg={theme.muted}>
          owner      {truncate(loop.delivery_owner_id, 70)}
        </text>
      )}
      {loop?.last_tick_at && (
        <text fg={theme.muted}>
          last tick  {truncate(loop.last_tick_at, 70)}
        </text>
      )}
      {loop?.last_delivery_trace?.outcome && (
        <text fg={theme.muted}>
          delivery   {truncate(loop.last_delivery_trace.outcome, 70)}
        </text>
      )}
      {health.tone !== "success" && (
        <text fg={theme.warning}>{truncate(startCommand, 110)}</text>
      )}
    </box>
  );
}

function RoomMessagesPreview({
  messages,
  loadState,
}: {
  messages: RoomMessage[];
  loadState: LoadState;
}) {
  const recent = messages.slice(0, 4);
  const title =
    loadState === "loading"
      ? "messages loading"
      : loadState === "error"
        ? "messages unavailable"
        : recent.length === 0
          ? "messages empty"
          : "recent messages";
  return (
    <box style={{ flexDirection: "column", gap: 1 }}>
      <text fg={loadState === "error" ? theme.warning : theme.focus}>
        {title}
      </text>
      {recent.length === 0 ? (
        <text fg={theme.muted}>Press m to write into this room.</text>
      ) : (
        recent.map((message) => (
          <box key={message.id} style={{ flexDirection: "column" }}>
            <text fg={theme.muted}>
              {truncate(message.sender, 18)} {"->"}{" "}
              {truncate(message.recipient, 18)} {message.kind}
            </text>
            <text fg={theme.text}>{truncate(message.body, 110)}</text>
          </box>
        ))
      )}
    </box>
  );
}

function CardListPanel({
  compact,
  short,
  panelWidth,
  focused,
  sections,
  selectedId,
  status,
  loadState,
  lastLoadedAt,
  artifact,
  sourceCount,
  filterText,
}: {
  compact: boolean;
  short: boolean;
  panelWidth: PanelWidth;
  focused: boolean;
  sections: WorklistSection<OrchestrationCardWorkItem>[];
  selectedId: string | null;
  status: StatusMessage;
  loadState: LoadState;
  lastLoadedAt: string | null;
  artifact: string | null;
  sourceCount: number;
  filterText: string;
}) {
  const cardCount = sections.reduce(
    (total, section) => total + section.items.length,
    0,
  );
  const stale = loadState === "error" && cardCount > 0;
  return (
    <Panel
      title="Cards"
      subtitle={artifact ? "artifact" : `${cardCount} total`}
      focused={focused}
      width={panelWidth}
      minWidth={30}
      height={compact ? (short ? 8 : 12) : "100%"}
      footer={
        <text fg={stale || artifact ? theme.warning : theme.muted}>
          {artifact
            ? `stored ${truncate(artifact, 22)}`
            : runFooter(loadState, lastLoadedAt, stale)}
        </text>
      }
    >
      <GroupedWorklist
        sections={sections}
        selectedId={selectedId}
        emptyState={
          <CardEmptyState
            loadState={loadState}
            status={status}
            compact={compact}
            artifact={artifact}
            sourceCount={sourceCount}
            filterText={filterText}
          />
        }
        renderItem={(item, selected) => (
          <CardRow key={item.id} item={item} selected={selected} />
        )}
      />
    </Panel>
  );
}

function CardEmptyState({
  loadState,
  status,
  compact,
  artifact,
  sourceCount,
  filterText,
}: {
  loadState: LoadState;
  status: StatusMessage;
  compact: boolean;
  artifact: string | null;
  sourceCount: number;
  filterText: string;
}) {
  if (loadState === "loading" || loadState === "idle") {
    return (
      <PanelState
        tone="focus"
        title="Loading orchestration cards..."
        detail={`workspace ${WORKSPACE_ID}`}
      />
    );
  }
  if (artifact) {
    return (
      <PanelState
        tone="warning"
        title="Board payload is stored as a CAS artifact."
        detail={truncate(artifact, compact ? 54 : 88)}
      />
    );
  }
  if (status.tone === "danger" || status.tone === "warning") {
    return (
      <PanelState
        tone={status.tone}
        title={truncate(status.text, compact ? 54 : 88)}
        detail="Press r to retry."
      />
    );
  }
  if (filterText.trim() !== "" && sourceCount > 0) {
    return (
      <PanelState
        tone="muted"
        title={`No cards match /${truncate(filterText, compact ? 40 : 72)}`}
        detail="Esc clears the filter."
      />
    );
  }
  return (
    <PanelState
      tone="muted"
      title="No orchestration cards found."
      detail="Press + to create a card, or n to start a board-context run."
    />
  );
}

function CardRow({
  item,
  selected,
}: {
  item: OrchestrationCardWorkItem;
  selected: boolean;
}) {
  const tone = cardToneForLane(item.laneId, item.card.state);
  const label = item.card.issue_identifier || item.card.issue_id;
  const policy = cardPolicySummary(item.card);
  const runtime = [
    item.card.run_id ? "run" : "",
    item.card.agent_id ? "agent" : "",
    typeof item.card.attempt === "number" && item.card.attempt > 0
      ? `try ${item.card.attempt}`
      : "",
  ]
    .filter(Boolean)
    .join(" ");
  return (
    <box
      style={{
        flexDirection: "column",
        paddingLeft: selected ? 0 : 2,
        backgroundColor: selected ? theme.panelAlt : theme.bg,
      }}
    >
      <text fg={selected ? theme.focus : theme.text}>
        {selected ? "> " : ""}
        {truncate(item.card.title || label, 38)}
      </text>
      <text fg={toneColor(tone)}>
        {"  "}
        {item.laneId.padEnd(12)} {truncate(label, 18)}
      </text>
      <text fg={cardPolicyTone(item.card)}>
        {"  "}
        {truncate([policy, runtime].filter(Boolean).join("  "), 42)}
      </text>
    </box>
  );
}

function CardDetailPanel({
  compact,
  focused,
  selectedItem,
  selectedId,
  selectedMissing,
  artifact,
}: {
  compact: boolean;
  focused: boolean;
  selectedItem?: OrchestrationCardWorkItem;
  selectedId: string | null;
  selectedMissing: boolean;
  artifact: string | null;
}) {
  const actionSummary = selectedItem
    ? cardAvailableActionSummary(selectedItem.card)
    : "";
  return (
    <Panel
      title="Card detail"
      subtitle={selectedItem ? selectedItem.laneTitle : undefined}
      focused={focused}
      flexGrow={1}
      height={compact ? undefined : "100%"}
    >
      {selectedItem ? (
        <scrollbox style={{ flexGrow: 1 }}>
          <box style={{ flexDirection: "column", gap: 1, marginTop: 1 }}>
            <text fg={theme.text}>
              title      {truncate(selectedItem.card.title ?? "-", compact ? 54 : 110)}
            </text>
            <text fg={theme.muted}>
              issue      {selectedItem.card.issue_identifier ?? selectedItem.card.issue_id}
            </text>
            <text fg={toneColor(cardToneForLane(selectedItem.laneId, selectedItem.card.state))}>
              lane       {selectedItem.laneTitle} / {selectedItem.card.state}
            </text>
            <text fg={cardPolicyTone(selectedItem.card)}>
              policy     {cardPolicySummary(selectedItem.card)}
            </text>
            <text fg={theme.muted}>
              outcome    {selectedItem.card.last_outcome || "-"}
            </text>
            <text fg={theme.muted}>
              event      {selectedItem.card.last_event_type || "-"}
            </text>
            <text fg={theme.muted}>
              updated    {selectedItem.card.last_event_at || "-"}
            </text>
            <text fg={theme.muted}>
              attempt    {typeof selectedItem.card.attempt === "number" ? selectedItem.card.attempt : "-"}
            </text>
            <text fg={theme.muted}>
              retry_due  {selectedItem.card.retry_due_at || "-"}
            </text>
            <text fg={selectedItem.card.run_id ? theme.focus : theme.muted}>
              run        {selectedItem.card.run_id || "-"}
            </text>
            <text fg={theme.muted}>
              agent      {selectedItem.card.agent_id || "-"}
            </text>
            <text fg={theme.muted}>
              actor      {selectedItem.card.actor_id || "-"}
            </text>
            <text fg={theme.focus}>actions</text>
            <text fg={theme.text}>
              {truncate(
                [
                  selectedItem.card.run_id ? "Enter linked run" : "",
                  actionSummary,
                  "n context run",
                ]
                  .filter(Boolean)
                  .join("  "),
                compact ? 68 : 120,
              )}
            </text>
            <text fg={theme.focus}>status detail</text>
            <text fg={theme.text}>
              {truncate(cardStatusDetail(selectedItem.card), compact ? 68 : 120)}
            </text>
            <text fg={theme.subtle}>
              {truncate(cardStatusDetail(selectedItem.card).slice(compact ? 68 : 120), compact ? 68 : 120)}
            </text>
          </box>
        </scrollbox>
      ) : selectedMissing ? (
        <PanelState
          tone="warning"
          title={truncate(
            `Selected card is not in the current list: ${selectedId}`,
            88,
          )}
          detail="Move the selection or press r to refresh."
        />
      ) : artifact ? (
        <PanelState
          tone="warning"
          title="Board payload moved to CAS."
          detail={truncate(artifact, compact ? 72 : 100)}
        />
      ) : (
        <PanelState
          tone="muted"
          title="Select an orchestration card to inspect it."
        />
      )}
    </Panel>
  );
}

function RunDetailPanel({
  compact,
  short,
  focused,
  selectedRun,
  selectedRunId,
  selectedRunMissing,
  events,
  transcriptItems,
  transcriptLoadState,
  activityScope,
  activeSection,
}: {
  compact: boolean;
  short: boolean;
  focused: boolean;
  selectedRun?: RunListItem;
  selectedRunId: string | null;
  selectedRunMissing: boolean;
  events: V2RuntimeEvent[];
  transcriptItems: V2RunTranscriptItem[];
  transcriptLoadState: LoadState;
  activityScope: ActivityScope;
  activeSection: string;
}) {
  const scopedEvents = useMemo(
    () => filterEventsForActivity(events, activityScope),
    [events, activityScope],
  );
  const eventLines = useMemo(
    () => compactEvents(scopedEvents, activityScope),
    [activityScope, scopedEvents],
  );
  const output = useMemo(() => latestRunOutput(events), [events]);

  return (
    <Panel
      title={`${activeSection} terminal`}
      subtitle={selectedRun ? selectedRun.status : undefined}
      focused={focused}
      flexGrow={1}
      height={compact ? undefined : "100%"}
    >
      {selectedRun ? (
        <box style={{ flexDirection: "column", gap: 1, marginTop: 1 }}>
          <text fg={theme.text}>run_id     {selectedRun.run_id}</text>
          <text fg={theme.text}>status     {selectedRun.status}</text>
          {!compact && (
            <>
              <text fg={theme.muted}>
                request   {selectedRun.request_id ?? "-"}
              </text>
              <text fg={theme.muted}>actor     {selectedRun.actor_id ?? "-"}</text>
              <text fg={theme.muted}>
                actions   n new / c continue /{" "}
                <span fg={canKillRunStatus(selectedRun.status) ? theme.warning : theme.subtle}>
                  x kill
                </span>
              </text>
            </>
          )}
          <text fg={theme.focus}>transcript</text>
          <scrollbox style={{ flexGrow: 1 }}>
            {transcriptItems.length > 0 ? (
              <RunTranscriptLines items={transcriptItems} compact={compact} />
            ) : output ? (
              <RunOutputLines output={output} compact={compact} />
            ) : (
              <text fg={theme.muted}>
                {transcriptLoadState === "loading"
                  ? "Loading transcript."
                  : "Waiting for transcript activity."}
              </text>
            )}
          </scrollbox>
          {!short && (
            <>
              <text fg={theme.focus}>activity {activityScope}</text>
              <box style={{ height: compact ? 5 : 8, flexDirection: "column" }}>
                <scrollbox style={{ flexGrow: 1 }}>
                  {eventLines.length === 0 ? (
                    <text fg={theme.muted}>
                      {events.length === 0
                        ? "Waiting for replay or live events."
                        : "No events in this activity scope."}
                    </text>
                  ) : (
                    eventLines.map((line, index) => (
                      <text key={`${line.key}-${index}`} fg={toneColor(line.tone)}>
                        {truncate(
                          `${line.text}${line.repeat > 1 ? ` x${line.repeat}` : ""}`,
                          92,
                        )}
                      </text>
                    ))
                  )}
                </scrollbox>
              </box>
            </>
          )}
        </box>
      ) : selectedRunMissing ? (
        <PanelState
          tone="warning"
          title={truncate(
            `Selected run is not in the current list: ${selectedRunId}`,
            88,
          )}
          detail="Move the selection or press r to refresh."
        />
      ) : (
        <PanelState
          tone="muted"
          title="Select a run to inspect the live stream."
        />
      )}
    </Panel>
  );
}

function RunTranscriptLines({
  items,
  compact,
}: {
  items: V2RunTranscriptItem[];
  compact: boolean;
}) {
  const maxLines = compact ? 10 : 18;
  return (
    <>
      {items.slice(0, maxLines).map((item) => (
        <box key={item.id} style={{ flexDirection: "column" }}>
          <text fg={transcriptTone(item)}>
            {transcriptPrefix(item)} {truncate(item.title || item.kind, 28)}
          </text>
          {item.text && (
            <text fg={theme.text}>  {truncate(item.text, compact ? 72 : 110)}</text>
          )}
        </box>
      ))}
      {items.length > maxLines && (
        <text fg={theme.muted}>transcript truncated in foxterm view</text>
      )}
    </>
  );
}

function RunOutputLines({
  output,
  compact,
}: {
  output: RunOutputSummary;
  compact: boolean;
}) {
  const maxLines = compact ? 8 : 16;
  const lines = output.summary
    .split(/\r?\n/)
    .map((line) => line.trimEnd())
    .filter((line) => line.trim() !== "")
    .slice(0, maxLines);
  return (
    <>
      {output.turnID && <text fg={theme.muted}>turn      {output.turnID}</text>}
      {typeof output.iterations === "number" && (
        <text fg={theme.muted}>
          stats     {output.iterations} iterations / {output.toolCalls ?? 0} tools
        </text>
      )}
      {lines.map((line, index) => (
        <text key={`${output.turnID ?? "summary"}-${index}`} fg={theme.text}>
          {truncate(line, compact ? 72 : 110)}
        </text>
      ))}
      {lines.length === maxLines && (
        <text fg={theme.muted}>output truncated in foxterm view</text>
      )}
    </>
  );
}

function transcriptTone(item: V2RunTranscriptItem): string {
  switch (item.role) {
    case "user":
      return theme.focus;
    case "assistant":
      return theme.success;
    case "tool":
      return theme.warning;
    case "system":
    default:
      return item.kind === "error" ? theme.danger : theme.muted;
  }
}

function transcriptPrefix(item: V2RunTranscriptItem): string {
  switch (item.role) {
    case "user":
      return "you";
    case "assistant":
      return "agent";
    case "tool":
      return "tool";
    case "system":
    default:
      return "sys";
  }
}

function spinnerFrame(tick: number): string {
  const frames = ["-", "\\", "|", "/"];
  return frames[tick % frames.length] ?? "-";
}

function modelEndpointLabel(endpoint: V2ModelEndpoint | null): string {
  if (!endpoint) return "model endpoint unknown";
  const provider = endpoint.provider || "default";
  const model = endpoint.model || "default";
  const baseURL = endpoint.base_url || "backend default";
  return `${provider}/${model} @ ${baseURL}`;
}

function roomMessageErrorText(error: unknown, roomId: string): string {
  const detail =
    error instanceof Error ? error.message : "failed to send room message";
  return `${detail} Start loop: ${roomLoopStartCommand(roomId)}`;
}

function atcpErrorText(error: unknown, fallback: string): string {
  const detail = error instanceof Error ? error.message : fallback;
  if (detail.includes("atcp daemon unavailable") || detail.includes("/api/atcp")) {
    return ATCP_DAEMON_HINT;
  }
  return detail;
}

function parseAgentSpawnDraft(raw: string): { role: string; prompt?: string } {
  const trimmed = raw.trim();
  if (trimmed === "") return { role: "researcher" };
  const separator = trimmed.indexOf(":");
  if (separator < 0) return { role: trimmed };
  const role = trimmed.slice(0, separator).trim() || "researcher";
  const prompt = trimmed.slice(separator + 1).trim();
  return prompt ? { role, prompt } : { role };
}

function cliPresetDescription(preset: CLIPreset): string {
  if (preset.id === "custom") return "agent@adapter: command args";
  return `${preset.agentId}@${preset.adapter}: ${preset.cmd.join(" ")}`;
}

function cliSpawnComposerValue(
  step: "preset" | "fields",
  form: CLISpawnForm,
  field: keyof CLISpawnForm,
): string {
  if (step === "preset") return "choose a CLI preset";
  const label =
    field === "agentId"
      ? "agent"
      : field === "adapter"
        ? "adapter"
        : field === "command"
          ? "command"
          : "raw";
  return `${label} ${form[field]}`;
}

function nextCLISpawnField(
  current: keyof CLISpawnForm,
  reverse?: boolean,
): keyof CLISpawnForm {
  const order: Array<keyof CLISpawnForm> = [
    "agentId",
    "adapter",
    "command",
    "raw",
  ];
  const index = order.indexOf(current);
  const safeIndex = index < 0 ? 0 : index;
  const delta = reverse ? -1 : 1;
  return order[(safeIndex + delta + order.length) % order.length] ?? "agentId";
}

function movePresetSelection(
  delta: number,
  setSelected: (updater: (current: number) => number) => void,
): void {
  setSelected(
    (current) => (current + delta + cliPresets.length) % cliPresets.length,
  );
}

function updateCLISpawnField(
  setForm: (updater: (current: CLISpawnForm) => CLISpawnForm) => void,
  field: keyof CLISpawnForm,
  update: (current: string) => string,
): void {
  setForm((current) => {
    const nextValue = update(current[field]);
    if (field === "raw") {
      return { ...current, raw: nextValue };
    }
    return { ...current, [field]: nextValue, raw: "" };
  });
}

function formFromCLIPreset(preset: CLIPreset | undefined): CLISpawnForm {
  const safe = preset ?? cliPresets[0];
  const command = safe.cmd.join(" ");
  return {
    agentId: safe.agentId,
    adapter: safe.adapter,
    command,
    raw:
      safe.id === "custom" ? `${safe.agentId}@${safe.adapter}: ${command}` : "",
  };
}

function draftFromCLIPreset(preset: CLIPreset | undefined): CLISpawnDraft {
  const form = formFromCLIPreset(preset);
  return draftFromCLIForm(form);
}

function draftFromCLIForm(form: CLISpawnForm): CLISpawnDraft {
  if (form.raw.trim() !== "") {
    return parseCLISpawnDraft(form.raw);
  }
  const agentId = form.agentId.trim();
  const adapter = form.adapter.trim();
  if (agentId === "" || adapter === "") {
    throw new Error("agent and adapter are required");
  }
  const cmd = parseCommandLine(form.command.trim());
  if (cmd.length === 0) {
    throw new Error("command is required");
  }
  return { agentId, adapter, cmd };
}

function parseCLISpawnDraft(raw: string): CLISpawnDraft {
  const trimmed = raw.trim();
  const separator = trimmed.indexOf(":");
  if (separator < 1) {
    throw new Error("use agent@adapter: command args");
  }
  const left = trimmed.slice(0, separator).trim();
  const command = trimmed.slice(separator + 1).trim();
  if (left === "" || command === "") {
    throw new Error("use agent@adapter: command args");
  }
  const at = left.indexOf("@");
  const agentId = (at >= 0 ? left.slice(0, at) : left).trim();
  const adapter = (at >= 0 ? left.slice(at + 1) : left).trim();
  if (agentId === "" || adapter === "") {
    throw new Error("agent and adapter are required");
  }
  const cmd = parseCommandLine(command);
  if (cmd.length === 0) {
    throw new Error("command is required");
  }
  return { agentId, adapter, cmd };
}

function parseCommandLine(input: string): string[] {
  const out: string[] = [];
  let current = "";
  let quote: "'" | '"' | null = null;
  let escaped = false;
  for (const char of input) {
    if (escaped) {
      current += char;
      escaped = false;
      continue;
    }
    if (char === "\\") {
      escaped = true;
      continue;
    }
    if (quote) {
      if (char === quote) {
        quote = null;
      } else {
        current += char;
      }
      continue;
    }
    if (char === "'" || char === '"') {
      quote = char;
      continue;
    }
    if (/\s/.test(char)) {
      if (current !== "") {
        out.push(current);
        current = "";
      }
      continue;
    }
    current += char;
  }
  if (escaped) current += "\\";
  if (quote) throw new Error("unterminated quote in command");
  if (current !== "") out.push(current);
  return out;
}

function roomLoopHealth(
  loop: RoomLoop | null,
  loadState: LoadState,
): { tone: Tone; text: string } {
  if (loadState === "loading" || loadState === "idle") {
    return { tone: "focus", text: "loading" };
  }
  if (loadState === "error") {
    return { tone: "warning", text: "status unavailable" };
  }
  if (!loop) {
    return { tone: "warning", text: "not configured" };
  }
  if (!loop.enabled) {
    return { tone: "warning", text: "disabled" };
  }
  if (!loop.last_tick_at) {
    return { tone: "warning", text: "no heartbeat" };
  }
  if (!loop.delivery_owner_id || !loop.delivery_lease_name) {
    return { tone: "warning", text: "no delivery owner" };
  }
  return { tone: "success", text: "active" };
}

function agentStateTone(state?: string): string {
  switch ((state ?? "").trim().toLowerCase()) {
    case "running":
      return theme.success;
    case "starting":
      return theme.focus;
    case "error":
    case "failed":
      return theme.danger;
    case "stopped":
      return theme.muted;
    default:
      return theme.text;
  }
}

function agentModelLabel(agent?: AgentSummary): string {
  if (!agent) return "not in agent store";
  const provider = agent.llm_provider?.trim() || "default";
  const model = agent.llm_model?.trim() || "default";
  const mode = agent.exec_mode?.trim() || "reactive";
  return `${provider}/${model} ${mode}`;
}

function atcpSessionTone(
  session: ATCPSession,
  ready?: ATCPReadiness,
): string {
  switch ((session.status ?? "").trim().toLowerCase()) {
    case "running":
      return ready?.idle ? theme.success : theme.focus;
    case "exited":
    case "stopped":
      return theme.muted;
    default:
      return theme.text;
  }
}

function atcpScreenPreview(screen?: ATCPScreen): string {
  return atcpScreenLines(screen, 1)[0] ?? "";
}

function atcpScreenLines(screen: ATCPScreen | undefined, max: number): string[] {
  if (!screen || !Array.isArray(screen.lines)) return [];
  const lines: string[] = [];
  for (let index = screen.lines.length - 1; index >= 0; index--) {
    const line = screen.lines[index]?.trimEnd();
    if (line?.trim()) lines.unshift(line);
    if (lines.length >= max) break;
  }
  return lines;
}

function roomLoopStartCommand(roomId: string): string {
  return `./bin/foxctl room loop ${shellQuote(roomId)} --workspace ${shellQuote(
    WORKSPACE_ID,
  )}`;
}

function shellQuote(value: string): string {
  if (/^[A-Za-z0-9_./:@=-]+$/.test(value)) return value;
  return `'${value.replace(/'/g, `'\\''`)}'`;
}

function nextFocus(
  current: FocusRegion,
  options: {
    reverse?: boolean;
    showNav: boolean;
    showWorklist: boolean;
    hasComposer: boolean;
  },
): FocusRegion {
  const order: FocusRegion[] = [
    ...(options.showNav ? (["nav"] as FocusRegion[]) : []),
    ...(options.showWorklist ? (["worklist"] as FocusRegion[]) : []),
    "detail",
    ...(options.hasComposer ? (["composer"] as FocusRegion[]) : []),
  ];
  const index = order.indexOf(current);
  const safeIndex = index < 0 ? 0 : index;
  const delta = options.reverse ? -1 : 1;
  return order[(safeIndex + delta + order.length) % order.length] ?? "worklist";
}

function statusToneForRun(status: string): Tone {
  switch (status) {
    case "completed":
      return "success";
    case "failed":
      return "danger";
    case "running":
      return "focus";
    default:
      return "muted";
  }
}

function canKillRunStatus(status: string): boolean {
  const normalized = status.trim().toLowerCase();
  return normalized !== "" && !nonKillableRunStatuses.has(normalized);
}

function taskToneForStatus(status: string): Tone {
  switch (status) {
    case "completed":
      return "success";
    case "blocked":
    case "failed":
    case "canceled":
    case "cancelled":
      return "danger";
    case "in_progress":
    case "ready_for_review":
      return "focus";
    default:
      return "muted";
  }
}

function cardToneForLane(laneID: string, state: string): Tone {
  switch (laneID) {
    case "Done":
      return "success";
    case "Blocked":
      return "danger";
    case "Running":
    case "Claimed":
    case "Review":
      return "focus";
    default:
      if (state === "failed" || state === "blocked") return "danger";
      if (state === "completed" || state === "done") return "success";
      return "muted";
  }
}

function cardPolicyTone(card: OrchestrationCard): string {
  if (card.denial_reason || card.policy_status === "denied") {
    return theme.danger;
  }
  if (card.suggestion || card.policy_status === "blocked") {
    return theme.warning;
  }
  if (card.policy_status === "ok" || card.eligibility === "eligible") {
    return theme.success;
  }
  return theme.muted;
}

function cardPolicySummary(card: OrchestrationCard): string {
  const policy = card.policy_status?.trim() || "policy unknown";
  const eligibility = card.eligibility?.trim();
  if (eligibility) return `${policy} / ${eligibility}`;
  return policy;
}

function cardStatusDetail(card: OrchestrationCard): string {
  return (
    card.denial_reason ||
    card.suggestion ||
    card.last_outcome ||
    card.last_event_type ||
    "No card status detail recorded."
  );
}

function cardActionAvailability(
  card: OrchestrationCard,
  action: OrchestrationCardAction,
): { available: boolean; reason: string } {
  const state = card.state.trim().toLowerCase();
  if (state === "running" || state === "claimed") {
    return {
      available: false,
      reason: `card is ${card.state}`,
    };
  }
  if (
    action === "retry-now" &&
    card.lane !== "Blocked" &&
    card.lane !== "RetryQueued"
  ) {
    return {
      available: false,
      reason: "retry only works for blocked or retry-queued cards",
    };
  }
  return { available: true, reason: "" };
}

function cardAvailableActionSummary(card: OrchestrationCard): string {
  const actions: string[] = [];
  if (cardActionAvailability(card, "mark-done").available) actions.push("d done");
  if (cardActionAvailability(card, "release").available) actions.push("u release");
  if (cardActionAvailability(card, "retry-now").available) actions.push("t retry");
  return actions.length > 0 ? actions.join("  ") : "no card actions available";
}

function cardActionLabel(action: OrchestrationCardAction): string {
  switch (action) {
    case "mark-done":
      return "mark done";
    case "release":
      return "release";
    case "retry-now":
      return "retry now";
  }
}

function cardActionDoneLabel(action: OrchestrationCardAction): string {
  switch (action) {
    case "mark-done":
      return "marked done";
    case "release":
      return "released";
    case "retry-now":
      return "queued for retry";
  }
}

function cardLaneTitle(laneID: string): string {
  switch (laneID) {
    case "RetryQueued":
      return "Retry queued";
    default:
      return laneID || "Other";
  }
}

function cardWorkItemFromCard(
  card: OrchestrationCard,
  previous?: OrchestrationCardWorkItem,
): OrchestrationCardWorkItem {
  const laneId = card.lane ?? previous?.laneId ?? "Other";
  return {
    id: card.issue_id,
    laneId,
    laneTitle: cardLaneTitle(laneId),
    card,
  };
}

function roomLinkedCards(
  cards: OrchestrationCardWorkItem[],
  room: RoomTaskWorkItem["room"],
  taskId?: string,
): OrchestrationCardWorkItem[] {
  const taskIDs = new Set(
    [
      taskId,
      ...(room.latest_subject ? [room.latest_subject] : []),
    ]
      .filter((value): value is string => typeof value === "string")
      .map((value) => value.trim())
      .filter(Boolean),
  );
  return cards
    .filter((item) => taskIDs.has(item.card.issue_id))
    .slice(0, 3);
}

function isActiveCard(card: OrchestrationCard): boolean {
  return (
    card.lane === "Running" ||
    card.lane === "Claimed" ||
    card.lane === "Blocked" ||
    card.lane === "RetryQueued" ||
    card.lane === "Review"
  );
}

function roomBoardSummary(cards: OrchestrationCardWorkItem[]): string {
  if (cards.length === 0) return "";
  const counts = new Map<string, number>();
  for (const item of cards) {
    if (!isActiveCard(item.card)) continue;
    counts.set(item.laneId, (counts.get(item.laneId) ?? 0) + 1);
  }
  const parts = ["Running", "Blocked", "Review", "RetryQueued", "Claimed"]
    .map((lane) => {
      const count = counts.get(lane) ?? 0;
      return count > 0 ? `${lane.toLowerCase()} ${count}` : "";
    })
    .filter(Boolean);
  return parts.length > 0 ? parts.join(" / ") : `${cards.length} board cards`;
}

function cardRoomCardMeta(item: OrchestrationCardWorkItem): string {
  return [
    item.card.agent_id ? shortActorLabel(item.card.agent_id) : "",
    item.card.run_id ? "run" : "",
    cardPolicySummary(item.card),
  ]
    .filter(Boolean)
    .join("  ");
}

interface RuntimeLine {
  text: string;
  tone: string;
}

function cardRuntimeLines(
  runtime: OrchestrationRuntimeTree | null,
  max: number,
): RuntimeLine[] {
  if (!runtime?.root) return [];
  const lines: RuntimeLine[] = [];
  appendRuntimeNodeLines(lines, runtime.root, "", max);
  return lines.slice(0, max);
}

function appendRuntimeNodeLines(
  lines: RuntimeLine[],
  node: OrchestrationRuntimeTreeNode,
  prefix: string,
  max: number,
): void {
  if (lines.length >= max) return;
  const label = node.agent_id || node.tag || node.pid || "runtime";
  const status = node.status || runtimeNodeStateSummary(node.state) || "";
  const err = node.error ? ` error=${node.error}` : "";
  lines.push({
    text: `${prefix}${label}${status ? `  ${status}` : ""}${err}`,
    tone: node.error ? theme.warning : status === "running" ? theme.success : theme.text,
  });
  const children = node.children ?? [];
  for (let index = 0; index < children.length; index += 1) {
    if (lines.length >= max) return;
    const child = children[index];
    if (!child) continue;
    appendRuntimeNodeLines(lines, child, `${prefix}  `, max);
  }
}

function runtimeNodeStateSummary(state: unknown): string {
  if (typeof state === "string") return state;
  if (!state || typeof state !== "object") return "";
  const record = state as Record<string, unknown>;
  for (const key of ["status", "state", "phase", "mode"]) {
    const value = record[key];
    if (typeof value === "string" && value.trim() !== "") return value.trim();
  }
  return "state";
}

function nextActivityScope(scope: ActivityScope): ActivityScope {
  switch (scope) {
    case "focused":
      return "important";
    case "important":
      return "all";
    case "all":
      return "debug";
    case "debug":
      return "focused";
  }
}

function filterEventsForActivity(
  events: V2RuntimeEvent[],
  scope: ActivityScope,
): V2RuntimeEvent[] {
  if (scope !== "important") return events;
  return events.filter((event) => importantEventTypes.has(event.event_type));
}

function latestRunOutput(events: V2RuntimeEvent[]): RunOutputSummary | null {
  const turnStats = latestTurnStats(events);
  for (let index = events.length - 1; index >= 0; index -= 1) {
    const event = events[index];
    if (event?.event_type !== "run.completed") continue;
    const payload = eventPayloadObject(event.payload);
    const summary = payload.summary;
    if (typeof summary !== "string" || summary.trim() === "") continue;
    const turnID = payload.turn_id;
    const iterations = payload.iterations;
    const toolCalls = payload.tool_calls;
    return {
      summary,
      turnID: typeof turnID === "string" ? turnID : undefined,
      iterations:
        typeof iterations === "number" ? iterations : turnStats.iterations,
      toolCalls: typeof toolCalls === "number" ? toolCalls : turnStats.toolCalls,
    };
  }
  return null;
}

function latestTurnStats(events: V2RuntimeEvent[]): {
  iterations?: number;
  toolCalls?: number;
} {
  for (let index = events.length - 1; index >= 0; index -= 1) {
    const event = events[index];
    if (event?.event_type !== "turn.recorded") continue;
    const payload = eventPayloadObject(event.payload);
    const iterations = payload.iterations;
    const toolCalls = payload.tool_calls;
    return {
      iterations: typeof iterations === "number" ? iterations : undefined,
      toolCalls: typeof toolCalls === "number" ? toolCalls : undefined,
    };
  }
  return {};
}

function compactEvents(
  events: V2RuntimeEvent[],
  scope: ActivityScope,
): CompactEventLine[] {
  const lines = events.map((event) => compactEvent(event, scope));
  if (scope === "debug") return lines;

  const out: CompactEventLine[] = [];
  for (const line of lines) {
    const previous = out[out.length - 1];
    if (previous && previous.key === line.key) {
      previous.repeat += line.repeat;
      continue;
    }
    out.push({ ...line });
  }
  return out;
}

function compactEvent(
  event: V2RuntimeEvent,
  scope: ActivityScope,
): CompactEventLine {
  const tone = eventTone(event.event_type);
  const label = eventLabel(event);
  const actor = event.actor_id ? ` @${event.actor_id}` : "";
  const command = event.command ? ` ${event.command}` : "";
  const debug =
    scope === "debug"
      ? [
          event.id ? `id=${event.id}` : "",
          event.request_id ? `req=${event.request_id}` : "",
          event.correlation_id ? `corr=${event.correlation_id}` : "",
        ]
          .filter(Boolean)
          .join(" ")
      : "";
  const text = [
    `#${event.stream_version}`,
    severityLabel(tone),
    label,
    command.trim(),
    actor.trim(),
    debug,
  ]
    .filter(Boolean)
    .join(" ");
  return {
    key: [tone, label, event.command ?? "", event.actor_id ?? ""].join("|"),
    tone,
    text,
    repeat: 1,
  };
}

function eventTone(eventType: string): Tone {
  switch (eventType) {
    case "run.completed":
      return "success";
    case "run.failed":
    case "stage.failed":
    case "artifact.failed":
      return "danger";
    case "run.started":
    case "tool.invoked":
    case "orchestration.updated":
      return "focus";
    default:
      return "muted";
  }
}

function eventLabel(event: V2RuntimeEvent): string {
  const payload = eventPayloadObject(event.payload);
  switch (event.event_type) {
    case "run.started":
      return "run started";
    case "run.completed":
      return compactPayloadText(payload, ["summary"], "run completed");
    case "run.failed":
      return compactPayloadText(payload, ["message", "error"], "run failed");
    case "tool.invoked":
      return compactPayloadText(payload, ["name", "tool"], "tool invoked");
    case "tool.responded":
      return compactPayloadText(
        payload,
        ["name", "tool", "status"],
        "tool responded",
      );
    case "turn.recorded":
      return compactPayloadText(payload, ["turn_id"], "turn recorded");
    case "stage.failed":
      return compactPayloadText(
        payload,
        ["stage", "message", "error"],
        "stage failed",
      );
    case "artifact.failed":
      return compactPayloadText(
        payload,
        ["artifact", "message", "error"],
        "artifact failed",
      );
    case "orchestration.updated":
      return compactPayloadText(
        payload,
        ["issue_id", "state", "last_event_type"],
        "orchestration updated",
      );
    default:
      return event.event_type || "event";
  }
}

function eventPayloadObject(payload: unknown): Record<string, unknown> {
  return payload && typeof payload === "object" && !Array.isArray(payload)
    ? (payload as Record<string, unknown>)
    : {};
}

function compactPayloadText(
  payload: Record<string, unknown>,
  fields: string[],
  fallback: string,
): string {
  const values = fields
    .map((field) => payload[field])
    .filter((value) => typeof value === "string" && value.trim() !== "")
    .map(String);
  return values.length > 0 ? `${fallback}: ${values.join(" ")}` : fallback;
}

function severityLabel(tone: Tone): string {
  switch (tone) {
    case "success":
      return "ok";
    case "danger":
      return "err";
    case "focus":
      return "run";
    case "warning":
      return "warn";
    case "muted":
    default:
      return "info";
  }
}

function matchesRunFilter(run: RunListItem, filterText: string): boolean {
  return matchesFilter(filterText, [
    run.run_id,
    run.status,
    run.command,
    run.actor_id,
    run.request_id,
  ]);
}

function matchesRoomTaskFilter(
  item: RoomTaskWorkItem,
  filterText: string,
): boolean {
  return matchesFilter(filterText, [
    item.id,
    item.room.id,
    item.room.title,
    item.room.latest_subject,
    item.room.latest_sender,
    item.room.latest_preview,
    item.task?.id,
    item.task?.title,
    item.task?.status,
    item.task?.owner_actor_id,
    item.task?.assigned_actor_id,
    item.task?.scope_path,
    item.task?.blocked_reason,
  ]);
}

function matchesCardFilter(
  item: OrchestrationCardWorkItem,
  filterText: string,
): boolean {
  return matchesFilter(filterText, [
    item.id,
    item.laneId,
    item.laneTitle,
    item.card.issue_id,
    item.card.issue_identifier,
    item.card.title,
    item.card.state,
    item.card.policy_status,
    item.card.run_id,
    item.card.agent_id,
    item.card.actor_id,
    item.card.last_event_type,
    item.card.denial_reason,
    item.card.suggestion,
  ]);
}

function contextSourceForRoomTask(item?: RoomTaskWorkItem): string {
  if (!item) return "room context";
  if (item.task) return `room task ${item.task.id}`;
  return `room ${item.room.id}`;
}

function roomMemberSummary(room: RoomTaskWorkItem["room"]): string {
  const members = room.members ?? [];
  if (members.length === 0) return "-";
  return members
    .slice(0, 4)
    .map((member) =>
      member.role
        ? `${member.actor_id}:${member.role}`
        : member.actor_id,
    )
    .join(", ");
}

function contextSourceForCard(item?: OrchestrationCardWorkItem): string {
  if (!item) return "card context";
  return `card ${item.card.issue_identifier || item.card.issue_id}`;
}

function contextPromptForRoomTask(item?: RoomTaskWorkItem): string {
  if (!item) {
    return [
      "Inspect the current foxctl room context.",
      `Workspace: ${WORKSPACE_ID}`,
      "There are no visible room tasks in foxterm right now.",
      "Find the current state and propose the next concrete action.",
    ].join("\n");
  }
  if (item.task) {
    return [
      `Work on room task ${item.task.id}.`,
      `Room: ${item.room.title} (${item.room.id})`,
      `Task: ${item.task.title}`,
      `Status: ${item.task.status}`,
      item.task.scope_path ? `Scope: ${item.task.scope_path}` : "",
      item.task.blocked_reason ? `Blocked: ${item.task.blocked_reason}` : "",
      item.task.notes || item.task.description
        ? `Notes: ${item.task.notes || item.task.description}`
        : "",
      "Inspect the relevant context and propose or take the next concrete step.",
    ]
      .filter(Boolean)
      .join("\n");
  }
  return [
    `Inspect room ${item.room.id}.`,
    `Room: ${item.room.title}`,
    item.room.latest_subject ? `Latest: ${item.room.latest_subject}` : "",
    item.room.latest_preview ? `Preview: ${item.room.latest_preview}` : "",
    "Summarize the current state and suggest the next concrete action.",
  ]
    .filter(Boolean)
    .join("\n");
}

function contextPromptForCard(item?: OrchestrationCardWorkItem): string {
  if (!item) {
    return [
      "Inspect the current foxctl orchestration board.",
      `Workspace: ${WORKSPACE_ID}`,
      "There are no visible cards in foxterm right now.",
      "Find the current state and propose the next concrete action.",
    ].join("\n");
  }
  const card = item.card;
  return [
    `Work on orchestration card ${card.issue_id}.`,
    card.issue_identifier ? `Issue: ${card.issue_identifier}` : "",
    card.title ? `Title: ${card.title}` : "",
    `Lane: ${item.laneTitle}`,
    `State: ${card.state}`,
    card.policy_status ? `Policy: ${card.policy_status}` : "",
    card.denial_reason ? `Denied: ${card.denial_reason}` : "",
    card.suggestion ? `Suggestion: ${card.suggestion}` : "",
    card.last_outcome ? `Last outcome: ${card.last_outcome}` : "",
    card.run_id ? `Linked run: ${card.run_id}` : "",
    "Inspect the relevant context and propose or take the next concrete step.",
  ]
    .filter(Boolean)
    .join("\n");
}

function matchesFilter(filterText: string, values: Array<unknown>): boolean {
  const terms = filterTerms(filterText);
  if (terms.length === 0) return true;
  const haystack = values
    .filter((value) => value !== null && typeof value !== "undefined")
    .map(String)
    .join(" ")
    .toLowerCase();
  return terms.every((term) => haystack.includes(term));
}

function filterCommandPaletteActions(
  actions: CommandPaletteAction[],
  query: string,
): CommandPaletteAction[] {
  const terms = filterTerms(query);
  if (terms.length === 0) return actions;
  return actions.filter((action) =>
    matchesFilter(terms.join(" "), [
      action.title,
      action.category,
      action.command,
      action.description,
      action.disabledReason,
    ]),
  );
}

function paletteWindowStart(
  selectedIndex: number,
  actionCount: number,
  maxItems: number,
): number {
  if (actionCount <= maxItems) return 0;
  const centered = selectedIndex - Math.floor(maxItems / 2);
  return clampInt(centered, 0, Math.max(0, actionCount - maxItems));
}

function filterTerms(filterText: string): string[] {
  return filterText
    .trim()
    .toLowerCase()
    .split(/\s+/)
    .filter(Boolean);
}

function keyChar(name: string): string {
  if (name.length === 1) return name;
  if (name === "space") return " ";
  return "";
}

function isSubmitKey(key: {
  name?: string;
  sequence?: string;
  raw?: string;
}): boolean {
  const name = key.name ?? "";
  return (
    name === "enter" ||
    name === "return" ||
    key.sequence === "\r" ||
    key.sequence === "\n" ||
    key.raw === "\r" ||
    key.raw === "\n"
  );
}

function isCommandPaletteKey(key: {
  name?: string;
  ctrl?: boolean;
  sequence?: string;
  raw?: string;
}): boolean {
  const name = key.name ?? "";
  return (
    name === ":" ||
    key.sequence === ":" ||
    key.raw === ":" ||
    (key.ctrl === true && name.toLowerCase() === "p")
  );
}

function isRoomCreateKey(key: {
  name?: string;
  shift?: boolean;
  sequence?: string;
  raw?: string;
}): boolean {
  return (
    key.name === "+" ||
    key.sequence === "+" ||
    key.raw === "+" ||
    (key.shift === true && key.name === "=") ||
    (key.shift === true && key.name === "n") ||
    key.name === "N" ||
    key.sequence === "N" ||
    key.raw === "N"
  );
}

function isCLISpawnKey(key: {
  name?: string;
  shift?: boolean;
  sequence?: string;
  raw?: string;
}): boolean {
  return (
    (key.shift === true && key.name === "s") ||
    key.name === "S" ||
    key.sequence === "S" ||
    key.raw === "S"
  );
}

function buildRunSections(
  runs: RunListItem[],
): WorklistSection<RunWorklistItem>[] {
  const buckets = [
    {
      id: "active",
      title: "Active",
      statuses: new Set(["running", "queued", "pending"]),
    },
    {
      id: "needs-attention",
      title: "Needs attention",
      statuses: new Set(["failed", "blocked", "cancelled", "killed"]),
    },
    {
      id: "completed",
      title: "Completed",
      statuses: new Set(["completed", "succeeded"]),
    },
    { id: "other", title: "Other", statuses: null },
  ];
  return buckets.map((bucket) => ({
    id: bucket.id,
    title: bucket.title,
    items: runs
      .filter((run) =>
        bucket.statuses === null
          ? !buckets.some((candidate) => candidate.statuses?.has(run.status))
          : bucket.statuses.has(run.status),
      )
      .map((run) => ({ id: run.run_id, run })),
  }));
}

function buildCardSections(
  items: OrchestrationCardWorkItem[],
): WorklistSection<OrchestrationCardWorkItem>[] {
  const laneOrder = [
    "Running",
    "Blocked",
    "Review",
    "Claimed",
    "Todo",
    "RetryQueued",
    "Done",
  ];
  const laneTitle = new Map(items.map((item) => [item.laneId, item.laneTitle]));
  const seen = new Set<string>();
  const orderedLaneIDs = [
    ...laneOrder.filter((laneID) => {
      const present = items.some((item) => item.laneId === laneID);
      if (present) seen.add(laneID);
      return present;
    }),
    ...items
      .map((item) => item.laneId)
      .filter((laneID) => {
        if (seen.has(laneID)) return false;
        seen.add(laneID);
        return true;
      }),
  ];
  return orderedLaneIDs.map((laneID) => ({
    id: laneID,
    title: laneTitle.get(laneID) ?? laneID,
    items: items.filter((item) => item.laneId === laneID),
  }));
}

function buildRoomTaskSections(
  items: RoomTaskWorkItem[],
): WorklistSection<RoomTaskWorkItem>[] {
  const buckets = [
    {
      id: "rooms",
      title: "Rooms",
      statuses: null,
      roomOnly: true,
    },
    {
      id: "active",
      title: "Active",
      statuses: new Set(["in_progress", "ready_for_review"]),
    },
    {
      id: "ready",
      title: "Ready",
      statuses: new Set(["pending"]),
    },
    {
      id: "needs-attention",
      title: "Needs attention",
      statuses: new Set(["blocked", "failed", "canceled", "cancelled"]),
    },
    {
      id: "completed",
      title: "Completed",
      statuses: new Set(["completed"]),
    },
    { id: "other", title: "Other", statuses: null },
  ];
  return buckets.map((bucket) => ({
    id: bucket.id,
    title: bucket.title,
    items: items.filter((item) =>
      bucket.roomOnly
        ? !item.task
        : !item.task
          ? false
          : bucket.statuses === null
        ? !buckets.some((candidate) =>
            candidate.statuses?.has(item.task?.status ?? ""),
          )
        : bucket.statuses.has(item.task.status),
    ),
  }));
}

function moveSelection<T extends { id: string }>(
  delta: number,
  items: T[],
  selectedId: string | null,
  setSelectedId: (value: string | null) => void,
): void {
  if (items.length === 0) return;
  const currentIndex =
    selectedId === null ? -1 : items.findIndex((item) => item.id === selectedId);
  const nextIndex =
    currentIndex < 0
      ? delta > 0
        ? 0
        : items.length - 1
      : Math.max(0, Math.min(items.length - 1, currentIndex + delta));
  setSelectedId(items[nextIndex]?.id ?? selectedId);
}

function runFooter(
  loadState: LoadState,
  lastLoadedAt: string | null,
  stale: boolean,
): string {
  if (loadState === "loading") return "refreshing";
  if (stale) {
    return lastLoadedAt ? `stale, last loaded ${lastLoadedAt}` : "stale";
  }
  if (lastLoadedAt) return `loaded ${lastLoadedAt}`;
  return "not loaded";
}

function roomTaskStatus(roomCount: number, taskCount: number): string {
  if (roomCount === 0) return "no rooms";
  if (taskCount === 0) return `${roomCount} rooms, no tasks`;
  return `${taskCount} room tasks loaded`;
}

function countRoomTasks(items: RoomTaskWorkItem[]): number {
  return items.filter((item) => item.task).length;
}

function cardStatus(cardCount: number, artifact?: string): string {
  if (artifact) return "board stored as artifact";
  if (cardCount === 0) return "no cards";
  return `${cardCount} cards loaded`;
}

function truncate(value: string, max: number): string {
  if (value.length <= max) return value;
  if (max <= 1) return value.slice(0, max);
  return `${value.slice(0, max - 1)}…`;
}
