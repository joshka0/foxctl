import { useEffect, useMemo, useRef, useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardHeader } from '@/components/ui/card'
import { Input } from '@/components/ui/input'
import { Textarea } from '@/components/ui/textarea'
import { cn, formatRelativeTime } from '@/lib/utils'
import { indexRoomsByActor, isPathWorkspace, roomDisplayName } from '@/lib/room-utils'
import { useActivityFocusStore } from '@/stores/activityFocusStore'
import { useOrchestrationBoardStore, ORCHESTRATION_LANE_ORDER } from '@/stores/orchestrationBoardStore'
import { useViewStore } from '@/stores/viewStore'
import { archiveOrchestrationCards, companionChat, listRooms, restoreOrchestrationCards, seedOrchestrationCards } from '@/api/client'
import type {
  OrchestrationCard,
  OrchestrationCardAction,
  OrchestrationLane,
  OrchestrationLaneID,
  Room,
  OrchestrationRuntimeTreeNode,
  OrchestrationSeedCardInput,
} from '@/api/types'
import {
  ArrowRight,
  CheckCircle2,
  Clock,
  GitBranch,
  Sparkles,
  LayoutGrid,
  RefreshCw,
  RotateCcw,
  ArchiveRestore,
  TriangleAlert,
  Undo2,
} from 'lucide-react'

interface RuntimeSummaryPanelProps {
  workspaceID?: string
  sourceSurface?: 'runtime' | 'orchestration'
  className?: string
}

function laneCount(counts: Partial<Record<OrchestrationLaneID, number>>, lane: OrchestrationLaneID): number {
  const value = counts[lane]
  return typeof value === 'number' && Number.isFinite(value) ? value : 0
}

function sortLanes(lanes: OrchestrationLane[]): OrchestrationLane[] {
  const rank = new Map<string, number>(ORCHESTRATION_LANE_ORDER.map((lane, index) => [lane, index]))
  return [...lanes].sort((a, b) => (rank.get(a.id) ?? 999) - (rank.get(b.id) ?? 999))
}

function cardTitle(card: OrchestrationCard): string {
  const issue = card.issue_identifier || card.issue_id
  const title = (card.title ?? '').trim()
  if (!title) return issue
  return `${issue}: ${title}`
}

function detailValue(value: string | number | undefined): string {
  if (value === undefined || value === null) return '—'
  const out = String(value).trim()
  return out.length > 0 ? out : '—'
}

function workspaceBadgeLabel(workspace: string): string {
  const trimmed = workspace.trim()
  if (!trimmed) return 'unscoped'
  if (trimmed === '/') return '/'
  const parts = trimmed.split('/').filter(Boolean)
  return parts[parts.length - 1] || trimmed
}

function localRequestID(prefix: string): string {
  if (typeof crypto !== 'undefined' && typeof crypto.randomUUID === 'function') {
    return `${prefix}-${crypto.randomUUID()}`
  }
  return `${prefix}-${Date.now()}-${Math.random().toString(16).slice(2)}`
}

function parseSeedCardsFromText(value: string): OrchestrationSeedCardInput[] {
  return value
    .split('\n')
    .map((line) => line.trim())
    .filter((line) => line.length > 0)
    .map((line) => {
      const parts = line.split('|').map((part) => part.trim()).filter(Boolean)
      if (parts.length >= 2) {
        return {
          issue_identifier: parts[0],
          title: parts.slice(1).join(' | '),
        }
      }
      return { title: line }
    })
    .filter((card) => (card.title ?? '').trim().length > 0)
}

function parseAISuggestedCards(raw: string): OrchestrationSeedCardInput[] {
  const trimmed = raw.trim()
  if (!trimmed) return []

  const tryParse = (candidate: string): OrchestrationSeedCardInput[] => {
    try {
      const parsed = JSON.parse(candidate)
      if (!Array.isArray(parsed)) return []
      return parsed
        .map((item) => {
          if (!item || typeof item !== 'object') return null
          const asRecord = item as Record<string, unknown>
          const title = String(asRecord.title ?? '').trim()
          if (!title) return null
          const issueIdentifier = String(asRecord.issue_identifier ?? '').trim()
          return issueIdentifier
            ? { issue_identifier: issueIdentifier, title }
            : { title }
        })
        .filter((item): item is OrchestrationSeedCardInput => item !== null)
    } catch {
      return []
    }
  }

  const direct = tryParse(trimmed)
  if (direct.length > 0) return direct

  const fence = trimmed.match(/```(?:json)?\s*([\s\S]*?)\s*```/i)
  if (fence?.[1]) {
    const fromFence = tryParse(fence[1])
    if (fromFence.length > 0) return fromFence
  }

  return parseSeedCardsFromText(trimmed)
}

function cardsToSeedText(cards: OrchestrationSeedCardInput[]): string {
  return cards
    .map((card) => {
      const issue = (card.issue_identifier ?? '').trim()
      const title = (card.title ?? '').trim()
      if (!title) return ''
      return issue ? `${issue} | ${title}` : title
    })
    .filter((line) => line.length > 0)
    .join('\n')
}

function runtimeStateText(state: unknown): string {
  if (state === null || typeof state === 'undefined') return ''
  if (typeof state === 'string') return state
  try {
    return JSON.stringify(state, null, 2)
  } catch {
    return String(state)
  }
}

function canRetryNow(card: OrchestrationCard): boolean {
  return card.lane === 'Blocked' || card.lane === 'RetryQueued'
}

function canRelease(card: OrchestrationCard): boolean {
  return card.state !== 'Running' && card.state !== 'Claimed' && card.lane !== 'Todo'
}

function canMarkDone(card: OrchestrationCard): boolean {
  return card.state !== 'Running' && card.state !== 'Claimed' && card.lane !== 'Done'
}

function RuntimeTreeNodeView({
  node,
  depth = 0,
  roomsByAgent,
  onOpenRoom,
}: {
  node: OrchestrationRuntimeTreeNode;
  depth?: number;
  roomsByAgent: Map<string, Room[]>;
  onOpenRoom: (room: Room) => void;
}) {
  const stateText = runtimeStateText(node.state)
  const rooms = node.agent_id ? roomsByAgent.get(node.agent_id) ?? [] : []

  return (
    <div className={cn('space-y-2', depth > 0 && 'ml-3 pl-3 border-l border-border/60')}>
      <div className="rounded-md border border-border bg-background/50 px-3 py-2 space-y-1">
        <div className="flex items-center gap-2 flex-wrap text-[11px]">
          <Badge variant="outline" className="font-mono text-[10px]">
            {node.tag || node.agent_id || 'runtime-node'}
          </Badge>
          {node.status && (
            <Badge variant="secondary" className="text-[10px]">
              {node.status}
            </Badge>
          )}
          {node.pid && (
            <span className="text-muted-foreground font-mono">
              pid {node.pid}
            </span>
          )}
        </div>
        {node.agent_id && (
          <div className="text-[11px] text-muted-foreground">
            agent <code>{node.agent_id}</code>
          </div>
        )}
        {rooms.length > 0 && (
          <div className="flex flex-wrap gap-1">
            {rooms.slice(0, 2).map((room) => (
              <button
                key={`${node.agent_id}-${room.id}`}
                type="button"
                onClick={() => onOpenRoom(room)}
              >
                <Badge variant="outline" className="text-[10px]">
                  room:{roomDisplayName(room)}
                </Badge>
              </button>
            ))}
            {rooms.length > 2 && (
              <Badge variant="outline" className="text-[10px]">
                +{rooms.length - 2} rooms
              </Badge>
            )}
          </div>
        )}
        {node.error && (
          <div className="text-[11px] text-red-300">
            {node.error}
          </div>
        )}
        {node.metadata && Object.keys(node.metadata).length > 0 && (
          <div className="text-[11px] text-muted-foreground">
            metadata <code>{runtimeStateText(node.metadata)}</code>
          </div>
        )}
        {stateText && (
          <pre className="overflow-x-auto rounded bg-muted/30 p-2 text-[10px] text-muted-foreground whitespace-pre-wrap">
            {stateText}
          </pre>
        )}
      </div>
      {Array.isArray(node.children) && node.children.length > 0 && (
        <div className="space-y-2">
          {node.children.map((child) => (
            <RuntimeTreeNodeView
              key={`${child.agent_id || child.tag || 'child'}-${depth + 1}`}
              node={child}
              depth={depth + 1}
              roomsByAgent={roomsByAgent}
              onOpenRoom={onOpenRoom}
            />
          ))}
        </div>
      )}
    </div>
  )
}

export function RuntimeSummaryPanel({
  workspaceID,
  sourceSurface = 'runtime',
  className,
}: RuntimeSummaryPanelProps) {
  const {
    board,
    artifact,
    selectedCard,
    selectedCardRuntime,
    loadingBoard,
    loadingCard,
    refreshing,
    actingOnCard,
    error,
    loadBoard,
    refreshBoard,
    loadCard,
    applyCardAction,
    clearSelectedCard,
  } = useOrchestrationBoardStore()
  const loadedWorkspaceKeyRef = useRef<string | null>(null)
  const setActivityFocus = useActivityFocusStore((s) => s.setFocus)
  const setActiveView = useViewStore((s) => s.setActiveView)
  const setSelectedRoom = useViewStore((s) => s.setSelectedRoom)
  const [showArchived, setShowArchived] = useState(false)
  const workspaceKey = `${workspaceID?.trim() || '_workspace'}:${showArchived ? 'archived' : 'active'}`
  const workspaceLabel = workspaceBadgeLabel(workspaceID ?? '')
  const workspaceTitle = workspaceID?.trim() || 'unscoped (all workspaces)'
  const [showGenerator, setShowGenerator] = useState(false)
  const [generatorMode, setGeneratorMode] = useState<'self' | 'ai'>('self')
  const [seedText, setSeedText] = useState('')
  const [aiGoal, setAIGoal] = useState('')
  const [aiCount, setAICount] = useState(5)
  const [generatorBusy, setGeneratorBusy] = useState(false)
  const [generatorError, setGeneratorError] = useState<string | null>(null)
  const [cleanupBusy, setCleanupBusy] = useState(false)

  useEffect(() => {
    if (loadedWorkspaceKeyRef.current === workspaceKey) return
    loadedWorkspaceKeyRef.current = workspaceKey
    clearSelectedCard()
    void loadBoard({ workspace_id: workspaceID, archived_only: showArchived })
  }, [clearSelectedCard, loadBoard, showArchived, workspaceID, workspaceKey])

  const lanes = useMemo(() => sortLanes(board?.lanes ?? []), [board?.lanes])
  const generatedAt = board?.generated_at || artifact?.generated_at
  const roomsQuery = useQuery({
    queryKey: ['rooms', workspaceID, 'runtime-summary'],
    enabled: isPathWorkspace(workspaceID),
    retry: false,
    queryFn: () => listRooms({ workspace_id: workspaceID!.trim(), limit: 100 }),
    staleTime: 5000,
  })
  const roomsByAgent = useMemo(
    () => indexRoomsByActor(roomsQuery.data?.rooms ?? []),
    [roomsQuery.data?.rooms],
  )

  const openRoom = (room: Room) => {
    setSelectedRoom(room.id, room.workspace_id)
    setActiveView('rooms')
  }

  const openEventsForCard = (card: OrchestrationCard) => {
    setActivityFocus({
      traceIDs: [],
      sourceSurface,
      label: `issue ${card.issue_identifier || card.issue_id}`,
    })
    setActiveView('events')
  }

  const handleAISuggest = async () => {
    if (!aiGoal.trim()) {
      setGeneratorError('Add a goal so AI can suggest cards.')
      return
    }
    setGeneratorBusy(true)
    setGeneratorError(null)
    try {
      const response = await companionChat({
        conversation_id: `orchestration-seed-${Date.now()}`,
        message: [
          'Generate a concise implementation card list for this goal.',
          `Return ONLY JSON array with objects: {"issue_identifier":"OPTIONAL","title":"REQUIRED"}.`,
          `Maximum cards: ${Math.max(1, Math.min(20, aiCount))}.`,
          `Goal: ${aiGoal.trim()}`,
        ].join('\n'),
        response_schema: {
          type: 'array',
          items: {
            type: 'object',
            additionalProperties: false,
            properties: {
              issue_identifier: { type: 'string' },
              title: { type: 'string' },
            },
            required: ['title'],
          },
        },
        response_keys: ['issue_identifier', 'title'],
      })
      const parsed = parseAISuggestedCards(response.response)
      if (parsed.length === 0) {
        throw new Error('No cards could be parsed from AI response.')
      }
      setSeedText(cardsToSeedText(parsed))
      setGeneratorMode('self')
    } catch (err) {
      setGeneratorError(err instanceof Error ? err.message : 'Failed to generate cards with AI')
    } finally {
      setGeneratorBusy(false)
    }
  }

  const handleCreateCards = async () => {
    const cards = parseSeedCardsFromText(seedText)
    if (cards.length === 0) {
      setGeneratorError('Add at least one card (one line per card).')
      return
    }
    setGeneratorBusy(true)
    setGeneratorError(null)
    try {
      await seedOrchestrationCards({
        request_id: localRequestID('seed-cards'),
        workspace_id: workspaceID,
        cards,
      })
      await refreshBoard({ workspaceID, archivedOnly: showArchived })
      setSeedText('')
      setAIGoal('')
      setShowGenerator(false)
    } catch (err) {
      setGeneratorError(err instanceof Error ? err.message : 'Failed to seed cards')
    } finally {
      setGeneratorBusy(false)
    }
  }

  const handleCardAction = async (card: OrchestrationCard, action: OrchestrationCardAction) => {
    await applyCardAction(card.issue_id, action, workspaceID)
  }

  const visibleIssueIDs = useMemo(
    () => lanes.flatMap((lane) => lane.cards.map((card) => card.issue_id)).filter(Boolean),
    [lanes],
  )

  const handleArchiveActionForVisibleCards = async () => {
    if (!workspaceID?.trim() || visibleIssueIDs.length === 0) return
    if (
      !window.confirm(
        `${showArchived ? 'Restore' : 'Archive'} ${visibleIssueIDs.length} visible orchestration card${visibleIssueIDs.length === 1 ? '' : 's'} for workspace "${workspaceLabel}"?`,
      )
    ) {
      return
    }
    setCleanupBusy(true)
    setGeneratorError(null)
    try {
      const request = {
        request_id: localRequestID(showArchived ? 'restore-cards' : 'archive-cards'),
        workspace_id: workspaceID,
        issue_ids: visibleIssueIDs,
      }
      if (showArchived) {
        await restoreOrchestrationCards(request)
      } else {
        await archiveOrchestrationCards(request)
      }
      clearSelectedCard()
      await loadBoard({ workspace_id: workspaceID, archived_only: showArchived })
    } catch (err) {
      setGeneratorError(err instanceof Error ? err.message : 'Failed to update visible cards')
    } finally {
      setCleanupBusy(false)
    }
  }

  return (
    <Card className={cn('border-border bg-card/70', className)}>
      <CardHeader className="pb-2">
        <div className="flex items-center justify-between gap-3">
          <div className="min-w-0">
            <div className="flex items-center gap-2">
              <LayoutGrid className="h-4 w-4 text-muted-foreground" />
              <h3 className="text-sm font-semibold text-foreground">Orchestration Board</h3>
            </div>
            {generatedAt && (
              <p className="text-xs text-muted-foreground mt-1">
                Updated {formatRelativeTime(generatedAt)}
              </p>
            )}
            <p className="text-[11px] text-muted-foreground mt-1" title={workspaceTitle}>
              Workspace: <code>{workspaceLabel}</code>
            </p>
          </div>
          <div className="flex items-center gap-2">
            {workspaceID?.trim() && visibleIssueIDs.length > 0 && (
              <Button
                variant="outline"
                size="sm"
                className="h-8"
                onClick={() => void handleArchiveActionForVisibleCards()}
                disabled={cleanupBusy || refreshing || loadingBoard}
              >
                <ArchiveRestore className={cn('h-3.5 w-3.5 mr-1', cleanupBusy && 'animate-pulse')} />
                {showArchived ? 'Recover Visible Cards' : 'Archive Visible Cards'}
              </Button>
            )}
            <Button
              variant={showArchived ? 'secondary' : 'outline'}
              size="sm"
              className="h-8"
              onClick={() => setShowArchived((value) => !value)}
              disabled={cleanupBusy || refreshing || loadingBoard}
            >
              {showArchived ? 'Show Active' : 'Show Archived'}
            </Button>
            <Button
              variant={showGenerator ? 'secondary' : 'outline'}
              size="sm"
              className="h-8"
              onClick={() => {
                setShowGenerator((value) => !value)
                setGeneratorError(null)
              }}
              disabled={generatorBusy}
            >
              <Sparkles className="h-3.5 w-3.5 mr-1" />
              Generate
            </Button>
            <Button
              variant="outline"
              size="sm"
              className="h-8"
              onClick={() => void refreshBoard({ workspaceID, archivedOnly: showArchived })}
              disabled={refreshing || loadingBoard}
            >
              <RefreshCw className={cn('h-3.5 w-3.5 mr-1', (refreshing || loadingBoard) && 'animate-spin')} />
              Refresh
            </Button>
          </div>
        </div>
      </CardHeader>
      <CardContent className="space-y-3">
        {error && (
          <div className="rounded-md border border-red-500/30 bg-red-500/5 px-3 py-2 text-xs text-red-300">
            {error}
          </div>
        )}

        {showGenerator && (
          <div className="rounded-md border border-border bg-background/40 px-3 py-3 space-y-3">
            <div className="text-[11px] text-muted-foreground" title={workspaceTitle}>
              New cards will be created in workspace <code>{workspaceLabel}</code>.
            </div>
            <div className="flex items-center gap-2">
              <Button
                variant={generatorMode === 'self' ? 'secondary' : 'outline'}
                size="sm"
                className="h-7 text-[11px]"
                onClick={() => setGeneratorMode('self')}
                disabled={generatorBusy}
              >
                Self
              </Button>
              <Button
                variant={generatorMode === 'ai' ? 'secondary' : 'outline'}
                size="sm"
                className="h-7 text-[11px]"
                onClick={() => setGeneratorMode('ai')}
                disabled={generatorBusy}
              >
                AI Assist
              </Button>
            </div>

            {generatorMode === 'ai' && (
              <div className="space-y-2">
                <Textarea
                  value={aiGoal}
                  onChange={(event) => setAIGoal(event.target.value)}
                  rows={3}
                  placeholder="Describe what this board should plan..."
                  className="text-xs"
                />
                <div className="flex items-center gap-2">
                  <Input
                    type="number"
                    min={1}
                    max={20}
                    value={aiCount}
                    onChange={(event) => setAICount(Number(event.target.value || 5))}
                    className="h-8 w-24 text-xs"
                  />
                  <Button
                    variant="outline"
                    size="sm"
                    className="h-8 text-xs"
                    onClick={() => void handleAISuggest()}
                    disabled={generatorBusy}
                  >
                    <Sparkles className={cn('h-3.5 w-3.5 mr-1', generatorBusy && 'animate-pulse')} />
                    Suggest
                  </Button>
                </div>
              </div>
            )}

            <Textarea
              value={seedText}
              onChange={(event) => setSeedText(event.target.value)}
              rows={6}
              placeholder={'One card per line.\nFormat: ISSUE-ID | Title\nor: Title only'}
              className="text-xs font-mono"
            />
            <p className="text-[11px] text-muted-foreground">
              Card format: <code>ISSUE-ID | title</code> or <code>title</code>.
            </p>

            {generatorError && (
              <div className="rounded-md border border-red-500/30 bg-red-500/5 px-2 py-1.5 text-[11px] text-red-300">
                {generatorError}
              </div>
            )}

            <div className="flex items-center justify-end gap-2">
              <Button
                variant="ghost"
                size="sm"
                className="h-8 text-xs"
                onClick={() => {
                  setShowGenerator(false)
                  setGeneratorError(null)
                }}
                disabled={generatorBusy}
              >
                Cancel
              </Button>
              <Button
                variant="secondary"
                size="sm"
                className="h-8 text-xs"
                onClick={() => void handleCreateCards()}
                disabled={generatorBusy}
              >
                {generatorBusy ? 'Working…' : 'Create Cards'}
              </Button>
            </div>
          </div>
        )}

        {board && (
          <div className="flex flex-wrap gap-1.5">
            {ORCHESTRATION_LANE_ORDER.map((lane) => (
              <Badge key={lane} variant="secondary" className="text-[10px]">
                {lane} {laneCount(board.counts, lane)}
              </Badge>
            ))}
          </div>
        )}

        {artifact && (
          <div className="rounded-md border border-amber-500/30 bg-amber-500/5 px-3 py-2 text-xs text-amber-200 space-y-1">
            <div className="inline-flex items-center gap-1.5">
              <TriangleAlert className="h-3.5 w-3.5" />
              Board payload was artifactized
            </div>
            <div>{artifact.summary}</div>
            <div className="font-mono text-[11px] text-amber-300">{artifact.artifact}</div>
            {artifact.hint && <div>{artifact.hint}</div>}
          </div>
        )}

        {!board && !artifact && loadingBoard && (
          <div className="text-xs text-muted-foreground">Loading orchestration board…</div>
        )}

        {board && lanes.length === 0 && (
          <div className="text-xs text-muted-foreground">No projected lanes yet.</div>
        )}

        {board && lanes.length > 0 && (
          <div className="overflow-x-auto">
            <div className="flex gap-3 pb-1 min-w-max">
              {lanes.map((lane) => {
                const cards = Array.isArray(lane.cards) ? lane.cards : []
                return (
                  <div key={lane.id} className="w-72 rounded-md border border-border bg-background/40 p-2">
                  <div className="flex items-center justify-between mb-2">
                    <div className="text-xs font-medium uppercase tracking-wide text-muted-foreground">
                      {lane.title || lane.id}
                    </div>
                    <Badge variant="outline" className="text-[10px]">
                      {cards.length}
                    </Badge>
                  </div>
                  <div className="space-y-2 max-h-80 overflow-y-auto pr-1">
                    {cards.map((card) => (
                      <div key={card.issue_id} className="rounded-md border border-border bg-card p-2 space-y-2">
                        <div className="space-y-1">
                          <div className="text-xs font-medium text-foreground line-clamp-2">
                            {cardTitle(card)}
                          </div>
                          <div className="text-[11px] text-muted-foreground flex items-center gap-2 flex-wrap">
                            {card.last_outcome && (
                              <Badge variant="outline" className="text-[10px]">
                                {card.last_outcome}
                              </Badge>
                            )}
                            {card.policy_status && (
                              <Badge variant="secondary" className="text-[10px]">
                                {card.policy_status}
                              </Badge>
                            )}
                            {card.attempt && card.attempt > 0 && (
                              <span>attempt {card.attempt}</span>
                            )}
                          </div>
                        </div>
                        <div className="flex items-center justify-between">
                          <Button
                            variant="ghost"
                            size="sm"
                            className="h-7 px-2 text-[11px]"
                            onClick={() => void loadCard(card.issue_id, workspaceID)}
                            disabled={loadingCard || actingOnCard}
                          >
                            Inspect
                          </Button>
                          <Button
                            variant="ghost"
                            size="sm"
                            className="h-7 px-2 text-[11px]"
                            onClick={() => openEventsForCard(card)}
                          >
                            Events
                            <ArrowRight className="h-3 w-3 ml-1" />
                          </Button>
                        </div>
                      </div>
                    ))}
                    {cards.length === 0 && (
                      <div className="text-[11px] text-muted-foreground px-1 py-2">No cards in this lane.</div>
                    )}
                  </div>
                  </div>
                )
              })}
            </div>
          </div>
        )}

        {selectedCard && (
          <div className="rounded-md border border-border bg-muted/20 px-3 py-2 space-y-3">
            <div className="flex items-center justify-between gap-2 flex-wrap">
              <div className="text-xs font-medium text-foreground">Card Detail</div>
              <div className="flex items-center gap-2 flex-wrap">
                <Button
                  variant="ghost"
                  size="sm"
                  className="h-7 px-2 text-[11px]"
                  onClick={() => void loadCard(selectedCard.issue_id, workspaceID)}
                  disabled={loadingCard || actingOnCard}
                >
                  <RefreshCw className={cn('h-3 w-3', loadingCard && 'animate-spin')} />
                  Reload
                </Button>
                {canRetryNow(selectedCard) && (
                  <Button
                    variant="outline"
                    size="sm"
                    className="h-7 px-2 text-[11px]"
                    onClick={() => void handleCardAction(selectedCard, 'retry-now')}
                    disabled={actingOnCard}
                  >
                    <RotateCcw className={cn('h-3 w-3', actingOnCard && 'animate-spin')} />
                    Retry Now
                  </Button>
                )}
                {canRelease(selectedCard) && (
                  <Button
                    variant="outline"
                    size="sm"
                    className="h-7 px-2 text-[11px]"
                    onClick={() => void handleCardAction(selectedCard, 'release')}
                    disabled={actingOnCard}
                  >
                    <Undo2 className="h-3 w-3" />
                    Release
                  </Button>
                )}
                {canMarkDone(selectedCard) && (
                  <Button
                    variant="secondary"
                    size="sm"
                    className="h-7 px-2 text-[11px]"
                    onClick={() => void handleCardAction(selectedCard, 'mark-done')}
                    disabled={actingOnCard}
                  >
                    <CheckCircle2 className="h-3 w-3" />
                    Mark Done
                  </Button>
                )}
                <Button
                  variant="ghost"
                  size="sm"
                  className="h-7 px-2 text-[11px]"
                  onClick={clearSelectedCard}
                  disabled={actingOnCard}
                >
                  Close
                </Button>
              </div>
            </div>
            <div className="grid grid-cols-2 gap-2 text-[11px]">
              <div><span className="text-muted-foreground">Issue:</span> {detailValue(selectedCard.issue_identifier || selectedCard.issue_id)}</div>
              <div><span className="text-muted-foreground">Lane:</span> {detailValue(selectedCard.lane)}</div>
              <div><span className="text-muted-foreground">Workspace:</span> {detailValue(selectedCard.workspace_id)}</div>
              <div><span className="text-muted-foreground">Agent:</span> {detailValue(selectedCard.agent_id)}</div>
              <div><span className="text-muted-foreground">State:</span> {detailValue(selectedCard.state)}</div>
              <div><span className="text-muted-foreground">Policy:</span> {detailValue(selectedCard.policy_status)}</div>
              <div><span className="text-muted-foreground">Outcome:</span> {detailValue(selectedCard.last_outcome)}</div>
              <div><span className="text-muted-foreground">Eligibility:</span> {detailValue(selectedCard.eligibility)}</div>
              <div><span className="text-muted-foreground">Run:</span> {detailValue(selectedCard.run_id)}</div>
              <div><span className="text-muted-foreground">Actor:</span> {detailValue(selectedCard.actor_id)}</div>
            </div>
            {selectedCard.denial_reason && (
              <div className="text-[11px] text-amber-300">
                <span className="text-muted-foreground">Reason:</span> {selectedCard.denial_reason}
              </div>
            )}
            {(selectedCard.retry_due_at || selectedCard.last_event_at) && (
              <div className="text-[11px] text-muted-foreground flex items-center gap-3">
                {selectedCard.retry_due_at && (
                  <span className="inline-flex items-center gap-1">
                    <Clock className="h-3 w-3" />
                    retry {formatRelativeTime(selectedCard.retry_due_at)}
                  </span>
                )}
                {selectedCard.last_event_at && (
                  <span className="inline-flex items-center gap-1">
                    <Clock className="h-3 w-3" />
                    event {formatRelativeTime(selectedCard.last_event_at)}
                  </span>
                )}
              </div>
            )}
            <div className="rounded-md border border-border bg-background/40 px-3 py-2 space-y-2">
              <div className="flex items-center gap-2 text-xs font-medium text-foreground">
                <GitBranch className="h-3.5 w-3.5 text-muted-foreground" />
                Runtime Tree
                {selectedCardRuntime?.enabled && (
                  <Badge variant="secondary" className="text-[10px]">
                    live
                  </Badge>
                )}
              </div>
              {selectedCardRuntime?.agent_id && (
                <div className="text-[11px] text-muted-foreground">
                  root agent <code>{selectedCardRuntime.agent_id}</code>
                </div>
              )}
              {selectedCardRuntime?.error && (
                <div className="rounded-md border border-amber-500/30 bg-amber-500/5 px-2 py-1.5 text-[11px] text-amber-200">
                  {selectedCardRuntime.error}
                </div>
              )}
              {selectedCardRuntime?.root ? (
                <RuntimeTreeNodeView
                  node={selectedCardRuntime.root}
                  roomsByAgent={roomsByAgent}
                  onOpenRoom={openRoom}
                />
              ) : (
                <div className="text-[11px] text-muted-foreground">
                  {selectedCard.agent_id ? 'Runtime tree is unavailable.' : 'No runtime agent is attached to this card.'}
                </div>
              )}
            </div>
          </div>
        )}
      </CardContent>
    </Card>
  )
}
