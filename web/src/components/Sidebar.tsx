import { useQuery } from '@tanstack/react-query'
import { NavLink } from 'react-router-dom'
import { useEffect, useRef, useState } from 'react'
import { api } from '../lib/api'
import { useAuth } from '../hooks/useAuth'
import { useUiStore } from '../store/ui'
import { wsClient, type ConnectionState } from '../lib/ws'
import { Avatar } from './Avatar'
import { CreateChannelModal } from './CreateChannelModal'
import { BrowseChannelsModal } from './BrowseChannelsModal'
import { ProfileModal } from './ProfileModal'
import { useQueryClient } from '@tanstack/react-query'

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

/** A small anchored popover menu, closing on Esc, outside click, or item select, with a basic focus trap. */
function PopoverMenu({
  anchorClassName,
  onClose,
  children,
}: {
  anchorClassName: string
  onClose: () => void
  children: React.ReactNode
}) {
  const ref = useRef<HTMLDivElement>(null)

  useEffect(() => {
    const first = ref.current?.querySelector<HTMLElement>('[role="menuitem"]')
    first?.focus()

    function handlePointerDown(e: MouseEvent) {
      if (ref.current && !ref.current.contains(e.target as Node)) onClose()
    }
    function handleKeyDown(e: KeyboardEvent) {
      if (e.key === 'Escape') {
        e.preventDefault()
        onClose()
        return
      }
      if (e.key !== 'Tab' || !ref.current) return
      const items = Array.from(ref.current.querySelectorAll<HTMLElement>('[role="menuitem"]'))
      if (items.length === 0) return
      const first = items[0]
      const last = items[items.length - 1]
      if (e.shiftKey && document.activeElement === first) {
        e.preventDefault()
        last.focus()
      } else if (!e.shiftKey && document.activeElement === last) {
        e.preventDefault()
        first.focus()
      }
    }
    document.addEventListener('mousedown', handlePointerDown)
    document.addEventListener('keydown', handleKeyDown)
    return () => {
      document.removeEventListener('mousedown', handlePointerDown)
      document.removeEventListener('keydown', handleKeyDown)
    }
  }, [onClose])

  return (
    <div
      ref={ref}
      role="menu"
      className={
        'absolute z-40 min-w-[170px] rounded-md border border-rule bg-paper py-1 font-body normal-case tracking-normal shadow-lg ' +
        anchorClassName
      }
    >
      {children}
    </div>
  )
}

function MenuItem({ onClick, danger, children }: { onClick: () => void; danger?: boolean; children: React.ReactNode }) {
  return (
    <button
      type="button"
      role="menuitem"
      onClick={onClick}
      className={
        'block w-full px-3 py-1.5 text-left text-sm hover:bg-paper-3 ' + (danger ? 'text-red-600' : 'text-ink-2')
      }
    >
      {children}
    </button>
  )
}

export function Sidebar() {
  const [createOpen, setCreateOpen] = useState(false)
  const [browseOpen, setBrowseOpen] = useState(false)
  const [profileOpen, setProfileOpen] = useState(false)
  const [profileMode, setProfileMode] = useState<'view' | 'edit'>('view')
  const [footerMenuOpen, setFooterMenuOpen] = useState(false)
  const [channelMenuOpen, setChannelMenuOpen] = useState(false)
  const [leaveMenuId, setLeaveMenuId] = useState<string | null>(null)
  const queryClient = useQueryClient()
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
  const publicAndPrivate = channels.filter((c) => c.kind !== 'dm' && c.joined)
  const dms = dmsQuery.data?.data ?? []

  function openProfile(mode: 'view' | 'edit') {
    setProfileMode(mode)
    setProfileOpen(true)
    setFooterMenuOpen(false)
  }

  async function logout() {
    setFooterMenuOpen(false)
    await api.logout()
    queryClient.clear()
    window.location.href = '/login'
  }

  async function leaveChannel(channelId: string) {
    setLeaveMenuId(null)
    await api.leaveChannel(channelId)
    await queryClient.invalidateQueries({ queryKey: ['channels'] })
  }

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

      <div className="lbl relative mb-1 flex items-center gap-1 px-1">
        <span>Channels</span>
        <button
          type="button"
          title="New channel"
          aria-label="New channel"
          onClick={() => setCreateOpen(true)}
          className="ml-auto px-1 text-sm leading-none text-ink-3 hover:text-teal"
        >
          +
        </button>
        <button
          type="button"
          title="More channel options"
          aria-label="More channel options"
          onClick={() => setChannelMenuOpen((v) => !v)}
          className="px-1 text-sm leading-none text-ink-3 hover:text-teal"
        >
          ⋯
        </button>
        {channelMenuOpen && (
          <PopoverMenu anchorClassName="right-0 top-full mt-1" onClose={() => setChannelMenuOpen(false)}>
            <MenuItem
              onClick={() => {
                setChannelMenuOpen(false)
                setCreateOpen(true)
              }}
            >
              Create channel
            </MenuItem>
            <MenuItem
              onClick={() => {
                setChannelMenuOpen(false)
                setBrowseOpen(true)
              }}
            >
              Browse channels
            </MenuItem>
          </PopoverMenu>
        )}
      </div>
      <ul className="mb-4 flex flex-col gap-0.5">
        {publicAndPrivate.length === 0 && (
          <li className="px-2 py-1 text-sm text-ink-3">No channels yet.</li>
        )}
        {publicAndPrivate.map((c) => {
          const unread = unreadByChannel.get(c.id)
          const canLeave = c.kind === 'public'
          return (
            <li key={c.id} className="group/row relative">
              <NavLink
                to={`/c/${c.slug}`}
                onContextMenu={(e) => {
                  if (!canLeave) return
                  e.preventDefault()
                  setLeaveMenuId(c.id)
                }}
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
                {canLeave && (
                  <button
                    type="button"
                    title="Channel options"
                    aria-label={`Channel options for #${c.name}`}
                    onClick={(e) => {
                      e.preventDefault()
                      e.stopPropagation()
                      setLeaveMenuId(c.id)
                    }}
                    className="ml-auto hidden shrink-0 px-1 text-sm leading-none text-ink-3 hover:text-ink group-hover/row:block"
                  >
                    ⋮
                  </button>
                )}
              </NavLink>
              {leaveMenuId === c.id && (
                <PopoverMenu anchorClassName="right-0 top-full mt-1" onClose={() => setLeaveMenuId(null)}>
                  <MenuItem onClick={() => leaveChannel(c.id)} danger>
                    Leave channel
                  </MenuItem>
                </PopoverMenu>
              )}
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
        <div className="relative mt-auto border-t border-rule pt-3">
          <button
            type="button"
            onClick={() => setFooterMenuOpen((v) => !v)}
            className="flex w-full items-center gap-2 px-1 text-left"
          >
            <Avatar
              name={auth.user.display_name || auth.user.username}
              color={auth.user.avatar_color}
              avatarUrl={auth.user.avatar_url}
              size={26}
            />
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
          </button>
          {footerMenuOpen && (
            <PopoverMenu anchorClassName="bottom-full left-1 mb-1" onClose={() => setFooterMenuOpen(false)}>
              <MenuItem onClick={() => openProfile('view')}>View profile</MenuItem>
              <MenuItem onClick={() => openProfile('edit')}>Edit profile</MenuItem>
              <div className="my-1 h-px bg-rule" />
              <MenuItem onClick={logout} danger>
                Log out
              </MenuItem>
            </PopoverMenu>
          )}
        </div>
      )}
      {auth?.user && <ProfileModal open={profileOpen} onClose={() => setProfileOpen(false)} user={auth.user} mode={profileMode} />}
      <CreateChannelModal open={createOpen} onClose={() => setCreateOpen(false)} />
      <BrowseChannelsModal open={browseOpen} onClose={() => setBrowseOpen(false)} />
    </nav>
  )
}
