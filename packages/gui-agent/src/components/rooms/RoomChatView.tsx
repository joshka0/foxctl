import { useMemo, useState } from 'react'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { ScrollArea } from '@/components/ui/scroll-area'
import { cn } from '@/lib/utils'
import { formatRelativeTime } from '@/lib/time'
import type { MailboxMessage, RoomMember, RoomMessageEvent } from '@foxctl/data/types'

interface RoomChatViewProps {
  messages: MailboxMessage[]
  currentActorID: string
  participants: RoomMember[]
  events: RoomMessageEvent[]
}

type ChatFilter = 'all' | 'awaiting' | 'resolved' | 'broadcasts' | 'direct' | 'system'

export function RoomChatView({ messages, currentActorID, participants, events }: RoomChatViewProps) {
  const [filter, setFilter] = useState<ChatFilter>('all')

  const messageMeta = useMemo(() => {
    const replyMap = new Map<string, MailboxMessage[]>()
    for (const message of messages) {
      if (!message.related_message_id) continue
      const replies = replyMap.get(message.related_message_id) ?? []
      replies.push(message)
      replyMap.set(message.related_message_id, replies)
    }

    const eventMap = new Map<
      string,
      {
        phases: string[]
        errors: string[]
        dispatched?: number
        skipped?: number
      }
    >()
    for (const event of events) {
      if (!event.message_id) continue
      const current = eventMap.get(event.message_id) ?? { phases: [], errors: [] }
      if (event.phase && !current.phases.includes(event.phase)) {
        current.phases.push(event.phase)
      }
      if (event.error) {
        current.errors.push(event.error)
      }
      if (typeof event.dispatched === 'number') {
        current.dispatched = event.dispatched
      }
      if (typeof event.skipped === 'number') {
        current.skipped = event.skipped
      }
      eventMap.set(event.message_id, current)
    }

    const participantIDs = participants
      .map((participant) => participant.actor_id)
      .filter((actorID) => actorID && !actorID.startsWith('actor:system:'))

    return new Map(
      messages.map((message) => {
        const replies = replyMap.get(message.id) ?? []
        const closedByStatus = message.status === 'read' || message.status === 'acked'
        const awaitingAck = message.ack_required && message.status !== 'acked'
        const awaitingReply = message.reply_expected && !closedByStatus && replies.length === 0
        const resolved = closedByStatus || (!awaitingAck && !awaitingReply && replies.length > 0)
        const audienceCount =
          message.recipient === '*'
            ? participantIDs.filter((actorID) => actorID !== message.sender).length
            : 1
        return [
          message.id,
          {
            replies,
            awaitingAck,
            awaitingReply,
            resolved,
            audienceCount,
            events: eventMap.get(message.id) ?? { phases: [], errors: [] },
          },
        ]
      }),
    )
  }, [events, messages, participants])

  const filtered = useMemo(() => {
    return messages.filter((message) => {
      const meta = messageMeta.get(message.id)
      const awaiting = Boolean(meta?.awaitingAck || meta?.awaitingReply)
      const resolved = Boolean(meta?.resolved)
      if (filter === 'all') return true
      if (filter === 'awaiting') return awaiting
      if (filter === 'resolved') return resolved
      if (filter === 'broadcasts') return message.recipient === '*'
      if (filter === 'direct') return message.recipient !== '*'
      if (filter === 'system') return message.sender.startsWith('actor:system:')
      return true
    })
  }, [filter, messageMeta, messages])

  const summary = useMemo(() => {
    let awaiting = 0
    let resolved = 0
    let interrupts = 0
    let broadcasts = 0
    for (const message of messages) {
      const meta = messageMeta.get(message.id)
      if (meta?.awaitingAck || meta?.awaitingReply) awaiting++
      if (meta?.resolved) resolved++
      if (message.interrupt) interrupts++
      if (message.recipient === '*') broadcasts++
    }
    return { awaiting, resolved, interrupts, broadcasts }
  }, [messageMeta, messages])

  return (
    <div className="flex flex-1 min-h-0 flex-col bg-muted/5">
      <div className="px-4 py-2 border-b bg-background/80 flex items-center justify-between gap-3 shrink-0">
        <div className="flex items-center gap-2 flex-wrap">
          <div className="text-[10px] font-black uppercase tracking-widest text-muted-foreground">Room Chat</div>
          <Badge variant="outline" className="text-[9px] h-4 px-1.5 font-mono">
            {filtered.length} shown
          </Badge>
          <Badge variant="outline" className="text-[9px] h-4 px-1.5 font-mono border-amber-500/30 text-amber-700">
            {summary.awaiting} awaiting
          </Badge>
          <Badge variant="outline" className="text-[9px] h-4 px-1.5 font-mono border-emerald-500/30 text-emerald-700">
            {summary.resolved} resolved
          </Badge>
          <Badge variant="outline" className="text-[9px] h-4 px-1.5 font-mono border-red-500/30 text-red-700">
            {summary.interrupts} interrupt
          </Badge>
          <Badge variant="outline" className="text-[9px] h-4 px-1.5 font-mono border-sky-500/30 text-sky-700">
            {summary.broadcasts} broadcasts
          </Badge>
        </div>
        <div className="flex flex-wrap gap-1.5">
          <ChatFilterButton label="all" active={filter === 'all'} onClick={() => setFilter('all')} />
          <ChatFilterButton label="awaiting" active={filter === 'awaiting'} onClick={() => setFilter('awaiting')} />
          <ChatFilterButton label="resolved" active={filter === 'resolved'} onClick={() => setFilter('resolved')} />
          <ChatFilterButton label="broadcasts" active={filter === 'broadcasts'} onClick={() => setFilter('broadcasts')} />
          <ChatFilterButton label="direct" active={filter === 'direct'} onClick={() => setFilter('direct')} />
          <ChatFilterButton label="system" active={filter === 'system'} onClick={() => setFilter('system')} />
        </div>
      </div>

      <ScrollArea className="flex-1 min-h-0">
        <div className="p-4 space-y-3">
          {filtered.map((message) => {
            const meta = messageMeta.get(message.id)
            const own = message.sender === currentActorID
            const actionable = Boolean(meta?.awaitingAck || meta?.awaitingReply)
            const system = message.sender.startsWith('actor:system:')
            const latestReply = meta?.replies.at(-1)
            return (
              <div
                key={message.id}
                className={cn(
                  'max-w-[92%] rounded-2xl border px-3 py-3 shadow-sm',
                  own ? 'ml-auto bg-primary/5 border-primary/20' : 'bg-background border-border',
                  system && 'border-blue-500/20 bg-blue-500/5',
                  actionable && 'ring-1 ring-amber-500/10',
                )}
              >
                <div className="flex items-start justify-between gap-3">
                  <div className="min-w-0">
                    <div className="flex items-center gap-2 flex-wrap">
                      <span className="text-[11px] font-bold text-foreground">{message.sender}</span>
                      <span className="text-[10px] text-muted-foreground">→</span>
                      <Badge variant="outline" className="text-[9px] h-4 px-1.5 font-mono">
                        {message.recipient === '*' ? 'broadcast' : message.recipient}
                      </Badge>
                      <Badge variant="secondary" className="text-[9px] h-4 px-1.5 uppercase">
                        {message.kind}
                      </Badge>
                      <Badge variant="outline" className="text-[9px] h-4 px-1.5 uppercase">
                        {message.status || 'stored'}
                      </Badge>
                      <Badge variant="outline" className="text-[9px] h-4 px-1.5 border-emerald-500/30 text-emerald-700">
                        stored
                      </Badge>
                      {meta?.awaitingAck && (
                        <Badge variant="outline" className="text-[9px] h-4 px-1.5 border-amber-500/40 text-amber-700">
                          awaiting ack
                        </Badge>
                      )}
                      {meta?.awaitingReply && (
                        <Badge variant="outline" className="text-[9px] h-4 px-1.5 border-purple-500/40 text-purple-700">
                          awaiting reply
                        </Badge>
                      )}
                      {meta?.resolved && (
                        <Badge variant="outline" className="text-[9px] h-4 px-1.5 border-emerald-500/40 text-emerald-700">
                          resolved
                        </Badge>
                      )}
                      {message.ack_required && (
                        <Badge variant="outline" className="text-[9px] h-4 px-1.5 border-amber-500/40 text-amber-700">
                          ack
                        </Badge>
                      )}
                      {message.reply_expected && (
                        <Badge variant="outline" className="text-[9px] h-4 px-1.5 border-purple-500/40 text-purple-700">
                          reply
                        </Badge>
                      )}
                      {message.interrupt && (
                        <Badge variant="outline" className="text-[9px] h-4 px-1.5 border-red-500/40 text-red-700">
                          interrupt
                        </Badge>
                      )}
                    </div>
                    {message.subject && message.subject !== message.body && (
                      <div className="mt-1 text-[11px] font-semibold text-foreground/90">{message.subject}</div>
                    )}
                  </div>
                  <div className="text-[10px] text-muted-foreground font-mono shrink-0">
                    {formatRelativeTime(message.created_at)}
                  </div>
                </div>

                <div className="mt-2 whitespace-pre-wrap text-[12px] leading-relaxed text-foreground/85">
                  {message.body}
                </div>

                <div className="mt-3 flex flex-wrap items-center gap-2 text-[10px] text-muted-foreground">
                  <span>{message.recipient === '*' ? `broadcast audience ${meta?.audienceCount ?? 0}` : 'direct delivery target'}</span>
                  {meta?.replies.length ? <span>{meta.replies.length} follow-up{meta.replies.length === 1 ? '' : 's'}</span> : null}
                  {meta?.events.phases.length ? <span>events: {meta.events.phases.join(', ')}</span> : null}
                  {typeof meta?.events.dispatched === 'number' ? (
                    <span>
                      agent dispatch {meta.events.dispatched}
                      {typeof meta.events.skipped === 'number' ? `/${meta.events.dispatched + meta.events.skipped}` : ''}
                    </span>
                  ) : null}
                  {meta?.events.errors[0] ? <span className="text-red-700">event error: {meta.events.errors[0]}</span> : null}
                </div>

                {latestReply && (
                  <div className="mt-3 rounded-xl border border-border/70 bg-muted/40 px-3 py-2">
                    <div className="flex items-center justify-between gap-3">
                      <div className="text-[10px] font-black uppercase tracking-widest text-muted-foreground">
                        Latest Follow-Up
                      </div>
                      <div className="text-[10px] text-muted-foreground font-mono">
                        {latestReply.sender} · {formatRelativeTime(latestReply.created_at)}
                      </div>
                    </div>
                    <div className="mt-1 whitespace-pre-wrap text-[11px] leading-relaxed text-foreground/80">
                      {latestReply.body}
                    </div>
                  </div>
                )}
              </div>
            )
          })}

          {filtered.length === 0 && (
            <div className="p-12 text-center text-sm text-muted-foreground italic">
              No messages match the current chat filter.
            </div>
          )}
        </div>
      </ScrollArea>
    </div>
  )
}

function ChatFilterButton({ label, active, onClick }: { label: string; active: boolean; onClick: () => void }) {
  return (
    <Button
      variant={active ? 'secondary' : 'ghost'}
      size="xs"
      onClick={onClick}
      className={cn('h-6 px-2 text-[9px] font-black uppercase tracking-tight', active && 'bg-primary/10 text-primary')}
    >
      {label}
    </Button>
  )
}
