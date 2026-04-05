import * as React from "react"
import { Info } from "lucide-react"
import { cn } from "@/lib/utils"

type TooltipSide = "top" | "right" | "bottom" | "left"

type TooltipRootProps = {
  content?: React.ReactNode
  children: React.ReactNode
  side?: TooltipSide
  className?: string
  bubbleClassName?: string
  disabled?: boolean
}

type TooltipTriggerProps = {
  children: React.ReactNode
  asChild?: boolean
}

type TooltipContentProps = {
  children: React.ReactNode
  side?: TooltipSide
  className?: string
}

const TooltipTrigger = ({ children, asChild }: TooltipTriggerProps) => {
  if (asChild && React.isValidElement(children)) {
    return children
  }
  return <span className="inline-flex">{children}</span>
}
TooltipTrigger.displayName = "TooltipTrigger"

const TooltipContent = ({ children }: TooltipContentProps) => <>{children}</>
TooltipContent.displayName = "TooltipContent"

const TooltipProvider = ({ children }: { children: React.ReactNode }) => <>{children}</>
TooltipProvider.displayName = "TooltipProvider"

export function Tooltip({
  content,
  children,
  side = "top",
  className,
  bubbleClassName,
  disabled = false,
}: TooltipRootProps) {
  if (disabled) {
    return <>{children}</>
  }

  let triggerNode: React.ReactNode = children
  let bubbleNode: React.ReactNode = content
  let bubbleSide = side
  let bubbleExtraClass = bubbleClassName

  if (content === undefined) {
    triggerNode = null
    bubbleNode = null

    React.Children.forEach(children, (child) => {
      if (!React.isValidElement(child)) {
        return
      }
      const props = child.props as TooltipTriggerProps & TooltipContentProps
      if (child.type === TooltipTrigger) {
        triggerNode = props.children
        return
      }
      if (child.type === TooltipContent) {
        bubbleNode = props.children
        bubbleSide = props.side ?? bubbleSide
        bubbleExtraClass = cn(bubbleExtraClass, props.className)
        return
      }
      if (triggerNode == null) {
        triggerNode = child
      }
    })
  }

  if (!bubbleNode) {
    return <>{triggerNode}</>
  }

  return (
    <span className={cn("group/tooltip relative inline-flex", className)}>
      {triggerNode}
      <span
        role="tooltip"
        className={cn(
          "pointer-events-none absolute z-50 w-64 rounded-lg border border-border/80 bg-popover px-3 py-2 text-xs leading-5 text-popover-foreground shadow-xl opacity-0 transition duration-150 ease-out group-hover/tooltip:opacity-100 group-focus-within/tooltip:opacity-100",
          sideClasses(bubbleSide),
          bubbleExtraClass,
        )}
      >
        {bubbleNode}
      </span>
    </span>
  )
}

interface HelpTooltipProps {
  content: React.ReactNode
  label?: string
  side?: TooltipSide
  className?: string
  iconClassName?: string
}

export function HelpTooltip({
  content,
  label = "More info",
  side = "top",
  className,
  iconClassName,
}: HelpTooltipProps) {
  return (
    <Tooltip content={content} side={side} className={className}>
      <button
        type="button"
        aria-label={label}
        className="inline-flex h-5 w-5 items-center justify-center rounded-full text-muted-foreground transition-colors hover:bg-accent/40 hover:text-foreground focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-ring"
      >
        <Info className={cn("h-3.5 w-3.5", iconClassName)} />
      </button>
    </Tooltip>
  )
}

function sideClasses(side: TooltipSide): string {
  switch (side) {
    case "right":
      return "left-full top-1/2 ml-2 -translate-y-1/2"
    case "bottom":
      return "left-1/2 top-full mt-2 -translate-x-1/2"
    case "left":
      return "right-full top-1/2 mr-2 -translate-y-1/2"
    case "top":
    default:
      return "bottom-full left-1/2 mb-2 -translate-x-1/2"
  }
}

export { TooltipProvider, TooltipTrigger, TooltipContent }
