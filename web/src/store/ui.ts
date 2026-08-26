import { create } from 'zustand'
import type { ConnectionState } from '../lib/ws'

interface UiState {
  openThreadId: string | null
  drafts: Record<string, string>
  sidebarOpen: boolean
  threadPanelOpen: boolean
  connectionState: ConnectionState

  openThread: (messageId: string) => void
  closeThread: () => void
  setDraft: (channelId: string, body: string) => void
  toggleSidebar: (open?: boolean) => void
  setConnectionState: (state: ConnectionState) => void
}

export const useUiStore = create<UiState>((set) => ({
  openThreadId: null,
  drafts: {},
  sidebarOpen: true,
  threadPanelOpen: false,
  connectionState: 'closed',

  openThread: (messageId) => set({ openThreadId: messageId, threadPanelOpen: true }),
  closeThread: () => set({ openThreadId: null, threadPanelOpen: false }),
  setDraft: (channelId, body) =>
    set((s) => ({ drafts: { ...s.drafts, [channelId]: body } })),
  toggleSidebar: (open) => set((s) => ({ sidebarOpen: open ?? !s.sidebarOpen })),
  setConnectionState: (state) => set({ connectionState: state }),
}))
