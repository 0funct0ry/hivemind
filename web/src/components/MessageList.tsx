import { forwardRef, useEffect, useImperativeHandle, useLayoutEffect, useRef, useState } from 'react'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { useNavigate } from 'react-router-dom'
import { api, type Attachment, type Message, type MessageUser } from '../lib/api'
import { renderMarkdown } from '../lib/markdown'
import { useDeleteMessage, useMessages, useToggleReaction } from '../hooks/useMessages'
import { useUiStore } from '../store/ui'
import { prefersReducedMotion } from '../lib/throttle'
import { formatTime, formatDay, dayKey, relativeTime } from '../lib/time'
import { shouldGroup } from '../lib/messageGrouping'
import { fileTypeAbbrev } from '../lib/fileType'
import { useUserName, useUserProfile } from '../hooks/useUserName'
import { Avatar } from './Avatar'
import { PopoverMenu, MenuItem } from './PopoverMenu'
import { DeleteMessageDialog } from './DeleteMessageDialog'
import { EmojiPicker } from './EmojiPicker'
import { QUICK_REACTIONS } from '../data/emojis'

const EDIT_WINDOW_MS = 15 * 60 * 1000
const REACTION_SPAM_WINDOW_MS = 2000
const REACTION_SPAM_MAX_CLICKS = 3

/** Chooses the deleted-message placeholder line from who's looking and who deleted it (SPEC §6.4). */
function deletedPlaceholder(message: Message, currentUserId?: string): string {
  const deletedBy = message.deleted_by
  if (!deletedBy || deletedBy.is_self) return 'This message has been deleted.'
  if (deletedBy.id === currentUserId) return 'Message deleted (Moderated)'
  if (message.user_id === currentUserId) return 'Your message was deleted by an administrator.'
  return 'This message has been deleted.'
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
            className="max-h-[340px] max-w-[340px] rounded-md border border-rule object-cover"
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
      className="mt-1.5 flex max-w-[330px] items-center gap-[9px] rounded-md border border-rule bg-paper px-[10px] py-[7px] text-sm text-ink-2 hover:bg-paper-3"
    >
      <span className="grid h-[26px] w-[26px] shrink-0 place-items-center rounded bg-paper-3 font-mono text-[8px] text-ink-2">
        {fileTypeAbbrev(att.name)}
      </span>
      <span className="min-w-0">
        <span className="block truncate text-[13px] font-medium text-ink">{att.name}</span>
        <span className="font-mono text-[9px] text-ink-3">{Math.round(att.size / 1024)} KB</span>
      </span>
    </a>
  )
}

/** A single thread-strip face, resolving its avatar live (see `useUserProfile`) rather than
 * from the possibly-stale snapshot embedded in the cached thread-preview reply. */
function ThreadFaceAvatar({ user }: { user: MessageUser }) {
  const profile = useUserProfile(user.id, user)
  return (
    <Avatar
      name={profile.displayName}
      color={profile.avatarColor}
      avatarUrl={profile.avatarUrl}
      size={18}
      className="-ml-1.5 border border-paper first:ml-0"
    />
  )
}

function ThreadStrip({ message, onOpen }: { message: Message; onOpen: () => void }) {
  const { data } = useQuery({
    queryKey: ['thread-preview', message.id],
    queryFn: () => api.listReplies(message.id, { limit: 3 }),
    enabled: message.reply_count > 0,
    staleTime: 30_000,
  })

  if (message.reply_count <= 0) return null

  const faces: MessageUser[] = []
  const seen = new Set<string>()
  for (const reply of data?.data ?? []) {
    if (!reply.user || seen.has(reply.user.id)) continue
    seen.add(reply.user.id)
    faces.push(reply.user)
    if (faces.length >= 3) break
  }

  return (
    <button
      type="button"
      onClick={onOpen}
      className="mt-1 flex w-fit items-center gap-2 rounded-md px-2 py-1 text-sm text-teal hover:bg-teal-soft"
    >
      {faces.length > 0 && (
        <span className="flex">
          {faces.map((f) => (
            <ThreadFaceAvatar key={f.id} user={f} />
          ))}
        </span>
      )}
      <span>
        {message.reply_count} {message.reply_count === 1 ? 'reply' : 'replies'}
      </span>
      <span className="text-ink-3">· last {relativeTime(message.created_at)}</span>
    </button>
  )
}

/** A single reactor name resolved for a badge tooltip: "You" for the current user, the display
 * name otherwise, or "[Deactivated Member]" for a deactivated reactor (SPEC.md §6.4). */
function ReactorName({ userId, currentUserId }: { userId: string; currentUserId?: string }) {
  const userQuery = useQuery({ queryKey: ['user', userId], queryFn: () => api.getUser(userId), staleTime: Infinity })
  const name = useUserName(userId)
  if (userId === currentUserId) return <>You</>
  if (userQuery.data?.user.status === 'deactivated') return <>[Deactivated Member]</>
  return <>{name}</>
}

function ReactionTooltip({ userIds, currentUserId }: { userIds: string[]; currentUserId?: string }) {
  const ordered = currentUserId && userIds.includes(currentUserId) ? [currentUserId, ...userIds.filter((id) => id !== currentUserId)] : userIds
  return (
    <span className="pointer-events-none absolute bottom-full left-1/2 z-30 mb-1.5 hidden -translate-x-1/2 whitespace-nowrap rounded-md border border-rule bg-paper px-2 py-1 text-xs text-ink shadow-lg group-hover/badge:block">
      {ordered.map((id, i) => (
        <span key={id}>
          {i > 0 && ', '}
          <ReactorName userId={id} currentUserId={currentUserId} />
        </span>
      ))}
    </span>
  )
}

/** The row of emoji pill badges below a message's body — one per distinct reaction, ordered by
 * first-applied time (server-side, SPEC.md §4.3). Exported so ThreadPanel can render the same
 * badges for the root/replies it renders outside MessageList. */
export function ReactionBadges({
  message,
  currentUserId,
  threadId,
}: {
  message: Message
  currentUserId?: string
  threadId?: string
}) {
  const toggle = useToggleReaction(message.channel_id, currentUserId)
  const clickTimes = useRef<Map<string, number[]>>(new Map())

  if (message.reactions.length === 0) return null

  const handleToggle = (emoji: string, active: boolean) => {
    const now = Date.now()
    const times = (clickTimes.current.get(emoji) ?? []).filter((t) => now - t < REACTION_SPAM_WINDOW_MS)
    if (times.length >= REACTION_SPAM_MAX_CLICKS) return
    times.push(now)
    clickTimes.current.set(emoji, times)

    toggle.mutate({ messageId: message.id, emoji, action: active ? 'remove' : 'add', threadId })
  }

  return (
    <div className="mt-1 flex flex-wrap gap-1">
      {message.reactions.map((r) => {
        const active = !!currentUserId && r.user_ids.includes(currentUserId)
        return (
          <button
            key={r.emoji}
            type="button"
            onClick={() => handleToggle(r.emoji, active)}
            className={
              'group/badge relative flex items-center gap-1 rounded-full border px-1.5 py-0.5 text-xs ' +
              (active ? 'border-teal bg-teal-soft text-teal' : 'border-rule bg-paper text-ink-2 hover:bg-paper-3')
            }
          >
            <span>{r.emoji}</span>
            <span className="font-mono text-[10px]">{r.user_ids.length}</span>
            <ReactionTooltip userIds={r.user_ids} currentUserId={currentUserId} />
          </button>
        )
      })}
    </div>
  )
}

/** The hover-revealed quick-reaction row (SPEC.md §6.4's 6 fixed shortcuts plus a "+" that
 * opens the full picker). */
function QuickReactions({
  message,
  currentUserId,
  threadId,
}: {
  message: Message
  currentUserId?: string
  threadId?: string
}) {
  const toggle = useToggleReaction(message.channel_id, currentUserId)
  const [pickerOpen, setPickerOpen] = useState(false)

  const react = (emoji: string) => {
    toggle.mutate({ messageId: message.id, emoji, action: 'add', threadId })
  }

  return (
    <div className="flex items-center gap-0.5">
      {QUICK_REACTIONS.map((emoji) => (
        <button
          key={emoji}
          type="button"
          title={`React with ${emoji}`}
          aria-label={`React with ${emoji}`}
          onClick={() => react(emoji)}
          className="rounded px-1 py-0.5 text-sm hover:bg-paper-3"
        >
          {emoji}
        </button>
      ))}
      <span className="relative">
        <button
          type="button"
          title="Add reaction"
          aria-label="Add reaction"
          onClick={() => setPickerOpen((v) => !v)}
          className="px-1.5 py-1 text-ink-3 hover:text-ink"
        >
          +
        </button>
        {pickerOpen && (
          <EmojiPicker
            anchorClassName="right-0 top-full mt-1"
            onSelect={(emoji) => {
              react(emoji)
              setPickerOpen(false)
            }}
            onDismiss={() => setPickerOpen(false)}
          />
        )}
      </span>
    </div>
  )
}

/** Clicking an avatar or display name opens a small popover — name, bot/role badge, presence,
 * and a Message button that starts (or reopens) a 1:1 DM with that person. */
function ProfilePopoverTrigger({ user, children }: { user: MessageUser; children: React.ReactNode }) {
  const [open, setOpen] = useState(false)
  const nav = useNavigate()
  const queryClient = useQueryClient()
  const presenceQuery = useQuery({ queryKey: ['presence'], queryFn: api.getPresence })
  const online = new Set(presenceQuery.data?.online ?? [])
  const profile = useUserProfile(user.id, user)
  const name = profile.displayName

  async function message() {
    setOpen(false)
    const r = await api.createDM([user.id])
    await queryClient.invalidateQueries({ queryKey: ['dms'] })
    nav(`/dm/id/${r.channel.id}`)
  }

  return (
    <span className="relative inline-block">
      <button type="button" onClick={() => setOpen((v) => !v)} className="rounded">
        {children}
      </button>
      {open && (
        <PopoverMenu anchorClassName="left-0 top-full mt-1" onClose={() => setOpen(false)}>
          <div className="min-w-[200px] p-3">
            <div className="flex items-center gap-2">
              <Avatar name={name} color={profile.avatarColor} avatarUrl={profile.avatarUrl} size={30} />
              <div className="min-w-0">
                <div className="flex items-center gap-1.5">
                  <span className="truncate text-sm font-semibold text-ink">{name}</span>
                  {profile.isBot && <span className="rounded bg-paper-3 px-1 font-mono text-[8px] text-ink-2">BOT</span>}
                </div>
                <div className="flex items-center gap-1.5 text-[11px] text-ink-3">
                  <span
                    className={
                      'h-[6px] w-[6px] shrink-0 rounded-full border-[1.5px] ' +
                      (online.has(user.id) ? 'border-teal bg-teal' : 'border-ink-3 bg-transparent')
                    }
                    aria-hidden
                  />
                  {online.has(user.id) ? 'Online' : 'Offline'}
                </div>
              </div>
            </div>
            <button
              type="button"
              role="menuitem"
              onClick={message}
              className="mt-2 w-full rounded bg-teal px-2 py-1.5 text-sm text-white hover:bg-[#0B564B]"
            >
              Message
            </button>
          </div>
        </PopoverMenu>
      )}
    </span>
  )
}

function MessageActions({
  message,
  canEdit,
  canDelete,
  currentUserId,
  onEdit,
}: {
  message: Message
  canEdit: boolean
  canDelete: boolean
  currentUserId?: string
  onEdit: () => void
}) {
  const [menuOpen, setMenuOpen] = useState(false)
  const [confirmOpen, setConfirmOpen] = useState(false)
  const deleteMutation = useDeleteMessage(message.channel_id)

  return (
    <div className="absolute right-3 top-0 hidden items-center gap-0.5 rounded-md border border-rule bg-paper shadow-sm group-hover:flex">
      <QuickReactions message={message} currentUserId={currentUserId} />
      {canEdit && (
        <button
          type="button"
          title="Edit message"
          aria-label="Edit message"
          onClick={onEdit}
          className="px-1.5 py-1 text-ink-3 hover:text-ink"
        >
          ✎
        </button>
      )}
      {canDelete && (
        <span className="relative">
          <button
            type="button"
            title="More actions"
            aria-label="More actions"
            onClick={() => setMenuOpen((v) => !v)}
            className="px-1.5 py-1 text-ink-3 hover:text-ink"
          >
            ⋯
          </button>
          {menuOpen && (
            <PopoverMenu anchorClassName="right-0 top-full mt-1" onClose={() => setMenuOpen(false)}>
              <MenuItem
                danger
                onClick={() => {
                  setMenuOpen(false)
                  setConfirmOpen(true)
                }}
              >
                Delete Message
              </MenuItem>
            </PopoverMenu>
          )}
        </span>
      )}
      <DeleteMessageDialog
        open={confirmOpen}
        busy={deleteMutation.isPending}
        onClose={() => setConfirmOpen(false)}
        onConfirm={() => deleteMutation.mutate(message.id, { onSuccess: () => setConfirmOpen(false) })}
      />
    </div>
  )
}

function MessageRow({
  message,
  grouped,
  currentUsername,
  currentUserId,
  currentUserRole,
  onOpenThread,
  onEditMessage,
}: {
  message: Message
  grouped: boolean
  currentUsername?: string
  currentUserId?: string
  currentUserRole?: string
  onOpenThread: (id: string) => void
  onEditMessage: (message: Message) => void
}) {
  const profile = useUserProfile(message.user_id, message.user)
  const name = message.user ? profile.displayName : 'Unknown'

  if (message.deleted_at) {
    return (
      <div
        data-message-id={message.id}
        className={
          'grid grid-cols-[34px_minmax(0,1fr)] gap-2 px-4 ' + (grouped ? 'py-0.5 ' : 'py-1.5 ')
        }
      >
        <div className="flex items-start justify-center pt-1">
          {grouped ? null : <Avatar name={name} color={profile.avatarColor} avatarUrl={profile.avatarUrl} size={30} />}
        </div>
        <div>
          {!grouped && (
            <div className="flex items-baseline gap-2">
              <b className="font-display text-sm font-semibold text-ink">{name}</b>
              <time className="font-mono text-[11px] text-ink-3">{formatTime(message.created_at)}</time>
            </div>
          )}
          <div className="text-sm italic text-ink-3">{deletedPlaceholder(message, currentUserId)}</div>
        </div>
      </div>
    )
  }

  const isAuthor = message.user_id === currentUserId
  const isAdmin = currentUserRole === 'admin'
  const canEdit = isAuthor && Date.now() - message.created_at < EDIT_WINDOW_MS
  const canDelete = isAuthor || isAdmin

  return (
    <div
      data-message-id={message.id}
      className={
        'group relative grid grid-cols-[34px_minmax(0,1fr)] gap-2 px-4 transition-colors duration-1000 ' +
        (grouped ? 'py-0.5 ' : 'py-1.5 ')
      }
    >
      <div className="flex items-start justify-center pt-1">
        {grouped ? (
          <time className="text-[10px] text-ink-3 opacity-0 group-hover:opacity-100">
            {formatTime(message.created_at)}
          </time>
        ) : message.user ? (
          <ProfilePopoverTrigger user={message.user}>
            <Avatar name={name} color={profile.avatarColor} avatarUrl={profile.avatarUrl} size={30} />
          </ProfilePopoverTrigger>
        ) : (
          <Avatar name={name} color="#999" size={30} />
        )}
      </div>
      <div className={message.status === 'sending' ? 'opacity-60' : ''}>
        {!grouped && (
          <div className="flex items-baseline gap-2">
            {message.user ? (
              <ProfilePopoverTrigger user={message.user}>
                <b className="font-display text-sm font-semibold text-ink">{name}</b>
              </ProfilePopoverTrigger>
            ) : (
              <b className="font-display text-sm font-semibold text-ink">{name}</b>
            )}
            {profile.isBot && (
              <span className="rounded bg-paper-3 px-1 font-mono text-[8px] text-ink-2">BOT</span>
            )}
            <time className="font-mono text-[11px] text-ink-3">{formatTime(message.created_at)}</time>
            {message.edited_at && (
              <span className="font-mono text-[10px] text-ink-3" title={new Date(message.edited_at).toLocaleString()}>
                (edited)
              </span>
            )}
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
        <ReactionBadges message={message} currentUserId={currentUserId} />
        <ThreadStrip message={message} onOpen={() => onOpenThread(message.id)} />
      </div>
      <MessageActions
        message={message}
        canEdit={canEdit}
        canDelete={canDelete}
        currentUserId={currentUserId}
        onEdit={() => onEditMessage(message)}
      />
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
  {
    channelId: string
    lastReadMessageId: string | null
    currentUsername?: string
    currentUserId?: string
    currentUserRole?: string
    onEditMessage?: (message: Message) => void
  }
>(function MessageList({ channelId, lastReadMessageId, currentUsername, currentUserId, currentUserRole, onEditMessage }, ref) {
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

  // Mark the channel read whenever it's open and showing its newest loaded message —
  // on first load and again as new messages arrive live. This is what actually clears
  // the sidebar's unread badge; the unread divider above is a separate, frozen anchor.
  const latestMessageId = messages[messages.length - 1]?.id
  const lastMarkedRef = useRef<string | null>(null)
  useEffect(() => {
    if (!latestMessageId || lastMarkedRef.current === latestMessageId) return
    lastMarkedRef.current = latestMessageId
    void api
      .markRead(channelId, latestMessageId)
      .then(() => {
        queryClient.invalidateQueries({ queryKey: ['unreads'] })
      })
      .catch(() => {
        lastMarkedRef.current = null
      })
  }, [channelId, latestMessageId, queryClient])

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
    el.scrollIntoView({ block: 'center' })
    if (!highlight || prefersReducedMotion()) return
    el.classList.add('flash-highlight')
    window.setTimeout(() => el.classList.remove('flash-highlight'), 1400)
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
      className="stream flex-1 overflow-y-auto"
    >
      <div ref={sentinelRef} />
      {isFetchingNextPage && <div className="py-2 text-center text-xs text-ink-3">Loading…</div>}
      {messages.map((m) => {
        const dk = dayKey(m.created_at)
        const showDay = dk !== lastDayKey
        const showUnreadDivider = Boolean(
          !dividerRendered && unreadAnchor && m.id !== unreadAnchor && Number(m.id) > Number(unreadAnchor),
        )
        const grouped = shouldGroup(lastAuthor, lastTs, m.user_id, m.created_at, showUnreadDivider)

        lastAuthor = m.user_id
        lastTs = m.created_at
        lastDayKey = dk
        if (showUnreadDivider) dividerRendered = true

        return (
          <div key={m.id}>
            {showDay && (
              <div className="sticky top-0 z-10 flex items-center gap-3 px-4 py-2.5">
                <div className="h-px flex-1 bg-rule-soft" />
                <span className="lbl rounded-full border border-rule bg-paper-2 px-2.5 py-0.5">
                  {formatDay(m.created_at)}
                </span>
                <div className="h-px flex-1 bg-rule-soft" />
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
              currentUserId={currentUserId}
              currentUserRole={currentUserRole}
              onOpenThread={openThread}
              onEditMessage={(message) => onEditMessage?.(message)}
            />
          </div>
        )
      })}
      <div ref={bottomRef} />
    </div>
  )
})
