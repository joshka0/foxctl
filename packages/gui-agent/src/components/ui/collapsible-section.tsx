import { useState } from 'react'
import { ChevronDown, ChevronRight } from 'lucide-react'
import { cn } from '@/lib/utils'
import { Badge } from '@/components/ui/badge'

interface CollapsibleSectionProps {
  title: string
  icon?: React.ReactNode
  defaultOpen?: boolean
  open?: boolean
  onToggle?: (open: boolean) => void
  badge?: string | number
  children: React.ReactNode
  className?: string
}

export function CollapsibleSection({
  title,
  icon,
  defaultOpen = false,
  open: controlledOpen,
  onToggle,
  badge,
  children,
  className,
}: CollapsibleSectionProps) {
  const [internalOpen, setInternalOpen] = useState(defaultOpen)
  const isControlled = controlledOpen !== undefined
  const isOpen = isControlled ? controlledOpen : internalOpen

  const toggle = () => {
    const next = !isOpen
    if (isControlled) {
      onToggle?.(next)
    } else {
      setInternalOpen(next)
      onToggle?.(next)
    }
  }

  return (
    <div className={cn('border-b border-border last:border-b-0', className)}>
      <button
        type="button"
        onClick={toggle}
        className="flex items-center gap-2 w-full px-3 py-2 text-xs font-medium text-muted-foreground hover:text-foreground hover:bg-accent/30 transition-colors"
      >
        {isOpen ? (
          <ChevronDown className="h-3.5 w-3.5 flex-shrink-0" />
        ) : (
          <ChevronRight className="h-3.5 w-3.5 flex-shrink-0" />
        )}
        {icon && <span className="flex-shrink-0">{icon}</span>}
        <span className="uppercase tracking-wider">{title}</span>
        {badge !== undefined && (
          <Badge variant="outline" className="text-[9px] px-1 py-0 ml-auto">
            {badge}
          </Badge>
        )}
      </button>
      {isOpen && <div className="px-3 pb-3 space-y-2">{children}</div>}
    </div>
  )
}
