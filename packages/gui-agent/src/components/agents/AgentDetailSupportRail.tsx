import { ScrollArea } from "@/components/ui/scroll-area";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card";
import type { CompanionMemoryStats, PersistedSession } from "@/api/client";
import type { Agent, CoChangeHit, Room } from "@/api/types";
import {
  getAgentDisplayName,
  getAgentRepoDisplayName,
  isSandboxBackedAgent,
} from "@/lib/agent-utils";
import { formatRelativeTime } from "@/lib/utils";
import {
  Brain,
  Cpu,
  FileText,
  Folder,
  Hash,
  Layers,
  Network,
  UserCircle2,
  Workflow,
  Wrench,
} from "lucide-react";

function MemoryStat({
  label,
  value,
  accent,
}: {
  label: string;
  value: string | number;
  accent?: string;
}) {
  return (
    <div className="rounded-lg border border-border bg-background/60 p-3">
      <div className="text-[11px] uppercase tracking-wider text-muted-foreground">
        {label}
      </div>
      <div className={`mt-1 text-lg font-semibold text-foreground ${accent ?? ""}`}>
        {value}
      </div>
    </div>
  );
}

interface AgentDetailSupportRailProps {
  activeAgent: Agent;
  activeMemoryScope: "agent" | "session";
  activeMemoryRetention: string;
  conversationExplicit: boolean;
  memoryStats: CompanionMemoryStats | null;
  loadingMemoryStats: boolean;
  controlRoom: Room | null;
  controlRoomID: string;
  controlRoomMembersCount: number;
  roomWorkspacePath?: string;
  openControlRoomPending: boolean;
  onOpenControlRoom: () => void;
  onOpenRooms: () => void;
  onOpenCompanionMemory: () => void;
  persistedSessions: PersistedSession[];
  onOpenCompanionHistory: () => void;
  cochangeHits: CoChangeHit[];
  cochangeLoading: boolean;
}

export function AgentDetailSupportRail({
  activeAgent,
  activeMemoryScope,
  activeMemoryRetention,
  conversationExplicit,
  memoryStats,
  loadingMemoryStats,
  controlRoom,
  controlRoomID,
  controlRoomMembersCount,
  roomWorkspacePath,
  openControlRoomPending,
  onOpenControlRoom,
  onOpenRooms,
  onOpenCompanionMemory,
  persistedSessions,
  onOpenCompanionHistory,
  cochangeHits,
  cochangeLoading,
}: AgentDetailSupportRailProps) {
  const sandboxBacked = isSandboxBackedAgent(activeAgent);
  const repoLabel = getAgentRepoDisplayName(activeAgent.repo_url);

  return (
    <ScrollArea className="min-h-0 pr-2">
      <div className="space-y-4">
        <Card>
          <CardHeader className="pb-3">
            <div className="flex items-start justify-between gap-3">
              <div>
                <CardTitle className="text-sm">Co-change</CardTitle>
                <CardDescription>
                  What usually changes with this agent/runtime area.
                </CardDescription>
              </div>
              <Hash className="h-4 w-4 text-muted-foreground" />
            </div>
          </CardHeader>
          <CardContent className="space-y-3">
            {cochangeLoading ? (
              <div className="rounded-lg border border-dashed border-border px-3 py-2 text-xs text-muted-foreground">
                Loading co-change clusters...
              </div>
            ) : cochangeHits.length > 0 ? (
              <div className="space-y-2">
                {cochangeHits.slice(0, 3).map((hit) => (
                  <div
                    key={hit.name}
                    className="rounded-lg border border-border bg-background/60 p-3"
                  >
                    <div className="text-xs font-medium text-foreground">
                      {hit.anchor_path}
                    </div>
                    <div className="mt-1 text-xs text-muted-foreground">
                      {hit.summary}
                    </div>
                    {hit.neighbors && hit.neighbors.length > 0 && (
                      <div className="mt-2 text-[11px] text-muted-foreground">
                        with {hit.neighbors.slice(0, 3).join(", ")}
                      </div>
                    )}
                  </div>
                ))}
              </div>
            ) : (
              <div className="rounded-lg border border-dashed border-border px-3 py-2 text-xs text-muted-foreground">
                No co-change clusters found for this agent yet.
              </div>
            )}
          </CardContent>
        </Card>

        <Card>
          <CardHeader className="pb-3">
            <div className="flex items-start justify-between gap-3">
              <div>
                <CardTitle className="text-sm">Execution Workspace</CardTitle>
                <CardDescription>
                  Where this agent runs code and what repo context it carries.
                </CardDescription>
              </div>
              <Folder className="h-4 w-4 text-muted-foreground" />
            </div>
          </CardHeader>
          <CardContent className="space-y-3">
            <div className="flex flex-wrap gap-2">
              <Badge
                variant="outline"
                className={
                  sandboxBacked
                    ? "border-sky-500/30 bg-sky-500/5 text-sky-600"
                    : undefined
                }
              >
                {sandboxBacked ? "sandbox clone" : "local runtime"}
              </Badge>
              {sandboxBacked && activeAgent.sandbox_provider && (
                <Badge variant="secondary">{activeAgent.sandbox_provider}</Badge>
              )}
              {repoLabel && (
                <Badge variant="outline">
                  {repoLabel}
                  {activeAgent.repo_ref ? ` @ ${activeAgent.repo_ref}` : ""}
                </Badge>
              )}
            </div>
            <div className="rounded-lg border border-border bg-background/60 p-3 space-y-2 text-xs text-muted-foreground">
              <div>
                {sandboxBacked
                  ? "Sandbox-backed agents keep their repo clone and prompt execution context outside the local foxctl pod runtime."
                  : "Local agents read and write against the runtime workspace managed by the current foxctl deployment."}
              </div>
              <div className="grid gap-1 text-[11px]">
                <div>
                  workspace <code>{activeAgent.ns || "/"}</code>
                </div>
                {activeAgent.workspace_root && (
                  <div>
                    root <code>{activeAgent.workspace_root}</code>
                  </div>
                )}
                {activeAgent.sandbox_id && (
                  <div>
                    sandbox <code>{activeAgent.sandbox_id}</code>
                  </div>
                )}
                {activeAgent.repo_url && (
                  <div>
                    remote <code>{activeAgent.repo_url}</code>
                  </div>
                )}
              </div>
            </div>
          </CardContent>
        </Card>

        <Card>
          <CardHeader className="pb-3">
            <div className="flex items-start justify-between gap-3">
              <div>
                <CardTitle className="text-sm">Layered Memory</CardTitle>
                <CardDescription>
                  Summary only. Detailed memory controls live in the Companion
                  surface.
                </CardDescription>
              </div>
              <Layers className="h-4 w-4 text-muted-foreground" />
            </div>
          </CardHeader>
          <CardContent className="space-y-3">
            <div className="grid grid-cols-2 gap-2">
              <MemoryStat
                label="Turns"
                value={memoryStats?.total_turns ?? (loadingMemoryStats ? "..." : 0)}
              />
              <MemoryStat
                label="Day Summaries"
                value={memoryStats?.day_summaries ?? (loadingMemoryStats ? "..." : 0)}
              />
              <MemoryStat
                label="Distilled"
                value={memoryStats?.has_distilled_history ? "yes" : "no"}
              />
              <MemoryStat
                label="Lineage"
                value={
                  activeMemoryScope === "session"
                    ? "session"
                    : conversationExplicit
                      ? "explicit"
                      : "implicit"
                }
              />
            </div>
            <div className="rounded-lg border border-border bg-background/60 p-3 space-y-3">
              <div className="flex items-center justify-between gap-3">
                <div>
                  <div className="text-[11px] uppercase tracking-wider text-muted-foreground">
                    Current Policy
                  </div>
                  <div className="mt-1 text-xs text-muted-foreground">
                    Workbench memory currently follows the agent-level
                    retention and lineage policy shown below.
                  </div>
                </div>
                <Badge variant="outline" className="text-[10px]">
                  {activeMemoryRetention}
                </Badge>
              </div>
              <div className="grid gap-1 text-[11px] text-muted-foreground">
                <div>
                  retention <code>{activeMemoryRetention}</code>
                </div>
                <div>
                  lineage{" "}
                  <code>
                    {activeMemoryScope === "session"
                      ? "session"
                      : conversationExplicit
                        ? "explicit"
                        : "implicit"}
                  </code>
                </div>
                {memoryStats?.last_summarized_date && (
                  <div>
                    last summarized <code>{memoryStats.last_summarized_date}</code>
                  </div>
                )}
              </div>
            </div>
            <div className="flex flex-wrap gap-2">
              <Button
                type="button"
                variant="outline"
                size="sm"
                onClick={onOpenCompanionMemory}
              >
                <Brain className="h-4 w-4" />
                Open In Companion
              </Button>
            </div>
            <div className="rounded-lg border border-dashed border-border px-3 py-2 text-xs text-muted-foreground">
              Layered memory inspection, policy changes, and compaction now
              belong in the Companion surface, not in this detail view.
            </div>
          </CardContent>
        </Card>

        <Card>
          <CardHeader className="pb-3">
            <div className="flex items-start justify-between gap-3">
              <div>
                <CardTitle className="text-sm">Control Room</CardTitle>
                <CardDescription>
                  Room coordination lives in the canonical Rooms surface.
                </CardDescription>
              </div>
              <div className="flex items-center gap-2">
                <Badge variant="outline" className="text-[10px] font-mono">
                  {controlRoomID}
                </Badge>
                <Workflow className="h-4 w-4 text-muted-foreground" />
              </div>
            </div>
          </CardHeader>
          <CardContent className="space-y-3">
            <div className="flex flex-wrap gap-2">
              <Badge variant="secondary" className="text-[10px]">
                {controlRoom ? `${controlRoom.message_count} messages` : "room missing"}
              </Badge>
              <Badge variant="outline" className="text-[10px]">
                {controlRoomMembersCount} members
              </Badge>
              <Badge variant="outline" className="text-[10px]">
                {(controlRoom?.dispatch_policy || "all_subtree").replace(/_/g, " ")}
              </Badge>
            </div>
            <div className="rounded-lg border border-border bg-background/60 p-3">
              {controlRoom ? (
                <div className="space-y-2">
                  <div className="text-xs text-muted-foreground">
                    The control room exists and can be used for subtree
                    coordination, dispatch policy, and timeline review from the
                    Rooms surface.
                  </div>
                  <div className="grid gap-1 text-[11px] text-muted-foreground">
                    <div>
                      workspace <code>{controlRoom.workspace_id}</code>
                    </div>
                    <div>
                      updated{" "}
                      {controlRoom.latest_message_at
                        ? formatRelativeTime(controlRoom.latest_message_at)
                        : "no messages yet"}
                    </div>
                  </div>
                </div>
              ) : (
                <div className="text-xs text-muted-foreground">
                  No control room exists yet. Create it, then continue
                  coordination and dispatch management in Rooms.
                </div>
              )}
            </div>
            <div className="flex flex-wrap gap-2">
              <Button
                type="button"
                size="sm"
                onClick={onOpenControlRoom}
                disabled={openControlRoomPending || !roomWorkspacePath}
              >
                <Hash className="h-4 w-4" />
                {controlRoom ? "Open Control Room" : "Create Control Room"}
              </Button>
              <Button type="button" variant="outline" size="sm" onClick={onOpenRooms}>
                Open Rooms
              </Button>
            </div>
            <div className="rounded-lg border border-dashed border-border px-3 py-2 text-xs text-muted-foreground">
              Detailed room editing, dispatch policy changes, and coordination
              messaging now belong in the Rooms surface, not in this detail
              view.
            </div>
          </CardContent>
        </Card>

        <Card>
          <CardHeader className="pb-3">
            <div className="flex items-start justify-between gap-3">
              <div>
                <CardTitle className="text-sm">Persisted Sessions</CardTitle>
                <CardDescription>
                  Summary only. Full archive browsing lives in the Companion
                  surface.
                </CardDescription>
              </div>
              <FileText className="h-4 w-4 text-muted-foreground" />
            </div>
          </CardHeader>
          <CardContent className="space-y-3">
            {persistedSessions.length > 0 ? (
              <>
                <div className="rounded-lg border border-border bg-background/60 p-3 space-y-3">
                  <div className="flex items-center justify-between gap-3">
                    <div>
                      <div className="text-[11px] uppercase tracking-wider text-muted-foreground">
                        Recent History
                      </div>
                      <div className="mt-1 text-xs text-muted-foreground">
                        {persistedSessions.length} persisted sessions tied to
                        this agent or role.
                      </div>
                    </div>
                    <Badge variant="outline" className="text-[10px]">
                      {persistedSessions.length}
                    </Badge>
                  </div>

                  {persistedSessions[0] && (
                    <div className="space-y-2 text-xs text-muted-foreground">
                      <div>
                        latest{" "}
                        <code>{formatRelativeTime(persistedSessions[0].started_at)}</code>
                      </div>
                      <div>
                        status <code>{persistedSessions[0].status}</code> ·{" "}
                        {persistedSessions[0].message_count} messages
                      </div>
                      {persistedSessions[0].summary && (
                        <div className="rounded bg-background/70 px-2 py-2 text-foreground">
                          {persistedSessions[0].summary}
                        </div>
                      )}
                    </div>
                  )}
                </div>
                <div className="flex flex-wrap gap-2">
                  <Button
                    type="button"
                    variant="outline"
                    size="sm"
                    onClick={onOpenCompanionHistory}
                  >
                    <FileText className="h-4 w-4" />
                    Open History In Companion
                  </Button>
                </div>
                <div className="rounded-lg border border-dashed border-border px-3 py-2 text-xs text-muted-foreground">
                  Detailed session history, transcript browsing, and follow-up
                  continuation now belong in the Companion surface.
                </div>
              </>
            ) : (
              <div className="rounded-lg border border-dashed border-border px-3 py-2 text-xs text-muted-foreground">
                No persisted sessions have been recorded for this agent yet.
              </div>
            )}
          </CardContent>
        </Card>

        <Card>
          <CardHeader className="pb-3">
            <CardTitle className="text-sm">Identity Context</CardTitle>
            <CardDescription>
              What gets threaded into companion requests for this agent.
            </CardDescription>
          </CardHeader>
          <CardContent>
            <div className="space-y-2 rounded-lg border border-border bg-background/60 p-3 text-xs text-muted-foreground">
              <div className="flex items-center gap-2">
                <UserCircle2 className="h-3.5 w-3.5" />
                Name: {getAgentDisplayName(activeAgent)}
              </div>
              <div className="flex items-center gap-2">
                <Wrench className="h-3.5 w-3.5" />
                Role: {activeAgent.role || "agent"}
              </div>
              <div className="flex items-center gap-2">
                <Network className="h-3.5 w-3.5" />
                Workspace: {activeAgent.ns || "/"} ({sandboxBacked ? "sandbox" : "local"})
              </div>
              <div className="flex items-center gap-2">
                <Cpu className="h-3.5 w-3.5" />
                Model: {activeAgent.llm_model || "default"}
              </div>
            </div>
          </CardContent>
        </Card>
      </div>
    </ScrollArea>
  )
}
