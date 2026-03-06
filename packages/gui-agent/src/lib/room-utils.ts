import type { Room } from '@/api/types'

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
