import { useQuery } from '@tanstack/react-query'
import { NavLink } from 'react-router-dom'
import { useEffect, useState } from 'react'
import { api } from '../lib/api'
import { useAuth } from '../hooks/useAuth'
import { useUiStore } from '../store/ui'
import { wsClient, type ConnectionState } from '../lib/ws'
import { Avatar } from './Avatar'

function UnreadIndicator({ count, mentioned }: { count: number; mentioned: boolean }) {
  if (count === 0) return null
  if (mentioned) {
    return (
      <span className="ml-auto rounded-full bg-pollen px-1.5 py-0.5 font-mono text-[11px] leading-none text-white">
        {count}
      </span>
    )
  }
  return <span className="ml-auto h-1.5 w-1.5 shrink-0 rounded-full bg-ink" aria-hidden />
}

function BrandMark() {
  return (
    <svg viewBox="0 0 24 24" className="h-[22px] w-[22px] shrink-0" aria-hidden>
      <path
        d="M12 1.6 21 6.8v10.4L12 22.4 3 17.2V6.8z"
        fill="none"
        stroke="currentColor"
        className="text-ink"
        strokeWidth="1.6"
        strokeLinejoin="round"
      />
      <circle cx="12" cy="12" r="2.6" className="fill-teal" />
      <circle cx="12" cy="5.4" r="1.35" className="fill-ink" />
      <circle cx="18" cy="8.7" r="1.35" className="fill-ink" />
      <circle cx="18" cy="15.3" r="1.35" className="fill-pollen" />
      <circle cx="12" cy="18.6" r="1.35" className="fill-ink" />
      <circle cx="6" cy="15.3" r="1.35" className="fill-ink" />
      <circle cx="6" cy="8.7" r="1.35" className="fill-ink" />
    </svg>
  )
}

function useConnectionState(): ConnectionState {
  const [state, setState] = useState<ConnectionState>('closed')
  useEffect(() => {
    const unsubscribe = wsClient.onStateChange(setState)
    return () => {
      unsubscribe()
    }
  }, [])
  return state
}

export function Sidebar() {
  const channelsQuery = useQuery({ queryKey: ['channels'], queryFn: api.listChannels })
  const dmsQuery = useQuery({ queryKey: ['dms'], queryFn: api.listDMs })
  const unreadsQuery = useQuery({ queryKey: ['unreads'], queryFn: api.unreadSummary })
  const presenceQuery = useQuery({
    queryKey: ['presence'],
    queryFn: api.getPresence,
    refetchInterval: 15000,
  })
  const { data: auth } = useAuth()
  const openCommandPalette = useUiStore((s) => s.openCommandPalette)
  const connectionState = useConnectionState()

  const unreadByChannel = new Map(
    (unreadsQuery.data?.data ?? []).map((u) => [u.channel_id, u]),
  )
  const online = new Set(presenceQuery.data?.online ?? [])

  const channels = channelsQuery.data?.data ?? []
  const publicAndPrivate = channels.filter((c) => c.kind !== 'dm')
  const dms = dmsQuery.data?.data ?? []

  return (
    <nav aria-label="Channels and direct messages" className="flex h-full flex-col border-r border-rule bg-paper-2 p-3">
      <div className="mb-2 flex items-center gap-2 px-1">
        <BrandMark />
        <div>
          <div className="font-display text-base font-semibold leading-none text-ink">hivemind</div>
          {auth?.workspace.name && (
            <div className="mt-0.5 font-mono text-[9px] uppercase tracking-wide text-ink-3">
              {auth.workspace.name}
            </div>
          )}
        </div>
      </div>

      <button
        type="button"
        onClick={() => openCommandPalette()}
        className="mb-3 flex items-center gap-2 rounded-md border border-rule bg-paper px-2 py-1.5 text-left text-sm text-ink-3 hover:border-ink-3 hover:text-ink-2"
      >
        Jump to…
        <kbd className="ml-auto rounded border border-rule px-1 font-mono text-[9px] text-ink-3">⌘K</kbd>
      </button>

      <div className="lbl mb-1 flex items-center gap-1 px-1">
        <span>Channels</span>
        <button
          type="button"
          title="New channel"
          aria-label="New channel"
          onClick={() => {
            // TODO: wire to a create-channel flow once one exists.
          }}
          className="ml-auto px-1 text-sm leading-none text-ink-3 hover:text-teal"
        >
          +
        </button>
      </div>
      <ul className="mb-4 flex flex-col gap-0.5">
        {publicAndPrivate.length === 0 && (
          <li className="px-2 py-1 text-sm text-ink-3">No channels yet.</li>
        )}
        {publicAndPrivate.map((c) => {
          const unread = unreadByChannel.get(c.id)
          return (
            <li key={c.id}>
              <NavLink
                to={`/c/${c.slug}`}
                className={({ isActive }) =>
                  'flex items-center gap-1.5 rounded px-2 py-1 text-sm ' +
                  (isActive ? 'bg-teal-soft text-teal' : 'text-ink-2 hover:bg-paper-3')
                }
              >
                <span>{c.kind === 'private' ? '🔒' : '#'}</span>
                <span className="truncate">{c.name}</span>
                {unread && (
                  <UnreadIndicator count={unread.unread_count} mentioned={unread.mention_count > 0} />
                )}
              </NavLink>
            </li>
          )
        })}
      </ul>

      <div className="lbl mb-1 flex items-center gap-1 px-1">
        <span>Direct messages</span>
        <button
          type="button"
          title="New direct message"
          aria-label="New direct message"
          onClick={() => openCommandPalette()}
          className="ml-auto px-1 text-sm leading-none text-ink-3 hover:text-teal"
        >
          +
        </button>
      </div>
      <ul className="flex flex-col gap-0.5">
        {dms.length === 0 && <li className="px-2 py-1 text-sm text-ink-3">No direct messages yet.</li>}
        {dms.map((d) => {
          const unread = unreadByChannel.get(d.id)
          const isOnline = online.has(d.peer.id)
          return (
            <li key={d.id}>
              <NavLink
                to={`/dm/${d.peer.username}`}
                className={({ isActive }) =>
                  'flex items-center gap-1.5 rounded px-2 py-1 text-sm ' +
                  (isActive ? 'bg-teal-soft text-teal' : 'text-ink-2 hover:bg-paper-3')
                }
              >
                <span
                  className={
                    'h-[7px] w-[7px] shrink-0 rounded-full border-[1.5px] ' +
                    (isOnline ? 'border-teal bg-teal' : 'border-ink-3 bg-transparent')
                  }
                  aria-hidden
                />
                <span className="truncate">{d.peer.display_name || d.peer.username}</span>
                {unread && (
                  <UnreadIndicator count={unread.unread_count} mentioned={unread.mention_count > 0} />
                )}
              </NavLink>
            </li>
          )
        })}
      </ul>

      {auth?.user && (
        <div className="mt-auto flex items-center gap-2 border-t border-rule px-1 pt-3">
          <Avatar name={auth.user.display_name || auth.user.username} color={auth.user.avatar_color} size={26} />
          <div className="min-w-0">
            <div className="truncate text-[13.5px] font-semibold text-ink">
              {auth.user.display_name || auth.user.username}
            </div>
            <div className="flex items-center gap-1.5 font-mono text-[9px] uppercase tracking-wide text-ink-3">
              <span
                className={
                  'h-[5px] w-[5px] rounded-full ' +
                  (connectionState === 'open' ? 'bg-teal' : 'bg-ink-3')
                }
                aria-hidden
              />
              {connectionState === 'open' ? 'connected' : connectionState}
            </div>
          </div>
        </div>
      )}
    </nav>
  )
}
