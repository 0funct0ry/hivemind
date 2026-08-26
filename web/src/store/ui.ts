import { create } from 'zustand'
import type { ConnectionState } from '../lib/ws'

interface TypingUser {
  userId: string
  name: string
  expiresAt: number
}

interface UiState {
  openThreadId: string | null
  drafts: Record<string, string>
  sidebarOpen: boolean
  threadPanelOpen: boolean
  connectionState: ConnectionState
  /** Unread-divider anchor per channel, frozen at channel-open time until the channel is left. */
  unreadAnchors: Record<string, string | null>
  /** channelId -> userId -> typing state, pruned by expiry. */
  typing: Record<string, Record<string, TypingUser>>
  commandPaletteOpen: boolean
  searchOverlayOpen: boolean
  shortcutsHelpOpen: boolean
  /** A cross-route "scroll to this message once its channel mounts" handoff, consumed by
   * ChannelView/DmView after navigating there from a search result. */
  pendingJump: { channelId: string; messageId: string } | null

  openThread: (messageId: string) => void
  closeThread: () => void
  setDraft: (channelId: string, body: string) => void
  toggleSidebar: (open?: boolean) => void
  setConnectionState: (state: ConnectionState) => void
  setUnreadAnchor: (channelId: string, lastReadMessageId: string | null) => void
  setTyping: (channelId: string, user: TypingUser) => void
  pruneTyping: (channelId: string, now: number) => void
  openCommandPalette: () => void
  closeCommandPalette: () => void
  openSearchOverlay: () => void
  closeSearchOverlay: () => void
  openShortcutsHelp: () => void
  closeShortcutsHelp: () => void
  setPendingJump: (jump: { channelId: string; messageId: string }) => void
  clearPendingJump: () => void
}

export const useUiStore = create<UiState>((set) => ({
  openThreadId: null,
  drafts: {},
  sidebarOpen: true,
  threadPanelOpen: false,
  connectionState: 'closed',
  unreadAnchors: {},
  typing: {},
  commandPaletteOpen: false,
  searchOverlayOpen: false,
  shortcutsHelpOpen: false,
  pendingJump: null,

  openThread: (messageId) => set({ openThreadId: messageId, threadPanelOpen: true }),
  closeThread: () => set({ openThreadId: null, threadPanelOpen: false }),
  setDraft: (channelId, body) =>
    set((s) => ({ drafts: { ...s.drafts, [channelId]: body } })),
  toggleSidebar: (open) => set((s) => ({ sidebarOpen: open ?? !s.sidebarOpen })),
  setConnectionState: (state) => set({ connectionState: state }),
  setUnreadAnchor: (channelId, lastReadMessageId) =>
    set((s) =>
      channelId in s.unreadAnchors
        ? s
        : { unreadAnchors: { ...s.unreadAnchors, [channelId]: lastReadMessageId } },
    ),
  setTyping: (channelId, user) =>
    set((s) => ({
      typing: {
        ...s.typing,
        [channelId]: { ...(s.typing[channelId] ?? {}), [user.userId]: user },
      },
    })),
  pruneTyping: (channelId, now) =>
    set((s) => {
      const existing = s.typing[channelId]
      if (!existing) return s
      const next: Record<string, TypingUser> = {}
      for (const [id, u] of Object.entries(existing)) {
        if (u.expiresAt > now) next[id] = u
      }
      return { typing: { ...s.typing, [channelId]: next } }
    }),
  openCommandPalette: () =>
    set({ commandPaletteOpen: true, searchOverlayOpen: false, shortcutsHelpOpen: false }),
  closeCommandPalette: () => set({ commandPaletteOpen: false }),
  openSearchOverlay: () =>
    set({ searchOverlayOpen: true, commandPaletteOpen: false, shortcutsHelpOpen: false }),
  closeSearchOverlay: () => set({ searchOverlayOpen: false }),
  openShortcutsHelp: () =>
    set({ shortcutsHelpOpen: true, commandPaletteOpen: false, searchOverlayOpen: false }),
  closeShortcutsHelp: () => set({ shortcutsHelpOpen: false }),
  setPendingJump: (jump) => set({ pendingJump: jump }),
  clearPendingJump: () => set({ pendingJump: null }),
}))
