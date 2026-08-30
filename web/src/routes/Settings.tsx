import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useState } from 'react'
import { useLocation, useNavigate } from 'react-router-dom'
import { ApiError, api, type Webhook } from '../lib/api'
import { useAuth } from '../hooks/useAuth'
import { ConfirmDialog } from '../components/ConfirmDialog'
import { WebhookFormModal, useOwnedChannels } from '../components/WebhookFormModal'
import { formatTimestamp } from '../components/TokenWidgets'

type Tab = 'profile' | 'notifications' | 'webhooks'

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
        <div className="overflow-x-auto rounded-lg border border-rule">
          <table className="w-full min-w-[760px] text-left text-sm">
            <thead className="bg-paper text-ink-2">
              <tr>
                <th className="px-4 py-2 font-medium">Webhook</th>
                <th className="px-4 py-2 font-medium">Channel</th>
                <th className="px-4 py-2 font-medium">Token</th>
                <th className="px-4 py-2 font-medium">Status</th>
                <th className="px-4 py-2 font-medium">Last used</th>
                <th className="px-4 py-2 font-medium">Actions</th>
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
                      <span className="font-medium text-ink">{w.name}</span>
                    </div>
                  </td>
                  <td className="px-4 py-2 text-ink-2">#{channelNameById.get(w.channel_id) ?? w.channel_id}</td>
                  <td className="px-4 py-2">
                    <code className="rounded bg-paper px-1.5 py-0.5 font-mono text-xs text-ink-2">
                      {w.masked_token}
                    </code>
                  </td>
                  <td className="px-4 py-2">
                    <StatusTag status={w.status} />
                  </td>
                  <td className="px-4 py-2 text-ink-2">{formatTimestamp(w.last_used_at ?? undefined)}</td>
                  <td className="px-4 py-2">
                    <div className="flex gap-2">
                      {w.status === 'orphaned' && isAdmin ? (
                        <button
                          className="text-teal hover:underline"
                          onClick={() => claim.mutate(w.id)}
                          disabled={claim.isPending}
                        >
                          Claim
                        </button>
                      ) : (
                        <>
                          <button
                            className="text-ink-2 hover:underline"
                            onClick={() => {
                              setEditing(w)
                              setFormOpen(true)
                            }}
                          >
                            Edit
                          </button>
                          <button className="text-ink-2 hover:underline" onClick={() => setPendingRegen(w)}>
                            Regenerate
                          </button>
                          <button className="text-red-600 hover:underline" onClick={() => setPendingDelete(w)}>
                            Delete
                          </button>
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
  const [tab, setTab] = useState<Tab>('webhooks')
  const navigate = useNavigate()
  const location = useLocation()
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
          <TabButton active={tab === 'profile'} disabled onClick={() => setTab('profile')}>
            My profile
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
        </div>
      </aside>
      <div className="mx-auto h-full max-w-4xl flex-1 overflow-y-auto p-8">
        {tab === 'webhooks' && <WebhooksTab />}
        {tab === 'profile' && <p className="text-ink-2">Coming soon.</p>}
        {tab === 'notifications' && <p className="text-ink-2">Coming soon.</p>}
      </div>
    </div>
  )
}
