import { create } from 'zustand'
import type { ConsoleMessage, ConsoleSession } from '@/api/client'

export interface ChatState {
  // Current session
  sessionId: string | null
  session: ConsoleSession | null
  messages: ConsoleMessage[]
  inflight: boolean

  // Source persisted session (for loading historical messages)
  persistedSessionId: string | null

  // Source agent metadata (for display)
  sourceAgent: {
    id: string
    name?: string
    role?: string
    ns?: string
  } | null

  // Flag to prevent auto-initialization when loading from agent
  isInitializing: boolean

  // Pending message being sent
  pendingMessage: string | null

  // Actions
  setSession: (session: ConsoleSession | null) => void
  setSessionId: (id: string | null) => void
  setPersistedSessionId: (id: string | null) => void
  setSourceAgent: (agent: ChatState['sourceAgent']) => void
  setMessages: (messages: ConsoleMessage[]) => void
  addMessage: (message: ConsoleMessage) => void
  updateLastMessage: (content: string) => void
  appendToLastMessage: (chunk: string) => void
  setInflight: (inflight: boolean) => void
  setPendingMessage: (message: string | null) => void
  setInitializing: (isInitializing: boolean) => void
  reset: () => void
}

export const useChatStore = create<ChatState>((set) => ({
  sessionId: null,
  session: null,
  messages: [],
  inflight: false,
  persistedSessionId: null,
  sourceAgent: null,
  isInitializing: false,
  pendingMessage: null,

  setSession: (session) => set({ session }),
  setSessionId: (sessionId) => set({ sessionId }),
  setPersistedSessionId: (id) => set({ persistedSessionId: id }),
  setSourceAgent: (agent) => set({ sourceAgent: agent }),
  setInitializing: (isInitializing) => set({ isInitializing }),
  setMessages: (messages) => set({ messages }),
  addMessage: (message) =>
    set((state) => ({ messages: [...state.messages, message] })),
  updateLastMessage: (content) =>
    set((state) => {
      const messages = [...state.messages]
      if (messages.length > 0) {
        messages[messages.length - 1] = {
          ...messages[messages.length - 1],
          content,
        }
      }
      return { messages }
    }),
  appendToLastMessage: (chunk) =>
    set((state) => {
      const messages = [...state.messages]
      if (messages.length > 0) {
        const lastMsg = messages[messages.length - 1]
        messages[messages.length - 1] = {
          ...lastMsg,
          content: lastMsg.content + chunk,
        }
      }
      return { messages }
    }),
  setInflight: (inflight) => set({ inflight }),
  setPendingMessage: (pendingMessage) => set({ pendingMessage }),
  reset: () =>
    set({
      sessionId: null,
      session: null,
      messages: [],
      inflight: false,
      persistedSessionId: null,
      sourceAgent: null,
      isInitializing: false,
      pendingMessage: null,
    }),
}))
