import { create } from 'zustand'
import type { RoomTask } from '@/api/types'

export type ObligationLane = 'ready to claim' | 'awaiting ack' | 'awaiting reply' | 'assigned' | 'blocked' | 'stale' | 'all'

interface RoomControlState {
  selectedRoomID: string | null
  activeLane: ObligationLane
  isTimelineOpen: boolean
  
  // Task Board state
  taskFilters: {
    includeCompleted: boolean
    staleAfter?: string
  }
  
  // Inbox state
  inboxFilters: {
    only: string // direct, ack, reply, alerts, all
  }
  
  // Dialog state
  dialogs: {
    transferLead: { isOpen: boolean; targetActorID?: string }
    reassignTask: { isOpen: boolean; task?: RoomTask; targetActorID?: string }
    reclaimTask: { isOpen: boolean; task?: RoomTask }
    blockTask: { isOpen: boolean; task?: RoomTask }
    abandonTask: { isOpen: boolean; task?: RoomTask }
    confirmAction: { isOpen: boolean; title: string; message: string; onConfirm?: () => void }
  }

  // Actions
  setSelectedRoomID: (id: string | null) => void
  setActiveLane: (lane: ObligationLane) => void
  setTimelineOpen: (isOpen: boolean) => void
  setTaskFilters: (filters: Partial<RoomControlState['taskFilters']>) => void
  setInboxFilters: (filters: Partial<RoomControlState['inboxFilters']>) => void
  openTransferLead: (targetActorID?: string) => void
  closeTransferLead: () => void
  openReassignTask: (task: RoomTask, targetActorID?: string) => void
  closeReassignTask: () => void
  openReclaimTask: (task: RoomTask) => void
  closeReclaimTask: () => void
  openBlockTask: (task: RoomTask) => void
  closeBlockTask: () => void
  openAbandonTask: (task: RoomTask) => void
  closeAbandonTask: () => void
  openConfirm: (title: string, message: string, onConfirm: () => void) => void
  closeConfirm: () => void
}

export const useRoomControlStore = create<RoomControlState>((set) => ({
  selectedRoomID: null,
  activeLane: 'all',
  isTimelineOpen: false,
  
  taskFilters: {
    includeCompleted: false,
  },
  
  inboxFilters: {
    only: 'all',
  },
  
  dialogs: {
    transferLead: { isOpen: false },
    reassignTask: { isOpen: false },
    reclaimTask: { isOpen: false },
    blockTask: { isOpen: false },
    abandonTask: { isOpen: false },
    confirmAction: { isOpen: false, title: '', message: '' },
  },

  setSelectedRoomID: (id) => set({ selectedRoomID: id }),
  setActiveLane: (lane) => set({ activeLane: lane }),
  setTimelineOpen: (isOpen) => set({ isTimelineOpen: isOpen }),
  setTaskFilters: (filters) => set((state) => ({ taskFilters: { ...state.taskFilters, ...filters } })),
  setInboxFilters: (filters) => set((state) => ({ inboxFilters: { ...state.inboxFilters, ...filters } })),
  
  openTransferLead: (targetActorID) => set((state) => ({ 
    dialogs: { ...state.dialogs, transferLead: { isOpen: true, targetActorID } } 
  })),
  closeTransferLead: () => set((state) => ({ 
    dialogs: { ...state.dialogs, transferLead: { isOpen: false } } 
  })),
  
  openReassignTask: (task, targetActorID) => set((state) => ({ 
    dialogs: { ...state.dialogs, reassignTask: { isOpen: true, task, targetActorID } } 
  })),
  closeReassignTask: () => set((state) => ({ 
    dialogs: { ...state.dialogs, reassignTask: { isOpen: false } } 
  })),
  
  openReclaimTask: (task) => set((state) => ({ 
    dialogs: { ...state.dialogs, reclaimTask: { isOpen: true, task } } 
  })),
  closeReclaimTask: () => set((state) => ({ 
    dialogs: { ...state.dialogs, reclaimTask: { isOpen: false } } 
  })),

  openBlockTask: (task) => set((state) => ({ 
    dialogs: { ...state.dialogs, blockTask: { isOpen: true, task } } 
  })),
  closeBlockTask: () => set((state) => ({ 
    dialogs: { ...state.dialogs, blockTask: { isOpen: false } } 
  })),

  openAbandonTask: (task) => set((state) => ({ 
    dialogs: { ...state.dialogs, abandonTask: { isOpen: true, task } } 
  })),
  closeAbandonTask: () => set((state) => ({ 
    dialogs: { ...state.dialogs, abandonTask: { isOpen: false } } 
  })),
  
  openConfirm: (title, message, onConfirm) => set((state) => ({ 
    dialogs: { ...state.dialogs, confirmAction: { isOpen: true, title, message, onConfirm } } 
  })),
  closeConfirm: () => set((state) => ({ 
    dialogs: { ...state.dialogs, confirmAction: { isOpen: false, title: '', message: '', onConfirm: undefined } } 
  })),
}))
