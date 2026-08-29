import { useQuery, useQueryClient } from '@tanstack/react-query'
import { useEffect, useState } from 'react'
import { api } from '../lib/api'
import { dmDisplayName } from '../lib/dm'

export interface ResolvedChannel {
  id: string
  name: string
  topic: string
  kind: 'public' | 'private' | 'dm' | 'group_dm' | null
  lastReadMessageId: string | null
  isLoading: boolean
  /** True once the backing list has been fetched at least once and no longer contains this
   * channel/DM — distinct from `isLoading`, so callers can tell "still loading" apart from
   * "genuinely gone" (e.g. you left the channel, or the conversation was removed) instead of
   * showing an indefinite loading spinner for both. */
  notFound: boolean
}

/** Resolves a `#slug` route param to its channel id + read watermark from the cached channel list. */
export function useChannelBySlug(slug: string | undefined): ResolvedChannel {
  const query = useQuery({ queryKey: ['channels'], queryFn: api.listChannels })
  const channel = query.data?.data.find((c) => c.slug === slug)
  return {
    id: channel?.id ?? '',
    name: channel?.name ?? slug ?? '',
    topic: channel?.topic ?? '',
    kind: channel?.kind ?? null,
    lastReadMessageId: channel?.last_read_message_id ?? null,
    isLoading: query.isLoading,
    notFound: !!slug && query.isFetched && !channel,
  }
}

/** Resolves a `@username` route param to its DM channel id, creating the DM on first open. */
export function useDmByUsername(username: string | undefined): ResolvedChannel {
  const queryClient = useQueryClient()
  const dmsQuery = useQuery({ queryKey: ['dms'], queryFn: api.listDMs })
  const [creating, setCreating] = useState(false)

  const dm = dmsQuery.data?.data.find((d) => d.kind === 'dm' && d.peer?.username === username)

  useEffect(() => {
    if (dm || creating || !username || dmsQuery.isLoading) return
    setCreating(true)
    void (async () => {
      try {
        const users = await api.listUsers({ q: username, limit: 5 })
        const peer = users.data.find((u) => u.username === username)
        if (peer) {
          await api.createDM([peer.id])
          await queryClient.invalidateQueries({ queryKey: ['dms'] })
        }
      } finally {
        setCreating(false)
      }
    })()
  }, [dm, creating, username, dmsQuery.isLoading, queryClient])

  return {
    id: dm?.id ?? '',
    name: dm?.peer?.display_name || dm?.peer?.username || username || '',
    topic: '',
    kind: dm ? 'dm' : null,
    lastReadMessageId: dm?.last_read_message_id ?? null,
    isLoading: dmsQuery.isLoading || (!dm && creating),
    // This route auto-creates the DM on first open (see the effect above), so "not found in
    // the list yet" is expected and transient here, not a signal that the conversation was
    // removed — leave notFound false and let isLoading cover the whole create flow.
    notFound: false,
  }
}

/** Resolves a DM or group DM route param by channel id from the cached DM list — no
 * create-on-open needed, since the caller always already has a real channel id (from a
 * POST /dms response or the sidebar list). */
export function useDmById(id: string | undefined): ResolvedChannel {
  const dmsQuery = useQuery({ queryKey: ['dms'], queryFn: api.listDMs })
  const dm = dmsQuery.data?.data.find((d) => d.id === id)

  return {
    id: dm?.id ?? '',
    name: dm ? dmDisplayName(dm) : '',
    topic: '',
    kind: dm?.kind ?? null,
    lastReadMessageId: dm?.last_read_message_id ?? null,
    isLoading: dmsQuery.isLoading,
    notFound: !!id && dmsQuery.isFetched && !dm,
  }
}
