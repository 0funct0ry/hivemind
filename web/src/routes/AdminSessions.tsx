import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useState } from 'react'
import { Navigate } from 'react-router-dom'
import { ApiError, api, type AdminSession } from '../lib/api'
import { useAuth } from '../hooks/useAuth'
import { NewTokenModal, StatusPill, formatTimestamp } from '../components/TokenWidgets'

export function AdminSessions() {
  const { data: auth, isLoading: authLoading } = useAuth()
  const qc = useQueryClient()
  const [error, setError] = useState<string | null>(null)
  const [rotatedToken, setRotatedToken] = useState<string | null>(null)
  const [pendingRevoke, setPendingRevoke] = useState<AdminSession | null>(null)

  const { data, isLoading } = useQuery({
    queryKey: ['admin', 'sessions'],
    queryFn: api.adminListSessions,
    enabled: auth?.user.role === 'admin',
  })

  function onError(e: unknown) {
    setError(e instanceof ApiError ? e.message : 'Something went wrong.')
  }

  const disable = useMutation({
    mutationFn: (id: string) => api.adminDisableSession(id),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['admin', 'sessions'] }),
    onError,
  })
  const enable = useMutation({
    mutationFn: (id: string) => api.adminEnableSession(id),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['admin', 'sessions'] }),
    onError,
  })
  const rotate = useMutation({
    mutationFn: (id: string) => api.adminRotateSession(id),
    onSuccess: (res) => {
      setRotatedToken(res.token)
      qc.invalidateQueries({ queryKey: ['admin', 'sessions'] })
    },
    onError,
  })
  const revoke = useMutation({
    mutationFn: (id: string) => api.adminRevokeSession(id),
    onSuccess: () => {
      setPendingRevoke(null)
      qc.invalidateQueries({ queryKey: ['admin', 'sessions'] })
    },
    onError,
  })

  if (authLoading) {
    return <div className="flex h-full items-center justify-center text-ink-2">Loading…</div>
  }
  if (!auth || auth.user.role !== 'admin') {
    return <Navigate to="/" replace />
  }

  return (
    <div className="mx-auto h-full max-w-4xl overflow-y-auto p-8">
      <h1 className="mb-1 font-display text-2xl font-semibold text-ink">Sessions</h1>
      <p className="mb-6 text-sm text-ink-2">
        Every active <code className="rounded bg-paper px-1 py-0.5">hivemind chat</code> CLI login
        across the workspace. These aren't personal API keys — they're minted automatically when
        someone logs into the CLI, so it doesn't have to ask for a password every time.
      </p>

      {error && (
        <div className="mb-4 rounded border border-red-200 bg-red-50 px-3 py-2 text-sm text-red-700">
          {error}
        </div>
      )}

      {isLoading ? (
        <p className="text-ink-2">Loading sessions…</p>
      ) : !data || data.data.length === 0 ? (
        <p className="text-ink-2">No CLI sessions are active.</p>
      ) : (
        <div className="overflow-x-auto rounded-lg border border-rule">
          <table className="w-full min-w-[720px] text-left text-sm">
            <thead className="bg-paper text-ink-2">
              <tr>
                <th className="px-4 py-2 font-medium">Name</th>
                <th className="px-4 py-2 font-medium">User</th>
                <th className="px-4 py-2 font-medium">Created</th>
                <th className="px-4 py-2 font-medium">Expires</th>
                <th className="px-4 py-2 font-medium">Last used</th>
                <th className="px-4 py-2 font-medium">Status</th>
                <th className="px-4 py-2 font-medium">Actions</th>
              </tr>
            </thead>
            <tbody>
              {data.data.map((t) => (
                <tr key={t.id} className="border-t border-rule">
                  <td className="px-4 py-2 text-ink">{t.name}</td>
                  <td className="px-4 py-2 text-ink">{t.display_name || t.username}</td>
                  <td className="px-4 py-2 text-ink-2">{formatTimestamp(t.created_at)}</td>
                  <td className="px-4 py-2 text-ink-2">{formatTimestamp(t.expires_at)}</td>
                  <td className="px-4 py-2 text-ink-2">{formatTimestamp(t.last_used_at)}</td>
                  <td className="px-4 py-2">
                    <StatusPill disabled={t.disabled} />
                  </td>
                  <td className="px-4 py-2">
                    <div className="flex gap-2">
                      {t.disabled ? (
                        <button
                          className="text-teal hover:underline"
                          onClick={() => enable.mutate(t.id)}
                          disabled={enable.isPending}
                        >
                          Enable
                        </button>
                      ) : (
                        <button
                          className="text-ink-2 hover:underline"
                          onClick={() => disable.mutate(t.id)}
                          disabled={disable.isPending}
                        >
                          Disable
                        </button>
                      )}
                      <button
                        className="text-ink-2 hover:underline"
                        onClick={() => rotate.mutate(t.id)}
                        disabled={rotate.isPending}
                      >
                        Rotate
                      </button>
                      <button
                        className="text-red-600 hover:underline"
                        onClick={() => setPendingRevoke(t)}
                      >
                        Revoke
                      </button>
                    </div>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}

      {rotatedToken && <NewTokenModal token={rotatedToken} onClose={() => setRotatedToken(null)} />}

      {pendingRevoke && (
        <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/40">
          <div className="w-full max-w-sm rounded-lg border border-rule bg-white p-6 shadow-lg">
            <h2 className="mb-1 font-display text-lg font-semibold text-ink">Revoke session?</h2>
            <p className="mb-4 text-sm text-ink-2">
              This will log {pendingRevoke.display_name || pendingRevoke.username} out of their
              CLI session immediately. This cannot be undone.
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
