import { useParams } from 'react-router-dom'
import { useEffect, useRef } from 'react'
import { MessageList, type MessageListHandle } from '../components/MessageList'
import { Composer } from '../components/Composer'
import { PulseRuler } from '../components/PulseRuler'
import { TypingIndicator } from '../components/TypingIndicator'
import { useAuth } from '../hooks/useAuth'
import { useChannelBySlug, useDmByUsername } from '../hooks/useResolvedChannel'
import { useUiStore } from '../store/ui'

/** Consumes a cross-route pendingJump handoff (from a search result) once its target
 * channel is mounted. */
function usePendingJump(channelId: string, listRef: React.RefObject<MessageListHandle | null>) {
  const pendingJump = useUiStore((s) => s.pendingJump)
  const clearPendingJump = useUiStore((s) => s.clearPendingJump)
  useEffect(() => {
    if (!pendingJump || pendingJump.channelId !== channelId) return
    void listRef.current?.scrollToMessage(pendingJump.messageId, { fetchIfMissing: true, highlight: true })
    clearPendingJump()
  }, [pendingJump, channelId, listRef, clearPendingJump])
}

export function ChannelView() {
  const { slug } = useParams()
  const { data: auth } = useAuth()
  const channel = useChannelBySlug(slug)
  const listRef = useRef<MessageListHandle>(null)
  usePendingJump(channel.id, listRef)

  return (
    <div className="flex h-full flex-col">
      <header className="border-b border-rule px-4 py-3">
        <h2 className="font-display text-lg font-semibold text-ink"># {channel.name}</h2>
      </header>
      {channel.isLoading || !channel.id ? (
        <div role="log" aria-live="polite" className="flex flex-1 items-center justify-center text-ink-3">
          Loading…
        </div>
      ) : (
        <>
          <PulseRuler
            channelId={channel.id}
            onJump={(messageId) => listRef.current?.scrollToMessage(messageId, { fetchIfMissing: true })}
          />
          <MessageList
            ref={listRef}
            channelId={channel.id}
            lastReadMessageId={channel.lastReadMessageId}
            currentUsername={auth?.user.username}
          />
          <TypingIndicator channelId={channel.id} currentUserId={auth?.user.id} />
          <Composer channelId={channel.id} placeholder={`Message #${slug}`} />
        </>
      )}
    </div>
  )
}

export function DmView() {
  const { username } = useParams()
  const { data: auth } = useAuth()
  const dm = useDmByUsername(username)
  const listRef = useRef<MessageListHandle>(null)
  usePendingJump(dm.id, listRef)

  return (
    <div className="flex h-full flex-col">
      <header className="border-b border-rule px-4 py-3">
        <h2 className="font-display text-lg font-semibold text-ink">@{dm.name}</h2>
      </header>
      {dm.isLoading || !dm.id ? (
        <div role="log" aria-live="polite" className="flex flex-1 items-center justify-center text-ink-3">
          Loading…
        </div>
      ) : (
        <>
          <PulseRuler
            channelId={dm.id}
            onJump={(messageId) => listRef.current?.scrollToMessage(messageId, { fetchIfMissing: true })}
          />
          <MessageList
            ref={listRef}
            channelId={dm.id}
            lastReadMessageId={dm.lastReadMessageId}
            currentUsername={auth?.user.username}
          />
          <TypingIndicator channelId={dm.id} currentUserId={auth?.user.id} />
          <Composer channelId={dm.id} placeholder={`Message @${username}`} />
        </>
      )}
    </div>
  )
}

export function NoChannelSelected() {
  return (
    <div role="log" aria-live="polite" className="flex h-full items-center justify-center text-ink-3">
      Select a channel or direct message.
    </div>
  )
}
