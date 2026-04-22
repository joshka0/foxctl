import { useEffect, useMemo, useState } from "react";
import { useKeyboard, useTerminalDimensions } from "@opentui/react";

import type { V2RunTranscriptItem, V2RuntimeEvent } from "@foxctl/data/types";
import {
  createRun,
  getOrchestrationCardWork,
  getRoomTaskWork,
  getRunTranscript,
  getRuns,
  getV2ModelEndpoint,
  killRun,
  subscribeToV2Stream,
  WORKSPACE_ID,
  type OrchestrationCardWorkItem,
  type RoomTaskWorkItem,
  type RunListItem,
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

type ComposerTarget =
  | { kind: "new" }
  | { kind: "continue"; runId: string };

export function App({ onExit }: AppProps) {
  const { width, height } = useTerminalDimensions();
  const short = height < 28;
  const compact = width < 120 || short;
  const focusOnly = compact && short;
  const tooSmall = width < 60 || height < 18;
  const [mode, setMode] = useState<Mode>("normal");
  const [focus, setFocus] = useState<FocusRegion>("worklist");
  const [filterText, setFilterText] = useState("");
  const [activityScope, setActivityScope] =
    useState<ActivityScope>("focused");
  const [composerText, setComposerText] = useState("");
  const [composerTarget, setComposerTarget] = useState<ComposerTarget>({
    kind: "new",
  });
  const [composerBusy, setComposerBusy] = useState(false);
  const [busyTick, setBusyTick] = useState(0);
  const [modelEndpoint, setModelEndpoint] = useState<V2ModelEndpoint | null>(
    null,
  );
  const [pendingKillRunId, setPendingKillRunId] = useState<string | null>(null);
  const [killBusy, setKillBusy] = useState(false);
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
  const selectedRoomTaskMissing =
    selectedRoomTaskId !== null &&
    selectedRoomTaskItem === undefined &&
    roomTaskItems.length > 0;
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

  const refreshRoomTasks = async () => {
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
        (current) => current ?? result.items[0]?.id ?? null,
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

  useEffect(() => {
    void refreshRuns();
    void refreshModelEndpoint();
  }, []);

  useEffect(() => {
    if (!composerBusy) {
      setBusyTick(0);
      return;
    }
    const timer = setInterval(() => {
      setBusyTick((current) => current + 1);
    }, 120);
    return () => clearInterval(timer);
  }, [composerBusy]);

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
    if (activeView === "cards" && cardLoadState === "idle") {
      void refreshCards();
    }
  }, [activeView, roomTaskLoadState, cardLoadState]);

  useEffect(() => {
    if (compact && focus === "nav") {
      setFocus("worklist");
    }
  }, [compact, focus]);

  useEffect(() => {
    if (activeView !== "runs" && focus === "composer") {
      setFocus("worklist");
    }
  }, [activeView, focus]);

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
      if (mode === "filter") {
        setFilterText("");
      }
      if (mode === "compose") {
        setComposerText("");
        setFocus("worklist");
      }
      if (mode === "confirmKill") {
        setPendingKillRunId(null);
      }
      setMode("normal");
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
    if (name === "n" && activeView === "runs") {
      openComposer({ kind: "new" });
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
    if (name === "a") {
      setActivityScope((current) => nextActivityScope(current));
      return;
    }
    if (name === "r") {
      if (activeView === "rooms") {
        void refreshRoomTasks();
      } else if (activeView === "cards") {
        void refreshCards();
      } else {
        void refreshRuns();
      }
      return;
    }
    if (name === "tab") {
      setFocus((current) =>
        nextFocus(current, {
          reverse: key.shift,
          compact,
          hasComposer: activeView === "runs",
        }),
      );
      return;
    }
    if (isSubmitKey(key) && activeView === "runs" && focus === "worklist") {
      setFocus("detail");
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
            {!focusOnly && (
              <Sidebar
                compact={compact}
                focused={focus === "nav"}
                activeIndex={navIndex}
              />
            )}
            {activeView === "rooms" ? (
              <>
                {(!focusOnly || focus === "worklist") && (
                  <RoomTaskListPanel
                    compact={compact}
                    short={short}
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
                {(!focusOnly || focus !== "worklist") && (
                  <RoomTaskDetailPanel
                    compact={compact}
                    focused={focus === "detail"}
                    selectedItem={selectedRoomTaskItem}
                    selectedId={selectedRoomTaskId}
                    selectedMissing={selectedRoomTaskMissing}
                    roomCount={roomCount}
                  />
                )}
              </>
            ) : activeView === "cards" ? (
              <>
                {(!focusOnly || focus === "worklist") && (
                  <CardListPanel
                    compact={compact}
                    short={short}
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
                {(!focusOnly || focus !== "worklist") && (
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
                {(!focusOnly || focus === "worklist") && (
                  <RunListPanel
                    compact={compact}
                    short={short}
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
                {(!focusOnly || focus !== "worklist") && (
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
          {activeView === "runs" && (
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
        </>
      )}
      {mode === "help" && <HelpOverlay compact={compact} width={width} />}
      {mode === "confirmKill" && pendingKillRunId && (
        <KillConfirmOverlay
          compact={compact}
          width={width}
          runId={pendingKillRunId}
          busy={killBusy}
        />
      )}
    </AppFrame>
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

function Sidebar({
  compact,
  focused,
  activeIndex,
}: {
  compact: boolean;
  focused: boolean;
  activeIndex: number;
}) {
  if (compact) return null;
  return (
    <box
      style={{
        width: 24,
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
  const label = target.kind === "continue" ? "follow-up" : "prompt";
  const spinner = busy ? spinnerFrame(busyTick) : "";
  const endpoint = modelEndpointLabel(modelEndpoint);
  const visibleValue =
    value.trim() === ""
      ? target.kind === "continue"
        ? `c to continue ${truncate(target.runId, compact ? 24 : 40)}`
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

function RunListPanel({
  compact,
  short,
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
      width={compact ? "100%" : 42}
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
      width={compact ? "100%" : 42}
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
        detail={`workspace ${WORKSPACE_ID}`}
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
      title="No room data found."
      detail="Rooms loaded, but no visible room rows were returned."
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
  focused,
  selectedItem,
  selectedId,
  selectedMissing,
  roomCount,
}: {
  compact: boolean;
  focused: boolean;
  selectedItem?: RoomTaskWorkItem;
  selectedId: string | null;
  selectedMissing: boolean;
  roomCount: number;
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
        <box style={{ flexDirection: "column", gap: 1, marginTop: 1 }}>
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
          <text fg={theme.focus}>notes</text>
          <scrollbox style={{ flexGrow: 1 }}>
            <text fg={theme.text}>
              {truncate(
                selectedItem.task.notes ||
                  selectedItem.task.description ||
                  "No notes recorded.",
                120,
              )}
            </text>
          </scrollbox>
        </box>
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
        <RoomSummaryDetail selectedItem={selectedItem} roomCount={roomCount} />
      )}
    </Panel>
  );
}

function RoomSummaryDetail({
  selectedItem,
  roomCount,
}: {
  selectedItem?: RoomTaskWorkItem;
  roomCount: number;
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
    <box style={{ flexDirection: "column", gap: 1, marginTop: 1 }}>
      <text fg={theme.text}>room       {selectedItem.room.title}</text>
      <text fg={theme.muted}>room_id    {selectedItem.room.id}</text>
      <text fg={theme.text}>messages   {selectedItem.room.message_count}</text>
      <text fg={theme.text}>unread     {selectedItem.room.unread_count}</text>
      <text fg={theme.muted}>
        latest    {truncate(selectedItem.room.latest_subject ?? "-", 88)}
      </text>
      <text fg={theme.muted}>
        sender    {selectedItem.room.latest_sender ?? "-"}
      </text>
      <text fg={theme.focus}>preview</text>
      <scrollbox style={{ flexGrow: 1 }}>
        <text fg={theme.text}>
          {truncate(
            selectedItem.room.latest_preview ||
              selectedItem.room.description ||
              "No linked tasks in this room yet.",
            120,
          )}
        </text>
      </scrollbox>
    </box>
  );
}

function CardListPanel({
  compact,
  short,
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
      width={compact ? "100%" : 42}
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
      detail={`workspace ${WORKSPACE_ID}`}
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
        {truncate(item.card.title || item.card.issue_identifier || item.id, 32)}
      </text>
      <text fg={toneColor(tone)}>
        {"  "}
        {item.laneId.padEnd(12)} {truncate(item.card.issue_id, 16)}
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
  return (
    <Panel
      title="Card detail"
      subtitle={selectedItem ? selectedItem.laneTitle : undefined}
      focused={focused}
      flexGrow={1}
      height={compact ? undefined : "100%"}
    >
      {selectedItem ? (
        <box style={{ flexDirection: "column", gap: 1, marginTop: 1 }}>
          <text fg={theme.text}>
            title      {selectedItem.card.title ?? "-"}
          </text>
          <text fg={theme.muted}>issue      {selectedItem.card.issue_id}</text>
          <text fg={theme.text}>lane       {selectedItem.laneTitle}</text>
          <text fg={theme.text}>state      {selectedItem.card.state}</text>
          <text fg={theme.muted}>
            policy    {selectedItem.card.policy_status ?? "-"}
          </text>
          <text fg={theme.muted}>
            run       {selectedItem.card.run_id ?? "-"}
          </text>
          <text fg={theme.muted}>
            agent     {selectedItem.card.agent_id ?? "-"}
          </text>
          <text fg={theme.muted}>
            actor     {selectedItem.card.actor_id ?? "-"}
          </text>
          <text fg={theme.focus}>status</text>
          <scrollbox style={{ flexGrow: 1 }}>
            <text fg={theme.text}>
              {truncate(
                selectedItem.card.denial_reason ||
                  selectedItem.card.suggestion ||
                  selectedItem.card.last_outcome ||
                  selectedItem.card.last_event_type ||
                  "No card status detail recorded.",
                120,
              )}
            </text>
          </scrollbox>
        </box>
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

function nextFocus(
  current: FocusRegion,
  options: { reverse?: boolean; compact: boolean; hasComposer: boolean },
): FocusRegion {
  const order: FocusRegion[] = [
    ...(options.compact ? [] : (["nav"] as FocusRegion[])),
    "worklist",
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
