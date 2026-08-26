import { Navigate, Outlet } from 'react-router-dom'
import { Sidebar } from '../components/Sidebar'
import { ThreadPanel } from '../components/ThreadPanel'
import { useAuth } from '../hooks/useAuth'
import { useRealtimeSync } from '../hooks/useRealtimeSync'
import { useUiStore } from '../store/ui'

function ConnectionStrip() {
  const state = useUiStore((s) => s.connectionState)
  if (state === 'open' || state === 'closed') return null
  return (
    <div className="bg-pollen-soft px-3 py-1 text-center font-mono text-xs text-ink-2">
      {state === 'connecting' ? 'Connecting…' : 'Reconnecting…'}
    </div>
  )
}

export function AppShell() {
  const { data, isLoading, isError } = useAuth()
  useRealtimeSync()
  const openThreadId = useUiStore((s) => s.openThreadId)

  if (isLoading) {
    return <div className="flex h-full items-center justify-center text-ink-2">Loading…</div>
  }
  if (isError || !data) {
    return <Navigate to="/login" replace />
  }

  return (
    <div
      className={
        'grid h-full grid-rows-[auto_1fr] ' +
        (openThreadId ? 'grid-cols-[244px_minmax(0,1fr)_372px]' : 'grid-cols-[244px_minmax(0,1fr)]')
      }
    >
      <div className="col-span-full">
        <ConnectionStrip />
      </div>
      <Sidebar />
      <main className="flex h-full flex-col overflow-hidden bg-paper">
        <Outlet />
      </main>
      {openThreadId && (
        <div className="hidden md:block">
          <ThreadPanel currentUsername={data.user.username} />
        </div>
      )}
      {openThreadId && (
        <div className="fixed inset-0 z-40 bg-paper md:hidden">
          <ThreadPanel currentUsername={data.user.username} />
        </div>
      )}
    </div>
  )
}
