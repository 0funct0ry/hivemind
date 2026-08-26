import { useParams } from 'react-router-dom'

export function ChannelView() {
  const { slug } = useParams()
  return (
    <div className="flex h-full flex-col">
      <header className="border-b border-rule px-4 py-3">
        <h2 className="font-display text-lg font-semibold text-ink"># {slug}</h2>
      </header>
      <div role="log" aria-live="polite" className="flex flex-1 items-center justify-center text-ink-3">
        Messages are coming in M16.
      </div>
    </div>
  )
}

export function DmView() {
  const { username } = useParams()
  return (
    <div className="flex h-full flex-col">
      <header className="border-b border-rule px-4 py-3">
        <h2 className="font-display text-lg font-semibold text-ink">@{username}</h2>
      </header>
      <div role="log" aria-live="polite" className="flex flex-1 items-center justify-center text-ink-3">
        Messages are coming in M16.
      </div>
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
