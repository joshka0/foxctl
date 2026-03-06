import { useEffect, useMemo, useRef, useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Card } from '@/components/ui/card'
import { Input } from '@/components/ui/input'
import { ScrollArea } from '@/components/ui/scroll-area'
import { Textarea } from '@/components/ui/textarea'
import {
  createRoom,
  listAgents,
  listRoomMessages,
  listRooms,
  listWorkspaces,
  patchRoom,
  patchRoomMembers,
  sendRoomMessage,
} from '@/api/client'
import type { Room, RoomMessageEvent } from '@/api/types'
import { cn, formatRelativeTime } from '@/lib/utils'
import { humanReadableWorkspacePath, isPathWorkspace, roomDisplayName } from '@/lib/room-utils'
import { useViewStore } from '@/stores/viewStore'
import { Hash, RefreshCw, SendHorizonal } from 'lucide-react'

const DEFAULT_SENDER = 'gui-agent'

function roomTimestamp(room: Room): number {
  return Date.parse(room.latest_message_at || '') || 0
}

export function RoomsView() {
  const queryClient = useQueryClient()
  const eventSourceRef = useRef<EventSource | null>(null)
  const selectedAgent = useViewStore((s) => s.selectedAgent)
  const setSelectedAgent = useViewStore((s) => s.setSelectedAgent)
  const setActiveView = useViewStore((s) => s.setActiveView)
  const selectedRoomID = useViewStore((s) => s.selectedRoomID)
  const selectedRoomWorkspaceID = useViewStore((s) => s.selectedRoomWorkspaceID)
  const setSelectedRoom = useViewStore((s) => s.setSelectedRoom)
  const [workspaceID, setWorkspaceID] = useState('')
  const [pendingRoomID, setPendingRoomID] = useState('')
  const [roomTitle, setRoomTitle] = useState('')
  const [roomDescription, setRoomDescription] = useState('')
  const [memberText, setMemberText] = useState('')
  const [draft, setDraft] = useState('')
  const [roomError, setRoomError] = useState<string | null>(null)
  const [sendError, setSendError] = useState<string | null>(null)

  const { data: agentsData } = useQuery({
    queryKey: ['agents'],
    queryFn: () => listAgents(100),
    staleTime: 10000,
  })
  const { data: workspacesData } = useQuery({
    queryKey: ['workspaces'],
    queryFn: listWorkspaces,
    staleTime: 10000,
  })

  const derivedWorkspaceID = useMemo(() => {
    const isPathLike = (value: string) => value.startsWith('/')
    const fromSelectedRoom = selectedRoomWorkspaceID?.trim()
    if (fromSelectedRoom && isPathLike(fromSelectedRoom)) return fromSelectedRoom

    const fromSelected = selectedAgent?.ns?.trim()
    if (fromSelected && isPathLike(fromSelected)) return fromSelected

    const fromAgents = (agentsData?.agents ?? [])
      .map((agent) => (agent.ns || '').trim())
      .filter((ns) => isPathLike(ns))
      .find((ns) => ns.length > 0)
    return fromAgents || ''
  }, [agentsData?.agents, selectedAgent?.ns, selectedRoomWorkspaceID])

  const workspaceOptions = useMemo(() => {
    const seen = new Set<string>()
    const ordered: string[] = []
    const preferred = [selectedRoomWorkspaceID, workspaceID, workspacesData?.current, derivedWorkspaceID]
      .map((value) => (value || '').trim())
      .filter((value) => isPathWorkspace(value))
    for (const value of preferred) {
      if (seen.has(value)) continue
      seen.add(value)
      ordered.push(value)
    }
    for (const workspace of workspacesData?.workspaces ?? []) {
      const path = (workspace.path || '').trim()
      if (!isPathWorkspace(path) || seen.has(path)) continue
      seen.add(path)
      ordered.push(path)
    }
    return ordered
  }, [derivedWorkspaceID, selectedRoomWorkspaceID, workspaceID, workspacesData?.current, workspacesData?.workspaces])

  const workspaceLabel = (workspace: string): string => {
    const trimmed = workspace.trim()
    if (!trimmed) return 'unscoped'
    const match = (workspacesData?.workspaces ?? []).find((entry) => entry.path.trim() === trimmed)
    if (match) {
      return `${match.name} — ${humanReadableWorkspacePath(match.path)}`
    }
    return humanReadableWorkspacePath(trimmed)
  }

  const agentsByID = useMemo(() => {
    const out = new Map<string, NonNullable<typeof agentsData>['agents'][number]>()
    for (const agent of agentsData?.agents ?? []) {
      out.set(agent.id, agent)
    }
    return out
  }, [agentsData?.agents])

  useEffect(() => {
    const selectedWorkspace = (selectedRoomWorkspaceID || '').trim()
    if (selectedRoomID && isPathWorkspace(selectedWorkspace) && selectedWorkspace !== workspaceID.trim()) {
      setWorkspaceID(selectedWorkspace)
      return
    }
    if (workspaceID.trim().length > 0) return
    if (isPathWorkspace(selectedWorkspace)) {
      setWorkspaceID(selectedWorkspace)
      return
    }
    const currentWorkspace = (workspacesData?.current || '').trim()
    if (isPathWorkspace(currentWorkspace)) {
      setWorkspaceID(currentWorkspace)
      return
    }
    if (derivedWorkspaceID) {
      setWorkspaceID(derivedWorkspaceID)
    }
  }, [derivedWorkspaceID, selectedRoomID, selectedRoomWorkspaceID, workspaceID, workspacesData?.current])

  useEffect(() => {
    if (workspaceID.trim().length > 0) return
    if (workspaceOptions[0]) {
      setWorkspaceID(workspaceOptions[0])
    }
  }, [workspaceID, workspaceOptions])

  const roomsQuery = useQuery({
    queryKey: ['rooms', workspaceID],
    enabled: workspaceID.trim().length > 0,
    retry: false,
    queryFn: () => listRooms({ workspace_id: workspaceID.trim(), limit: 100 }),
    staleTime: 5000,
  })

  const rooms = useMemo(
    () => [...(roomsQuery.data?.rooms ?? [])].sort((a, b) => roomTimestamp(b) - roomTimestamp(a)),
    [roomsQuery.data?.rooms],
  )

  useEffect(() => {
    if (rooms.length === 0) return
    if (!selectedRoomID || !rooms.some((room) => room.id === selectedRoomID)) {
      setSelectedRoom(rooms[0].id, rooms[0].workspace_id)
    }
  }, [rooms, selectedRoomID, setSelectedRoom])

  const selectedRoom = useMemo(
    () => rooms.find((room) => room.id === selectedRoomID) ?? null,
    [rooms, selectedRoomID],
  )

  useEffect(() => {
    const activeRoomID = (selectedRoom?.id || selectedRoomID || pendingRoomID).trim()
    if (selectedRoom) {
      setRoomTitle(selectedRoom.title || selectedRoom.id)
      setRoomDescription(selectedRoom.description || '')
      setMemberText(formatMembersText(selectedRoom.members || []))
      setPendingRoomID(activeRoomID)
      return
    }
    if (activeRoomID) {
      setRoomTitle(activeRoomID)
      setRoomDescription('')
      setMemberText('')
    }
  }, [pendingRoomID, selectedRoom, selectedRoomID])

  const roomMessagesQuery = useQuery({
    queryKey: ['room-messages', workspaceID, selectedRoomID],
    enabled: workspaceID.trim().length > 0 && selectedRoomID !== null,
    retry: false,
    queryFn: () =>
      listRoomMessages(selectedRoomID as string, {
        workspace_id: workspaceID.trim(),
        limit: 200,
      }),
  })

  const sendMutation = useMutation({
    mutationFn: async () => {
      const roomID = (selectedRoomID || pendingRoomID).trim()
      if (!roomID) {
        throw new Error('Room ID is required')
      }
      if (!draft.trim()) {
        throw new Error('Message body is required')
      }
      return sendRoomMessage(roomID, {
        workspace_id: workspaceID.trim(),
        sender: DEFAULT_SENDER,
        body: draft.trim(),
      })
    },
    onSuccess: async (result) => {
      setSendError(null)
      setDraft('')
      setSelectedRoom(result.room_id, workspaceID.trim())
      setPendingRoomID('')
      await Promise.all([
        queryClient.invalidateQueries({ queryKey: ['rooms', workspaceID] }),
        queryClient.invalidateQueries({ queryKey: ['room-messages', workspaceID, result.room_id] }),
      ])
    },
    onError: (error) => {
      setSendError(error instanceof Error ? error.message : 'Failed to send room message')
    },
  })

  const saveRoomMutation = useMutation({
    mutationFn: async () => {
      const roomID = (selectedRoomID || pendingRoomID).trim()
      if (!workspaceID.trim()) throw new Error('Workspace is required')
      if (!roomID) throw new Error('Room ID is required')
      const title = roomTitle.trim() || roomID
      const description = roomDescription.trim()
      const members = parseMembersText(memberText)

      if (selectedRoom?.id === roomID) {
        await patchRoom(roomID, {
          workspace_id: workspaceID.trim(),
          title,
          description,
        })
        return patchRoomMembers(roomID, {
          workspace_id: workspaceID.trim(),
          members,
        })
      }

      return createRoom({
        workspace_id: workspaceID.trim(),
        id: roomID,
        title,
        description,
        members,
      })
    },
    onSuccess: async (result) => {
      setRoomError(null)
      if (result?.room?.id) {
        setSelectedRoom(result.room.id, result.room.workspace_id || workspaceID.trim())
      }
      await Promise.all([
        queryClient.invalidateQueries({ queryKey: ['rooms', workspaceID] }),
        queryClient.invalidateQueries({ queryKey: ['room-messages', workspaceID, selectedRoomID] }),
      ])
    },
    onError: (error) => {
      setRoomError(error instanceof Error ? error.message : 'Failed to save room')
    },
  })

  const transcript = roomMessagesQuery.data?.messages ?? []

  const openAgent = (actorID: string) => {
    const target = agentsByID.get(actorID)
    if (!target) return
    setSelectedAgent(target)
    setActiveView('runtime')
  }

  useEffect(() => {
    if (typeof window === 'undefined') return

    const eventSource = new EventSource('/api/events')
    eventSourceRef.current = eventSource
    eventSource.onmessage = (rawEvent) => {
      let parsed: { type?: string; data?: unknown } | null = null
      try {
        parsed = JSON.parse(rawEvent.data) as { type?: string; data?: unknown }
      } catch {
        return
      }
      if (parsed?.type !== 'room.message' || !parsed.data || typeof parsed.data !== 'object') {
        return
      }
      const event = parsed.data as RoomMessageEvent
      if (event.workspace_id !== workspaceID.trim()) {
        return
      }
      if (
        event.phase !== 'sent' &&
        event.phase !== 'agent_completed' &&
        event.phase !== 'agent_error'
      ) {
        return
      }
      void queryClient.invalidateQueries({ queryKey: ['rooms', workspaceID.trim()] })
      if (selectedRoomID && event.room_id === selectedRoomID) {
        void queryClient.invalidateQueries({ queryKey: ['room-messages', workspaceID.trim(), selectedRoomID] })
      }
    }
    return () => {
      eventSource.close()
      if (eventSourceRef.current === eventSource) {
        eventSourceRef.current = null
      }
    }
  }, [queryClient, selectedRoomID, workspaceID])

  return (
    <div className="flex h-full min-h-0 bg-background">
      <div className="w-[320px] min-w-[280px] border-r border-border bg-muted/10 flex flex-col">
        <div className="border-b border-border px-4 py-3 space-y-3">
          <div className="flex items-center justify-between gap-2">
            <div>
              <div className="text-sm font-medium text-foreground">Rooms</div>
              <div className="text-xs text-muted-foreground">Derived from `room:*` board streams</div>
            </div>
            <Button
              variant="ghost"
              size="icon"
              onClick={() => roomsQuery.refetch()}
              disabled={roomsQuery.isFetching || workspaceID.trim().length === 0}
            >
              <RefreshCw className={cn('h-4 w-4', roomsQuery.isFetching && 'animate-spin')} />
            </Button>
          </div>

          <div className="space-y-2">
            <label className="text-[11px] uppercase tracking-wide text-muted-foreground">Workspace</label>
            {workspaceOptions.length > 0 ? (
              <select
                value={workspaceID}
                onChange={(e) => setWorkspaceID(e.target.value)}
                className="h-9 w-full rounded-md border border-input bg-background px-3 text-sm font-mono"
                title={workspaceID || 'Select workspace'}
              >
                <option value="">Select workspace</option>
                {workspaceOptions.map((workspace) => (
                  <option key={workspace} value={workspace}>
                    {workspaceLabel(workspace)}
                  </option>
                ))}
              </select>
            ) : (
              <Input
                value={workspaceID}
                onChange={(e) => setWorkspaceID(e.target.value)}
                placeholder="workspace path"
                className="h-9"
              />
            )}
          </div>

          <div className="space-y-2">
            <label className="text-[11px] uppercase tracking-wide text-muted-foreground">Open Or Create Room</label>
            <div className="flex gap-2">
              <Input
                value={pendingRoomID}
                onChange={(e) => setPendingRoomID(e.target.value)}
                placeholder="room id"
                className="h-9"
              />
              <Button
                variant="secondary"
                onClick={() => {
                  const nextID = pendingRoomID.trim()
                  if (!nextID) return
                  setSelectedRoom(nextID, workspaceID.trim() || selectedRoomWorkspaceID)
                  setSendError(null)
                }}
              >
                Open
              </Button>
            </div>
          </div>
        </div>

        <ScrollArea className="flex-1">
          <div className="p-3 space-y-2">
            {rooms.length === 0 && (
              <Card className="border-dashed bg-background/70 p-4 text-sm text-muted-foreground">
                {workspaceID.trim().length === 0
                  ? 'Set a workspace to load rooms.'
                  : 'No room streams found yet. Open a room ID and send the first message.'}
              </Card>
            )}
            {rooms.map((room) => {
              const active = room.id === selectedRoomID
              return (
                <button
                  key={room.id}
                  type="button"
                  onClick={() => {
                    setSelectedRoom(room.id, room.workspace_id)
                    setSendError(null)
                  }}
                  className={cn(
                    'w-full rounded-lg border px-3 py-3 text-left transition-colors',
                    active ? 'border-primary bg-primary/5' : 'border-border bg-background hover:bg-accent/40',
                  )}
                >
                  <div className="flex items-center justify-between gap-2">
                    <div className="flex min-w-0 items-center gap-2">
                      <Hash className="h-4 w-4 text-muted-foreground" />
                      <span className="truncate text-sm font-medium text-foreground">{roomDisplayName(room)}</span>
                    </div>
                    <Badge variant="secondary" className="text-[10px]">
                      {room.message_count}
                    </Badge>
                  </div>
                  <div className="mt-2 text-xs text-muted-foreground line-clamp-2">
                    {room.latest_preview || 'No preview'}
                  </div>
                  <div className="mt-2 flex items-center justify-between text-[11px] text-muted-foreground">
                    <span>{room.latest_sender || 'unknown sender'}</span>
                    <span>{room.latest_message_at ? formatRelativeTime(room.latest_message_at) : '—'}</span>
                  </div>
                </button>
              )
            })}
          </div>
        </ScrollArea>
      </div>

      <div className="flex-1 min-w-0 flex flex-col">
        <div className="border-b border-border px-5 py-4">
          <div className="flex flex-wrap items-center gap-3">
            <div>
              <div className="text-lg font-semibold text-foreground">
                {selectedRoom ? roomDisplayName(selectedRoom) : selectedRoomID || 'No room selected'}
              </div>
              <div className="text-sm text-muted-foreground">
                {selectedRoom ? selectedRoom.stream : 'Open a room or select one from the list'}
              </div>
            </div>
            {selectedRoom && (
              <>
                <Badge variant="secondary">{selectedRoom.message_count} messages</Badge>
                <Badge variant="outline">{selectedRoom.unread_count} unread</Badge>
                {selectedRoom.participants?.length ? (
                  <Badge variant="outline">{selectedRoom.participants.length} participants</Badge>
                ) : null}
              </>
            )}
          </div>
          {selectedRoom?.participants?.length ? (
            <div className="mt-3 flex flex-wrap gap-2">
              {selectedRoom.participants.map((participant) => (
                <button
                  key={participant}
                  type="button"
                  onClick={() => openAgent(participant)}
                  disabled={!agentsByID.has(participant)}
                  className="disabled:cursor-default"
                >
                  <Badge variant="outline" className="font-mono text-[10px]">
                    {participant}
                  </Badge>
                </button>
              ))}
            </div>
          ) : null}
          <div className="mt-4 grid gap-3 md:grid-cols-2">
            <div className="space-y-2">
              <label className="text-[11px] uppercase tracking-wide text-muted-foreground">Room Title</label>
              <Input
                value={roomTitle}
                onChange={(e) => setRoomTitle(e.target.value)}
                placeholder="Room title"
              />
            </div>
            <div className="space-y-2">
              <label className="text-[11px] uppercase tracking-wide text-muted-foreground">Members</label>
              <Input
                value={memberText}
                onChange={(e) => setMemberText(e.target.value)}
                placeholder="actor:agent:a:role=owner, actor:agent:b"
              />
            </div>
          </div>
          <div className="mt-3 space-y-2">
            <label className="text-[11px] uppercase tracking-wide text-muted-foreground">Description</label>
            <Textarea
              value={roomDescription}
              onChange={(e) => setRoomDescription(e.target.value)}
              rows={2}
              placeholder="What this room is for"
            />
          </div>
          <div className="mt-3 flex items-center justify-between gap-3">
            {roomError ? <div className="text-sm text-red-400">{roomError}</div> : <div />}
            <Button
              variant="secondary"
              onClick={() => saveRoomMutation.mutate()}
              disabled={saveRoomMutation.isPending || workspaceID.trim().length === 0 || (selectedRoomID || pendingRoomID).trim().length === 0}
            >
              {selectedRoom ? 'Update Room' : 'Create Room'}
            </Button>
          </div>
        </div>

        <ScrollArea className="flex-1">
          <div className="p-5 space-y-3">
            {!selectedRoomID && (
              <Card className="border-dashed bg-background/70 p-6 text-sm text-muted-foreground">
                Select a room from the left, or open a new room ID to start a transcript.
              </Card>
            )}
            {selectedRoomID && roomMessagesQuery.isLoading && (
              <Card className="border-dashed bg-background/70 p-6 text-sm text-muted-foreground">
                Loading room transcript…
              </Card>
            )}
            {selectedRoomID && !roomMessagesQuery.isLoading && transcript.length === 0 && (
              <Card className="border-dashed bg-background/70 p-6 text-sm text-muted-foreground">
                This room has no messages yet. Send the first one below.
              </Card>
            )}
            {transcript.map((message) => {
              const mine = message.sender === DEFAULT_SENDER
              return (
                <div
                  key={message.id}
                  className={cn('flex', mine ? 'justify-end' : 'justify-start')}
                >
                  <Card className={cn('max-w-[720px] px-4 py-3', mine ? 'bg-primary/5 border-primary/20' : 'bg-background')}>
                    <div className="flex flex-wrap items-center gap-2 text-xs text-muted-foreground">
                      <Badge variant="outline" className="font-mono text-[10px]">
                        {message.sender}
                      </Badge>
                      <span>{message.created_at ? formatRelativeTime(message.created_at) : '—'}</span>
                      {message.status ? (
                        <Badge variant="secondary" className="text-[10px]">
                          {message.status}
                        </Badge>
                      ) : null}
                    </div>
                    {message.subject ? (
                      <div className="mt-2 text-sm font-medium text-foreground">{message.subject}</div>
                    ) : null}
                    <div className="mt-2 whitespace-pre-wrap text-sm text-foreground">{message.body}</div>
                  </Card>
                </div>
              )
            })}
          </div>
        </ScrollArea>

        <div className="border-t border-border px-5 py-4 space-y-3">
          {sendError && (
            <div className="text-sm text-red-400">{sendError}</div>
          )}
          <Textarea
            value={draft}
            onChange={(e) => setDraft(e.target.value)}
            placeholder={selectedRoomID ? `Send to room:${selectedRoomID}` : 'Open or create a room, then type a message'}
            rows={4}
          />
          <div className="flex items-center justify-between gap-3">
            <div className="text-xs text-muted-foreground">
              Sender <code>{DEFAULT_SENDER}</code>
            </div>
            <Button
              onClick={() => sendMutation.mutate()}
              disabled={sendMutation.isPending || workspaceID.trim().length === 0 || (selectedRoomID || pendingRoomID).trim().length === 0}
            >
              <SendHorizonal className="mr-2 h-4 w-4" />
              Send
            </Button>
          </div>
        </div>
      </div>
    </div>
  )
}

function parseMembersText(raw: string): Array<{ actor_id: string; role?: string }> {
  return raw
    .split(',')
    .map((entry) => entry.trim())
    .filter((entry) => entry.length > 0)
    .map((entry) => {
      const [actorID, role] = entry.split(':role=')
      const actor = actorID.trim()
      const nextRole = (role || '').trim()
      return nextRole ? { actor_id: actor, role: nextRole } : { actor_id: actor }
    })
    .filter((entry) => entry.actor_id.length > 0)
}

function formatMembersText(members: Room['members']): string {
  return (members || [])
    .map((member) => {
      const actorID = (member?.actor_id || '').trim()
      const role = (member?.role || '').trim()
      if (!actorID) return ''
      return role ? `${actorID}:role=${role}` : actorID
    })
    .filter((entry) => entry.length > 0)
    .join(', ')
}
