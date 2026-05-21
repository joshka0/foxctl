import { memo } from 'react'
import { BaseEdge, EdgeLabelRenderer, getBezierPath, type EdgeProps } from '@xyflow/react'
import { cn } from '@/lib/utils'

interface FlowEdgeData {
  transform?: string
  trigger?: string
  active?: boolean
}

export const FlowCanvasEdge = memo(function FlowCanvasEdge(props: EdgeProps) {
  const data = props.data as FlowEdgeData | undefined
  const [edgePath, labelX, labelY] = getBezierPath({
    sourceX: props.sourceX,
    sourceY: props.sourceY,
    sourcePosition: props.sourcePosition,
    targetX: props.targetX,
    targetY: props.targetY,
    targetPosition: props.targetPosition,
  })

  const isActive = data?.active ?? false
  const label = data?.transform && data.transform !== 'passthrough'
    ? data.transform.replace(/_/g, ' ')
    : undefined

  return (
    <>
      <BaseEdge
        path={edgePath}
        markerEnd={props.markerEnd}
        style={{
          strokeWidth: isActive ? 2.5 : 1.5,
          stroke: isActive ? '#f59e0b' : '#52525b',
          strokeDasharray: isActive ? undefined : '4 2',
          transition: 'all 300ms ease',
        }}
      />
      {label && (
        <EdgeLabelRenderer>
          <div
            className={cn(
              'nodrag nopan pointer-events-none',
              'absolute px-1.5 py-0.5 rounded text-[9px] font-mono font-bold uppercase tracking-wider',
              'bg-background/90 border border-border shadow-sm',
              isActive && 'text-amber-500 border-amber-500/30',
            )}
            style={{
              transform: `translate(-50%, -50%) translate(${labelX}px,${labelY}px)`,
            }}
          >
            {label}
          </div>
        </EdgeLabelRenderer>
      )}
    </>
  )
})
