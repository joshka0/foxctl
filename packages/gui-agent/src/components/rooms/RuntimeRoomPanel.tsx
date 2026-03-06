import { useEffect, useMemo, useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Checkbox } from '@/components/ui/checkbox'
import { Input } from '@/components/ui/input'
import { Textarea } from '@/components/ui/textarea'
import { createRoom, listRooms, listWorkspaces, patchRoom, patchRoomMembers } from '@/api/client'
import type { Agent } from '@/api/types'
import { humanReadableWorkspacePath, isPathWorkspace, roomDisplayName } from '@/lib/room-utils'
import { useViewStore } from '@/stores/viewStore'
import { cn } from '@/lib/utils'
import { Hash, SendHorizonal } from 'lucide-react'

interface RuntimeRoomPanelProps {
  agents: Agent[]
}

function slugRoomID(title: string): string {
  const normalized = title.trim().toLowerCase()
  if (!normalized) return ''
  return normalized
    .replace(/[^a-z0-9]+/g, '-')
    .replace(/^-+|-+$/g, '')
}

export function RuntimeRoomPanel({ agents }: RuntimeRoomPanelProps) {
  const queryClient = useQueryClient()
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
    return [...new Set(agents.map((agent) => (agent.ns || '').trim()).filter((ns) => ns.length > 0))].sort()
  }, [agents, workspacesData?.workspaces])
  const [workspaceID, setWorkspaceID] = useState('')
  const [roomID, setRoomID] = useState('')
  const [title, setTitle] = useState('')
  const [description, setDescription] = useState('')
  const [selectedMembers, setSelectedMembers] = useState<string[]>([])
  const [roomError, setRoomError] = useState<string | null>(null)

  useEffect(() => {
    const selectedWorkspace = selectedRoomWorkspaceID ?? ''
    if (isPathWorkspace(selectedWorkspace)) {
      setWorkspaceID(selectedWorkspace.trim())
      return
    }
    if (!workspaceID && workspaceOptions[0]) {
      setWorkspaceID(workspaceOptions[0])
    }
  }, [selectedRoomWorkspaceID, workspaceID, workspaceOptions])

  const workspaceLabel = (workspace: string): string => {
    const trimmed = workspace.trim()
    if (!trimmed) return 'unscoped'
    if (trimmed.startsWith('/')) return humanReadableWorkspacePath(trimmed)

    const matching = workspacesData?.workspaces?.find((entry) => entry.path === trimmed)
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

  const rooms = roomsQuery.data?.rooms ?? []
  const selectedRoom = useMemo(
    () => rooms.find((room) => room.id === selectedRoomID) ?? null,
    [rooms, selectedRoomID],
  )
  const workspaceAgents = useMemo(
    () => agents
      .filter((agent) => (agent.ns || '').trim() === workspaceID.trim())
      .sort((a, b) => (a.name || a.id).localeCompare(b.name || b.id)),
    [agents, workspaceID],
  )

  useEffect(() => {
    if (selectedRoom && selectedRoom.workspace_id === workspaceID.trim()) {
      setRoomID(selectedRoom.id)
      setTitle(selectedRoom.title || selectedRoom.id)
      setDescription(selectedRoom.description || '')
      setSelectedMembers((selectedRoom.members ?? []).map((member) => member.actor_id))
      return
    }
    if (!selectedRoomID) {
      setRoomID('')
      setTitle('')
      setDescription('')
      setSelectedMembers([])
    }
  }, [selectedRoom, selectedRoomID, workspaceID])

  const saveRoomMutation = useMutation({
    mutationFn: async () => {
      const nextRoomID = roomID.trim() || slugRoomID(title)
      if (!workspaceID.trim()) throw new Error('Workspace is required')
      if (!nextRoomID) throw new Error('Room ID or title is required')
      const nextTitle = title.trim() || nextRoomID
      const members = selectedMembers.map((actor_id) => ({ actor_id }))

      if (selectedRoom?.id === nextRoomID) {
        await patchRoom(nextRoomID, {
          workspace_id: workspaceID.trim(),
          title: nextTitle,
          description: description.trim(),
        })
        return patchRoomMembers(nextRoomID, {
          workspace_id: workspaceID.trim(),
          members,
        })
      }

      return createRoom({
        workspace_id: workspaceID.trim(),
        id: nextRoomID,
        title: nextTitle,
        description: description.trim(),
        members,
      })
    },
    onSuccess: async (result) => {
      setRoomError(null)
      if (result.room?.id) {
        setSelectedRoom(result.room.id, result.room.workspace_id)
        setRoomID(result.room.id)
      }
      await queryClient.invalidateQueries({ queryKey: ['rooms', workspaceID] })
    },
    onError: (error) => {
      setRoomError(error instanceof Error ? error.message : 'Failed to save room')
    },
  })

  const ensureRoomAndOpen = async (mode: 'open' | 'spawn') => {
    const result = await saveRoomMutation.mutateAsync()
    const nextRoom = result.room
    if (!nextRoom) return
    setSelectedRoom(nextRoom.id, nextRoom.workspace_id)
    setSelectedAgent(null)
    if (mode === 'spawn') {
      setSpawnRoomDraft(nextRoom.id, nextRoom.workspace_id, null)
      setSpawnAgentOpen(true)
      return
    }
    setActiveView('rooms')
  }

  const toggleMember = (actorID: string) => {
    setSelectedMembers((current) =>
      current.includes(actorID)
        ? current.filter((id) => id !== actorID)
        : [...current, actorID],
    )
  }

  return (
    <Card className="bg-muted/30 border-border">
      <CardHeader className="pb-3">
        <div className="flex items-center justify-between gap-3">
          <div>
            <CardTitle className="text-sm">Room Control</CardTitle>
            <div className="mt-1 text-xs text-muted-foreground">
              Create a room, attach agents from this workspace, then open or spawn into it.
            </div>
          </div>
          <Badge variant="secondary" className="text-[10px]">
            {selectedMembers.length} selected
          </Badge>
        </div>
      </CardHeader>
      <CardContent className="space-y-3">
        <div className="grid gap-3 md:grid-cols-3">
          <div className="md:col-span-1">
            <label className="text-xs font-medium text-muted-foreground">Workspace</label>
            <select
              value={workspaceID}
              onChange={(e) => {
                setWorkspaceID(e.target.value)
                setSelectedRoom(null, e.target.value)
              }}
              className="mt-1 w-full h-9 rounded-md border border-input bg-background px-3 text-sm font-mono"
            >
              <option value="">Select workspace</option>
              {workspaceOptions.map((workspace) => (
                <option key={workspace} value={workspace}>
                  {workspaceLabel(workspace)}
                </option>
              ))}
            </select>
          </div>
          <div className="md:col-span-2">
            <label className="text-xs font-medium text-muted-foreground">Existing Rooms</label>
            <div className="mt-1 flex gap-2">
              <select
                value={selectedRoom?.id || ''}
                onChange={(e) => {
                  const nextID = e.target.value
                  if (!nextID) {
                    setSelectedRoom(null, workspaceID.trim())
                    setRoomID('')
                    return
                  }
                  const nextRoom = rooms.find((room) => room.id === nextID)
                  if (!nextRoom) return
                  setSelectedRoom(nextRoom.id, nextRoom.workspace_id)
                  setRoomError(null)
                }}
                className="flex-1 h-9 rounded-md border border-input bg-background px-3 text-sm"
              >
                <option value="">New room…</option>
                {rooms.map((room) => (
                  <option key={room.id} value={room.id}>
                    {roomDisplayName(room)}
                  </option>
                ))}
              </select>
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
            </div>
          </div>
        </div>

        <div className="grid gap-3 md:grid-cols-2">
          <div>
            <label className="text-xs font-medium text-muted-foreground">Room ID</label>
            <Input
              value={roomID}
              onChange={(e) => setRoomID(e.target.value)}
              placeholder="room id"
              className="mt-1 h-9"
            />
          </div>
          <div>
            <label className="text-xs font-medium text-muted-foreground">Title</label>
            <Input
              value={title}
              onChange={(e) => setTitle(e.target.value)}
              placeholder="Human-facing room title"
              className="mt-1 h-9"
            />
          </div>
        </div>

        <div>
          <label className="text-xs font-medium text-muted-foreground">Description</label>
          <Textarea
            value={description}
            onChange={(e) => setDescription(e.target.value)}
            rows={2}
            className="mt-1"
            placeholder="What is this room coordinating?"
          />
        </div>

        <div className="space-y-2">
          <div className="flex items-center justify-between gap-2">
            <label className="text-xs font-medium text-muted-foreground">Agents In Workspace</label>
            <Badge variant="outline" className="text-[10px]">
              {workspaceAgents.length}
            </Badge>
          </div>
          <div className="grid gap-2 md:grid-cols-2">
            {workspaceAgents.map((agent) => (
              <label
                key={agent.id}
                className={cn(
                  'flex items-center gap-2 rounded-md border border-border bg-background px-3 py-2 text-sm',
                  selectedMembers.includes(agent.id) && 'border-primary/40 bg-primary/5',
                )}
              >
                <Checkbox
                  checked={selectedMembers.includes(agent.id)}
                  onCheckedChange={() => toggleMember(agent.id)}
                />
                <div className="min-w-0">
                  <div className="truncate font-medium text-foreground">
                    {agent.name || agent.id.slice(0, 8)}
                  </div>
                  <div className="truncate text-xs text-muted-foreground">
                    {agent.role || 'agent'} · {agent.state}
                  </div>
                </div>
              </label>
            ))}
            {workspaceAgents.length === 0 && (
              <div className="rounded-md border border-dashed border-border px-3 py-2 text-xs text-muted-foreground md:col-span-2">
                No agents found in this workspace yet.
              </div>
            )}
          </div>
        </div>

        {roomError && (
          <div className="rounded-md border border-destructive/20 bg-destructive/5 px-3 py-2 text-xs text-destructive">
            {roomError}
          </div>
        )}

        <div className="flex flex-wrap items-center justify-end gap-2">
          <Button
            type="button"
            variant="outline"
            size="sm"
            className="h-8"
            onClick={() => void saveRoomMutation.mutate()}
            disabled={saveRoomMutation.isPending || !workspaceID.trim()}
          >
            <Hash className="mr-1 h-3.5 w-3.5" />
            Save Room
          </Button>
          <Button
            type="button"
            variant="outline"
            size="sm"
            className="h-8"
            onClick={() => void ensureRoomAndOpen('open')}
            disabled={saveRoomMutation.isPending || !workspaceID.trim()}
          >
            Open Room
          </Button>
          <Button
            type="button"
            size="sm"
            className="h-8"
            onClick={() => void ensureRoomAndOpen('spawn')}
            disabled={saveRoomMutation.isPending || !workspaceID.trim()}
          >
            <SendHorizonal className="mr-1 h-3.5 w-3.5" />
            Spawn Into Room
          </Button>
        </div>
      </CardContent>
    </Card>
  )
}
