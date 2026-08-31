import { useMutation, useQueryClient } from '@tanstack/react-query'
import { useState } from 'react'
import { api, ApiError, type Bot } from '../lib/api'
import { Modal } from './Modal'

interface FormState {
  trigger: string
  bot_id: string
  description: string
  syntax_hint: string
  webhook_url: string
  admin_only: boolean
}

function emptyForm(): FormState {
  return { trigger: '', bot_id: '', description: '', syntax_hint: '', webhook_url: '', admin_only: false }
}

/** Create panel for a slash command. On successful create it stays open and flips in place to a
 * "copy your signing secret" success state, mirroring OutgoingWebhookFormModal exactly —
 * trigger and bot_id are immutable after creation, so this is create-only, matching
 * SPEC.md §4.12. */
export function SlashCommandFormModal({
  open,
  onClose,
  bots,
}: {
  open: boolean
  onClose: () => void
  bots: Bot[]
}) {
  const qc = useQueryClient()
  const [form, setForm] = useState<FormState>(emptyForm)
  const [error, setError] = useState<string | null>(null)
  const [issuedSecret, setIssuedSecret] = useState<string | null>(null)
  const [copied, setCopied] = useState(false)

  function onError(e: unknown) {
    setError(e instanceof ApiError ? e.message : 'Something went wrong.')
  }

  const create = useMutation({
    mutationFn: () =>
      api.createSlashCommand({
        trigger: form.trigger.trim(),
        bot_id: form.bot_id,
        description: form.description.trim(),
        syntax_hint: form.syntax_hint.trim(),
        webhook_url: form.webhook_url.trim(),
        admin_only: form.admin_only,
      }),
    onSuccess: (res) => {
      setIssuedSecret(res.command.secret ?? null)
      qc.invalidateQueries({ queryKey: ['slash-commands-admin'] })
      qc.invalidateQueries({ queryKey: ['slash-commands'] })
    },
    onError,
  })

  function onSubmit(e: React.FormEvent) {
    e.preventDefault()
    setError(null)
    if (!form.trigger.trim().startsWith('/')) {
      setError('Trigger must start with /.')
      return
    }
    if (!form.bot_id) {
      setError('Pick a bot to post as.')
      return
    }
    if (!form.description.trim()) {
      setError('Description is required.')
      return
    }
    if (!form.webhook_url.trim()) {
      setError('Webhook URL is required.')
      return
    }
    create.mutate()
  }

  function handleClose() {
    setIssuedSecret(null)
    setCopied(false)
    setError(null)
    setForm(emptyForm())
    onClose()
  }

  return (
    <Modal open={open} onClose={handleClose} labelledBy="slash-command-form-title">
      {issuedSecret ? (
        <div className="max-h-[80vh] overflow-y-auto p-5">
          <h2 id="slash-command-form-title" className="font-display text-lg font-semibold text-ink">
            Slash command ready
          </h2>
          <p className="mt-2 text-sm text-ink-2">
            This is the only time the full signing secret will be shown. Copy it now — it can't be
            retrieved again afterward.
          </p>
          <code className="mt-4 block break-all rounded border border-rule bg-paper px-3 py-2 text-sm text-ink">
            {issuedSecret}
          </code>

          <div className="mt-5 flex justify-end gap-2">
            <button
              type="button"
              className="rounded border border-rule px-3 py-1.5 text-sm text-ink hover:bg-paper-3"
              onClick={async () => {
                await navigator.clipboard.writeText(issuedSecret)
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
          <h2 id="slash-command-form-title" className="font-display text-lg font-semibold text-ink">
            New command
          </h2>

          {error && (
            <div className="mt-3 rounded border border-red-200 bg-red-50 px-3 py-2 text-sm text-red-700">
              {error}
            </div>
          )}

          <div className="mt-4 space-y-4">
            <label className="block">
              <span className="mb-1 block text-xs font-medium uppercase tracking-wide text-ink-3">Trigger</span>
              <input
                autoFocus
                value={form.trigger}
                onChange={(e) => setForm((f) => ({ ...f, trigger: e.target.value }))}
                placeholder="/deploy"
                className="w-full rounded border border-rule bg-paper px-3 py-2 font-mono text-sm text-ink outline-none focus:border-teal"
              />
            </label>

            <label className="block">
              <span className="mb-1 block text-xs font-medium uppercase tracking-wide text-ink-3">Post as</span>
              <select
                value={form.bot_id}
                onChange={(e) => setForm((f) => ({ ...f, bot_id: e.target.value }))}
                className="w-full rounded border border-rule bg-paper px-3 py-2 text-sm text-ink outline-none focus:border-teal"
              >
                <option value="">Select a bot…</option>
                {bots.map((b) => (
                  <option key={b.user_id} value={b.user_id}>
                    {b.display_name}
                  </option>
                ))}
              </select>
            </label>

            <label className="block">
              <span className="mb-1 block text-xs font-medium uppercase tracking-wide text-ink-3">
                Description
              </span>
              <input
                value={form.description}
                onChange={(e) => setForm((f) => ({ ...f, description: e.target.value }))}
                placeholder="Shown in the autocomplete menu"
                className="w-full rounded border border-rule bg-paper px-3 py-2 text-sm text-ink outline-none focus:border-teal"
              />
            </label>

            <label className="block">
              <span className="mb-1 block text-xs font-medium uppercase tracking-wide text-ink-3">
                Syntax hint
              </span>
              <input
                value={form.syntax_hint}
                onChange={(e) => setForm((f) => ({ ...f, syntax_hint: e.target.value }))}
                placeholder="<environment> <branch>"
                className="w-full rounded border border-rule bg-paper px-3 py-2 font-mono text-sm text-ink outline-none focus:border-teal"
              />
            </label>

            <label className="block">
              <span className="mb-1 block text-xs font-medium uppercase tracking-wide text-ink-3">
                Webhook URL
              </span>
              <input
                value={form.webhook_url}
                onChange={(e) => setForm((f) => ({ ...f, webhook_url: e.target.value }))}
                placeholder="https://example.com/hooks/deploy"
                className="w-full rounded border border-rule bg-paper px-3 py-2 text-sm text-ink outline-none focus:border-teal"
              />
              <span className="mt-1 block text-xs text-ink-3">
                Must be https:// and resolve to a public host — not loopback or a private-range
                address.
              </span>
            </label>

            <label className="flex items-center gap-2">
              <input
                type="checkbox"
                checked={form.admin_only}
                onChange={(e) => setForm((f) => ({ ...f, admin_only: e.target.checked }))}
              />
              <span className="text-sm text-ink">Restrict execution to owners/admins</span>
            </label>
          </div>

          <div className="mt-5 flex gap-2">
            <button
              type="submit"
              disabled={create.isPending}
              className="rounded bg-teal px-4 py-2 text-sm font-medium text-white hover:opacity-90 disabled:opacity-50"
            >
              {create.isPending ? 'Creating…' : 'Create command'}
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
