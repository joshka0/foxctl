import { useEffect } from 'react'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { useViewStore } from '@/stores/viewStore'
import {
  listAgents,
  startAgent,
  killAgent,
  trashAgent,
  listAgentSessions,
} from '@/api/client'
import type { Agent } from '@/api/types'

/**
 * Extracts agent management logic (reconciliation, sessions, start/kill/trash)
 * from the former AgentHUD into a reusable hook.
 *
 * @param linkedAgent - The agent linked to the current conversation (if any).
 *                      Takes priority over the globally selected agent.
 */
export function useAgentOperations(linkedAgent: Agent | null) {
  const selectedAgent = useViewStore((s) => s.selectedAgent)
  const setSelectedAgent = useViewStore((s) => s.setSelectedAgent)
  const queryClient = useQueryClient()

  // The agent to display: prefer conversation-linked, fall back to sidebar selection
  const targetAgent = linkedAgent || selectedAgent

  // Fetch fresh agent list to reconcile stale selectedAgent
  const { data: agentsData } = useQuery({
    queryKey: ['agents'],
    queryFn: () => listAgents(100),
    refetchInterval: 10000,
  })

  // Reconcile selected agent with fresh data
  useEffect(() => {
    if (!selectedAgent || !agentsData?.agents) return
    const fresh = agentsData.agents.find((a) => a.id === selectedAgent.id)
    if (
      fresh &&
      (fresh.state !== selectedAgent.state ||
        fresh.updated_at !== selectedAgent.updated_at)
    ) {
      setSelectedAgent(fresh)
    }
  }, [agentsData, selectedAgent, setSelectedAgent])

  // Fetch sessions for the target agent
  const { data: sessionsData } = useQuery({
    queryKey: ['agent-sessions', targetAgent?.id],
    queryFn: () => listAgentSessions(targetAgent!.id),
    enabled: !!targetAgent,
    refetchInterval: 5000,
  })

  // Start agent
  const startMutation = useMutation({
    mutationFn: (agentId: string) => startAgent(agentId, {}),
    onSuccess: (_data, agentId) => {
      queryClient.invalidateQueries({ queryKey: ['agents'] })
      queryClient.invalidateQueries({
        queryKey: ['agent-sessions', agentId],
      })
    },
  })

  // Kill agent
  const killMutation = useMutation({
    mutationFn: (agentId: string) => killAgent(agentId),
    onSuccess: (_data, agentId) => {
      queryClient.invalidateQueries({ queryKey: ['agents'] })
      queryClient.invalidateQueries({
        queryKey: ['agent-sessions', agentId],
      })
    },
  })

  // Trash agent
  const trashMutation = useMutation({
    mutationFn: (agentId: string) => trashAgent(agentId),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['agents'] })
      setSelectedAgent(null)
    },
  })

  return {
    targetAgent,
    sessions: sessionsData?.sessions ?? [],
    startAgent: startMutation,
    killAgent: killMutation,
    trashAgent: trashMutation,
  }
}
