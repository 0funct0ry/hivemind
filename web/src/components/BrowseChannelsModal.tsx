import { useState } from 'react'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { useNavigate } from 'react-router-dom'
import { api, type Channel } from '../lib/api'
import { Modal } from './Modal'

export function BrowseChannelsModal({ open, onClose }: { open: boolean; onClose: () => void }) {
  const [q, setQ] = useState('')
  const [joining, setJoining] = useState<string | null>(null)
  const { data } = useQuery({ queryKey: ['joinable-channels'], queryFn: api.listJoinableChannels, enabled: open })
  const nav = useNavigate()
  const qc = useQueryClient()
  const list = (data?.data ?? []).filter((c) => c.name.toLowerCase().includes(q.toLowerCase()))

  async function join(c: Channel) {
    setJoining(c.id)
    await api.joinChannel(c.id)
    await qc.invalidateQueries({ queryKey: ['channels'] })
    setJoining(null)
    onClose()
    nav(`/c/${c.slug}`)
  }

  return (
    <Modal open={open} onClose={onClose} labelledBy="browse-channels-title">
      <div className="p-5">
        <h2 id="browse-channels-title" className="font-display text-lg font-semibold">
          Browse channels
        </h2>
        <input
          autoFocus
          value={q}
          onChange={(e) => setQ(e.target.value)}
          placeholder="Filter channels…"
          className="mt-4 w-full rounded border border-rule bg-paper p-2"
        />
        <div className="mt-3 max-h-72 overflow-auto">
          {list.length === 0 ? (
            <p className="py-5 text-sm text-ink-3">No public channels to join</p>
          ) : (
            list.map((c) => (
              <button
                key={c.id}
                onClick={() => join(c)}
                disabled={joining !== null}
                className="flex w-full items-center gap-2 border-b border-rule p-2 text-left hover:bg-paper-3"
              >
                <span className="font-semibold"># {c.name}</span>
                <span className="truncate text-xs text-ink-3">{c.topic}</span>
                <span className="ml-auto text-xs text-ink-3">{c.member_count}</span>
                {joining === c.id && <span>…</span>}
              </button>
            ))
          )}
        </div>
        <div className="mt-4">
          <button
            type="button"
            onClick={onClose}
            className="rounded border border-rule px-3 py-2 text-sm text-ink-2 hover:bg-paper-3"
          >
            Cancel
          </button>
        </div>
      </div>
    </Modal>
  )
}
