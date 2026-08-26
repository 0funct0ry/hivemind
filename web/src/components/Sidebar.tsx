import { useQuery } from '@tanstack/react-query'
import { NavLink } from 'react-router-dom'
import { api } from '../lib/api'

function UnreadBadge({ count, mentioned }: { count: number; mentioned: boolean }) {
  if (count === 0) return null
  return (
    <span
      className={
        'ml-auto rounded-full px-1.5 py-0.5 font-mono text-[11px] leading-none ' +
        (mentioned ? 'bg-pollen text-white' : 'bg-paper-3 text-ink-2')
      }
    >
      {count}
    </span>
  )
}

export function Sidebar() {
  const channelsQuery = useQuery({ queryKey: ['channels'], queryFn: api.listChannels })
  const dmsQuery = useQuery({ queryKey: ['dms'], queryFn: api.listDMs })
  const unreadsQuery = useQuery({ queryKey: ['unreads'], queryFn: api.unreadSummary })

  const unreadByChannel = new Map(
    (unreadsQuery.data?.data ?? []).map((u) => [u.channel_id, u]),
  )

  const channels = channelsQuery.data?.data ?? []
  const publicAndPrivate = channels.filter((c) => c.kind !== 'dm')
  const dms = dmsQuery.data?.data ?? []

  return (
    <nav aria-label="Channels and direct messages" className="flex h-full flex-col border-r border-rule bg-paper-2 p-3">
      <div className="mb-4 px-1 font-display text-lg font-semibold text-ink">hivemind</div>

      <div className="mb-1 px-1 font-mono text-[11px] uppercase tracking-wide text-ink-3">Channels</div>
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
                  <UnreadBadge count={unread.unread_count} mentioned={unread.mention_count > 0} />
                )}
              </NavLink>
            </li>
          )
        })}
      </ul>

      <div className="mb-1 px-1 font-mono text-[11px] uppercase tracking-wide text-ink-3">Direct messages</div>
      <ul className="flex flex-col gap-0.5">
        {dms.length === 0 && <li className="px-2 py-1 text-sm text-ink-3">No direct messages yet.</li>}
        {dms.map((d) => {
          const unread = unreadByChannel.get(d.id)
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
                  className="h-4 w-4 shrink-0 rounded-full"
                  style={{ backgroundColor: d.peer.avatar_color }}
                  aria-hidden
                />
                <span className="truncate">{d.peer.display_name || d.peer.username}</span>
                {unread && (
                  <UnreadBadge count={unread.unread_count} mentioned={unread.mention_count > 0} />
                )}
              </NavLink>
            </li>
          )
        })}
      </ul>
    </nav>
  )
}
