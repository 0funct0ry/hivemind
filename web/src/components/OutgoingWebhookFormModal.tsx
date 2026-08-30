import { useMutation, useQueryClient } from '@tanstack/react-query'
import { useState } from 'react'
import { api, ApiError, type Channel, type OutgoingWebhook, type OutgoingWebhookPatchBody } from '../lib/api'
import { Modal } from './Modal'

interface FormState {
  channel_id: string
  name: string
  target_url: string
  keyword_filter: string
}

function emptyForm(): FormState {
  return { channel_id: '', name: '', target_url: '', keyword_filter: '' }
}

const VERIFY_SNIPPET = `const crypto = require('crypto')

function isValid(rawBody, signatureHeader, secret) {
  const expected = 'sha256=' + crypto.createHmac('sha256', secret).update(rawBody).digest('hex')
  return crypto.timingSafeEqual(Buffer.from(expected), Buffer.from(signatureHeader))
}`

/** Create/edit panel for an outgoing webhook. On successful create/regenerate it stays open and
 * flips in place to a "copy your signing secret" success state, mirroring WebhookFormModal's
 * shown-once convention for incoming webhooks. */
export function OutgoingWebhookFormModal({
  open,
  onClose,
  webhook,
  ownedChannels,
}: {
  open: boolean
  onClose: () => void
  webhook: OutgoingWebhook | null
  ownedChannels: Channel[]
}) {
  const qc = useQueryClient()
  const isEdit = webhook !== null
  const [form, setForm] = useState<FormState>(() =>
    webhook
      ? {
          channel_id: webhook.channel_id,
          name: webhook.name,
          target_url: webhook.target_url,
          keyword_filter: webhook.keyword_filter,
        }
      : emptyForm(),
  )
  const [error, setError] = useState<string | null>(null)
  const [issuedSecret, setIssuedSecret] = useState<string | null>(null)
  const [copied, setCopied] = useState(false)

  function onError(e: unknown) {
    setError(e instanceof ApiError ? e.message : 'Something went wrong.')
  }

  const create = useMutation({
    mutationFn: () =>
      api.createOutgoingWebhook(form.channel_id, {
        name: form.name.trim(),
        target_url: form.target_url.trim(),
        keyword_filter: form.keyword_filter.trim(),
      }),
    onSuccess: (res) => {
      setIssuedSecret(res.webhook.secret ?? null)
      qc.invalidateQueries({ queryKey: ['outgoing-webhooks'] })
    },
    onError,
  })

  const update = useMutation({
    mutationFn: () => {
      const patch: OutgoingWebhookPatchBody = {
        name: form.name.trim(),
        target_url: form.target_url.trim(),
        keyword_filter: form.keyword_filter.trim(),
      }
      return api.updateOutgoingWebhook(webhook!.id, patch)
    },
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['outgoing-webhooks'] })
      onClose()
    },
    onError,
  })

  function onSubmit(e: React.FormEvent) {
    e.preventDefault()
    setError(null)
    if (!form.name.trim()) {
      setError('Name is required.')
      return
    }
    if (!form.target_url.trim()) {
      setError('Target URL is required.')
      return
    }
    if (!isEdit && !form.channel_id) {
      setError('Pick a source channel.')
      return
    }
    if (isEdit) update.mutate()
    else create.mutate()
  }

  function handleClose() {
    setIssuedSecret(null)
    setCopied(false)
    setError(null)
    onClose()
  }

  const busy = create.isPending || update.isPending

  return (
    <Modal open={open} onClose={handleClose} labelledBy="outgoing-webhook-form-title">
      {issuedSecret ? (
        <div className="max-h-[80vh] overflow-y-auto p-5">
          <h2 id="outgoing-webhook-form-title" className="font-display text-lg font-semibold text-ink">
            Outgoing webhook ready
          </h2>
          <p className="mt-2 text-sm text-ink-2">
            This is the only time the full signing secret will be shown. Copy it now and give it
            to whoever owns the receiving endpoint — it can't be retrieved again afterward.
          </p>
          <code className="mt-4 block break-all rounded border border-rule bg-paper px-3 py-2 text-sm text-ink">
            {issuedSecret}
          </code>

          <p className="mt-4 text-xs font-medium uppercase tracking-wide text-ink-3">
            Verify the signature
          </p>
          <p className="mt-1 text-xs text-ink-3">
            Every delivery carries <code className="text-ink-2">X-Hivemind-Signature</code> —
            HMAC-SHA256 over the raw request body using this secret.
          </p>
          <pre className="mt-2 overflow-x-auto rounded border border-rule bg-paper px-3 py-2 font-mono text-[11px] text-ink-2">
            {VERIFY_SNIPPET}
          </pre>

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
          <h2 id="outgoing-webhook-form-title" className="font-display text-lg font-semibold text-ink">
            {isEdit ? 'Edit outgoing webhook' : 'New outgoing webhook'}
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
                value={form.name}
                onChange={(e) => setForm((f) => ({ ...f, name: e.target.value }))}
                placeholder="e.g. Ops relay"
                className="w-full rounded border border-rule bg-paper px-3 py-2 text-sm text-ink outline-none focus:border-teal"
              />
            </label>

            {!isEdit && (
              <label className="block">
                <span className="mb-1 block text-xs font-medium uppercase tracking-wide text-ink-3">
                  Source channel
                </span>
                <select
                  value={form.channel_id}
                  onChange={(e) => setForm((f) => ({ ...f, channel_id: e.target.value }))}
                  className="w-full rounded border border-rule bg-paper px-3 py-2 text-sm text-ink outline-none focus:border-teal"
                >
                  <option value="">Select a channel…</option>
                  {ownedChannels.map((c) => (
                    <option key={c.id} value={c.id}>
                      #{c.slug ?? c.name}
                    </option>
                  ))}
                </select>
              </label>
            )}

            <label className="block">
              <span className="mb-1 block text-xs font-medium uppercase tracking-wide text-ink-3">
                Target URL
              </span>
              <input
                value={form.target_url}
                onChange={(e) => setForm((f) => ({ ...f, target_url: e.target.value }))}
                placeholder="https://example.com/hooks/hivemind"
                className="w-full rounded border border-rule bg-paper px-3 py-2 text-sm text-ink outline-none focus:border-teal"
              />
              <span className="mt-1 block text-xs text-ink-3">
                Must be https:// and resolve to a public host — not loopback or a private-range
                address.
              </span>
            </label>

            <label className="block">
              <span className="mb-1 block text-xs font-medium uppercase tracking-wide text-ink-3">
                Keyword filter
              </span>
              <input
                value={form.keyword_filter}
                onChange={(e) => setForm((f) => ({ ...f, keyword_filter: e.target.value }))}
                placeholder="e.g. outage"
                className="w-full rounded border border-rule bg-paper px-3 py-2 text-sm text-ink outline-none focus:border-teal"
              />
              <span className="mt-1 block text-xs text-ink-3">
                Leave blank to fire on every message in this channel.
              </span>
            </label>
          </div>

          <div className="mt-5 flex gap-2">
            <button
              type="submit"
              disabled={busy}
              className="rounded bg-teal px-4 py-2 text-sm font-medium text-white hover:opacity-90 disabled:opacity-50"
            >
              {busy ? 'Saving…' : isEdit ? 'Save changes' : 'Create outgoing webhook'}
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
