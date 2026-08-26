import { useEffect, useState } from 'react'
import { useUiStore } from '../store/ui'
import { useUserName } from '../hooks/useUserName'

function TypingName({ userId }: { userId: string }) {
  return <>{useUserName(userId)}</>
}

export function TypingIndicator({ channelId, currentUserId }: { channelId: string; currentUserId?: string }) {
  const typingMap = useUiStore((s) => s.typing[channelId])
  const pruneTyping = useUiStore((s) => s.pruneTyping)
  const [, tick] = useState(0)

  useEffect(() => {
    const interval = setInterval(() => {
      pruneTyping(channelId, Date.now())
      tick((n) => n + 1)
    }, 1000)
    return () => clearInterval(interval)
  }, [channelId, pruneTyping])

  const userIds = Object.keys(typingMap ?? {}).filter((id) => id !== currentUserId)
  if (userIds.length === 0) return null

  const shown = userIds.slice(0, 3)
  const rest = userIds.length - shown.length

  return (
    <div className="flex items-center gap-2 px-4 py-1 font-mono text-xs text-ink-3">
      <span className="flex gap-0.5">
        <i className="h-1 w-1 animate-bounce rounded-full bg-ink-3 [animation-delay:0ms]" />
        <i className="h-1 w-1 animate-bounce rounded-full bg-ink-3 [animation-delay:150ms]" />
        <i className="h-1 w-1 animate-bounce rounded-full bg-ink-3 [animation-delay:300ms]" />
      </span>
      <span>
        {shown.map((id, i) => (
          <span key={id}>
            <TypingName userId={id} />
            {i < shown.length - 1 ? ', ' : ''}
          </span>
        ))}
        {rest > 0 ? ` and ${rest} other${rest > 1 ? 's' : ''}` : ''} {userIds.length === 1 ? 'is' : 'are'} typing…
      </span>
    </div>
  )
}
