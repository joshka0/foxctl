import { useMemo } from 'react'
import { useQuery } from '@tanstack/react-query'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Input } from '@/components/ui/input'
import { HelpTooltip, Tooltip } from '@/components/ui/tooltip'
import { listRooms, listWorkspaces } from '@/api/client'
import type { Agent } from '@/api/types'
import {
  humanReadableWorkspacePath,
  isPathWorkspace,
  roomDisplayName,
} from '@/lib/room-utils'
import { useViewStore } from '@/stores/viewStore'
import { cn, formatRelativeTime } from '@/lib/utils'
import { ArrowRight, Hash, SendHorizonal } from 'lucide-react'

interface RuntimeRoomPanelProps {
  agents: Agent[]
}

export function RuntimeRoomPanel({ agents }: RuntimeRoomPanelProps) {
  const selectedRoomID = useViewStore((s) => s.selectedRoomID)
  const selectedRoomWorkspaceID = useViewStore((s) => s.selectedRoomWorkspaceID)
  const setSelectedRoom = useViewStore((s) => s.setSelectedRoom)
  const setSpawnRoomDraft = useViewStore((s) => s.setSpawnRoomDraft)
  const setSpawnAgentOpen = useViewStore((s) => s.setSpawnAgentOpen)
  const setSelectedAgent = useViewStore((s) => s.setSelectedAgent)
  const setActiveView = useViewStore((s) => s.setActiveView)

  const { data: workspacesData } = useQuery({
    queryKey: ['workspaces'],
    queryFn: listWorkspaces,
    staleTime: 10000,
  })

  const workspaceOptions = useMemo(() => {
    const fromWorkspaces = (workspacesData?.workspaces ?? [])
      .map((workspace) => workspace.path.trim())
      .filter((path) => path.length > 0)
    if (fromWorkspaces.length > 0) {
      return [...new Set(fromWorkspaces)].sort()
    }
    return [
      ...new Set(
        agents
          .map((agent) => (agent.ns || '').trim())
          .filter((ns) => ns.length > 0),
      ),
    ].sort()
  }, [agents, workspacesData?.workspaces])

  const workspaceID = useMemo(() => {
    const selectedWorkspace = (selectedRoomWorkspaceID || '').trim()
    if (isPathWorkspace(selectedWorkspace)) {
      return selectedWorkspace
    }
    return workspaceOptions[0] || ''
  }, [selectedRoomWorkspaceID, workspaceOptions])

  const workspaceLabel = (workspace: string): string => {
    const trimmed = workspace.trim()
    if (!trimmed) return 'unscoped'
    if (trimmed.startsWith('/')) return humanReadableWorkspacePath(trimmed)

    const matching = workspacesData?.workspaces?.find(
      (entry) => entry.path === trimmed,
    )
    if (matching) {
      return `${matching.name} — ${humanReadableWorkspacePath(matching.path)}`
    }

    if ((workspacesData?.workspaces?.length || 0) === 1) {
      const only = workspacesData!.workspaces[0]
      return `${only.name} — ${humanReadableWorkspacePath(only.path)}`
    }

    if (workspacesData?.current?.trim()) {
      return `${trimmed} · ${humanReadableWorkspacePath(workspacesData.current)}`
    }

    return trimmed
  }

  const roomsQuery = useQuery({
    queryKey: ['rooms', workspaceID, 'runtime-room-panel'],
    enabled: workspaceID.trim().length > 0,
    queryFn: () => listRooms({ workspace_id: workspaceID.trim(), limit: 100 }),
    staleTime: 5000,
  })

  const rooms = useMemo(
    () =>
      [...(roomsQuery.data?.rooms ?? [])].sort((left, right) => {
        const leftTS = Date.parse(left.latest_message_at || '') || 0
        const rightTS = Date.parse(right.latest_message_at || '') || 0
        return rightTS - leftTS
      }),
    [roomsQuery.data?.rooms],
  )

  const selectedRoom = useMemo(
    () => rooms.find((room) => room.id === selectedRoomID) ?? null,
    [rooms, selectedRoomID],
  )

  const openRoom = (roomID?: string | null, roomWorkspace?: string | null) => {
    if (roomID && roomWorkspace) {
      setSelectedRoom(roomID, roomWorkspace)
    } else {
      setSelectedRoom(null, workspaceID.trim())
    }
    setSelectedAgent(null)
    setActiveView('rooms')
  }

  const spawnIntoRoom = (roomID?: string | null, roomWorkspace?: string | null) => {
    if (!roomID || !roomWorkspace) return
    setSelectedRoom(roomID, roomWorkspace)
    setSpawnRoomDraft(roomID, roomWorkspace, null)
    setSelectedAgent(null)
    setSpawnAgentOpen(true)
  }

  return (
    <Card className="bg-muted/30 border-border">
      <CardHeader className="pb-3">
        <div className="flex items-center justify-between gap-3">
          <div>
            <div className="flex items-center gap-1.5">
              <CardTitle className="text-sm">Rooms Shortcut</CardTitle>
              <HelpTooltip
                side="top"
                content="Use this shortcut to jump into an existing room from Runtime or start a new agent directly inside one."
              />
            </div>
            <div className="mt-1 text-xs text-muted-foreground">
              Use Runtime to jump into an existing room or spawn into one. Create
              and edit rooms in the dedicated Rooms surface.
            </div>
          </div>
          <Badge variant="secondary" className="text-[10px]">
            {rooms.length} rooms
          </Badge>
        </div>
      </CardHeader>
      <CardContent className="space-y-3">
        <div className="grid gap-3 md:grid-cols-[minmax(0,1fr)_auto]">
          <div className="space-y-2">
            <label className="text-xs font-medium text-muted-foreground">
              <span className="inline-flex items-center gap-1">
                Workspace
                <HelpTooltip
                  content="Choose which workspace's rooms should be listed here."
                  side="top"
                />
              </span>
            </label>
            {workspaceOptions.length > 0 ? (
              <select
                value={workspaceID}
                onChange={(e) => {
                  setSelectedRoom(null, e.target.value)
                }}
                className="w-full h-9 rounded-md border border-input bg-background px-3 text-sm font-mono"
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
                onChange={(e) => setSelectedRoom(null, e.target.value)}
                placeholder="workspace path"
                className="h-9"
              />
            )}
          </div>
          <div className="flex items-end gap-2">
            <Tooltip content="Reload the room list for the selected workspace.">
              <Button
                type="button"
                variant="outline"
                size="sm"
                className="h-9"
                onClick={() => roomsQuery.refetch()}
                disabled={roomsQuery.isFetching || !workspaceID.trim()}
              >
                Refresh
              </Button>
            </Tooltip>
            <Tooltip content="Open the full Rooms surface for this workspace.">
              <Button
                type="button"
                size="sm"
                className="h-9"
                onClick={() => openRoom(null, workspaceID.trim())}
                disabled={!workspaceID.trim()}
              >
                Open Rooms
              </Button>
            </Tooltip>
          </div>
        </div>

        {selectedRoom && (
          <div className="rounded-md border border-primary/20 bg-primary/5 px-3 py-3">
            <div className="flex items-center justify-between gap-3">
              <div className="min-w-0">
                <div className="text-sm font-medium text-foreground truncate">
                  {roomDisplayName(selectedRoom)}
                </div>
                <div className="text-xs text-muted-foreground">
                  {selectedRoom.message_count} messages
                  {selectedRoom.latest_message_at
                    ? ` · updated ${formatRelativeTime(selectedRoom.latest_message_at)}`
                    : ''}
                </div>
              </div>
              <div className="flex gap-2">
                <Tooltip content="Open this room in the dedicated Rooms surface.">
                  <Button
                    type="button"
                    variant="outline"
                    size="sm"
                    className="h-8"
                    onClick={() => openRoom(selectedRoom.id, selectedRoom.workspace_id)}
                  >
                    <ArrowRight className="mr-1 h-3.5 w-3.5" />
                    Open
                  </Button>
                </Tooltip>
                <Tooltip content="Spawn a new agent and connect it directly to this room.">
                  <Button
                    type="button"
                    size="sm"
                    className="h-8"
                    onClick={() =>
                      spawnIntoRoom(selectedRoom.id, selectedRoom.workspace_id)
                    }
                  >
                    <SendHorizonal className="mr-1 h-3.5 w-3.5" />
                    Spawn Into
                  </Button>
                </Tooltip>
              </div>
            </div>
          </div>
        )}

        <div className="space-y-2">
          <div className="flex items-center justify-between">
            <label className="text-xs font-medium text-muted-foreground">
              Recent Rooms
            </label>
            <Badge variant="outline" className="text-[10px]">
              {rooms.length > 6 ? 'showing 6' : `${rooms.length}`}
            </Badge>
          </div>

          {workspaceID.trim().length === 0 ? (
            <div className="rounded-md border border-dashed border-border px-3 py-2 text-xs text-muted-foreground">
              Select a workspace to inspect rooms.
            </div>
          ) : rooms.length === 0 ? (
            <div className="rounded-md border border-dashed border-border px-3 py-2 text-xs text-muted-foreground">
              No rooms found yet. Use the Rooms surface to create the first one.
            </div>
          ) : (
            <div className="grid gap-2">
              {rooms.slice(0, 6).map((room) => {
                const active = room.id === selectedRoomID
                return (
                  <div
                    key={room.id}
                    className={cn(
                      'rounded-md border px-3 py-3',
                      active
                        ? 'border-primary bg-primary/5'
                        : 'border-border bg-background',
                    )}
                  >
                    <div className="flex items-center justify-between gap-3">
                      <div className="min-w-0">
                        <div className="flex items-center gap-2">
                          <Hash className="h-3.5 w-3.5 text-muted-foreground" />
                          <span className="text-sm font-medium text-foreground truncate">
                            {roomDisplayName(room)}
                          </span>
                        </div>
                        <div className="mt-1 text-xs text-muted-foreground line-clamp-1">
                          {room.latest_preview || room.description || 'No room preview'}
                        </div>
                        <div className="mt-1 text-[11px] text-muted-foreground">
                          {room.message_count} messages
                          {room.latest_message_at
                            ? ` · ${formatRelativeTime(room.latest_message_at)}`
                            : ''}
                        </div>
                      </div>
                      <div className="flex flex-col gap-2 sm:flex-row">
                        <Button
                          type="button"
                          variant="outline"
                          size="sm"
                          className="h-8"
                          onClick={() => openRoom(room.id, room.workspace_id)}
                        >
                          Open
                        </Button>
                        <Button
                          type="button"
                          size="sm"
                          className="h-8"
                          onClick={() => spawnIntoRoom(room.id, room.workspace_id)}
                        >
                          Spawn Into
                        </Button>
                      </div>
                    </div>
                  </div>
                )
              })}
            </div>
          )}
        </div>
      </CardContent>
    </Card>
  )
}
