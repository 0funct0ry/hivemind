import { useEffect } from 'react'
import { useNavigate } from 'react-router-dom'
import { useQueryClient } from '@tanstack/react-query'
import { useUiStore } from '../store/ui'
import type { Channel, DM, UnreadEntry } from '../lib/api'

export interface ShortcutDef {
  combo: string
  description: string
  category: 'Navigation' | 'Messaging' | 'Search'
}

export const SHORTCUTS: ShortcutDef[] = [
  { combo: '⌘K / Ctrl+K', description: 'Jump to a channel, DM, or person', category: 'Navigation' },
  { combo: '⌘/ / Ctrl+/', description: 'Search messages', category: 'Search' },
  { combo: '?', description: 'Show this shortcuts overlay', category: 'Navigation' },
  { combo: 'Esc', description: 'Close thread, then overlay, then blur composer', category: 'Navigation' },
  { combo: 'Alt+↑ / Alt+↓', description: 'Previous / next channel', category: 'Navigation' },
  { combo: 'Alt+Shift+↑ / Alt+Shift+↓', description: 'Previous / next unread channel', category: 'Navigation' },
  { combo: '⌘⇧A', description: 'All unreads', category: 'Navigation' },
]

function isTypingTarget(e: KeyboardEvent): boolean {
  const el = e.target as HTMLElement | null
  if (!el) return false
  const tag = el.tagName
  return tag === 'INPUT' || tag === 'TEXTAREA' || el.isContentEditable
}

/** Ordered list of all channels/DMs a user can navigate between with Alt+arrows. */
function orderedNavItems(channels: Channel[], dms: DM[]): { path: string; id: string }[] {
  const chanItems = channels
    .filter((c) => c.kind !== 'dm')
    .map((c) => ({ path: `/c/${c.slug}`, id: c.id }))
  const dmItems = dms.map((d) => ({ path: `/dm/${d.peer.username}`, id: d.id }))
  return [...chanItems, ...dmItems]
}

export function useGlobalShortcuts() {
  const navigate = useNavigate()
  const queryClient = useQueryClient()
  const openCommandPalette = useUiStore((s) => s.openCommandPalette)
  const openSearchOverlay = useUiStore((s) => s.openSearchOverlay)
  const openShortcutsHelp = useUiStore((s) => s.openShortcutsHelp)
  const closeCommandPalette = useUiStore((s) => s.closeCommandPalette)
  const closeSearchOverlay = useUiStore((s) => s.closeSearchOverlay)
  const closeShortcutsHelp = useUiStore((s) => s.closeShortcutsHelp)
  const closeThread = useUiStore((s) => s.closeThread)

  useEffect(() => {
    const handleKeyDown = (e: KeyboardEvent) => {
      const mod = e.metaKey || e.ctrlKey

      if (mod && e.key.toLowerCase() === 'k') {
        e.preventDefault()
        openCommandPalette()
        return
      }
      if (mod && e.key === '/') {
        e.preventDefault()
        openSearchOverlay()
        return
      }
      if (e.key === '?' && !isTypingTarget(e)) {
        e.preventDefault()
        openShortcutsHelp()
        return
      }
      if (e.key === 'Escape') {
        const s = useUiStore.getState()
        if (s.threadPanelOpen) {
          e.preventDefault()
          closeThread()
        } else if (s.commandPaletteOpen) {
          e.preventDefault()
          closeCommandPalette()
        } else if (s.searchOverlayOpen) {
          e.preventDefault()
          closeSearchOverlay()
        } else if (s.shortcutsHelpOpen) {
          e.preventDefault()
          closeShortcutsHelp()
        } else if (document.activeElement instanceof HTMLElement) {
          document.activeElement.blur()
        }
        return
      }
      if (mod && e.shiftKey && e.key.toLowerCase() === 'a') {
        e.preventDefault()
        navigate('/')
        return
      }
      if (e.altKey && (e.key === 'ArrowUp' || e.key === 'ArrowDown')) {
        e.preventDefault()
        const channels = queryClient.getQueryData<{ data: Channel[] }>(['channels'])?.data ?? []
        const dms = queryClient.getQueryData<{ data: DM[] }>(['dms'])?.data ?? []
        const items = orderedNavItems(channels, dms)
        if (items.length === 0) return

        let candidates = items
        if (e.shiftKey) {
          const unreads = queryClient.getQueryData<{ data: UnreadEntry[] }>(['unreads'])?.data ?? []
          const unreadIds = new Set(unreads.filter((u) => u.unread_count > 0).map((u) => u.channel_id))
          candidates = items.filter((i) => unreadIds.has(i.id))
          if (candidates.length === 0) return
        }

        const currentPath = window.location.pathname
        const idx = candidates.findIndex((i) => i.path === currentPath)
        const delta = e.key === 'ArrowUp' ? -1 : 1
        const next = candidates[(((idx === -1 ? 0 : idx) + delta) % candidates.length + candidates.length) % candidates.length]
        navigate(next.path)
      }
    }

    window.addEventListener('keydown', handleKeyDown)
    return () => window.removeEventListener('keydown', handleKeyDown)
  }, [
    navigate,
    queryClient,
    openCommandPalette,
    openSearchOverlay,
    openShortcutsHelp,
    closeCommandPalette,
    closeSearchOverlay,
    closeShortcutsHelp,
    closeThread,
  ])
}
