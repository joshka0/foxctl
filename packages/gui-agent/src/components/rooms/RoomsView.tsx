import { useEffect, useMemo, useState } from 'react'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Card } from '@/components/ui/card'
import { Input } from '@/components/ui/input'
import { ScrollArea } from '@/components/ui/scroll-area'
import { Tooltip, HelpTooltip } from '@/components/ui/tooltip'
import { archiveRoom, listRooms, listWorkspaces, restoreRoom } from '@/api/client'
import { useViewStore } from '@/stores/viewStore'
import { cn, formatRelativeTime } from '@/lib/utils'
import { ArchiveRestore, Hash, RefreshCw } from 'lucide-react'
import type { Room } from '@foxctl/data/types'
import { RoomControlCenter } from './RoomControlCenter'

export function RoomsView() {
  const queryClient = useQueryClient()
  const { selectedRoomID, selectedRoomWorkspaceID, setSelectedRoom } = useViewStore()
  const [pendingRoomID, setPendingRoomID] = useState('')
  const [showArchived, setShowArchived] = useState(false)
  const [bulkArchiveLoading, setBulkArchiveLoading] = useState(false)

  const workspacesQuery = useQuery({
    queryKey: ['workspaces'],
    queryFn: listWorkspaces,
    staleTime: 10000,
  })

  const workspaceOptions = useMemo(
    () =>
      [...new Set((workspacesQuery.data?.workspaces ?? []).map((workspace) => workspace.path.trim()).filter(Boolean))].sort(),
    [workspacesQuery.data?.workspaces],
  )

  const workspaceID = useMemo(() => {
    const selected = (selectedRoomWorkspaceID || '').trim()
    if (selected) return selected
    return workspaceOptions[0] || ''
  }, [selectedRoomWorkspaceID, workspaceOptions])

  const roomsQuery = useQuery({
    queryKey: ['rooms', workspaceID, showArchived],
    enabled: workspaceID.trim().length > 0,
    queryFn: () => listRooms({ workspace_id: workspaceID.trim(), limit: 100, archived_only: showArchived }),
  })

  const rooms = useMemo(() => roomsQuery.data?.rooms ?? [], [roomsQuery.data?.rooms])

  useEffect(() => {
    if (rooms.length === 0) return
    if (!selectedRoomID || !rooms.some((room) => room.id === selectedRoomID)) {
      setSelectedRoom(rooms[0].id, rooms[0].workspace_id)
    }
  }, [rooms, selectedRoomID, setSelectedRoom])

  const workspaceLabel = (path: string) => {
    if (!path) return 'none'
    const parts = path.split('/')
    return parts[parts.length - 1] || path
  }

  const roomDisplayName = (room: Room) => room.title || room.id

  const handleArchiveActionForVisibleRooms = async () => {
    if (rooms.length === 0 || workspaceID.trim().length === 0) return
    if (
      !window.confirm(
        `${showArchived ? 'Restore' : 'Archive'} ${rooms.length} visible room${rooms.length === 1 ? '' : 's'} in workspace "${workspaceLabel(workspaceID)}"?`,
      )
    ) {
      return
    }

    setBulkArchiveLoading(true)
    try {
      const results = await Promise.allSettled(
        rooms.map((room) =>
          showArchived
            ? restoreRoom(room.id, { workspace_id: workspaceID.trim() })
            : archiveRoom(room.id, { workspace_id: workspaceID.trim() }),
        ),
      )
      const failures = results.filter(
        (result): result is PromiseRejectedResult => result.status === 'rejected',
      )
      await queryClient.invalidateQueries({ queryKey: ['rooms', workspaceID.trim()] })
      if (selectedRoomID && rooms.some((room) => room.id === selectedRoomID)) {
        setSelectedRoom(null, workspaceID.trim())
      }
      if (failures.length > 0) {
        alert(
          `${showArchived ? 'Restored' : 'Archived'} ${rooms.length - failures.length} room${rooms.length - failures.length === 1 ? '' : 's'}, ${failures.length} failed.`,
        )
      }
    } finally {
      setBulkArchiveLoading(false)
    }
  }

  return (
    <div className="flex h-full min-h-0 bg-background">
      <div className="w-[320px] min-w-[280px] border-r border-border bg-muted/10 flex flex-col">
        <div className="border-b border-border px-4 py-3 space-y-3">
          <div className="flex items-center justify-between gap-2">
            <div>
              <div className="flex items-center gap-1.5">
                <div className="text-sm font-medium text-foreground">Rooms</div>
                <HelpTooltip
                  side="bottom"
                  content="Rooms are shared work threads for agents. Use them to organize conversations, dispatch work, and keep related messages together."
                />
              </div>
              <div className="text-xs text-muted-foreground">Derived from room streams</div>
            </div>
            <Tooltip content="Reload the room list for the selected workspace.">
              <Button
                variant="ghost"
                size="icon"
                onClick={() => roomsQuery.refetch()}
                disabled={roomsQuery.isFetching || workspaceID.trim().length === 0}
              >
                <RefreshCw className={cn('h-4 w-4', roomsQuery.isFetching && 'animate-spin')} />
              </Button>
            </Tooltip>
            <Tooltip content={showArchived ? 'Return archived rooms to the active list.' : 'Move visible rooms out of the active list without deleting their history.'}>
              <Button
                variant="ghost"
                size="icon"
                onClick={() => void handleArchiveActionForVisibleRooms()}
                disabled={bulkArchiveLoading || rooms.length === 0 || workspaceID.trim().length === 0}
              >
                <ArchiveRestore className={cn('h-4 w-4', bulkArchiveLoading && 'animate-pulse')} />
              </Button>
            </Tooltip>
          </div>

          <div className="space-y-2">
            <div className="text-[11px] uppercase tracking-wide text-muted-foreground inline-flex items-center gap-1">
              <span>Workspace</span>
              <HelpTooltip side="top" content="Choose which workspace's rooms you want to inspect." />
            </div>
            {workspaceOptions.length > 0 ? (
              <Tooltip content={workspaceID || 'Select a workspace to load rooms.'}>
                <select
                  value={workspaceID}
                  onChange={(e) => setSelectedRoom(null, e.target.value)}
                  className="h-9 w-full rounded-md border border-input bg-background px-3 text-sm font-mono"
                >
                  <option value="">Select workspace</option>
                  {workspaceOptions.map((workspace: string) => (
                    <option key={workspace} value={workspace}>
                      {workspaceLabel(workspace)}
                    </option>
                  ))}
                </select>
              </Tooltip>
            ) : (
              <Input
                value={workspaceID}
                onChange={(e) => setSelectedRoom(null, e.target.value)}
                placeholder="workspace path"
                className="h-9"
              />
            )}
          </div>

          <div className="space-y-2">
            <div className="text-[11px] uppercase tracking-wide text-muted-foreground inline-flex items-center gap-1">
              <span>Open Room</span>
              <HelpTooltip side="top" content="Enter a room ID to open an existing room in this workspace." />
            </div>
            <div className="flex gap-2">
              <Input
                value={pendingRoomID}
                onChange={(e) => setPendingRoomID(e.target.value)}
                placeholder="room id"
                className="h-9"
              />
              <Tooltip content="Open this room if it already exists, or keep it selected while the first message creates it.">
                <Button
                  variant="secondary"
                  onClick={() => {
                    const nextID = pendingRoomID.trim()
                    if (!nextID) return
                    setSelectedRoom(nextID, workspaceID.trim() || selectedRoomWorkspaceID)
                  }}
                >
                  Open
                </Button>
              </Tooltip>
            </div>
          </div>
          {rooms.length > 0 && (
            <div className="flex items-center justify-between text-[11px] text-muted-foreground">
              <span>{rooms.length} {showArchived ? 'archived' : 'visible'}</span>
              <Button
                variant={showArchived ? 'secondary' : 'outline'}
                size="sm"
                className="h-7 text-[11px]"
                onClick={() => setShowArchived((value) => !value)}
              >
                {showArchived ? 'Show Active' : 'Show Archived'}
              </Button>
              <Button
                variant="outline"
                size="sm"
                className="h-7 text-[11px]"
                onClick={() => void handleArchiveActionForVisibleRooms()}
                disabled={bulkArchiveLoading}
              >
                <ArchiveRestore className="h-3.5 w-3.5" />
                {showArchived ? 'Recover Visible Rooms' : 'Archive Visible Rooms'}
              </Button>
            </div>
          )}
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
                  onClick={() => setSelectedRoom(room.id, room.workspace_id)}
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
                    {room.archived_at ? (
                      <Badge variant="secondary" className="h-4 px-1.5 min-w-[1rem] text-[10px]">
                        archived
                      </Badge>
                    ) : room.unread_count > 0 && (
                      <Badge variant="default" className="h-4 px-1.5 min-w-[1rem] text-[10px]">
                        {room.unread_count}
                      </Badge>
                    )}
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
        {selectedRoomID ? (
          <RoomControlCenter roomId={selectedRoomID} />
        ) : (
          <div className="flex-1 flex flex-col items-center justify-center text-muted-foreground bg-muted/5">
            <div className="w-16 h-16 rounded-full bg-muted/20 flex items-center justify-center mb-4 text-2xl">
              #
            </div>
            <h3 className="text-lg font-medium text-foreground/80">Select a room to begin coordination</h3>
            <p className="text-sm max-w-xs text-center mt-2">
              Choose a room from the sidebar to manage tasks, obligations, and the room loop.
            </p>
          </div>
        )}
      </div>
    </div>
  )
}
