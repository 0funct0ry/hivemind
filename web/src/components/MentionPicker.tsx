import { useEffect, useRef, useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { api, type User } from '../lib/api'

export interface MentionCandidate {
  key: string
  label: string
  sublabel?: string
  warning?: string
  isSpecial?: boolean
  user?: User
}

export function useMentionCandidates(channelId: string, query: string) {
  const usersQuery = useQuery({
    queryKey: ['mention-users', channelId, query],
    queryFn: () => api.listUsers({ q: query, channelId, limit: 8 }),
  })

  const special: MentionCandidate[] = ['channel', 'here']
    .filter((k) => k.startsWith(query.toLowerCase()))
    .map((k) => ({
      key: k,
      label: `@${k}`,
      isSpecial: true,
      warning: k === 'channel' ? 'Notifies everyone in this channel' : 'Notifies everyone currently online',
    }))

  const userCandidates: MentionCandidate[] = (usersQuery.data?.data ?? []).map((u) => ({
    key: u.username,
    label: u.display_name || u.username,
    sublabel: `@${u.username}`,
    user: u,
  }))

  return [...special, ...userCandidates]
}

export function MentionPicker({
  candidates,
  onSelect,
  onDismiss,
}: {
  candidates: MentionCandidate[]
  onSelect: (c: MentionCandidate) => void
  onDismiss: () => void
}) {
  const [index, setIndex] = useState(0)
  const containerRef = useRef<HTMLDivElement>(null)

  useEffect(() => {
    setIndex(0)
  }, [candidates.length])

  useEffect(() => {
    const handler = (e: KeyboardEvent) => {
      if (e.key === 'ArrowDown') {
        e.preventDefault()
        setIndex((i) => Math.min(i + 1, candidates.length - 1))
      } else if (e.key === 'ArrowUp') {
        e.preventDefault()
        setIndex((i) => Math.max(i - 1, 0))
      } else if (e.key === 'Tab' || e.key === 'Enter') {
        e.preventDefault()
        if (candidates[index]) onSelect(candidates[index])
      } else if (e.key === 'Escape') {
        e.preventDefault()
        onDismiss()
      }
    }
    window.addEventListener('keydown', handler, true)
    return () => window.removeEventListener('keydown', handler, true)
  }, [candidates, index, onSelect, onDismiss])

  if (candidates.length === 0) return null

  return (
    <div
      ref={containerRef}
      role="listbox"
      className="absolute bottom-full left-2 z-20 mb-1 max-h-64 w-72 overflow-y-auto rounded-md border border-rule bg-paper shadow-lg"
    >
      {candidates.map((c, i) => (
        <button
          key={c.key}
          type="button"
          role="option"
          aria-selected={i === index}
          onMouseEnter={() => setIndex(i)}
          onClick={() => onSelect(c)}
          className={
            'flex w-full items-center gap-2 px-3 py-2 text-left text-sm ' +
            (i === index ? 'bg-teal-soft' : 'hover:bg-paper-2')
          }
        >
          {c.user ? (
            <span
              className="h-5 w-5 shrink-0 rounded-full"
              style={{ backgroundColor: c.user.avatar_color }}
              aria-hidden
            />
          ) : (
            <span className="flex h-5 w-5 shrink-0 items-center justify-center rounded-full bg-pollen-soft text-pollen">
              @
            </span>
          )}
          <span className="flex-1 truncate">
            <span className="font-medium text-ink">{c.label}</span>
            {c.sublabel && <span className="ml-1 text-ink-3">{c.sublabel}</span>}
          </span>
          {c.warning && <span className="text-[11px] text-pollen">{c.warning}</span>}
        </button>
      ))}
    </div>
  )
}
