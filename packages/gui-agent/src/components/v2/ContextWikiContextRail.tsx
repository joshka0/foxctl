import { type ReactNode, useEffect, useMemo, useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import {
  getContextNextProposalMerge,
  getContextOverview,
  listWorkspaces,
  mergeContextProposal,
  releaseContextProposalMerge,
} from '@/api/client'
import type {
  ContextWikiEvidenceImportRun,
  ContextWikiOverview,
  ContextWikiMaintenanceTask,
  ContextWikiMemoryProposal,
  ContextWikiPromotionJob,
} from '@/types/contextwiki'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardHeader } from '@/components/ui/card'
import { CollapsibleSection } from '@/components/ui/collapsible-section'
import { HelpTooltip } from '@/components/ui/tooltip'
import { humanReadableWorkspacePath } from '@/lib/room-utils'
import { formatRelativeTime } from '@/lib/utils'
import { useActivityStore } from '@/stores/activityStore'
import { useViewStore } from '@/stores/viewStore'
import {
  ArrowRight,
  BrainCircuit,
  FileClock,
  GitMerge,
  Layers3,
  RotateCcw,
  Sparkles,
} from 'lucide-react'

const CONTEXTWIKI_OVERVIEW_LIMIT = 6

export function ContextWikiContextRail({
  selectedAgentWorkspaceRoot,
}: {
  selectedAgentWorkspaceRoot?: string
}) {
  const queryClient = useQueryClient()
  const activityEvents = useActivityStore((s) => s.events)
  const selectedContextWorkspace = useViewStore((s) => s.selectedContextWorkspace)
  const setSelectedContextWorkspace = useViewStore((s) => s.setSelectedContextWorkspace)
  const selectedAgentWorkspace = selectedAgentWorkspaceRoot?.trim() ?? ''
  const [actionError, setActionError] = useState<string | null>(null)

  const { data: workspacesData } = useQuery({
    queryKey: ['workspaces'],
    queryFn: listWorkspaces,
    staleTime: 10000,
  })
  const currentWorkspace = (workspacesData?.current ?? '').trim()

  const workspaceOptions = useMemo(() => {
    const workspaceEntries = workspacesData?.workspaces ?? []
    const seen = new Set<string>()
    const out: string[] = []
    for (const value of [
      selectedAgentWorkspace,
      currentWorkspace,
      ...workspaceEntries.map((workspace) => workspace.path.trim()),
    ]) {
      if (!value || seen.has(value)) continue
      seen.add(value)
      out.push(value)
    }
    return out
  }, [currentWorkspace, selectedAgentWorkspace, workspacesData?.workspaces])

  const effectiveWorkspaceRoot =
    selectedContextWorkspace?.trim() ||
    selectedAgentWorkspace ||
    currentWorkspace ||
    ''

  const autoWorkspaceLabel = selectedAgentWorkspace
    ? 'Use selected agent project'
    : currentWorkspace
      ? 'Use current project'
      : 'Choose automatically'

  const overviewQuery = useQuery({
    queryKey: ['contextwiki-overview', effectiveWorkspaceRoot],
    queryFn: () =>
      getContextOverview({
        workspace: effectiveWorkspaceRoot,
        limit: CONTEXTWIKI_OVERVIEW_LIMIT,
      }),
    enabled: effectiveWorkspaceRoot.length > 0,
    staleTime: 5000,
    refetchOnWindowFocus: false,
    refetchInterval: 15000,
  })

  const overview = overviewQuery.data ?? null
  const claimedTask = overview?.claimed_proposal_merge ?? null
  const nextTask = overview?.next_proposal_merge ?? null
  const activeTask = claimedTask ?? nextTask
  const activePacket = activeTask?.work_packet ?? null
  const hasVaultPath = Boolean(overview?.vault_path?.trim())
  const mergeDisabled = Boolean(
    !activePacket?.proposal_id ||
      (activePacket?.requires_vault_path && !hasVaultPath),
  )

  const contextMutationSignal = useMemo(() => {
    const relevant = activityEvents.find((event) => {
      if (!event.operation.startsWith('web.context.')) return false
      if (
        event.workspace_id &&
        effectiveWorkspaceRoot &&
        event.workspace_id !== effectiveWorkspaceRoot
      ) {
        return false
      }
      return (
        event.operation === 'web.context.next_proposal_merge' ||
        event.operation === 'web.context.proposal_release_merge' ||
        event.operation === 'web.context.proposal_merge'
      )
    })
    return relevant?.ts ?? ''
  }, [activityEvents, effectiveWorkspaceRoot])

  useEffect(() => {
    if (!contextMutationSignal || !effectiveWorkspaceRoot) return
    void queryClient.invalidateQueries({ queryKey: ['contextwiki-overview', effectiveWorkspaceRoot] })
  }, [contextMutationSignal, effectiveWorkspaceRoot, queryClient])

  const refresh = async () => {
    setActionError(null)
    await queryClient.invalidateQueries({ queryKey: ['contextwiki-overview', effectiveWorkspaceRoot] })
    await overviewQuery.refetch()
  }

  const claimMutation = useMutation({
    mutationFn: () =>
      getContextNextProposalMerge({
        workspace: effectiveWorkspaceRoot,
        claim: true,
        vault_path: overview?.vault_path,
      }),
    onSuccess: async () => {
      setActionError(null)
      await refresh()
    },
    onError: (error) => {
      setActionError(error instanceof Error ? error.message : 'Failed to take review item')
    },
  })

  const releaseMutation = useMutation({
    mutationFn: async () => {
      if (!claimedTask?.work_packet?.proposal_id) {
        throw new Error('No review item is currently checked out')
      }
      return releaseContextProposalMerge({
        workspace: effectiveWorkspaceRoot,
        proposal_id: claimedTask.work_packet.proposal_id,
      })
    },
    onSuccess: async () => {
      setActionError(null)
      await refresh()
    },
    onError: (error) => {
      setActionError(error instanceof Error ? error.message : 'Failed to release review item')
    },
  })

  const mergeMutation = useMutation({
    mutationFn: async () => {
      if (!activePacket?.proposal_id) {
        throw new Error('No memory update is ready to add')
      }
      return mergeContextProposal({
        workspace: effectiveWorkspaceRoot,
        proposal_id: activePacket.proposal_id,
        vault_path: overview?.vault_path,
      })
    },
    onSuccess: async () => {
      setActionError(null)
      await refresh()
    },
    onError: (error) => {
      setActionError(error instanceof Error ? error.message : 'Failed to add update to project memory')
    },
  })

  if (!effectiveWorkspaceRoot && workspaceOptions.length === 0) {
    return (
      <Card className="border-border/80">
        <CardHeader className="py-3">
          <div className="flex items-center gap-2">
            <Layers3 className="h-4 w-4" />
            <div className="text-sm font-medium">Project Memory</div>
          </div>
        </CardHeader>
        <CardContent className="pt-0 pb-3 text-xs text-muted-foreground">
          Choose a project first. This surface shows the notes, sources, and
          suggested updates that keep project knowledge from resetting between sessions.
        </CardContent>
      </Card>
    )
  }

  return (
    <div className="space-y-3">
      <Card className="border-border/80 bg-[linear-gradient(135deg,rgba(8,47,73,0.09),rgba(6,78,59,0.07)_48%,transparent_100%)]">
        <CardHeader className="py-3">
          <div className="flex flex-col gap-4 lg:flex-row lg:items-start lg:justify-between">
            <div className="space-y-2">
              <div className="flex items-center gap-2">
                <Sparkles className="h-4 w-4 text-emerald-400" />
                <div className="text-sm font-medium">Project Memory</div>
                <HelpTooltip
                  side="bottom"
                  content="Project Memory keeps useful project knowledge from resetting between sessions. It collects outside sources, repeated corrections, and reviewed notes before they become durable memory."
                />
                <Badge variant="outline" className="text-[10px]">
                  keeps context across sessions
                </Badge>
              </div>
              <div className="max-w-2xl text-sm text-muted-foreground">
                Keep useful project knowledge from resetting. Imported sources,
                repeated corrections, and reviewed updates all land here before
                they become durable memory.
              </div>
              <div className="grid gap-2 pt-1 md:grid-cols-3">
                <StepPill
                  title="1. Bring in a source"
                  description="Transcript, note, or repeated correction"
                />
                <StepPill
                  title="2. Review the suggestion"
                  description="Check what should be kept and where it belongs"
                />
                <StepPill
                  title="3. Add it to memory"
                  description="Merge the reviewed update into durable project notes"
                />
              </div>
            </div>

            <div className="flex w-full flex-col gap-2 lg:w-[22rem]">
              <div className="text-[10px] uppercase tracking-[0.18em] text-muted-foreground">
                <span className="inline-flex items-center gap-1">
                  Project
                  <HelpTooltip
                    side="left"
                    content="Choose which project workspace to inspect. Memory items, sources, and review queues are scoped to the selected project."
                  />
                </span>
              </div>
              <select
                className="h-9 rounded-md border border-input bg-background px-3 text-sm shadow-sm outline-none focus-visible:ring-1 focus-visible:ring-ring"
                value={selectedContextWorkspace?.trim() || '__auto__'}
                onChange={(event) => {
                  const value = event.target.value
                  setSelectedContextWorkspace(value === '__auto__' ? null : value)
                }}
              >
                <option value="__auto__">{autoWorkspaceLabel}</option>
                {workspaceOptions.map((workspace) => (
                  <option key={workspace} value={workspace}>
                    {humanReadableWorkspacePath(workspace)}
                  </option>
                ))}
              </select>
              {effectiveWorkspaceRoot && (
                <div className="rounded-md border border-border/60 bg-background/70 px-3 py-2 text-[11px] text-muted-foreground">
                  Viewing memory for {humanReadableWorkspacePath(effectiveWorkspaceRoot)}
                </div>
              )}
            </div>
          </div>
        </CardHeader>
        <CardContent className="pt-0 pb-4 space-y-3">
          <div className="grid gap-3 xl:grid-cols-[1.15fr,0.85fr]">
            <ReviewQueueCard
              overview={overview}
              activeTask={activeTask}
              claimedTask={claimedTask}
              actionError={actionError}
              mergeDisabled={mergeDisabled}
              onClaim={() => {
                setActionError(null)
                claimMutation.mutate()
              }}
              onRelease={() => {
                setActionError(null)
                releaseMutation.mutate()
              }}
              onMerge={() => {
                setActionError(null)
                mergeMutation.mutate()
              }}
              onRefresh={() => void refresh()}
              busy={
                overviewQuery.isFetching ||
                claimMutation.isPending ||
                releaseMutation.isPending ||
                mergeMutation.isPending
              }
            />
            <div className="grid grid-cols-2 gap-2">
              <MetricTile
                label="Suggested Updates"
                value={overview?.stats.active_proposal_count ?? 0}
                hint={`${overview?.stats.proposal_count ?? 0} total ideas recorded`}
                tooltip="Suggestions are candidate memory changes the system found from imported sources, repeated corrections, or retrieval review."
              />
              <MetricTile
                label="Ready to Review"
                value={overview?.stats.prepared_merge_count ?? 0}
                hint={`${overview?.stats.claimed_merge_count ?? 0} already taken`}
                tooltip="These updates are prepared and waiting for a human or agent to review before they are added to durable memory."
              />
              <MetricTile
                label="Imported Sources"
                value={overview?.stats.evidence_import_count ?? 0}
                hint="outside notes and transcripts"
                tooltip="Outside material that has been brought into the system, such as transcripts, notes, or text files."
              />
              <MetricTile
                label="Added to Memory"
                value={overview?.stats.promotion_merged_count ?? 0}
                hint={`${overview?.stats.promotion_draft_count ?? 0} drafts still open`}
                tooltip="Reviewed updates that have already been merged into durable project notes."
              />
            </div>
          </div>
        </CardContent>
      </Card>

      <div className="grid gap-3 xl:grid-cols-3">
        <ListCard
          title="Suggested Updates"
          icon={<BrainCircuit className="h-4 w-4" />}
          badge={overview?.proposals.length ?? 0}
          loading={overviewQuery.isLoading}
          emptyMessage="Nothing has been suggested for this project yet."
          tooltip="A short list of recent memory suggestions, including imported evidence drafts and other proposed updates."
        >
          {(overview?.proposals ?? []).map((proposal) => (
            <ProposalRow key={proposal.id} proposal={proposal} />
          ))}
        </ListCard>

        <ListCard
          title="Imported Sources"
          icon={<FileClock className="h-4 w-4" />}
          badge={overview?.evidence_imports.length ?? 0}
          loading={overviewQuery.isLoading}
          emptyMessage="No outside sources have been imported for this project yet."
          tooltip="Recent outside material that entered the memory pipeline for this project."
        >
          {(overview?.evidence_imports ?? []).map((item) => (
            <EvidenceRow key={item.id} item={item} />
          ))}
        </ListCard>

        <ListCard
          title="Recent Memory Changes"
          icon={<GitMerge className="h-4 w-4" />}
          badge={overview?.promotion_jobs.length ?? 0}
          loading={overviewQuery.isLoading}
          emptyMessage="No memory changes have been recorded yet."
          tooltip="Recent promotion jobs showing what has been drafted or already added to durable memory."
        >
          {(overview?.promotion_jobs ?? []).map((job) => (
            <PromotionRow key={job.id} job={job} />
          ))}
        </ListCard>
      </div>

      <Card className="border-border/70">
        <CollapsibleSection
          title="Advanced Details"
          icon={<Layers3 className="h-3.5 w-3.5" />}
          defaultOpen={false}
          badge={effectiveWorkspaceRoot ? 'ready' : 'idle'}
        >
          <div className="grid gap-2 text-xs text-muted-foreground sm:grid-cols-2">
            <div className="rounded-md border border-border/60 bg-muted/20 px-3 py-2">
              Memory source: {selectedContextWorkspace ? 'manual project selection' : autoWorkspaceLabel.toLowerCase()}
            </div>
            <div className="rounded-md border border-border/60 bg-muted/20 px-3 py-2">
              Vault path: {overview?.vault_path ? humanReadableWorkspacePath(overview.vault_path) : 'not configured'}
            </div>
          </div>
        </CollapsibleSection>
      </Card>
    </div>
  )
}

function ReviewQueueCard({
  overview,
  activeTask,
  claimedTask,
  actionError,
  mergeDisabled,
  onClaim,
  onRelease,
  onMerge,
  onRefresh,
  busy,
}: {
  overview: ContextWikiOverview | null
  activeTask: ContextWikiMaintenanceTask | null
  claimedTask: ContextWikiMaintenanceTask | null
  actionError: string | null
  mergeDisabled: boolean
  onClaim: () => void
  onRelease: () => void
  onMerge: () => void
  onRefresh: () => void
  busy: boolean
}) {
  const packet = activeTask?.work_packet ?? null
  const isClaimed = Boolean(claimedTask)

  return (
    <div className="rounded-xl border border-border/70 bg-background/85 p-4 shadow-sm">
      <div className="flex flex-wrap items-start justify-between gap-3">
        <div className="space-y-1">
          <div className="flex items-center gap-2">
            <GitMerge className="h-4 w-4" />
            <div className="text-sm font-medium">Ready to Add</div>
            <HelpTooltip
              side="bottom"
              content="This queue shows the next memory update that looks worth keeping. Review it, then add it to durable project notes when it is correct."
            />
            <Badge className={statusBadgeClass(isClaimed ? 'claimed' : activeTask ? 'ready' : 'idle')}>
              {isClaimed ? 'in review' : activeTask ? 'ready' : 'nothing waiting'}
            </Badge>
          </div>
          <div className="text-xs text-muted-foreground">
            Review the next memory update that looks worth keeping.
          </div>
        </div>
        <Button
          variant="ghost"
          size="sm"
          className="h-7 text-[11px]"
          disabled={busy}
          onClick={onRefresh}
        >
          Refresh
        </Button>
      </div>

      {!activeTask || !packet ? (
        <div className="mt-4 space-y-3 rounded-lg border border-dashed border-border/70 px-3 py-4">
          <div className="text-sm font-medium text-foreground">
            Nothing needs review right now.
          </div>
          <div className="text-xs text-muted-foreground">
            When the system finds something worth keeping from imported sources
            or repeated corrections, it will show up here as a suggested memory update.
          </div>
          <div className="flex flex-wrap gap-1.5">
            <Badge variant="outline" className="text-[10px]">
              {overview?.stats.evidence_import_count ?? 0} sources imported
            </Badge>
            <Badge variant="outline" className="text-[10px]">
              {overview?.stats.promotion_merged_count ?? 0} updates already added
            </Badge>
          </div>
        </div>
      ) : (
        <div className="mt-4 space-y-3">
          <div className="space-y-1">
            <div className="text-sm font-medium text-foreground">
              {activeTask.title || packet.target_path || packet.proposal_id}
            </div>
            <div className="text-xs text-muted-foreground">{activeTask.reason}</div>
          </div>

          <div className="flex flex-wrap gap-1.5">
            <Badge variant="outline" className="text-[10px]">
              {proposalKindLabel(packet.proposal_kind)}
            </Badge>
            {packet.target_path && (
              <Badge variant="outline" className="text-[10px]">
                suggested note {shortTail(packet.target_path)}
              </Badge>
            )}
            {packet.heading && (
              <Badge variant="secondary" className="text-[10px]">
                section {packet.heading}
              </Badge>
            )}
            {overview?.vault_path ? (
              <Badge className="text-[10px] bg-emerald-500/10 text-emerald-500 border-emerald-500/20">
                merge enabled
              </Badge>
            ) : (
              <Badge className="text-[10px] bg-amber-500/10 text-amber-500 border-amber-500/20">
                merge setup needed
              </Badge>
            )}
          </div>

          <div className="grid gap-2 sm:grid-cols-2">
            <InfoBlock
              label="Draft note"
              value={packet.draft_path || 'n/a'}
              tooltip="The temporary note or draft content produced from the source before it is merged into durable memory."
            />
            <InfoBlock
              label="Suggested destination"
              value={packet.target_path || 'n/a'}
              tooltip="The project note ContextWiki thinks this update should land in after review."
            />
          </div>

          {actionError && (
            <div className="rounded-md border border-red-500/20 bg-red-500/10 px-3 py-2 text-[11px] text-red-400">
              {actionError}
            </div>
          )}

          {mergeDisabled && (
            <div className="rounded-md border border-amber-500/20 bg-amber-500/10 px-3 py-2 text-[11px] text-amber-300">
              GUI merging is unavailable until the server knows where the project
              vault lives. Set `FOXCTL_CONTEXTWIKI_VAULT_PATH` or
              `FOXCTL_OBSIDIAN_VAULT_PATH` on the server, then refresh this
              page.
            </div>
          )}

          <div className="flex flex-wrap gap-2">
            {!isClaimed && (
              <Button size="sm" className="h-7 text-[11px]" disabled={busy} onClick={onClaim}>
                Take Review
              </Button>
            )}
            {isClaimed && (
              <Button
                variant="outline"
                size="sm"
                className="h-7 text-[11px]"
                disabled={busy}
                onClick={onRelease}
              >
                <RotateCcw className="mr-1 h-3 w-3" />
                Release
              </Button>
            )}
            <Button
              variant="secondary"
              size="sm"
              className="h-7 text-[11px]"
              disabled={busy || mergeDisabled}
              onClick={onMerge}
            >
              Add to Memory
            </Button>
          </div>
        </div>
      )}

      {claimedTask && Math.max(0, (overview?.stats.prepared_merge_count ?? 0) - 1) > 0 ? (
        <div className="mt-3 flex items-center gap-2 rounded-md border border-border/60 bg-muted/15 px-3 py-2 text-[11px] text-muted-foreground">
          <ArrowRight className="h-3.5 w-3.5" />
          {Math.max(0, (overview?.stats.prepared_merge_count ?? 0) - 1)} more review item
          {Math.max(0, (overview?.stats.prepared_merge_count ?? 0) - 1) === 1 ? '' : 's'} waiting after the current one.
        </div>
      ) : null}
    </div>
  )
}

function StepPill({
  title,
  description,
}: {
  title: string
  description: string
}) {
  return (
    <div className="rounded-lg border border-border/60 bg-background/70 px-3 py-3">
      <div className="text-xs font-medium text-foreground">{title}</div>
      <div className="mt-1 text-[11px] leading-5 text-muted-foreground">
        {description}
      </div>
    </div>
  )
}

function MetricTile({
  label,
  value,
  hint,
  tooltip,
}: {
  label: string
  value: number
  hint: string
  tooltip?: string
}) {
  return (
    <div className="rounded-xl border border-border/70 bg-background/80 px-3 py-3">
      <div className="inline-flex items-center gap-1 text-[10px] uppercase tracking-[0.18em] text-muted-foreground">
        <span>{label}</span>
        {tooltip && <HelpTooltip content={tooltip} side="top" />}
      </div>
      <div className="mt-1 text-lg font-semibold text-foreground">{value}</div>
      <div className="mt-1 text-[11px] text-muted-foreground">{hint}</div>
    </div>
  )
}

function ListCard({
  title,
  icon,
  badge,
  loading,
  emptyMessage,
  tooltip,
  children,
}: {
  title: string
  icon: ReactNode
  badge: number
  loading: boolean
  emptyMessage: string
  tooltip?: string
  children: ReactNode
}) {
  const hasChildren = Array.isArray(children) ? children.length > 0 : Boolean(children)

  return (
    <Card className="border-border/70">
      <CardHeader className="py-3">
        <div className="flex items-center justify-between gap-2">
          <div className="flex items-center gap-2">
            {icon}
            <div className="text-sm font-medium">{title}</div>
            {tooltip && <HelpTooltip content={tooltip} side="top" />}
          </div>
          <Badge variant="outline" className="text-[10px]">
            {badge}
          </Badge>
        </div>
      </CardHeader>
      <CardContent className="pt-0 pb-3">
        {loading ? (
          <div className="text-xs text-muted-foreground">Loading…</div>
        ) : hasChildren ? (
          <div className="space-y-2">{children}</div>
        ) : (
          <div className="rounded-md border border-dashed border-border/70 px-3 py-4 text-xs text-muted-foreground">
            {emptyMessage}
          </div>
        )}
      </CardContent>
    </Card>
  )
}

function ProposalRow({ proposal }: { proposal: ContextWikiMemoryProposal }) {
  return (
    <div className="rounded-md border border-border/60 bg-muted/15 px-3 py-2">
      <div className="flex items-start justify-between gap-2">
        <div className="text-sm font-medium text-foreground">{proposal.summary}</div>
        <Badge className={statusBadgeClass(proposal.apply_status || proposal.status)}>
          {statusLabel(proposal.apply_status || proposal.status)}
        </Badge>
      </div>
      <div className="mt-2 flex flex-wrap gap-1.5">
        <Badge variant="outline" className="text-[10px]">
          {proposalKindLabel(proposal.kind)}
        </Badge>
        {proposal.blast_radius && (
          <Badge variant="secondary" className="text-[10px]">
            {proposal.blast_radius} impact
          </Badge>
        )}
        {proposal.count > 1 && (
          <Badge variant="secondary" className="text-[10px]">
            repeated {proposal.count}x
          </Badge>
        )}
      </div>
      <div className="mt-2 text-[11px] text-muted-foreground">
        updated {formatRelativeTime(proposal.updated_at)}
      </div>
    </div>
  )
}

function EvidenceRow({ item }: { item: ContextWikiEvidenceImportRun }) {
  return (
    <div className="rounded-md border border-border/60 bg-muted/15 px-3 py-2">
      <div className="flex items-start justify-between gap-2">
        <div className="text-sm font-medium text-foreground">{item.title}</div>
        <Badge className={statusBadgeClass(item.status)}>{statusLabel(item.status)}</Badge>
      </div>
      <div className="mt-1 text-xs text-muted-foreground">{item.summary}</div>
      <div className="mt-2 flex flex-wrap gap-1.5">
        <Badge variant="outline" className="text-[10px]">
          {sourceKindLabel(item.source_kind)}
        </Badge>
        {item.processor_model && (
          <Badge variant="secondary" className="text-[10px]">
            {item.processor_model}
          </Badge>
        )}
        <Badge variant="outline" className="text-[10px]">
          draft {shortTail(item.draft_path)}
        </Badge>
      </div>
      <div className="mt-2 text-[11px] text-muted-foreground">
        imported {formatRelativeTime(item.created_at)}
      </div>
    </div>
  )
}

function PromotionRow({ job }: { job: ContextWikiPromotionJob }) {
  return (
    <div className="rounded-md border border-border/60 bg-muted/15 px-3 py-2">
      <div className="flex items-start justify-between gap-2">
        <div className="text-sm font-medium text-foreground">{job.title}</div>
        <Badge className={statusBadgeClass(job.status)}>{statusLabel(job.status)}</Badge>
      </div>
      <div className="mt-2 flex flex-wrap gap-1.5">
        <Badge variant="outline" className="text-[10px]">
          {job.status === 'reviewed_merged' ? 'added to memory' : 'draft waiting'}
        </Badge>
        <Badge variant="outline" className="text-[10px]">
          {shortTail(job.draft_path)}
        </Badge>
        <Badge variant="secondary" className="text-[10px]">
          {job.note_type}
        </Badge>
      </div>
      <div className="mt-2 text-[11px] text-muted-foreground">
        {job.status === 'reviewed_merged' ? 'added' : 'updated'} {formatRelativeTime(job.created_at)}
      </div>
    </div>
  )
}

function InfoBlock({
  label,
  value,
  tooltip,
}: {
  label: string
  value: string
  tooltip?: string
}) {
  return (
    <div className="rounded-md border border-border/60 bg-muted/20 px-3 py-2">
      <div className="inline-flex items-center gap-1 text-[10px] uppercase tracking-[0.18em] text-muted-foreground">
        <span>{label}</span>
        {tooltip && <HelpTooltip content={tooltip} side="top" />}
      </div>
      <div className="mt-1 break-all text-[11px] text-foreground">{value}</div>
    </div>
  )
}

function statusBadgeClass(value: string) {
  switch (value) {
    case 'review_claimed':
    case 'claimed':
      return 'text-[10px] bg-amber-500/10 text-amber-500 border-amber-500/20'
    case 'review_prepared':
    case 'prepared':
    case 'ready':
      return 'text-[10px] bg-sky-500/10 text-sky-500 border-sky-500/20'
    case 'reviewed_merged':
    case 'merged':
      return 'text-[10px] bg-emerald-500/10 text-emerald-500 border-emerald-500/20'
    case 'drafted':
    case 'open':
      return 'text-[10px] bg-violet-500/10 text-violet-400 border-violet-500/20'
    case 'idle':
      return 'text-[10px]'
    default:
      return 'text-[10px]'
  }
}

function statusLabel(value: string) {
  switch (value) {
    case 'review_claimed':
    case 'claimed':
      return 'in review'
    case 'review_prepared':
    case 'prepared':
    case 'ready':
      return 'ready'
    case 'reviewed_merged':
    case 'merged':
      return 'added'
    case 'drafted':
      return 'drafted'
    case 'open':
      return 'new'
    default:
      return value || 'unknown'
  }
}

function proposalKindLabel(value: string) {
  switch (value) {
    case 'external_evidence_import':
      return 'source-based update'
    case 'methodology_draft':
      return 'workflow update'
    case 'retrieval_policy_patch':
      return 'retrieval rule'
    default:
      return value.replace(/_/g, ' ')
    }
}

function sourceKindLabel(value: string) {
  switch (value) {
    case 'transcript':
      return 'transcript'
    case 'text':
      return 'text note'
    default:
      return value || 'source'
  }
}

function shortTail(value: string) {
  const parts = value.split('/').filter(Boolean)
  if (parts.length <= 2) return value
  return `${parts[parts.length - 2]}/${parts[parts.length - 1]}`
}
