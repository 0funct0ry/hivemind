import { useEffect } from 'react'
import { useQuery } from '@tanstack/react-query'
import { renderMarkdown } from '../lib/markdown'
import { useThread } from '../hooks/useMessages'
import { useUiStore } from '../store/ui'
import { formatTime } from '../lib/time'
import { shouldGroup } from '../lib/messageGrouping'
import { api, type Channel, type DM } from '../lib/api'
import { Composer } from './Composer'

/** Resolves a channel/DM id to a human "#slug" or "@username" label from the cached
 * channels/DMs lists, for the thread panel's header subtitle. */
function useChannelLabel(channelId: string | undefined): string | null {
  const channelsQuery = useQuery({ queryKey: ['channels'], queryFn: api.listChannels, enabled: !!channelId })
  const dmsQuery = useQuery({ queryKey: ['dms'], queryFn: api.listDMs, enabled: !!channelId })
  if (!channelId) return null
  const channel = channelsQuery.data?.data.find((c: Channel) => c.id === channelId)
  if (channel) return `#${channel.slug ?? channel.name}`
  const dm = dmsQuery.data?.data.find((d: DM) => d.id === channelId)
  if (dm) return `@${dm.peer.username}`
  return null
}

export function ThreadPanel({ currentUsername }: { currentUsername?: string }) {
  const openThreadId = useUiStore((s) => s.openThreadId)
  const closeThread = useUiStore((s) => s.closeThread)
  const { data, isLoading } = useThread(openThreadId)
  const channelLabel = useChannelLabel(data?.root.channel_id)

  useEffect(() => {
    if (!openThreadId) return
    const handler = (e: KeyboardEvent) => {
      if (e.key === 'Escape') closeThread()
    }
    window.addEventListener('keydown', handler)
    return () => window.removeEventListener('keydown', handler)
  }, [openThreadId, closeThread])

  if (!openThreadId) return null

  let lastAuthor: string | null = null
  let lastTs = 0

  return (
    <aside className="flex h-full flex-col border-l border-rule bg-paper">
      <div className="flex items-center justify-between border-b border-rule px-4 py-3">
        <div className="flex items-baseline gap-2">
          <h3 className="font-display text-sm font-semibold text-ink">Thread</h3>
          {channelLabel && <span className="font-mono text-[9px] text-ink-3">{channelLabel}</span>}
        </div>
        <button
          type="button"
          onClick={closeThread}
          aria-label="Close thread"
          className="text-lg text-ink-3 hover:text-ink"
        >
          ×
        </button>
      </div>
      <div className="flex-1 overflow-y-auto">
        {isLoading && <div className="p-4 text-sm text-ink-3">Loading…</div>}
        {data && (
          <>
            <div className="border-b border-rule px-4 py-3">
              <div className="flex items-baseline gap-2">
                <span
                  className="h-6 w-6 rounded-full"
                  style={{ backgroundColor: data.root.user?.avatar_color ?? '#999' }}
                  aria-hidden
                />
                <b className="font-display text-sm font-semibold text-ink">
                  {data.root.user?.display_name || data.root.user?.username}
                </b>
                <time className="font-mono text-[9px] text-ink-3">{formatTime(data.root.created_at)}</time>
              </div>
              <div className="mt-1 text-sm text-ink">{renderMarkdown(data.root.body, { currentUsername })}</div>
            </div>
            <div className="px-4 py-2 font-mono text-[11px] uppercase text-ink-3">
              {data.data.length} {data.data.length === 1 ? 'reply' : 'replies'}
            </div>
            <div className="flex flex-col gap-1 px-4 pb-4">
              {data.data.map((reply) => {
                const grouped = shouldGroup(lastAuthor, lastTs, reply.user_id, reply.created_at, false)
                lastAuthor = reply.user_id
                lastTs = reply.created_at
                return (
                  <div key={reply.id} className="group flex items-baseline gap-2 py-1">
                    {grouped ? (
                      <time className="w-5 shrink-0 text-[9px] text-ink-3 opacity-0 group-hover:opacity-100">
                        {formatTime(reply.created_at)}
                      </time>
                    ) : (
                      <span
                        className="h-5 w-5 shrink-0 rounded-full"
                        style={{ backgroundColor: reply.user?.avatar_color ?? '#999' }}
                        aria-hidden
                      />
                    )}
                    <div>
                      {!grouped && (
                        <div className="flex items-baseline gap-2">
                          <b className="font-display text-xs font-semibold text-ink">
                            {reply.user?.display_name || reply.user?.username}
                          </b>
                          <time className="font-mono text-[9px] text-ink-3">{formatTime(reply.created_at)}</time>
                        </div>
                      )}
                      <div className="text-sm text-ink">{renderMarkdown(reply.body, { currentUsername })}</div>
                    </div>
                  </div>
                )
              })}
            </div>
          </>
        )}
      </div>
      {data && <Composer channelId={data.root.channel_id} threadId={data.root.id} placeholder="Reply…" />}
    </aside>
  )
}
