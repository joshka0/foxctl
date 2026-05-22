import { memo, useEffect, useRef } from 'react'
import { Handle, Position, type NodeProps } from '@xyflow/react'
import { cn } from '@/lib/utils'
import {
  TerminalSquare,
  Bot,
  Wrench,
  Globe,
  Image,
  Shuffle,
  Play,
  Loader2,
  CheckCircle2,
  XCircle,
  Pause,
} from 'lucide-react'
import { Terminal } from '@xterm/xterm'
import '@xterm/xterm/css/xterm.css'
import { useQuery } from '@tanstack/react-query'
import { getFlowNodeTerminal } from '@/api/client'
import type { FlowNodeKind } from '@/types/flow'

interface FlowNodeData extends Record<string, unknown> {
  label: string
  kind: FlowNodeKind
  config?: Record<string, unknown>
  state?: string // idle | running | completed | errored
  error?: string
  duration_ms?: number
  session_id?: string
  flow_id?: string
}

const kindIcons: Record<FlowNodeKind, React.ReactNode> = {
  pty: <TerminalSquare className="w-4 h-4" />,
  agent: <Bot className="w-4 h-4" />,
  skill: <Wrench className="w-4 h-4" />,
  http: <Globe className="w-4 h-4" />,
  playwright: <Play className="w-4 h-4" />,
  image: <Image className="w-4 h-4" />,
  transform: <Shuffle className="w-4 h-4" />,
}

const kindColors: Record<FlowNodeKind, string> = {
  pty: 'border-blue-500/40 bg-blue-500/5',
  agent: 'border-purple-500/40 bg-purple-500/5',
  skill: 'border-amber-500/40 bg-amber-500/5',
  http: 'border-cyan-500/40 bg-cyan-500/5',
  playwright: 'border-pink-500/40 bg-pink-500/5',
  image: 'border-emerald-500/40 bg-emerald-500/5',
  transform: 'border-slate-500/40 bg-slate-500/5',
}

const kindAccent: Record<FlowNodeKind, string> = {
  pty: 'text-blue-400',
  agent: 'text-purple-400',
  skill: 'text-amber-400',
  http: 'text-cyan-400',
  playwright: 'text-pink-400',
  image: 'text-emerald-400',
  transform: 'text-slate-400',
}

function StateIcon({ state }: { state?: string }) {
  switch (state) {
    case 'running':
      return <Loader2 className="w-3 h-3 animate-spin text-amber-400" />
    case 'completed':
      return <CheckCircle2 className="w-3 h-3 text-green-400" />
    case 'errored':
      return <XCircle className="w-3 h-3 text-red-400" />
    case 'idle':
      return <Pause className="w-3 h-3 text-muted-foreground" />
    default:
      return null
  }
}

export const FlowCanvasNode = memo(function FlowCanvasNode(props: NodeProps) {
  const data = props.data as FlowNodeData
  const kind = data.kind ?? 'skill'
  const isRunning = data.state === 'running'

  return (
    <div
      className={cn(
        'min-w-[160px] rounded-lg border shadow-sm transition-all',
        'bg-card/90 backdrop-blur-sm',
        kindColors[kind],
        isRunning && 'ring-2 ring-amber-500/30 shadow-amber-500/10',
        props.selected && 'ring-2 ring-primary/50',
      )}
    >
      {/* Input handle */}
      <Handle
        type="target"
        position={Position.Left}
        className="!w-2.5 !h-2.5 !bg-background !border-2 !border-primary"
      />

      {/* Header */}
      <div className="flex items-center gap-2 px-3 py-2 border-b border-border/50">
        <span className={cn('shrink-0', kindAccent[kind])}>{kindIcons[kind]}</span>
        <span className="text-[11px] font-bold truncate flex-1">{data.label}</span>
        <StateIcon state={data.state} />
      </div>

      {/* Body */}
      <div className="px-3 py-2 space-y-1">
        {data.error ? (
          <div className="text-[9px] text-red-400 font-mono truncate" title={data.error}>
            {data.error}
          </div>
        ) : (
          <div className="text-[9px] text-muted-foreground font-mono uppercase tracking-wider">
            {kind}
          </div>
        )}
        {typeof data.duration_ms === 'number' && data.duration_ms > 0 && (
          <div className="text-[9px] text-muted-foreground font-mono">
            {data.duration_ms}ms
          </div>
        )}
        {data.config && Object.keys(data.config).length > 0 && (
          <div className="text-[9px] text-muted-foreground font-mono truncate">
            {Object.keys(data.config).slice(0, 2).join(', ')}
            {Object.keys(data.config).length > 2 && '…'}
          </div>
        )}
        {kind === 'pty' && data.session_id && data.flow_id && (
          <PtyTerminalEmbed flowId={data.flow_id} nodeId={props.id} sessionId={data.session_id} />
        )}
      </div>

      {/* Output handle */}
      <Handle
        type="source"
        position={Position.Right}
        className="!w-2.5 !h-2.5 !bg-background !border-2 !border-primary"
      />
    </div>
  )
})

function PtyTerminalEmbed({
  flowId,
  nodeId,
  sessionId,
}: {
  flowId: string
  nodeId: string
  sessionId: string
}) {
  const containerRef = useRef<HTMLDivElement>(null)
  const termRef = useRef<Terminal | null>(null)

  const { data: screenData } = useQuery({
    queryKey: ['flow-node-terminal', flowId, nodeId],
    queryFn: () => getFlowNodeTerminal(flowId, nodeId, '.'),
    refetchInterval: 1000,
    enabled: !!sessionId,
  })

  useEffect(() => {
    if (!containerRef.current || termRef.current) return

    const term = new Terminal({
      cols: 80,
      rows: 12,
      fontSize: 10,
      fontFamily: 'monospace',
      theme: {
        background: '#0c0c0c',
        foreground: '#e6e6e6',
      },
      disableStdin: true,
      cursorBlink: false,
      convertEol: true,
    })

    term.open(containerRef.current)
    termRef.current = term

    return () => {
      term.dispose()
      termRef.current = null
    }
  }, [])

  useEffect(() => {
    if (!screenData || !termRef.current) return
    const term = termRef.current
    term.clear()
    for (const line of screenData.lines) {
      term.writeln(line)
    }
  }, [screenData])

  return (
    <div
      ref={containerRef}
      className="mt-1 rounded overflow-hidden border border-white/10"
      style={{ width: 320, height: 160 }}
    />
  )
}
