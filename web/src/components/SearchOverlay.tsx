import { useEffect, useRef, useState, type ReactNode } from 'react'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { useNavigate } from 'react-router-dom'
import { api, type SearchHit, type DM, type Channel } from '../lib/api'
import { useUiStore } from '../store/ui'
import { Modal } from './Modal'

const MARK_RE = /<mark>([\s\S]*?)<\/mark>/g

/**
 * Renders the server's FTS `snippet()` output, which embeds `<mark>` tags. Rather than
 * trusting that HTML via dangerouslySetInnerHTML, split on the one tag we expect and build
 * React nodes directly — no injection surface, matching lib/markdown.tsx's approach.
 */
function renderSnippet(snippet: string): ReactNode {
  const nodes: ReactNode[] = []
  let lastIndex = 0
  let key = 0
  for (const m of snippet.matchAll(MARK_RE)) {
    if (m.index! > lastIndex) nodes.push(snippet.slice(lastIndex, m.index))
    nodes.push(
      <mark key={key++} className="rounded bg-pollen-soft text-pollen">
        {m[1]}
      </mark>,
    )
    lastIndex = m.index! + m[0].length
  }
  if (lastIndex < snippet.length) nodes.push(snippet.slice(lastIndex))
  return <>{nodes}</>
}

interface Filters {
  in?: string
  from?: string
  has?: string
}

function parseInlineFilters(text: string): { filters: Filters; rest: string } {
  const filters: Filters = {}
  const rest = text
    .replace(/\bin:(\S+)/i, (_, v) => {
      filters.in = v
      return ''
    })
    .replace(/\bfrom:(\S+)/i, (_, v) => {
      filters.from = v
      return ''
    })
    .replace(/\bhas:(\S+)/i, (_, v) => {
      filters.has = v
      return ''
    })
    .trim()
  return { filters, rest }
}

export function SearchOverlay() {
  const open = useUiStore((s) => s.searchOverlayOpen)
  const close = useUiStore((s) => s.closeSearchOverlay)
  const setPendingJump = useUiStore((s) => s.setPendingJump)
  const navigate = useNavigate()
  const queryClient = useQueryClient()
  const inputRef = useRef<HTMLInputElement>(null)
  const [text, setText] = useState('')

  useEffect(() => {
    if (!open) setText('')
  }, [open])

  const { filters, rest } = parseInlineFilters(text)

  const { data, isFetching } = useQuery({
    queryKey: ['search', rest, filters.in, filters.from, filters.has],
    queryFn: () => api.search({ q: rest, in: filters.in, from: filters.from, has: filters.has, limit: 30 }),
    enabled: open && rest.trim() !== '',
  })

  if (!open) return null

  const hits: SearchHit[] = data?.data ?? []

  const jumpTo = (hit: SearchHit) => {
    close()
    setPendingJump({ channelId: hit.message.channel_id, messageId: hit.message.id })
    if (hit.channel.kind === 'dm') {
      const dms = queryClient.getQueryData<{ data: DM[] }>(['dms'])
      const dm = dms?.data.find((d) => d.id === hit.channel.id)
      if (dm) navigate(`/dm/${dm.peer.username}`)
      return
    }
    const channels = queryClient.getQueryData<{ data: Channel[] }>(['channels'])
    const channel = channels?.data.find((c) => c.id === hit.channel.id)
    if (channel?.slug) navigate(`/c/${channel.slug}`)
    else if (hit.channel.slug) navigate(`/c/${hit.channel.slug}`)
  }

  return (
    <Modal open={open} onClose={close} labelledBy="search-overlay-label" initialFocusRef={inputRef}>
      <div className="p-2">
        <h2 id="search-overlay-label" className="sr-only">
          Search
        </h2>
        <input
          ref={inputRef}
          value={text}
          onChange={(e) => setText(e.target.value)}
          placeholder="Search messages… (try in:general from:priya has:file)"
          className="w-full rounded-md border border-rule bg-paper px-3 py-2 text-sm text-ink outline-none focus-visible:ring-2 focus-visible:ring-teal"
        />
        {(filters.in || filters.from || filters.has) && (
          <div className="mt-1.5 flex flex-wrap gap-1.5">
            {filters.in && <span className="rounded-full bg-paper-3 px-2 py-0.5 font-mono text-[11px] text-ink-2">in: {filters.in}</span>}
            {filters.from && <span className="rounded-full bg-paper-3 px-2 py-0.5 font-mono text-[11px] text-ink-2">from: {filters.from}</span>}
            {filters.has && <span className="rounded-full bg-paper-3 px-2 py-0.5 font-mono text-[11px] text-ink-2">has: {filters.has}</span>}
          </div>
        )}
      </div>
      <ul className="max-h-96 overflow-y-auto px-2 pb-2">
        {rest.trim() === '' && <li className="px-2 py-2 text-sm text-ink-3">Type to search.</li>}
        {rest.trim() !== '' && !isFetching && hits.length === 0 && (
          <li className="px-2 py-2 text-sm text-ink-3">No results.</li>
        )}
        {hits.map((hit) => (
          <li key={hit.message.id}>
            <button
              type="button"
              onClick={() => jumpTo(hit)}
              className="flex w-full flex-col items-start gap-0.5 rounded-md px-2 py-1.5 text-left hover:bg-paper-3"
            >
              <span className="flex items-center gap-2 font-mono text-[11px] text-ink-3">
                <span>{hit.channel.kind === 'dm' ? '@' : '#'}{hit.channel.name}</span>
                <span>·</span>
                <span>{hit.message.user?.display_name || hit.message.user?.username}</span>
                <span>·</span>
                <time>{new Date(hit.message.created_at).toLocaleString()}</time>
              </span>
              <span className="text-sm text-ink">{renderSnippet(hit.snippet)}</span>
            </button>
          </li>
        ))}
      </ul>
    </Modal>
  )
}
