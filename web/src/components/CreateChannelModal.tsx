import { useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { useQueryClient } from '@tanstack/react-query'
import { api, ApiError } from '../lib/api'
import { Modal } from './Modal'

export function CreateChannelModal({ open, onClose }: { open: boolean; onClose: () => void }) {
  const [name, setName] = useState('')
  const [topic, setTopic] = useState('')
  const [kind, setKind] = useState<'public' | 'private'>('public')
  const [error, setError] = useState('')
  const [busy, setBusy] = useState(false)
  const nav = useNavigate()
  const qc = useQueryClient()
  const valid = /^[a-z0-9][a-z0-9-]{1,31}$/.test(name)

  function reset() {
    setName('')
    setTopic('')
    setKind('public')
    setError('')
  }

  async function submit(e: React.FormEvent) {
    e.preventDefault()
    if (!valid) return
    setBusy(true)
    setError('')
    try {
      const r = await api.createChannel({ kind, slug: name, name, topic: topic.trim() || undefined })
      await qc.invalidateQueries({ queryKey: ['channels'] })
      reset()
      onClose()
      nav(`/c/${r.channel.slug}`)
    } catch (e) {
      setError(e instanceof ApiError ? e.message : 'Could not create channel.')
    } finally {
      setBusy(false)
    }
  }

  function cancel() {
    reset()
    onClose()
  }

  return (
    <Modal open={open} onClose={cancel} labelledBy="create-channel-title">
      <form onSubmit={submit} className="p-5">
        <h2 id="create-channel-title" className="font-display text-lg font-semibold">
          Create channel
        </h2>

        <label className="mt-4 block text-sm">
          Channel name
          <input
            autoFocus
            value={name}
            onChange={(e) => setName(e.target.value.toLowerCase())}
            className="mt-1 w-full rounded border border-rule bg-paper p-2"
          />
          {name && !valid && (
            <span className="mt-1 block text-xs text-red-600">Use 2–32 lowercase letters, numbers, or hyphens.</span>
          )}
        </label>

        <label className="mt-3 block text-sm">
          Topic <span className="text-ink-3">(optional)</span>
          <input
            value={topic}
            onChange={(e) => setTopic(e.target.value)}
            placeholder="What's this channel about?"
            className="mt-1 w-full rounded border border-rule bg-paper p-2"
          />
        </label>

        <div className="mt-3 flex gap-3 text-sm">
          <label>
            <input type="radio" checked={kind === 'public'} onChange={() => setKind('public')} /> Public
          </label>
          <label>
            <input type="radio" checked={kind === 'private'} onChange={() => setKind('private')} /> Private
          </label>
        </div>

        {error && <p className="mt-3 text-sm text-red-600">{error}</p>}

        <div className="mt-5 flex gap-2">
          <button
            type="submit"
            disabled={!valid || busy}
            className="rounded bg-teal px-3 py-2 text-sm text-white disabled:opacity-50"
          >
            {busy ? 'Creating…' : 'Create channel'}
          </button>
          <button
            type="button"
            onClick={cancel}
            className="rounded border border-rule px-3 py-2 text-sm text-ink-2 hover:bg-paper-3"
          >
            Cancel
          </button>
        </div>
      </form>
    </Modal>
  )
}
