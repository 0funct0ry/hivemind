import { useQuery } from '@tanstack/react-query'
import { NavLink, useLocation, useNavigate } from 'react-router-dom'
import { useEffect, useState } from 'react'
import { api, type Channel, type DM } from '../lib/api'
import { useAuth } from '../hooks/useAuth'
import { useUiStore } from '../store/ui'
import { wsClient, type ConnectionState } from '../lib/ws'
import { Avatar } from './Avatar'
import { CreateChannelModal } from './CreateChannelModal'
import { BrowseChannelsModal } from './BrowseChannelsModal'
import { NewMessageModal } from './NewMessageModal'
import { PopoverMenu, MenuItem } from './PopoverMenu'
import { dmDisplayName, dmIsOnline } from '../lib/dm'
import { useQueryClient } from '@tanstack/react-query'

/** Matches the mockup's `.badge`/`.badge.mute`: the unread count always renders as a pill —
 * pollen when the channel has a mention, muted ink-3 gray otherwise. Never a bare dot. */
function UnreadIndicator({ count, mentioned }: { count: number; mentioned: boolean }) {
  if (count === 0) return null
  return (
    <span
      className={
        'shrink-0 rounded-full px-1.5 py-0.5 font-mono text-[9.5px] font-semibold leading-none text-white ' +
        (mentioned ? 'bg-pollen' : 'bg-ink-3')
      }
    >
      {count}
    </span>
  )
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
  const [createOpen, setCreateOpen] = useState(false)
  const [browseOpen, setBrowseOpen] = useState(false)
  const [footerMenuOpen, setFooterMenuOpen] = useState(false)
  const [channelMenuOpen, setChannelMenuOpen] = useState(false)
  const [leaveMenuId, setLeaveMenuId] = useState<string | null>(null)
  const [dmMenuId, setDmMenuId] = useState<string | null>(null)
  const [newMessageOpen, setNewMessageOpen] = useState(false)
  const queryClient = useQueryClient()
  const location = useLocation()
  const navigate = useNavigate()
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
    (unreadsQuery.data?.channels ?? []).map((u) => [u.channel_id, u]),
  )
  const online = new Set(presenceQuery.data?.online ?? [])

  const channels = channelsQuery.data?.data ?? []
  const publicAndPrivate = channels.filter((c) => c.kind !== 'dm' && c.kind !== 'group_dm' && c.joined)
  const dms = dmsQuery.data?.data ?? []

  async function logout() {
    setFooterMenuOpen(false)
    await api.logout()
    queryClient.clear()
    window.location.href = '/login'
  }

  async function leaveChannel(channel: Channel) {
    setLeaveMenuId(null)
    const wasOpen = location.pathname === `/c/${channel.slug}`
    await api.leaveChannel(channel.id)
    await queryClient.invalidateQueries({ queryKey: ['channels'] })
    // Navigate away immediately rather than leaving the main pane to render whatever empty
    // state ChannelView falls back to for a channel that just vanished out from under it.
    if (wasOpen) navigate('/')
  }

  async function removeConversation(dm: DM) {
    setDmMenuId(null)
    const wasOpen =
      location.pathname === `/dm/id/${dm.id}` ||
      (dm.kind === 'dm' && dm.peer && location.pathname === `/dm/${dm.peer.username}`)
    await api.hideDM(dm.id)
    await queryClient.invalidateQueries({ queryKey: ['dms'] })
    if (wasOpen) navigate('/')
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
                {/* Badge + kebab share one fixed-width trailing group so the kebab's
                    hover-only reveal (opacity, not display) never shifts the badge. */}
                <span className="ml-auto flex shrink-0 items-center gap-1">
                  {unread && (
                    <UnreadIndicator count={unread.unread_count} mentioned={unread.has_mention} />
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
                      className="px-1 text-sm leading-none text-ink-3 opacity-0 hover:text-ink focus-visible:opacity-100 group-hover/row:opacity-100"
                    >
                      ⋮
                    </button>
                  )}
                </span>
              </NavLink>
              {leaveMenuId === c.id && (
                <PopoverMenu anchorClassName="right-0 top-full mt-1" onClose={() => setLeaveMenuId(null)}>
                  <MenuItem onClick={() => leaveChannel(c)} danger>
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
          onClick={() => setNewMessageOpen(true)}
          className="ml-auto px-1 text-sm leading-none text-ink-3 hover:text-teal"
        >
          +
        </button>
      </div>
      <ul className="flex flex-col gap-0.5">
        {dms.length === 0 && (
          <li className="px-2 py-1">
            <p className="text-sm text-ink-3">No direct messages yet.</p>
            <button
              type="button"
              onClick={() => setNewMessageOpen(true)}
              className="mt-1 text-sm text-teal hover:underline"
            >
              Start a conversation
            </button>
          </li>
        )}
        {dms.map((d: DM) => {
          const unread = unreadByChannel.get(d.id)
          const isOnline = dmIsOnline(d, online)
          const name = dmDisplayName(d)
          return (
            <li key={d.id} className="group/row relative">
              <NavLink
                to={`/dm/id/${d.id}`}
                onContextMenu={(e) => {
                  e.preventDefault()
                  setDmMenuId(d.id)
                }}
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
                <span className="truncate">{name}</span>
                {/* Badge + kebab share one fixed-width trailing group, same as channel
                    rows, so the kebab's hover-only reveal never shifts the badge. */}
                <span className="ml-auto flex shrink-0 items-center gap-1">
                  {unread && (
                    <UnreadIndicator count={unread.unread_count} mentioned={unread.has_mention} />
                  )}
                  <button
                    type="button"
                    title="Conversation options"
                    aria-label={`Conversation options for ${name}`}
                    onClick={(e) => {
                      e.preventDefault()
                      e.stopPropagation()
                      setDmMenuId(d.id)
                    }}
                    className="px-1 text-sm leading-none text-ink-3 opacity-0 hover:text-ink focus-visible:opacity-100 group-hover/row:opacity-100"
                  >
                    ⋮
                  </button>
                </span>
              </NavLink>
              {dmMenuId === d.id && (
                <PopoverMenu anchorClassName="right-0 top-full mt-1" onClose={() => setDmMenuId(null)}>
                  <MenuItem onClick={() => removeConversation(d)} danger>
                    Remove conversation
                  </MenuItem>
                </PopoverMenu>
              )}
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
              <MenuItem
                onClick={() => {
                  setFooterMenuOpen(false)
                  navigate('/settings/profile', { state: { from: location.pathname } })
                }}
              >
                Profile
              </MenuItem>
              <MenuItem
                onClick={() => {
                  setFooterMenuOpen(false)
                  navigate('/settings/api-keys', { state: { from: location.pathname } })
                }}
              >
                API keys
              </MenuItem>
              <div className="my-1 h-px bg-rule" />
              <MenuItem
                onClick={() => {
                  setFooterMenuOpen(false)
                  navigate('/settings/webhooks', { state: { from: location.pathname } })
                }}
              >
                Settings
              </MenuItem>
              {auth.user.role === 'admin' && (
                <MenuItem
                  onClick={() => {
                    setFooterMenuOpen(false)
                    navigate('/admin/sessions')
                  }}
                >
                  Sessions
                </MenuItem>
              )}
              <div className="my-1 h-px bg-rule" />
              <MenuItem onClick={logout} danger>
                Log out
              </MenuItem>
            </PopoverMenu>
          )}
        </div>
      )}
      <CreateChannelModal open={createOpen} onClose={() => setCreateOpen(false)} />
      <BrowseChannelsModal open={browseOpen} onClose={() => setBrowseOpen(false)} />
      <NewMessageModal open={newMessageOpen} onClose={() => setNewMessageOpen(false)} />
    </nav>
  )
}
