import { useMemo } from 'react'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardHeader } from '@/components/ui/card'
import { ScrollArea } from '@/components/ui/scroll-area'
import { ArrowRight } from 'lucide-react'

type SurfaceTarget = 'turns' | 'context' | 'artifacts'

interface RefDrilldownPanelProps {
  label: string
  data?: Record<string, unknown>
  canNavigate?: boolean
  onNavigate: (target: SurfaceTarget) => void
}

function toStringArray(value: unknown): string[] {
  if (!Array.isArray(value)) return []
  return value.filter((item): item is string => typeof item === 'string' && item.length > 0)
}

export function RefDrilldownPanel({
  label,
  data,
  canNavigate = true,
  onNavigate,
}: RefDrilldownPanelProps) {
  const refs = useMemo(() => {
    const source = data ?? {}
    return {
      turnRefs: toStringArray(source.turn_refs),
      sliceRefs: toStringArray(source.slice_refs),
      episodeRefs: toStringArray(source.episode_refs),
      narrativeRefs: toStringArray(source.narrative_refs),
      artifactRefs: toStringArray(source.artifact_refs),
      genericRefs: toStringArray(source.refs),
      expandableRefs: toStringArray(source.expandable_refs),
    }
  }, [data])

  const totalRefs =
    refs.turnRefs.length +
    refs.sliceRefs.length +
    refs.episodeRefs.length +
    refs.narrativeRefs.length +
    refs.artifactRefs.length +
    refs.genericRefs.length +
    refs.expandableRefs.length

  return (
    <Card className="border-border bg-card/60">
      <CardHeader className="py-3">
        <div className="flex items-center justify-between">
          <div>
            <div className="text-xs uppercase tracking-wide text-muted-foreground">
              Ref Drilldown
            </div>
            <div className="text-sm text-foreground truncate max-w-[26rem]">{label}</div>
          </div>
          <Badge variant="secondary" className="text-[10px]">
            {totalRefs} refs
          </Badge>
        </div>
      </CardHeader>
      <CardContent className="pt-0 pb-3 space-y-2">
        <div className="flex items-center gap-1.5 flex-wrap">
          <Button
            variant="outline"
            size="sm"
            className="h-7 text-[11px] px-2"
            onClick={() => onNavigate('turns')}
            disabled={!canNavigate || refs.turnRefs.length === 0}
          >
            Turns
            <ArrowRight className="h-3 w-3 ml-1" />
          </Button>
          <Button
            variant="outline"
            size="sm"
            className="h-7 text-[11px] px-2"
            onClick={() => onNavigate('context')}
            disabled={
              !canNavigate ||
              refs.sliceRefs.length === 0 &&
              refs.episodeRefs.length === 0 &&
              refs.narrativeRefs.length === 0 &&
              refs.genericRefs.length === 0 &&
              refs.expandableRefs.length === 0
            }
          >
            Context
            <ArrowRight className="h-3 w-3 ml-1" />
          </Button>
          <Button
            variant="outline"
            size="sm"
            className="h-7 text-[11px] px-2"
            onClick={() => onNavigate('artifacts')}
            disabled={!canNavigate || refs.artifactRefs.length === 0}
          >
            Artifacts
            <ArrowRight className="h-3 w-3 ml-1" />
          </Button>
        </div>

        {totalRefs === 0 ? (
          <div className="text-xs text-muted-foreground">No structured refs on selected event.</div>
        ) : !canNavigate ? (
          <div className="text-xs text-muted-foreground">
            Selected event has refs but no trace/session selector for contextual navigation.
          </div>
        ) : (
          <ScrollArea className="max-h-44">
            <div className="space-y-2">
              {[
                { label: 'turn_refs', values: refs.turnRefs },
                { label: 'slice_refs', values: refs.sliceRefs },
                { label: 'episode_refs', values: refs.episodeRefs },
                { label: 'narrative_refs', values: refs.narrativeRefs },
                { label: 'artifact_refs', values: refs.artifactRefs },
                { label: 'refs', values: refs.genericRefs },
                { label: 'expandable_refs', values: refs.expandableRefs },
              ]
                .filter((entry) => entry.values.length > 0)
                .map((entry) => (
                  <div key={entry.label}>
                    <div className="text-[10px] uppercase tracking-wide text-muted-foreground mb-1">
                      {entry.label}
                    </div>
                    <div className="flex flex-wrap gap-1">
                      {entry.values.slice(0, 8).map((value) => (
                        <Badge key={value} variant="outline" className="text-[10px] font-mono">
                          {value}
                        </Badge>
                      ))}
                      {entry.values.length > 8 && (
                        <Badge variant="secondary" className="text-[10px]">
                          +{entry.values.length - 8}
                        </Badge>
                      )}
                    </div>
                  </div>
                ))}
            </div>
          </ScrollArea>
        )}
      </CardContent>
    </Card>
  )
}
