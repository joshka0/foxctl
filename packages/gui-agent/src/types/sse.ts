import type {
  AgentChatStreamEvent,
  LeadChangeEvent,
  MailboxMessage,
  RoomMessageEvent,
} from '@foxctl/data/types'

export interface SSEEnvelope<T = unknown> {
  type?: string
  data?: T
  ts?: string
}

export type TimelineMessageEvent = MailboxMessage & Partial<LeadChangeEvent>

export function parseSSEEnvelope(raw: string): SSEEnvelope | null {
  try {
    const parsed: unknown = JSON.parse(raw)
    return isRecord(parsed) ? parsed : null
  } catch {
    return null
  }
}

export function isRoomMessageEvent(value: unknown): value is RoomMessageEvent {
  const record = asRecord(value)
  return (
    !!record &&
    typeof record.workspace_id === 'string' &&
    typeof record.room_id === 'string' &&
    typeof record.stream === 'string'
  )
}

export function isAgentChatStreamEvent(value: unknown): value is AgentChatStreamEvent {
  const record = asRecord(value)
  return (
    !!record &&
    typeof record.agent_id === 'string' &&
    typeof record.conversation_id === 'string' &&
    typeof record.correlation_id === 'string' &&
    typeof record.phase === 'string'
  )
}

export function isTimelineLeadChangeEvent(event: TimelineMessageEvent): event is TimelineMessageEvent & LeadChangeEvent {
  return (
    event.kind === 'lead_change' &&
    typeof event.previous_lead === 'string' &&
    typeof event.new_lead === 'string'
  )
}

function asRecord(value: unknown): Record<string, unknown> | null {
  return isRecord(value) ? value : null
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === 'object' && value !== null && !Array.isArray(value)
}
