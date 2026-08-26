import { Navigate, Outlet } from 'react-router-dom'
import { Sidebar } from '../components/Sidebar'
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

  if (isLoading) {
    return <div className="flex h-full items-center justify-center text-ink-2">Loading…</div>
  }
  if (isError || !data) {
    return <Navigate to="/login" replace />
  }

  return (
    <div className="grid h-full grid-cols-[244px_minmax(0,1fr)] grid-rows-[auto_1fr]">
      <div className="col-span-2">
        <ConnectionStrip />
      </div>
      <Sidebar />
      <main className="flex h-full flex-col overflow-hidden bg-paper">
        <Outlet />
      </main>
    </div>
  )
}
