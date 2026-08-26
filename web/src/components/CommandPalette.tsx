import { useEffect, useRef, useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { useNavigate } from 'react-router-dom'
import { api, type User } from '../lib/api'
import { useUiStore } from '../store/ui'
import { fuzzySearch } from '../lib/fuzzy'
import { Modal } from './Modal'

const RECENTS_KEY = 'hivemind.recentNav'
const RECENTS_LIMIT = 10

interface RecentEntry {
  type: 'channel' | 'dm' | 'person'
  id: string
  label: string
  path: string
  ts: number
}

function loadRecents(): RecentEntry[] {
  try {
    const raw = localStorage.getItem(RECENTS_KEY)
    return raw ? (JSON.parse(raw) as RecentEntry[]) : []
  } catch {
    return []
  }
}

function pushRecent(entry: Omit<RecentEntry, 'ts'>) {
  try {
    const existing = loadRecents().filter((r) => !(r.type === entry.type && r.id === entry.id))
    const next = [{ ...entry, ts: Date.now() }, ...existing].slice(0, RECENTS_LIMIT)
    localStorage.setItem(RECENTS_KEY, JSON.stringify(next))
  } catch {
    // localStorage unavailable (private mode, quota) — recents are a convenience, not critical
  }
}

interface PaletteItem {
  type: 'channel' | 'dm' | 'person'
  id: string
  label: string
  sublabel?: string
  path: string
}

export function CommandPalette() {
  const open = useUiStore((s) => s.commandPaletteOpen)
  const close = useUiStore((s) => s.closeCommandPalette)
  const navigate = useNavigate()
  const inputRef = useRef<HTMLInputElement>(null)
  const [query, setQuery] = useState('')
  const [selected, setSelected] = useState(0)

  const channelsQuery = useQuery({ queryKey: ['channels'], queryFn: api.listChannels, enabled: open })
  const dmsQuery = useQuery({ queryKey: ['dms'], queryFn: api.listDMs, enabled: open })
  const [people, setPeople] = useState<User[]>([])

  useEffect(() => {
    if (!open) {
      setQuery('')
      setSelected(0)
    }
  }, [open])

  useEffect(() => {
    if (!open || query.trim() === '') {
      setPeople([])
      return
    }
    let cancelled = false
    const t = window.setTimeout(() => {
      void api.listUsers({ q: query, limit: 8 }).then((res) => {
        if (!cancelled) setPeople(res.data)
      })
    }, 150)
    return () => {
      cancelled = true
      window.clearTimeout(t)
    }
  }, [open, query])

  if (!open) return null

  const channelItems: PaletteItem[] = (channelsQuery.data?.data ?? [])
    .filter((c) => c.kind !== 'dm')
    .map((c) => ({ type: 'channel', id: c.id, label: `#${c.name}`, path: `/c/${c.slug}` }))
  const dmItems: PaletteItem[] = (dmsQuery.data?.data ?? []).map((d) => ({
    type: 'dm',
    id: d.id,
    label: d.peer.display_name || d.peer.username,
    sublabel: `@${d.peer.username}`,
    path: `/dm/${d.peer.username}`,
  }))
  const personItems: PaletteItem[] = people.map((u) => ({
    type: 'person',
    id: u.id,
    label: u.display_name || u.username,
    sublabel: `@${u.username}`,
    path: `/dm/${u.username}`,
  }))

  const all = [...channelItems, ...dmItems, ...personItems]

  let results: PaletteItem[]
  if (query.trim() === '') {
    const recents = loadRecents()
    const byKey = new Map(all.map((i) => [`${i.type}:${i.id}`, i]))
    results = recents.map((r) => byKey.get(`${r.type}:${r.id}`) ?? { ...r }).filter(Boolean) as PaletteItem[]
    if (results.length === 0) results = channelItems.slice(0, 10)
  } else {
    const recentKeys = new Set(loadRecents().map((r) => `${r.type}:${r.id}`))
    const scored = fuzzySearch(query, all, (i) => i.label + ' ' + (i.sublabel ?? ''), 20)
    results = scored
      .map((m) => ({ ...m, score: m.score + (recentKeys.has(`${m.item.type}:${m.item.id}`) ? 3 : 0) }))
      .sort((a, b) => b.score - a.score)
      .map((m) => m.item)
  }

  const activate = (item: PaletteItem, openAsDM: boolean) => {
    pushRecent({ type: item.type, id: item.id, label: item.label, path: item.path })
    navigate(openAsDM && item.type === 'person' ? `/dm/${item.sublabel?.replace('@', '')}` : item.path)
    close()
  }

  const handleKeyDown = (e: React.KeyboardEvent) => {
    if (e.key === 'ArrowDown') {
      e.preventDefault()
      setSelected((s) => Math.min(results.length - 1, s + 1))
    } else if (e.key === 'ArrowUp') {
      e.preventDefault()
      setSelected((s) => Math.max(0, s - 1))
    } else if (e.key === 'Enter') {
      e.preventDefault()
      const item = results[selected]
      if (item) activate(item, e.metaKey || e.ctrlKey)
    }
  }

  return (
    <Modal open={open} onClose={close} labelledBy="command-palette-label" initialFocusRef={inputRef}>
      <div className="p-2">
        <h2 id="command-palette-label" className="sr-only">
          Command palette
        </h2>
        <input
          ref={inputRef}
          value={query}
          onChange={(e) => {
            setQuery(e.target.value)
            setSelected(0)
          }}
          onKeyDown={handleKeyDown}
          placeholder="Jump to a channel, DM, or person…"
          className="w-full rounded-md border border-rule bg-paper px-3 py-2 text-sm text-ink outline-none focus-visible:ring-2 focus-visible:ring-teal"
        />
      </div>
      <ul className="max-h-80 overflow-y-auto px-2 pb-2">
        {results.length === 0 && <li className="px-2 py-2 text-sm text-ink-3">No matches.</li>}
        {results.map((item, i) => (
          <li key={`${item.type}:${item.id}`}>
            <button
              type="button"
              onClick={(e) => activate(item, e.metaKey || e.ctrlKey)}
              className={
                'flex w-full items-center gap-2 rounded-md px-2 py-1.5 text-left text-sm ' +
                (i === selected ? 'bg-teal-soft text-teal' : 'text-ink-2 hover:bg-paper-3')
              }
            >
              <span className="truncate">{item.label}</span>
              {item.sublabel && <span className="text-xs text-ink-3">{item.sublabel}</span>}
            </button>
          </li>
        ))}
      </ul>
    </Modal>
  )
}
