import { useCallback, useEffect, useState } from 'react'
import {
  ReactFlow,
  Background,
  Controls,
  MiniMap,
  useNodesState,
  useEdgesState,
  addEdge,
  Panel,
  type Connection,
  type Edge,
  type Node,
  type ReactFlowInstance,
} from '@xyflow/react'
import '@xyflow/react/dist/style.css'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Button } from '@/components/ui/button'
import { Badge } from '@/components/ui/badge'
import { ScrollArea } from '@/components/ui/scroll-area'
import { cn } from '@/lib/utils'
import {
  getFlow,
  patchFlow,
  addFlowNode,
  removeFlowNode,
  addFlowEdge,
  removeFlowEdge,
  startFlow,
  stopFlow,
  pauseFlow,
  listRooms,
  getRoomStatus,
} from '@/api/client'
import { FlowCanvasNode } from './FlowNodeComponents'
import { FlowCanvasEdge } from './FlowEdgeComponents'
import {
  ArrowLeft,
  Play,
  Square,
  Pause,
  Plus,
  Trash2,
  Pencil,
  Eye,
  Loader2,
  TerminalSquare,
  Bot,
  Wrench,
  Globe,
  Image,
  Shuffle,
  PlayCircle,
  Link2,
  Unlink,
  Users,
} from 'lucide-react'
import type {
  FlowNodeKind,
  FlowNode as FlowNodeType,
  FlowEdge as FlowEdgeType,
  FlowStatusResponse,
} from '@/types/flow'
import type {
  RoomStatus,
} from '@foxctl/data/types'

// ---------------------------------------------------------------------------
// Types
// ---------------------------------------------------------------------------

interface FlowCanvasProps {
  flowId: string
  flowName: string
  onBack: () => void
}

type Mode = 'design' | 'runtime'

interface CanvasNodeData extends Record<string, unknown> {
  label: string
  kind: FlowNodeKind
  config?: Record<string, unknown>
  state?: string
  error?: string
  duration_ms?: number
  session_id?: string
  flow_id?: string
}

// ---------------------------------------------------------------------------
// Node kind palette
// ---------------------------------------------------------------------------

const NODE_KINDS: { kind: FlowNodeKind; label: string; icon: React.ReactNode; defaultConfig: Record<string, unknown> }[] = [
  { kind: 'pty', label: 'PTY', icon: <TerminalSquare className="w-3.5 h-3.5" />, defaultConfig: { cmd: ['bash'] } },
  { kind: 'agent', label: 'Agent', icon: <Bot className="w-3.5 h-3.5" />, defaultConfig: { role: 'researcher', prompt: '' } },
  { kind: 'skill', label: 'Skill', icon: <Wrench className="w-3.5 h-3.5" />, defaultConfig: { skill: '' } },
  { kind: 'http', label: 'HTTP', icon: <Globe className="w-3.5 h-3.5" />, defaultConfig: { url: '' } },
  { kind: 'transform', label: 'Transform', icon: <Shuffle className="w-3.5 h-3.5" />, defaultConfig: {} },
  { kind: 'playwright', label: 'Browser', icon: <PlayCircle className="w-3.5 h-3.5" />, defaultConfig: {} },
  { kind: 'image', label: 'Image', icon: <Image className="w-3.5 h-3.5" />, defaultConfig: {} },
]

const nodeTypes = { flowNode: FlowCanvasNode }
const edgeTypes = { flowEdge: FlowCanvasEdge }

// ---------------------------------------------------------------------------
// Component
// ---------------------------------------------------------------------------

export function FlowCanvas({ flowId, flowName, onBack }: FlowCanvasProps) {
  const queryClient = useQueryClient()
  const [mode, setMode] = useState<Mode>('design')
  const [selectedNodeId, setSelectedNodeId] = useState<string | null>(null)
  const [selectedEdgeId, setSelectedEdgeId] = useState<string | null>(null)
  const [showPalette, setShowPalette] = useState(false)
  const [showRoomPanel, setShowRoomPanel] = useState(false)

  // React Flow state
  const [nodes, setNodes, onNodesChange] = useNodesState<Node<CanvasNodeData>>([])
  const [edges, setEdges, onEdgesChange] = useEdgesState<Edge>([])
  const [rfInstance, setRfInstance] = useState<ReactFlowInstance | null>(null)
  const [statusData, setStatusData] = useState<FlowStatusResponse | null>(null)

  // Load flow data
  const { data: flowData, isLoading } = useQuery({
    queryKey: ['flow', flowId],
    queryFn: () => getFlow(flowId),
  })

  const flowRoomId = (flowData as unknown as { room_id?: string })?.room_id

  // SSE stream for flow status (replaces polling)
  useEffect(() => {
    if (mode !== 'runtime') {
      setStatusData(null)
      return
    }
    const es = new EventSource(`/api/flows/${encodeURIComponent(flowId)}/events?workspace=.`,)
    es.onmessage = (ev) => {
      try {
        const parsed = JSON.parse(ev.data) as FlowStatusResponse
        setStatusData(parsed)
      } catch {
        // ignore malformed events
      }
    }
    es.onerror = () => {
      // Connection error; browser auto-retries per SSE spec
    }
    return () => {
      es.close()
    }
  }, [mode, flowId])

  // Room list for linking
  const { data: roomsData } = useQuery({
    queryKey: ['rooms', '.'],
    queryFn: () => listRooms({ workspace_id: '.' }),
    enabled: mode === 'design',
  })

  // Room status when linked
  const { data: roomStatus } = useQuery({
    queryKey: ['room-status', flowRoomId],
    queryFn: () => getRoomStatus(flowRoomId!, { workspace_id: '.' }),
    enabled: !!flowRoomId,
    refetchInterval: mode === 'runtime' ? 3000 : false,
  })

  const linkRoomMutation = useMutation({
    mutationFn: (roomId: string) => patchFlow(flowId, { room_id: roomId }),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: ['flow', flowId] }),
  })

  // Convert API data to React Flow nodes/edges
  useEffect(() => {
    if (!flowData) return

    const apiNodes: FlowNodeType[] = (flowData as unknown as { nodes: FlowNodeType[] }).nodes ?? []
    const apiEdges: FlowEdgeType[] = (flowData as unknown as { edges: FlowEdgeType[] }).edges ?? []

    const rfNodes: Node<CanvasNodeData>[] = apiNodes.map((n) => ({
      id: n.id,
      type: 'flowNode',
      position: n.position ?? { x: Math.random() * 400, y: Math.random() * 300 },
      data: {
        label: n.label,
        kind: n.kind,
        config: n.config as Record<string, unknown>,
        flow_id: flowId,
      },
    }))

    const rfEdges: Edge[] = apiEdges.map((e) => ({
      id: e.id,
      source: e.from_node_id,
      target: e.to_node_id,
      type: 'flowEdge',
      data: {
        transform: e.transform,
        trigger: e.trigger,
      },
    }))

    setNodes(rfNodes)
    setEdges(rfEdges)
  }, [flowData, setNodes, setEdges])

  // Overlay runtime status onto nodes/edges
  useEffect(() => {
    if (!statusData || mode !== 'runtime') return

    setNodes((prev) =>
      prev.map((n) => {
        const nodeStatus = statusData.nodes.find((ns) => ns.id === n.id)
        if (!nodeStatus) return n
        return {
          ...n,
          data: {
            ...n.data,
            state: nodeStatus.state,
            error: nodeStatus.error,
            duration_ms: nodeStatus.duration_ms,
            session_id: nodeStatus.session_id,
          },
        }
      }),
    )

    setEdges((prev) =>
      prev.map((e) => {
        const edgeStatus = statusData.edges.find((es) => es.id === e.id)
        if (!edgeStatus) return { ...e, data: { ...e.data, active: false } }
        return {
          ...e,
          animated: edgeStatus.delivery_count > 0,
          data: {
            ...e.data,
            active: edgeStatus.delivery_count > 0,
          },
        }
      }),
    )
  }, [statusData, mode, setNodes, setEdges])

  // Mutations
  const addNodeMutation = useMutation({
    mutationFn: (params: { kind: FlowNodeKind; label: string; position: { x: number; y: number } }) =>
      addFlowNode(flowId, {
        kind: params.kind,
        label: params.label,
        position: params.position,
        config: NODE_KINDS.find((k) => k.kind === params.kind)?.defaultConfig ?? {},
      }),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: ['flow', flowId] }),
  })

  const removeNodeMutation = useMutation({
    mutationFn: (nodeId: string) => removeFlowNode(flowId, nodeId),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['flow', flowId] })
      setSelectedNodeId(null)
    },
  })

  const addEdgeMutation = useMutation({
    mutationFn: (params: { from_node_id: string; to_node_id: string }) =>
      addFlowEdge(flowId, params),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: ['flow', flowId] }),
  })

  const removeEdgeMutation = useMutation({
    mutationFn: (edgeId: string) => removeFlowEdge(flowId, edgeId),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['flow', flowId] })
      setSelectedEdgeId(null)
    },
  })

  const startMutation = useMutation({
    mutationFn: () => startFlow(flowId, '.'),
    onSuccess: () => {
      setMode('runtime')
      queryClient.invalidateQueries({ queryKey: ['flow-status', flowId] })
      queryClient.invalidateQueries({ queryKey: ['flow', flowId] })
    },
  })

  const stopMutation = useMutation({
    mutationFn: () => stopFlow(flowId, '.'),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['flow-status', flowId] })
      queryClient.invalidateQueries({ queryKey: ['flow', flowId] })
    },
  })

  const pauseMutation = useMutation({
    mutationFn: () => pauseFlow(flowId, '.'),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: ['flow-status', flowId] }),
  })

  // Connection handler
  const onConnect = useCallback(
    (connection: Connection) => {
      if (!connection.source || !connection.target) return
      addEdgeMutation.mutate({
        from_node_id: connection.source,
        to_node_id: connection.target,
      })
      setEdges((eds) => addEdge(connection, eds))
    },
    [addEdgeMutation, setEdges],
  )

  // Node selection
  const onNodeClick = useCallback((_event: unknown, node: Node) => {
    setSelectedNodeId(node.id)
    setSelectedEdgeId(null)
  }, [])

  const onEdgeClick = useCallback((_event: unknown, edge: Edge) => {
    setSelectedEdgeId(edge.id)
    setSelectedNodeId(null)
  }, [])

  const onPaneClick = useCallback(() => {
    setSelectedNodeId(null)
    setSelectedEdgeId(null)
  }, [])

  // Add node from palette
  const handleAddNode = (kind: FlowNodeKind, label: string) => {
    const viewport = rfInstance?.getViewport() ?? { x: 0, y: 0, zoom: 1 }
    const position = {
      x: -viewport.x / viewport.zoom + 100 + Math.random() * 50,
      y: -viewport.y / viewport.zoom + 100 + Math.random() * 50,
    }
    addNodeMutation.mutate({ kind, label, position })
    setShowPalette(false)
  }

  const isRunning = statusData?.state === 'running'
  const isPaused = statusData?.state === 'paused'

  if (isLoading) {
    return (
      <div className="flex h-full items-center justify-center">
        <Loader2 className="w-8 h-8 animate-spin text-muted-foreground" />
      </div>
    )
  }

  return (
    <div className="flex h-full w-full bg-background"
    >
      {/* Canvas Area */}
      <div className="flex-1 relative"
      >
        <ReactFlow
          nodes={nodes}
          edges={edges}
          onNodesChange={onNodesChange as never}
          onEdgesChange={onEdgesChange as never}
          onConnect={onConnect as never}
          onNodeClick={onNodeClick as never}
          onEdgeClick={onEdgeClick as never}
          onPaneClick={onPaneClick}
          nodeTypes={nodeTypes as never}
          edgeTypes={edgeTypes as never}
          onInit={setRfInstance}
          fitView
          attributionPosition="bottom-left"
          deleteKeyCode={mode === 'design' ? 'Delete' : null}
          nodesConnectable={mode === 'design'}
          nodesDraggable={mode === 'design'}
          elementsSelectable={true}
        >
          <Background color="#3f3f46" gap={20} size={1} />
          <Controls className="!bg-card !border-border !shadow-sm" />
          <MiniMap
            className="!bg-card/80 !border-border !rounded-lg"
            nodeColor={(node) => {
              const colors: Record<string, string> = {
                pty: '#3b82f6',
                agent: '#a855f7',
                skill: '#f59e0b',
                http: '#06b6d4',
                transform: '#64748b',
              }
              return colors[node.data?.kind as string] ?? '#64748b'
            }}
            maskColor="rgba(0,0,0,0.4)"
          />

          {/* Top Toolbar */}
          <Panel position="top-left" className="m-4"
          >
            <div className="flex items-center gap-2 bg-card/90 backdrop-blur-sm border rounded-lg shadow-sm px-3 py-2"
            >
              <Button variant="ghost" size="xs" className="h-7 px-2" onClick={onBack}
              >
                <ArrowLeft className="w-3.5 h-3.5 mr-1" /> Back
              </Button
              >
              <div className="h-4 w-px bg-border mx-1"
              />
              <span className="text-xs font-bold truncate max-w-[160px]"
              >{flowName}
              </span>
              <FlowStateBadge state={statusData?.state ?? (flowData as unknown as { state: string })?.state ?? 'draft'}
              />
            </div>
          </Panel>

          {/* Room Link Toolbar */}
          {mode === 'design' && (
            <Panel position="top-center" className="m-4">
              <div className="flex items-center gap-2 bg-card/90 backdrop-blur-sm border rounded-lg shadow-sm px-3 py-2">
                <Link2 className="w-3 h-3 text-muted-foreground" />
                <span className="text-[10px] font-black uppercase tracking-tight text-muted-foreground">Room</span>
                {flowRoomId ? (
                  <>
                    <Badge variant="outline" className="text-[9px] font-mono h-4 px-1.5 cursor-pointer" onClick={() => setShowRoomPanel(!showRoomPanel)}>
                      {flowRoomId.slice(0, 8)}
                    </Badge>
                    <Button variant="ghost" size="xs" className="h-5 px-1 text-muted-foreground" onClick={() => linkRoomMutation.mutate('')}>
                      <Unlink className="w-3 h-3" />
                    </Button>
                  </>
                ) : (
                  <select
                    className="text-[10px] bg-background border rounded px-1.5 py-0.5 h-5 min-w-[120px]"
                    value=""
                    onChange={(e) => {
                      if (e.target.value) linkRoomMutation.mutate(e.target.value)
                    }}
                  >
                    <option value="">Link room…</option>
                    {roomsData?.rooms?.map((r) => (
                      <option key={r.id} value={r.id}>{r.title || r.id.slice(0, 8)}</option>
                    ))}
                  </select>
                )}
              </div>
            </Panel>
          )}

          {/* Mode + Execution Toolbar */}
          <Panel position="top-right" className="m-4"
          >
            <div className="flex items-center gap-2 bg-card/90 backdrop-blur-sm border rounded-lg shadow-sm px-3 py-2"
            >
              {/* Mode Toggle */}
              <div className="flex items-center rounded-md border border-border bg-background p-0.5"
              >
                <Button
                  variant={mode === 'design' ? 'secondary' : 'ghost'}
                  size="xs"
                  className="h-6 px-2 text-[10px] font-black uppercase"
                  onClick={() => setMode('design')}
                >
                  <Pencil className="w-3 h-3 mr-1" />
                  Design
                </Button>
                <Button
                  variant={mode === 'runtime' ? 'secondary' : 'ghost'}
                  size="xs"
                  className="h-6 px-2 text-[10px] font-black uppercase"
                  onClick={() => setMode('runtime')}
                >
                  <Eye className="w-3 h-3 mr-1" />
                  Runtime
                </Button>
              </div
              >

              <div className="h-4 w-px bg-border"
              />

              {/* Execution Controls */}
              {!isRunning && !isPaused && (
                <Button
                  variant="default"
                  size="xs"
                  className="h-6 px-2 text-[10px] font-black uppercase bg-green-600 hover:bg-green-700"
                  onClick={() => startMutation.mutate()}
                  disabled={startMutation.isPending}
                >
                  {startMutation.isPending ? <Loader2 className="w-3 h-3 animate-spin" /> : <Play className="w-3 h-3 mr-1" />}
                  Start
                </Button>
              )}
              {isRunning && (
                <>
                  <Button
                    variant="outline"
                    size="xs"
                    className="h-6 px-2 text-[10px] font-black uppercase"
                    onClick={() => pauseMutation.mutate()}
                    disabled={pauseMutation.isPending}
                  >
                    <Pause className="w-3 h-3 mr-1" />
                    Pause
                  </Button>
                  <Button
                    variant="outline"
                    size="xs"
                    className="h-6 px-2 text-[10px] font-black uppercase text-red-600 border-red-500/30 hover:bg-red-500/10"
                    onClick={() => stopMutation.mutate()}
                    disabled={stopMutation.isPending}
                  >
                    <Square className="w-3 h-3 mr-1" />
                    Stop
                  </Button>
                </>
              )}
              {isPaused && (
                <>
                  <Button
                    variant="default"
                    size="xs"
                    className="h-6 px-2 text-[10px] font-black uppercase bg-green-600 hover:bg-green-700"
                    onClick={() => startMutation.mutate()}
                    disabled={startMutation.isPending}
                  >
                    <Play className="w-3 h-3 mr-1" />
                    Resume
                  </Button>
                  <Button
                    variant="outline"
                    size="xs"
                    className="h-6 px-2 text-[10px] font-black uppercase text-red-600 border-red-500/30 hover:bg-red-500/10"
                    onClick={() => stopMutation.mutate()}
                    disabled={stopMutation.isPending}
                  >
                    <Square className="w-3 h-3 mr-1" />
                    Stop
                  </Button>
                </>
              )}
            </div>
          </Panel>

          {/* Add Node Palette (Design mode only) */}
          {mode === 'design' && (
            <Panel position="bottom-left" className="m-4"
            >
              <div className="bg-card/90 backdrop-blur-sm border rounded-lg shadow-sm overflow-hidden"
              >
                <button
                  onClick={() => setShowPalette(!showPalette)}
                  className="flex items-center gap-2 px-3 py-2 w-full text-left hover:bg-muted/40 transition-colors"
                >
                  <Plus className="w-3.5 h-3.5"
                  />
                  <span className="text-[10px] font-black uppercase tracking-tight"
                  >Add Node</span
                  >
                </button>
                {showPalette && (
                  <div className="grid grid-cols-2 gap-1 p-2 border-t"
                  >
                    {NODE_KINDS.map((k) => (
                      <button
                        key={k.kind}
                        onClick={() => handleAddNode(k.kind, k.label)}
                        className="flex items-center gap-2 px-2 py-1.5 rounded text-[10px] font-medium hover:bg-muted/60 transition-colors"
                      >
                        {k.icon}
                        {k.label}
                      </button>
                    ))}
                  </div>
                )}
              </div>
            </Panel>
          )}
        </ReactFlow>
      </div>

      {/* Right Side Panel: Properties + Room */}
      {(selectedNodeId || selectedEdgeId || (showRoomPanel && flowRoomId)) && (
        <aside className="w-64 border-l bg-muted/5 flex flex-col shrink-0"
        >
          <div className="px-4 py-3 border-b"
          >
            <h3 className="text-[10px] font-black uppercase tracking-widest text-muted-foreground"
            >
              {selectedNodeId ? 'Node Properties' : selectedEdgeId ? 'Edge Properties' : 'Room'}
            </h3>
          </div>
          <ScrollArea className="flex-1"
          >
            {selectedNodeId && (
              <NodePropertyPanel
                node={nodes.find((n) => n.id === selectedNodeId)}
                onRemove={() => removeNodeMutation.mutate(selectedNodeId)}
                isPending={removeNodeMutation.isPending}
              />
            )}
            {selectedEdgeId && (
              <EdgePropertyPanel
                edge={edges.find((e) => e.id === selectedEdgeId)}
                onRemove={() => removeEdgeMutation.mutate(selectedEdgeId)}
                isPending={removeEdgeMutation.isPending}
              />
            )}
            {showRoomPanel && flowRoomId && roomStatus && (
              <RoomPanel status={roomStatus} onClose={() => setShowRoomPanel(false)} />
            )}
          </ScrollArea>
        </aside>
      )}
    </div>
  )
}

// ---------------------------------------------------------------------------
// Property Panels
// ---------------------------------------------------------------------------

function NodePropertyPanel({
  node,
  onRemove,
  isPending,
}: {
  node?: Node<CanvasNodeData>
  onRemove: () => void
  isPending: boolean
}) {
  if (!node) return null
  const data = node.data

  return (
    <div className="p-4 space-y-4"
    >
      <div className="space-y-1"
      >
        <div className="text-[9px] font-black uppercase tracking-widest text-muted-foreground"
        >Label
        </div
        >
        <div className="text-xs font-medium"
        >{data.label}
        </div
        >
      </div>
      <div className="space-y-1"
      >
        <div className="text-[9px] font-black uppercase tracking-widest text-muted-foreground"
        >Kind
        </div
        >
        <Badge variant="outline" className="text-[10px]"
        >{data.kind}
        </Badge>
      </div>
      <div className="space-y-1"
      >
        <div className="text-[9px] font-black uppercase tracking-widest text-muted-foreground"
        >ID
        </div
        >
        <div className="text-[10px] font-mono text-muted-foreground break-all"
        >{node.id}
        </div
        >
      </div>
      {data.state && (
        <div className="space-y-1"
        >
          <div className="text-[9px] font-black uppercase tracking-widest text-muted-foreground"
          >State
          </div
          >
          <Badge variant="outline" className="text-[10px]"
          >{data.state}
          </Badge>
        </div>
      )}
      {data.error && (
        <div className="space-y-1"
        >
          <div className="text-[9px] font-black uppercase tracking-widest text-red-500"
          >Error
          </div
          >
          <div className="text-[10px] font-mono text-red-400 break-all"
          >{data.error}
          </div
          >
        </div>
      )}
      {data.config && Object.keys(data.config).length > 0 && (
        <div className="space-y-1"
        >
          <div className="text-[9px] font-black uppercase tracking-widest text-muted-foreground"
          >Config
          </div
          >
          <pre className="text-[9px] font-mono text-muted-foreground bg-muted/30 p-2 rounded overflow-auto"
          >
            {JSON.stringify(data.config, null, 2)}
          </pre
          >
        </div>
      )}
      <Button
        variant="outline"
        size="sm"
        className="w-full text-red-600 border-red-500/30 hover:bg-red-500/10 text-[10px] font-black uppercase"
        onClick={onRemove}
        disabled={isPending}
      >
        {isPending ? <Loader2 className="w-3 h-3 animate-spin" /> : <Trash2 className="w-3 h-3 mr-1" />}
        Remove Node
      </Button>
    </div>
  )
}

function EdgePropertyPanel({
  edge,
  onRemove,
  isPending,
}: {
  edge?: Edge
  onRemove: () => void
  isPending: boolean
}) {
  if (!edge) return null
  const data = edge.data as { transform?: string; trigger?: string } | undefined

  return (
    <div className="p-4 space-y-4"
    >
      <div className="space-y-1"
      >
        <div className="text-[9px] font-black uppercase tracking-widest text-muted-foreground"
        >From → To
        </div
        >
        <div className="text-[10px] font-mono text-muted-foreground"
        >
          {edge.source.slice(0, 8)} → {edge.target.slice(0, 8)}
        </div
        >
      </div>
      {data?.transform && (
        <div className="space-y-1"
        >
          <div className="text-[9px] font-black uppercase tracking-widest text-muted-foreground"
          >Transform
          </div
          >
          <Badge variant="outline" className="text-[10px]"
          >{data.transform}
          </Badge>
        </div>
      )}
      {data?.trigger && (
        <div className="space-y-1"
        >
          <div className="text-[9px] font-black uppercase tracking-widest text-muted-foreground"
          >Trigger
          </div
          >
          <Badge variant="outline" className="text-[10px]"
          >{data.trigger}
          </Badge>
        </div>
      )}
      <Button
        variant="outline"
        size="sm"
        className="w-full text-red-600 border-red-500/30 hover:bg-red-500/10 text-[10px] font-black uppercase"
        onClick={onRemove}
        disabled={isPending}
      >
        {isPending ? <Loader2 className="w-3 h-3 animate-spin" /> : <Trash2 className="w-3 h-3 mr-1" />}
        Remove Edge
      </Button>
    </div>
  )
}

function FlowStateBadge({ state }: { state: string }) {
  const colors: Record<string, string> = {
    draft: 'bg-slate-500/10 text-slate-500 border-slate-500/20',
    running: 'bg-green-500/10 text-green-500 border-green-500/20',
    paused: 'bg-amber-500/10 text-amber-500 border-amber-500/20',
    stopped: 'bg-muted text-muted-foreground',
    errored: 'bg-red-500/10 text-red-500 border-red-500/20',
  }
  return (
    <Badge variant="outline" className={cn('text-[9px] h-4 px-1.5 font-bold', colors[state] ?? colors.draft)}
    >
      {state}
    </Badge>
  )
}

function RoomPanel({ status, onClose }: { status: RoomStatus; onClose: () => void }) {
  return (
    <div className="p-4 space-y-4">
      <div className="flex items-center justify-between">
        <div className="flex items-center gap-1.5">
          <Users className="w-3 h-3 text-muted-foreground" />
          <span className="text-[9px] font-black uppercase tracking-widest text-muted-foreground">Participants</span>
        </div>
        <button onClick={onClose} className="text-[10px] text-muted-foreground hover:text-foreground">✕</button>
      </div>
      <div className="space-y-2">
        {status.participants.length === 0 && (
          <div className="text-[10px] text-muted-foreground italic">No participants</div>
        )}
        {status.participants.map((p) => (
          <div key={p.actor_id} className="flex items-center gap-2 p-1.5 rounded bg-muted/30">
            <div className="w-5 h-5 rounded-full bg-primary/10 flex items-center justify-center text-[8px] font-bold uppercase">
              {p.actor_id.slice(0, 2)}
            </div>
            <div className="flex-1 min-w-0">
              <div className="text-[10px] font-medium truncate">{p.actor_id}</div>
              <div className="text-[9px] text-muted-foreground">{p.status} · {p.assigned_task_count} tasks</div>
            </div>
          </div>
        ))}
      </div>
      <div className="pt-2 border-t space-y-2">
        <div className="flex items-center justify-between text-[9px]">
          <span className="text-muted-foreground">Pending</span>
          <span className="font-medium">{status.task_pulse.pending}</span>
        </div>
        <div className="flex items-center justify-between text-[9px]">
          <span className="text-muted-foreground">In Progress</span>
          <span className="font-medium">{status.task_pulse.in_progress}</span>
        </div>
        <div className="flex items-center justify-between text-[9px]">
          <span className="text-muted-foreground">Blocked</span>
          <span className="font-medium">{status.task_pulse.blocked}</span>
        </div>
      </div>
    </div>
  )
}
