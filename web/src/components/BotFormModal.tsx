import { useMutation, useQueryClient } from '@tanstack/react-query'
import { useState } from 'react'
import { api, ApiError } from '../lib/api'
import { Modal } from './Modal'

/** Create panel for a bot. On successful create it stays open and flips in place to a "copy
 * your bearer token" success state, mirroring OutgoingWebhookFormModal's shown-once secret
 * convention. Bots have no edit form — name/description are set once at creation. */
export function BotFormModal({ open, onClose }: { open: boolean; onClose: () => void }) {
  const qc = useQueryClient()
  const [name, setName] = useState('')
  const [description, setDescription] = useState('')
  const [error, setError] = useState<string | null>(null)
  const [issuedToken, setIssuedToken] = useState<string | null>(null)
  const [copied, setCopied] = useState(false)

  function onError(e: unknown) {
    setError(e instanceof ApiError ? e.message : 'Something went wrong.')
  }

  const create = useMutation({
    mutationFn: () => api.createBot({ name: name.trim(), description: description.trim() }),
    onSuccess: (res) => {
      setIssuedToken(res.bot.token ?? null)
      qc.invalidateQueries({ queryKey: ['bots'] })
    },
    onError,
  })

  function onSubmit(e: React.FormEvent) {
    e.preventDefault()
    setError(null)
    if (!name.trim()) {
      setError('Name is required.')
      return
    }
    create.mutate()
  }

  function handleClose() {
    setIssuedToken(null)
    setCopied(false)
    setError(null)
    setName('')
    setDescription('')
    onClose()
  }

  return (
    <Modal open={open} onClose={handleClose} labelledBy="bot-form-title">
      {issuedToken ? (
        <div className="max-h-[80vh] overflow-y-auto p-5">
          <h2 id="bot-form-title" className="font-display text-lg font-semibold text-ink">
            Bot ready
          </h2>
          <p className="mt-2 text-sm text-ink-2">
            This is the only time the full bearer token will be shown. Copy it now — it can't be
            retrieved again afterward.
          </p>
          <code className="mt-4 block break-all rounded border border-rule bg-paper px-3 py-2 text-sm text-ink">
            {issuedToken}
          </code>

          <p className="mt-4 text-xs font-medium uppercase tracking-wide text-ink-3">Use it</p>
          <p className="mt-1 text-xs text-ink-3">
            Send it as <code className="text-ink-2">Authorization: Bearer {'<token>'}</code> on any
            request to <code className="text-ink-2">POST /api/v1/channels/:id/messages</code> — the
            bot posts through the same path a regular user does.
          </p>

          <div className="mt-5 flex justify-end gap-2">
            <button
              type="button"
              className="rounded border border-rule px-3 py-1.5 text-sm text-ink hover:bg-paper-3"
              onClick={async () => {
                await navigator.clipboard.writeText(issuedToken)
                setCopied(true)
              }}
            >
              {copied ? 'Copied' : 'Copy'}
            </button>
            <button
              type="button"
              className="rounded bg-teal px-3 py-1.5 text-sm text-white hover:opacity-90"
              onClick={handleClose}
            >
              Done
            </button>
          </div>
        </div>
      ) : (
        <form onSubmit={onSubmit} className="max-h-[80vh] overflow-y-auto p-5">
          <h2 id="bot-form-title" className="font-display text-lg font-semibold text-ink">
            New bot
          </h2>

          {error && (
            <div className="mt-3 rounded border border-red-200 bg-red-50 px-3 py-2 text-sm text-red-700">
              {error}
            </div>
          )}

          <div className="mt-4 space-y-4">
            <label className="block">
              <span className="mb-1 block text-xs font-medium uppercase tracking-wide text-ink-3">Name</span>
              <input
                autoFocus
                value={name}
                onChange={(e) => setName(e.target.value)}
                placeholder="e.g. Deploy Bot"
                className="w-full rounded border border-rule bg-paper px-3 py-2 text-sm text-ink outline-none focus:border-teal"
              />
            </label>
            <label className="block">
              <span className="mb-1 block text-xs font-medium uppercase tracking-wide text-ink-3">
                Description
              </span>
              <input
                value={description}
                onChange={(e) => setDescription(e.target.value)}
                placeholder="What does this bot do?"
                className="w-full rounded border border-rule bg-paper px-3 py-2 text-sm text-ink outline-none focus:border-teal"
              />
            </label>
          </div>

          <div className="mt-5 flex gap-2">
            <button
              type="submit"
              disabled={create.isPending}
              className="rounded bg-teal px-4 py-2 text-sm font-medium text-white hover:opacity-90 disabled:opacity-50"
            >
              {create.isPending ? 'Creating…' : 'Create bot'}
            </button>
            <button
              type="button"
              onClick={handleClose}
              className="rounded border border-rule px-3 py-2 text-sm text-ink-2 hover:bg-paper-3"
            >
              Cancel
            </button>
          </div>
        </form>
      )}
    </Modal>
  )
}
