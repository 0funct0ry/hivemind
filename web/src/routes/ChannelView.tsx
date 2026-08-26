import { useParams } from 'react-router-dom'
import { MessageList } from '../components/MessageList'
import { Composer } from '../components/Composer'
import { TypingIndicator } from '../components/TypingIndicator'
import { useAuth } from '../hooks/useAuth'
import { useChannelBySlug, useDmByUsername } from '../hooks/useResolvedChannel'

export function ChannelView() {
  const { slug } = useParams()
  const { data: auth } = useAuth()
  const channel = useChannelBySlug(slug)

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
          <MessageList
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
          <MessageList
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
