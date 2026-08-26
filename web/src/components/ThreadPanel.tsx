import { useEffect } from 'react'
import { renderMarkdown } from '../lib/markdown'
import { useThread } from '../hooks/useMessages'
import { useUiStore } from '../store/ui'
import { Composer } from './Composer'

export function ThreadPanel({ currentUsername }: { currentUsername?: string }) {
  const openThreadId = useUiStore((s) => s.openThreadId)
  const closeThread = useUiStore((s) => s.closeThread)
  const { data, isLoading } = useThread(openThreadId)

  useEffect(() => {
    if (!openThreadId) return
    const handler = (e: KeyboardEvent) => {
      if (e.key === 'Escape') closeThread()
    }
    window.addEventListener('keydown', handler)
    return () => window.removeEventListener('keydown', handler)
  }, [openThreadId, closeThread])

  if (!openThreadId) return null

  return (
    <aside className="flex h-full flex-col border-l border-rule bg-paper">
      <div className="flex items-center justify-between border-b border-rule px-4 py-3">
        <h3 className="font-display text-sm font-semibold text-ink">Thread</h3>
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
              </div>
              <div className="mt-1 text-sm text-ink">{renderMarkdown(data.root.body, { currentUsername })}</div>
            </div>
            <div className="px-4 py-2 font-mono text-[11px] uppercase text-ink-3">
              {data.data.length} {data.data.length === 1 ? 'reply' : 'replies'}
            </div>
            <div className="flex flex-col gap-3 px-4 pb-4">
              {data.data.map((reply) => (
                <div key={reply.id} className="flex items-baseline gap-2">
                  <span
                    className="h-5 w-5 shrink-0 rounded-full"
                    style={{ backgroundColor: reply.user?.avatar_color ?? '#999' }}
                    aria-hidden
                  />
                  <div>
                    <b className="font-display text-xs font-semibold text-ink">
                      {reply.user?.display_name || reply.user?.username}
                    </b>
                    <div className="text-sm text-ink">{renderMarkdown(reply.body, { currentUsername })}</div>
                  </div>
                </div>
              ))}
            </div>
          </>
        )}
      </div>
      {data && <Composer channelId={data.root.channel_id} threadId={data.root.id} placeholder="Reply…" />}
    </aside>
  )
}
