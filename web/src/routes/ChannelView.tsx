import { useParams } from 'react-router-dom'
import { useEffect, useRef } from 'react'
import { useQuery } from '@tanstack/react-query'
import { MessageList, type MessageListHandle } from '../components/MessageList'
import { Composer } from '../components/Composer'
import { PulseRuler } from '../components/PulseRuler'
import { TypingIndicator } from '../components/TypingIndicator'
import { useAuth } from '../hooks/useAuth'
import { useChannelBySlug, useDmByUsername, useDmById } from '../hooks/useResolvedChannel'
import { useUiStore } from '../store/ui'
import { api } from '../lib/api'
import { Avatar } from '../components/Avatar'

const FACE_STACK_LIMIT = 5

function SearchButton() {
  const openSearchOverlay = useUiStore((s) => s.openSearchOverlay)
  return (
    <button
      type="button"
      onClick={() => openSearchOverlay()}
      className="ml-auto flex items-center gap-1.5 rounded-md px-2 py-1 text-[12.5px] text-ink-2 hover:bg-paper-2 hover:text-ink"
    >
      <svg width="13" height="13" viewBox="0 0 16 16" fill="none" stroke="currentColor" strokeWidth="1.6" aria-hidden>
        <circle cx="7" cy="7" r="4.4" />
        <path d="M10.3 10.3 14 14" />
      </svg>
      Search
    </button>
  )
}

function FaceStack({ channelId }: { channelId: string }) {
  const { data } = useQuery({
    queryKey: ['channel-members', channelId],
    queryFn: () => api.listChannelMembers(channelId),
    enabled: !!channelId,
  })
  const members = data?.data ?? []
  if (members.length === 0) return null
  const visible = members.slice(0, FACE_STACK_LIMIT)
  const overflow = members.length - visible.length

  return (
    <div className="flex items-center" aria-label={`${members.length} members`}>
      {visible.map((m) => (
        <Avatar
          key={m.id}
          name={m.display_name || m.username}
          color={m.avatar_color}
          size={22}
          className="-ml-1.5 border-[1.5px] border-paper first:ml-0"
          title={m.display_name || m.username}
        />
      ))}
      {overflow > 0 && (
        <span
          className="-ml-1.5 flex h-[22px] w-[22px] shrink-0 items-center justify-center border-[1.5px] border-paper bg-paper-3 font-mono text-[9px] text-ink-2"
          style={{ borderRadius: 5 }}
        >
          +{overflow}
        </span>
      )}
    </div>
  )
}

function ChannelHeader({
  channelId,
  name,
  topic,
  isPrivate,
}: {
  channelId: string
  name: string
  topic: string
  isPrivate: boolean
}) {
  return (
    <header className="flex items-center gap-3 border-b border-rule px-4 py-3">
      <h2 className="flex items-center gap-1 font-display text-lg font-semibold text-ink">
        <span className="font-normal text-ink-3">{isPrivate ? '🔒' : '#'}</span>
        {name}
      </h2>
      {topic && <span className="truncate border-l border-rule pl-3 text-[13px] text-ink-2">{topic}</span>}
      <FaceStack channelId={channelId} />
      <SearchButton />
    </header>
  )
}

function DmHeader({ name, isGroup, memberCount }: { name: string; isGroup?: boolean; memberCount?: number }) {
  return (
    <header className="flex items-center gap-3 border-b border-rule px-4 py-3">
      <h2 className="font-display text-lg font-semibold text-ink">{isGroup ? name : `@${name}`}</h2>
      <span className="truncate border-l border-rule pl-3 text-[13px] text-ink-2">
        {isGroup ? `Direct message · ${memberCount ?? 0} people` : 'Direct message · only the two of you'}
      </span>
      <SearchButton />
    </header>
  )
}

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
      <ChannelHeader
        channelId={channel.id}
        name={channel.name}
        topic={channel.topic}
        isPrivate={channel.kind === 'private'}
      />
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
      <DmHeader name={dm.name} />
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

export function DmByIdView() {
  const { id } = useParams()
  const { data: auth } = useAuth()
  const dm = useDmById(id)
  const dmsQuery = useQuery({ queryKey: ['dms'], queryFn: api.listDMs })
  const memberCount = dmsQuery.data?.data.find((d) => d.id === id)?.members?.length
  const listRef = useRef<MessageListHandle>(null)
  usePendingJump(dm.id, listRef)

  return (
    <div className="flex h-full flex-col">
      <DmHeader name={dm.name} isGroup={dm.kind === 'group_dm'} memberCount={memberCount} />
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
          <Composer channelId={dm.id} placeholder={`Message ${dm.name}`} />
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
