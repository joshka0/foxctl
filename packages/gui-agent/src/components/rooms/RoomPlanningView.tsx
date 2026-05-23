import { useMemo, useState } from 'react'
import { Badge } from '@/components/ui/badge'
import { ScrollArea } from '@/components/ui/scroll-area'
import { cn } from '@/lib/utils'
import { formatRelativeTime } from '@/lib/time'
import type { MailboxMessage } from '@foxctl/data/types'

interface RoomPlanningViewProps {
  messages: MailboxMessage[]
  currentActorID: string
}

type PlanningSession = {
  id: string
  topic: string
  root: MailboxMessage
  status: 'drafting' | 'in_review' | 'blocked' | 'closed'
  proposals: MailboxMessage[]
  questions: MailboxMessage[]
  decisions: MailboxMessage[]
  reviews: MailboxMessage[]
  closes: MailboxMessage[]
}

const PLAN_SESSION_KIND = 'plan_session'
const PLAN_PROPOSAL_KIND = 'plan_proposal'
const PLAN_QUESTION_KIND = 'plan_question'
const PLAN_DECISION_KIND = 'plan_decision'
const PLAN_REVIEW_KIND = 'plan_review'
const PLAN_CLOSE_KIND = 'plan_close'

export function RoomPlanningView({ messages, currentActorID }: RoomPlanningViewProps) {
  const sessions = useMemo(() => buildPlanningSessions(messages), [messages])
  const [selectedSessionID, setSelectedSessionID] = useState<string>(sessions[0]?.id ?? '')

  const selectedSession = sessions.find((session) => session.id === selectedSessionID) ?? sessions[0] ?? null

  return (
    <div className="flex flex-1 min-h-0 bg-muted/5">
      <aside className="w-80 border-r bg-background/80 flex flex-col shrink-0">
        <div className="px-4 py-3 border-b">
          <div className="text-[10px] font-black uppercase tracking-widest text-muted-foreground">Planning Sessions</div>
          <div className="mt-1 text-xs text-muted-foreground">{sessions.length} durable sessions in room history</div>
        </div>
        <ScrollArea className="flex-1 min-h-0">
          <div className="p-2 space-y-2">
            {sessions.map((session) => (
              <button
                key={session.id}
                type="button"
                onClick={() => setSelectedSessionID(session.id)}
                className={cn(
                  'w-full rounded-xl border px-3 py-3 text-left transition-colors',
                  selectedSession?.id === session.id ? 'border-primary bg-primary/5' : 'border-border bg-background hover:bg-muted/40',
                )}
              >
                <div className="flex items-start justify-between gap-2">
                  <div className="min-w-0">
                    <div className="truncate text-xs font-bold text-foreground">{session.topic}</div>
                    <div className="mt-1 text-[10px] text-muted-foreground font-mono">{formatRelativeTime(session.root.created_at)}</div>
                  </div>
                  <SessionStatusBadge status={session.status} />
                </div>
                <div className="mt-2 flex flex-wrap gap-1.5">
                  <MiniCount label="proposals" value={session.proposals.length} />
                  <MiniCount label="questions" value={session.questions.length} />
                  <MiniCount label="decisions" value={session.decisions.length} />
                  <MiniCount label="reviews" value={session.reviews.length} />
                </div>
              </button>
            ))}
            {sessions.length === 0 && (
              <div className="p-6 text-center text-sm text-muted-foreground italic">
                No planning sessions found. Start one with `foxctl room plan start`.
              </div>
            )}
          </div>
        </ScrollArea>
      </aside>

      <div className="flex-1 min-w-0 flex flex-col">
        {!selectedSession ? (
          <div className="flex flex-1 items-center justify-center text-sm text-muted-foreground italic">
            No planning session selected.
          </div>
        ) : (
          <>
            <div className="px-4 py-3 border-b bg-background/80">
              <div className="flex items-start justify-between gap-3">
                <div className="min-w-0">
                  <div className="text-[10px] font-black uppercase tracking-widest text-muted-foreground">Planning Ledger</div>
                  <div className="mt-1 text-sm font-black tracking-tight text-foreground">{selectedSession.topic}</div>
                  <div className="mt-2 whitespace-pre-wrap text-[12px] leading-relaxed text-foreground/80">{selectedSession.root.body}</div>
                </div>
                <div className="flex flex-col items-end gap-2 shrink-0">
                  <SessionStatusBadge status={selectedSession.status} />
                  <Badge variant="outline" className="text-[9px] h-4 px-1.5 font-mono">
                    {selectedSession.id}
                  </Badge>
                </div>
              </div>
            </div>

            <ScrollArea className="flex-1 min-h-0">
              <div className="p-4 space-y-6">
                <PlanningSection title="Proposals" entries={selectedSession.proposals} currentActorID={currentActorID} empty="No proposals yet." />
                <PlanningSection title="Questions" entries={selectedSession.questions} currentActorID={currentActorID} empty="No open questions recorded." />
                <PlanningSection title="Decisions" entries={selectedSession.decisions} currentActorID={currentActorID} empty="No coordinator decisions recorded." />
                <PlanningSection title="Reviews" entries={selectedSession.reviews} currentActorID={currentActorID} empty="No reviews recorded." />
                <PlanningSection title="Closure" entries={selectedSession.closes} currentActorID={currentActorID} empty="Session is still open." />
              </div>
            </ScrollArea>
          </>
        )}
      </div>
    </div>
  )
}

function buildPlanningSessions(messages: MailboxMessage[]): PlanningSession[] {
  const roots = messages
    .filter((message) => message.kind === PLAN_SESSION_KIND)
    .sort((a, b) => new Date(b.created_at).getTime() - new Date(a.created_at).getTime())

  return roots.map((root) => {
    const entries = messages
      .filter((message) => message.related_message_id === root.id)
      .sort((a, b) => new Date(a.created_at).getTime() - new Date(b.created_at).getTime())

    const proposals = entries.filter((entry) => entry.kind === PLAN_PROPOSAL_KIND)
    const questions = entries.filter((entry) => entry.kind === PLAN_QUESTION_KIND)
    const decisions = entries.filter((entry) => entry.kind === PLAN_DECISION_KIND)
    const reviews = entries.filter((entry) => entry.kind === PLAN_REVIEW_KIND)
    const closes = entries.filter((entry) => entry.kind === PLAN_CLOSE_KIND)

    let status: PlanningSession['status'] = 'drafting'
    if (closes.length > 0) {
      status = 'closed'
    } else if (reviews.some((review) => review.subject.toLowerCase().includes('block'))) {
      status = 'blocked'
    } else if (reviews.length > 0) {
      status = 'in_review'
    }

    return {
      id: root.id,
      topic: root.subject.replace(/^Plan Session:\s*/i, '').trim() || root.subject,
      root,
      status,
      proposals,
      questions,
      decisions,
      reviews,
      closes,
    }
  })
}

function PlanningSection({ title, entries, currentActorID, empty }: { title: string; entries: MailboxMessage[]; currentActorID: string; empty: string }) {
  return (
    <section className="space-y-2">
      <div className="flex items-center justify-between gap-3">
        <div className="text-[10px] font-black uppercase tracking-widest text-muted-foreground">{title}</div>
        <Badge variant="outline" className="text-[9px] h-4 px-1.5 font-mono">{entries.length}</Badge>
      </div>
      {entries.length === 0 ? (
        <div className="rounded-xl border border-dashed px-3 py-4 text-sm text-muted-foreground italic">{empty}</div>
      ) : (
        <div className="space-y-2">
          {entries.map((entry) => {
            const own = entry.sender === currentActorID
            return (
              <div
                key={entry.id}
                className={cn(
                  'rounded-xl border px-3 py-3',
                  own ? 'border-primary/20 bg-primary/5' : 'border-border bg-background',
                )}
              >
                <div className="flex items-start justify-between gap-3">
                  <div className="min-w-0">
                    <div className="text-xs font-bold text-foreground">{entry.subject}</div>
                    <div className="mt-1 text-[10px] text-muted-foreground font-mono">{entry.sender}</div>
                  </div>
                  <div className="text-[10px] text-muted-foreground font-mono shrink-0">{formatRelativeTime(entry.created_at)}</div>
                </div>
                <div className="mt-2 whitespace-pre-wrap text-[12px] leading-relaxed text-foreground/80">{entry.body}</div>
              </div>
            )
          })}
        </div>
      )}
    </section>
  )
}

function SessionStatusBadge({ status }: { status: PlanningSession['status'] }) {
  const className =
    status === 'closed'
      ? 'border-emerald-500/30 text-emerald-700'
      : status === 'blocked'
        ? 'border-red-500/30 text-red-700'
        : status === 'in_review'
          ? 'border-amber-500/30 text-amber-700'
          : 'border-blue-500/30 text-blue-700'
  return (
    <Badge variant="outline" className={cn('text-[9px] h-4 px-1.5 uppercase', className)}>
      {status}
    </Badge>
  )
}

function MiniCount({ label, value }: { label: string; value: number }) {
  return (
    <Badge variant="outline" className="text-[9px] h-4 px-1.5 font-mono">
      {label}:{value}
    </Badge>
  )
}
