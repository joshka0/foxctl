import { useMemo } from 'react'
import { ContextWikiContextRail } from '@/components/v2/ContextWikiContextRail'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardHeader } from '@/components/ui/card'
import { CollapsibleSection } from '@/components/ui/collapsible-section'
import { ScrollArea } from '@/components/ui/scroll-area'
import { formatRelativeTime } from '@/lib/utils'
import { useActivityStore } from '@/stores/activityStore'
import { useActivityFocusStore } from '@/stores/activityFocusStore'
import { useViewStore } from '@/stores/viewStore'
import type { ActivityEvent } from '@/api/types'
import { Activity, ArrowRight, FileSearch, Layers, Workflow } from 'lucide-react'

type ActivityData = Record<string, unknown>
type SurfaceMode = 'turn' | 'context' | 'artifact'

type BuiltTraceStep = {
  id: string
  ts: string
  operation: string
  status: string
  summary: string[]
}

type BuiltTrace = {
  id: string
  label: string
  sessionID?: string
  traceIDs: string[]
  startTS: string
  endTS: string
  status: string
  eventCount: number
  iterations: number
  toolCalls: number
  refCount: number
  queryEvents: number
  totalHits: number
  workingContextApplied: number
  searchPaths: string[]
  steps: BuiltTraceStep[]
}

function toStringArray(value: unknown): string[] {
  if (!Array.isArray(value)) return []
  return value.filter((item): item is string => typeof item === 'string' && item.length > 0)
}

function eventRefData(event: ActivityEvent) {
  const data = event.data ?? {}
  return {
    refs: toStringArray(data.refs),
    turnRefs: toStringArray(data.turn_refs),
    sliceRefs: toStringArray(data.slice_refs),
    episodeRefs: toStringArray(data.episode_refs),
    narrativeRefs: toStringArray(data.narrative_refs),
    artifactRefs: toStringArray(data.artifact_refs),
    expandableRefs: toStringArray(data.expandable_refs),
  }
}

function eventData(event: ActivityEvent): ActivityData {
  return (event.data ?? {}) as ActivityData
}

function latestTimestamp(events: ActivityEvent[]): string | null {
  let latestMS = Number.NEGATIVE_INFINITY
  let latestTS: string | null = null
  for (const event of events) {
    const parsed = Date.parse(event.ts)
    if (!Number.isFinite(parsed)) continue
    if (parsed > latestMS) {
      latestMS = parsed
      latestTS = event.ts
    }
  }
  return latestTS
}

function activitySourceLabel(connected: boolean, initialLoaded: boolean): string {
  if (connected && initialLoaded) return 'source: stream + logs'
  if (connected) return 'source: stream'
  if (initialLoaded) return 'source: logs'
  return 'source: connecting'
}

function parseTS(ts: string): number {
  const parsed = Date.parse(ts)
  return Number.isFinite(parsed) ? parsed : 0
}

function shortID(value: string, n = 8): string {
  return value.length <= n ? value : value.slice(0, n)
}

function numberField(data: ActivityData, key: string): number {
  const value = data[key]
  if (typeof value !== 'number' || !Number.isFinite(value)) return 0
  return value
}

function stringField(data: ActivityData, key: string): string {
  const value = data[key]
  return typeof value === 'string' ? value : ''
}

function addUnique(values: string[], seen: Set<string>, value: string) {
  if (!value || seen.has(value)) return
  seen.add(value)
  values.push(value)
}

function splitPathTail(path: string): string {
  if (!path.includes('/')) return path
  const parts = path.split('/').filter(Boolean)
  return parts[parts.length - 1] ?? path
}

function totalRefCount(event: ActivityEvent): number {
  const refs = eventRefData(event)
  return (
    refs.refs.length +
    refs.turnRefs.length +
    refs.sliceRefs.length +
    refs.episodeRefs.length +
    refs.narrativeRefs.length +
    refs.artifactRefs.length +
    refs.expandableRefs.length
  )
}

function summarizeStep(event: ActivityEvent, mode: SurfaceMode): string[] {
  const data = eventData(event)
  const summary: string[] = []
  const refs = totalRefCount(event)

  if (event.command) summary.push(event.command)

  switch (mode) {
    case 'turn': {
      const iteration = numberField(data, 'iteration')
      const toolCalls = numberField(data, 'tool_calls')
      const totalTokens = numberField(data, 'total_tokens')
      const finishReason = stringField(data, 'finish_reason')
      if (iteration > 0) summary.push(`iter ${iteration}`)
      if (toolCalls > 0) summary.push(`${toolCalls} tools`)
      if (totalTokens > 0) summary.push(`${totalTokens} tok`)
      if (finishReason) summary.push(`finish ${finishReason}`)
      if (refs > 0) summary.push(`${refs} refs`)
      break
    }
    case 'context': {
      const queryLen = numberField(data, 'input_query_length')
      const scopeCount = numberField(data, 'input_scope_count')
      const hitCount = numberField(data, 'artifact_hit_count')
      const vectorCapability = stringField(data, 'artifact_vector_capability')
      const searchPath = stringField(data, 'artifact_search_path')
      const workspace = stringField(data, 'input_workspace')
      if (queryLen > 0) summary.push(`q ${queryLen} chars`)
      if (scopeCount > 0) summary.push(`${scopeCount} scopes`)
      if (data.working_context_applied === true) summary.push('working-context')
      if (searchPath) summary.push(splitPathTail(searchPath))
      if (!searchPath && workspace) summary.push(splitPathTail(workspace))
      if (hitCount > 0) summary.push(`${hitCount} hits`)
      if (vectorCapability) summary.push(`vector ${vectorCapability}`)
      if (refs > 0) summary.push(`${refs} refs`)
      break
    }
    case 'artifact': {
      const processed = numberField(data, 'processed')
      const hitCount = numberField(data, 'artifact_hit_count')
      const dims = numberField(data, 'dimensions')
      const model = stringField(data, 'model')
      const provider = stringField(data, 'provider')
      if (processed > 0) summary.push(`processed ${processed}`)
      if (hitCount > 0) summary.push(`${hitCount} hits`)
      if (dims > 0) summary.push(`${dims} dims`)
      if (provider) summary.push(provider)
      if (model) summary.push(model)
      if (refs > 0) summary.push(`${refs} refs`)
      break
    }
  }

  return summary.slice(0, 5)
}

function baseGroupKey(event: ActivityEvent): string {
  if (event.session_id) return `session:${event.session_id}`
  if (event.trace_id) return `trace:${event.trace_id}`
  if (event.agent_id) return `agent:${event.agent_id}`
  if (event.job_id) return `job:${event.job_id}`
  return `operation:${event.operation}`
}

function shouldSplitSegment(prev: ActivityEvent, next: ActivityEvent, currentSize: number): boolean {
  const maxGapMS = 2 * 60 * 1000
  const gap = parseTS(next.ts) - parseTS(prev.ts)
  if (gap > maxGapMS) return true
  if (currentSize >= 32) return true
  if (prev.trace_id && next.trace_id && prev.trace_id !== next.trace_id) return true
  if (prev.command !== next.command && gap > 15_000) return true
  return false
}

function buildTraceFromSegment(
  key: string,
  segment: ActivityEvent[],
  segmentIndex: number,
  mode: SurfaceMode,
): BuiltTrace {
  const traceIDs: string[] = []
  const seenTraceIDs = new Set<string>()
  const searchPaths: string[] = []
  const seenPaths = new Set<string>()
  let status = 'ok'
  let iterations = 0
  let toolCalls = 0
  let refCount = 0
  let queryEvents = 0
  let totalHits = 0
  let workingContextApplied = 0
  let sessionID = ''

  const steps: BuiltTraceStep[] = segment.map((event, idx) => {
    const data = eventData(event)
    if (!sessionID && event.session_id) sessionID = event.session_id
    if (event.trace_id) addUnique(traceIDs, seenTraceIDs, event.trace_id)
    if (event.status === 'error') status = 'error'
    const iteration = numberField(data, 'iteration')
    const totalIterations = numberField(data, 'iterations')
    if (iteration > iterations) iterations = iteration
    if (totalIterations > iterations) iterations = totalIterations
    toolCalls += numberField(data, 'tool_calls')
    refCount += totalRefCount(event)
    if (numberField(data, 'input_query_length') > 0) queryEvents++
    totalHits += numberField(data, 'artifact_hit_count')
    if (data.working_context_applied === true) workingContextApplied++
    addUnique(searchPaths, seenPaths, stringField(data, 'artifact_search_path'))
    addUnique(searchPaths, seenPaths, stringField(data, 'input_workspace'))
    return {
      id: `${event.ts}-${event.operation}-${idx}`,
      ts: event.ts,
      operation: event.operation,
      status: event.status,
      summary: summarizeStep(event, mode),
    }
  })

  const startTS = segment[0]?.ts ?? ''
  const endTS = segment[segment.length - 1]?.ts ?? startTS
  const label = sessionID
    ? `session ${shortID(sessionID)}`
    : traceIDs.length === 1
      ? `trace ${shortID(traceIDs[0])}`
      : key.replace(/^.*:/, '')

  return {
    id: `${key}:${segmentIndex}:${startTS}`,
    label,
    sessionID: sessionID || undefined,
    traceIDs,
    startTS,
    endTS,
    status,
    eventCount: segment.length,
    iterations,
    toolCalls,
    refCount,
    queryEvents,
    totalHits,
    workingContextApplied,
    searchPaths: searchPaths.slice(0, 8),
    steps,
  }
}

function buildPrebuiltTraces(events: ActivityEvent[], mode: SurfaceMode, limit = 12): BuiltTrace[] {
  if (events.length === 0) return []
  const groups = new Map<string, ActivityEvent[]>()

  for (const event of events) {
    const key = baseGroupKey(event)
    const group = groups.get(key)
    if (group) {
      group.push(event)
    } else {
      groups.set(key, [event])
    }
  }

  const traces: BuiltTrace[] = []
  for (const [key, group] of groups) {
    const sorted = [...group].sort((a, b) => parseTS(a.ts) - parseTS(b.ts))
    let segment: ActivityEvent[] = []
    let segmentIndex = 0
    for (const event of sorted) {
      if (segment.length === 0) {
        segment.push(event)
        continue
      }
      const prev = segment[segment.length - 1]
      if (shouldSplitSegment(prev, event, segment.length)) {
        traces.push(buildTraceFromSegment(key, segment, segmentIndex, mode))
        segmentIndex++
        segment = [event]
      } else {
        segment.push(event)
      }
    }
    if (segment.length > 0) {
      traces.push(buildTraceFromSegment(key, segment, segmentIndex, mode))
    }
  }

  return traces.sort((a, b) => parseTS(b.endTS) - parseTS(a.endTS)).slice(0, limit)
}

function EmptyState({
  title,
  description,
  ctaLabel,
  onCTA,
}: {
  title: string
  description: string
  ctaLabel?: string
  onCTA?: () => void
}) {
  return (
    <div className="text-center py-12 text-muted-foreground">
      <Activity className="h-8 w-8 mx-auto mb-2 opacity-50" />
      <p className="text-sm font-medium">{title}</p>
      <p className="text-xs mt-1">{description}</p>
      {ctaLabel && onCTA && (
        <Button
          variant="outline"
          size="sm"
          className="mt-3 text-[11px]"
          onClick={onCTA}
        >
          {ctaLabel}
        </Button>
      )}
    </div>
  )
}

function SurfaceStats({
  items,
}: {
  items: Array<{
    label: string
    value: number | string
  }>
}) {
  return (
    <div className="grid grid-cols-2 lg:grid-cols-4 gap-2">
      {items.map((item) => (
        <Card key={item.label}>
          <CardContent className="px-3 py-2">
            <div className="text-[10px] uppercase tracking-wider text-muted-foreground">
              {item.label}
            </div>
            <div className="text-sm font-semibold text-foreground">
              {item.value}
            </div>
          </CardContent>
        </Card>
      ))}
    </div>
  )
}

function RefBadges({ refs }: { refs: string[] }) {
  if (refs.length === 0) return null
  return (
    <div className="flex flex-wrap gap-1">
      {refs.slice(0, 6).map((ref) => (
        <Badge key={ref} variant="outline" className="text-[10px] font-mono">
          {ref}
        </Badge>
      ))}
      {refs.length > 6 && (
        <Badge variant="secondary" className="text-[10px]">
          +{refs.length - 6}
        </Badge>
      )}
    </div>
  )
}

function TraceFlowCard({
  trace,
  mode,
  onOpenEvents,
}: {
  trace: BuiltTrace
  mode: SurfaceMode
  onOpenEvents?: (trace: BuiltTrace) => void
}) {
  const recentSteps = trace.steps.slice(-6).reverse()
  const durationMS = Math.max(0, parseTS(trace.endTS) - parseTS(trace.startTS))
  const metricChips: string[] = []

  if (trace.iterations > 0) metricChips.push(`${trace.iterations} iterations`)
  if (trace.toolCalls > 0) metricChips.push(`${trace.toolCalls} tools`)
  if (mode !== 'turn' && trace.queryEvents > 0) metricChips.push(`${trace.queryEvents} queries`)
  if (mode !== 'turn' && trace.totalHits > 0) metricChips.push(`${trace.totalHits} hits`)
  if (mode === 'context' && trace.workingContextApplied > 0) {
    metricChips.push(`${trace.workingContextApplied} gated`)
  }
  if (trace.refCount > 0) metricChips.push(`${trace.refCount} refs`)

  return (
    <Card>
      <CardHeader className="py-3">
        <div className="flex items-center justify-between gap-2">
          <div className="text-sm font-medium">{trace.label}</div>
          <div className="flex items-center gap-2">
            {onOpenEvents && (
              <Button
                variant="outline"
                size="sm"
                className="h-7 text-[11px] px-2"
                onClick={(e) => {
                  e.stopPropagation()
                  onOpenEvents(trace)
                }}
              >
                Events
                <ArrowRight className="h-3 w-3 ml-1" />
              </Button>
            )}
            <Badge variant={trace.status === 'error' ? 'destructive' : 'secondary'}>
              {trace.status}
            </Badge>
          </div>
        </div>
        <div className="text-xs text-muted-foreground flex flex-wrap gap-x-3 gap-y-1">
          <span>{trace.eventCount} events</span>
          <span>{trace.steps.length} steps</span>
          <span>{formatRelativeTime(trace.endTS)}</span>
          {durationMS > 0 && <span>{durationMS}ms window</span>}
          {trace.sessionID && <span>session {shortID(trace.sessionID)}</span>}
          {trace.traceIDs.length > 0 && <span>{trace.traceIDs.length} trace ids</span>}
        </div>
      </CardHeader>
      <CardContent className="pt-0 pb-3 space-y-2">
        {metricChips.length > 0 && <RefBadges refs={metricChips} />}
        {trace.searchPaths.length > 0 && <RefBadges refs={trace.searchPaths} />}
        <div className="space-y-1">
          {recentSteps.map((step) => (
            <div key={step.id} className="rounded border border-border/60 px-2 py-1">
              <div className="flex items-center justify-between gap-2 text-xs">
                <span className="font-medium text-foreground">{step.operation}</span>
                <span className="text-muted-foreground">{formatRelativeTime(step.ts)}</span>
              </div>
              {step.summary.length > 0 && (
                <div className="text-[11px] text-muted-foreground mt-0.5">
                  {step.summary.join(' · ')}
                </div>
              )}
            </div>
          ))}
        </div>
      </CardContent>
    </Card>
  )
}

export function TurnsExplorer() {
  const setActiveView = useViewStore((s) => s.setActiveView)
  const setActivityFocus = useActivityFocusStore((s) => s.setFocus)
  const events = useActivityStore((s) => s.events)
  const connected = useActivityStore((s) => s.connected)
  const initialLoaded = useActivityStore((s) => s.initialLoaded)
  const turnEvents = useMemo(() => {
    return events.filter((event) => {
      const refs = eventRefData(event)
      const data = eventData(event)
      const isTurnOperation =
        event.operation === 'agent.iteration' ||
        event.operation === 'agent.complete' ||
        event.operation.startsWith('turn.')
      const hasTurnMetrics =
        typeof data.iteration === 'number' ||
        typeof data.message_count === 'number' ||
        typeof data.tool_calls === 'number'
      return (
        refs.turnRefs.length > 0 ||
        refs.sliceRefs.length > 0 ||
        refs.episodeRefs.length > 0 ||
        isTurnOperation ||
        hasTurnMetrics
      )
    })
  }, [events])
  const turnSource = activitySourceLabel(connected, initialLoaded)
  const turnLastUpdated = useMemo(() => latestTimestamp(turnEvents), [turnEvents])
  const turnTraces = useMemo(() => buildPrebuiltTraces(turnEvents, 'turn'), [turnEvents])
  const turnStats = useMemo(() => {
    let refLinkCount = 0
    let toolCallCount = 0
    let errorCount = 0
    for (const event of turnEvents) {
      const data = eventData(event)
      refLinkCount += totalRefCount(event)
      if (typeof data.tool_calls === 'number' && Number.isFinite(data.tool_calls)) {
        toolCallCount += data.tool_calls
      }
      if (event.status === 'error') errorCount++
    }
    return {
      refLinkCount,
      toolCallCount,
      errorCount,
    }
  }, [turnEvents])
  const recentTurnSignals = useMemo(() => {
    const traceSignals = turnTraces
      .flatMap((trace) => trace.steps.slice(-2).map((step) => `${step.operation}`))
      .slice(0, 12)
    if (traceSignals.length > 0) return traceSignals
    const fallback = [...turnEvents]
      .sort((a, b) => parseTS(b.ts) - parseTS(a.ts))
      .slice(0, 8)
      .map((event) => (event.command ? `${event.operation} · ${event.command}` : event.operation))
    return fallback
  }, [turnEvents, turnTraces])
  const openTurnTraceInEvents = (trace: BuiltTrace) => {
    setActivityFocus({
      traceIDs: trace.traceIDs,
      sessionID: trace.sessionID,
      sourceSurface: 'turns',
      label: trace.label,
    })
    setActiveView('events')
  }

  return (
    <div className="flex flex-col h-full">
      <div className="p-4 border-b border-border flex items-center justify-between">
        <div className="flex items-center gap-2">
          <Workflow className="h-5 w-5" />
          <h2 className="text-lg font-semibold text-foreground">Turns</h2>
        </div>
        <div className="flex items-center gap-2">
          <div className="text-[10px] text-muted-foreground text-right leading-tight">
            <div>{turnSource}</div>
            <div>
              {turnLastUpdated
                ? `updated ${formatRelativeTime(turnLastUpdated)}`
                : 'no updates yet'}
            </div>
          </div>
          <Badge variant="secondary">{turnEvents.length} turn events</Badge>
        </div>
      </div>
      <ScrollArea className="flex-1">
        <div className="p-4 space-y-3">
          <SurfaceStats
            items={[
              { label: 'turn events', value: turnEvents.length },
              { label: 'prebuilt traces', value: turnTraces.length },
              { label: 'tool calls', value: turnStats.toolCallCount },
              { label: 'ref links', value: turnStats.refLinkCount },
              { label: 'errors', value: turnStats.errorCount },
            ]}
          />
          <Card>
            <CardHeader className="py-3">
              <div className="text-sm font-medium">Recent turn signal</div>
            </CardHeader>
            <CardContent className="pt-0 pb-3">
              {recentTurnSignals.length > 0 ? (
                <RefBadges refs={recentTurnSignals} />
              ) : (
                <div className="text-xs text-muted-foreground">
                  No turn signals captured yet.
                </div>
              )}
            </CardContent>
          </Card>
          <div className="text-xs uppercase tracking-wider text-muted-foreground px-1">
            Prebuilt turn traces
          </div>
          {turnTraces.length === 0 ? (
            <EmptyState
              title="No turn traces yet"
              description="Run an agent turn to build trace flows from iteration and tool-call events."
              ctaLabel="Open Runtime"
              onCTA={() => setActiveView('runtime')}
            />
          ) : (
            turnTraces.map((trace) => (
              <TraceFlowCard
                key={trace.id}
                trace={trace}
                mode="turn"
                onOpenEvents={openTurnTraceInEvents}
              />
            ))
          )}
        </div>
      </ScrollArea>
    </div>
  )
}

export function ContextExplorer() {
  const setActiveView = useViewStore((s) => s.setActiveView)
  const selectedAgent = useViewStore((s) => s.selectedAgent)
  const setActivityFocus = useActivityFocusStore((s) => s.setFocus)
  const events = useActivityStore((s) => s.events)
  const connected = useActivityStore((s) => s.connected)
  const initialLoaded = useActivityStore((s) => s.initialLoaded)
  const workspaceRoot = selectedAgent?.workspace_root?.trim() ?? ''
  const contextEvents = useMemo(() => {
    return events.filter((event) => {
      const data = eventData(event)
      const refs = eventRefData(event)
      const operation = event.operation.toLowerCase()
      const command = (event.command ?? '').toLowerCase()
      const looksLikeContextCommand =
        command.includes('context') ||
        command.includes('semantic_search') ||
        command.includes('smart_search') ||
        command.includes('session/restore') ||
        command.includes('session/recall')
      const hasContextInputs =
        typeof data.input_scope_count === 'number' ||
        typeof data.input_query_length === 'number' ||
        typeof data.working_context_applied === 'boolean' ||
        typeof data.input_workspace === 'string'
      if (operation.includes('context')) return true
      if (refs.refs.length > 0 || refs.expandableRefs.length > 0) return true
      return Boolean(
        data.artifact_search_path ||
          data.working_context_applied ||
          looksLikeContextCommand ||
          hasContextInputs,
      )
    })
  }, [events])
  const contextSource = activitySourceLabel(connected, initialLoaded)
  const contextLastUpdated = useMemo(() => latestTimestamp(contextEvents), [contextEvents])
  const contextTraces = useMemo(() => buildPrebuiltTraces(contextEvents, 'context'), [contextEvents])
  const contextStats = useMemo(() => {
    let workingContextApplied = 0
    let queryEvents = 0
    let totalHits = 0
    let errorCount = 0
    const searchPaths: string[] = []
    const seenPaths = new Set<string>()
    for (const event of contextEvents) {
      const data = eventData(event)
      if (data.working_context_applied === true) {
        workingContextApplied++
      }
      if (typeof data.input_query_length === 'number' && data.input_query_length > 0) {
        queryEvents++
      }
      if (typeof data.artifact_search_path === 'string' && data.artifact_search_path.length > 0) {
        if (!seenPaths.has(data.artifact_search_path)) {
          seenPaths.add(data.artifact_search_path)
          searchPaths.push(data.artifact_search_path)
        }
      }
      if (typeof data.input_workspace === 'string' && data.input_workspace.length > 0) {
        if (!seenPaths.has(data.input_workspace)) {
          seenPaths.add(data.input_workspace)
          searchPaths.push(data.input_workspace)
        }
      }
      if (typeof data.artifact_hit_count === 'number' && Number.isFinite(data.artifact_hit_count)) {
        totalHits += data.artifact_hit_count
      }
      if (event.status === 'error') errorCount++
    }
    return {
      workingContextApplied,
      queryEvents,
      totalHits,
      errorCount,
      searchPaths: searchPaths.slice(0, 8),
    }
  }, [contextEvents])
  const contextTraceSignals = useMemo(() => {
    const out: string[] = []
    const seen = new Set<string>()
    for (const trace of contextTraces) {
      for (const path of trace.searchPaths) {
        if (seen.has(path)) continue
        seen.add(path)
        out.push(path)
        if (out.length >= 10) return out
      }
    }
    return out
  }, [contextTraces])
  const openContextTraceInEvents = (trace: BuiltTrace) => {
    setActivityFocus({
      traceIDs: trace.traceIDs,
      sessionID: trace.sessionID,
      sourceSurface: 'context',
      label: trace.label,
    })
    setActiveView('events')
  }

  return (
    <div className="flex flex-col h-full">
      <div className="p-4 border-b border-border flex items-center justify-between">
        <div className="flex items-center gap-2">
          <Layers className="h-5 w-5" />
          <div>
            <h2 className="text-lg font-semibold text-foreground">Project Memory</h2>
            <div className="text-xs text-muted-foreground">
              Review what should be remembered, then open diagnostics only when needed.
            </div>
          </div>
        </div>
        <div className="flex items-center gap-2">
          <div className="text-[10px] text-muted-foreground text-right leading-tight">
            <div>{contextSource}</div>
            <div>
              {contextLastUpdated
                ? `updated ${formatRelativeTime(contextLastUpdated)}`
                : 'no updates yet'}
            </div>
          </div>
          <Badge variant="outline">advanced diagnostics below</Badge>
        </div>
      </div>
      <ScrollArea className="flex-1">
        <div className="p-4 space-y-3">
          <ContextWikiContextRail selectedAgentWorkspaceRoot={workspaceRoot} />
          <Card>
            <CollapsibleSection
              title="Context Diagnostics"
              icon={<Layers className="h-3.5 w-3.5" />}
              defaultOpen={false}
              badge={`${contextEvents.length} events`}
            >
              <div className="text-xs text-muted-foreground">
                Use this section to debug retrieval and working-context behavior.
                Most users can ignore it unless something looks wrong.
              </div>
              <SurfaceStats
                items={[
                  { label: 'context events', value: contextEvents.length },
                  { label: 'prebuilt traces', value: contextTraces.length },
                  { label: 'working context', value: contextStats.workingContextApplied },
                  { label: 'query events', value: contextStats.queryEvents },
                  { label: 'errors', value: contextStats.errorCount },
                ]}
              />
              <Card>
                <CardHeader className="py-3">
                  <div className="text-sm font-medium">Observed search paths</div>
                </CardHeader>
                <CardContent className="pt-0 pb-3">
                  {contextTraceSignals.length > 0 ? (
                    <RefBadges refs={contextTraceSignals} />
                  ) : contextStats.searchPaths.length > 0 ? (
                    <RefBadges refs={contextStats.searchPaths} />
                  ) : (
                    <div className="text-xs text-muted-foreground">
                      No search paths observed yet.
                    </div>
                  )}
                  {contextStats.totalHits > 0 && (
                    <div className="text-xs text-muted-foreground mt-2">
                      Total artifact hits observed: {contextStats.totalHits}
                    </div>
                  )}
                </CardContent>
              </Card>
              <div className="text-xs uppercase tracking-wider text-muted-foreground px-1">
                Recent context traces
              </div>
              {contextTraces.length === 0 ? (
                <EmptyState
                  title="No context traces yet"
                  description="Run a context-heavy command (restore/search) to build trace flows for debugging."
                  ctaLabel="Open Companion"
                  onCTA={() => setActiveView('companion')}
                />
              ) : (
                contextTraces.map((trace) => (
                  <TraceFlowCard
                    key={trace.id}
                    trace={trace}
                    mode="context"
                    onOpenEvents={openContextTraceInEvents}
                  />
                ))
              )}
            </CollapsibleSection>
          </Card>
        </div>
      </ScrollArea>
    </div>
  )
}

export function ArtifactsExplorer() {
  const setActiveView = useViewStore((s) => s.setActiveView)
  const setActivityFocus = useActivityFocusStore((s) => s.setFocus)
  const events = useActivityStore((s) => s.events)
  const connected = useActivityStore((s) => s.connected)
  const initialLoaded = useActivityStore((s) => s.initialLoaded)
  const artifactEvents = useMemo(() => {
    return events.filter((event) => {
      const data = eventData(event)
      const refs = eventRefData(event)
      const operation = event.operation.toLowerCase()
      const command = (event.command ?? '').toLowerCase()
      const isArtifactOperation =
        operation.includes('artifact') ||
        operation.includes('embedding.generate') ||
        operation.includes('filesummary')
      const isArtifactCommand =
        command.includes('artifact') ||
        command.includes('session/anchor') ||
        command.includes('embedding')
      const hasArtifactSignals =
        typeof data.artifact_search_path === 'string' ||
        typeof data.artifact_hit_count === 'number' ||
        typeof data.processed === 'number' ||
        typeof data.dimensions === 'number'
      return (
        refs.artifactRefs.length > 0 ||
        isArtifactOperation ||
        isArtifactCommand ||
        hasArtifactSignals
      )
    })
  }, [events])
  const artifactSource = activitySourceLabel(connected, initialLoaded)
  const artifactLastUpdated = useMemo(() => latestTimestamp(artifactEvents), [artifactEvents])
  const artifactTraces = useMemo(() => buildPrebuiltTraces(artifactEvents, 'artifact'), [artifactEvents])
  const artifactStats = useMemo(() => {
    let artifactRefCount = 0
    let processedCount = 0
    let embeddingCalls = 0
    let searchErrors = 0
    const searchPaths: string[] = []
    const seenPaths = new Set<string>()
    for (const event of artifactEvents) {
      const refs = eventRefData(event)
      const data = eventData(event)
      artifactRefCount += refs.artifactRefs.length
      if (event.operation.toLowerCase() === 'embedding.generate') {
        embeddingCalls++
      }
      if (typeof data.processed === 'number' && Number.isFinite(data.processed)) {
        processedCount += data.processed
      } else if (typeof data.artifact_hit_count === 'number' && Number.isFinite(data.artifact_hit_count)) {
        processedCount += data.artifact_hit_count
      }
      if (
        (typeof data.artifact_search_error === 'string' && data.artifact_search_error.length > 0) ||
        event.status === 'error'
      ) {
        searchErrors++
      }
      if (typeof data.artifact_search_path === 'string' && data.artifact_search_path.length > 0) {
        if (!seenPaths.has(data.artifact_search_path)) {
          seenPaths.add(data.artifact_search_path)
          searchPaths.push(data.artifact_search_path)
        }
      }
    }
    return {
      artifactRefCount,
      processedCount,
      embeddingCalls,
      searchErrors,
      searchPaths: searchPaths.slice(0, 8),
    }
  }, [artifactEvents])
  const artifactTraceSignals = useMemo(() => {
    const out: string[] = []
    const seen = new Set<string>()
    for (const trace of artifactTraces) {
      for (const path of trace.searchPaths) {
        if (seen.has(path)) continue
        seen.add(path)
        out.push(path)
        if (out.length >= 10) return out
      }
    }
    return out
  }, [artifactTraces])
  const openArtifactTraceInEvents = (trace: BuiltTrace) => {
    setActivityFocus({
      traceIDs: trace.traceIDs,
      sessionID: trace.sessionID,
      sourceSurface: 'artifacts',
      label: trace.label,
    })
    setActiveView('events')
  }

  return (
    <div className="flex flex-col h-full">
      <div className="p-4 border-b border-border flex items-center justify-between">
        <div className="flex items-center gap-2">
          <FileSearch className="h-5 w-5" />
          <h2 className="text-lg font-semibold text-foreground">Artifacts</h2>
        </div>
        <div className="flex items-center gap-2">
          <div className="text-[10px] text-muted-foreground text-right leading-tight">
            <div>{artifactSource}</div>
            <div>
              {artifactLastUpdated
                ? `updated ${formatRelativeTime(artifactLastUpdated)}`
                : 'no updates yet'}
            </div>
          </div>
          <Badge variant="secondary">{artifactEvents.length} retrieval events</Badge>
        </div>
      </div>
      <ScrollArea className="flex-1">
        <div className="p-4 space-y-3">
          <SurfaceStats
            items={[
              { label: 'retrieval events', value: artifactEvents.length },
              { label: 'prebuilt traces', value: artifactTraces.length },
              { label: 'artifact refs', value: artifactStats.artifactRefCount },
              { label: 'embeddings', value: artifactStats.embeddingCalls },
              { label: 'processed', value: artifactStats.processedCount },
              { label: 'errors', value: artifactStats.searchErrors },
            ]}
          />
          <Card>
            <CardHeader className="py-3">
              <div className="text-sm font-medium">Artifact search paths</div>
            </CardHeader>
            <CardContent className="pt-0 pb-3">
              {artifactTraceSignals.length > 0 ? (
                <RefBadges refs={artifactTraceSignals} />
              ) : artifactStats.searchPaths.length > 0 ? (
                <RefBadges refs={artifactStats.searchPaths} />
              ) : (
                <div className="text-xs text-muted-foreground">
                  No artifact search paths observed yet.
                </div>
              )}
            </CardContent>
          </Card>
          <div className="text-xs uppercase tracking-wider text-muted-foreground px-1">
            Prebuilt artifact traces
          </div>
          {artifactTraces.length === 0 ? (
            <EmptyState
              title="No artifact traces yet"
              description="Run embedding or retrieval tasks to build artifact trace flows."
              ctaLabel="Open Runtime"
              onCTA={() => setActiveView('runtime')}
            />
          ) : (
            artifactTraces.map((trace) => (
              <TraceFlowCard
                key={trace.id}
                trace={trace}
                mode="artifact"
                onOpenEvents={openArtifactTraceInEvents}
              />
            ))
          )}
        </div>
      </ScrollArea>
    </div>
  )
}
