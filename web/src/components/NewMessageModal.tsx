import { useMemo, useRef, useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { api, type User } from '../lib/api'
import { useAuth } from '../hooks/useAuth'
import { Modal } from './Modal'
import { Avatar } from './Avatar'

/** Wraps the substring of `name` matching `query` in a <mark>, mirroring the mockup's
 * highlighted-match treatment in the New Message and search result lists. */
function highlightMatch(name: string, query: string) {
  if (!query) return name
  const i = name.toLowerCase().indexOf(query.toLowerCase())
  if (i < 0) return name
  return (
    <>
      {name.slice(0, i)}
      <mark className="rounded-sm bg-pollen-soft text-[#7A4E00]">{name.slice(i, i + query.length)}</mark>
      {name.slice(i + query.length)}
    </>
  )
}

function PresenceDot({ online }: { online?: boolean }) {
  return (
    <span
      className={
        'h-[7px] w-[7px] shrink-0 rounded-full border-[1.5px] ' +
        (online ? 'border-teal bg-teal' : 'border-ink-3 bg-transparent')
      }
      aria-hidden
    />
  )
}

function PersonRow({ user, query, isSelf, onClick }: { user: User; query: string; isSelf: boolean; onClick: () => void }) {
  const name = user.display_name || user.username
  return (
    <li>
      <button
        type="button"
        onClick={onClick}
        className="prow flex w-full items-center gap-2.5 rounded-md px-2.5 py-2 text-left hover:bg-teal-soft"
      >
        <PresenceDot online={user.online} />
        <Avatar name={name} color={user.avatar_color} avatarUrl={user.avatar_url} size={26} />
        <span className="truncate text-[14px] font-semibold text-ink">{highlightMatch(name, query)}</span>
        <span className="ml-auto shrink-0 text-[12px] text-ink-3">
          {isSelf ? 'You' : user.online ? 'Online' : 'Offline'}
        </span>
      </button>
    </li>
  )
}

export function NewMessageModal({ open, onClose }: { open: boolean; onClose: () => void }) {
  const [query, setQuery] = useState('')
  const [selected, setSelected] = useState<User[]>([])
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
  const inputRef = useRef<HTMLInputElement>(null)
  const nav = useNavigate()
  const qc = useQueryClient()
  const { data: auth } = useAuth()
  const currentUserId = auth?.user.id

  const recentQuery = useQuery({
    queryKey: ['dms', 'recent'],
    queryFn: api.recentDMs,
    enabled: open && query.trim() === '',
  })
  const allMembersQuery = useQuery({
    queryKey: ['users', 'search', ''],
    queryFn: () => api.listUsers({ limit: 50 }),
    enabled: open && query.trim() === '',
  })
  const searchQuery = useQuery({
    queryKey: ['users', 'search', query],
    queryFn: () => api.listUsers({ q: query, limit: 20 }),
    enabled: open && query.trim() !== '',
  })

  const selectedIds = useMemo(() => new Set(selected.map((u) => u.id)), [selected])

  const recent = (recentQuery.data?.data ?? []).filter((u) => !selectedIds.has(u.id))
  const recentIds = new Set(recent.map((u) => u.id))
  const allMembers = (allMembersQuery.data?.data ?? []).filter((u) => !selectedIds.has(u.id) && !recentIds.has(u.id))
  const matches = (searchQuery.data?.data ?? []).filter((u) => !selectedIds.has(u.id))

  // Guards go()'s async continuation: if the user cancels while createDM is in flight, the
  // modal unmounts (Modal renders null when closed), but the pending promise still resolves
  // later — without this, its .then would silently navigate into the DM the user just
  // canceled out of.
  const cancelledRef = useRef(false)

  function reset() {
    setQuery('')
    setSelected([])
    setError('')
  }

  function cancel() {
    cancelledRef.current = true
    reset()
    onClose()
  }

  function addUser(u: User) {
    setSelected((prev) => [...prev, u])
    setQuery('')
    inputRef.current?.focus()
  }

  function removeUser(id: string) {
    setSelected((prev) => prev.filter((u) => u.id !== id))
  }

  async function go() {
    if (selected.length === 0) return
    cancelledRef.current = false
    setBusy(true)
    setError('')
    try {
      const r = await api.createDM(selected.map((u) => u.id))
      if (cancelledRef.current) return
      await qc.invalidateQueries({ queryKey: ['dms'] })
      reset()
      onClose()
      nav(`/dm/id/${r.channel.id}`)
    } catch {
      if (!cancelledRef.current) setError('Could not start the conversation.')
    } finally {
      if (!cancelledRef.current) setBusy(false)
    }
  }

  const searching = query.trim() !== ''
  const isEmpty = searching ? matches.length === 0 : recent.length === 0 && allMembers.length === 0

  return (
    <Modal open={open} onClose={cancel} labelledBy="new-message-label" initialFocusRef={inputRef}>
      <h2 id="new-message-label" className="sr-only">
        New message
      </h2>

      <div className="flex items-center gap-2.5 border-b border-rule px-4 py-3.5">
        <span className="lbl shrink-0 tracking-[.08em]">To</span>
        <div className="flex flex-1 flex-wrap items-center gap-1.5">
          {selected.map((u) => (
            <span
              key={u.id}
              className="flex items-center gap-1.5 rounded-md border border-rule bg-paper-2 py-1 pl-1.5 pr-2 text-xs"
            >
              {u.display_name || u.username}
              <button
                type="button"
                aria-label={`Remove ${u.username}`}
                onClick={() => removeUser(u.id)}
                className="text-ink-3 hover:text-ink"
              >
                ×
              </button>
            </span>
          ))}
          <input
            ref={inputRef}
            value={query}
            onChange={(e) => setQuery(e.target.value)}
            placeholder="Type a name, email, or username…"
            className="min-w-[110px] flex-1 bg-transparent text-base text-ink outline-none placeholder:text-ink-3"
          />
        </div>
        <kbd className="shrink-0 rounded border border-rule px-1 font-mono text-[9px] text-ink-3">ESC</kbd>
      </div>

      <div className="max-h-[52vh] overflow-y-auto p-1.5">
        {isEmpty && (
          <p className="px-2.5 py-4 text-sm text-ink-3">
            {searching ? `Nobody matches "${query}".` : 'No one to suggest yet.'}
          </p>
        )}

        {!searching && recent.length > 0 && (
          <>
            <div className="lbl px-2.5 pb-1 pt-2">Suggested / Recent</div>
            <ul>
              {recent.map((u) => (
                <PersonRow key={u.id} user={u} query="" isSelf={u.id === currentUserId} onClick={() => addUser(u)} />
              ))}
            </ul>
          </>
        )}

        {!searching && allMembers.length > 0 && (
          <>
            <div className="lbl px-2.5 pb-1 pt-2">All members</div>
            <ul>
              {allMembers.map((u) => (
                <PersonRow key={u.id} user={u} query="" isSelf={u.id === currentUserId} onClick={() => addUser(u)} />
              ))}
            </ul>
          </>
        )}

        {searching && matches.length > 0 && (
          <>
            <div className="lbl px-2.5 pb-1 pt-2">Matches</div>
            <ul>
              {matches.map((u) => (
                <PersonRow key={u.id} user={u} query={query} isSelf={u.id === currentUserId} onClick={() => addUser(u)} />
              ))}
            </ul>
          </>
        )}
      </div>

      {error && <p className="px-4 pb-2 text-sm text-red-600">{error}</p>}

      <div className="flex justify-end gap-2 border-t border-rule bg-paper-2 px-3.5 py-2">
        <button
          type="button"
          onClick={cancel}
          className="flex items-center rounded-md px-2 py-1.5 text-[12.5px] text-ink-2 hover:bg-paper-3 hover:text-ink"
        >
          Cancel
        </button>
        <button
          type="button"
          onClick={go}
          disabled={selected.length === 0 || busy}
          className="flex items-center gap-1.5 rounded-md bg-teal px-3 py-1.5 text-[13px] font-semibold text-white hover:bg-[#0B564B] disabled:opacity-50"
        >
          {busy ? 'Starting…' : 'Go!'}
          <kbd className="font-mono text-[8.5px] opacity-75">↵</kbd>
        </button>
      </div>
    </Modal>
  )
}
