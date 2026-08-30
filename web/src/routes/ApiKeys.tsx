import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useState, type FormEvent } from 'react'
import { ApiError, api, type APIToken } from '../lib/api'
import { NewTokenModal, formatTimestamp } from '../components/TokenWidgets'

/** Self-service personal API keys — every user manages their own, regardless of role. These
 * are for bots/scripts/integrations, deliberately created here; they're unrelated to (and
 * never include) the CLI's auto-minted login session, which only admins can see, in Sessions. */
export function ApiKeys() {
  const qc = useQueryClient()
  const [error, setError] = useState<string | null>(null)
  const [newToken, setNewToken] = useState<string | null>(null)
  const [pendingRevoke, setPendingRevoke] = useState<APIToken | null>(null)
  const [name, setName] = useState('')

  const { data, isLoading } = useQuery({
    queryKey: ['tokens', 'mine'],
    queryFn: api.listMyTokens,
  })

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

  return (
    <div className="mx-auto h-full max-w-3xl overflow-y-auto p-8">
      <h1 className="mb-1 font-display text-2xl font-semibold text-ink">API keys</h1>
      <p className="mb-6 text-sm text-ink-2">
        Personal bearer tokens for your own bots, scripts, and integrations. Anyone can create
        one — these are separate from logging into <code className="rounded bg-paper px-1 py-0.5">hivemind chat</code>.
      </p>

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
      ) : !data || data.data.length === 0 ? (
        <p className="text-ink-2">You haven't created any API keys yet.</p>
      ) : (
        <div className="overflow-x-auto rounded-lg border border-rule">
          <table className="w-full min-w-[560px] text-left text-sm">
            <thead className="bg-paper text-ink-2">
              <tr>
                <th className="px-4 py-2 font-medium">Name</th>
                <th className="px-4 py-2 font-medium">Created</th>
                <th className="px-4 py-2 font-medium">Last used</th>
                <th className="px-4 py-2 font-medium">Actions</th>
              </tr>
            </thead>
            <tbody>
              {data.data.map((t) => (
                <tr key={t.id} className="border-t border-rule">
                  <td className="px-4 py-2 text-ink">{t.name}</td>
                  <td className="px-4 py-2 text-ink-2">{formatTimestamp(t.created_at)}</td>
                  <td className="px-4 py-2 text-ink-2">{formatTimestamp(t.last_used_at)}</td>
                  <td className="px-4 py-2">
                    <button className="text-red-600 hover:underline" onClick={() => setPendingRevoke(t)}>
                      Revoke
                    </button>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}

      {newToken && <NewTokenModal token={newToken} onClose={() => setNewToken(null)} />}

      {pendingRevoke && (
        <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/40">
          <div className="w-full max-w-sm rounded-lg border border-rule bg-white p-6 shadow-lg">
            <h2 className="mb-1 font-display text-lg font-semibold text-ink">Revoke key?</h2>
            <p className="mb-4 text-sm text-ink-2">
              "{pendingRevoke.name}" will stop working immediately. This cannot be undone.
            </p>
            <div className="flex justify-end gap-2">
              <button
                className="rounded border border-rule px-3 py-1.5 text-sm text-ink hover:bg-paper"
                onClick={() => setPendingRevoke(null)}
              >
                Cancel
              </button>
              <button
                className="rounded bg-red-600 px-3 py-1.5 text-sm text-white hover:opacity-90"
                onClick={() => revoke.mutate(pendingRevoke.id)}
                disabled={revoke.isPending}
              >
                Revoke
              </button>
            </div>
          </div>
        </div>
      )}
    </div>
  )
}
