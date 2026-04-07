import { create } from 'zustand'
import {
  applyOrchestrationCardAction,
  getOrchestrationBoard,
  getOrchestrationBoardCardRuntime,
  refreshOrchestration,
  type OrchestrationBoardGetParams,
} from '@/api/client'
import type {
  OrchestrationBoard,
  OrchestrationBoardArtifactRef,
  OrchestrationCard,
  OrchestrationCardAction,
  OrchestrationRuntimeTree,
} from '@/api/types'

export const ORCHESTRATION_LANE_ORDER = [
  'Todo',
  'Claimed',
  'Running',
  'RetryQueued',
  'Blocked',
  'Review',
  'Done',
] as const

let boardDataSeq = 0
let boardLoadSeq = 0
let boardRefreshSeq = 0
let cardLoadSeq = 0
let cardActionSeq = 0

function requestID(prefix: string): string {
  if (typeof crypto !== 'undefined' && typeof crypto.randomUUID === 'function') {
    return `${prefix}-${crypto.randomUUID()}`
  }
  return `${prefix}-${Date.now()}-${Math.random().toString(16).slice(2)}`
}

interface OrchestrationBoardState {
  board: OrchestrationBoard | null
  artifact: OrchestrationBoardArtifactRef | null
  selectedCard: OrchestrationCard | null
  selectedCardRuntime: OrchestrationRuntimeTree | null
  loadingBoard: boolean
  loadingCard: boolean
  refreshing: boolean
  actingOnCard: boolean
  error: string | null
  loadBoard: (params?: Omit<OrchestrationBoardGetParams, 'request_id'>) => Promise<void>
  refreshBoard: (params?: { workspaceID?: string; archivedOnly?: boolean }) => Promise<void>
  loadCard: (issueID: string, workspaceID?: string) => Promise<void>
  applyCardAction: (issueID: string, action: OrchestrationCardAction, workspaceID?: string) => Promise<void>
  clearSelectedCard: () => void
}

export const useOrchestrationBoardStore = create<OrchestrationBoardState>((set) => ({
  board: null,
  artifact: null,
  selectedCard: null,
  selectedCardRuntime: null,
  loadingBoard: false,
  loadingCard: false,
  refreshing: false,
  actingOnCard: false,
  error: null,
  loadBoard: async (params) => {
    const loadSeq = ++boardLoadSeq
    const dataSeq = ++boardDataSeq
    cardLoadSeq++
    set({
      loadingBoard: true,
      error: null,
      board: null,
      artifact: null,
      selectedCard: null,
      selectedCardRuntime: null,
    })
    try {
      const result = await getOrchestrationBoard({
        request_id: requestID('board'),
        ...params,
      })
      if (dataSeq !== boardDataSeq) return
      set({
        board: result.board,
        artifact: result.artifact,
      })
    } catch (error) {
      if (dataSeq !== boardDataSeq) return
      set({
        error: error instanceof Error ? error.message : 'Failed to load orchestration board',
      })
    } finally {
      if (loadSeq === boardLoadSeq) {
        set({ loadingBoard: false })
      }
    }
  },
  refreshBoard: async (params) => {
    const refreshSeq = ++boardRefreshSeq
    const dataSeq = ++boardDataSeq
    set({ refreshing: true, error: null })
    try {
      await refreshOrchestration({
        request_id: requestID('refresh'),
        workspace_id: params?.workspaceID,
      })
      const result = await getOrchestrationBoard({
        request_id: requestID('board'),
        workspace_id: params?.workspaceID,
        archived_only: params?.archivedOnly,
      })
      if (dataSeq !== boardDataSeq) return
      set({
        board: result.board,
        artifact: result.artifact,
      })
    } catch (error) {
      if (dataSeq !== boardDataSeq) return
      set({
        error: error instanceof Error ? error.message : 'Failed to refresh orchestration board',
      })
    } finally {
      if (refreshSeq === boardRefreshSeq) {
        set({ refreshing: false })
      }
    }
  },
  loadCard: async (issueID, workspaceID) => {
    const seq = ++cardLoadSeq
    set({ loadingCard: true, error: null, selectedCardRuntime: null })
    try {
      const result = await getOrchestrationBoardCardRuntime({
        request_id: requestID('card'),
        workspace_id: workspaceID,
        issue_id: issueID,
        depth: 3,
      })
      if (seq !== cardLoadSeq) return
      set({
        selectedCard: result.card,
        selectedCardRuntime: result.runtime ?? null,
      })
    } catch (error) {
      if (seq !== cardLoadSeq) return
      set({
        error: error instanceof Error ? error.message : 'Failed to load board card',
      })
    } finally {
      if (seq === cardLoadSeq) {
        set({ loadingCard: false })
      }
    }
  },
  applyCardAction: async (issueID, action, workspaceID) => {
    const seq = ++cardActionSeq
    set({ actingOnCard: true, error: null })
    try {
      const result = await applyOrchestrationCardAction({
        request_id: requestID(`card-${action}`),
        workspace_id: workspaceID,
        issue_id: issueID,
        action,
      })
      const [boardResult, runtimeResult] = await Promise.all([
        getOrchestrationBoard({
          request_id: requestID('board'),
          workspace_id: workspaceID,
        }),
        getOrchestrationBoardCardRuntime({
          request_id: requestID('card'),
          workspace_id: workspaceID,
          issue_id: issueID,
          depth: 3,
        }).catch(() => null),
      ])
      if (seq !== cardActionSeq) return
      set({
        board: boardResult.board,
        artifact: boardResult.artifact,
        selectedCard: runtimeResult?.card ?? result.card,
        selectedCardRuntime: runtimeResult?.runtime ?? null,
      })
    } catch (error) {
      if (seq !== cardActionSeq) return
      set({
        error: error instanceof Error ? error.message : 'Failed to apply orchestration card action',
      })
    } finally {
      if (seq === cardActionSeq) {
        set({ actingOnCard: false })
      }
    }
  },
  clearSelectedCard: () => {
    cardLoadSeq++
    cardActionSeq++
    set({ selectedCard: null, selectedCardRuntime: null, loadingCard: false, actingOnCard: false })
  },
}))
