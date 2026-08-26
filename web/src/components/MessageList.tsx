import { forwardRef, useEffect, useImperativeHandle, useLayoutEffect, useRef, useState } from 'react'
import { useQueryClient } from '@tanstack/react-query'
import { api, type Attachment, type Message } from '../lib/api'
import { renderMarkdown } from '../lib/markdown'
import { useMessages } from '../hooks/useMessages'
import { useUiStore } from '../store/ui'
import { prefersReducedMotion } from '../lib/throttle'

const GROUP_WINDOW_MS = 5 * 60 * 1000

function formatTime(ts: number): string {
  return new Date(ts).toLocaleTimeString(undefined, { hour: 'numeric', minute: '2-digit' })
}

function formatDay(ts: number): string {
  return new Date(ts).toLocaleDateString(undefined, { weekday: 'long', month: 'long', day: 'numeric' })
}

function dayKey(ts: number): string {
  const d = new Date(ts)
  return `${d.getFullYear()}-${d.getMonth()}-${d.getDate()}`
}

function relativeTime(ts: number): string {
  const diffMin = Math.max(1, Math.round((Date.now() - ts) / 60000))
  if (diffMin < 60) return `${diffMin}m ago`
  const diffHr = Math.round(diffMin / 60)
  if (diffHr < 24) return `${diffHr}h ago`
  return `${Math.round(diffHr / 24)}d ago`
}

function AttachmentView({ att }: { att: Attachment }) {
  const isImage = att.mime.startsWith('image/')
  const [lightbox, setLightbox] = useState(false)

  if (isImage) {
    return (
      <>
        <button type="button" onClick={() => setLightbox(true)} className="mt-1 block">
          <img
            src={att.url}
            alt={att.name}
            className="max-h-[360px] max-w-[360px] rounded-md border border-rule object-cover"
          />
        </button>
        {lightbox && (
          <div
            className="fixed inset-0 z-50 flex items-center justify-center bg-black/80 p-8"
            onClick={() => setLightbox(false)}
          >
            <img src={att.url} alt={att.name} className="max-h-full max-w-full rounded-md" />
          </div>
        )}
      </>
    )
  }

  return (
    <a
      href={att.url}
      download={att.name}
      className="mt-1 flex w-fit items-center gap-2 rounded-md border border-rule bg-paper-2 px-3 py-2 text-sm text-ink-2 hover:bg-paper-3"
    >
      <span aria-hidden>📎</span>
      <span className="truncate">{att.name}</span>
      <span className="font-mono text-xs text-ink-3">{Math.round(att.size / 1024)} KB</span>
    </a>
  )
}

function ThreadStrip({ message, onOpen }: { message: Message; onOpen: () => void }) {
  if (message.reply_count <= 0) return null
  return (
    <button
      type="button"
      onClick={onOpen}
      className="mt-1 flex w-fit items-center gap-2 rounded-md px-2 py-1 text-sm text-teal hover:bg-teal-soft"
    >
      <span>
        {message.reply_count} {message.reply_count === 1 ? 'reply' : 'replies'}
      </span>
      <span className="text-ink-3">· last {relativeTime(message.created_at)}</span>
    </button>
  )
}

function MessageRow({
  message,
  grouped,
  currentUsername,
  onOpenThread,
}: {
  message: Message
  grouped: boolean
  currentUsername?: string
  onOpenThread: (id: string) => void
}) {
  const name = message.user?.display_name || message.user?.username || 'Unknown'
  return (
    <div
      data-message-id={message.id}
      className={
        'group grid grid-cols-[34px_minmax(0,1fr)] gap-2 px-4 transition-colors duration-1000 ' +
        (grouped ? 'py-0.5 ' : 'py-1.5 ')
      }
    >
      <div className="flex items-start justify-center pt-1">
        {grouped ? (
          <time className="text-[10px] text-ink-3 opacity-0 group-hover:opacity-100">
            {formatTime(message.created_at)}
          </time>
        ) : (
          <span
            className="h-7 w-7 shrink-0 rounded-full"
            style={{ backgroundColor: message.user?.avatar_color ?? '#999' }}
            aria-hidden
          />
        )}
      </div>
      <div className={message.status === 'sending' ? 'opacity-60' : ''}>
        {!grouped && (
          <div className="flex items-baseline gap-2">
            <b className="font-display text-sm font-semibold text-ink">{name}</b>
            {message.user?.is_bot && (
              <span className="rounded bg-paper-3 px-1 font-mono text-[10px] text-ink-3">BOT</span>
            )}
            <time className="font-mono text-[11px] text-ink-3">{formatTime(message.created_at)}</time>
          </div>
        )}
        <div className="text-sm leading-relaxed text-ink [&_a]:break-all">
          {renderMarkdown(message.body, { currentUsername })}
        </div>
        {message.attachments.map((a) => (
          <AttachmentView key={a.id} att={a} />
        ))}
        {message.status === 'failed' && (
          <div className="mt-1 text-xs text-red-600">
            Didn&apos;t send — <span className="cursor-pointer underline">retry</span>
          </div>
        )}
        <ThreadStrip message={message} onOpen={() => onOpenThread(message.id)} />
      </div>
    </div>
  )
}

export interface MessageListHandle {
  /** Scrolls the given message into view, optionally fetching a page around it first,
   * and optionally applying a transient highlight flash. */
  scrollToMessage: (messageId: string, opts?: { fetchIfMissing?: boolean; highlight?: boolean }) => Promise<void>
}

export const MessageList = forwardRef<
  MessageListHandle,
  { channelId: string; lastReadMessageId: string | null; currentUsername?: string }
>(function MessageList({ channelId, lastReadMessageId, currentUsername }, ref) {
  const { messages, fetchNextPage, hasNextPage, isFetchingNextPage } = useMessages(channelId)
  const openThread = useUiStore((s) => s.openThread)
  const setUnreadAnchor = useUiStore((s) => s.setUnreadAnchor)
  const unreadAnchor = useUiStore((s) => s.unreadAnchors[channelId])
  const queryClient = useQueryClient()

  const scrollRef = useRef<HTMLDivElement>(null)
  const sentinelRef = useRef<HTMLDivElement>(null)
  const prevScrollHeight = useRef(0)

  useEffect(() => {
    setUnreadAnchor(channelId, lastReadMessageId)
  }, [channelId, lastReadMessageId, setUnreadAnchor])

  useEffect(() => {
    const el = sentinelRef.current
    const scroller = scrollRef.current
    if (!el || !scroller) return
    const observer = new IntersectionObserver(
      (entries) => {
        if (entries[0]?.isIntersecting && hasNextPage && !isFetchingNextPage) {
          prevScrollHeight.current = scroller.scrollHeight
          fetchNextPage()
        }
      },
      { root: scroller },
    )
    observer.observe(el)
    return () => observer.disconnect()
  }, [fetchNextPage, hasNextPage, isFetchingNextPage])

  useLayoutEffect(() => {
    const scroller = scrollRef.current
    if (!scroller || prevScrollHeight.current === 0) return
    const delta = scroller.scrollHeight - prevScrollHeight.current
    scroller.scrollTop += delta
    prevScrollHeight.current = 0
  }, [messages.length])

  const bottomRef = useRef<HTMLDivElement>(null)
  const isNearBottomRef = useRef(true)
  useLayoutEffect(() => {
    if (isNearBottomRef.current) bottomRef.current?.scrollIntoView({ block: 'end' })
  }, [messages.length])

  const handleScroll = () => {
    const scroller = scrollRef.current
    if (!scroller) return
    isNearBottomRef.current = scroller.scrollHeight - scroller.scrollTop - scroller.clientHeight < 120
  }

  const findRow = (messageId: string) =>
    scrollRef.current?.querySelector<HTMLElement>(`[data-message-id="${messageId}"]`) ?? null

  const scrollAndHighlight = (el: HTMLElement, highlight: boolean) => {
    el.scrollIntoView({ block: 'center', behavior: prefersReducedMotion() ? 'auto' : 'smooth' })
    if (!highlight) return
    el.classList.add('bg-pollen-soft')
    window.setTimeout(() => el.classList.remove('bg-pollen-soft'), 1200)
  }

  useImperativeHandle(
    ref,
    () => ({
      scrollToMessage: async (messageId, opts = {}) => {
        const existing = findRow(messageId)
        if (existing) {
          scrollAndHighlight(existing, opts.highlight ?? false)
          return
        }
        if (!opts.fetchIfMissing) return
        const fetched = await api.listMessages(channelId, { around: messageId, limit: 50 })
        queryClient.setQueryData(['messages', channelId], {
          pages: [{ data: fetched.data, has_more: fetched.has_more, next_before: fetched.next_before }],
          pageParams: [undefined],
        })
        // Wait for the new page to render before measuring/scrolling.
        await new Promise((resolve) => requestAnimationFrame(() => requestAnimationFrame(resolve)))
        const el = findRow(messageId)
        if (el) scrollAndHighlight(el, opts.highlight ?? false)
      },
    }),
    [channelId, queryClient],
  )

  if (messages.length === 0) {
    return (
      <div role="log" aria-live="polite" className="flex flex-1 items-center justify-center text-ink-3">
        No messages yet — say hello.
      </div>
    )
  }

  let lastAuthor: string | null = null
  let lastTs = 0
  let lastDayKey = ''
  let dividerRendered = false

  return (
    <div
      ref={scrollRef}
      onScroll={handleScroll}
      role="log"
      aria-live="polite"
      className="flex-1 overflow-y-auto"
    >
      <div ref={sentinelRef} />
      {isFetchingNextPage && <div className="py-2 text-center text-xs text-ink-3">Loading…</div>}
      {messages.map((m) => {
        const dk = dayKey(m.created_at)
        const showDay = dk !== lastDayKey
        const showUnreadDivider = Boolean(
          !dividerRendered && unreadAnchor && m.id !== unreadAnchor && Number(m.id) > Number(unreadAnchor),
        )
        const grouped = lastAuthor === m.user_id && m.created_at - lastTs < GROUP_WINDOW_MS && !showUnreadDivider

        lastAuthor = m.user_id
        lastTs = m.created_at
        lastDayKey = dk
        if (showUnreadDivider) dividerRendered = true

        return (
          <div key={m.id}>
            {showDay && (
              <div className="sticky top-0 z-10 flex justify-center py-2">
                <span className="rounded-full bg-paper-2 px-3 py-1 font-mono text-[11px] text-ink-2 shadow-sm">
                  {formatDay(m.created_at)}
                </span>
              </div>
            )}
            {showUnreadDivider && (
              <div className="relative my-1 flex items-center px-4">
                <div className="h-px flex-1 bg-pollen" />
                <span className="mx-2 font-mono text-[11px] font-semibold uppercase text-pollen">New</span>
                <div className="h-px flex-1 bg-pollen" />
              </div>
            )}
            <MessageRow
              message={m}
              grouped={grouped}
              currentUsername={currentUsername}
              onOpenThread={openThread}
            />
          </div>
        )
      })}
      <div ref={bottomRef} />
    </div>
  )
})
