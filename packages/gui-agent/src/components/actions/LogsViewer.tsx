import { useState, useMemo, useEffect, useCallback } from 'react'
import type { MouseEvent } from 'react'
import { ScrollArea } from '@/components/ui/scroll-area'
import { Button } from '@/components/ui/button'
import { Badge } from '@/components/ui/badge'
import { Input } from '@/components/ui/input'
import { Checkbox } from '@/components/ui/checkbox'
import { HelpTooltip, Tooltip } from '@/components/ui/tooltip'
import { cn, formatRelativeTime } from '@/lib/utils'
import { useActivityStore } from '@/stores/activityStore'
import { useActivityFocusStore } from '@/stores/activityFocusStore'
import { useEventProjectionStore } from '@/stores/eventProjectionStore'
import { useViewStore } from '@/stores/viewStore'
import { EventTraceDrawer } from '@/components/v2/EventTraceDrawer'
import { RefDrilldownPanel } from '@/components/v2/RefDrilldownPanel'
import { getLogs } from '@/api/client'
import type { ActivityEvent } from '@/api/types'
import {
  RefreshCw,
  AlertCircle,
  Clock,
  Filter,
  Search,
  Copy,
  Bot,
  Webhook,
  Zap,
  Server,
  Loader2,
  ChevronDown,
  ChevronUp,
  CornerUpLeft,
  ArrowRight,
  X,
} from 'lucide-react'

function formatSurfaceLabel(
  surface?:
    | 'runtime'
    | 'orchestration'
    | 'turns'
    | 'context'
    | 'artifacts'
    | 'companion'
    | 'events',
): string {
  if (!surface) return 'Surface'
  return surface.charAt(0).toUpperCase() + surface.slice(1)
}

// Convert ActivityEvent to LogEntry-like format
interface LogEntry {
  ts: string
  operation: string
  command?: string // Skill/hook name (e.g., "code/semantic_search")
  status: string
  component?: string
  caller?: string
  caller_file?: string
  caller_path?: string
  caller_line?: number
  caller_func?: string
  trace_id?: string
  span_id?: string
  parent_id?: string
  service?: string
  version?: string
  subtype?: string
  session_id?: string
  agent_id?: string
  workspace_id?: string
  job_id?: string
  duration_ms?: number
  error_type?: string
  error_code?: string
  error_message?: string
  retriable?: boolean
  data?: Record<string, unknown>
  raw?: Record<string, unknown>
}

type SurfaceTarget = 'runtime' | 'turns' | 'context' | 'artifacts' | 'companion'

type TraceSummary = {
  traceID: string
  sessionID?: string
  count: number
  status: 'ok' | 'error'
  lastTS: string
  lastOperation: string
}

function toStringArray(value: unknown): string[] {
  if (!Array.isArray(value)) return []
  return value.filter((item): item is string => typeof item === 'string' && item.length > 0)
}

function inferSurfaceTarget(log: LogEntry): SurfaceTarget | null {
  const data = (log.data ?? {}) as Record<string, unknown>
  const turnRefs = toStringArray(data.turn_refs)
  const contextRefs = [
    ...toStringArray(data.slice_refs),
    ...toStringArray(data.episode_refs),
    ...toStringArray(data.narrative_refs),
    ...toStringArray(data.refs),
    ...toStringArray(data.expandable_refs),
  ]
  const artifactRefs = toStringArray(data.artifact_refs)

  if (turnRefs.length > 0) return 'turns'
  if (artifactRefs.length > 0) return 'artifacts'
  if (contextRefs.length > 0) return 'context'
  return null
}

/**
 * Convert an ActivityEvent into a normalized LogEntry for UI rendering.
 *
 * The result preserves key metadata from the event and normalizes derived fields:
 * - `component` is taken from `event.component` when present, otherwise taken from the first segment of `event.operation`.
 * - caller-related fields (`caller`, `caller_file`, `caller_path`, `caller_func`) are extracted from `event.data` when they are strings.
 * - `caller_line` is normalized to a number if it is a numeric value or numeric string.
 *
 * @param event - The source ActivityEvent to convert.
 * @returns A LogEntry that consolidates event fields, extracted caller information, normalized caller line, and the original raw event under `raw`.
 */
function activityToLog(event: ActivityEvent): LogEntry {
  // Extract component from operation (e.g., "hook.execute" -> "hook")
  const component = (event as { component?: string }).component ?? event.operation.split('.')[0]
  const data = event.data as Record<string, unknown> | undefined
  const caller = typeof data?.caller === 'string' ? data.caller : undefined
  const callerFile = typeof data?.caller_file === 'string' ? data.caller_file : undefined
  const callerPath = typeof data?.caller_path === 'string' ? data.caller_path : undefined
  const callerFunc = typeof data?.caller_func === 'string' ? data.caller_func : undefined
  const callerLine = (() => {
    const raw = data?.caller_line
    if (typeof raw === 'number') return raw
    if (typeof raw === 'string') {
      const parsed = Number(raw)
      return Number.isFinite(parsed) ? parsed : undefined
    }
    return undefined
  })()

  return {
    ts: event.ts,
    operation: event.operation,
    command: event.command,
    status: event.status,
    component,
    caller,
    caller_file: callerFile,
    caller_path: callerPath,
    caller_line: callerLine,
    caller_func: callerFunc,
    trace_id: event.trace_id,
    span_id: event.span_id,
    parent_id: event.parent_id,
    service: event.service,
    version: event.version,
    subtype: event.subtype,
    session_id: event.session_id,
    agent_id: event.agent_id,
    workspace_id: event.workspace_id,
    job_id: event.job_id,
    duration_ms: event.duration_ms,
    error_type: event.error_type,
    error_code: event.error_code,
    error_message: event.error_message ?? (event.data?.error as string | undefined),
    retriable: event.retriable,
    data: event.data,
    raw: event as unknown as Record<string, unknown>,
  }
}

/**
 * Render an interactive activity logs viewer with filtering, search, and live connection state.
 *
 * Displays activity events from the shared activity store (including API-loaded and SSE events),
 * fetches initial logs on mount, and provides controls to refresh or clear events. Built-in filters
 * include per-command visibility (with a collapsible hide panel and default hidden set), errors-only,
 * component and workspace selectors, and a free-text search that matches operation, command, error
 * message, or payload data. Each entry is presented as a compact card that can expand to show error
 * details and the raw payload (with copy-to-clipboard).
 *
 * @returns A JSX element containing the activity logs UI.
 */
export function LogsViewer() {
  const [searchInput, setSearchInput] = useState('')
  const [showFilterPanel, setShowFilterPanel] = useState(false)
  const [loading, setLoading] = useState(false)
  const [selectedTraceID, setSelectedTraceID] = useState<string | null>(null)
  const [selectedLog, setSelectedLog] = useState<LogEntry | null>(null)

  // Use activity store events - includes both API-loaded and SSE events
  const events = useActivityStore((s) => s.events)
  const connected = useActivityStore((s) => s.connected)
  const clearEvents = useActivityStore((s) => s.clearEvents)
  const setEvents = useActivityStore((s) => s.setEvents)
  const initialLoaded = useActivityStore((s) => s.initialLoaded)
  const focus = useActivityFocusStore((s) => s.focus)
  const setActivityFocus = useActivityFocusStore((s) => s.setFocus)
  const clearFocus = useActivityFocusStore((s) => s.clearFocus)
  const setActiveView = useViewStore((s) => s.setActiveView)
  const {
    errorsOnly,
    hiddenCommands,
    componentFilter,
    workspaceFilter,
    searchQuery,
    showRawEvents,
    setErrorsOnly,
    toggleHiddenCommand,
    setComponentFilter,
    setWorkspaceFilter,
    setSearchQuery,
    setShowRawEvents,
    clearHiddenCommands,
    resetToDefaults,
  } = useEventProjectionStore()
  const hiddenCommandSet = useMemo(() => new Set(hiddenCommands), [hiddenCommands])

  useEffect(() => {
    setSearchInput(searchQuery)
  }, [searchQuery])

  useEffect(() => {
    const timer = setTimeout(() => setSearchQuery(searchInput), 300)
    return () => clearTimeout(timer)
  }, [searchInput, setSearchQuery])

  useEffect(() => {
    if (!focus) return
    const onKeyDown = (event: KeyboardEvent) => {
      if (event.key !== 'Escape') return
      const target = event.target as HTMLElement | null
      if (target) {
        const tag = target.tagName?.toLowerCase()
        const isEditable =
          target.isContentEditable ||
          tag === 'input' ||
          tag === 'textarea' ||
          tag === 'select'
        if (isEditable) return
      }
      clearFocus()
    }
    window.addEventListener('keydown', onKeyDown)
    return () => window.removeEventListener('keydown', onKeyDown)
  }, [focus, clearFocus])

  // Fetch initial logs from API
  const fetchLogs = useCallback(async () => {
    setLoading(true)
    try {
      const response = await getLogs({ limit: 500 })
      // Preserve all log metadata from the API.
      const activityEvents = response.entries.map((entry) => ({ ...entry })) as ActivityEvent[]
      setEvents(activityEvents)
    } catch (err) {
      console.error(JSON.stringify({
        message: 'Failed to fetch logs',
        error: err instanceof Error ? err.message : String(err),
      }))
    } finally {
      setLoading(false)
    }
  }, [setEvents])

  // Load initial logs on mount
  useEffect(() => {
    if (!initialLoaded) {
      fetchLogs()
    }
  }, [initialLoaded, fetchLogs])

  // Extract unique commands from all events
  const uniqueCommands = useMemo(() => {
    const commands = new Set<string>()
    events.forEach((event) => {
      if (event.command) {
        commands.add(event.command)
      }
    })
    // Sort alphabetically
    return Array.from(commands).sort()
  }, [events])

  const activityLogs = useMemo(() => events.map(activityToLog), [events])

  // Extract unique components from all events
  const uniqueComponents = useMemo(() => {
    const components = new Set<string>()
    activityLogs.forEach((log) => {
      const component = log.component
      if (component) {
        components.add(component)
      }
    })
    return Array.from(components).sort()
  }, [activityLogs])

  // Extract unique workspaces from all events
  const uniqueWorkspaces = useMemo(() => {
    const workspaces = new Set<string>()
    events.forEach((event) => {
      if (event.workspace_id) {
        workspaces.add(event.workspace_id)
      }
    })
    // Sort alphabetically
    return Array.from(workspaces).sort()
  }, [events])

  // Get display name for workspace (last path component)
  const getWorkspaceDisplayName = (workspace: string) => {
    const parts = workspace.split('/')
    return parts[parts.length - 1] || workspace
  }

  // Convert and filter events
  const logsAfterPrimaryFilters = useMemo(() => {
    let logs = activityLogs

    // Focused trace/session filter from Turns/Context explorers
    const hasFocusSelectors = Boolean(
      focus && (focus.traceIDs.length > 0 || focus.sessionID),
    )
    if (hasFocusSelectors && focus) {
      const traceSet = new Set(focus.traceIDs)
      logs = logs.filter((log) => {
        const traceMatch = Boolean(log.trace_id && traceSet.has(log.trace_id))
        const sessionMatch = Boolean(focus.sessionID && log.session_id === focus.sessionID)
        return traceMatch || sessionMatch
      })
    }

    // Filter out hidden commands
    if (hiddenCommandSet.size > 0) {
      logs = logs.filter((log) => !hiddenCommandSet.has(log.command || ''))
    }

    // Filter by component
    if (componentFilter) {
      logs = logs.filter((log) => log.component === componentFilter)
    }

    // Filter by workspace
    if (workspaceFilter) {
      logs = logs.filter((log) => log.workspace_id === workspaceFilter)
    }

    // Filter by search (include command in search)
    if (searchQuery) {
      const query = searchQuery.toLowerCase()
      logs = logs.filter(
        (log) =>
          log.operation.toLowerCase().includes(query) ||
          log.command?.toLowerCase().includes(query) ||
          log.error_message?.toLowerCase().includes(query) ||
          JSON.stringify(log.data).toLowerCase().includes(query)
      )
    }

    return logs
  }, [activityLogs, focus, hiddenCommandSet, componentFilter, workspaceFilter, searchQuery])

  const filteredLogs = useMemo(() => {
    if (!errorsOnly) return logsAfterPrimaryFilters
    return logsAfterPrimaryFilters.filter((log) => log.status === 'error')
  }, [logsAfterPrimaryFilters, errorsOnly])

  const summarySourceLogs = useMemo(
    () =>
      [...filteredLogs].sort(
        (a, b) => Date.parse(b.ts) - Date.parse(a.ts),
      ),
    [filteredLogs],
  )
  const recentErrors = useMemo(
    () => summarySourceLogs.filter((log) => log.status === 'error').slice(0, 5),
    [summarySourceLogs],
  )
  const slowOperations = useMemo(
    () =>
      summarySourceLogs
        .filter(
          (log) =>
            typeof log.duration_ms === 'number' &&
            Number.isFinite(log.duration_ms) &&
            log.duration_ms >= 1000,
        )
        .sort((a, b) => (b.duration_ms ?? 0) - (a.duration_ms ?? 0))
        .slice(0, 5),
    [summarySourceLogs],
  )
  const activeTraces = useMemo(() => {
    const traces = new Map<string, TraceSummary>()
    for (const log of summarySourceLogs) {
      if (!log.trace_id) continue
      const existing = traces.get(log.trace_id)
      if (!existing) {
        traces.set(log.trace_id, {
          traceID: log.trace_id,
          sessionID: log.session_id,
          count: 1,
          status: log.status === 'error' ? 'error' : 'ok',
          lastTS: log.ts,
          lastOperation: log.operation,
        })
        continue
      }
      const existingTS = Date.parse(existing.lastTS)
      const nextTS = Date.parse(log.ts)
      existing.count += 1
      if (log.status === 'error') existing.status = 'error'
      if (Number.isFinite(nextTS) && nextTS > existingTS) {
        existing.lastTS = log.ts
        existing.lastOperation = log.operation
        if (log.session_id) existing.sessionID = log.session_id
      }
    }
    return Array.from(traces.values())
      .sort((a, b) => Date.parse(b.lastTS) - Date.parse(a.lastTS))
      .slice(0, 5)
  }, [summarySourceLogs])

  const selectLog = useCallback((log: LogEntry) => {
    setSelectedLog(log)
    setSelectedTraceID(log.trace_id ?? null)
  }, [])

  const selectTrace = useCallback((traceID: string) => {
    setSelectedTraceID(traceID)
    setSelectedLog((prev) => (prev?.trace_id === traceID ? prev : null))
  }, [])

  const openLogFocus = useCallback((log: LogEntry, label?: string) => {
    if (!log.trace_id && !log.session_id) {
      return
    }
    setActivityFocus({
      traceIDs: log.trace_id ? [log.trace_id] : [],
      sessionID: log.session_id,
      sourceSurface: 'events',
      label: label ?? formatOperation(log.operation, log.command),
    })
  }, [setActivityFocus])

  const navigateFromLog = useCallback(
    (log: LogEntry, target: SurfaceTarget) => {
      selectLog(log)
      openLogFocus(log)
      setActiveView(target)
    },
    [openLogFocus, selectLog, setActiveView],
  )

  return (
    <div className="flex flex-col h-full">
      {/* Header */}
      <div className="p-4 border-b border-border space-y-3">
        <div className="flex items-center justify-between">
          <div className="flex items-center gap-2">
            <h2 className="text-lg font-semibold text-foreground">
              Activity Logs
            </h2>
            <div className="flex items-center gap-1">
              <span
                className={cn(
                  'h-2 w-2 rounded-full',
                  connected ? 'bg-green-500' : 'bg-red-500'
                )}
              />
              <span className="text-xs text-muted-foreground">
                {connected ? 'Live' : 'Disconnected'}
              </span>
            </div>
          </div>
          <div className="flex items-center gap-2">
            <Badge variant="secondary">{filteredLogs.length} events</Badge>
            <Button
              variant="ghost"
              size="sm"
              onClick={fetchLogs}
              disabled={loading}
              className="h-8 text-xs"
            >
              {loading ? (
                <Loader2 className="h-3 w-3 animate-spin" />
              ) : (
                <RefreshCw className="h-3 w-3" />
              )}
            </Button>
            <Button
              variant="ghost"
              size="sm"
              onClick={clearEvents}
              className="h-8 text-xs"
            >
              Clear
            </Button>
          </div>
        </div>

        {focus && (
          <div className="flex items-center justify-between gap-2 rounded-md border border-border bg-muted/40 px-3 py-2">
            <div className="text-xs text-muted-foreground flex flex-wrap gap-x-3 gap-y-1">
              <span>
                Focused from {focus.sourceSurface ?? 'surface'}
                {focus.label ? ` · ${focus.label}` : ''}
              </span>
              {focus.sessionID && <span>session {focus.sessionID.slice(0, 8)}</span>}
              {focus.traceIDs.length > 0 && <span>{focus.traceIDs.length} trace ids</span>}
              <span>Esc to clear</span>
            </div>
            <div className="flex items-center gap-2">
              {focus.sourceSurface && (
                <Button
                  variant="outline"
                  size="sm"
                  className="h-7 text-[11px] px-2"
                  onClick={() => setActiveView(focus.sourceSurface!)}
                >
                  <CornerUpLeft className="h-3 w-3 mr-1" />
                  Back to {formatSurfaceLabel(focus.sourceSurface)}
                </Button>
              )}
              <Tooltip content="Clear the current event focus and return to the full stream. Shortcut: Esc.">
                <Button
                  variant="ghost"
                  size="sm"
                  className="h-7 text-[11px] px-2"
                  onClick={clearFocus}
                >
                  <X className="h-3 w-3 mr-1" />
                  Clear focus (Esc)
                </Button>
              </Tooltip>
            </div>
          </div>
        )}

        {/* Filters */}
        <div className="flex items-center gap-2">
          <div className="relative flex-1">
            <Search className="absolute left-2.5 top-2.5 h-4 w-4 text-muted-foreground" />
            <Input
              placeholder="Search operations..."
              value={searchInput}
              onChange={(e) => setSearchInput(e.target.value)}
              className="pl-9 h-9"
            />
          </div>
          <Tooltip content="Hide noisy command names from the event list so you can focus on what matters.">
            <Button
              variant={hiddenCommands.length > 0 ? 'default' : 'outline'}
              size="sm"
              onClick={() => setShowFilterPanel(!showFilterPanel)}
              className="h-9"
            >
              <Filter className="h-4 w-4 mr-1" />
              {hiddenCommands.length > 0 ? `${hiddenCommands.length} hidden` : 'Filter'}
              {showFilterPanel ? (
                <ChevronUp className="h-3 w-3 ml-1" />
              ) : (
                <ChevronDown className="h-3 w-3 ml-1" />
              )}
            </Button>
          </Tooltip>
          <Tooltip content="Show only error events in the current stream.">
            <Button
              variant={errorsOnly ? 'destructive' : 'outline'}
              size="sm"
              onClick={() => setErrorsOnly(!errorsOnly)}
              className="h-9"
            >
              <AlertCircle className="h-4 w-4 mr-1" />
              Errors
            </Button>
          </Tooltip>
          <select
            value={componentFilter}
            onChange={(e) => setComponentFilter(e.target.value)}
            className="h-9 rounded-md border border-input bg-background px-3 text-sm"
          >
            <option value="">All Components</option>
            {uniqueComponents.map((component) => (
              <option key={component} value={component}>
                {component}
              </option>
            ))}
          </select>
          {uniqueWorkspaces.length > 1 && (
            <Tooltip content={workspaceFilter || 'All workspaces'}>
              <select
                value={workspaceFilter}
                onChange={(e) => setWorkspaceFilter(e.target.value)}
                className="h-9 rounded-md border border-input bg-background px-3 text-sm max-w-[200px]"
              >
                <option value="">All Workspaces</option>
                {uniqueWorkspaces.map((ws) => (
                  <option key={ws} value={ws}>
                    {getWorkspaceDisplayName(ws)}
                  </option>
                ))}
              </select>
            </Tooltip>
          )}
        </div>

        {/* Command Filter Panel */}
        {showFilterPanel && (
          <div className="border border-border rounded-lg p-3 bg-muted/30">
            <div className="flex items-center justify-between mb-2">
              <div className="inline-flex items-center gap-1.5">
                <span className="text-sm font-medium">Hide Commands</span>
                <HelpTooltip
                  side="top"
                  content="Disable specific command names below to reduce noise in the event stream."
                />
              </div>
              <div className="flex items-center gap-2">
                <Button
                  variant="ghost"
                  size="sm"
                  onClick={resetToDefaults}
                  className="h-6 text-xs px-2"
                >
                  Reset
                </Button>
                <Button
                  variant="ghost"
                  size="sm"
                  onClick={clearHiddenCommands}
                  className="h-6 text-xs px-2"
                >
                  Clear All
                </Button>
              </div>
            </div>
            <div className="flex flex-wrap gap-2">
              {uniqueCommands.length === 0 ? (
                <span className="text-xs text-muted-foreground">
                  No commands found
                </span>
              ) : (
                uniqueCommands.map((command) => (
                  <label
                    key={command}
                    className={cn(
                      'flex items-center gap-1.5 px-2 py-1 rounded-md text-xs cursor-pointer transition-colors',
                      hiddenCommands.includes(command)
                        ? 'bg-destructive/20 text-destructive-foreground'
                        : 'bg-muted hover:bg-muted/80'
                    )}
                  >
                    <Checkbox
                      checked={hiddenCommands.includes(command)}
                      onCheckedChange={() => toggleHiddenCommand(command)}
                    />
                    <span className="truncate max-w-[150px]">{command}</span>
                    {hiddenCommands.includes(command) && (
                      <X className="h-3 w-3 opacity-60" />
                    )}
                  </label>
                ))
              )}
            </div>
          </div>
        )}
      </div>

      {/* Logs List */}
      <ScrollArea className="flex-1">
        <div className="p-4 space-y-3">
          <div className="grid grid-cols-1 lg:grid-cols-3 gap-3">
            <div className="rounded-md border border-border bg-card px-3 py-2">
              <div className="text-[11px] uppercase tracking-wide text-muted-foreground mb-2">
                Recent errors
              </div>
              {recentErrors.length === 0 ? (
                <div className="text-xs text-muted-foreground">No recent errors.</div>
              ) : (
                <div className="space-y-1.5">
                  {recentErrors.map((log) => (
                    <button
                      key={`error-${log.ts}-${log.operation}`}
                      type="button"
                      className="w-full text-left rounded border border-red-500/20 bg-red-500/5 px-2 py-1.5 hover:bg-red-500/10"
                      onClick={() => {
                        selectLog(log)
                        openLogFocus(log)
                        if (showRawEvents === false) setShowRawEvents(true)
                      }}
                    >
                      <div className="text-[11px] font-medium text-red-300 truncate">
                        {formatOperation(log.operation, log.command)}
                      </div>
                      <div className="text-[10px] text-red-200/80 flex items-center justify-between">
                        <span>{log.component || 'event'}</span>
                        <span>{formatRelativeTime(log.ts)}</span>
                      </div>
                    </button>
                  ))}
                </div>
              )}
            </div>
            <div className="rounded-md border border-border bg-card px-3 py-2">
              <div className="text-[11px] uppercase tracking-wide text-muted-foreground mb-2">
                High latency
              </div>
              {slowOperations.length === 0 ? (
                <div className="text-xs text-muted-foreground">No slow operations in current filters.</div>
              ) : (
                <div className="space-y-1.5">
                  {slowOperations.map((log) => (
                    <button
                      key={`slow-${log.ts}-${log.operation}`}
                      type="button"
                      className="w-full text-left rounded border border-border/70 bg-muted/30 px-2 py-1.5 hover:bg-muted/50"
                      onClick={() => {
                        selectLog(log)
                        openLogFocus(log)
                        if (showRawEvents === false) setShowRawEvents(true)
                      }}
                    >
                      <div className="text-[11px] font-medium text-foreground truncate">
                        {formatOperation(log.operation, log.command)}
                      </div>
                      <div className="text-[10px] text-muted-foreground flex items-center justify-between">
                        <span>{log.duration_ms ?? 0}ms</span>
                        <span>{formatRelativeTime(log.ts)}</span>
                      </div>
                    </button>
                  ))}
                </div>
              )}
            </div>
            <div className="rounded-md border border-border bg-card px-3 py-2">
              <div className="text-[11px] uppercase tracking-wide text-muted-foreground mb-2">
                Active traces
              </div>
              {activeTraces.length === 0 ? (
                <div className="text-xs text-muted-foreground">No trace ids in current filters.</div>
              ) : (
                <div className="space-y-1.5">
                  {activeTraces.map((trace) => (
                    <button
                      key={trace.traceID}
                      type="button"
                      className="w-full text-left rounded border border-border/70 bg-muted/30 px-2 py-1.5 hover:bg-muted/50"
                      onClick={() => {
                        setActivityFocus({
                          traceIDs: [trace.traceID],
                          sessionID: trace.sessionID,
                          sourceSurface: 'events',
                          label: `trace ${trace.traceID.slice(0, 8)}`,
                        })
                        selectTrace(trace.traceID)
                        if (showRawEvents === false) setShowRawEvents(true)
                      }}
                    >
                      <div className="text-[11px] font-medium text-foreground flex items-center justify-between gap-2">
                        <span className="font-mono truncate">{trace.traceID.slice(0, 12)}</span>
                        <Badge
                          variant={trace.status === 'error' ? 'destructive' : 'secondary'}
                          className="text-[10px]"
                        >
                          {trace.count}
                        </Badge>
                      </div>
                      <div className="text-[10px] text-muted-foreground flex items-center justify-between">
                        <span className="truncate">{trace.lastOperation}</span>
                        <span>{formatRelativeTime(trace.lastTS)}</span>
                      </div>
                    </button>
                  ))}
                </div>
              )}
            </div>
          </div>

          <div className="flex items-center justify-between rounded-md border border-border bg-muted/30 px-3 py-2">
            <div className="text-xs text-muted-foreground">
              Summary-first mode: open raw event rows only when needed.
            </div>
            <Button
              variant="outline"
              size="sm"
              className="h-7 text-[11px]"
              onClick={() => setShowRawEvents(!showRawEvents)}
            >
              {showRawEvents ? 'Hide raw events' : `Show raw events (${filteredLogs.length})`}
              <ArrowRight className={cn('h-3 w-3 ml-1 transition-transform', showRawEvents && 'rotate-90')} />
            </Button>
          </div>

          {(selectedTraceID || selectedLog) && (
            <div className="grid grid-cols-1 xl:grid-cols-2 gap-3">
              <EventTraceDrawer
                traceID={selectedTraceID}
                events={filteredLogs}
                onClose={() => setSelectedTraceID(null)}
              />
              {selectedLog && (
                <RefDrilldownPanel
                  label={formatOperation(selectedLog.operation, selectedLog.command)}
                  data={selectedLog.data}
                  canNavigate={Boolean(selectedLog.trace_id || selectedLog.session_id)}
                  onNavigate={(target) => navigateFromLog(selectedLog, target)}
                />
              )}
            </div>
          )}

          {loading ? (
            <div className="text-center py-12 text-muted-foreground">
              <Loader2 className="h-8 w-8 mx-auto mb-2 animate-spin opacity-50" />
              <p>Loading activity logs...</p>
            </div>
          ) : filteredLogs.length === 0 ? (
            <div className="text-center py-12 text-muted-foreground">
              <AlertCircle className="h-8 w-8 mx-auto mb-2 opacity-50" />
              <p>No events found</p>
              <p className="text-sm mt-1">
                {connected
                  ? 'Events will appear here as agents and hooks run'
                  : 'Click refresh to load historical logs'}
              </p>
            </div>
          ) : !showRawEvents ? (
            <div className="text-center py-8 text-muted-foreground">
              <p className="text-sm">Raw events are hidden.</p>
              <p className="text-xs mt-1">Use “Show raw events” to inspect full event rows and payloads.</p>
            </div>
          ) : (
            filteredLogs.map((log, idx) => (
              <LogEntryCard
                key={`${log.ts}-${log.operation}-${log.session_id || idx}`}
                log={log}
                onFocus={openLogFocus}
                onNavigate={navigateFromLog}
                onSelect={selectLog}
              />
            ))
          )}
        </div>
      </ScrollArea>
    </div>
  )
}

/**
 * Render a collapsible card for a single activity log entry showing operation, status,
 * timestamp, metadata, a compact data summary, and—when available—expanded error details
 * and raw payload with a copy-to-clipboard action.
 *
 * @param log - The log entry to display
 * @returns A JSX element representing the rendered log entry card
 */
function LogEntryCard({
  log,
  onFocus,
  onNavigate,
  onSelect,
}: {
  log: LogEntry
  onFocus: (log: LogEntry, label?: string) => void
  onNavigate: (log: LogEntry, target: SurfaceTarget) => void
  onSelect: (log: LogEntry) => void
}) {
  const isError = log.status === "error"
  const [expanded, setExpanded] = useState(isError)
  const [copied, setCopied] = useState(false)
  const errorDetails = getErrorDetails(log)
  const summary = getDataSummary(log)
  const rawPayload = getRawPayload(log)
  const rawPayloadText = useMemo(() => JSON.stringify(rawPayload, null, 2), [rawPayload])
  const callerLabel =
    log.caller ??
    (log.caller_file && log.caller_line ? `${log.caller_file}:${log.caller_line}` : log.caller_file)
  const callerTitle = [log.caller_func, log.caller_path].filter(Boolean).join(' · ') || undefined
  const inferredSurface = inferSurfaceTarget(log)
  const canFocus = Boolean(log.trace_id || log.session_id)
  const canRuntime = Boolean(log.session_id || log.agent_id)

  const handleCopyRaw = (event: MouseEvent<HTMLButtonElement>) => {
    event.stopPropagation()
    if (typeof navigator === "undefined" || !navigator.clipboard) {
      return
    }

    navigator.clipboard
      .writeText(rawPayloadText)
      .then(() => {
        setCopied(true)
        window.setTimeout(() => setCopied(false), 1500)
      })
      .catch(() => {
        setCopied(false)
      })
  }

  const icon = getComponentIcon(log.component)

  return (
    <div
      className={cn(
        'flex gap-3 p-3 rounded-lg bg-card border border-border hover:bg-accent/30 transition-colors cursor-pointer',
        isError && 'border-red-500/30'
      )}
      onClick={() => {
        setExpanded(!expanded)
        onSelect(log)
      }}
    >
        <div className="flex-shrink-0 mt-0.5">{icon}</div>
        <div className="flex-1 min-w-0">
          <div className="flex items-start justify-between gap-2">
            <div>
              <span className="font-medium text-foreground text-sm">
                {formatOperation(log.operation, log.command)}
              </span>
              <Badge
                variant={isError ? 'destructive' : 'secondary'}
                className="ml-2 text-xs"
              >
                {log.status}
              </Badge>
            </div>
            <span className="text-xs text-muted-foreground whitespace-nowrap">
              {formatRelativeTime(log.ts)}
            </span>
          </div>

          <div className="flex items-center gap-2 mt-1 text-xs text-muted-foreground flex-wrap">
            {log.component && (
              <span className="flex items-center gap-1">
                <Server className="h-3 w-3" />
                {log.component}
              </span>
            )}
            {callerLabel && (
              <span className="font-mono" title={callerTitle}>
                {callerLabel}
              </span>
            )}
            {log.session_id && (
              <span className="font-mono">{log.session_id.slice(0, 8)}</span>
            )}
            {log.duration_ms !== undefined && log.duration_ms > 0 && (
              <span className="flex items-center gap-1">
                <Clock className="h-3 w-3" />
                {log.duration_ms}ms
              </span>
            )}
          </div>

          {summary.length > 0 && (
            <div className="mt-2 text-xs text-muted-foreground">
              <div className="flex flex-wrap gap-x-3 gap-y-1">
                {summary.map((item, i) => (
                  <span key={i} className="text-muted-foreground/80">
                    {item}
                  </span>
                ))}
              </div>
            </div>
          )}

          {(canFocus || canRuntime || inferredSurface) && (
            <div className="mt-2 flex items-center gap-1.5 flex-wrap">
              {canFocus && (
                <Button
                  variant="outline"
                  size="sm"
                  className="h-6 px-2 text-[10px]"
                  onClick={(event) => {
                    event.stopPropagation()
                    onSelect(log)
                    onFocus(log)
                  }}
                >
                  Focus
                </Button>
              )}
              {canRuntime && (
                <Button
                  variant="outline"
                  size="sm"
                  className="h-6 px-2 text-[10px]"
                  onClick={(event) => {
                    event.stopPropagation()
                    onNavigate(log, 'runtime')
                  }}
                >
                  Runtime
                </Button>
              )}
              {inferredSurface && canFocus && (
                <Button
                  variant="outline"
                  size="sm"
                  className="h-6 px-2 text-[10px]"
                  onClick={(event) => {
                    event.stopPropagation()
                    onNavigate(log, inferredSurface)
                  }}
                >
                  {formatSurfaceLabel(inferredSurface)}
                </Button>
              )}
            </div>
          )}

          {isError && log.error_message && (
            <p className="mt-2 text-sm text-red-400 truncate">
              {log.error_message}
            </p>
          )}

          {expanded && isError && errorDetails.length > 0 && (
            <div className="mt-3 rounded-md border border-red-500/20 bg-red-500/5 p-2">
              <div className="text-[11px] font-semibold uppercase tracking-wide text-red-400/80 mb-2">
                Error details
              </div>
              <div className="space-y-1 text-xs">
                {errorDetails.map((detail) => (
                  <div key={detail.label} className="flex flex-wrap gap-x-2 gap-y-1">
                    <span className="shrink-0 text-muted-foreground">{detail.label}</span>
                    <span className="whitespace-pre-wrap break-words font-mono text-red-200">
                      {detail.value}
                    </span>
                  </div>
                ))}
              </div>
            </div>
          )}

          {expanded && Object.keys(rawPayload).length > 0 && (
            <div className="mt-3 p-2 rounded bg-muted/50">
              <div className="flex items-center justify-between mb-2">
                <div className="text-[11px] font-semibold uppercase tracking-wide text-muted-foreground">
                  Raw payload
                </div>
                <Button
                  variant="ghost"
                  size="sm"
                  onClick={handleCopyRaw}
                  className="h-6 px-2 text-[10px]"
                >
                  <Copy className="h-3 w-3 mr-1" />
                  {copied ? 'Copied' : 'Copy'}
                </Button>
              </div>
              <pre className="text-xs overflow-x-auto whitespace-pre-wrap">
                {rawPayloadText}
              </pre>
            </div>
          )}
        </div>
    </div>
  )
}

/**
 * Selects an icon element representing the given component type.
 *
 * @param component - The component type identifier (e.g., 'agent', 'hook', 'skill', 'embedding', 'index', 'web', 'cli')
 * @returns The JSX icon element corresponding to `component`; falls back to a generic server icon when `component` is unknown or not provided.
 */
function getComponentIcon(component?: string) {
  switch (component) {
    case 'agent':
      return <Bot className="h-4 w-4 text-blue-500" />
    case 'hook':
      return <Webhook className="h-4 w-4 text-purple-500" />
    case 'skill':
      return <Zap className="h-4 w-4 text-yellow-500" />
    case 'embedding':
      return <Server className="h-4 w-4 text-cyan-500" />
    case 'index':
      return <Server className="h-4 w-4 text-orange-500" />
    case 'web':
      return <Server className="h-4 w-4 text-green-500" />
    case 'cli':
      return <Server className="h-4 w-4 text-gray-400" />
    default:
      return <Server className="h-4 w-4 text-gray-500" />
  }
}

/**
 * Format an activity operation into a human-friendly label for display.
 *
 * @param operation - The dot-separated operation identifier (for example, `session.create` or `hooks.dispatch`)
 * @param command - Optional command, skill, or hook name to emphasize in the label
 * @returns If `command` is provided, returns `"command (opType)"` where `opType` is the operation's type (the second segment if present, otherwise the first). If `command` is not provided, returns the operation segments joined with `" → "` with each segment capitalized.
 */
function formatOperation(operation: string, command?: string): string {
  // If we have a command (skill/hook name), show it prominently
  if (command) {
    const opType = operation.split('.')[1] || operation.split('.')[0]
    return `${command} (${opType})`
  }
  return operation
    .split('.')
    .map((part) => part.charAt(0).toUpperCase() + part.slice(1))
    .join(' → ')
}

/**
 * Produce a concise human-readable summary of salient fields from a log's data.
 *
 * @param log - The log entry to extract summary items from
 * @returns An array (up to five items) of short summary strings describing key data fields (e.g., input query, model/provider, token counts, tool calls, file/text counts); returns an empty array if no relevant data is present
 */
function getDataSummary(log: LogEntry): string[] {
  const summary: string[] = []
  const data = log.data

  if (!data) return summary

  // Skill input fields (most useful for understanding what the skill did)
  if (data.input_query) {
    const q = String(data.input_query)
    summary.push(`"${q.length > 30 ? q.slice(0, 30) + '...' : q}"`)
  }
  if (data.input_scope) summary.push(`scope: ${data.input_scope}`)
  if (data.input_action) summary.push(`action: ${data.input_action}`)
  if (data.input_path) {
    const p = String(data.input_path)
    summary.push(p.split('/').pop() || p)
  }
  if (data.input_pattern) summary.push(`/${data.input_pattern}/`)
  if (data.input_limit) summary.push(`limit: ${data.input_limit}`)
  if (data.input_name) summary.push(`name: ${data.input_name}`)
  if (data.input_type) summary.push(`type: ${data.input_type}`)

  // LLM/Agent info
  if (data.model) summary.push(`${data.model}`)
  if (data.provider && !data.model) summary.push(`${data.provider}`)
  if (data.total_tokens) summary.push(`${data.total_tokens} tokens`)
  if (data.tokens_actual) summary.push(`${data.tokens_actual} tokens`)
  if (data.prompt_tokens && data.completion_tokens) {
    summary.push(`${data.prompt_tokens}→${data.completion_tokens}`)
  }
  if (data.iteration) summary.push(`iter ${data.iteration}`)
  if (data.tool_calls && Number(data.tool_calls) > 0) {
    summary.push(`${data.tool_calls} tools`)
  }

  // Embedding/Index info
  if (data.texts_count) summary.push(`${data.texts_count} texts`)
  if (data.cost_usd) summary.push(`$${Number(data.cost_usd).toFixed(6)}`)
  if (data.files_processed) summary.push(`${data.files_processed} files`)
  if (data.symbols_processed) summary.push(`${data.symbols_processed} symbols`)
  if (data.scope && !data.input_scope) summary.push(`scope: ${data.scope}`)

  // Hook dispatch info
  if (data.event) summary.push(`${data.event}`)
  if (data.tool_name) summary.push(`tool: ${data.tool_name}`)
  if (data.tool_kind) summary.push(`(${data.tool_kind})`)
  if (data.hooks_run !== undefined) summary.push(`${data.hooks_run} hooks`)
  if (data.hook_names && Array.isArray(data.hook_names)) {
    summary.push(data.hook_names.slice(0, 3).join(', '))
  }
  if (data.hook_name) summary.push(`hook: ${data.hook_name}`)
  if (data.blocked) {
    const blockedBy = data.blocked_by ? ` by ${data.blocked_by}` : ''
    summary.push(`BLOCKED${blockedBy}`)
  }

  return summary.slice(0, 5) // Limit to 5 items
}

type ErrorDetail = { label: string; value: string }

/**
 * Convert a value to a human-readable string suitable for display.
 *
 * @param value - The value to stringify or format for presentation
 * @returns A string representation of `value`; returns an empty string if `value` is `null` or `undefined`
 */
function formatErrorValue(value: unknown): string {
  if (value === null || value === undefined) return ""
  if (typeof value === "string") return value
  try {
    return JSON.stringify(value, null, 2)
  } catch {
    return String(value)
  }
}

/**
 * Extracts human-friendly error details from a log entry with status "error".
 *
 * Collects common error fields (for example: `message`, `error`, `code`, `hint`, `stack`) from the log and its `data`
 * and returns them as an ordered list of label/value pairs. Values are converted to formatted strings; duplicate
 * message values are not repeated.
 *
 * @param log - The log entry to inspect for error information
 * @returns An array of error details (label and formatted value). Returns an empty array if `log.status` is not `"error"`
 *          or no error fields are present.
 */
function getErrorDetails(log: LogEntry): ErrorDetail[] {
  if (log.status !== "error") return []

  const details: ErrorDetail[] = []
  const data = log.data ?? {}

  const pushDetail = (label: string, value: unknown) => {
    const formatted = formatErrorValue(value).trim()
    if (!formatted) return
    details.push({ label, value: formatted })
  }

  if (log.error_message) {
    pushDetail("message", log.error_message)
  }

  const dataError =
    (data as Record<string, unknown>).error ??
    (data as Record<string, unknown>).error_message ??
    (data as Record<string, unknown>).error_msg ??
    (data as Record<string, unknown>).message

  if (dataError && dataError !== log.error_message) {
    pushDetail("error", dataError)
  }

  const dataCode =
    (data as Record<string, unknown>).code ??
    (data as Record<string, unknown>).error_code ??
    (data as Record<string, unknown>).errorCode

  if (dataCode) {
    pushDetail("code", dataCode)
  }

  const dataHint =
    (data as Record<string, unknown>).hint ??
    (data as Record<string, unknown>).error_hint ??
    (data as Record<string, unknown>).help

  if (dataHint) {
    pushDetail("hint", dataHint)
  }

  const dataStack =
    (data as Record<string, unknown>).stack ??
    (data as Record<string, unknown>).stacktrace

  if (dataStack) {
    pushDetail("stack", dataStack)
  }

  return details
}

/**
 * Build an augmented raw payload object from a LogEntry for display or copying.
 *
 * The returned object contains the original `log.raw` (if present) merged with
 * top-level metadata such as `ts`, `operation`, and `status`, plus any present
 * identifiers, timing, error, retriable, and `data` fields from the log.
 *
 * @returns An object containing the raw payload augmented with metadata and any available log fields
 */
function getRawPayload(log: LogEntry): Record<string, unknown> {
  const payload: Record<string, unknown> = log.raw ? { ...log.raw } : {}

  payload.ts = log.ts
  payload.operation = log.operation
  payload.status = log.status

  if (log.command) payload.command = log.command
  if (log.component) payload.component = log.component
  if (log.trace_id) payload.trace_id = log.trace_id
  if (log.span_id) payload.span_id = log.span_id
  if (log.parent_id) payload.parent_id = log.parent_id
  if (log.service) payload.service = log.service
  if (log.version) payload.version = log.version
  if (log.subtype) payload.subtype = log.subtype
  if (log.session_id) payload.session_id = log.session_id
  if (log.agent_id) payload.agent_id = log.agent_id
  if (log.workspace_id) payload.workspace_id = log.workspace_id
  if (log.job_id) payload.job_id = log.job_id
  if (log.duration_ms !== undefined) payload.duration_ms = log.duration_ms
  if (log.error_type) payload.error_type = log.error_type
  if (log.error_code) payload.error_code = log.error_code
  if (log.error_message) payload.error_message = log.error_message
  if (log.retriable !== undefined) payload.retriable = log.retriable
  if (log.data !== undefined) payload.data = log.data

  return payload
}
