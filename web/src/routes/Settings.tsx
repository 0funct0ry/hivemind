import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useEffect, useState, type FormEvent } from 'react'
import { useLocation, useNavigate } from 'react-router-dom'
import { ApiError, api, type APIToken, type Bot, type OutgoingWebhook, type SlashCommand, type Webhook } from '../lib/api'
import { useAuth } from '../hooks/useAuth'
import { Avatar } from '../components/Avatar'
import { ConfirmDialog } from '../components/ConfirmDialog'
import { WebhookFormModal, useOwnedChannels } from '../components/WebhookFormModal'
import { OutgoingWebhookFormModal } from '../components/OutgoingWebhookFormModal'
import { BotFormModal } from '../components/BotFormModal'
import { BotInfoModal } from '../components/BotInfoModal'
import { SlashCommandFormModal } from '../components/SlashCommandFormModal'
import { NewTokenModal, formatTimestamp } from '../components/TokenWidgets'

type Tab = 'profile' | 'api-keys' | 'notifications' | 'webhooks' | 'outgoing-webhooks' | 'bots'

/** Maps a /settings/* sub-path to its initial tab, so sidebar menu items ("Profile", "API
 * keys") can deep-link into a specific tab instead of always landing on the default. */
function tabFromPathname(pathname: string): Tab {
  if (pathname.endsWith('/profile')) return 'profile'
  if (pathname.endsWith('/api-keys')) return 'api-keys'
  if (pathname.endsWith('/outgoing-webhooks')) return 'outgoing-webhooks'
  if (pathname.endsWith('/bots')) return 'bots'
  return 'webhooks'
}

function StatusTag({ status }: { status: Webhook['status'] }) {
  if (status === 'active') {
    return <span className="rounded-full bg-teal/10 px-2 py-0.5 text-xs font-medium text-teal">Active</span>
  }
  if (status === 'orphaned') {
    return (
      <span className="rounded-full bg-pollen-soft px-2 py-0.5 text-xs font-medium text-pollen">
        Owner deactivated
      </span>
    )
  }
  return <span className="rounded-full bg-paper-3 px-2 py-0.5 text-xs font-medium text-ink-2">Disabled</span>
}

const ICONS = {
  edit: <path d="M11.3 2.3a1.5 1.5 0 0 1 2.1 2.1L5.6 12.2l-3 .8.8-3z" />,
  send: <path d="M13.5 2.5 2 7.2l4.4 1.4L9 13.5 13.5 2.5zM6.6 8.6l4-3.6" />,
  log: <path d="M3 4.5h10M3 8h10M3 11.5h6" />,
  refresh: (
    <>
      <path d="M13 4.5A5.5 5.5 0 1 0 13.8 8.2" />
      <path d="M13.8 1.8v3.2H10.6" />
    </>
  ),
  power: (
    <>
      <path d="M8 2.2v5" />
      <path d="M4.6 4.7a5 5 0 1 0 6.8 0" />
    </>
  ),
  trash: <path d="M3 4.2h10M6 4.2V2.6h4v1.6M4.7 4.2l.6 9a1 1 0 0 0 1 .9h3.4a1 1 0 0 0 1-.9l.6-9" />,
  claim: (
    <>
      <circle cx="8" cy="8" r="5.5" />
      <path d="M5.4 8.2 7.2 10l3.2-3.6" />
    </>
  ),
  info: (
    <>
      <circle cx="8" cy="8" r="5.5" />
      <path d="M8 7.3v3.4" />
      <circle cx="8" cy="5.1" r="0.75" fill="currentColor" stroke="none" />
    </>
  ),
}

/** Icon-only action button, matching MOCKUP.html's `.ib` row actions — padded square, rounded
 * hover background, and a title/aria-label attribute standing in for the visible text label. */
function IconButton({
  title,
  onClick,
  disabled,
  danger,
  icon,
}: {
  title: string
  onClick: () => void
  disabled?: boolean
  danger?: boolean
  icon: React.ReactNode
}) {
  return (
    <button
      type="button"
      title={title}
      aria-label={title}
      onClick={onClick}
      disabled={disabled}
      className={
        'grid shrink-0 place-items-center rounded p-1.5 hover:bg-paper-3 disabled:cursor-not-allowed disabled:opacity-50 ' +
        (danger ? 'text-ink-2 hover:text-red-600' : 'text-ink-2 hover:text-ink')
      }
    >
      <svg width="14" height="14" viewBox="0 0 16 16" fill="none" stroke="currentColor" strokeWidth="1.4">
        {icon}
      </svg>
    </button>
  )
}

function WebhooksTab() {
  const { data: auth } = useAuth()
  const isAdmin = auth?.user.role === 'admin'
  const qc = useQueryClient()
  const [error, setError] = useState<string | null>(null)
  const [formOpen, setFormOpen] = useState(false)
  const [editing, setEditing] = useState<Webhook | null>(null)
  const [pendingRegen, setPendingRegen] = useState<Webhook | null>(null)
  const [pendingDelete, setPendingDelete] = useState<Webhook | null>(null)
  const [regenResult, setRegenResult] = useState<Webhook | null>(null)
  const [copied, setCopied] = useState(false)

  const { data, isLoading } = useQuery({ queryKey: ['webhooks'], queryFn: api.listWebhooks })
  const { data: channels } = useOwnedChannels(formOpen)
  const channelNameById = new Map((channels ?? []).map((c) => [c.id, c.slug ?? c.name]))

  function onError(e: unknown) {
    setError(e instanceof ApiError ? e.message : 'Something went wrong.')
  }

  const regenerate = useMutation({
    mutationFn: (id: string) => api.regenerateWebhook(id),
    onSuccess: (res) => {
      setPendingRegen(null)
      setRegenResult(res.webhook)
      qc.invalidateQueries({ queryKey: ['webhooks'] })
    },
    onError,
  })
  const del = useMutation({
    mutationFn: (id: string) => api.deleteWebhook(id),
    onSuccess: () => {
      setPendingDelete(null)
      qc.invalidateQueries({ queryKey: ['webhooks'] })
    },
    onError,
  })
  const claim = useMutation({
    mutationFn: (id: string) => api.claimWebhook(id),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['webhooks'] }),
    onError,
  })

  const webhooks = data?.data ?? []

  return (
    <div>
      <div className="mb-6 flex items-start justify-between gap-4">
        <div>
          <h1 className="mb-1 font-display text-2xl font-semibold text-ink">Incoming webhooks</h1>
          <p className="text-sm text-ink-2">
            Let external systems post alert cards into a channel via a signed URL. Only a
            channel's owner (or an administrator) can create or manage one.
          </p>
        </div>
        <button
          type="button"
          onClick={() => {
            setEditing(null)
            setFormOpen(true)
          }}
          className="shrink-0 rounded bg-teal px-4 py-2 text-sm font-medium text-white hover:opacity-90"
        >
          New webhook
        </button>
      </div>

      {error && (
        <div className="mb-4 rounded border border-red-200 bg-red-50 px-3 py-2 text-sm text-red-700">
          {error}
        </div>
      )}

      {isLoading ? (
        <p className="text-ink-2">Loading webhooks…</p>
      ) : webhooks.length === 0 ? (
        <p className="text-ink-2">No incoming webhooks yet.</p>
      ) : (
        <div className="rounded-lg border border-rule">
          <table className="w-full table-fixed text-left text-sm">
            <thead className="bg-paper text-ink-2">
              <tr>
                <th className="w-[22%] px-4 py-2 font-medium">Webhook</th>
                <th className="w-[14%] px-4 py-2 font-medium">Channel</th>
                <th className="w-[20%] px-4 py-2 font-medium">Token</th>
                <th className="w-[12%] px-4 py-2 font-medium">Status</th>
                <th className="w-[16%] px-4 py-2 font-medium">Last used</th>
                <th className="w-[16%] px-4 py-2 font-medium">Actions</th>
              </tr>
            </thead>
            <tbody>
              {webhooks.map((w) => (
                <tr key={w.id} className="border-t border-rule">
                  <td className="px-4 py-2">
                    <div className="flex items-center gap-2">
                      <span
                        className="h-4 w-4 shrink-0 rounded"
                        style={{ backgroundColor: w.default_avatar_color || '#7C867F' }}
                      />
                      <span className="truncate font-medium text-ink">{w.name}</span>
                    </div>
                  </td>
                  <td className="truncate px-4 py-2 text-ink-2">#{channelNameById.get(w.channel_id) ?? w.channel_id}</td>
                  <td className="px-4 py-2">
                    <code className="block truncate rounded bg-paper px-1.5 py-0.5 font-mono text-xs text-ink-2">
                      {w.masked_token}
                    </code>
                  </td>
                  <td className="px-4 py-2">
                    <StatusTag status={w.status} />
                  </td>
                  <td className="truncate px-4 py-2 text-ink-2">{formatTimestamp(w.last_used_at ?? undefined)}</td>
                  <td className="px-4 py-2">
                    <div className="flex flex-wrap items-center gap-0.5">
                      {w.status === 'orphaned' && isAdmin ? (
                        <IconButton
                          title="Claim ownership"
                          icon={ICONS.claim}
                          onClick={() => claim.mutate(w.id)}
                          disabled={claim.isPending}
                        />
                      ) : (
                        <>
                          <IconButton
                            title="Edit"
                            icon={ICONS.edit}
                            onClick={() => {
                              setEditing(w)
                              setFormOpen(true)
                            }}
                          />
                          <IconButton title="Regenerate" icon={ICONS.refresh} onClick={() => setPendingRegen(w)} />
                          <IconButton title="Delete" icon={ICONS.trash} danger onClick={() => setPendingDelete(w)} />
                        </>
                      )}
                    </div>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}

      <WebhookFormModal
        open={formOpen}
        onClose={() => {
          setFormOpen(false)
          setEditing(null)
        }}
        webhook={editing}
        ownedChannels={channels ?? []}
      />

      <ConfirmDialog
        open={pendingRegen !== null}
        title="Regenerate token?"
        body={
          <>
            The webhook's current URL will stop accepting requests immediately. Update the
            sending system with the new URL once it's issued.
          </>
        }
        confirmLabel="Regenerate"
        busyLabel="Regenerating…"
        busy={regenerate.isPending}
        onClose={() => setPendingRegen(null)}
        onConfirm={() => pendingRegen && regenerate.mutate(pendingRegen.id)}
      />

      <ConfirmDialog
        open={pendingDelete !== null}
        title="Delete webhook?"
        body={<>This can't be undone. Anything still posting to its URL will start failing.</>}
        confirmLabel="Delete"
        busyLabel="Deleting…"
        busy={del.isPending}
        onClose={() => setPendingDelete(null)}
        onConfirm={() => pendingDelete && del.mutate(pendingDelete.id)}
      />

      {regenResult?.ingest_url && (
        <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/40">
          <div className="w-full max-w-md rounded-lg border border-rule bg-white p-6 shadow-lg">
            <h2 className="mb-1 font-display text-lg font-semibold text-ink">New webhook URL</h2>
            <p className="mb-4 text-sm text-ink-2">
              This is shown once. Copy it now and update the sending system — it cannot be
              retrieved again.
            </p>
            <code className="mb-4 block break-all rounded border border-rule bg-paper px-3 py-2 text-sm text-ink">
              {regenResult.ingest_url}
            </code>
            <div className="flex justify-end gap-2">
              <button
                className="rounded border border-rule px-3 py-1.5 text-sm text-ink hover:bg-paper"
                onClick={async () => {
                  await navigator.clipboard.writeText(regenResult.ingest_url!)
                  setCopied(true)
                }}
              >
                {copied ? 'Copied' : 'Copy'}
              </button>
              <button
                className="rounded bg-teal px-3 py-1.5 text-sm text-white hover:opacity-90"
                onClick={() => {
                  setRegenResult(null)
                  setCopied(false)
                }}
              >
                Done
              </button>
            </div>
          </div>
        </div>
      )}
    </div>
  )
}

function OutgoingStatusTag({ status }: { status: OutgoingWebhook['status'] }) {
  if (status === 'active') {
    return <span className="rounded-full bg-teal/10 px-2 py-0.5 text-xs font-medium text-teal">Active</span>
  }
  if (status === 'unhealthy') {
    return <span className="rounded-full bg-red-50 px-2 py-0.5 text-xs font-medium text-red-700">Unhealthy</span>
  }
  return <span className="rounded-full bg-paper-3 px-2 py-0.5 text-xs font-medium text-ink-2">Disabled</span>
}

function DeliveryLogModal({ webhook, onClose }: { webhook: OutgoingWebhook | null; onClose: () => void }) {
  const { data, isLoading } = useQuery({
    queryKey: ['outgoing-webhook-deliveries', webhook?.id],
    queryFn: () => api.listOutgoingWebhookDeliveries(webhook!.id),
    enabled: webhook !== null,
  })

  if (!webhook) return null
  const deliveries = data?.data ?? []

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/40 p-4" onClick={onClose}>
      <div
        className="max-h-[80vh] w-full max-w-2xl overflow-y-auto rounded-lg border border-rule bg-white p-6 shadow-lg"
        onClick={(e) => e.stopPropagation()}
      >
        <div className="mb-4 flex items-center justify-between">
          <h2 className="font-display text-lg font-semibold text-ink">Deliveries · {webhook.name}</h2>
          <button className="text-sm text-ink-2 hover:text-ink" onClick={onClose}>
            Close
          </button>
        </div>
        {isLoading ? (
          <p className="text-ink-2">Loading…</p>
        ) : deliveries.length === 0 ? (
          <p className="text-ink-2">No deliveries yet.</p>
        ) : (
          <table className="w-full text-left text-sm">
            <thead className="text-ink-2">
              <tr>
                <th className="py-1 pr-3 font-medium">When</th>
                <th className="py-1 pr-3 font-medium">Attempt</th>
                <th className="py-1 pr-3 font-medium">Status</th>
                <th className="py-1 pr-3 font-medium">Latency</th>
                <th className="py-1 font-medium">Response</th>
              </tr>
            </thead>
            <tbody>
              {deliveries.map((d) => (
                <tr key={d.id} className="border-t border-rule align-top">
                  <td className="py-1.5 pr-3 text-ink-2">{formatTimestamp(d.created_at)}</td>
                  <td className="py-1.5 pr-3 text-ink-2">{d.attempt_number}</td>
                  <td className="py-1.5 pr-3 text-ink">{d.response_status ?? 'timeout'}</td>
                  <td className="py-1.5 pr-3 text-ink-2">{d.latency_ms != null ? `${d.latency_ms}ms` : '—'}</td>
                  <td className="py-1.5 text-ink-2">
                    <code className="block max-w-xs truncate font-mono text-xs">{d.response_body_snippet || '—'}</code>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
      </div>
    </div>
  )
}

function OutgoingWebhooksTab() {
  const qc = useQueryClient()
  const [error, setError] = useState<string | null>(null)
  const [formOpen, setFormOpen] = useState(false)
  const [editing, setEditing] = useState<OutgoingWebhook | null>(null)
  const [pendingRegen, setPendingRegen] = useState<OutgoingWebhook | null>(null)
  const [pendingDelete, setPendingDelete] = useState<OutgoingWebhook | null>(null)
  const [regenResult, setRegenResult] = useState<OutgoingWebhook | null>(null)
  const [viewingDeliveries, setViewingDeliveries] = useState<OutgoingWebhook | null>(null)
  const [copied, setCopied] = useState(false)
  const [testResult, setTestResult] = useState<{ id: string; ok: boolean; error?: string } | null>(null)

  const { data, isLoading } = useQuery({ queryKey: ['outgoing-webhooks'], queryFn: api.listOutgoingWebhooks })
  const { data: channels } = useOwnedChannels(formOpen)
  const channelNameById = new Map((channels ?? []).map((c) => [c.id, c.slug ?? c.name]))

  function onError(e: unknown) {
    setError(e instanceof ApiError ? e.message : 'Something went wrong.')
  }

  const regenerate = useMutation({
    mutationFn: (id: string) => api.regenerateOutgoingWebhookSecret(id),
    onSuccess: (res) => {
      setPendingRegen(null)
      setRegenResult(res.webhook)
      qc.invalidateQueries({ queryKey: ['outgoing-webhooks'] })
    },
    onError,
  })
  const del = useMutation({
    mutationFn: (id: string) => api.deleteOutgoingWebhook(id),
    onSuccess: () => {
      setPendingDelete(null)
      qc.invalidateQueries({ queryKey: ['outgoing-webhooks'] })
    },
    onError,
  })
  const toggleStatus = useMutation({
    mutationFn: (w: OutgoingWebhook) =>
      api.updateOutgoingWebhook(w.id, { status: w.status === 'disabled' ? 'active' : 'disabled' }),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['outgoing-webhooks'] }),
    onError,
  })
  const reenable = useMutation({
    mutationFn: (w: OutgoingWebhook) => api.updateOutgoingWebhook(w.id, { status: 'active' }),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['outgoing-webhooks'] }),
    onError,
  })
  const sendTest = useMutation({
    mutationFn: (id: string) => api.sendTestOutgoingWebhookEvent(id),
    onSuccess: (res, id) => {
      setTestResult({ id, ok: res.ok, error: res.error })
      qc.invalidateQueries({ queryKey: ['outgoing-webhook-deliveries', id] })
      qc.invalidateQueries({ queryKey: ['outgoing-webhooks'] })
    },
    onError,
  })

  const webhooks = data?.data ?? []

  return (
    <div>
      <div className="mb-6 flex items-start justify-between gap-4">
        <div>
          <h1 className="mb-1 font-display text-2xl font-semibold text-ink">Outgoing webhooks</h1>
          <p className="text-sm text-ink-2">
            Fire a signed HTTP POST to an external URL whenever a new message is posted in a
            channel, optionally filtered by a keyword. Only a channel's owner (or an
            administrator) can create or manage one.
          </p>
        </div>
        <button
          type="button"
          onClick={() => {
            setEditing(null)
            setFormOpen(true)
          }}
          className="shrink-0 rounded bg-teal px-4 py-2 text-sm font-medium text-white hover:opacity-90"
        >
          New outgoing webhook
        </button>
      </div>

      {error && (
        <div className="mb-4 rounded border border-red-200 bg-red-50 px-3 py-2 text-sm text-red-700">
          {error}
        </div>
      )}

      {isLoading ? (
        <p className="text-ink-2">Loading outgoing webhooks…</p>
      ) : webhooks.length === 0 ? (
        <p className="text-ink-2">No outgoing webhooks yet.</p>
      ) : (
        <div className="rounded-lg border border-rule">
          <table className="w-full table-fixed text-left text-sm">
            <thead className="bg-paper text-ink-2">
              <tr>
                <th className="w-[12%] px-4 py-2 font-medium">Webhook</th>
                <th className="w-[10%] px-4 py-2 font-medium">Channel</th>
                <th className="w-[16%] px-4 py-2 font-medium">Target URL</th>
                <th className="w-[8%] px-4 py-2 font-medium">Status</th>
                <th className="w-[12%] px-4 py-2 font-medium">Last triggered</th>
                <th className="w-[42%] px-4 py-2 font-medium">Actions</th>
              </tr>
            </thead>
            <tbody>
              {webhooks.map((w) => (
                <tr key={w.id} className="border-t border-rule">
                  <td className="truncate px-4 py-2 font-medium text-ink">{w.name}</td>
                  <td className="truncate px-4 py-2 text-ink-2">#{channelNameById.get(w.channel_id) ?? w.channel_id}</td>
                  <td className="px-4 py-2 text-ink-2">
                    <code className="block truncate rounded bg-paper px-1.5 py-0.5 font-mono text-xs">
                      {w.target_url}
                    </code>
                  </td>
                  <td className="px-4 py-2">
                    <OutgoingStatusTag status={w.status} />
                  </td>
                  <td className="truncate px-4 py-2 text-ink-2">{formatTimestamp(w.last_triggered_at ?? undefined)}</td>
                  <td className="px-4 py-2">
                    <div className="flex flex-wrap items-center gap-0.5">
                      <IconButton
                        title="Edit"
                        icon={ICONS.edit}
                        onClick={() => {
                          setEditing(w)
                          setFormOpen(true)
                        }}
                      />
                      <IconButton
                        title="Send test"
                        icon={ICONS.send}
                        onClick={() => sendTest.mutate(w.id)}
                        disabled={sendTest.isPending}
                      />
                      <IconButton title="Deliveries" icon={ICONS.log} onClick={() => setViewingDeliveries(w)} />
                      <IconButton title="Regenerate secret" icon={ICONS.refresh} onClick={() => setPendingRegen(w)} />
                      {w.status === 'unhealthy' ? (
                        <IconButton title="Re-enable" icon={ICONS.power} onClick={() => reenable.mutate(w)} />
                      ) : (
                        <IconButton
                          title={w.status === 'disabled' ? 'Enable' : 'Disable'}
                          icon={ICONS.power}
                          onClick={() => toggleStatus.mutate(w)}
                        />
                      )}
                      <IconButton title="Delete" icon={ICONS.trash} danger onClick={() => setPendingDelete(w)} />
                    </div>
                    {testResult?.id === w.id && (
                      <div className={'mt-1 text-xs ' + (testResult.ok ? 'text-teal' : 'text-red-600')}>
                        {testResult.ok ? 'Test event delivered.' : `Test event failed: ${testResult.error}`}
                      </div>
                    )}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}

      <OutgoingWebhookFormModal
        open={formOpen}
        onClose={() => {
          setFormOpen(false)
          setEditing(null)
        }}
        webhook={editing}
        ownedChannels={channels ?? []}
      />

      <DeliveryLogModal webhook={viewingDeliveries} onClose={() => setViewingDeliveries(null)} />

      <ConfirmDialog
        open={pendingRegen !== null}
        title="Regenerate secret?"
        body={
          <>
            The webhook's current signing secret will stop validating immediately. Update the
            receiving system with the new secret once it's issued.
          </>
        }
        confirmLabel="Regenerate"
        busyLabel="Regenerating…"
        busy={regenerate.isPending}
        onClose={() => setPendingRegen(null)}
        onConfirm={() => pendingRegen && regenerate.mutate(pendingRegen.id)}
      />

      <ConfirmDialog
        open={pendingDelete !== null}
        title="Delete outgoing webhook?"
        body={<>This can't be undone. The external system will stop receiving events.</>}
        confirmLabel="Delete"
        busyLabel="Deleting…"
        busy={del.isPending}
        onClose={() => setPendingDelete(null)}
        onConfirm={() => pendingDelete && del.mutate(pendingDelete.id)}
      />

      {regenResult?.secret && (
        <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/40">
          <div className="w-full max-w-md rounded-lg border border-rule bg-white p-6 shadow-lg">
            <h2 className="mb-1 font-display text-lg font-semibold text-ink">New signing secret</h2>
            <p className="mb-4 text-sm text-ink-2">
              This is shown once. Copy it now and update the receiving system — it cannot be
              retrieved again.
            </p>
            <code className="mb-4 block break-all rounded border border-rule bg-paper px-3 py-2 text-sm text-ink">
              {regenResult.secret}
            </code>
            <div className="flex justify-end gap-2">
              <button
                className="rounded border border-rule px-3 py-1.5 text-sm text-ink hover:bg-paper"
                onClick={async () => {
                  await navigator.clipboard.writeText(regenResult.secret!)
                  setCopied(true)
                }}
              >
                {copied ? 'Copied' : 'Copy'}
              </button>
              <button
                className="rounded bg-teal px-3 py-1.5 text-sm text-white hover:opacity-90"
                onClick={() => {
                  setRegenResult(null)
                  setCopied(false)
                }}
              >
                Done
              </button>
            </div>
          </div>
        </div>
      )}
    </div>
  )
}

function BotStatusTag({ status }: { status: Bot['status'] }) {
  if (status === 'active') {
    return <span className="rounded-full bg-teal/10 px-2 py-0.5 text-xs font-medium text-teal">Active</span>
  }
  return <span className="rounded-full bg-paper-3 px-2 py-0.5 text-xs font-medium text-ink-2">Revoked</span>
}

function CommandStatusTag({ status }: { status: SlashCommand['status'] }) {
  if (status === 'active') {
    return <span className="rounded-full bg-teal/10 px-2 py-0.5 text-xs font-medium text-teal">Active</span>
  }
  return <span className="rounded-full bg-paper-3 px-2 py-0.5 text-xs font-medium text-ink-2">Disabled</span>
}

/** Settings → Bots: bot management plus registered slash commands, both admin-only per
 * SPEC.md §4.12/§7.2. Two sections on one tab rather than two separate tabs, since a slash
 * command's "post as" picker directly depends on the bots list above it. */
function BotsTab() {
  const qc = useQueryClient()
  const [error, setError] = useState<string | null>(null)
  const [botFormOpen, setBotFormOpen] = useState(false)
  const [commandFormOpen, setCommandFormOpen] = useState(false)
  const [pendingRevoke, setPendingRevoke] = useState<Bot | null>(null)
  const [pendingDelete, setPendingDelete] = useState<Bot | null>(null)
  const [infoBot, setInfoBot] = useState<Bot | null>(null)
  const [regenResult, setRegenResult] = useState<Bot | null>(null)
  const [copied, setCopied] = useState(false)
  const [pendingCommandRegen, setPendingCommandRegen] = useState<SlashCommand | null>(null)
  const [pendingCommandDelete, setPendingCommandDelete] = useState<SlashCommand | null>(null)
  const [commandSecretResult, setCommandSecretResult] = useState<SlashCommand | null>(null)
  const [commandSecretCopied, setCommandSecretCopied] = useState(false)

  const { data: botsData, isLoading: botsLoading } = useQuery({ queryKey: ['bots'], queryFn: api.listBots })
  const { data: commandsData, isLoading: commandsLoading } = useQuery({
    queryKey: ['slash-commands-admin'],
    queryFn: api.listSlashCommandsAdmin,
  })
  const bots = botsData?.data ?? []
  const commands = commandsData?.data ?? []
  const botNameById = new Map(bots.map((b) => [b.user_id, b.display_name]))

  function onError(e: unknown) {
    setError(e instanceof ApiError ? e.message : 'Something went wrong.')
  }

  const regenerateBot = useMutation({
    mutationFn: (userId: string) => api.regenerateBotToken(userId),
    onSuccess: (res) => {
      setRegenResult(res.bot)
      qc.invalidateQueries({ queryKey: ['bots'] })
    },
    onError,
  })
  const revokeBot = useMutation({
    mutationFn: (userId: string) => api.revokeBot(userId),
    onSuccess: () => {
      setPendingRevoke(null)
      qc.invalidateQueries({ queryKey: ['bots'] })
    },
    onError,
  })
  const deleteBot = useMutation({
    mutationFn: (userId: string) => api.deleteBot(userId),
    onSuccess: () => {
      setPendingDelete(null)
      qc.invalidateQueries({ queryKey: ['bots'] })
    },
    onError,
  })
  const toggleCommandStatus = useMutation({
    mutationFn: (c: SlashCommand) => api.updateSlashCommand(c.id, { status: c.status === 'disabled' ? 'active' : 'disabled' }),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['slash-commands-admin'] })
      qc.invalidateQueries({ queryKey: ['slash-commands'] })
    },
    onError,
  })
  const regenerateCommandSecret = useMutation({
    mutationFn: (id: string) => api.regenerateSlashCommandSecret(id),
    onSuccess: (res) => {
      setPendingCommandRegen(null)
      setCommandSecretResult(res.command)
      qc.invalidateQueries({ queryKey: ['slash-commands-admin'] })
    },
    onError,
  })
  const deleteCommand = useMutation({
    mutationFn: (id: string) => api.deleteSlashCommand(id),
    onSuccess: () => {
      setPendingCommandDelete(null)
      qc.invalidateQueries({ queryKey: ['slash-commands-admin'] })
      qc.invalidateQueries({ queryKey: ['slash-commands'] })
    },
    onError,
  })

  return (
    <div>
      <div className="mb-6 flex items-start justify-between gap-4">
        <div>
          <h1 className="mb-1 font-display text-2xl font-semibold text-ink">Bots</h1>
          <p className="text-sm text-ink-2">
            A bot is a dedicated user with its own bearer token that can post messages on its own
            initiative. Register a slash command to let members trigger an external webhook from
            the composer.
          </p>
        </div>
        <button
          type="button"
          onClick={() => setBotFormOpen(true)}
          className="shrink-0 rounded bg-teal px-4 py-2 text-sm font-medium text-white hover:opacity-90"
        >
          New bot
        </button>
      </div>

      {error && (
        <div className="mb-4 rounded border border-red-200 bg-red-50 px-3 py-2 text-sm text-red-700">
          {error}
        </div>
      )}

      {botsLoading ? (
        <p className="text-ink-2">Loading bots…</p>
      ) : bots.length === 0 ? (
        <p className="text-ink-2">No bots yet.</p>
      ) : (
        <div className="mb-10 rounded-lg border border-rule">
          <table className="w-full table-fixed text-left text-sm">
            <thead className="bg-paper text-ink-2">
              <tr>
                <th className="w-[22%] px-4 py-2 font-medium">Bot</th>
                <th className="w-[40%] px-4 py-2 font-medium">Description</th>
                <th className="w-[13%] px-4 py-2 font-medium">Status</th>
                <th className="w-[25%] px-4 py-2 font-medium">Actions</th>
              </tr>
            </thead>
            <tbody>
              {bots.map((b) => (
                <tr key={b.user_id} className="border-t border-rule">
                  <td className="truncate px-4 py-2 font-medium text-ink">
                    <span className="flex items-center gap-1.5">
                      <Avatar name={b.display_name} color={b.avatar_color} size={20} />
                      {b.display_name}
                      <span className="rounded bg-paper-3 px-1 font-mono text-[8px] text-ink-2">BOT</span>
                    </span>
                  </td>
                  <td className="truncate px-4 py-2 text-ink-2">{b.description || '—'}</td>
                  <td className="px-4 py-2">
                    <BotStatusTag status={b.status} />
                  </td>
                  <td className="px-4 py-2">
                    <div className="flex flex-wrap items-center gap-0.5">
                      <IconButton
                        title="Regenerate token"
                        icon={ICONS.refresh}
                        disabled={b.status === 'revoked'}
                        onClick={() => regenerateBot.mutate(b.user_id)}
                      />
                      <IconButton
                        title="Revoke"
                        icon={ICONS.power}
                        danger
                        disabled={b.status === 'revoked'}
                        onClick={() => setPendingRevoke(b)}
                      />
                      <IconButton title="Bot info" icon={ICONS.info} onClick={() => setInfoBot(b)} />
                      <IconButton
                        title={b.status === 'revoked' ? 'Delete' : 'Revoke before deleting'}
                        icon={ICONS.trash}
                        danger
                        disabled={b.status !== 'revoked'}
                        onClick={() => setPendingDelete(b)}
                      />
                    </div>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}

      <div className="mb-4 flex items-start justify-between gap-4">
        <div>
          <h2 className="mb-1 font-display text-lg font-semibold text-ink">Slash commands</h2>
          <p className="text-sm text-ink-2">
            Workspace-wide — available in every channel a member can already post in. Execution
            of an admin-only command is restricted to the channel's owner or an administrator.
          </p>
        </div>
        <button
          type="button"
          onClick={() => setCommandFormOpen(true)}
          disabled={bots.length === 0}
          className="shrink-0 rounded bg-teal px-4 py-2 text-sm font-medium text-white hover:opacity-90 disabled:opacity-50"
        >
          New command
        </button>
      </div>

      {commandsLoading ? (
        <p className="text-ink-2">Loading slash commands…</p>
      ) : commands.length === 0 ? (
        <p className="text-ink-2">No slash commands registered yet.</p>
      ) : (
        <div className="rounded-lg border border-rule">
          <table className="w-full table-fixed text-left text-sm">
            <thead className="bg-paper text-ink-2">
              <tr>
                <th className="w-[12%] px-4 py-2 font-medium">Trigger</th>
                <th className="w-[26%] px-4 py-2 font-medium">Description</th>
                <th className="w-[14%] px-4 py-2 font-medium">Bot</th>
                <th className="w-[10%] px-4 py-2 font-medium">Admin only</th>
                <th className="w-[10%] px-4 py-2 font-medium">Status</th>
                <th className="w-[28%] px-4 py-2 font-medium">Actions</th>
              </tr>
            </thead>
            <tbody>
              {commands.map((c) => (
                <tr key={c.id} className="border-t border-rule">
                  <td className="truncate px-4 py-2 font-mono font-medium text-ink">{c.trigger}</td>
                  <td className="truncate px-4 py-2 text-ink-2">{c.description}</td>
                  <td className="truncate px-4 py-2 text-ink-2">{botNameById.get(c.bot_id) ?? c.bot_id}</td>
                  <td className="px-4 py-2 text-ink-2">{c.admin_only ? 'Yes' : 'No'}</td>
                  <td className="px-4 py-2">
                    <CommandStatusTag status={c.status} />
                  </td>
                  <td className="px-4 py-2">
                    <div className="flex flex-wrap items-center gap-0.5">
                      <IconButton title="Regenerate secret" icon={ICONS.refresh} onClick={() => setPendingCommandRegen(c)} />
                      <IconButton
                        title={c.status === 'disabled' ? 'Enable' : 'Disable'}
                        icon={ICONS.power}
                        onClick={() => toggleCommandStatus.mutate(c)}
                      />
                      <IconButton title="Delete" icon={ICONS.trash} danger onClick={() => setPendingCommandDelete(c)} />
                    </div>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}

      <BotFormModal open={botFormOpen} onClose={() => setBotFormOpen(false)} />
      <SlashCommandFormModal open={commandFormOpen} onClose={() => setCommandFormOpen(false)} bots={bots} />

      <ConfirmDialog
        open={pendingRevoke !== null}
        title="Revoke bot?"
        body={
          <>
            The bot's token will stop authenticating immediately. Messages it already posted will
            keep rendering.
          </>
        }
        confirmLabel="Revoke"
        busyLabel="Revoking…"
        busy={revokeBot.isPending}
        onClose={() => setPendingRevoke(null)}
        onConfirm={() => pendingRevoke && revokeBot.mutate(pendingRevoke.user_id)}
      />

      <ConfirmDialog
        open={pendingDelete !== null}
        title="Delete bot?"
        body={
          <>
            This removes "{pendingDelete?.display_name}" from the bots list permanently. This
            can't be undone.
          </>
        }
        confirmLabel="Delete"
        busyLabel="Deleting…"
        busy={deleteBot.isPending}
        onClose={() => setPendingDelete(null)}
        onConfirm={() => pendingDelete && deleteBot.mutate(pendingDelete.user_id)}
      />

      <BotInfoModal bot={infoBot} onClose={() => setInfoBot(null)} />

      <ConfirmDialog
        open={pendingCommandRegen !== null}
        title="Regenerate secret?"
        body={<>The command's current signing secret will stop validating immediately.</>}
        confirmLabel="Regenerate"
        busyLabel="Regenerating…"
        busy={regenerateCommandSecret.isPending}
        onClose={() => setPendingCommandRegen(null)}
        onConfirm={() => pendingCommandRegen && regenerateCommandSecret.mutate(pendingCommandRegen.id)}
      />

      <ConfirmDialog
        open={pendingCommandDelete !== null}
        title="Delete slash command?"
        body={<>This can't be undone. The trigger will stop working immediately.</>}
        confirmLabel="Delete"
        busyLabel="Deleting…"
        busy={deleteCommand.isPending}
        onClose={() => setPendingCommandDelete(null)}
        onConfirm={() => pendingCommandDelete && deleteCommand.mutate(pendingCommandDelete.id)}
      />

      {regenResult?.token && (
        <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/40">
          <div className="w-full max-w-md rounded-lg border border-rule bg-white p-6 shadow-lg">
            <h2 className="mb-1 font-display text-lg font-semibold text-ink">New bearer token</h2>
            <p className="mb-4 text-sm text-ink-2">
              This is shown once. Copy it now — it cannot be retrieved again.
            </p>
            <code className="mb-4 block break-all rounded border border-rule bg-paper px-3 py-2 text-sm text-ink">
              {regenResult.token}
            </code>
            <div className="flex justify-end gap-2">
              <button
                className="rounded border border-rule px-3 py-1.5 text-sm text-ink hover:bg-paper"
                onClick={async () => {
                  await navigator.clipboard.writeText(regenResult.token!)
                  setCopied(true)
                }}
              >
                {copied ? 'Copied' : 'Copy'}
              </button>
              <button
                className="rounded bg-teal px-3 py-1.5 text-sm text-white hover:opacity-90"
                onClick={() => {
                  setRegenResult(null)
                  setCopied(false)
                }}
              >
                Done
              </button>
            </div>
          </div>
        </div>
      )}

      {commandSecretResult?.secret && (
        <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/40">
          <div className="w-full max-w-md rounded-lg border border-rule bg-white p-6 shadow-lg">
            <h2 className="mb-1 font-display text-lg font-semibold text-ink">New signing secret</h2>
            <p className="mb-4 text-sm text-ink-2">
              This is shown once. Copy it now and update the receiving system — it cannot be
              retrieved again.
            </p>
            <code className="mb-4 block break-all rounded border border-rule bg-paper px-3 py-2 text-sm text-ink">
              {commandSecretResult.secret}
            </code>
            <div className="flex justify-end gap-2">
              <button
                className="rounded border border-rule px-3 py-1.5 text-sm text-ink hover:bg-paper"
                onClick={async () => {
                  await navigator.clipboard.writeText(commandSecretResult.secret!)
                  setCommandSecretCopied(true)
                }}
              >
                {commandSecretCopied ? 'Copied' : 'Copy'}
              </button>
              <button
                className="rounded bg-teal px-3 py-1.5 text-sm text-white hover:opacity-90"
                onClick={() => {
                  setCommandSecretResult(null)
                  setCommandSecretCopied(false)
                }}
              >
                Done
              </button>
            </div>
          </div>
        </div>
      )}
    </div>
  )
}

const ACCEPTED_AVATAR_TYPES = ['image/png', 'image/jpeg', 'image/gif', 'image/webp']

function formatJoined(ts?: number): string {
  if (!ts) return '—'
  return new Date(ts).toLocaleDateString(undefined, { year: 'numeric', month: 'long', day: 'numeric' })
}

/** "My profile" tab: view mode by default (display name, username, email, role, joined date)
 * with an Edit affordance that flips the same panel into an editable form — one tab, not two,
 * since "view" and "edit" are just two states of the same profile. */
function ProfileTab() {
  const { data: auth } = useAuth()
  const user = auth?.user
  const qc = useQueryClient()
  const [edit, setEdit] = useState(false)
  const [name, setName] = useState('')
  const [error, setError] = useState('')
  const [busy, setBusy] = useState(false)
  const [dragging, setDragging] = useState(false)
  const [photoSaved, setPhotoSaved] = useState(false)

  useEffect(() => {
    if (user) setName(user.display_name)
  }, [user?.display_name])

  if (!user) return <p className="text-ink-2">Loading…</p>

  const nameValid = name.trim().length > 0
  const nameChanged = name.trim() !== user.display_name

  async function save(patch: { display_name?: string; avatar_file_id?: string | null }) {
    setBusy(true)
    setError('')
    try {
      await api.updateMe(patch)
      await qc.invalidateQueries({ queryKey: ['auth', 'me'] })
      // ['user', id] is the cache MessageList/ThreadPanel resolve authors' live avatar/name
      // from (useUserProfile) — distinct from ['auth','me'], so it needs its own invalidation.
      await qc.invalidateQueries({ queryKey: ['user', user!.id] })
      if (patch.display_name !== undefined) setEdit(false)
      if (patch.avatar_file_id !== undefined) setPhotoSaved(true)
    } catch (e) {
      setError(e instanceof ApiError ? e.message : 'Could not update profile.')
    } finally {
      setBusy(false)
    }
  }

  async function uploadFile(file: File) {
    if (!ACCEPTED_AVATAR_TYPES.includes(file.type)) {
      setError('Avatar must be PNG, JPEG, GIF, or WebP.')
      return
    }
    setBusy(true)
    setError('')
    try {
      const uploaded = await api.uploadAvatar(file)
      await api.updateMe({ avatar_file_id: uploaded.id })
      await qc.invalidateQueries({ queryKey: ['auth', 'me'] })
      await qc.invalidateQueries({ queryKey: ['user', user!.id] })
      setPhotoSaved(true)
    } catch (e) {
      setError(e instanceof ApiError ? e.message : 'Could not upload photo.')
    } finally {
      setBusy(false)
    }
  }

  async function upload(e: React.ChangeEvent<HTMLInputElement>) {
    const file = e.target.files?.[0]
    e.target.value = ''
    if (!file) return
    await uploadFile(file)
  }

  function onDrop(e: React.DragEvent<HTMLDivElement>) {
    e.preventDefault()
    setDragging(false)
    if (busy) return
    const file = e.dataTransfer.files?.[0]
    if (file) void uploadFile(file)
  }

  function onDragOver(e: React.DragEvent<HTMLDivElement>) {
    e.preventDefault()
    if (!busy) setDragging(true)
  }

  function onDragLeave(e: React.DragEvent<HTMLDivElement>) {
    e.preventDefault()
    setDragging(false)
  }

  function cancelEdit() {
    setName(user!.display_name)
    setError('')
    setEdit(false)
  }

  return (
    <div>
      <div className="mb-6">
        <h1 className="mb-1 font-display text-2xl font-semibold text-ink">My profile</h1>
        <p className="text-sm text-ink-2">Your display name, avatar, and account details.</p>
      </div>

      {error && (
        <div className="mb-4 rounded border border-red-200 bg-red-50 px-3 py-2 text-sm text-red-700">
          {error}
        </div>
      )}

      <div className="max-w-md">
        <div
          className={
            'flex flex-col items-center gap-3 border-b border-rule pb-5' +
            (dragging ? ' rounded-md bg-teal-soft outline-dashed outline-2 outline-teal outline-offset-4' : '')
          }
          onDragOver={edit ? onDragOver : undefined}
          onDragLeave={edit ? onDragLeave : undefined}
          onDrop={edit ? onDrop : undefined}
        >
          <Avatar name={user.display_name || user.username} color={user.avatar_color} avatarUrl={user.avatar_url} size={72} />
          {edit && (
            <>
              <div className="flex gap-2">
                <label className="cursor-pointer rounded border border-rule px-2.5 py-1 text-xs font-medium text-ink-2 hover:bg-paper-3">
                  Upload photo
                  <input type="file" accept="image/png,image/jpeg,image/gif,image/webp" onChange={upload} disabled={busy} className="hidden" />
                </label>
                <button
                  type="button"
                  onClick={() => save({ avatar_file_id: null })}
                  disabled={busy || !user.avatar_url}
                  className="rounded border border-rule px-2.5 py-1 text-xs font-medium text-red-600 hover:bg-red-50 disabled:cursor-not-allowed disabled:opacity-40 disabled:hover:bg-transparent"
                >
                  Remove photo
                </button>
              </div>
              <p className="text-[11px] text-ink-3">
                {dragging ? 'Drop to upload' : photoSaved ? 'Photo saved.' : 'or drag and drop an image here'}
              </p>
            </>
          )}
        </div>

        {edit ? (
          <label className="mt-5 block text-sm">
            <span className="font-medium text-ink-2">Display name</span>
            <input
              autoFocus
              value={name}
              onChange={(e) => setName(e.target.value)}
              className="mt-1 w-full rounded border border-rule bg-paper p-2"
            />
            {!nameValid && <span className="mt-1 block text-xs text-red-600">Display name can't be empty.</span>}
          </label>
        ) : (
          <dl className="mt-5 space-y-3 text-sm">
            <div>
              <dt className="lbl text-ink-3">Display name</dt>
              <dd className="mt-0.5 text-ink">{user.display_name || user.username}</dd>
            </div>
            <div>
              <dt className="lbl text-ink-3">Username</dt>
              <dd className="mt-0.5 text-ink">@{user.username}</dd>
            </div>
            <div>
              <dt className="lbl text-ink-3">Email</dt>
              <dd className="mt-0.5 text-ink">{user.email}</dd>
            </div>
            <div>
              <dt className="lbl text-ink-3">Role</dt>
              <dd className="mt-0.5 capitalize text-ink">{user.role}</dd>
            </div>
            <div>
              <dt className="lbl text-ink-3">Joined</dt>
              <dd className="mt-0.5 text-ink">{formatJoined(user.created_at)}</dd>
            </div>
          </dl>
        )}

        <div className="mt-6 flex gap-2">
          {edit ? (
            <>
              <button
                type="button"
                onClick={() => (nameChanged ? save({ display_name: name.trim() }) : setEdit(false))}
                disabled={busy || !nameValid || (!nameChanged && !photoSaved)}
                className="rounded bg-teal px-3 py-2 text-sm text-white disabled:opacity-50"
              >
                {busy ? 'Saving…' : 'Save'}
              </button>
              <button
                type="button"
                onClick={cancelEdit}
                disabled={busy}
                className="rounded border border-rule px-3 py-2 text-sm text-ink-2 hover:bg-paper-3"
              >
                Cancel
              </button>
            </>
          ) : (
            <button type="button" onClick={() => setEdit(true)} className="rounded bg-teal px-3 py-2 text-sm text-white">
              Edit
            </button>
          )}
        </div>
      </div>
    </div>
  )
}

/** Self-service personal API keys — every user manages their own, regardless of role. These
 * are for bots/scripts/integrations, deliberately created here; unrelated to (and never
 * including) the CLI's auto-minted login session, which only admins can see, in Sessions. */
function ApiKeysTab() {
  const qc = useQueryClient()
  const [error, setError] = useState<string | null>(null)
  const [newToken, setNewToken] = useState<string | null>(null)
  const [pendingRevoke, setPendingRevoke] = useState<APIToken | null>(null)
  const [name, setName] = useState('')

  const { data, isLoading } = useQuery({ queryKey: ['tokens', 'mine'], queryFn: api.listMyTokens })

  function onError(e: unknown) {
    setError(e instanceof ApiError ? e.message : 'Something went wrong.')
  }

  const create = useMutation({
    mutationFn: (body: { name: string }) => api.createMyToken(body),
    onSuccess: (res) => {
      setNewToken(res.token)
      setName('')
      qc.invalidateQueries({ queryKey: ['tokens', 'mine'] })
    },
    onError,
  })
  const revoke = useMutation({
    mutationFn: (id: string) => api.deleteMyToken(id),
    onSuccess: () => {
      setPendingRevoke(null)
      qc.invalidateQueries({ queryKey: ['tokens', 'mine'] })
    },
    onError,
  })

  function onSubmit(e: FormEvent) {
    e.preventDefault()
    if (!name.trim()) return
    create.mutate({ name: name.trim() })
  }

  const tokens = data?.data ?? []

  return (
    <div>
      <div className="mb-6">
        <h1 className="mb-1 font-display text-2xl font-semibold text-ink">API keys</h1>
        <p className="text-sm text-ink-2">
          Personal bearer tokens for your own bots, scripts, and integrations. Anyone can create
          one — these are separate from logging into <code className="rounded bg-paper px-1 py-0.5">hivemind chat</code>.
        </p>
      </div>

      {error && (
        <div className="mb-4 rounded border border-red-200 bg-red-50 px-3 py-2 text-sm text-red-700">
          {error}
        </div>
      )}

      <form onSubmit={onSubmit} className="mb-6 flex gap-2">
        <input
          className="flex-1 rounded border border-rule bg-paper px-3 py-2 text-sm text-ink outline-none focus:border-teal"
          placeholder="Key name, e.g. deploy-bot"
          value={name}
          onChange={(e) => setName(e.target.value)}
        />
        <button
          type="submit"
          className="rounded bg-teal px-4 py-2 text-sm font-medium text-white hover:opacity-90 disabled:opacity-50"
          disabled={create.isPending || !name.trim()}
        >
          Create key
        </button>
      </form>

      {isLoading ? (
        <p className="text-ink-2">Loading keys…</p>
      ) : tokens.length === 0 ? (
        <p className="text-ink-2">You haven't created any API keys yet.</p>
      ) : (
        <div className="rounded-lg border border-rule">
          <table className="w-full table-fixed text-left text-sm">
            <thead className="bg-paper text-ink-2">
              <tr>
                <th className="w-[34%] px-4 py-2 font-medium">Name</th>
                <th className="w-[22%] px-4 py-2 font-medium">Created</th>
                <th className="w-[22%] px-4 py-2 font-medium">Last used</th>
                <th className="w-[22%] px-4 py-2 font-medium">Actions</th>
              </tr>
            </thead>
            <tbody>
              {tokens.map((t) => (
                <tr key={t.id} className="border-t border-rule">
                  <td className="truncate px-4 py-2 text-ink">{t.name}</td>
                  <td className="truncate px-4 py-2 text-ink-2">{formatTimestamp(t.created_at)}</td>
                  <td className="truncate px-4 py-2 text-ink-2">{formatTimestamp(t.last_used_at)}</td>
                  <td className="px-4 py-2">
                    <IconButton title="Revoke" icon={ICONS.trash} danger onClick={() => setPendingRevoke(t)} />
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}

      {newToken && <NewTokenModal token={newToken} onClose={() => setNewToken(null)} />}

      <ConfirmDialog
        open={pendingRevoke !== null}
        title="Revoke key?"
        body={
          <>
            "{pendingRevoke?.name}" will stop working immediately. This cannot be undone.
          </>
        }
        confirmLabel="Revoke"
        busyLabel="Revoking…"
        busy={revoke.isPending}
        onClose={() => setPendingRevoke(null)}
        onConfirm={() => pendingRevoke && revoke.mutate(pendingRevoke.id)}
      />
    </div>
  )
}

function TabButton({
  active,
  disabled,
  onClick,
  children,
}: {
  active: boolean
  disabled?: boolean
  onClick: () => void
  children: React.ReactNode
}) {
  return (
    <button
      type="button"
      disabled={disabled}
      onClick={onClick}
      className={
        'block w-full rounded px-3 py-2 text-left text-sm ' +
        (disabled
          ? 'cursor-not-allowed text-ink-3'
          : active
            ? 'bg-teal-soft font-medium text-teal'
            : 'text-ink-2 hover:bg-paper-3')
      }
    >
      {children}
    </button>
  )
}

export function Settings() {
  const navigate = useNavigate()
  const location = useLocation()
  const { data: auth } = useAuth()
  const isAdmin = auth?.user.role === 'admin'
  const [tab, setTab] = useState<Tab>(() => tabFromPathname(location.pathname))
  const backTo = (location.state as { from?: string } | null)?.from ?? '/'

  return (
    <div className="flex h-full">
      <aside className="w-56 shrink-0 border-r border-rule p-4">
        <button
          type="button"
          onClick={() => navigate(backTo)}
          className="mb-4 text-sm text-ink-2 hover:text-ink"
        >
          ← Back
        </button>
        <div className="mb-4">
          <div className="mb-1 px-3 text-xs font-medium uppercase tracking-wide text-ink-3">Account</div>
          <TabButton active={tab === 'profile'} onClick={() => setTab('profile')}>
            My profile
          </TabButton>
          <TabButton active={tab === 'api-keys'} onClick={() => setTab('api-keys')}>
            API keys
          </TabButton>
          <TabButton active={tab === 'notifications'} disabled onClick={() => setTab('notifications')}>
            Notifications
          </TabButton>
        </div>
        <div>
          <div className="mb-1 px-3 text-xs font-medium uppercase tracking-wide text-ink-3">Workspace</div>
          <TabButton active={tab === 'webhooks'} onClick={() => setTab('webhooks')}>
            Incoming webhooks
          </TabButton>
          <TabButton active={tab === 'outgoing-webhooks'} onClick={() => setTab('outgoing-webhooks')}>
            Outgoing webhooks
          </TabButton>
          {isAdmin && (
            <TabButton active={tab === 'bots'} onClick={() => setTab('bots')}>
              Bots
            </TabButton>
          )}
        </div>
      </aside>
      <div className="h-full flex-1 overflow-y-auto p-8">
        {tab === 'webhooks' && <WebhooksTab />}
        {tab === 'outgoing-webhooks' && <OutgoingWebhooksTab />}
        {tab === 'bots' && isAdmin && <BotsTab />}
        {tab === 'profile' && <ProfileTab />}
        {tab === 'api-keys' && <ApiKeysTab />}
        {tab === 'notifications' && <p className="text-ink-2">Coming soon.</p>}
      </div>
    </div>
  )
}
