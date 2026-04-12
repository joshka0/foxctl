import type { Room, RoomDeliveryBinding, RoomMember } from '@/api/types'

export function roomDisplayName(room: Room): string {
  const title = room.title?.trim()
  if (title) return title
  return room.id
}

export function roomActors(room: Room): string[] {
  const explicit = (room.members ?? [])
    .map((member) => (member.actor_id || '').trim())
    .filter((actorID) => actorID.length > 0)
  if (explicit.length > 0) return explicit
  return (room.participants ?? [])
    .map((participant) => participant.trim())
    .filter((participant) => participant.length > 0)
}

export function indexRoomsByActor(rooms: Room[]): Map<string, Room[]> {
  const out = new Map<string, Room[]>()
  for (const room of rooms) {
    for (const actorID of roomActors(room)) {
      const existing = out.get(actorID)
      if (existing) {
        existing.push(room)
      } else {
        out.set(actorID, [room])
      }
    }
  }
  return out
}

export function humanReadableWorkspacePath(workspace: string): string {
  const trimmed = (workspace || '').trim()
  if (!trimmed) return 'unscoped'
  if (trimmed === '/') return '/'
  return trimmed.replace(/^\/Users\/joshka/, '~')
}

export function isPathWorkspace(workspace: string | null | undefined): boolean {
  return !!workspace && workspace.trim().startsWith('/')
}

export function resolveRoomWorkspacePath(
  candidate: string | null | undefined,
  knownWorkspacePaths: string[],
  currentWorkspace?: string | null,
): string {
  const trimmed = (candidate || '').trim()
  if (isPathWorkspace(trimmed)) return trimmed

  const normalizedKnown = knownWorkspacePaths
    .map((path) => path.trim())
    .filter((path) => path.length > 0)

  if (normalizedKnown.length === 1) return normalizedKnown[0]

  const current = (currentWorkspace || '').trim()
  if (isPathWorkspace(current)) return current

  return ''
}

export function roomMemberDeliveryBinding(member: RoomMember | null | undefined): RoomDeliveryBinding | undefined {
  if (!member) return undefined
  if (member.delivery_binding) return member.delivery_binding
  if (
    !member.backend &&
    !member.session &&
    !member.pane_id &&
    !member.transport_endpoint &&
    !member.transport_kind
  ) {
    return undefined
  }
  return {
    mux_backend: member.backend,
    mux_session: member.session,
    mux_pane_id: member.pane_id,
    transport_endpoint: member.transport_endpoint,
    transport_kind: member.transport_kind,
  }
}

export function roomMemberMuxBackend(member: RoomMember | null | undefined): string {
  return roomMemberDeliveryBinding(member)?.mux_backend?.trim() || member?.backend?.trim() || ''
}

export function roomMemberMuxSession(member: RoomMember | null | undefined): string {
  return roomMemberDeliveryBinding(member)?.mux_session?.trim() || member?.session?.trim() || ''
}

export function roomMemberMuxPaneID(member: RoomMember | null | undefined): string {
  return roomMemberDeliveryBinding(member)?.mux_pane_id?.trim() || member?.pane_id?.trim() || ''
}

export function roomMemberTransportEndpoint(member: RoomMember | null | undefined): string {
  return roomMemberDeliveryBinding(member)?.transport_endpoint?.trim() || member?.transport_endpoint?.trim() || ''
}

export function roomMemberTransportKind(member: RoomMember | null | undefined): string {
  return roomMemberDeliveryBinding(member)?.transport_kind?.trim() || member?.transport_kind?.trim() || ''
}
