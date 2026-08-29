import { useEffect, useRef, useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { renderMarkdown } from '../lib/markdown'
import { useDeleteMessage, useThread } from '../hooks/useMessages'
import { useUiStore } from '../store/ui'
import { formatTime } from '../lib/time'
import { shouldGroup } from '../lib/messageGrouping'
import { api, type Channel, type DM, type Message } from '../lib/api'
import { dmDisplayName } from '../lib/dm'
import { Composer, type ComposerHandle } from './Composer'
import { Avatar } from './Avatar'
import { PopoverMenu, MenuItem } from './PopoverMenu'
import { DeleteMessageDialog } from './DeleteMessageDialog'

const EDIT_WINDOW_MS = 15 * 60 * 1000

function deletedPlaceholder(message: Message, currentUserId?: string): string {
  const deletedBy = message.deleted_by
  if (!deletedBy || deletedBy.is_self) return 'This message has been deleted.'
  if (deletedBy.id === currentUserId) return 'Message deleted (Moderated)'
  if (message.user_id === currentUserId) return 'Your message was deleted by an administrator.'
  return 'This message has been deleted.'
}

function ThreadMessageActions({
  message,
  canEdit,
  canDelete,
  onEdit,
}: {
  message: Message
  canEdit: boolean
  canDelete: boolean
  onEdit: () => void
}) {
  const [menuOpen, setMenuOpen] = useState(false)
  const [confirmOpen, setConfirmOpen] = useState(false)
  const deleteMutation = useDeleteMessage(message.channel_id)

  if (!canEdit && !canDelete) return null

  return (
    <div className="absolute right-2 top-0 hidden items-center gap-0.5 rounded-md border border-rule bg-paper shadow-sm group-hover:flex">
      {canEdit && (
        <button
          type="button"
          title="Edit message"
          aria-label="Edit message"
          onClick={onEdit}
          className="px-1 py-0.5 text-ink-3 hover:text-ink"
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
            className="px-1 py-0.5 text-ink-3 hover:text-ink"
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

/** Resolves a channel/DM id to a human "#slug" or "@username" label from the cached
 * channels/DMs lists, for the thread panel's header subtitle. */
function useChannelLabel(channelId: string | undefined): string | null {
  const channelsQuery = useQuery({ queryKey: ['channels'], queryFn: api.listChannels, enabled: !!channelId })
  const dmsQuery = useQuery({ queryKey: ['dms'], queryFn: api.listDMs, enabled: !!channelId })
  if (!channelId) return null
  const channel = channelsQuery.data?.data.find((c: Channel) => c.id === channelId)
  if (channel) return `#${channel.slug ?? channel.name}`
  const dm = dmsQuery.data?.data.find((d: DM) => d.id === channelId)
  if (dm) return dm.kind === 'dm' && dm.peer ? `@${dm.peer.username}` : dmDisplayName(dm)
  return null
}

export function ThreadPanel({
  currentUsername,
  currentUserId,
  currentUserRole,
}: {
  currentUsername?: string
  currentUserId?: string
  currentUserRole?: string
}) {
  const openThreadId = useUiStore((s) => s.openThreadId)
  const closeThread = useUiStore((s) => s.closeThread)
  const { data, isLoading } = useThread(openThreadId)
  const channelLabel = useChannelLabel(data?.root.channel_id)
  const composerRef = useRef<ComposerHandle>(null)
  const isRootLocked = Boolean(data?.root.deleted_at)

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
          <h3 className="font-display text-sm font-semibold text-ink">
            {isRootLocked ? 'This root message has been deleted' : 'Thread'}
          </h3>
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
            <div className="group relative border-b border-rule px-4 py-3">
              <div className="flex items-baseline gap-2">
                <Avatar
                  name={data.root.user?.display_name || data.root.user?.username || 'Unknown'}
                  color={data.root.user?.avatar_color ?? '#999'}
                  avatarUrl={data.root.user?.avatar_url}
                  size={30}
                />
                <b className="font-display text-sm font-semibold text-ink">
                  {data.root.user?.display_name || data.root.user?.username}
                </b>
                {data.root.user?.is_bot && (
                  <span className="rounded bg-paper-3 px-1 font-mono text-[8px] text-ink-2">BOT</span>
                )}
                <time className="font-mono text-[9px] text-ink-3">{formatTime(data.root.created_at)}</time>
                {data.root.edited_at && !data.root.deleted_at && (
                  <span className="font-mono text-[9px] text-ink-3" title={new Date(data.root.edited_at).toLocaleString()}>
                    (edited)
                  </span>
                )}
              </div>
              {data.root.deleted_at ? (
                <div className="mt-1 text-sm italic text-ink-3">{deletedPlaceholder(data.root, currentUserId)}</div>
              ) : (
                <div className="mt-1 text-sm text-ink">{renderMarkdown(data.root.body, { currentUsername })}</div>
              )}
              {!data.root.deleted_at && (
                <ThreadMessageActions
                  message={data.root}
                  canEdit={data.root.user_id === currentUserId && Date.now() - data.root.created_at < EDIT_WINDOW_MS}
                  canDelete={data.root.user_id === currentUserId || currentUserRole === 'admin'}
                  onEdit={() => composerRef.current?.startEdit(data.root)}
                />
              )}
            </div>
            <div className="flex items-center gap-[10px] px-4 pb-1 pt-2.5">
              <span className="lbl">
                {data.data.length} {data.data.length === 1 ? 'reply' : 'replies'}
              </span>
              <div className="h-px flex-1 bg-rule" />
            </div>
            <div className="flex flex-col gap-1 px-4 pb-4">
              {data.data.map((reply) => {
                const grouped = shouldGroup(lastAuthor, lastTs, reply.user_id, reply.created_at, false)
                lastAuthor = reply.user_id
                lastTs = reply.created_at
                const canEditReply =
                  reply.user_id === currentUserId && Date.now() - reply.created_at < EDIT_WINDOW_MS
                const canDeleteReply = reply.user_id === currentUserId || currentUserRole === 'admin'
                return (
                  <div key={reply.id} className="group relative flex items-baseline gap-2 py-1">
                    {grouped ? (
                      <time className="w-[26px] shrink-0 text-[9px] text-ink-3 opacity-0 group-hover:opacity-100">
                        {formatTime(reply.created_at)}
                      </time>
                    ) : (
                      <Avatar
                        name={reply.user?.display_name || reply.user?.username || 'Unknown'}
                        color={reply.user?.avatar_color ?? '#999'}
                        avatarUrl={reply.user?.avatar_url}
                        size={26}
                      />
                    )}
                    <div className="min-w-0 flex-1">
                      {!grouped && (
                        <div className="flex items-baseline gap-2">
                          <b className="font-display text-xs font-semibold text-ink">
                            {reply.user?.display_name || reply.user?.username}
                          </b>
                          {reply.user?.is_bot && (
                            <span className="rounded bg-paper-3 px-1 font-mono text-[8px] text-ink-2">BOT</span>
                          )}
                          <time className="font-mono text-[9px] text-ink-3">{formatTime(reply.created_at)}</time>
                          {reply.edited_at && !reply.deleted_at && (
                            <span className="font-mono text-[9px] text-ink-3" title={new Date(reply.edited_at).toLocaleString()}>
                              (edited)
                            </span>
                          )}
                        </div>
                      )}
                      {reply.deleted_at ? (
                        <div className="text-sm italic text-ink-3">{deletedPlaceholder(reply, currentUserId)}</div>
                      ) : (
                        <div className="text-sm text-ink">{renderMarkdown(reply.body, { currentUsername })}</div>
                      )}
                    </div>
                    {!reply.deleted_at && (
                      <ThreadMessageActions
                        message={reply}
                        canEdit={canEditReply}
                        canDelete={canDeleteReply}
                        onEdit={() => composerRef.current?.startEdit(reply)}
                      />
                    )}
                  </div>
                )
              })}
            </div>
          </>
        )}
      </div>
      {data && isRootLocked && (
        <div className="border-t border-rule bg-paper-2 px-4 py-3 text-center text-[12.5px] text-ink-3">
          This thread has been closed because the root message was deleted.
        </div>
      )}
      {data && !isRootLocked && (
        <Composer
          ref={composerRef}
          channelId={data.root.channel_id}
          threadId={data.root.id}
          placeholder="Reply…"
          currentUserId={currentUserId}
        />
      )}
    </aside>
  )
}
