import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useState } from 'react'
import { api, ApiError, type Channel, type Webhook, type WebhookPatchBody } from '../lib/api'
import { Modal } from './Modal'

const AVATAR_SWATCHES = ['#0E6E60', '#8A4B2A', '#3D5A8A', '#5A4A7A', '#C9860A']

const SEVERITIES = ['critical', 'warning', 'info', 'success', 'neutral'] as const

const GENERIC_EXAMPLE = `{
  "title": "SEV-2: elevated error rate",
  "severity": "critical",
  "fields": [
    { "label": "Service", "value": "checkout-api" },
    { "label": "Rate", "value": "14.2%" }
  ],
  "body": "Timeouts climbing over the last 5 minutes.",
  "display_name": "Datadog",
  "avatar_url": "https://example.com/datadog.png"
}`

const SLACK_EXAMPLE = `{
  "text": "Deploy finished",
  "username": "CI Bot",
  "icon_emoji": ":rocket:",
  "attachments": [
    {
      "color": "good",
      "title": "build #482 succeeded",
      "text": "main branch, 3m12s",
      "fields": [{ "title": "Duration", "value": "3m12s", "short": true }]
    }
  ]
}`

interface FormState {
  channel_id: string
  name: string
  format_preset: 'generic' | 'slack_compatible'
  default_display_name: string
  default_avatar_color: string
  allow_payload_override: boolean
  default_severity: string
  notify_channel_on_critical: boolean
  thread_id: string
  thread_mode: 'root' | 'thread'
}

function emptyForm(): FormState {
  return {
    channel_id: '',
    name: '',
    format_preset: 'generic',
    default_display_name: '',
    default_avatar_color: AVATAR_SWATCHES[0],
    allow_payload_override: true,
    default_severity: 'info',
    notify_channel_on_critical: false,
    thread_id: '',
    thread_mode: 'root',
  }
}

/** Create/edit panel for an incoming webhook. On successful create/regenerate it stays open and
 * flips in place to a "copy your ingest URL" success state, per the shown-once convention the
 * rest of the app uses for tokens (TokenWidgets.NewTokenModal). */
export function WebhookFormModal({
  open,
  onClose,
  webhook,
  ownedChannels,
}: {
  open: boolean
  onClose: () => void
  webhook: Webhook | null
  ownedChannels: Channel[]
}) {
  const qc = useQueryClient()
  const isEdit = webhook !== null
  const [form, setForm] = useState(() =>
    webhook
      ? {
          channel_id: webhook.channel_id,
          name: webhook.name,
          format_preset: webhook.format_preset,
          default_display_name: webhook.default_display_name,
          default_avatar_color: webhook.default_avatar_color || AVATAR_SWATCHES[0],
          allow_payload_override: webhook.allow_payload_override,
          default_severity: webhook.default_severity,
          notify_channel_on_critical: webhook.notify_channel_on_critical,
          thread_id: webhook.thread_id ?? '',
          thread_mode: (webhook.thread_id ? 'thread' : 'root') as 'root' | 'thread',
        }
      : emptyForm(),
  )
  const [showExample, setShowExample] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [issuedUrl, setIssuedUrl] = useState<string | null>(null)
  const [copied, setCopied] = useState(false)

  function onError(e: unknown) {
    setError(e instanceof ApiError ? e.message : 'Something went wrong.')
  }

  const create = useMutation({
    mutationFn: () =>
      api.createWebhook(form.channel_id, {
        name: form.name.trim(),
        format_preset: form.format_preset,
        default_display_name: (form.default_display_name ?? '').trim(),
        default_avatar_color: form.default_avatar_color,
        allow_payload_override: form.allow_payload_override,
        default_severity: form.default_severity,
        notify_channel_on_critical: form.notify_channel_on_critical,
        thread_id: form.thread_mode === 'thread' ? (form.thread_id ?? '').trim() : '',
      }),
    onSuccess: (res) => {
      setIssuedUrl(res.webhook.ingest_url ?? null)
      qc.invalidateQueries({ queryKey: ['webhooks'] })
    },
    onError,
  })

  const update = useMutation({
    mutationFn: () => {
      const patch: WebhookPatchBody = {
        name: form.name.trim(),
        format_preset: form.format_preset,
        default_display_name: (form.default_display_name ?? '').trim(),
        default_avatar_color: form.default_avatar_color,
        allow_payload_override: form.allow_payload_override,
        default_severity: form.default_severity,
        notify_channel_on_critical: form.notify_channel_on_critical,
        thread_id: form.thread_mode === 'thread' ? (form.thread_id ?? '').trim() : '',
      }
      return api.updateWebhook(webhook!.id, patch)
    },
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['webhooks'] })
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
    if (!isEdit && !form.channel_id) {
      setError('Pick a target channel.')
      return
    }
    if (isEdit) update.mutate()
    else create.mutate()
  }

  function handleClose() {
    setIssuedUrl(null)
    setCopied(false)
    setError(null)
    onClose()
  }

  const busy = create.isPending || update.isPending

  return (
    <Modal open={open} onClose={handleClose} labelledBy="webhook-form-title">
      {issuedUrl ? (
        <div className="p-5">
          <h2 id="webhook-form-title" className="font-display text-lg font-semibold text-ink">
            Webhook ready
          </h2>
          <p className="mt-2 text-sm text-ink-2">
            This is the only time the full URL — with its secret token — will be shown. Copy it
            now and add it to the sending system; it can't be retrieved again afterward.
          </p>
          <code className="mt-4 block break-all rounded border border-rule bg-paper px-3 py-2 text-sm text-ink">
            {issuedUrl}
          </code>
          <div className="mt-5 flex justify-end gap-2">
            <button
              type="button"
              className="rounded border border-rule px-3 py-1.5 text-sm text-ink hover:bg-paper-3"
              onClick={async () => {
                await navigator.clipboard.writeText(issuedUrl)
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
          <h2 id="webhook-form-title" className="font-display text-lg font-semibold text-ink">
            {isEdit ? 'Edit webhook' : 'New incoming webhook'}
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
                placeholder="e.g. Datadog alerts"
                className="w-full rounded border border-rule bg-paper px-3 py-2 text-sm text-ink outline-none focus:border-teal"
              />
            </label>

            {!isEdit && (
              <label className="block">
                <span className="mb-1 block text-xs font-medium uppercase tracking-wide text-ink-3">
                  Target channel
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
                Default display name
              </span>
              <input
                value={form.default_display_name}
                onChange={(e) => setForm((f) => ({ ...f, default_display_name: e.target.value }))}
                placeholder="Shown when the payload doesn't override it"
                className="w-full rounded border border-rule bg-paper px-3 py-2 text-sm text-ink outline-none focus:border-teal"
              />
            </label>

            <div>
              <span className="mb-1 block text-xs font-medium uppercase tracking-wide text-ink-3">
                Avatar color
              </span>
              <div className="flex gap-2">
                {AVATAR_SWATCHES.map((color) => (
                  <button
                    key={color}
                    type="button"
                    aria-label={color}
                    onClick={() => setForm((f) => ({ ...f, default_avatar_color: color }))}
                    className={
                      'h-7 w-7 rounded-full border-2 ' +
                      (form.default_avatar_color === color ? 'border-ink' : 'border-transparent')
                    }
                    style={{ backgroundColor: color }}
                  />
                ))}
              </div>
            </div>

            <div>
              <span className="mb-1 block text-xs font-medium uppercase tracking-wide text-ink-3">
                Payload format
              </span>
              <div className="flex gap-2">
                <button
                  type="button"
                  onClick={() => setForm((f) => ({ ...f, format_preset: 'generic' }))}
                  className={
                    'flex-1 rounded border px-3 py-2 text-sm ' +
                    (form.format_preset === 'generic'
                      ? 'border-teal bg-teal-soft text-teal'
                      : 'border-rule text-ink-2 hover:bg-paper-3')
                  }
                >
                  Generic alert
                </button>
                <button
                  type="button"
                  onClick={() => setForm((f) => ({ ...f, format_preset: 'slack_compatible' }))}
                  className={
                    'flex-1 rounded border px-3 py-2 text-sm ' +
                    (form.format_preset === 'slack_compatible'
                      ? 'border-teal bg-teal-soft text-teal'
                      : 'border-rule text-ink-2 hover:bg-paper-3')
                  }
                >
                  SaaS-compatible
                </button>
              </div>
              <button
                type="button"
                onClick={() => setShowExample((v) => !v)}
                className="mt-1 text-xs text-ink-3 underline hover:text-ink-2"
              >
                {showExample ? 'Hide' : 'View'} payload example
              </button>
              {showExample && (
                <pre className="mt-2 overflow-x-auto rounded border border-rule bg-paper px-3 py-2 font-mono text-[11px] text-ink-2">
                  {form.format_preset === 'slack_compatible' ? SLACK_EXAMPLE : GENERIC_EXAMPLE}
                </pre>
              )}
            </div>

            <label className="block">
              <span className="mb-1 block text-xs font-medium uppercase tracking-wide text-ink-3">
                Default severity
              </span>
              <select
                value={form.default_severity}
                onChange={(e) => setForm((f) => ({ ...f, default_severity: e.target.value }))}
                className="w-full rounded border border-rule bg-paper px-3 py-2 text-sm text-ink outline-none focus:border-teal"
              >
                {SEVERITIES.map((s) => (
                  <option key={s} value={s}>
                    {s}
                  </option>
                ))}
              </select>
              <span className="mt-1 block text-xs text-ink-3">
                Used when a payload doesn't specify (or specifies an unrecognized) severity.
              </span>
            </label>

            <label className="flex items-center gap-2 text-sm text-ink">
              <input
                type="checkbox"
                checked={form.allow_payload_override}
                onChange={(e) => setForm((f) => ({ ...f, allow_payload_override: e.target.checked }))}
              />
              Allow the payload to override display name / avatar / severity
            </label>

            <div>
              <span className="mb-1 block text-xs font-medium uppercase tracking-wide text-ink-3">
                Post to
              </span>
              <div className="flex gap-2">
                <button
                  type="button"
                  onClick={() => setForm((f) => ({ ...f, thread_mode: 'root' }))}
                  className={
                    'flex-1 rounded border px-3 py-2 text-sm ' +
                    (form.thread_mode === 'root'
                      ? 'border-teal bg-teal-soft text-teal'
                      : 'border-rule text-ink-2 hover:bg-paper-3')
                  }
                >
                  Channel root
                </button>
                <button
                  type="button"
                  onClick={() => setForm((f) => ({ ...f, thread_mode: 'thread' }))}
                  className={
                    'flex-1 rounded border px-3 py-2 text-sm ' +
                    (form.thread_mode === 'thread'
                      ? 'border-teal bg-teal-soft text-teal'
                      : 'border-rule text-ink-2 hover:bg-paper-3')
                  }
                >
                  A specific thread
                </button>
              </div>
              {form.thread_mode === 'thread' && (
                <input
                  value={form.thread_id}
                  onChange={(e) => setForm((f) => ({ ...f, thread_id: e.target.value }))}
                  placeholder="Root message id"
                  className="mt-2 w-full rounded border border-rule bg-paper px-3 py-2 text-sm text-ink outline-none focus:border-teal"
                />
              )}
            </div>

            <label className="flex items-start gap-2 text-sm text-ink">
              <input
                type="checkbox"
                className="mt-0.5"
                checked={form.notify_channel_on_critical}
                onChange={(e) => setForm((f) => ({ ...f, notify_channel_on_critical: e.target.checked }))}
              />
              <span>
                Notify the channel on critical alerts
                <span className="mt-0.5 block text-xs text-ink-3">
                  Adds a @channel broadcast — use sparingly.
                </span>
              </span>
            </label>
          </div>

          <div className="mt-5 flex gap-2">
            <button
              type="submit"
              disabled={busy}
              className="rounded bg-teal px-4 py-2 text-sm font-medium text-white hover:opacity-90 disabled:opacity-50"
            >
              {busy ? 'Saving…' : isEdit ? 'Save changes' : 'Create webhook'}
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

/** Fetches the channels the current user can create a webhook on, for the create-panel's
 * channel picker — reuses the same "channels I own" filter the tab's own visibility relies on. */
export function useOwnedChannels(enabled: boolean) {
  return useQuery({
    queryKey: ['channels'],
    queryFn: api.listChannels,
    enabled,
    select: (res) => res.data,
  })
}
