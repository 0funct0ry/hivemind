import { Link, useParams } from 'react-router-dom'
import { useEffect, useRef } from 'react'
import { useQuery } from '@tanstack/react-query'
import { MessageList, type MessageListHandle } from '../components/MessageList'
import { Composer, type ComposerHandle } from '../components/Composer'
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

/** Shown in place of the message stream when the currently-routed channel/DM has vanished
 * out from under the viewer — they left the channel or removed the conversation (from this
 * tab, another tab, or another device) while it was still open here. Distinct from the
 * loading state so the user gets a clear explanation instead of an indefinite spinner. */
function GoneState({ message }: { message: string }) {
  return (
    <div role="log" aria-live="polite" className="flex flex-1 flex-col items-center justify-center gap-3 px-6 text-center">
      <svg width="72" height="72" viewBox="0 0 128 128" className="shrink-0 opacity-60" aria-hidden>
        <path
          d="M64 10 112 37.5V92.5L64 120 16 92.5V37.5Z"
          fill="none"
          stroke="currentColor"
          className="text-rule"
          strokeWidth="2"
          strokeLinejoin="round"
        />
        <circle cx="64" cy="64" r="16" className="fill-paper-3" />
        <path d="M56 56 72 72M72 56 56 72" stroke="currentColor" className="text-ink-3" strokeWidth="2.5" strokeLinecap="round" />
      </svg>
      <p className="max-w-xs text-[15px] text-ink-2">{message}</p>
      <Link
        to="/"
        className="rounded-md border border-rule bg-paper px-3 py-1.5 text-sm text-ink-2 hover:border-teal hover:text-teal"
      >
        Back to hivemind
      </Link>
    </div>
  )
}

export function ChannelView() {
  const { slug } = useParams()
  const { data: auth } = useAuth()
  const channel = useChannelBySlug(slug)
  const listRef = useRef<MessageListHandle>(null)
  const composerRef = useRef<ComposerHandle>(null)
  usePendingJump(channel.id, listRef)

  if (channel.notFound) {
    return (
      <div className="flex h-full flex-col">
        <GoneState message="This channel is no longer available — you may have left it, or it was archived." />
      </div>
    )
  }

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
            currentUserId={auth?.user.id}
            currentUserRole={auth?.user.role}
            onEditMessage={(message) => composerRef.current?.startEdit(message)}
          />
          <TypingIndicator channelId={channel.id} currentUserId={auth?.user.id} />
          <Composer ref={composerRef} channelId={channel.id} placeholder={`Message #${slug}`} currentUserId={auth?.user.id} />
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
  const composerRef = useRef<ComposerHandle>(null)
  usePendingJump(dm.id, listRef)

  if (dm.notFound) {
    return (
      <div className="flex h-full flex-col">
        <GoneState message="This conversation is no longer available." />
      </div>
    )
  }

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
            currentUserId={auth?.user.id}
            currentUserRole={auth?.user.role}
            onEditMessage={(message) => composerRef.current?.startEdit(message)}
          />
          <TypingIndicator channelId={dm.id} currentUserId={auth?.user.id} />
          <Composer ref={composerRef} channelId={dm.id} placeholder={`Message @${username}`} currentUserId={auth?.user.id} />
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
  const composerRef = useRef<ComposerHandle>(null)
  usePendingJump(dm.id, listRef)

  if (dm.notFound) {
    return (
      <div className="flex h-full flex-col">
        <GoneState message="This conversation is no longer available." />
      </div>
    )
  }

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
            currentUserId={auth?.user.id}
            currentUserRole={auth?.user.role}
            onEditMessage={(message) => composerRef.current?.startEdit(message)}
          />
          <TypingIndicator channelId={dm.id} currentUserId={auth?.user.id} />
          <Composer ref={composerRef} channelId={dm.id} placeholder={`Message ${dm.name}`} currentUserId={auth?.user.id} />
        </>
      )}
    </div>
  )
}

/** A larger, six-dot echo of the sidebar's hexagon brand mark — keeps the empty state on the
 * same visual language instead of introducing an unrelated illustration. */
function NoChannelSelectedGraphic() {
  return (
    <svg width="128" height="128" viewBox="0 0 128 128" className="shrink-0" aria-hidden>
      <path
        d="M64 10 112 37.5V92.5L64 120 16 92.5V37.5Z"
        fill="none"
        stroke="currentColor"
        className="text-rule"
        strokeWidth="2"
        strokeLinejoin="round"
      />
      <circle cx="64" cy="64" r="16" className="fill-teal-soft" />
      <circle cx="64" cy="64" r="7" className="fill-teal" />
      <circle cx="64" cy="27.5" r="4.5" className="fill-pollen" />
      <circle cx="95.5" cy="45.75" r="4.5" className="fill-ink-3 opacity-35" />
      <circle cx="95.5" cy="82.25" r="4.5" className="fill-ink-3 opacity-35" />
      <circle cx="64" cy="100.5" r="4.5" className="fill-ink-3 opacity-35" />
      <circle cx="32.5" cy="82.25" r="4.5" className="fill-ink-3 opacity-35" />
      <circle cx="32.5" cy="45.75" r="4.5" className="fill-ink-3 opacity-35" />
    </svg>
  )
}

export function NoChannelSelected() {
  const openCommandPalette = useUiStore((s) => s.openCommandPalette)
  return (
    <div
      role="log"
      aria-live="polite"
      className="flex h-full flex-col items-center justify-center gap-5 px-6 text-center"
    >
      <NoChannelSelectedGraphic />
      <div className="max-w-xs">
        <h2 className="font-display text-xl font-semibold text-ink">Nothing selected yet</h2>
        <p className="mt-1.5 text-[15px] leading-relaxed text-ink-2">
          Pick a channel or direct message from the sidebar, or jump straight to one.
        </p>
      </div>
      <button
        type="button"
        onClick={() => openCommandPalette()}
        className="flex items-center gap-2 rounded-md border border-rule bg-paper px-3 py-1.5 text-sm text-ink-2 hover:border-teal hover:text-teal"
      >
        Jump to…
        <kbd className="rounded border border-rule px-1 font-mono text-[10px] text-ink-3">⌘K</kbd>
      </button>
    </div>
  )
}
